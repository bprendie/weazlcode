package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/project"
)

func (m model) buildWorkerPacket(task coding.Task) (coding.TaskPacket, error) {
	diagnostics := m.codingDiagnostics()
	allowedTools := []string{"apply_patch"}
	skills, err := m.skillContexts(task.Skills)
	if err != nil {
		return coding.TaskPacket{}, err
	}
	maxFileChars := workerContextFileCharBudget(m.cfg.ProviderForRole("worker").ContextWindow)
	task.ContextFiles = m.workerContextFiles(task, maxFileChars)
	packet, err := coding.BuildTaskPacket(task, coding.ContextPackOptions{
		ProjectRoot:  m.project.Root,
		MaxFileChars: maxFileChars,
		DefaultAllowed: []string{
			".",
		},
		DefaultTools:  allowedTools,
		DefaultVerify: m.defaultVerificationCommands(),
		Diagnostics:   diagnostics,
		Skills:        skills,
	})
	if err != nil {
		return coding.TaskPacket{}, err
	}
	profile := m.workerCapacityProfile()
	artifactInstruction := workerArtifactInstruction(task)
	packet.WorkerProfile = profile.Label + ": " + strings.TrimSpace(strings.Join(nonEmptyWorkerInstructions(profile.Instruction, artifactInstruction), "\n"))
	packet.ContextPolicy.Mode = "provided"
	packet.ContextPolicy.RequestTools = nil
	packet.ContextPolicy.Instruction = "Use the provided context_files as the source of truth for existing files. If an allowed path is missing from context_files, treat it as a create-only target or return blocker instead of inventing existing content."
	if artifactInstruction != "" {
		packet.ContextPolicy.Instruction += " " + artifactInstruction
	}
	return packet, nil
}

