package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/lsp"
	"github.com/bprendie/weazlcode/internal/project"
)

const maxRepairAttempts = 2

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
	case "project":
		m.setIDEView("project", m.projectCommandText())
	case "models":
		m.setIDEView("models", m.modelRolesText())
	case "tools":
		m.setIDEView("tools", m.toolsViewText())
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
	case "worker-patch":
		return m.importWorkerPatch(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "reviewer-input":
		m.setIDEView("reviewer-input", m.reviewerInputCommandText())
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

func slashHelp() string {
	return strings.Join([]string{
		"Slash commands:",
		"/help - show commands",
		"/project - show active project",
		"/models - show model role mapping",
		"/tools - list enabled tools",
		"/config - show current local configuration summary",
		"/diff - show current git diff",
		"/outputs - show recent task events and tool outputs",
		"/files [query] - fuzzy-find project files",
		"/preview <path> - preview a project file",
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
		"/plan import <json> - validate and store a structured plan JSON payload",
		"/tasks - list latest plan tasks",
		"/packet - show local-worker packet for the first pending task",
		"/approve - approve the latest draft plan",
		"/reject [reason] - block the latest plan",
		"/run-task - mark first pending task running and show its worker packet",
		"/run-worker - ask configured worker role for a WorkerPatch JSON",
		"/worker-patch <json> - import a worker patch or blocker for the running task",
		"/reviewer-input - show frontier-review payload for the reviewing task",
		"/review <json> - import a reviewer verdict for the reviewing task",
		"/sessions - open sessions",
		"/workspaces - open workspace saves",
		"/new - start a new session",
		"/clear - clear current session context",
		"/trim - compact context",
		"/copy - release mouse for terminal selection",
		"/mouse - restore mouse scrolling",
	}, "\n")
}

func (m model) approveLatestPlan() (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Approve error: " + err.Error())
		m.status = "approve failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan to approve. Use `/plan draft <title>` or `/plan import <json>` first.")
		m.status = "no plan"
		return m, nil, true
	}
	if plan.Status != coding.PlanStatusDraft {
		m.addSystemNote(fmt.Sprintf("Plan %s is %s, not draft.", plan.Title, plan.Status))
		m.status = "plan not draft"
		return m, nil, true
	}
	if err := m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusApproved); err != nil {
		m.addSystemNote("Approve error: " + err.Error())
		m.status = "approve failed"
		return m, nil, true
	}
	for _, task := range plan.Tasks {
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "approval",
			Message: "Plan approved by user; task is eligible for worker packet generation.",
		})
	}
	plan.Status = coding.PlanStatusApproved
	m.addSystemNote(renderPlan(plan))
	m.status = "plan approved"
	return m, nil, true
}

func (m model) rejectLatestPlan(reason string) (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Reject error: " + err.Error())
		m.status = "reject failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan to reject.")
		m.status = "no plan"
		return m, nil, true
	}
	if reason == "" {
		reason = "Plan rejected by user."
	}
	if err := m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusBlocked); err != nil {
		m.addSystemNote("Reject error: " + err.Error())
		m.status = "reject failed"
		return m, nil, true
	}
	for _, task := range plan.Tasks {
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{TaskID: task.ID, Type: "rejection", Message: reason})
	}
	plan.Status = coding.PlanStatusBlocked
	m.addSystemNote(renderPlan(plan) + "\n\nRejection: " + reason)
	m.status = "plan rejected"
	return m, nil, true
}

func (m model) packetCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Packet error: " + err.Error()
	}
	if !ok {
		return "No plan yet. Use `/plan draft <title>` to create a draft plan."
	}
	task, ok, err := m.firstRunnableTask(plan.Tasks)
	if err != nil {
		return "Packet error: " + err.Error()
	}
	if !ok {
		return "No pending task found for packet generation."
	}
	packet, err := m.buildWorkerPacketForRun(task)
	if err != nil {
		return "Packet error: " + err.Error()
	}
	return "Worker task packet:\n" + renderJSON(packet)
}

func (m model) runNextTask() (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Run task error: " + err.Error())
		m.status = "run task failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan. Use `/plan draft <title>` or `/plan import <json>` first.")
		m.status = "no plan"
		return m, nil, true
	}
	if plan.Status != coding.PlanStatusApproved {
		m.addSystemNote(fmt.Sprintf("Plan must be approved before running a task. Current status: %s", plan.Status))
		m.status = "plan not approved"
		return m, nil, true
	}
	task, ok, err := m.firstRunnableTask(plan.Tasks)
	if err != nil {
		m.addSystemNote("Run task error: " + err.Error())
		m.status = "run task failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No pending or repairable task to run.")
		m.status = "no runnable task"
		return m, nil, true
	}
	packet, err := m.buildWorkerPacketForRun(task)
	if err != nil {
		m.addSystemNote("Run task error: " + err.Error())
		m.status = "run task failed"
		return m, nil, true
	}
	if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusRunning); err != nil {
		m.addSystemNote("Run task error: " + err.Error())
		m.status = "run task failed"
		return m, nil, true
	}
	payload, _ := json.Marshal(packet)
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    workerStartEventType(task),
		Message: workerStartMessage(task),
		Payload: payload,
	})
	m.addSystemNote("Worker dispatch prepared:\n" + renderJSON(packet))
	m.status = "task running"
	return m, nil, true
}

func (m model) importWorkerPatch(raw string) (tea.Model, tea.Cmd, bool) {
	if raw == "" {
		m.addSystemNote("Usage: /worker-patch <json>")
		m.status = "worker patch usage"
		return m, nil, true
	}
	patch, err := coding.ParseWorkerPatchJSON([]byte(raw))
	if err != nil {
		m.addSystemNote("Worker patch import error: " + err.Error())
		m.status = "worker patch failed"
		return m, nil, true
	}
	return m.applyWorkerPatch(patch)
}

