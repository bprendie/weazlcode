package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
)

type artifactValidationIssue struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func (m model) validateArtifactTaskOutput(task coding.Task) []artifactValidationIssue {
	issues := m.validatePythonTaskOutputs(task)
	if !cohesiveWholeFileArtifactTask(task) {
		return issues
	}
	allowed := map[string]bool{}
	for _, path := range normalizedArtifactTaskPaths(task.AllowedPaths) {
		allowed[path] = true
	}
	indexPath := artifactIndexPath(allowed)
	cssPaths := artifactCSSPaths(allowed)
	if indexPath != "" {
		html, err := os.ReadFile(filepath.Join(m.project.Root, filepath.FromSlash(indexPath)))
		if err != nil {
			issues = append(issues, artifactValidationIssue{Path: indexPath, Message: "index HTML file is missing or unreadable: " + err.Error()})
		} else {
			issues = append(issues, validateIndexArtifact(indexPath, string(html), cssPaths)...)
		}
	}
	for _, cssPath := range cssPaths {
		css, err := os.ReadFile(filepath.Join(m.project.Root, filepath.FromSlash(cssPath)))
		if err != nil {
			issues = append(issues, artifactValidationIssue{Path: cssPath, Message: "CSS file is missing or unreadable: " + err.Error()})
			continue
		}
		issues = append(issues, validateCSSArtifact(cssPath, string(css))...)
	}
	issues = append(issues, m.validateArtifactSourceCopy(task)...)
	issues = append(issues, m.validateArtifactAssetReferences(task)...)
	return issues
}

func (m model) validatePythonTaskOutputs(task coding.Task) []artifactValidationIssue {
	var issues []artifactValidationIssue
	for _, path := range normalizedArtifactTaskPaths(task.AllowedPaths) {
		if !strings.HasSuffix(strings.ToLower(path), ".py") {
			continue
		}
		fullPath := filepath.Join(m.project.Root, filepath.FromSlash(path))
		if _, err := os.Stat(fullPath); err != nil {
			issues = append(issues, artifactValidationIssue{Path: path, Message: "Python file is missing or unreadable: " + err.Error()})
			continue
		}
		for _, message := range runPythonStaticValidator(fullPath) {
			issues = append(issues, artifactValidationIssue{Path: path, Message: message})
		}
	}
	return issues
}

func runPythonStaticValidator(path string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", pythonStaticValidatorScript, path)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		return []string{"Python static validation timed out"}
	}
	if err != nil && text == "" {
		return []string{"Python static validation failed: " + err.Error()}
	}
	var issues []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			issues = append(issues, line)
		}
	}
	return issues
}