func nonEmptyWorkerInstructions(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func workerArtifactInstruction(task coding.Task) string {
	paths := make([]string, 0, len(task.AllowedPaths))
	for _, path := range task.AllowedPaths {
		path = normalizeTaskPathForOverlap(path)
		if path != "" && path != "." {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return ""
	}
	var hasIndexHTML, hasFragmentHTML, hasCSS bool
	for _, path := range paths {
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".html") {
			if lower == "index.html" || strings.HasSuffix(lower, "/index.html") {
				hasIndexHTML = true
			} else {
				hasFragmentHTML = true
			}
		}
		if strings.HasSuffix(lower, ".css") {
			hasCSS = true
		}
	}
	var instructions []string
	if cohesiveWholeFileArtifactTask(task) {
		instructions = append(instructions, "Artifact contract: cohesive whole-file artifact task. Own the complete deliverable across the allowed files in one pass. Prefer files[] with complete content for each allowed output file so the result is coherent. Do not split the work into imaginary subtasks, placeholders, TODOs, or shortened copy.")
	}
	if generatedCodeArtifactTask(task) {
		instructions = append(instructions, "Maintainability contract: keep generated code modular and readable. Prefer files around 300 lines or less. If this task owns multiple allowed files, put domain logic, UI/rendering, persistence/adapters, entrypoint, and smoke/verification code in separate focused modules instead of one giant file.")
		instructions = append(instructions, "Module contract: use explicit imports between generated modules. Do not use wildcard imports such as from module import *; static validation and later workers need visible names and stable interfaces.")
	}
	if interactivePythonArtifactTask(task) {
		instructions = append(instructions, "Smoke contract: generated interactive Python entrypoints/final wiring tasks must include a non-interactive --smoke path. The smoke path should initialize the app, exercise one lightweight update/render or health check, then exit before entering the interactive loop. Leaf modules should stay importable and should not add their own command-line entrypoint unless explicitly requested.")
		instructions = append(instructions, "Entrypoint contract: structure interactive Python entrypoints as small functions: smoke_test(), main(), and if __name__ == \"__main__\" dispatch. Keep the event loop state inside main(). Do not reference event-loop variables such as event, font, overlay, or game_over_surface outside the branch or loop where they are created; create render surfaces/fonts in the same block that uses them or initialize them before the loop.")
	}
	if singleFileGeneratedArtifactTask(task) {
		instructions = append(instructions, "Artifact contract: single-file generated artifact. You must return files[] with complete content for the allowed file and leave patch empty, especially during repair, because full-file replacement is safer than fragile unified diffs for standalone generated outputs. Keep the file compact and well-structured; if it is likely to exceed about 300 lines, return a blocker explaining that the task should be split unless the task explicitly requires one file.")
	}
	if hasFragmentHTML && !hasIndexHTML {
		instructions = append(instructions, "Artifact contract: this task writes an HTML fragment/module only. Do not include <!doctype>, <html>, <head>, or <body>; return only the requested section/card/component markup.")
	}
	if hasIndexHTML {
		instructions = append(instructions, "Artifact contract: index.html is the final assembled page and must be a complete browser-openable HTML document with doctype, html, head, body, linked CSS, and asset references.")
	}
	if hasCSS {
		instructions = append(instructions, "Artifact contract: CSS output must be plain browser CSS only. Do not use preprocessor-style nested rule blocks, Sass/Less/PostCSS-only syntax, or CSS inside HTML style tags. Normal descendant selectors, pseudo-classes, and pseudo-elements are allowed.")
	}
	return strings.Join(instructions, " ")
}

func interactivePythonArtifactTask(task coding.Task) bool {
	hasEntrypointPath := false
	for _, path := range normalizedArtifactTaskPaths(task.AllowedPaths) {
		lower := strings.ToLower(path)
		if lower == "main.py" || strings.HasSuffix(lower, "/main.py") || lower == "app.py" || strings.HasSuffix(lower, "/app.py") {
			hasEntrypointPath = true
		}
	}
	text := strings.ToLower(task.Title + " " + task.Goal)
	for _, check := range task.AcceptanceChecks {
		if strings.Contains(strings.ToLower(check.Description+" "+check.Command), "--smoke") {
			return true
		}
	}
	if !hasEntrypointPath {
		return false
	}
	for _, marker := range []string{"entrypoint", "main", "smoke", "game loop", "interactive", "pygame"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func singleFileGeneratedArtifactTask(task coding.Task) bool {
	paths := normalizedArtifactTaskPaths(task.AllowedPaths)
	if len(paths) != 1 {
		return false
	}
	lowerPath := strings.ToLower(paths[0])
	switch {
	case strings.HasSuffix(lowerPath, ".py"),
		strings.HasSuffix(lowerPath, ".html"),
		strings.HasSuffix(lowerPath, ".htm"),
		strings.HasSuffix(lowerPath, ".css"),
		strings.HasSuffix(lowerPath, ".md"),
		strings.HasSuffix(lowerPath, ".js"),
		strings.HasSuffix(lowerPath, ".ts"):
	default:
		return false
	}
	goal := strings.ToLower(task.Title + " " + task.Goal)
	for _, marker := range []string{"create", "build", "generate", "standalone", "game", "script", "tool", "artifact"} {
		if strings.Contains(goal, marker) {
			return true
		}
	}
	return false
}

func generatedCodeArtifactTask(task coding.Task) bool {
	for _, path := range normalizedArtifactTaskPaths(task.AllowedPaths) {
		lowerPath := strings.ToLower(path)
		switch {
		case strings.HasSuffix(lowerPath, ".py"),
			strings.HasSuffix(lowerPath, ".js"),
			strings.HasSuffix(lowerPath, ".ts"),
			strings.HasSuffix(lowerPath, ".tsx"),
			strings.HasSuffix(lowerPath, ".jsx"),
			strings.HasSuffix(lowerPath, ".go"),
			strings.HasSuffix(lowerPath, ".rs"),
			strings.HasSuffix(lowerPath, ".java"),
			strings.HasSuffix(lowerPath, ".cs"),
			strings.HasSuffix(lowerPath, ".rb"),
			strings.HasSuffix(lowerPath, ".php"),
			strings.HasSuffix(lowerPath, ".swift"),
			strings.HasSuffix(lowerPath, ".kt"),
			strings.HasSuffix(lowerPath, ".kts"):
			return true
		}
	}
	return false
}

func cohesiveWholeFileArtifactTask(task coding.Task) bool {
	paths := normalizedArtifactTaskPaths(task.AllowedPaths)
	if len(paths) == 0 {
		return false
	}
	fullPage := false
	artifactFiles := 0
	for _, path := range paths {
		lower := strings.ToLower(path)
		switch {
		case lower == "index.html" || strings.HasSuffix(lower, "/index.html"):
			fullPage = true
			artifactFiles++
		case strings.HasSuffix(lower, ".html"), strings.HasSuffix(lower, ".htm"), strings.HasSuffix(lower, ".css"), strings.HasSuffix(lower, ".md"):
			artifactFiles++
		}
	}
	if fullPage {
		return true
	}
	goal := strings.ToLower(task.Title + " " + task.Goal)
	return artifactFiles >= 2 && (strings.Contains(goal, "artifact") || strings.Contains(goal, "deliverable") || strings.Contains(goal, "landing page") || strings.Contains(goal, "website") || strings.Contains(goal, "static site"))
}

func normalizedArtifactTaskPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = normalizeTaskPathForOverlap(path)
		if path == "" || path == "." {
			continue
		}
		out = append(out, path)
	}
	return out
}

func (m model) workerContextFiles(task coding.Task, maxFileChars int) []string {
	out := make([]string, 0, len(task.ContextFiles)+len(task.AllowedPaths))
	seen := map[string]bool{}
	seenPath := map[string]bool{}
	add := func(spec string) {
		key := strings.TrimSpace(spec)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		if path := contextSpecPath(key); path != "" {
			seenPath[path] = true
		}
		out = append(out, key)
	}
	for _, spec := range task.ContextFiles {
		add(spec)
	}
	for _, path := range task.AllowedPaths {
		spec, ok := m.allowedPathContextSpec(path, maxFileChars)
		if ok && !seenPath[spec] {
			add(spec)
		}
	}
	for _, spec := range m.dependencyContextFiles(task, maxFileChars) {
		path := contextSpecPath(spec)
		if path != "" && !seenPath[path] {
			add(spec)
		}
	}
	return out
}

func (m model) dependencyContextFiles(task coding.Task, maxFileChars int) []string {
	if m.store == nil || len(task.DependsOn) == 0 {
		return nil
	}
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		return nil
	}
	byID := map[string]coding.Task{}
	for _, candidate := range plan.Tasks {
		byID[candidate.ID] = candidate
	}
	out := []string{}
	seen := map[string]bool{}
	for _, depID := range task.DependsOn {
		dep, ok := byID[strings.TrimSpace(depID)]
		if !ok {
			continue
		}
		if dep.Status != coding.TaskStatusDone && dep.Status != coding.TaskStatusReviewing {
			continue
		}
		for _, path := range dep.AllowedPaths {
			spec, ok := m.allowedPathContextSpec(path, maxFileChars)
			if !ok || seen[spec] {
				continue
			}
			seen[spec] = true
			out = append(out, spec)
		}
	}
	return out
}