func (m model) applyWorkerPatch(patch coding.WorkerPatch) (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Worker patch error: " + err.Error())
		m.status = "worker patch failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan. Use `/plan draft <title>` or `/plan import <json>` first.")
		m.status = "no plan"
		return m, nil, true
	}
	task, ok := taskByID(plan.Tasks, patch.TaskID)
	if !ok {
		m.addSystemNote(fmt.Sprintf("Worker patch task %q is not in the latest plan.", patch.TaskID))
		m.status = "worker patch failed"
		return m, nil, true
	}
	if task.Status != coding.TaskStatusRunning {
		m.addSystemNote(fmt.Sprintf("Task %s is %s, not running.", task.ID, task.Status))
		m.status = "worker patch failed"
		return m, nil, true
	}
	if strings.TrimSpace(patch.Blocker) != "" {
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Worker patch error: " + err.Error())
			m.status = "worker patch failed"
			return m, nil, true
		}
		payload, _ := json.Marshal(patch)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "worker_blocker",
			Message: patch.Blocker,
			Payload: payload,
		})
		m.addSystemNote(fmt.Sprintf("Worker reported blocker for %s:\n%s", task.Title, patch.Blocker))
		m.status = "task blocked"
		return m, nil, true
	}
	paths := coding.PatchPaths(patch.Patch)
	if err := coding.ValidatePatchPaths(paths, taskAllowedPaths(task), task.ForbiddenPaths); err != nil {
		m.addSystemNote("Worker patch rejected: " + err.Error())
		m.status = "worker patch rejected"
		return m, nil, true
	}
	result, err := coding.ApplyPatch(m.project.Root, patch.Patch)
	if err != nil {
		m.addSystemNote("Worker patch apply error: " + err.Error())
		m.status = "worker patch failed"
		return m, nil, true
	}
	if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusReviewing); err != nil {
		m.addSystemNote("Worker patch error: " + err.Error())
		m.status = "worker patch failed"
		return m, nil, true
	}
	payload, _ := json.Marshal(struct {
		Summary    string   `json:"summary"`
		Paths      []string `json:"paths"`
		PatchChars int      `json:"patch_chars"`
		Output     string   `json:"output"`
	}{
		Summary:    patch.Summary,
		Paths:      result.Paths,
		PatchChars: len(patch.Patch),
		Output:     result.Output,
	})
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "worker_patch",
		Message: emptyFallback(patch.Summary, "Worker patch applied; task is ready for review."),
		Payload: payload,
	})
	verification, err := m.runTaskVerification(task)
	if err != nil {
		payload, _ := json.Marshal(struct {
			Error string `json:"error"`
		}{Error: err.Error()})
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "verification_error",
			Message: err.Error(),
			Payload: payload,
		})
		m.addSystemNote("Verification error: " + err.Error())
		m.status = "verification failed"
		return m, nil, true
	}
	if len(verification) > 0 {
		payload, _ := json.Marshal(verification)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "verification",
			Message: fmt.Sprintf("Ran %d verification command(s).", len(verification)),
			Payload: payload,
		})
	}
	m.addSystemNote("Worker patch applied; task is ready for review:\n" + renderJSON(struct {
		Patch        coding.PatchApplyResult `json:"patch"`
		Verification []verificationResult    `json:"verification,omitempty"`
	}{
		Patch:        result,
		Verification: verification,
	}))
	m.status = "task reviewing"
	return m, nil, true
}

func (m model) runWorkerModel() (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Worker run error: " + err.Error())
		m.status = "worker run failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan. Use `/plan generate`, `/plan draft`, or `/plan import` first.")
		m.status = "no plan"
		return m, nil, true
	}
	task, ok := firstRunningTask(plan.Tasks)
	if !ok {
		m.addSystemNote("No running task. Use `/run-task` first.")
		m.status = "no running task"
		return m, nil, true
	}
	packet, err := m.buildWorkerPacketForRun(task)
	if err != nil {
		m.addSystemNote("Worker run error: " + err.Error())
		m.status = "worker run failed"
		return m, nil, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	raw, err := m.generateWorkerPatchJSON(ctx, packet)
	if err != nil {
		m.addSystemNote("Worker model error: " + err.Error())
		m.status = "worker run failed"
		return m, nil, true
	}
	patch, err := m.workerPatchFromGeneratedJSON(ctx, packet, raw)
	if err != nil {
		m.addSystemNote("Worker model patch error: " + err.Error() + "\n\nRaw response:\n" + raw)
		m.status = "worker run failed"
		return m, nil, true
	}
	return m.applyWorkerPatch(patch)
}

func (m model) generateWorkerPatchJSON(ctx context.Context, packet coding.TaskPacket) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("worker"))
	return client.Complete(ctx, workerPatchMessages(packet), 4096)
}

func (m model) repairWorkerPatchJSON(ctx context.Context, packet coding.TaskPacket, raw string, parseErr error) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("worker"))
	return client.Complete(ctx, workerPatchRepairMessages(packet, raw, parseErr), 4096)
}

func (m model) workerPatchFromGeneratedJSON(ctx context.Context, packet coding.TaskPacket, raw string) (coding.WorkerPatch, error) {
	patch, err := coding.ParseWorkerPatchJSON([]byte(extractJSONObject(raw)))
	if err == nil {
		return patch, nil
	}
	initialErr := err
	repaired, repairErr := m.repairWorkerPatchJSON(ctx, packet, raw, initialErr)
	if repairErr != nil {
		return coding.WorkerPatch{}, fmt.Errorf("%v; repair error: %w", initialErr, repairErr)
	}
	patch, err = coding.ParseWorkerPatchJSON([]byte(extractJSONObject(repaired)))
	if err != nil {
		return coding.WorkerPatch{}, fmt.Errorf("%v; repair parse error: %w", initialErr, err)
	}
	return patch, nil
}