const pythonStaticValidatorScript = `
import ast
import builtins
import difflib
import os
import sys

path = sys.argv[1]
try:
    source = open(path, "r", encoding="utf-8").read()
except Exception as exc:
    print(f"could not read Python file: {exc}")
    raise SystemExit(0)

try:
    tree = ast.parse(source, filename=path)
except SyntaxError as exc:
    guidance = ""
    if "unterminated string literal" in str(exc.msg).lower():
        guidance = "; avoid splitting normal quoted strings across physical lines. Use triple-quoted strings, adjacent quoted strings inside parentheses, or explicit \\n escapes for multiline text such as SQL."
    print(f"syntax error at line {exc.lineno}: {exc.msg}{guidance}")
    raise SystemExit(0)

module_names = set(dir(builtins)) | {"__name__", "__file__", "__package__", "__spec__"}
module_issues = []
from_imports = []

def add_target_names(target, names):
    if isinstance(target, ast.Name):
        names.add(target.id)
    elif isinstance(target, (ast.Tuple, ast.List)):
        for item in target.elts:
            add_target_names(item, names)

for node in tree.body:
    if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
        module_names.add(node.name)
    elif isinstance(node, ast.Import):
        for alias in node.names:
            module_names.add((alias.asname or alias.name.split(".")[0]))
    elif isinstance(node, ast.ImportFrom):
        for alias in node.names:
            if alias.name == "*":
                module = node.module or ""
                module_issues.append(f"wildcard import from {module or 'module'} cannot be statically validated; replace it with explicit imported names")
                continue
            module_names.add(alias.asname or alias.name)
            if node.level == 0 and node.module:
                from_imports.append((node.module, alias.name))
    elif isinstance(node, (ast.Assign, ast.AnnAssign, ast.AugAssign)):
        targets = getattr(node, "targets", [getattr(node, "target", None)])
        for target in targets:
            if target is not None:
                add_target_names(target, module_names)
    elif isinstance(node, (ast.For, ast.AsyncFor)):
        add_target_names(node.target, module_names)
    elif isinstance(node, ast.With):
        for item in node.items:
            if item.optional_vars is not None:
                add_target_names(item.optional_vars, module_names)

def exported_names_for_local_module(module):
    base = os.path.dirname(path)
    module_path = os.path.join(base, *module.split(".")) + ".py"
    if not os.path.exists(module_path):
        package_path = os.path.join(base, *module.split("."), "__init__.py")
        if os.path.exists(package_path):
            module_path = package_path
        else:
            return None
    try:
        module_source = open(module_path, "r", encoding="utf-8").read()
        module_tree = ast.parse(module_source, filename=module_path)
    except Exception:
        return None
    exports = set()
    for item in module_tree.body:
        if isinstance(item, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            exports.add(item.name)
        elif isinstance(item, ast.Import):
            for alias in item.names:
                exports.add(alias.asname or alias.name.split(".")[0])
        elif isinstance(item, ast.ImportFrom):
            for alias in item.names:
                if alias.name != "*":
                    exports.add(alias.asname or alias.name)
        elif isinstance(item, (ast.Assign, ast.AnnAssign, ast.AugAssign)):
            targets = getattr(item, "targets", [getattr(item, "target", None)])
            for target in targets:
                if isinstance(target, ast.Name):
                    exports.add(target.id)
                elif isinstance(target, (ast.Tuple, ast.List)):
                    for child in target.elts:
                        if isinstance(child, ast.Name):
                            exports.add(child.id)
    return exports

for module, name in from_imports:
    exports = exported_names_for_local_module(module)
    if exports is None or name in exports:
        continue
    suggestion = difflib.get_close_matches(name, sorted(exports), n=1)
    suffix = f"; did you mean {suggestion[0]}?" if suggestion else ""
    module_issues.append(f"from {module} import {name} references missing local export {name}{suffix}")

class ModuleStoreVisitor(ast.NodeVisitor):
    def __init__(self):
        self.names = set()

    def visit_FunctionDef(self, node):
        self.names.add(node.name)
        return

    def visit_AsyncFunctionDef(self, node):
        self.names.add(node.name)
        return

    def visit_ClassDef(self, node):
        self.names.add(node.name)
        return

    def visit_Lambda(self, node):
        return

    def visit_Name(self, node):
        if isinstance(node.ctx, ast.Store):
            self.names.add(node.id)

module_stores = ModuleStoreVisitor()
for node in tree.body:
    module_stores.visit(node)
module_names |= module_stores.names

class ModuleLoadVisitor(ast.NodeVisitor):
    def __init__(self):
        self.names = []

    def visit_FunctionDef(self, node):
        return

    def visit_AsyncFunctionDef(self, node):
        return

    def visit_ClassDef(self, node):
        return

    def visit_Lambda(self, node):
        return

    def visit_Name(self, node):
        if isinstance(node.ctx, ast.Load):
            self.names.append(node.id)

def assigned_target_names(target):
    names = set()
    add_target_names(target, names)
    return names

def load_names(node):
    names = set()
    for child in ast.walk(node):
        if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef, ast.Lambda)):
            continue
        if isinstance(child, ast.Name) and isinstance(child.ctx, ast.Load):
            names.add(child.id)
    return names

def assignment_names_in_stmt(stmt):
    names = set()
    for child in ast.walk(stmt):
        if isinstance(child, ast.Name) and isinstance(child.ctx, ast.Store):
            names.add(child.id)
    return names

def assigned_names_in_block(body):
    names = set()
    for stmt in body:
        names |= assignment_names_in_stmt(stmt)
    return names

class IssueVisitor(ast.NodeVisitor):
    def __init__(self):
        self.issues = []
        self.scope_stack = []
        self.loop_depth = 0

    def visit_ClassDef(self, node):
        methods = {child.name for child in node.body if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef))}
        init_attrs = set()
        for child in node.body:
            if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)) and child.name == "__init__":
                for sub in ast.walk(child):
                    if isinstance(sub, ast.Attribute) and isinstance(sub.ctx, ast.Store) and isinstance(sub.value, ast.Name) and sub.value.id == "self":
                        init_attrs.add(sub.attr)
        for name in sorted(methods & init_attrs):
            self.issues.append(f"class {node.name} assigns self.{name} in __init__, which shadows method {name}() at runtime")
        self.generic_visit(node)

    def visit_FunctionDef(self, node):
        self._check_function(node)
        locals_ = self._function_locals(node)
        self.scope_stack.append(locals_)
        self.generic_visit(node)
        self.scope_stack.pop()

    def visit_AsyncFunctionDef(self, node):
        self._check_function(node)
        locals_ = self._function_locals(node)
        self.scope_stack.append(locals_)
        self.generic_visit(node)
        self.scope_stack.pop()

    def visit_While(self, node):
        is_true_loop = isinstance(node.test, ast.Constant) and node.test.value is True
        if self.loop_depth > 0 and is_true_loop:
            self.issues.append("nested while True inside another loop can hang interactive control flow; use game state branches or return/break to the outer loop instead")
        self.loop_depth += 1
        self.generic_visit(node)
        self.loop_depth -= 1

    def _function_locals(self, node):
        locals_ = set()
        for arg in list(node.args.posonlyargs) + list(node.args.args) + list(node.args.kwonlyargs):
            locals_.add(arg.arg)
        if node.args.vararg:
            locals_.add(node.args.vararg.arg)
        if node.args.kwarg:
            locals_.add(node.args.kwarg.arg)
        for sub in ast.walk(node):
            if isinstance(sub, ast.Name) and isinstance(sub.ctx, ast.Store):
                locals_.add(sub.id)
            elif isinstance(sub, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                locals_.add(sub.name)
            elif isinstance(sub, ast.ExceptHandler) and sub.name:
                locals_.add(sub.name)
            elif isinstance(sub, (ast.Import, ast.ImportFrom)):
                for alias in sub.names:
                    locals_.add(alias.asname or alias.name.split(".")[0])
        return locals_

    def _check_function(self, node):
        locals_ = self._function_locals(node)
        inherited = set()
        for scope in self.scope_stack:
            inherited |= scope
        undefined = []
        for sub in ast.walk(node):
            if isinstance(sub, ast.Name) and isinstance(sub.ctx, ast.Load):
                if sub.id not in locals_ and sub.id not in inherited and sub.id not in module_names:
                    undefined.append(sub.id)
        for name in sorted(set(undefined)):
            if name.endswith("_rect"):
                self.issues.append(f"function {node.name} references undefined name {name}; for pygame UI rectangles shared across methods, define self.{name} before event handling and reference self.{name} instead of a local {name}")
            else:
                self.issues.append(f"function {node.name} references undefined name {name}")
        self._check_unstable_branch_locals(node)

    def _check_unstable_branch_locals(self, node):
        def walk_block(body, unstable, stable=None):
            unstable = dict(unstable)
            stable = set(stable or set())
            for stmt in body:
                if isinstance(stmt, (ast.For, ast.AsyncFor)):
                    loop_targets = assigned_target_names(stmt.target)
                    loaded = load_names(stmt.iter)
                    for name, source in sorted(unstable.items()):
                        if name in loaded:
                            self.issues.append(f"function {node.name} references {source}-scoped local {name} after the block where it may not be defined; initialize {name} before the block or keep its use inside that block")
                    body_unstable = dict(unstable)
                    for name in loop_targets:
                        body_unstable.pop(name, None)
                    walk_block(stmt.body, body_unstable, stable | loop_targets)
                    for name in loop_targets:
                        if name not in stable:
                            unstable[name] = "loop"
                    walk_block(stmt.orelse, unstable, stable)
                elif isinstance(stmt, ast.If):
                    loaded = load_names(stmt.test)
                    for name, source in sorted(unstable.items()):
                        if name in loaded:
                            self.issues.append(f"function {node.name} references {source}-scoped local {name} after the block where it may not be defined; initialize {name} before the block or keep its use inside that block")
                    walk_block(stmt.body, unstable, stable)
                    walk_block(stmt.orelse, unstable, stable)
                    body_assigned = assigned_names_in_block(stmt.body)
                    else_assigned = assigned_names_in_block(stmt.orelse)
                    branch_only = (body_assigned | else_assigned) - (body_assigned & else_assigned)
                    for name in branch_only:
                        if name not in stable:
                            unstable[name] = "conditional"
                    for name in body_assigned & else_assigned:
                        unstable.pop(name, None)
                        stable.add(name)
                elif isinstance(stmt, (ast.While, ast.With, ast.Try)):
                    loaded = load_names(getattr(stmt, "test", stmt))
                    assigned_here = assignment_names_in_stmt(stmt)
                    for name, source in sorted(unstable.items()):
                        if name in loaded and name not in assigned_here:
                            self.issues.append(f"function {node.name} references {source}-scoped local {name} after the block where it may not be defined; initialize {name} before the block or keep its use inside that block")
                    for child_body_name in ("body", "orelse", "finalbody"):
                        child_body = getattr(stmt, child_body_name, None)
                        if child_body:
                            walk_block(child_body, unstable, stable)
                    for handler in getattr(stmt, "handlers", []):
                        walk_block(handler.body, unstable, stable)
                else:
                    loaded = load_names(stmt)
                    assigned_here = assignment_names_in_stmt(stmt)
                    for name, source in sorted(unstable.items()):
                        if name in loaded and name not in assigned_here:
                            self.issues.append(f"function {node.name} references {source}-scoped local {name} after the block where it may not be defined; initialize {name} before the block or keep its use inside that block")
                    for name in assigned_here:
                        unstable.pop(name, None)
                        stable.add(name)
        walk_block(node.body, {})

visitor = IssueVisitor()
visitor.issues.extend(module_issues)
module_loads = ModuleLoadVisitor()
module_loads.visit(tree)
for name in sorted(set(module_loads.names)):
    if name not in module_names:
        visitor.issues.append(f"module top-level references undefined name {name}")
visitor.visit(tree)

has_pygame = "pygame" in source
is_entrypoint_file = os.path.basename(path) in {"main.py", "app.py", "__main__.py"}
has_interactive_loop = False
for node in ast.walk(tree):
    if isinstance(node, ast.While):
        has_interactive_loop = True
        break
    if isinstance(node, ast.Call):
        try:
            call_text = ast.unparse(node.func)
        except Exception:
            call_text = ""
        if call_text.endswith(".mainloop") or call_text.endswith(".run"):
            has_interactive_loop = True
            break
if is_entrypoint_file and has_pygame and has_interactive_loop and "--smoke" not in source:
    visitor.issues.append("interactive Python/Pygame artifact has no --smoke path for non-interactive validation; add a --smoke branch that initializes, performs one lightweight update/render or health check, and exits before the interactive loop")

for node in tree.body:
    if not isinstance(node, ast.If):
        continue
    try:
        test_text = ast.unparse(node.test)
    except Exception:
        test_text = ""
    if "__name__" in test_text:
        statements = node.body
    elif "--smoke" in test_text:
        statements = [node]
    else:
        continue
    for index, stmt in enumerate(statements):
        if not isinstance(stmt, ast.If):
            continue
        try:
            branch_text = ast.unparse(stmt.test)
        except Exception:
            branch_text = ""
        if "--smoke" not in branch_text:
            continue
        smoke_calls = False
        for child in ast.walk(ast.Module(body=stmt.body, type_ignores=[])):
            if isinstance(child, ast.Call):
                try:
                    call_text = ast.unparse(child.func)
                except Exception:
                    call_text = ""
                if "smoke" in call_text.lower() or call_text in {"sys.exit", "exit", "quit"}:
                    smoke_calls = True
            elif isinstance(child, ast.Raise):
                try:
                    raise_text = ast.unparse(child)
                except Exception:
                    raise_text = ""
                if "SystemExit" in raise_text:
                    smoke_calls = True
        if not smoke_calls:
            visitor.issues.append("--smoke branch does not call a smoke routine or exit before interactive execution")
        for later in node.body[index + 1:]:
            try:
                later_text = ast.unparse(later)
            except Exception:
                later_text = ""
            lower = later_text.lower()
            if any(token in lower for token in (".play(", ".run(", "main_loop(", ".loop(")) and "smoke" not in lower:
                visitor.issues.append("--smoke branch is followed by an unconditional interactive loop call")
                break

for issue in visitor.issues[:20]:
    print(issue)
`