func contextSpecPath(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	if hash := strings.LastIndex(spec, "#L"); hash >= 0 {
		spec = spec[:hash]
	}
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(spec)))
	if clean == "." || clean == "" {
		return ""
	}
	return clean
}

func (m model) allowedPathContextSpec(rawPath string, maxFileChars int) (string, bool) {
	path := strings.TrimSpace(filepath.ToSlash(rawPath))
	if path == "" || path == "." || strings.Contains(path, "\x00") {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", false
	}
	fullPath := filepath.Join(m.project.Root, filepath.FromSlash(clean))
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return "", false
	}
	if maxFileChars > 0 && info.Size() > int64(maxFileChars) {
		return "", false
	}
	if !looksTextFile(fullPath) {
		return "", false
	}
	return clean, true
}

func looksTextFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

func workerContextFileCharBudget(contextWindow int) int {
	switch {
	case contextWindow >= 32768:
		return 48000
	case contextWindow >= 16384:
		return 24000
	default:
		return 12000
	}
}

func (m model) workerOutputTokens(packet coding.TaskPacket) int {
	normal := m.cfg.Workers.OutputTokens
	if normal <= 0 {
		normal = 4096
	}
	if !cohesiveWholeFileArtifactPacket(packet) {
		return normal
	}
	artifact := m.cfg.Workers.ArtifactOutputTokens
	if artifact <= 0 {
		artifact = 24576
	}
	contextWindow := m.cfg.ProviderForRole("worker").ContextWindow
	if contextWindow > 0 {
		// Leave room for the task packet and model bookkeeping.
		maxReasonable := contextWindow - 4096
		if maxReasonable < normal {
			maxReasonable = normal
		}
		if artifact > maxReasonable {
			artifact = maxReasonable
		}
	}
	if artifact < normal {
		return normal
	}
	return artifact
}

func cohesiveWholeFileArtifactPacket(packet coding.TaskPacket) bool {
	task := coding.Task{
		Title:        packet.TaskID,
		Goal:         packet.Goal + " " + packet.WorkerProfile + " " + packet.ContextPolicy.Instruction,
		AllowedPaths: packet.AllowedPaths,
	}
	return cohesiveWholeFileArtifactTask(task)
}