func workerPatchMessages(packet coding.TaskPacket) []llm.ChatMessage {
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You are the WeazlCode local worker.",
				"Return a WorkerPatch JSON object.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"blocker\":\"...\"}.",
				"If you can complete the task, return a unified diff in patch and leave blocker empty.",
				"If you need missing context or cannot safely complete the task, set blocker and leave patch empty.",
				"Do not edit outside allowed_paths. Do not include prose outside JSON.",
			}, "\n"),
		},
		{
			Role:    "user",
			Content: "Task packet:\n" + renderJSON(packet),
		},
	}
}

func workerPatchRepairMessages(packet coding.TaskPacket, raw string, parseErr error) []llm.ChatMessage {
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You repair WeazlCode WorkerPatch JSON.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"blocker\":\"...\"}.",
				"Use the task_id from the task packet.",
				"If a safe patch is not possible, set blocker and leave patch empty.",
				"Do not include prose outside JSON.",
			}, "\n"),
		},
		{
			Role: "user",
			Content: fmt.Sprintf("Task packet:\n%s\n\nParser error:\n%s\n\nRaw response to repair:\n%s",
				renderJSON(packet),
				parseErr,
				strings.TrimSpace(raw),
			),
		},
	}
}

type verificationResult struct {
	Command string `json:"command"`
	Output  string `json:"output"`
}

func (m model) runTaskVerification(task coding.Task) ([]verificationResult, error) {
	commands := taskVerification(task)
	if len(commands) == 0 {
		return nil, nil
	}
	tool, ok := m.toolRegistry.Get("run_verification_command")
	if !ok {
		return nil, fmt.Errorf("run_verification_command tool is not registered")
	}
	results := make([]verificationResult, 0, len(commands))
	for _, commandText := range commands {
		name, args, err := splitVerificationCommand(commandText)
		if err != nil {
			return nil, err
		}
		rawArgs := make([]any, 0, len(args))
		for _, arg := range args {
			rawArgs = append(rawArgs, arg)
		}
		output, err := tool.Execute(context.Background(), map[string]any{
			"command": name,
			"args":    rawArgs,
			"cwd":     m.project.Root,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", commandText, err)
		}
		results = append(results, verificationResult{Command: commandText, Output: output})
	}
	return results, nil
}

func (m model) buildWorkerPacket(task coding.Task) (coding.TaskPacket, error) {
	diagnostics := m.codingDiagnostics()
	allowedTools := []string{"read_file", "read_file_range", "search_files", "apply_patch"}
	return coding.BuildTaskPacket(task, coding.ContextPackOptions{
		ProjectRoot: m.project.Root,
		DefaultAllowed: []string{
			".",
		},
		DefaultTools: allowedTools,
		DefaultVerify: []string{
			"go test ./...",
		},
		Diagnostics: diagnostics,
	})
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
		return packet, nil
	}
	packet.Goal = strings.TrimSpace(packet.Goal + "\n\nRepair focus:\n" + repair)
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

func (m model) firstRunnableTask(tasks []coding.Task) (coding.Task, bool, error) {
	for _, task := range tasks {
		if task.Status == coding.TaskStatusPending {
			return task, true, nil
		}
		if task.Status != coding.TaskStatusBlocked {
			continue
		}
		events, err := m.store.TaskEvents(task.ID)
		if err != nil {
			return coding.Task{}, false, err
		}
		if repairableTask(events) {
			return task, true, nil
		}
	}
	return coding.Task{}, false, nil
}

func taskByID(tasks []coding.Task, id string) (coding.Task, bool) {
	for _, task := range tasks {
		if task.ID == id {
			return task, true
		}
	}
	return coding.Task{}, false
}

func taskAllowedPaths(task coding.Task) []string {
	if len(task.AllowedPaths) == 0 {
		return []string{"."}
	}
	return task.AllowedPaths
}

func taskVerification(task coding.Task) []string {
	if len(task.Verification) == 0 {
		return []string{"go test ./..."}
	}
	return task.Verification
}

func splitVerificationCommand(commandText string) (string, []string, error) {
	fields := strings.Fields(commandText)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("verification command is empty")
	}
	return fields[0], fields[1:], nil
}

func (m model) reviewerInputCommandText() string {
	input, err := m.buildReviewerInput()
	if err != nil {
		return "Reviewer input error: " + err.Error()
	}
	return "Reviewer input:\n" + renderJSON(input)
}

func (m model) buildReviewerInput() (coding.ReviewerInput, error) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return coding.ReviewerInput{}, err
	}
	if !ok {
		return coding.ReviewerInput{}, fmt.Errorf("no plan found")
	}
	task, ok := firstReviewingTask(plan.Tasks)
	if !ok {
		return coding.ReviewerInput{}, fmt.Errorf("no reviewing task found")
	}
	packet, err := m.buildWorkerPacket(task)
	if err != nil {
		return coding.ReviewerInput{}, err
	}
	diff, err := m.gitDiff()
	if err != nil {
		return coding.ReviewerInput{}, err
	}
	events, err := m.store.TaskEvents(task.ID)
	if err != nil {
		return coding.ReviewerInput{}, err
	}
	instructions, _, _ := project.LoadInstructions(m.project.Root)
	return coding.ReviewerInput{
		Plan:              plan,
		TaskPacket:        packet,
		Diff:              diff,
		VerificationOut:   verificationOutputFromEvents(events),
		TaskEventsSummary: taskEventsSummary(events),
		Constraints: []string{
			"Project instructions:\n" + strings.TrimSpace(instructions.Content),
			"Review only the current task requirements and allowed paths.",
			"Return JSON with verdict approve, needs_fix, or blocked.",
			"Use needs_fix for focused repairable issues; use blocked only when more context or user input is required.",
		},
	}, nil
}