func artifactIndexPath(allowed map[string]bool) string {
	for path := range allowed {
		lower := strings.ToLower(path)
		if lower == "index.html" || strings.HasSuffix(lower, "/index.html") {
			return path
		}
	}
	return ""
}

func artifactCSSPaths(allowed map[string]bool) []string {
	var out []string
	for path := range allowed {
		if strings.HasSuffix(strings.ToLower(path), ".css") {
			out = append(out, path)
		}
	}
	return out
}

func validateIndexArtifact(path, html string, cssPaths []string) []artifactValidationIssue {
	lower := strings.ToLower(html)
	var issues []artifactValidationIssue
	for _, required := range []string{"<!doctype", "<html", "<head", "<body"} {
		if !strings.Contains(lower, required) {
			issues = append(issues, artifactValidationIssue{Path: path, Message: fmt.Sprintf("index HTML is missing %s", required)})
		}
	}
	if len(cssPaths) > 0 && strings.Contains(lower, "<style") {
		issues = append(issues, artifactValidationIssue{Path: path, Message: "index HTML contains an inline <style> tag even though CSS output files are assigned"})
	}
	for _, cssPath := range cssPaths {
		if !strings.Contains(html, cssPath) && !strings.Contains(html, filepath.Base(cssPath)) {
			issues = append(issues, artifactValidationIssue{Path: path, Message: "index HTML does not reference assigned CSS file " + cssPath})
		}
	}
	issues = append(issues, placeholderArtifactIssues(path, html)...)
	return issues
}