func (m model) skillContexts(names []string) ([]coding.SkillContext, error) {
	if !m.cfg.Skills.SkillsEnabled() || len(names) == 0 {
		return nil, nil
	}
	skills, err := project.LoadSkillContents(m.project.Root, m.cfg.Skills.Paths, names, 12000)
	if err != nil {
		return nil, err
	}
	out := make([]coding.SkillContext, 0, len(skills))
	for _, skill := range skills {
		out = append(out, coding.SkillContext{
			Name:        skill.Name,
			Description: skill.Description,
			Path:        skill.Path,
			Content:     skill.Content,
			Truncated:   skill.Truncated,
		})
	}
	return out, nil
}

func (m model) codingDiagnostics() []coding.Diagnostic {
	diagnostics, err := m.lspManager().Diagnostics(context.Background())
	if err != nil || len(diagnostics) == 0 {
		return nil
	}
	out := make([]coding.Diagnostic, 0, min(len(diagnostics), 25))
	for i, diagnostic := range diagnostics {
		if i >= 25 {
			break
		}
		out = append(out, coding.Diagnostic{
			File:     diagnostic.File,
			Line:     diagnostic.Line,
			Column:   diagnostic.Column,
			Severity: diagnostic.Severity,
			Message:  diagnostic.Message,
			Source:   diagnostic.Source,
		})
	}
	return out
}

func (m model) buildWorkerPacketForRun(task coding.Task) (coding.TaskPacket, error) {
	packet, err := m.buildWorkerPacket(task)
	if err != nil {
		return coding.TaskPacket{}, err
	}
	if task.Status != coding.TaskStatusBlocked {
		return packet, nil
	}
	events, err := m.store.TaskEvents(task.ID)
	if err != nil {
		return coding.TaskPacket{}, err
	}
	repair, ok := latestRepairRequest(events)
	if !ok {
		if validation, ok := latestArtifactValidation(events); ok {
			if singleFileGeneratedArtifactTask(task) {
				packet.Goal = strings.TrimSpace(packet.Goal + "\n\nArtifact validation repair focus:\nThe previous worker output was applied to the current file, but deterministic artifact validation failed. Return files[] with complete replacement content for the single allowed file and leave patch empty. If the current file is syntactically broken, truncated, or internally inconsistent, ignore the broken current content and reconstruct a clean complete file from the task requirements and available dependency context. Do not try fragile line edits against the broken output. Fix exactly these validator issues:\n" + validation)
			} else {
				packet.Goal = strings.TrimSpace(packet.Goal + "\n\nArtifact validation repair focus:\nThe previous worker output was applied to the current files, but deterministic artifact validation failed. Repair the current generated files in place. Fix exactly these validator issues, preserve correct existing content, and do not regenerate unrelated sections:\n" + validation)
			}
			packet.AcceptanceChecks = append(packet.AcceptanceChecks, artifactValidationRepairCheck())
			return packet, nil
		}
		if workerErr, retry := latestWorkerError(events); retry {
			packet.Goal = strings.TrimSpace(packet.Goal + "\n\nRetry note:\nPrevious worker attempt failed before producing a patch: " + workerErr)
			return packet, nil
		}
		return packet, nil
	}
	if singleFileGeneratedArtifactTask(task) && artifactValidationFailureCount(events) > 0 {
		validation := ""
		if latest, ok := latestArtifactValidation(events); ok {
			validation = "\n\nLatest artifact validation failure:\n" + latest
		}
		packet.Goal = strings.TrimSpace(packet.Goal + "\n\nRepair focus:\nThe previous single-file artifact remains in the workspace, but deterministic validation has failed. Return files[] with complete replacement content for the single allowed file and leave patch empty. If the current file is syntactically broken, truncated, or internally inconsistent, ignore the broken current content and reconstruct a clean complete file from the task requirements and available dependency context. Do not try fragile line edits against the broken output. Address the reviewer issues and validation failure below:\n" + repair + validation)
	} else {
		packet.Goal = strings.TrimSpace(packet.Goal + "\n\nRepair focus:\nThe previous worker output remains applied in the current workspace and is included in context_files when it fits the context budget. Treat this as a surgical repair pass: fix the reviewer issues below, preserve unrelated working code and content, and do not restart from scratch unless the reviewer explicitly asks for a full rewrite. If the repair mentions a failing --smoke path or other deterministic validation failure, make the minimal main-guard/control-flow change needed for that check before touching lower-priority gameplay polish.\n" + repair)
	}
	packet.AcceptanceChecks = append(packet.AcceptanceChecks, coding.AcceptanceCheck{Description: "Reviewer needs_fix issues are addressed without broadening the task scope."})
	return packet, nil
}

func renderJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("JSON render error: %v", err)
	}
	return string(b)
}