func (m model) importReviewVerdict(raw string) (tea.Model, tea.Cmd, bool) {
	if raw == "" {
		m.addSystemNote("Usage: /review <json>")
		m.status = "review usage"
		return m, nil, true
	}
	verdict, err := coding.ParseReviewVerdictJSON([]byte(raw))
	if err != nil {
		m.addSystemNote("Review import error: " + err.Error())
		m.status = "review failed"
		return m, nil, true
	}
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Review error: " + err.Error())
		m.status = "review failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan. Use `/plan draft <title>` or `/plan import <json>` first.")
		m.status = "no plan"
		return m, nil, true
	}
	task, ok := firstReviewingTask(plan.Tasks)
	if !ok {
		m.addSystemNote("No reviewing task found.")
		m.status = "no reviewing task"
		return m, nil, true
	}
	payload, _ := json.Marshal(verdict)
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "reviewer_verdict",
		Message: verdict.Summary,
		Payload: payload,
	})
	switch verdict.Verdict {
	case coding.ReviewApprove:
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusDone); err != nil {
			m.addSystemNote("Review error: " + err.Error())
			m.status = "review failed"
			return m, nil, true
		}
		if planDoneAfterTask(plan.Tasks, task.ID) {
			_ = m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusDone)
		}
		m.addSystemNote("Reviewer approved task:\n" + renderJSON(verdict))
		m.status = "task done"
	case coding.ReviewNeedsFix:
		events, err := m.store.TaskEvents(task.ID)
		if err != nil {
			m.addSystemNote("Review error: " + err.Error())
			m.status = "review failed"
			return m, nil, true
		}
		attempt := repairAttemptCount(events) + 1
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Review error: " + err.Error())
			m.status = "review failed"
			return m, nil, true
		}
		if attempt > maxRepairAttempts {
			_, _ = m.store.AddTaskEvent(coding.TaskEvent{
				TaskID:  task.ID,
				Type:    "repair_limit",
				Message: fmt.Sprintf("Repair limit reached after %d attempts.", maxRepairAttempts),
			})
			m.addSystemNote("Reviewer requested fixes, but the repair limit has been reached:\n" + renderJSON(verdict))
			m.status = "repair limit reached"
			return m, nil, true
		}
		repairPayload, _ := json.Marshal(struct {
			Attempt int      `json:"attempt"`
			Summary string   `json:"summary"`
			Issues  []string `json:"issues,omitempty"`
		}{
			Attempt: attempt,
			Summary: verdict.Summary,
			Issues:  verdict.Issues,
		})
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "repair_requested",
			Message: repairRequestText(verdict, attempt),
			Payload: repairPayload,
		})
		m.addSystemNote("Reviewer requested focused repair:\n" + renderJSON(verdict))
		m.status = "repair requested"
	case coding.ReviewBlocked:
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Review error: " + err.Error())
			m.status = "review failed"
			return m, nil, true
		}
		m.addSystemNote("Reviewer blocked task:\n" + renderJSON(verdict))
		m.status = "review blocked"
	}
	return m, nil, true
}

func (m model) gitDiff() (string, error) {
	tool, ok := m.toolRegistry.Get("git_diff")
	if !ok {
		return "", fmt.Errorf("git_diff tool is not registered")
	}
	return tool.Execute(context.Background(), map[string]any{"cwd": m.project.Root})
}

func firstReviewingTask(tasks []coding.Task) (coding.Task, bool) {
	for _, task := range tasks {
		if task.Status == coding.TaskStatusReviewing {
			return task, true
		}
	}
	return coding.Task{}, false
}

func firstRunningTask(tasks []coding.Task) (coding.Task, bool) {
	for _, task := range tasks {
		if task.Status == coding.TaskStatusRunning {
			return task, true
		}
	}
	return coding.Task{}, false
}

func planDoneAfterTask(tasks []coding.Task, doneTaskID string) bool {
	for _, task := range tasks {
		if task.ID == doneTaskID {
			continue
		}
		if task.Status != coding.TaskStatusDone {
			return false
		}
	}
	return true
}

func verificationOutputFromEvents(events []coding.TaskEvent) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "verification" && events[i].Type != "verification_error" {
			continue
		}
		if len(events[i].Payload) > 0 {
			return string(events[i].Payload)
		}
		return events[i].Message
	}
	return ""
}

func taskEventsSummary(events []coding.TaskEvent) string {
	if len(events) == 0 {
		return ""
	}
	var b strings.Builder
	for _, event := range events {
		fmt.Fprintf(&b, "- %s: %s\n", event.Type, event.Message)
	}
	return strings.TrimRight(b.String(), "\n")
}

func repairableTask(events []coding.TaskEvent) bool {
	if repairAttemptCount(events) >= maxRepairAttempts {
		return false
	}
	repairRequested := false
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "repair_requested":
			repairRequested = true
			continue
		case "repair_limit", "repair_start", "worker_patch", "worker_blocker":
			if !repairRequested {
				return false
			}
		}
		if events[i].Type == "reviewer_verdict" {
			verdict, ok := reviewVerdictFromEvent(events[i])
			if !ok {
				continue
			}
			return verdict.Verdict == coding.ReviewNeedsFix && repairRequested
		}
	}
	return false
}

func repairAttemptCount(events []coding.TaskEvent) int {
	count := 0
	for _, event := range events {
		if event.Type == "repair_requested" {
			count++
		}
	}
	return count
}

func latestRepairRequest(events []coding.TaskEvent) (string, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "repair_requested" && strings.TrimSpace(events[i].Message) != "" {
			return events[i].Message, true
		}
	}
	return "", false
}

func reviewVerdictFromEvent(event coding.TaskEvent) (coding.ReviewVerdict, bool) {
	if len(event.Payload) == 0 {
		return coding.ReviewVerdict{}, false
	}
	var verdict coding.ReviewVerdict
	if err := json.Unmarshal(event.Payload, &verdict); err != nil {
		return coding.ReviewVerdict{}, false
	}
	return verdict, true
}