func validateCSSArtifact(path, css string) []artifactValidationIssue {
	var issues []artifactValidationIssue
	if strings.TrimSpace(css) == "" {
		issues = append(issues, artifactValidationIssue{Path: path, Message: "CSS file is empty"})
	}
	if unbalancedCSSBraces(css) {
		issues = append(issues, artifactValidationIssue{Path: path, Message: "CSS braces are unbalanced"})
	}
	if strings.Contains(css, ": hover") || strings.Contains(css, ": before") || strings.Contains(css, ": after") {
		issues = append(issues, artifactValidationIssue{Path: path, Message: "CSS appears to contain corrupted pseudo-selector spacing"})
	}
	issues = append(issues, placeholderArtifactIssues(path, css)...)
	return issues
}

func (m model) validateArtifactSourceCopy(task coding.Task) []artifactValidationIssue {
	copyBlock := requiredSourceCopyBlock(task)
	if copyBlock == "" {
		return nil
	}
	output := m.copyBearingTaskOutput(task)
	if strings.TrimSpace(output) == "" {
		return []artifactValidationIssue{{Message: "required source copy check could not read generated text output"}}
	}
	var issues []artifactValidationIssue
	for _, fragment := range requiredCopyFragments(copyBlock) {
		if !copyContainsFragment(output, fragment) {
			issues = append(issues, artifactValidationIssue{Message: "required source-copy fragment is missing: " + fragment})
			if len(issues) >= 8 {
				break
			}
		}
	}
	return issues
}

