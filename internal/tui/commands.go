package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
)

const maxRepairAttempts = 2
const maxPlanGenerateTokens = 8192
const maxTaskBaselineBytes = 2 * 1024 * 1024

type taskBaselinePayload struct {
	Files     []taskBaselineFile `json:"files"`
	Truncated bool               `json:"truncated,omitempty"`
}

type taskBaselineFile struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Content string `json:"content,omitempty"`
}

func (m model) handleSlashCommand(input string) (tea.Model, tea.Cmd, bool) {
	if !strings.HasPrefix(strings.TrimSpace(input), "/") {
		return m, nil, false
	}
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return m, nil, false
	}
	name := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	m.input.Reset()
	m.pasteText = ""
	m.pasteLines = 0
	m.historyIdx = 0
	m.historyDraft = ""
	m.err = ""

	switch name {
	case "", "help", "?":
		m.setIDEView("help", slashHelp())
	case "commands", "palette":
		m.setIDEView("commands", commandPaletteText())
	case "cancel":
		return m.cancelModelCommand(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "project":
		m.setIDEView("project", m.projectCommandText())
	case "models":
		m.setIDEView("models", m.modelRolesText())
	case "tools":
		m.setIDEView("tools", m.toolsViewText())
	case "skills":
		m.setIDEView("skills", m.skillsViewText())
	case "config":
		m.setIDEView("config", m.configViewText())
	case "diff":
		m.setIDEView("diff", m.diffCommandText())
	case "outputs", "logs":
		m.setIDEView("outputs", m.outputsCommandText())
	case "files":
		m.setIDEView("files", m.filesCommandText(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0]))))
	case "preview":
		m.setIDEView("preview", m.previewCommandText(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0]))))
	case "edit", "editor":
		return m.openExternalEditorCommand(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "debug":
		return m.handleDebugCommand(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "attach":
		return m.attachFileCommand(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "lsp":
		m.setIDEView("lsp", m.lspCommandText())
	case "diagnostics":
		m.setIDEView("diagnostics", m.diagnosticsCommandText())
	case "symbols":
		m.setIDEView("symbols", m.symbolsCommandText(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0]))))
	case "definition":
		m.setIDEView("definition", m.definitionCommandText(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0]))))
	case "references":
		m.setIDEView("references", m.referencesCommandText(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0]))))
	case "instructions":
		m.setIDEView("instructions", m.instructionsCommandText())
	case "memory":
		return m.handleProjectMemoryCommand(fields[1:], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "final-review":
		m.setIDEView("final-review", m.finalReviewCommandText())
	case "commit-message":
		m.setIDEView("commit-message", m.commitMessageCommandText())
	case "commit":
		return m.commitCommand(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "export-run":
		m.setIDEView("export-run", m.exportRunCommandText())
	case "chat":
		m.mode = modeChat
		m.status = "chat"
		m.renderMessages()
	case "plan":
		return m.handlePlanCommand(fields[1:], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "tasks":
		m.setIDEView("tasks", m.tasksCommandText())
	case "task":
		m.setIDEView("task", m.taskDetailCommandText(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0]))))
	case "packet":
		m.setIDEView("packet", m.packetCommandText())
	case "approve":
		return m.approveLatestPlan()
	case "reject":
		return m.rejectLatestPlan(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "run-task":
		return m.runNextTask()
	case "run-worker":
		return m.runWorkerModel()
	case "run-workers":
		return m.runParallelWorkers()
	case "worker-patch":
		return m.importWorkerPatch(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "reviewer-input":
		m.setIDEView("reviewer-input", m.reviewerInputCommandText())
	case "run-reviewer":
		return m.runReviewerModel()
	case "review-diff":
		m.setIDEView("review-diff", m.reviewDiffCommandText())
	case "review":
		return m.importReviewVerdict(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "sessions":
		return m.showSessionsWithInputCleared()
	case "workspaces", "workspace":
		return m.showWorkspacesWithInputCleared()
	case "new":
		updated, cmd := m.newSession()
		return updated, cmd, true
	case "clear":
		updated, cmd := m.startClearContextConfirm()
		return updated, cmd, true
	case "trim":
		updated, cmd := m.trimContext(false, "", 0, 0)
		return updated, cmd, true
	case "copy":
		if m.mouseScroll {
			updated, cmd := m.toggleMouseMode()
			return updated, cmd, true
		}
		m.status = "copy mode already enabled"
	case "mouse":
		if !m.mouseScroll {
			updated, cmd := m.toggleMouseMode()
			return updated, cmd, true
		}
		m.status = "mouse scroll already enabled"
	default:
		m.addSystemNote(fmt.Sprintf("Unknown slash command: /%s\n\n%s", name, slashHelp()))
		m.status = "unknown slash command"
	}
	return m, nil, true
}

func (m *model) setIDEView(name, text string) {
	m.mode = modeIDEView
	m.status = "view " + name
	m.viewport.SetContent(m.styles.system.Render(text))
	m.viewport.GotoTop()
	m.input.Focus()
}

func (m model) showSessionsWithInputCleared() (tea.Model, tea.Cmd, bool) {
	updated, cmd := m.showSessions()
	return updated, cmd, true
}

func (m model) showWorkspacesWithInputCleared() (tea.Model, tea.Cmd, bool) {
	updated, cmd := m.showWorkspaces()
	return updated, cmd, true
}

func (m *model) addSystemNote(text string) {
	note := "system\n" + text
	if strings.TrimSpace(m.viewport.View()) == "" && len(m.messages) == 0 && m.streamText == "" {
		m.viewport.SetContent(m.styles.system.Render(note))
		m.viewport.GotoBottom()
		return
	}
	content := m.renderTranscript(m.messages)
	if strings.TrimSpace(content) != "" {
		content += "\n"
	}
	content += m.styles.system.Render(note) + "\n\n"
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m model) cancelModelCommand(raw string) (tea.Model, tea.Cmd, bool) {
	target := strings.TrimSpace(raw)
	if target != "" && target != "all" && target != "workers" {
		cancelled := 0
		for runID, taskID := range m.workerRuns {
			if target == taskID {
				if cancel := m.cancelWorkerRuns[runID]; cancel != nil {
					cancel()
				}
				delete(m.workerRuns, runID)
				delete(m.cancelWorkerRuns, runID)
				cancelled++
			}
		}
		if cancelled == 0 {
			m.status = "worker not found"
			return m, nil, true
		}
		m.thinking = m.hasModelWork()
		m.addSystemNote(fmt.Sprintf("Cancelled worker task %s.", target))
		m.status = "worker cancelled"
		return m, nil, true
	}
	if (m.cancelModel == nil || m.activeModelRunID == 0) && len(m.cancelWorkerRuns) == 0 {
		m.status = "nothing to cancel"
		return m, nil, true
	}
	if target != "workers" && m.cancelModel != nil {
		m.cancelModel()
	}
	m.cancelModel = nil
	m.activeModelRunID = 0
	for runID, cancel := range m.cancelWorkerRuns {
		if cancel != nil {
			cancel()
		}
		delete(m.workerRuns, runID)
		delete(m.cancelWorkerRuns, runID)
	}
	m.thinking = false
	m.addSystemNote("Cancelled active model request.")
	m.status = "model cancelled"
	return m, nil, true
}

func slashHelp() string {
	return strings.Join([]string{
		"Slash commands:",
		"/help - show commands",
		"/commands - show grouped command palette",
		"/cancel [all|workers|task_id] - cancel active model requests",
		"/project - show active project",
		"/models - show model role mapping",
		"/tools - list enabled tools",
		"/skills - list discovered skills",
		"/config - show current local configuration summary",
		"/diff - show current git diff",
		"/outputs - show recent task events and tool outputs",
		"/files [query] - fuzzy-find project files",
		"/preview <path> - preview a project file",
		"/edit <path> [line] - open a project file in the configured external editor",
		"/debug - show configured debug adapters and launch configurations",
		"/debug launch <name> - run a debug-adapter launch handshake",
		"/attach [task] <path> [start-end] - attach a file or line range to a draft task",
		"/lsp - show detected language servers",
		"/diagnostics - show project diagnostics",
		"/symbols [query] - search project symbols",
		"/definition <symbol> - show symbol definition",
		"/references <symbol> - show symbol references",
		"/instructions - show project instructions and discovered commands",
		"/memory [key=value] - list or save project memory",
		"/final-review - summarize latest reviewed task and current diff",
		"/commit-message - generate a commit message from the final review",
		"/commit yes - git add and commit with generated message",
		"/export-run - write latest task review artifact under .weazlcode/runs",
		"/chat - return to chat transcript",
		"/plan - show latest plan",
		"/plan draft <title> - create a draft plan with one seed task",
		"/plan generate <request> - ask orchestrator role for a strict draft plan",
		"/plan replan [guidance] - ask orchestrator role to replace blocked/pending work with a fresh draft plan",
		"/plan edit <task> <field> <value> - edit draft task fields, including skills, before approval",
		"/plan validate - check draft task specificity before approval",
		"/plan import <json> - validate and store a structured plan JSON payload",
		"/tasks - list latest plan tasks",
		"/task [n|id] - show task detail, packet, events, and review state",
		"/packet - show local-worker packet for the first pending task",
		"/approve - approve the latest draft plan",
		"/reject [reason] - block the latest plan",
		"/run-task - mark first pending task running and show its worker packet",
		"/run-worker - ask configured worker role for a WorkerPatch JSON",
		"/run-workers - dispatch independent pending tasks up to worker concurrency",
		"/worker-patch <json> - import a worker patch or blocker for the running task",
		"/reviewer-input - show frontier-review payload for the reviewing task",
		"/run-reviewer - ask configured reviewer role for a verdict",
		"/review-diff - inspect changed files and diff before review",
		"/review <json|approve|needs-fix|blocked> - import or enter a reviewer verdict",
		"/sessions - open sessions",
		"/workspaces - open workspace saves",
		"/new - start a new session",
		"/clear - clear current session context",
		"/trim - compact context",
		"/copy - release mouse for terminal selection",
		"/mouse - restore mouse scrolling",
	}, "\n")
}

func commandPaletteText() string {
	groups := []struct {
		Title    string
		Commands []string
	}{
		{"Plan", []string{"/plan", "/plan generate <request>", "/plan replan [guidance]", "/plan edit <task> <field> <value>", "/plan validate", "/tasks", "/task [n|id]", "/approve", "/reject [reason]"}},
		{"Worker", []string{"/packet", "/run-task", "/run-worker", "/run-workers", "/worker-patch <json>"}},
		{"Review", []string{"/review-diff", "/reviewer-input", "/run-reviewer", "/review approve [summary]", "/review needs-fix <issue>[;; issue]", "/final-review", "/export-run"}},
		{"Project", []string{"/project", "/files [query]", "/preview <path>", "/edit <path> [line]", "/debug", "/debug launch <name>", "/attach [task] <path> [start-end]", "/instructions", "/memory [key=value]", "/diagnostics", "/symbols [query]"}},
		{"Skills", []string{"/skills"}},
		{"Git", []string{"/diff", "/commit-message", "/commit yes"}},
		{"Session", []string{"/chat", "/cancel", "/sessions", "/workspaces", "/new", "/clear", "/trim", "/copy"}},
	}
	var b strings.Builder
	b.WriteString("Command palette:\n")
	for _, group := range groups {
		fmt.Fprintf(&b, "\n%s:\n", group.Title)
		for _, command := range group.Commands {
			fmt.Fprintf(&b, "- %s\n", command)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) gitDiff() (string, error) {
	tool, ok := m.toolRegistry.Get("git_diff")
	if !ok {
		return "", fmt.Errorf("git_diff tool is not registered")
	}
	return tool.Execute(context.Background(), map[string]any{"cwd": m.project.Root})
}

func (m model) taskGitDiff(task coding.Task) (string, error) {
	allowed := taskAllowedPaths(task)
	if len(allowed) == 0 {
		return "", fmt.Errorf("task has no allowed paths")
	}
	tool, ok := m.toolRegistry.Get("git_diff")
	if !ok {
		return "", fmt.Errorf("git_diff tool is not registered")
	}
	var parts []string
	for _, path := range allowed {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		diff, err := tool.Execute(context.Background(), map[string]any{"cwd": m.project.Root, "path": path})
		if err != nil {
			return "", err
		}
		diff = strings.TrimSpace(diff)
		if diff != "" && !gitDiffOutputEmpty(diff) {
			parts = append(parts, diff)
			continue
		}
		untracked, err := m.untrackedFileDiff(path)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(untracked) != "" {
			parts = append(parts, untracked)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func (m model) recordTaskBaseline(task coding.Task) error {
	baseline, err := m.captureTaskBaseline(task)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(baseline)
	if err != nil {
		return err
	}
	message := fmt.Sprintf("Captured baseline for %d allowed file(s).", len(baseline.Files))
	if baseline.Truncated {
		message += " Baseline was truncated."
	}
	_, err = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "task_baseline",
		Message: message,
		Payload: payload,
	})
	return err
}

func (m model) captureTaskBaseline(task coding.Task) (taskBaselinePayload, error) {
	var baseline taskBaselinePayload
	total := 0
	seen := map[string]bool{}
	for _, rawPath := range taskAllowedPaths(task) {
		path := strings.TrimSpace(filepath.ToSlash(rawPath))
		if path == "" || path == "." || strings.Contains(path, "\x00") || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") || seen[path] {
			continue
		}
		seen[path] = true
		fullPath := filepath.Join(m.project.Root, filepath.FromSlash(path))
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				baseline.Files = append(baseline.Files, taskBaselineFile{Path: path, Exists: false})
				continue
			}
			return baseline, err
		}
		if info.IsDir() {
			continue
		}
		if total+int(info.Size()) > maxTaskBaselineBytes {
			baseline.Truncated = true
			continue
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return baseline, err
		}
		total += len(content)
		baseline.Files = append(baseline.Files, taskBaselineFile{
			Path:    path,
			Exists:  true,
			Content: string(content),
		})
	}
	return baseline, nil
}

func (m model) taskReviewDiff(task coding.Task, events []coding.TaskEvent) (string, error) {
	if baseline, ok := latestTaskBaseline(events); ok && len(baseline.Files) > 0 {
		diff, err := m.taskBaselineDiff(task, baseline)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(diff) != "" {
			return diff, nil
		}
	}
	return m.taskGitDiff(task)
}

func latestTaskBaseline(events []coding.TaskEvent) (taskBaselinePayload, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "task_baseline" || len(events[i].Payload) == 0 {
			continue
		}
		var baseline taskBaselinePayload
		if err := json.Unmarshal(events[i].Payload, &baseline); err != nil {
			continue
		}
		return baseline, true
	}
	return taskBaselinePayload{}, false
}