func repairRequestText(verdict coding.ReviewVerdict, attempt int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repair attempt %d/%d: %s", attempt, maxRepairAttempts, emptyFallback(verdict.Summary, "Address reviewer issues."))
	for _, issue := range verdict.Issues {
		if strings.TrimSpace(issue) != "" {
			fmt.Fprintf(&b, "\n- %s", strings.TrimSpace(issue))
		}
	}
	return b.String()
}

func workerStartEventType(task coding.Task) string {
	if task.Status == coding.TaskStatusBlocked {
		return "repair_start"
	}
	return "worker_start"
}

func workerStartMessage(task coding.Task) string {
	if task.Status == coding.TaskStatusBlocked {
		return "Repair task marked running; worker packet prepared for local model dispatch."
	}
	return "Task marked running; worker packet prepared for local model dispatch."
}

func (m model) instructionsCommandText() string {
	instructions, ok, err := project.LoadInstructions(m.project.Root)
	if err != nil {
		return "Instructions error: " + err.Error()
	}
	commands := project.DiscoverCommands(m.project.Root)
	var b strings.Builder
	b.WriteString("Project instructions:\n")
	if ok {
		fmt.Fprintf(&b, "file: %s\n\n%s", filepath.Base(instructions.Path), strings.TrimSpace(instructions.Content))
	} else {
		b.WriteString("No WEAZLCODE.md or AGENTS.md found. Run `weazlcode init`.\n")
	}
	if len(commands) > 0 {
		b.WriteString("\n\nDiscovered commands:")
		for _, command := range commands {
			fmt.Fprintf(&b, "\n- %s", command)
		}
	}
	memories, err := m.store.ProjectMemories(m.project.Root, 10)
	if err == nil && len(memories) > 0 {
		b.WriteString("\n\nProject memory:")
		for _, memory := range memories {
			fmt.Fprintf(&b, "\n- %s: %s", memory.Key, memory.Value)
		}
	}
	return b.String()
}

func (m model) handleProjectMemoryCommand(args []string, rawArgs string) (tea.Model, tea.Cmd, bool) {
	_ = args
	rawArgs = strings.TrimSpace(rawArgs)
	if rawArgs == "" {
		m.setIDEView("memory", m.projectMemoryText())
		return m, nil, true
	}
	key, value, ok := strings.Cut(rawArgs, "=")
	if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		m.addSystemNote("Usage: /memory key=value")
		m.status = "memory usage"
		return m, nil, true
	}
	if err := m.store.RememberProject(m.project.Root, strings.TrimSpace(key), strings.TrimSpace(value), "project"); err != nil {
		m.addSystemNote("Project memory error: " + err.Error())
		m.status = "memory failed"
		return m, nil, true
	}
	m.addSystemNote("Saved project memory: " + strings.TrimSpace(key))
	m.status = "memory saved"
	return m, nil, true
}

func (m model) projectMemoryText() string {
	memories, err := m.store.ProjectMemories(m.project.Root, 50)
	if err != nil {
		return "Project memory error: " + err.Error()
	}
	if len(memories) == 0 {
		return "Project memory:\nNo project memories yet. Use `/memory key=value`."
	}
	var b strings.Builder
	b.WriteString("Project memory:\n")
	for _, memory := range memories {
		fmt.Fprintf(&b, "- %s: %s", memory.Key, memory.Value)
		if memory.Tags != "" {
			fmt.Fprintf(&b, " [%s]", memory.Tags)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) finalReviewCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Final review error: " + err.Error()
	}
	diff, diffErr := m.gitDiff()
	if !ok {
		if diffErr != nil {
			return "Final review error: " + diffErr.Error()
		}
		return "Final review:\nNo plan found.\n\nDiff:\n" + emptyFallback(diff, "No changes.")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Final review:\nplan: %s\nstatus: %s\n", plan.Title, plan.Status)
	for _, task := range plan.Tasks {
		fmt.Fprintf(&b, "- [%s] %s\n", task.Status, task.Title)
		events, err := m.store.TaskEvents(task.ID)
		if err == nil {
			if verdict, ok := latestReviewVerdict(events); ok {
				fmt.Fprintf(&b, "  reviewer: %s - %s\n", verdict.Verdict, verdict.Summary)
			}
		}
	}
	if diffErr != nil {
		fmt.Fprintf(&b, "\nDiff error: %s", diffErr)
	} else {
		fmt.Fprintf(&b, "\nDiff:\n%s", emptyFallback(diff, "No changes."))
	}
	fmt.Fprintf(&b, "\n\nRollback guidance:\n- Review the diff before committing.\n- To discard uncommitted changes manually, use git restore on specific files.")
	return b.String()
}

func (m model) commitMessageCommandText() string {
	return m.generatedCommitMessage()
}

func (m model) generatedCommitMessage() string {
	plan, ok, _ := m.store.LatestPlan(m.session.ID)
	if ok && strings.TrimSpace(plan.Title) != "" {
		return "Complete " + strings.TrimSpace(plan.Title)
	}
	diff, err := m.gitDiff()
	if err != nil || strings.TrimSpace(diff) == "" {
		return "Update WeazlCode project"
	}
	return "Update project files"
}

func (m model) commitCommand(raw string) (tea.Model, tea.Cmd, bool) {
	if strings.ToLower(strings.TrimSpace(raw)) != "yes" {
		m.addSystemNote("Commit is confirmation-gated. Use `/commit yes` to run `git add .` and `git commit` with the generated message.")
		m.status = "commit needs confirmation"
		return m, nil, true
	}
	message := m.generatedCommitMessage()
	if out, err := runGitCommit(m.project.Root, message); err != nil {
		m.addSystemNote("Commit error:\n" + out + "\n" + err.Error())
		m.status = "commit failed"
		return m, nil, true
	}
	m.addSystemNote("Committed changes:\n" + message)
	m.status = "committed"
	return m, nil, true
}

func (m model) exportRunCommandText() string {
	text := m.finalReviewCommandText()
	dir := filepath.Join(m.project.StateDir, "runs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "Export error: " + err.Error()
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405")+".md")
	body := "# WeazlCode Run Artifact\n\n" + text + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "Export error: " + err.Error()
	}
	return "Exported run artifact:\n" + path
}

func latestReviewVerdict(events []coding.TaskEvent) (coding.ReviewVerdict, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "reviewer_verdict" {
			return reviewVerdictFromEvent(events[i])
		}
	}
	return coding.ReviewVerdict{}, false
}