func (m model) validateArtifactAssetReferences(task coding.Task) []artifactValidationIssue {
	taskText := strings.ToLower(task.Title + " " + task.Goal + " " + acceptanceCheckTextForArtifact(task.AcceptanceChecks))
	if !strings.Contains(taskText, "asset") && !strings.Contains(taskText, "image") && !strings.Contains(taskText, "png") && !strings.Contains(taskText, "screenshot") {
		return nil
	}
	assets := localArtifactAssetNames(m.project.Root)
	if len(assets) == 0 {
		return nil
	}
	output := m.copyBearingTaskOutput(task)
	var issues []artifactValidationIssue
	for _, asset := range assets {
		if !strings.Contains(output, asset) {
			issues = append(issues, artifactValidationIssue{Message: "local asset filename is not referenced: " + asset})
		}
	}
	return issues
}

func acceptanceCheckTextForArtifact(checks []coding.AcceptanceCheck) string {
	var b strings.Builder
	for _, check := range checks {
		b.WriteString(" ")
		b.WriteString(check.Description)
		b.WriteString(" ")
		b.WriteString(check.Command)
	}
	return b.String()
}

func localArtifactAssetNames(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var assets []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		switch {
		case strings.HasSuffix(lower, ".png"),
			strings.HasSuffix(lower, ".jpg"),
			strings.HasSuffix(lower, ".jpeg"),
			strings.HasSuffix(lower, ".webp"),
			strings.HasSuffix(lower, ".gif"),
			strings.HasSuffix(lower, ".svg"):
			assets = append(assets, name)
		}
	}
	return assets
}

