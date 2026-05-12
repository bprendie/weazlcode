package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type GitStatusTool struct{ limits Limits }
type GitDiffTool struct{ limits Limits }
type GitLogTool struct{ limits Limits }
type GitShowTool struct{ limits Limits }
type ListChangedFilesTool struct{ limits Limits }
type ApplyPatchTool struct{ limits Limits }

func NewGitStatusTool(limits Limits) *GitStatusTool { return &GitStatusTool{limits: limits} }
func NewGitDiffTool(limits Limits) *GitDiffTool     { return &GitDiffTool{limits: limits} }
func NewGitLogTool(limits Limits) *GitLogTool       { return &GitLogTool{limits: limits} }
func NewGitShowTool(limits Limits) *GitShowTool     { return &GitShowTool{limits: limits} }
func NewListChangedFilesTool(limits Limits) *ListChangedFilesTool {
	return &ListChangedFilesTool{limits: limits}
}
func NewApplyPatchTool(limits Limits) *ApplyPatchTool { return &ApplyPatchTool{limits: limits} }

func (t *GitStatusTool) Name() string        { return "git_status" }
func (t *GitDiffTool) Name() string          { return "git_diff" }
func (t *GitLogTool) Name() string           { return "git_log" }
func (t *GitShowTool) Name() string          { return "git_show" }
func (t *ListChangedFilesTool) Name() string { return "list_changed_files" }
func (t *ApplyPatchTool) Name() string       { return "apply_patch" }

func (t *GitStatusTool) SafetyLevel() SafetyLevel        { return SafetyLevelSafe }
func (t *GitDiffTool) SafetyLevel() SafetyLevel          { return SafetyLevelSafe }
func (t *GitLogTool) SafetyLevel() SafetyLevel           { return SafetyLevelSafe }
func (t *GitShowTool) SafetyLevel() SafetyLevel          { return SafetyLevelSafe }
func (t *ListChangedFilesTool) SafetyLevel() SafetyLevel { return SafetyLevelSafe }
func (t *ApplyPatchTool) SafetyLevel() SafetyLevel       { return SafetyLevelPrompt }

func (t *GitStatusTool) Description() string {
	return "Show concise git status for a repository under a configured workspace root"
}
func (t *GitDiffTool) Description() string {
	return "Show git diff for a repository under a configured workspace root"
}
func (t *GitLogTool) Description() string {
	return "Show recent git commits for a repository under a configured workspace root"
}
func (t *GitShowTool) Description() string {
	return "Show a git object, commit, or file revision under a configured workspace root"
}
func (t *ListChangedFilesTool) Description() string {
	return "List changed files from git status porcelain output"
}
func (t *ApplyPatchTool) Description() string {
	return "Apply a unified diff patch under a configured workspace root after validating affected paths"
}

func cwdParam() Parameter {
	return Parameter{Name: "cwd", Type: "string", Description: "Repository directory under a configured workspace root", Required: true}
}

func (t *GitStatusTool) Parameters() []Parameter {
	return []Parameter{cwdParam()}
}
func (t *GitDiffTool) Parameters() []Parameter {
	return []Parameter{
		cwdParam(),
		{Name: "staged", Type: "boolean", Description: "Show staged diff with --cached", Required: false},
		{Name: "path", Type: "string", Description: "Optional path under the repository to diff", Required: false},
	}
}
func (t *GitLogTool) Parameters() []Parameter {
	return []Parameter{
		cwdParam(),
		{Name: "max_count", Type: "number", Description: "Maximum commits to return, defaults to 20", Required: false},
	}
}
func (t *GitShowTool) Parameters() []Parameter {
	return []Parameter{
		cwdParam(),
		{Name: "rev", Type: "string", Description: "Revision or object to show, defaults to HEAD", Required: false},
	}
}
func (t *ListChangedFilesTool) Parameters() []Parameter {
	return []Parameter{cwdParam()}
}
func (t *ApplyPatchTool) Parameters() []Parameter {
	return []Parameter{
		cwdParam(),
		{Name: "patch", Type: "string", Description: "Unified diff patch to apply", Required: true},
	}
}