func runGitCommit(root, message string) (string, error) {
	add := exec.Command("git", "add", ".")
	add.Dir = root
	out, err := add.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	commit := exec.Command("git", "commit", "-m", message)
	commit.Dir = root
	commitOut, err := commit.CombinedOutput()
	return string(out) + string(commitOut), err
}

func (m model) handlePlanCommand(args []string, rawArgs string) (tea.Model, tea.Cmd, bool) {
	if len(args) > 0 && strings.ToLower(args[0]) == "generate" {
		request := strings.TrimSpace(strings.TrimPrefix(rawArgs, args[0]))
		return m.generatePlanCommand(request)
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "draft" {
		title := strings.TrimSpace(strings.Join(args[1:], " "))
		if title == "" {
			title = "Draft coding plan"
		}
		plan := coding.Plan{
			ID:          uuid.NewString(),
			SessionID:   m.session.ID,
			ProjectRoot: m.project.Root,
			Title:       title,
			Summary:     "Manual draft plan. Replace this with orchestrator-generated steps in the next phase increment.",
			Status:      coding.PlanStatusDraft,
			Tasks: []coding.Task{
				{
					ID:     uuid.NewString(),
					Title:  "Define implementation steps",
					Goal:   title,
					Status: coding.TaskStatusPending,
					AcceptanceChecks: []coding.AcceptanceCheck{
						{Description: "Plan is reviewed and approved before worker execution."},
					},
				},
			},
		}
		plan.Tasks[0].PlanID = plan.ID
		if err := m.store.SavePlan(plan); err != nil {
			m.err = err.Error()
			return m, nil, true
		}
		if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
			plan = saved
		}
		m.addSystemNote(renderPlan(plan))
		m.status = "draft plan created"
		return m, nil, true
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "import" {
		raw := strings.TrimSpace(strings.TrimPrefix(rawArgs, args[0]))
		if raw == "" {
			m.addSystemNote("Usage: /plan import <json>")
			m.status = "plan import usage"
			return m, nil, true
		}
		plan, err := coding.DecodePlanJSON([]byte(raw))
		if err != nil {
			m.addSystemNote("Plan import error: " + err.Error())
			m.status = "plan import failed"
			return m, nil, true
		}
		plan = coding.PrepareImportedPlan(plan, m.session.ID, m.project.Root, uuid.NewString)
		if err := coding.ValidatePlan(plan); err != nil {
			m.addSystemNote("Plan import error: " + err.Error())
			m.status = "plan import failed"
			return m, nil, true
		}
		if err := m.store.SavePlan(plan); err != nil {
			m.err = err.Error()
			return m, nil, true
		}
		if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
			plan = saved
		}
		m.addSystemNote(renderPlan(plan))
		m.status = "plan imported"
		return m, nil, true
	}
	m.setIDEView("plan", m.planCommandText())
	return m, nil, true
}

func (m model) generatePlanCommand(request string) (tea.Model, tea.Cmd, bool) {
	if strings.TrimSpace(request) == "" {
		m.addSystemNote("Usage: /plan generate <request>")
		m.status = "plan generate usage"
		return m, nil, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	raw, err := m.generatePlanJSON(ctx, request)
	if err != nil {
		m.addSystemNote("Plan generate error: " + err.Error())
		m.status = "plan generate failed"
		return m, nil, true
	}
	plan, err := m.planFromGeneratedJSON(raw)
	if err != nil {
		initialErr := err
		repaired, repairErr := m.repairPlanJSON(ctx, request, raw, initialErr)
		if repairErr != nil {
			m.addSystemNote("Plan generate parse error: " + initialErr.Error() + "\n\nRepair error: " + repairErr.Error() + "\n\nRaw response:\n" + raw)
			m.status = "plan generate failed"
			return m, nil, true
		}
		plan, err = m.planFromGeneratedJSON(repaired)
		if err != nil {
			m.addSystemNote("Plan generate parse error: " + initialErr.Error() + "\n\nRepair parse error: " + err.Error() + "\n\nRaw response:\n" + raw + "\n\nRepaired response:\n" + repaired)
			m.status = "plan generate failed"
			return m, nil, true
		}
	}
	if err := m.store.SavePlan(plan); err != nil {
		m.err = err.Error()
		m.status = "plan generate failed"
		return m, nil, true
	}
	if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
		plan = saved
	}
	m.addSystemNote(renderPlan(plan))
	m.status = "plan generated"
	return m, nil, true
}

func (m model) generatePlanJSON(ctx context.Context, request string) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("orchestrator"))
	return client.Complete(ctx, m.planGenerateMessages(request), 2048)
}

func (m model) repairPlanJSON(ctx context.Context, request, raw string, parseErr error) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("orchestrator"))
	return client.Complete(ctx, m.planRepairMessages(request, raw, parseErr), 2048)
}