func unbalancedCSSBraces(css string) bool {
	depth := 0
	for _, r := range css {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return true
			}
		}
	}
	return depth != 0
}

func placeholderArtifactIssues(path, content string) []artifactValidationIssue {
	lower := strings.ToLower(content)
	sentinels := []string{
		"todo",
		"omitted for brevity",
		"existing content omitted",
		"rest of the file",
		"remaining content unchanged",
		"previous content here",
	}
	var issues []artifactValidationIssue
	for _, sentinel := range sentinels {
		if strings.Contains(lower, sentinel) {
			issues = append(issues, artifactValidationIssue{Path: path, Message: "artifact contains placeholder sentinel " + sentinel})
		}
	}
	return issues
}

func formatArtifactValidationIssues(issues []artifactValidationIssue) string {
	var b strings.Builder
	b.WriteString("Artifact validation failed before reviewer handoff.")
	for _, issue := range issues {
		if issue.Path != "" {
			fmt.Fprintf(&b, "\n- %s: %s", issue.Path, issue.Message)
		} else {
			fmt.Fprintf(&b, "\n- %s", issue.Message)
		}
	}
	return b.String()
}

func artifactValidationPayload(issues []artifactValidationIssue) any {
	return struct {
		Issues []artifactValidationIssue `json:"issues"`
	}{Issues: issues}
}

func artifactValidationRepairCheck() coding.AcceptanceCheck {
	return coding.AcceptanceCheck{Description: "Artifact validator issues are fixed without broadening the task scope."}
}