func (t *GitStatusTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	return runGitTool(ctx, t.limits, params, []string{"status", "--short", "--branch"})
}

func (t *GitDiffTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	args := []string{"diff"}
	if staged, _ := params["staged"].(bool); staged {
		args = append(args, "--cached")
	}
	if path, _ := params["path"].(string); strings.TrimSpace(path) != "" {
		if strings.Contains(path, "\x00") || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
			return "", fmt.Errorf("path must be a relative repository path")
		}
		args = append(args, "--", path)
	}
	return runGitTool(ctx, t.limits, params, args)
}

func (t *GitLogTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	maxCount := intParam(params, "max_count", 20, 1, 100)
	return runGitTool(ctx, t.limits, params, []string{"log", "--oneline", "--decorate", fmt.Sprintf("-%d", maxCount)})
}

func (t *GitShowTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	rev, _ := params["rev"].(string)
	rev = strings.TrimSpace(rev)
	if rev == "" {
		rev = "HEAD"
	}
	if strings.ContainsAny(rev, "\x00\r\n") || strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("rev cannot contain control characters or start with -")
	}
	return runGitTool(ctx, t.limits, params, []string{"show", "--stat", "--patch", rev})
}

func (t *ListChangedFilesTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	return runGitTool(ctx, t.limits, params, []string{"status", "--porcelain"})
}

func (t *ApplyPatchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	cwd, err := resolveToolCWD(t.limits, params)
	if err != nil {
		return "", err
	}
	patch, _ := params["patch"].(string)
	if strings.TrimSpace(patch) == "" {
		return "", fmt.Errorf("patch parameter is required")
	}
	if err := validatePatchPaths(t.limits, cwd, patch); err != nil {
		return "", err
	}
	if _, err := runGit(ctx, t.limits, cwd, []string{"apply", "--check"}, patch); err != nil {
		return "", err
	}
	out, err := runGit(ctx, t.limits, cwd, []string{"apply"}, patch)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		out = "Patch applied."
	}
	return out, nil
}

func runGitTool(ctx context.Context, limits Limits, params map[string]any, args []string) (string, error) {
	cwd, err := resolveToolCWD(limits, params)
	if err != nil {
		return "", err
	}
	return runGit(ctx, limits, cwd, args, "")
}

func resolveToolCWD(limits Limits, params map[string]any) (string, error) {
	if err := limits.RequireRoots(); err != nil {
		return "", err
	}
	cwdParam, _ := params["cwd"].(string)
	return limits.ResolveAllowed(cwdParam)
}

func runGit(ctx context.Context, limits Limits, cwd string, args []string, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("%s", strings.TrimSpace(text))
	}
	if text == "" {
		text = fmt.Sprintf("$ git %s\n", strings.Join(args, " "))
	}
	return limits.Truncate(text), nil
}

func validatePatchPaths(limits Limits, cwd, patch string) error {
	paths := patchPaths(patch)
	if len(paths) == 0 {
		return fmt.Errorf("patch does not contain file paths")
	}
	for _, p := range paths {
		if strings.Contains(p, "\x00") || filepath.IsAbs(p) || strings.HasPrefix(filepath.Clean(p), "..") {
			return fmt.Errorf("patch path %q is not allowed", p)
		}
		if _, err := limits.ResolveCreateAllowed(filepath.Join(cwd, p)); err != nil {
			return err
		}
	}
	return nil
}

func patchPaths(patch string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- ") {
			p := strings.TrimSpace(line[4:])
			if p == "/dev/null" {
				continue
			}
			if i := strings.IndexAny(p, "\t "); i >= 0 {
				p = p[:i]
			}
			p = strings.TrimPrefix(p, "a/")
			p = strings.TrimPrefix(p, "b/")
			if p != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths
}