func (m model) planGenerateMessages(request string) []llm.ChatMessage {
	instructions, _, _ := project.LoadInstructions(m.project.Root)
	memories, _ := m.store.ProjectMemories(m.project.Root, 10)
	commands := project.DiscoverCommands(m.project.Root)
	var contextText strings.Builder
	fmt.Fprintf(&contextText, "Project root: %s\n", m.project.Root)
	if len(m.project.Languages) > 0 {
		fmt.Fprintf(&contextText, "Languages: %s\n", strings.Join(m.project.Languages, ", "))
	}
	if strings.TrimSpace(instructions.Content) != "" {
		fmt.Fprintf(&contextText, "\nProject instructions:\n%s\n", strings.TrimSpace(instructions.Content))
	}
	if len(commands) > 0 {
		contextText.WriteString("\nDiscovered commands:\n")
		for _, command := range commands {
			fmt.Fprintf(&contextText, "- %s\n", command)
		}
	}
	if len(memories) > 0 {
		contextText.WriteString("\nProject memory:\n")
		for _, memory := range memories {
			fmt.Fprintf(&contextText, "- %s: %s\n", memory.Key, memory.Value)
		}
	}
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You are the WeazlCode orchestrator.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"title\":\"...\",\"summary\":\"...\",\"tasks\":[{\"title\":\"...\",\"goal\":\"...\",\"allowed_paths\":[\"...\"],\"forbidden_paths\":[\"...\"],\"context_files\":[\"...\"],\"verification\":[\"...\"],\"acceptance_checks\":[{\"description\":\"...\",\"command\":\"...\"}]}]}",
				"Every task must be small enough for one local worker and must include explicit allowed_paths.",
				"Prefer verification commands discovered from the project context.",
			}, "\n"),
		},
		{
			Role:    "user",
			Content: strings.TrimSpace(contextText.String()) + "\n\nUser request:\n" + strings.TrimSpace(request),
		},
	}
}

func (m model) planRepairMessages(request, raw string, parseErr error) []llm.ChatMessage {
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You repair WeazlCode plan JSON.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"title\":\"...\",\"summary\":\"...\",\"tasks\":[{\"title\":\"...\",\"goal\":\"...\",\"allowed_paths\":[\"...\"],\"forbidden_paths\":[\"...\"],\"context_files\":[\"...\"],\"verification\":[\"...\"],\"acceptance_checks\":[{\"description\":\"...\",\"command\":\"...\"}]}]}",
				"Do not add unknown fields. Every task must include title, goal, and allowed_paths.",
			}, "\n"),
		},
		{
			Role: "user",
			Content: fmt.Sprintf("User request:\n%s\n\nParser error:\n%s\n\nRaw response to repair:\n%s",
				strings.TrimSpace(request),
				parseErr,
				strings.TrimSpace(raw),
			),
		},
	}
}

func (m model) planFromGeneratedJSON(raw string) (coding.Plan, error) {
	plan, err := coding.DecodePlanJSON([]byte(extractJSONObject(raw)))
	if err != nil {
		return coding.Plan{}, err
	}
	plan = coding.PrepareImportedPlan(plan, m.session.ID, m.project.Root, uuid.NewString)
	if err := coding.ValidatePlan(plan); err != nil {
		return coding.Plan{}, err
	}
	return plan, nil
}

func extractJSONObject(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return raw
}

func (m model) planCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Plan error: " + err.Error()
	}
	if !ok {
		return "No plan yet. Use `/plan draft <title>` to create one."
	}
	return renderPlan(plan)
}