func (m model) taskBaselineDiff(task coding.Task, baseline taskBaselinePayload) (string, error) {
	var parts []string
	for _, file := range baseline.Files {
		if err := coding.ValidatePatchPaths([]string{file.Path}, taskAllowedPaths(task), task.ForbiddenPaths); err != nil {
			return "", err
		}
		current, exists, err := m.readTaskFile(file.Path)
		if err != nil {
			return "", err
		}
		if file.Exists == exists && file.Content == current {
			continue
		}
		parts = append(parts, renderFullFileDiff(file.Path, file.Exists, file.Content, exists, current))
	}
	return strings.Join(parts, "\n\n"), nil
}

func (m model) restoreTaskBaseline(task coding.Task, events []coding.TaskEvent, reason string) error {
	baseline, ok := latestTaskBaseline(events)
	if !ok || len(baseline.Files) == 0 {
		return nil
	}
	var restored []string
	for _, file := range baseline.Files {
		if err := coding.ValidatePatchPaths([]string{file.Path}, taskAllowedPaths(task), task.ForbiddenPaths); err != nil {
			return err
		}
		fullPath := filepath.Join(m.project.Root, filepath.FromSlash(file.Path))
		if file.Exists {
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(fullPath, []byte(file.Content), 0o644); err != nil {
				return err
			}
		} else if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		restored = append(restored, file.Path)
	}
	payload, _ := json.Marshal(struct {
		Reason string   `json:"reason"`
		Paths  []string `json:"paths"`
	}{
		Reason: reason,
		Paths:  restored,
	})
	_, err := m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "output_cleanup",
		Message: fmt.Sprintf("Restored %d allowed path(s) to task baseline after %s.", len(restored), reason),
		Payload: payload,
	})
	return err
}