func (m model) tasksCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Task error: " + err.Error()
	}
	if !ok {
		return "No plan tasks yet. Use `/plan draft <title>` to create a draft plan."
	}
	if len(plan.Tasks) == 0 {
		return "Plan has no tasks."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Tasks for %s:\n", plan.Title)
	fmt.Fprintf(&b, "%s\n\n", taskProgressSummary(plan))
	for i, task := range plan.Tasks {
		fmt.Fprintf(&b, "%d. [%s] %s\n   %s\n", i+1, task.Status, task.Title, task.Goal)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderPlan(plan coding.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan: %s\nstatus: %s\nid: %s\nupdated: %s\n\n%s",
		plan.Title,
		plan.Status,
		plan.ID,
		plan.UpdatedAt.Format(time.RFC3339),
		plan.Summary,
	)
	if len(plan.Tasks) > 0 {
		b.WriteString("\n\nTasks:")
		for i, task := range plan.Tasks {
			fmt.Fprintf(&b, "\n%d. [%s] %s\n   %s", i+1, task.Status, task.Title, task.Goal)
			if len(task.AcceptanceChecks) > 0 {
				b.WriteString("\n   checks:")
				for _, check := range task.AcceptanceChecks {
					label := check.Description
					if label == "" {
						label = check.Command
					}
					fmt.Fprintf(&b, "\n   - %s", label)
				}
			}
		}
	}
	return b.String()
}

func (m model) projectCommandText() string {
	langs := "none detected"
	if len(m.project.Languages) > 0 {
		langs = strings.Join(m.project.Languages, ", ")
	}
	ignore := "none"
	if m.project.IgnoreFile != "" {
		ignore = filepath.Base(m.project.IgnoreFile)
	}
	return fmt.Sprintf("Project:\nroot: %s\ngit: %t\nbranch: %s\ndirty: %t\nfiles: %d\nlanguages: %s\nstate: %s\nlogs: %s\nignore: %s",
		m.project.Root,
		m.project.GitRoot,
		emptyFallback(m.project.Branch, "none"),
		m.project.Dirty,
		m.project.FileCount,
		langs,
		m.project.StateDir,
		m.project.LogDir,
		ignore,
	)
}

func (m model) modelRolesText() string {
	roleProvider := func(role, providerName string) string {
		p, ok := m.cfg.Providers[providerName]
		if !ok {
			return fmt.Sprintf("%s: %s (missing provider)", role, providerName)
		}
		return fmt.Sprintf("%s: %s -> %s/%s", role, providerName, p.Type, p.Model)
	}
	return strings.Join([]string{
		"Model roles:",
		roleProvider("orchestrator", m.cfg.ModelRoles.Orchestrator),
		roleProvider("worker", m.cfg.ModelRoles.Worker),
		roleProvider("reviewer", m.cfg.ModelRoles.Reviewer),
		roleProvider("summarizer", m.cfg.ModelRoles.Summarizer),
	}, "\n")
}

func (m model) toolsViewText() string {
	registered := m.toolRegistry.List()
	if len(registered) == 0 {
		return "Tools:\nnone registered"
	}
	var b strings.Builder
	b.WriteString("Tools:\n")
	for _, tool := range registered {
		fmt.Fprintf(&b, "- %s [%s]\n  %s\n", tool.Name(), safetyLabel(tool.SafetyLevel()), tool.Description())
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) configViewText() string {
	active := m.cfg.Active()
	var b strings.Builder
	fmt.Fprintf(&b, "Config:\npath: %s\nactive_provider: %s\nactive_model: %s/%s\nserver_url: %s\ncontext_window: %d\nmarkdown: %t (%s)\nresume_last_session: %t\n\nModel roles:\n",
		emptyFallback(m.cfgPath, "default"),
		m.cfg.ActiveProvider,
		active.Type,
		active.Model,
		emptyFallback(active.ServerURL, "not set"),
		active.ContextWindow,
		m.cfg.UI.MarkdownEnabled(),
		m.cfg.UI.MarkdownStyle,
		m.cfg.UI.ResumeLastSession,
	)
	fmt.Fprintf(&b, "- orchestrator: %s\n", m.cfg.ModelRoles.Orchestrator)
	fmt.Fprintf(&b, "- worker: %s\n", m.cfg.ModelRoles.Worker)
	fmt.Fprintf(&b, "- reviewer: %s\n", m.cfg.ModelRoles.Reviewer)
	fmt.Fprintf(&b, "- summarizer: %s\n", m.cfg.ModelRoles.Summarizer)
	fmt.Fprintf(&b, "\nTools:\nenabled: %t\nauto_execute_safe: %t\nmax_output_chars: %d\nmax_file_bytes: %d\n", m.cfg.Tools.Enabled, m.cfg.Tools.AutoExecute, m.cfg.Tools.MaxOutputChars, m.cfg.Tools.MaxFileBytes)
	return strings.TrimRight(b.String(), "\n")
}

func (m model) diffCommandText() string {
	diff, err := m.gitDiff()
	if err != nil {
		return "Diff error: " + err.Error()
	}
	return renderDiffView(diff)
}

func (m model) lspManager() lsp.Manager {
	return lsp.NewManager(m.project.Root, m.project.Languages)
}

func (m model) lspCommandText() string {
	servers := m.lspManager().DetectServers()
	if len(servers) == 0 {
		return "LSP:\nNo language servers detected for this project."
	}
	var b strings.Builder
	b.WriteString("LSP:\n")
	for _, server := range servers {
		state := "missing"
		if server.Available {
			state = "available"
		}
		fmt.Fprintf(&b, "- %s: %s (%s)\n", server.Language, server.Command, state)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) diagnosticsCommandText() string {
	diagnostics, err := m.lspManager().Diagnostics(context.Background())
	if err != nil {
		return "Diagnostics error: " + err.Error()
	}
	if len(diagnostics) == 0 {
		return "Diagnostics:\nNo diagnostics."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Diagnostics: %d\n", len(diagnostics))
	for i, diagnostic := range diagnostics {
		if i >= 100 {
			fmt.Fprintf(&b, "\n[truncated: %d diagnostics omitted]", len(diagnostics)-i)
			break
		}
		fmt.Fprintf(&b, "\n%d. %s:%d:%d [%s] %s", i+1, diagnostic.File, diagnostic.Line, diagnostic.Column, diagnostic.Severity, diagnostic.Message)
		if diagnostic.Source != "" {
			fmt.Fprintf(&b, " (%s)", diagnostic.Source)
		}
	}
	return b.String()
}

func (m model) symbolsCommandText(query string) string {
	symbols, err := m.lspManager().Symbols(context.Background(), query)
	if err != nil {
		return "Symbols error: " + err.Error()
	}
	if len(symbols) == 0 {
		return "Symbols:\nNo symbols found."
	}
	var b strings.Builder
	if query == "" {
		fmt.Fprintf(&b, "Symbols: %d\n", len(symbols))
	} else {
		fmt.Fprintf(&b, "Symbols: %d match(es) for %q\n", len(symbols), query)
	}
	for i, symbol := range symbols {
		if i >= 100 {
			fmt.Fprintf(&b, "\n[truncated: %d symbols omitted]", len(symbols)-i)
			break
		}
		fmt.Fprintf(&b, "\n%d. %s %s  %s:%d", i+1, symbol.Kind, symbol.Name, symbol.File, symbol.Line)
	}
	return b.String()
}

func (m model) definitionCommandText(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Usage: /definition <symbol>"
	}
	symbol, ok, err := m.lspManager().Definition(context.Background(), name)
	if err != nil {
		return "Definition error: " + err.Error()
	}
	if !ok {
		return "Definition:\nNo definition found for " + name
	}
	return fmt.Sprintf("Definition:\n%s %s\n%s:%d", symbol.Kind, symbol.Name, symbol.File, symbol.Line)
}

func (m model) referencesCommandText(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Usage: /references <symbol>"
	}
	refs, err := m.lspManager().References(context.Background(), name)
	if err != nil {
		return "References error: " + err.Error()
	}
	if len(refs) == 0 {
		return "References:\nNo references found for " + name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "References: %d for %s\n", len(refs), name)
	for i, ref := range refs {
		if i >= 100 {
			fmt.Fprintf(&b, "\n[truncated: %d references omitted]", len(refs)-i)
			break
		}
		fmt.Fprintf(&b, "\n%d. %s:%d  %s", i+1, ref.File, ref.Line, ref.Text)
	}
	return b.String()
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