func (m model) readTaskFile(path string) (string, bool, error) {
	if strings.TrimSpace(path) == "" || strings.Contains(path, "\x00") || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
		return "", false, fmt.Errorf("invalid task path %q", path)
	}
	content, err := os.ReadFile(filepath.Join(m.project.Root, filepath.FromSlash(path)))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(content), true, nil
}

func renderFullFileDiff(path string, oldExists bool, oldContent string, newExists bool, newContent string) string {
	if diff := renderNoIndexDiff(path, oldExists, oldContent, newExists, newContent); strings.TrimSpace(diff) != "" {
		return diff
	}
	oldLines := splitDiffLines(oldContent)
	newLines := splitDiffLines(newContent)
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	if !oldExists && newExists {
		b.WriteString("new file mode 100644\n")
		b.WriteString("--- /dev/null\n")
		fmt.Fprintf(&b, "+++ b/%s\n", path)
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(newLines))
		for _, line := range newLines {
			fmt.Fprintf(&b, "+%s\n", line)
		}
		return strings.TrimRight(b.String(), "\n")
	}
	if oldExists && !newExists {
		b.WriteString("deleted file mode 100644\n")
		fmt.Fprintf(&b, "--- a/%s\n", path)
		b.WriteString("+++ /dev/null\n")
		fmt.Fprintf(&b, "@@ -1,%d +0,0 @@\n", len(oldLines))
		for _, line := range oldLines {
			fmt.Fprintf(&b, "-%s\n", line)
		}
		return strings.TrimRight(b.String(), "\n")
	}
	fmt.Fprintf(&b, "--- a/%s\n", path)
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines))
	for _, line := range oldLines {
		fmt.Fprintf(&b, "-%s\n", line)
	}
	for _, line := range newLines {
		fmt.Fprintf(&b, "+%s\n", line)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderNoIndexDiff(path string, oldExists bool, oldContent string, newExists bool, newContent string) string {
	dir, err := os.MkdirTemp("", "weazlcode-taskdiff-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	oldPath := filepath.Join(dir, "old")
	newPath := filepath.Join(dir, "new")
	if err := os.WriteFile(oldPath, []byte(oldContent), 0o600); err != nil {
		return ""
	}
	if err := os.WriteFile(newPath, []byte(newContent), 0o600); err != nil {
		return ""
	}
	cmd := exec.Command("git", "diff", "--no-index", "--", oldPath, newPath)
	out, err := cmd.CombinedOutput()
	if len(out) == 0 || err == nil {
		return ""
	}
	return normalizeNoIndexDiff(string(out), path, oldExists, newExists)
}

func normalizeNoIndexDiff(diff, path string, oldExists, newExists bool) string {
	var out []string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			out = append(out, fmt.Sprintf("diff --git a/%s b/%s", path, path))
		case strings.HasPrefix(line, "--- "):
			if oldExists {
				out = append(out, "--- a/"+path)
			} else {
				out = append(out, "--- /dev/null")
			}
		case strings.HasPrefix(line, "+++ "):
			if newExists {
				out = append(out, "+++ b/"+path)
			} else {
				out = append(out, "+++ /dev/null")
			}
		default:
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func splitDiffLines(content string) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func gitDiffOutputEmpty(diff string) bool {
	diff = strings.TrimSpace(diff)
	return strings.HasPrefix(diff, "$ git diff") && !strings.Contains(diff, "\ndiff --git ")
}

func (m model) untrackedFileDiff(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.Contains(path, "\x00") || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
		return "", nil
	}
	tracked := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
	tracked.Dir = m.project.Root
	if err := tracked.Run(); err == nil {
		return "", nil
	}
	fullPath := filepath.Join(m.project.Root, filepath.FromSlash(path))
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return "", nil
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	if strings.Contains(string(content), "\x00") {
		return fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\nBinary files /dev/null and b/%s differ", path, path, path), nil
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	b.WriteString("new file mode 100644\n")
	b.WriteString("index 0000000..0000000\n")
	b.WriteString("--- /dev/null\n")
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (m model) changedFiles() (string, error) {
	tool, ok := m.toolRegistry.Get("list_changed_files")
	if !ok {
		return "", fmt.Errorf("list_changed_files tool is not registered")
	}
	return tool.Execute(context.Background(), map[string]any{"cwd": m.project.Root})
}
