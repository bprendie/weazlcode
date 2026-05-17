package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/lsp"
	"github.com/bprendie/weazlcode/internal/project"
)

const maxRepairAttempts = 2
const maxWorkerPatchDiffRepairAttempts = 2
const maxPlanGenerateTokens = 8192
const maxTaskBaselineBytes = 2 * 1024 * 1024

var modelSizePattern = regexp.MustCompile(`(?i)(?:^|[^0-9])([0-9]+(?:\.[0-9]+)?)\s*b(?:[^a-z]|$)`)

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
	if issues := coding.ValidatePlanQuality(plan); len(issues) > 0 {
		m.addSystemNote("Plan quality check failed:\n" + renderPlanQualityIssues(issues) + "\n\nUse `/plan edit`, `/attach`, or `/plan validate` before approving.")
		m.status = "approval blocked"
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

func (m model) planValidationCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Plan validation error: " + err.Error()
	}
	if !ok {
		return "No plan. Use `/plan draft`, `/plan generate`, or `/plan import` first."
	}
	issues := coding.ValidatePlanQuality(plan)
	if len(issues) == 0 {
		return "Plan validation passed.\n\n" + renderPlan(plan)
	}
	return "Plan validation failed:\n" + renderPlanQualityIssues(issues)
}

func renderPlanQualityIssues(issues []coding.PlanQualityIssue) string {
	var b strings.Builder
	for i, issue := range issues {
		title := strings.TrimSpace(issue.TaskTitle)
		if title == "" {
			title = strings.TrimSpace(issue.TaskID)
		}
		if title == "" {
			title = "plan"
		}
		fmt.Fprintf(&b, "%d. [%s] %s: %s\n", i+1, issue.Severity, title, issue.Message)
	}
	return strings.TrimRight(b.String(), "\n")
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
	if err := m.recordTaskBaseline(task); err != nil {
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
	packetKind := "worker_packet"
	if workerStartEventType(task) == "repair_start" {
		packetKind = "repair_packet"
	}
	m.writeRunArtifact(packetKind, packet)
	m.addSystemNote("Worker dispatch prepared:\n" + renderJSON(packet))
	m.status = "task running"
	return m, nil, true
}

func (m model) runParallelWorkers() (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Parallel worker error: " + err.Error())
		m.status = "parallel workers failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan. Use `/plan draft`, `/plan generate`, or `/plan import` first.")
		m.status = "no plan"
		return m, nil, true
	}
	if plan.Status != coding.PlanStatusApproved {
		m.addSystemNote(fmt.Sprintf("Plan must be approved before running workers. Current status: %s", plan.Status))
		m.status = "plan not approved"
		return m, nil, true
	}
	candidates, err := m.parallelRunnableTasks(plan.Tasks, m.workerConcurrency()-len(m.workerRuns))
	if err != nil {
		m.addSystemNote("Parallel worker error: " + err.Error())
		m.status = "parallel workers failed"
		return m, nil, true
	}
	if len(candidates) == 0 {
		m.addSystemNote("No independent pending tasks are available for parallel dispatch.")
		m.status = "no parallel tasks"
		return m, nil, true
	}
	type dispatch struct {
		task   coding.Task
		packet coding.TaskPacket
	}
	dispatches := make([]dispatch, 0, len(candidates))
	for _, task := range candidates {
		packet, err := m.buildWorkerPacketForRun(task)
		if err != nil {
			m.addSystemNote("Parallel worker error: " + err.Error())
			m.status = "parallel workers failed"
			return m, nil, true
		}
		dispatches = append(dispatches, dispatch{task: task, packet: packet})
	}
	var cmds []tea.Cmd
	var packets []coding.TaskPacket
	for _, dispatch := range dispatches {
		task := dispatch.task
		packet := dispatch.packet
		if err := m.recordTaskBaseline(task); err != nil {
			m.addSystemNote("Parallel worker error: " + err.Error())
			m.status = "parallel workers failed"
			return m, nil, true
		}
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusRunning); err != nil {
			m.addSystemNote("Parallel worker error: " + err.Error())
			m.status = "parallel workers failed"
			return m, nil, true
		}
		payload, _ := json.Marshal(packet)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "parallel_worker_start",
			Message: "Parallel worker dispatch prepared.",
			Payload: payload,
		})
		m.writeRunArtifact("worker_packet_"+task.ID, packet)
		ctx, cancel := context.WithTimeout(context.Background(), m.workerRequestTimeout())
		m.modelRunID++
		runID := m.modelRunID
		m.ensureWorkerRunMaps()
		m.workerRuns[runID] = task.ID
		m.cancelWorkerRuns[runID] = cancel
		cmds = append(cmds, m.runWorkerModelCmd(ctx, runID, packet))
		packets = append(packets, packet)
	}
	m.thinking = true
	m.working.Spinner = spinner.Jump
	m.streamAt = time.Now()
	m.status = fmt.Sprintf("running %d worker(s)", len(candidates))
	m.addSystemNote("Parallel worker dispatch prepared:\n" + renderJSON(packets))
	return m, tea.Batch(append(cmds, m.working.Tick)...), true
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
	return m.applyWorkerPatch(patch, false)
}

func (m model) applyWorkerPatch(patch coding.WorkerPatch, repairInvalidDiff bool) (tea.Model, tea.Cmd, bool) {
	return m.applyWorkerPatchWithTelemetry(patch, repairInvalidDiff, nil)
}

func (m model) applyWorkerPatchWithTelemetry(patch coding.WorkerPatch, repairInvalidDiff bool, telemetry *modelTelemetry) (tea.Model, tea.Cmd, bool) {
	return m.applyWorkerPatchWithRepair(patch, repairInvalidDiff, 0, nil, telemetry)
}

func (m model) applyWorkerPatchWithRepair(patch coding.WorkerPatch, repairInvalidDiff bool, repairAttempts int, applyErrors []error, telemetry *modelTelemetry) (tea.Model, tea.Cmd, bool) {
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
	if telemetry != nil {
		payload, _ := json.Marshal(telemetry)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "worker_model",
			Message: fmt.Sprintf("%s/%s in %dms", telemetry.Provider, telemetry.Model, telemetry.LatencyMS),
			Payload: payload,
		})
	}
	if strings.TrimSpace(patch.Blocker) != "" {
		events, _ := m.store.TaskEvents(task.ID)
		if err := m.restoreTaskBaseline(task, events, "worker blocker"); err != nil {
			m.addSystemNote("Worker cleanup error: " + err.Error())
			m.status = "worker cleanup failed"
			return m, nil, true
		}
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
		m.status = "worker reported blocker"
		return m, m.notificationCmd("worker_blocker", task.Title, patch.Blocker), true
	}
	paths := coding.PatchPaths(patch.Patch)
	if len(patch.Files) > 0 {
		paths = coding.WorkerFileEditPaths(patch.Files)
	}
	allowedPaths := taskAllowedPaths(task)
	if err := coding.ValidatePatchPaths(paths, allowedPaths, task.ForbiddenPaths); err != nil {
		message := formatWorkerPathRejection(paths, allowedPaths, task.ForbiddenPaths, err)
		payload, _ := json.Marshal(struct {
			Paths         []string `json:"paths"`
			AllowedPaths  []string `json:"allowed_paths"`
			ForbiddenPath []string `json:"forbidden_paths,omitempty"`
			Error         string   `json:"error"`
		}{
			Paths:         paths,
			AllowedPaths:  allowedPaths,
			ForbiddenPath: task.ForbiddenPaths,
			Error:         err.Error(),
		})
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "worker_rejected",
			Message: message,
			Payload: payload,
		})
		m.writeRunArtifact("worker_rejected", struct {
			TaskID        string   `json:"task_id"`
			Paths         []string `json:"paths"`
			AllowedPaths  []string `json:"allowed_paths"`
			ForbiddenPath []string `json:"forbidden_paths,omitempty"`
			Error         string   `json:"error"`
		}{TaskID: task.ID, Paths: paths, AllowedPaths: allowedPaths, ForbiddenPath: task.ForbiddenPaths, Error: err.Error()})
		m.addSystemNote(message)
		m.status = "worker patch rejected"
		return m, nil, true
	}
	if rewrites := coding.DetectSuspiciousFileRewrites(m.project.Root, patch.Files); len(rewrites) > 0 {
		message := formatSuspiciousRewriteRejection(rewrites)
		payload, _ := json.Marshal(rewrites)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "worker_rejected",
			Message: message,
			Payload: payload,
		})
		m.writeRunArtifact("worker_rejected", struct {
			TaskID   string                     `json:"task_id"`
			Rewrites []coding.SuspiciousRewrite `json:"rewrites"`
		}{TaskID: task.ID, Rewrites: rewrites})
		m.addSystemNote(message)
		m.status = "worker patch rejected"
		return m, nil, true
	}
	result, err := applyWorkerPatchContent(m.project.Root, patch)
	if err != nil {
		applyErrors = append(applyErrors, err)
		if len(patch.Files) == 0 && repairInvalidDiff && repairAttempts < maxWorkerPatchDiffRepairAttempts {
			ctx, cancel := context.WithTimeout(context.Background(), m.workerRequestTimeout())
			defer cancel()
			repaired, repairErr := m.repairWorkerPatchDiff(ctx, task, patch, err)
			if repairErr == nil {
				return m.applyWorkerPatchWithRepair(repaired, true, repairAttempts+1, applyErrors, nil)
			}
			message := formatWorkerPatchApplyFailure(applyErrors, repairErr)
			return m.blockTaskAfterWorkerApplyFailure(task, message)
		}
		message := formatWorkerPatchApplyFailure(applyErrors, nil)
		return m.blockTaskAfterWorkerApplyFailure(task, message)
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
	m.writeRunArtifact("worker_output", struct {
		TaskID string                  `json:"task_id"`
		Patch  coding.WorkerPatch      `json:"patch"`
		Result coding.PatchApplyResult `json:"result"`
		Paths  []string                `json:"paths"`
	}{TaskID: task.ID, Patch: patch, Result: result, Paths: result.Paths})
	if diff, err := m.gitDiff(); err == nil {
		m.writeRunArtifact("diff", struct {
			TaskID string `json:"task_id"`
			Diff   string `json:"diff"`
		}{TaskID: task.ID, Diff: diff})
	}
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
		m.writeRunArtifact("verification_error", struct {
			TaskID string `json:"task_id"`
			Error  string `json:"error"`
		}{TaskID: task.ID, Error: err.Error()})
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
		m.writeRunArtifact("verification", struct {
			TaskID       string               `json:"task_id"`
			Verification []verificationResult `json:"verification"`
		}{TaskID: task.ID, Verification: verification})
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

func applyWorkerPatchContent(projectRoot string, patch coding.WorkerPatch) (coding.PatchApplyResult, error) {
	if len(patch.Files) > 0 {
		return coding.ApplyFileEdits(projectRoot, patch.Files)
	}
	return coding.ApplyPatch(projectRoot, patch.Patch)
}

func (m model) blockTaskAfterWorkerApplyFailure(task coding.Task, message string) (tea.Model, tea.Cmd, bool) {
	events, _ := m.store.TaskEvents(task.ID)
	if err := m.restoreTaskBaseline(task, events, "worker apply failure"); err != nil {
		m.addSystemNote("Worker cleanup error: " + err.Error())
		m.status = "worker cleanup failed"
		return m, nil, true
	}
	if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
		m.addSystemNote("Worker patch error: " + err.Error())
		m.status = "worker patch failed"
		return m, nil, true
	}
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "worker_error",
		Message: message,
	})
	m.addSystemNote(message)
	m.status = "worker patch apply failed"
	return m, m.notificationCmd("task_blocked", task.Title, message), true
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
	m.thinking = true
	m.working.Spinner = spinner.Jump
	m.status = "running worker"
	m.streamAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), m.workerRequestTimeout())
	m.modelRunID++
	runID := m.modelRunID
	m.ensureWorkerRunMaps()
	m.workerRuns[runID] = task.ID
	m.cancelWorkerRuns[runID] = cancel
	return m, tea.Batch(m.runWorkerModelCmd(ctx, runID, packet), m.working.Tick), true
}

func (m model) runWorkerModelCmd(ctx context.Context, runID int, packet coding.TaskPacket) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		raw, usage, err := m.generateWorkerPatchJSON(ctx, packet)
		telemetry := m.modelTelemetry("worker", time.Since(start), len(raw), 0, usage)
		if err != nil {
			return workerRunMsg{runID: runID, telemetry: telemetry, err: err}
		}
		patch, repairAttempts, err := m.workerPatchFromGeneratedJSON(ctx, packet, raw)
		telemetry.JSONRepairAttempts = repairAttempts
		if err != nil {
			return workerRunMsg{runID: runID, raw: raw, telemetry: telemetry, err: fmt.Errorf("%w\n\nRaw response:\n%s", err, raw)}
		}
		return workerRunMsg{runID: runID, raw: raw, patch: patch, telemetry: telemetry}
	}
}

func (m model) modelTelemetry(role string, latency time.Duration, rawChars, repairAttempts int, usage llm.Usage) modelTelemetry {
	providerName := m.providerNameForRole(role)
	provider := m.cfg.ProviderForRole(role)
	return modelTelemetry{
		Role:               role,
		Provider:           providerName,
		Model:              provider.Model,
		LatencyMS:          latency.Milliseconds(),
		RawChars:           rawChars,
		InputTokens:        usage.InputTokens,
		OutputTokens:       usage.OutputTokens,
		JSONRepairAttempts: repairAttempts,
	}
}

func (m *model) ensureWorkerRunMaps() {
	if m.workerRuns == nil {
		m.workerRuns = map[int]string{}
	}
	if m.cancelWorkerRuns == nil {
		m.cancelWorkerRuns = map[int]context.CancelFunc{}
	}
}

func (m model) workerConcurrency() int {
	if m.cfg.Workers.Concurrency <= 0 {
		return 1
	}
	return m.cfg.Workers.Concurrency
}

func (m model) workerRequestTimeout() time.Duration {
	seconds := m.cfg.Workers.RequestTimeoutSeconds
	if seconds <= 0 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

type workerCapacityProfile struct {
	Label        string
	SizeBillions float64
	Instruction  string
}

func (m model) workerCapacityProfile() workerCapacityProfile {
	worker := m.cfg.ProviderForRole("worker")
	modelName := strings.TrimSpace(worker.Model)
	size := inferModelSizeBillions(modelName)
	switch {
	case size > 0 && size <= 10:
		return workerCapacityProfile{
			Label:        fmt.Sprintf("small %.1fB-class local worker", size),
			SizeBillions: size,
			Instruction:  "Assume the worker has limited reasoning and output budget. Split work into tiny, concrete tasks with narrow allowed_paths, minimal context_files, and single-purpose acceptance checks. Prefer create-only module tasks plus a later wiring task for shared files.",
		}
	case size > 0 && size <= 16:
		return workerCapacityProfile{
			Label:        fmt.Sprintf("mid %.1fB-class local worker", size),
			SizeBillions: size,
			Instruction:  "Keep worker tasks compact and bounded. Avoid broad rewrites, keep allowed_paths narrow, and separate planning/design choices from implementation tasks.",
		}
	case size > 0:
		return workerCapacityProfile{
			Label:        fmt.Sprintf("large %.1fB-class local worker", size),
			SizeBillions: size,
			Instruction:  "The worker can handle larger packets than small local models, but tasks must still be bounded, reviewable, and constrained to explicit paths.",
		}
	default:
		return workerCapacityProfile{
			Label:       "unknown-size local worker",
			Instruction: "Worker size could not be inferred from the configured model name. Plan conservatively as if the worker is small: narrow files, small patches, explicit acceptance checks, and no broad rewrites.",
		}
	}
}

func inferModelSizeBillions(modelName string) float64 {
	match := modelSizePattern.FindStringSubmatch(modelName)
	if len(match) != 2 {
		return 0
	}
	size, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0
	}
	return size
}

func (m model) providerNameForRole(role string) string {
	switch role {
	case "orchestrator":
		return emptyFallback(m.cfg.ModelRoles.Orchestrator, m.cfg.ActiveProvider)
	case "worker":
		return emptyFallback(m.cfg.ModelRoles.Worker, m.cfg.ActiveProvider)
	case "reviewer":
		return emptyFallback(m.cfg.ModelRoles.Reviewer, m.cfg.ActiveProvider)
	case "summarizer":
		return emptyFallback(m.cfg.ModelRoles.Summarizer, m.cfg.ActiveProvider)
	default:
		return m.cfg.ActiveProvider
	}
}

func (m model) generateWorkerPatchJSON(ctx context.Context, packet coding.TaskPacket) (string, llm.Usage, error) {
	client := llm.New(m.cfg.ProviderForRole("worker"))
	return client.CompleteWithUsage(ctx, workerPatchMessages(packet), 4096)
}

func (m model) repairWorkerPatchJSON(ctx context.Context, packet coding.TaskPacket, raw string, parseErr error) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("worker"))
	return client.Complete(ctx, workerPatchRepairMessages(packet, raw, parseErr), 4096)
}

func (m model) workerPatchFromGeneratedJSON(ctx context.Context, packet coding.TaskPacket, raw string) (coding.WorkerPatch, int, error) {
	patch, err := coding.ParseWorkerPatchJSON([]byte(extractJSONObject(raw)))
	if err == nil {
		return patch, 0, nil
	}
	initialErr := err
	repaired, repairErr := m.repairWorkerPatchJSON(ctx, packet, raw, initialErr)
	if repairErr != nil {
		return coding.WorkerPatch{}, 1, fmt.Errorf("%v; repair error: %w", initialErr, repairErr)
	}
	patch, err = coding.ParseWorkerPatchJSON([]byte(extractJSONObject(repaired)))
	if err != nil {
		return coding.WorkerPatch{}, 1, fmt.Errorf("%v; repair parse error: %w", initialErr, err)
	}
	return patch, 1, nil
}

func (m model) repairWorkerPatchDiff(ctx context.Context, task coding.Task, patch coding.WorkerPatch, applyErr error) (coding.WorkerPatch, error) {
	packet, err := m.buildWorkerPacketForRun(task)
	if err != nil {
		return coding.WorkerPatch{}, err
	}
	client := llm.New(m.cfg.ProviderForRole("worker"))
	raw, err := client.Complete(ctx, workerPatchDiffRepairMessages(packet, patch, applyErr), 4096)
	if err != nil {
		return coding.WorkerPatch{}, err
	}
	repaired, err := coding.ParseWorkerPatchJSON([]byte(extractJSONObject(raw)))
	if err != nil {
		return coding.WorkerPatch{}, err
	}
	if repaired.TaskID != patch.TaskID {
		return coding.WorkerPatch{}, fmt.Errorf("repaired patch task_id %q does not match %q", repaired.TaskID, patch.TaskID)
	}
	return repaired, nil
}

func formatWorkerPatchApplyFailure(applyErrors []error, repairErr error) string {
	var b strings.Builder
	b.WriteString("Worker patch apply error:")
	for i, err := range applyErrors {
		fmt.Fprintf(&b, "\n%d. %s", i+1, err)
	}
	if repairErr != nil {
		fmt.Fprintf(&b, "\n\nPatch repair error: %s", repairErr)
	}
	return b.String()
}

func workerPatchMessages(packet coding.TaskPacket) []llm.ChatMessage {
	profile := workerCapacityProfileForPacket(packet)
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You are the WeazlCode local worker.",
				profile,
				"Return a WorkerPatch JSON object.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"For small file edits, prefer files with full replacement content and leave patch empty.",
				"For larger edits, return a unified diff in patch and leave files empty.",
				"If you need missing context or cannot safely complete the task, set blocker and leave patch/files empty.",
				"If the task packet includes Repair focus, make only the focused repair requested by the reviewer and keep the original allowed_paths scope.",
				"Unified diffs must be valid for git apply: include diff --git, ---/+++ file headers, @@ hunk headers with correct line counts, and unchanged context lines.",
				"Do not edit outside allowed_paths. Do not include prose outside JSON.",
			}, "\n"),
		},
		{
			Role:    "user",
			Content: "Task packet:\n" + renderJSON(packet),
		},
	}
}

func workerCapacityProfileForPacket(packet coding.TaskPacket) string {
	if strings.TrimSpace(packet.WorkerProfile) != "" {
		return packet.WorkerProfile
	}
	if strings.Contains(strings.ToLower(packet.Goal), "repair focus:") {
		return "This is a constrained repair pass. Make the smallest possible focused change."
	}
	return "Assume you are a local coding worker with limited reasoning and output budget. Complete only this bounded task, keep changes small, and return blocker if the packet is too broad or missing context."
}

func workerPatchDiffRepairMessages(packet coding.TaskPacket, patch coding.WorkerPatch, applyErr error) []llm.ChatMessage {
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You repair invalid unified diffs for WeazlCode WorkerPatch JSON.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"Keep the same task_id.",
				"Prefer files with full replacement content for small edits; otherwise return a corrected unified diff in patch.",
				"Set blocker if you cannot safely repair it.",
				"Unified diffs must be valid for git apply: include diff --git, ---/+++ file headers, @@ hunk headers with correct line counts, and unchanged context lines.",
				"Do not include prose outside JSON.",
			}, "\n"),
		},
		{
			Role: "user",
			Content: fmt.Sprintf("Task packet:\n%s\n\nApply error:\n%s\n\nInvalid WorkerPatch:\n%s",
				renderJSON(packet),
				applyErr,
				renderJSON(patch),
			),
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
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"Use the task_id from the task packet.",
				"For small file edits, prefer files with full replacement content and leave patch empty.",
				"If a safe patch is not possible, set blocker and leave patch/files empty.",
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
	commands := m.taskVerification(task)
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
	skills, err := m.skillContexts(task.Skills)
	if err != nil {
		return coding.TaskPacket{}, err
	}
	packet, err := coding.BuildTaskPacket(task, coding.ContextPackOptions{
		ProjectRoot: m.project.Root,
		MaxFileChars: workerContextFileCharBudget(
			m.cfg.ProviderForRole("worker").ContextWindow,
		),
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
	packet.WorkerProfile = profile.Label + ": " + profile.Instruction
	return packet, nil
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
		if workerErr, retry := latestWorkerError(events); retry {
			packet.Goal = strings.TrimSpace(packet.Goal + "\n\nRetry note:\nPrevious worker attempt failed before producing a patch: " + workerErr)
			return packet, nil
		}
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
		if retryableWorkerErrorTask(events) {
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
	return task.AllowedPaths
}

func formatWorkerPathRejection(paths, allowed, forbidden []string, err error) string {
	var b strings.Builder
	b.WriteString("Worker patch rejected: returned paths are outside the task scope.")
	if len(paths) > 0 {
		fmt.Fprintf(&b, "\nReturned paths:\n%s", bulletList(paths))
	}
	if len(allowed) > 0 {
		fmt.Fprintf(&b, "\nAllowed paths:\n%s", bulletList(allowed))
	} else {
		b.WriteString("\nAllowed paths: none")
	}
	if len(forbidden) > 0 {
		fmt.Fprintf(&b, "\nForbidden paths:\n%s", bulletList(forbidden))
	}
	if err != nil {
		fmt.Fprintf(&b, "\nError: %s", err)
	}
	return b.String()
}

func formatSuspiciousRewriteRejection(rewrites []coding.SuspiciousRewrite) string {
	var b strings.Builder
	b.WriteString("Worker patch rejected: suspicious full-file rewrite detected.")
	for _, rewrite := range rewrites {
		fmt.Fprintf(&b, "\n- %s: %s (%d old lines, %d new lines, %d%% common lines)",
			rewrite.Path,
			rewrite.Reason,
			rewrite.OldLineCount,
			rewrite.NewLineCount,
			rewrite.CommonLinePct,
		)
	}
	b.WriteString("\nUse a focused unified diff or a smaller file edit for the requested change.")
	return b.String()
}

func (m model) taskVerification(task coding.Task) []string {
	if len(task.Verification) == 0 {
		return m.defaultVerificationCommands()
	}
	return task.Verification
}

func (m model) defaultVerificationCommands() []string {
	commands := project.DiscoverCommands(m.project.Root)
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		if allowlistedVerificationCommand(command) {
			out = append(out, strings.TrimSpace(command))
		}
	}
	return out
}

func normalizePlanVerification(plan *coding.Plan) {
	for i := range plan.Tasks {
		plan.Tasks[i].Verification = filterVerificationCommands(plan.Tasks[i].Verification)
	}
}

func filterVerificationCommands(commands []string) []string {
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		if allowlistedVerificationCommand(command) {
			out = append(out, strings.TrimSpace(command))
		}
	}
	return out
}

func allowlistedVerificationCommand(commandText string) bool {
	name, args, err := splitVerificationCommand(commandText)
	if err != nil {
		return false
	}
	switch name {
	case "go":
		return len(args) > 0 && (args[0] == "test" || args[0] == "build" || args[0] == "vet")
	case "npm":
		return len(args) > 0 && (args[0] == "test" || args[0] == "run")
	case "python", "python3":
		if len(args) < 2 || args[0] != "-m" {
			return false
		}
		return args[1] == "pytest" || args[1] == "unittest" || args[1] == "compileall"
	case "pytest", "shellcheck":
		return true
	case "cargo":
		return len(args) > 0 && (args[0] == "test" || args[0] == "build" || args[0] == "check" || args[0] == "clippy")
	case "make":
		return len(args) > 0 && (args[0] == "test" || args[0] == "check" || args[0] == "lint" || args[0] == "build")
	default:
		return false
	}
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

func (m model) reviewDiffCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Review diff error: " + err.Error()
	}
	if !ok {
		return "Review diff:\nNo plan found."
	}
	task, ok := firstReviewingTask(plan.Tasks)
	if !ok {
		return "Review diff:\nNo reviewing task found. Run a worker patch first."
	}
	events, eventsErr := m.store.TaskEvents(task.ID)
	diff, diffErr := m.taskReviewDiff(task, events)
	paths := coding.PatchPaths(diff)
	var b strings.Builder
	fmt.Fprintf(&b, "Review diff: %s\nstatus: %s\nplan: %s\n\nGoal:\n%s\n", task.Title, task.Status, plan.Title, task.Goal)
	if diffErr != nil {
		fmt.Fprintf(&b, "\nChanged files error: %s\n", diffErr)
	} else if len(paths) > 0 {
		fmt.Fprintf(&b, "\nChanged files:\n%s", bulletList(paths))
	} else {
		b.WriteString("\nChanged files:\nNo task-scoped changes.\n")
	}
	if eventsErr != nil {
		fmt.Fprintf(&b, "\nEvents error: %s\n", eventsErr)
	}
	if len(events) > 0 {
		fmt.Fprintf(&b, "\nEvents:\n%s\n", taskEventsSummary(events))
	}
	b.WriteString("\nReview commands:\n")
	b.WriteString("- /review approve [summary]\n")
	b.WriteString("- /review needs-fix <issue>[;; issue]\n")
	b.WriteString("- /review blocked <summary>\n")
	if diffErr != nil {
		fmt.Fprintf(&b, "\nDiff error: %s", diffErr)
	} else {
		fmt.Fprintf(&b, "\nTask-scoped diff:\n%s", emptyFallback(diff, "No diff."))
	}
	return strings.TrimRight(b.String(), "\n")
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
	events, err := m.store.TaskEvents(task.ID)
	if err != nil {
		return coding.ReviewerInput{}, err
	}
	diff, err := m.taskReviewDiff(task, events)
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

func (m model) runReviewerModel() (tea.Model, tea.Cmd, bool) {
	input, err := m.buildReviewerInput()
	if err != nil {
		m.addSystemNote("Reviewer run error: " + err.Error())
		m.status = "reviewer run failed"
		return m, nil, true
	}
	m.thinking = true
	m.working.Spinner = spinner.Jump
	m.status = "running reviewer"
	m.streamAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), m.reviewerRequestTimeout())
	m.modelRunID++
	m.activeModelRunID = m.modelRunID
	m.cancelModel = cancel
	return m, tea.Batch(m.runReviewerModelCmd(ctx, m.activeModelRunID, input), m.working.Tick), true
}

func (m model) reviewerRequestTimeout() time.Duration {
	timeout := m.workerRequestTimeout()
	if timeout > 2*time.Minute {
		return 2 * time.Minute
	}
	return timeout
}

func (m model) runReviewerModelCmd(ctx context.Context, runID int, input coding.ReviewerInput) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		raw, usage, err := m.generateReviewVerdictJSON(ctx, input)
		telemetry := m.modelTelemetry("reviewer", time.Since(start), len(raw), 0, usage)
		if err != nil {
			return reviewerRunMsg{runID: runID, raw: raw, telemetry: telemetry, err: err}
		}
		verdict, err := coding.ParseReviewVerdictJSON([]byte(extractJSONObject(raw)))
		if err != nil {
			return reviewerRunMsg{runID: runID, raw: raw, telemetry: telemetry, err: fmt.Errorf("%w\n\nRaw response:\n%s", err, raw)}
		}
		return reviewerRunMsg{runID: runID, raw: raw, verdict: verdict, telemetry: telemetry}
	}
}

func (m model) generateReviewVerdictJSON(ctx context.Context, input coding.ReviewerInput) (string, llm.Usage, error) {
	client := llm.NewWithTimeout(m.cfg.ProviderForRole("reviewer"), m.reviewerRequestTimeout())
	return client.CompleteWithUsage(ctx, reviewerVerdictMessages(input), 2048)
}

func (m model) recordReviewerModelError(err error, telemetry *modelTelemetry) {
	plan, ok, loadErr := m.store.LatestPlan(m.session.ID)
	if loadErr != nil || !ok {
		return
	}
	task, ok := firstReviewingTask(plan.Tasks)
	if !ok {
		return
	}
	if telemetry != nil {
		payload, _ := json.Marshal(telemetry)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "reviewer_model",
			Message: fmt.Sprintf("%s/%s in %dms", telemetry.Provider, telemetry.Model, telemetry.LatencyMS),
			Payload: payload,
		})
	}
	eventType := "reviewer_error"
	if strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		eventType = "reviewer_timeout"
	}
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    eventType,
		Message: err.Error(),
	})
}

func reviewerVerdictMessages(input coding.ReviewerInput) []llm.ChatMessage {
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You are the WeazlCode frontier reviewer.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"verdict\":\"approve|needs_fix|blocked\",\"summary\":\"...\",\"issues\":[\"...\"]}.",
				"Approve only when the diff satisfies the task packet, allowed paths, verification output, and acceptance checks.",
				"Mechanically compare the task goal, allowed paths, diff paths, verification output, and each acceptance check before approving.",
				"If the diff is empty, unrelated, outside allowed paths, missing expected verification, or only plausibly related, use needs_fix with concrete issues.",
				"Use needs_fix for focused repairable issues. Use blocked only for missing context or user decisions.",
			}, "\n"),
		},
		{
			Role:    "user",
			Content: "Reviewer input:\n" + renderJSON(input),
		},
	}
}

func (m model) importReviewVerdict(raw string) (tea.Model, tea.Cmd, bool) {
	if raw == "" {
		m.addSystemNote("Usage: /review <json|approve|needs-fix|blocked>")
		m.status = "review usage"
		return m, nil, true
	}
	verdict, err := parseReviewCommand(raw)
	if err != nil {
		m.addSystemNote("Review import error: " + err.Error())
		m.status = "review failed"
		return m, nil, true
	}
	return m.applyReviewVerdict(verdict, nil)
}

func (m model) applyReviewVerdict(verdict coding.ReviewVerdict, telemetry *modelTelemetry) (tea.Model, tea.Cmd, bool) {
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
	if telemetry != nil {
		payload, _ := json.Marshal(telemetry)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "reviewer_model",
			Message: fmt.Sprintf("%s/%s in %dms", telemetry.Provider, telemetry.Model, telemetry.LatencyMS),
			Payload: payload,
		})
	}
	if verdict.Verdict == coding.ReviewApprove {
		if issues := m.reviewApprovalIssues(task); len(issues) > 0 {
			guardrail := coding.ReviewVerdict{
				Verdict: coding.ReviewNeedsFix,
				Summary: "Approval blocked by local review guardrails.",
				Issues:  issues,
			}
			payload, _ := json.Marshal(guardrail)
			_, _ = m.store.AddTaskEvent(coding.TaskEvent{
				TaskID:  task.ID,
				Type:    "review_guardrail",
				Message: guardrail.Summary,
				Payload: payload,
			})
			m.writeRunArtifact("review_guardrail", struct {
				TaskID  string               `json:"task_id"`
				Verdict coding.ReviewVerdict `json:"verdict"`
			}{TaskID: task.ID, Verdict: guardrail})
			m.addSystemNote("Reviewer approval blocked by local guardrails:\n" + renderJSON(guardrail))
			m.status = "review guardrail"
			return m, nil, true
		}
	}
	payload, _ := json.Marshal(verdict)
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "reviewer_verdict",
		Message: verdict.Summary,
		Payload: payload,
	})
	m.writeRunArtifact("review", struct {
		TaskID  string               `json:"task_id"`
		Verdict coding.ReviewVerdict `json:"verdict"`
	}{TaskID: task.ID, Verdict: verdict})
	var notify tea.Cmd
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
		m.runHooks("task_done", map[string]any{
			"task_id": task.ID,
			"title":   task.Title,
			"goal":    task.Goal,
			"plan_id": plan.ID,
			"summary": verdict.Summary,
		})
		notify = m.notificationCmd("task_done", task.Title, verdict.Summary)
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
		if err := m.restoreTaskBaseline(task, events, "review needs fix"); err != nil {
			m.addSystemNote("Review cleanup error: " + err.Error())
			m.status = "review cleanup failed"
			return m, nil, true
		}
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
			return m, m.notificationCmd("task_blocked", task.Title, verdict.Summary), true
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
		m.writeRunArtifact("repair_request", struct {
			TaskID  string               `json:"task_id"`
			Attempt int                  `json:"attempt"`
			Verdict coding.ReviewVerdict `json:"verdict"`
		}{TaskID: task.ID, Attempt: attempt, Verdict: verdict})
		notify = m.notificationCmd("repair_requested", task.Title, verdict.Summary)
		m.addSystemNote("Reviewer requested focused repair:\n" + renderJSON(verdict))
		m.status = "repair requested"
	case coding.ReviewBlocked:
		events, _ := m.store.TaskEvents(task.ID)
		if err := m.restoreTaskBaseline(task, events, "review blocked"); err != nil {
			m.addSystemNote("Review cleanup error: " + err.Error())
			m.status = "review cleanup failed"
			return m, nil, true
		}
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Review error: " + err.Error())
			m.status = "review failed"
			return m, nil, true
		}
		notify = m.notificationCmd("task_blocked", task.Title, verdict.Summary)
		m.addSystemNote("Reviewer blocked task:\n" + renderJSON(verdict))
		m.status = "review blocked"
	}
	return m, notify, true
}

func (m model) reviewApprovalIssues(task coding.Task) []string {
	var issues []string
	events, err := m.store.TaskEvents(task.ID)
	if err != nil {
		issues = append(issues, "could not read task events: "+err.Error())
	}
	diff, err := m.taskReviewDiff(task, events)
	if err != nil {
		issues = append(issues, "could not read git diff: "+err.Error())
	} else {
		paths := coding.PatchPaths(diff)
		if len(paths) == 0 {
			issues = append(issues, "diff is empty; there is nothing to approve")
		} else if err := coding.ValidatePatchPaths(paths, taskAllowedPaths(task), task.ForbiddenPaths); err != nil {
			issues = append(issues, "diff paths do not match task scope: "+err.Error())
		}
	}
	if len(task.AcceptanceChecks) == 0 {
		issues = append(issues, "task has no acceptance checks to review")
	}
	verification := m.taskVerification(task)
	if len(verification) > 0 {
		if latestVerificationFailed(events) {
			issues = append(issues, "latest verification failed")
		} else if !latestVerificationPassed(events) {
			issues = append(issues, "verification was expected but no passing verification event was recorded")
		}
	}
	return issues
}

func latestVerificationPassed(events []coding.TaskEvent) bool {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "verification":
			return true
		case "verification_error":
			return false
		}
	}
	return false
}

func latestVerificationFailed(events []coding.TaskEvent) bool {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "verification_error":
			return true
		case "verification":
			return false
		}
	}
	return false
}

func parseReviewCommand(raw string) (coding.ReviewVerdict, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") {
		return coding.ParseReviewVerdictJSON([]byte(raw))
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return coding.ReviewVerdict{}, fmt.Errorf("review command is empty")
	}
	command := strings.ReplaceAll(strings.ToLower(fields[0]), "-", "_")
	detail := strings.TrimSpace(strings.TrimPrefix(raw, fields[0]))
	switch command {
	case "approve", "approved":
		if detail == "" {
			detail = "Approved."
		}
		return coding.ReviewVerdict{Verdict: coding.ReviewApprove, Summary: detail}, nil
	case "needs_fix", "fix", "needsfix":
		if detail == "" {
			return coding.ReviewVerdict{}, fmt.Errorf("needs-fix requires at least one issue")
		}
		issues := splitPlanEditList(detail)
		return coding.ReviewVerdict{Verdict: coding.ReviewNeedsFix, Summary: strings.Join(issues, "; "), Issues: issues}, nil
	case "blocked", "block":
		if detail == "" {
			detail = "Blocked."
		}
		return coding.ReviewVerdict{Verdict: coding.ReviewBlocked, Summary: detail}, nil
	default:
		return coding.ReviewVerdict{}, fmt.Errorf("unknown review command %q", fields[0])
	}
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

func parallelRunnableTasks(tasks []coding.Task, limit int) []coding.Task {
	tasks, _ = parallelRunnableTasksWithRetry(tasks, limit, nil)
	return tasks
}

func (m model) parallelRunnableTasks(tasks []coding.Task, limit int) ([]coding.Task, error) {
	return parallelRunnableTasksWithRetry(tasks, limit, func(task coding.Task) (bool, error) {
		if task.Status != coding.TaskStatusBlocked {
			return false, nil
		}
		events, err := m.store.TaskEvents(task.ID)
		if err != nil {
			return false, err
		}
		return retryableWorkerErrorTask(events) || repairableTask(events), nil
	})
}

func parallelRunnableTasksWithRetry(tasks []coding.Task, limit int, retryable func(coding.Task) (bool, error)) ([]coding.Task, error) {
	if limit <= 0 {
		return nil, nil
	}
	done := map[string]bool{}
	activeOrSelected := map[string]bool{}
	for _, task := range tasks {
		if task.Status == coding.TaskStatusDone {
			done[task.ID] = true
		}
		if task.Status == coding.TaskStatusRunning || task.Status == coding.TaskStatusReviewing {
			for _, path := range task.AllowedPaths {
				activeOrSelected[normalizeTaskPathForOverlap(path)] = true
			}
		}
	}
	selected := make([]coding.Task, 0, limit)
	for _, task := range tasks {
		if task.Status != coding.TaskStatusPending {
			if retryable == nil {
				continue
			}
			ok, err := retryable(task)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		if len(task.AllowedPaths) == 0 {
			continue
		}
		if !dependenciesDone(task, done) {
			continue
		}
		if pathsOverlapAny(task.AllowedPaths, activeOrSelected) {
			continue
		}
		selected = append(selected, task)
		for _, path := range task.AllowedPaths {
			activeOrSelected[normalizeTaskPathForOverlap(path)] = true
		}
		if len(selected) >= limit {
			break
		}
	}
	return selected, nil
}

func dependenciesDone(task coding.Task, done map[string]bool) bool {
	for _, dep := range task.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if !done[dep] {
			return false
		}
	}
	return true
}

func pathsOverlapAny(paths []string, selected map[string]bool) bool {
	for _, path := range paths {
		path = normalizeTaskPathForOverlap(path)
		if path == "" {
			continue
		}
		for selectedPath := range selected {
			if taskPathsOverlap(path, selectedPath) {
				return true
			}
		}
	}
	return false
}

func taskPathsOverlap(a, b string) bool {
	if a == "." || b == "." || a == b {
		return true
	}
	return strings.HasPrefix(a+"/", b+"/") || strings.HasPrefix(b+"/", a+"/")
}

func normalizeTaskPathForOverlap(path string) string {
	path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if path == "/" || path == "" {
		return "."
	}
	return strings.TrimPrefix(path, "./")
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

func retryableWorkerErrorTask(events []coding.TaskEvent) bool {
	_, ok := latestWorkerError(events)
	return ok
}

func latestWorkerError(events []coding.TaskEvent) (string, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "worker_error", "worker_timeout", "worker_json_error":
			return strings.TrimSpace(events[i].Message), true
		case "worker_model", "worker_patch", "worker_blocker", "repair_start", "repair_limit", "reviewer_verdict":
			return "", false
		}
	}
	return "", false
}

func classifyWorkerRunError(err error) (eventType, status, note string) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "context deadline exceeded") || strings.Contains(text, "timeout") || strings.Contains(text, "deadline exceeded"):
		return "worker_timeout", "worker provider timeout", "Worker provider timeout"
	case strings.Contains(text, "parse") || strings.Contains(text, "json") || strings.Contains(text, "raw response"):
		return "worker_json_error", "worker json failed", "Worker returned malformed patch JSON"
	default:
		return "worker_error", "worker run failed", "Worker model error"
	}
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

func (m model) writeRunArtifact(kind string, payload any) {
	if strings.TrimSpace(m.project.StateDir) == "" || strings.TrimSpace(m.session.ID) == "" {
		return
	}
	name := time.Now().Format("20060102-150405.000000000") + "-" + safeArtifactName(kind) + ".json"
	dir := filepath.Join(m.project.StateDir, "runs", safeArtifactName(m.session.ID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	body, err := json.MarshalIndent(struct {
		Kind      string `json:"kind"`
		SessionID string `json:"session_id"`
		CreatedAt string `json:"created_at"`
		Payload   any    `json:"payload"`
	}{
		Kind:      kind,
		SessionID: m.session.ID,
		CreatedAt: time.Now().Format(time.RFC3339Nano),
		Payload:   payload,
	}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, name), append(body, '\n'), 0o600)
}

func safeArtifactName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "artifact"
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "artifact"
	}
	return out
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
	if len(args) > 0 && strings.ToLower(args[0]) == "replan" {
		guidance := strings.TrimSpace(strings.TrimPrefix(rawArgs, args[0]))
		return m.replanCommand(guidance)
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "edit" {
		return m.editPlanCommand(strings.TrimSpace(strings.TrimPrefix(rawArgs, args[0])))
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "validate" {
		m.setIDEView("plan validation", m.planValidationCommandText())
		return m, nil, true
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
		m.writeRunArtifact("plan", plan)
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
		normalizePlanVerification(&plan)
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
		m.writeRunArtifact("plan", plan)
		m.addSystemNote(renderPlan(plan))
		m.status = "plan imported"
		return m, nil, true
	}
	m.setIDEView("plan", m.planCommandText())
	return m, nil, true
}

func (m model) editPlanCommand(raw string) (tea.Model, tea.Cmd, bool) {
	taskSelector, field, value, ok := splitPlanEditArgs(raw)
	if !ok {
		m.addSystemNote("Usage: /plan edit <task> <field> <value>\nFields: title, goal, allowed_paths, forbidden_paths, context_files, skills, depends_on, verification, checks.")
		m.status = "plan edit usage"
		return m, nil, true
	}
	plan, found, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Plan edit error: " + err.Error())
		m.status = "plan edit failed"
		return m, nil, true
	}
	if !found {
		m.addSystemNote("No plan. Use `/plan draft`, `/plan generate`, or `/plan import` first.")
		m.status = "no plan"
		return m, nil, true
	}
	if plan.Status != coding.PlanStatusDraft {
		m.addSystemNote("Only draft plans can be edited. Use `/plan` to inspect the current status.")
		m.status = "plan edit blocked"
		return m, nil, true
	}
	_, index, ok := selectTask(plan.Tasks, taskSelector)
	if !ok {
		m.addSystemNote(fmt.Sprintf("Task %q not found. Use `/tasks` to list task numbers and ids.", taskSelector))
		m.status = "task not found"
		return m, nil, true
	}
	if err := applyPlanTaskEdit(&plan.Tasks[index], field, value); err != nil {
		m.addSystemNote("Plan edit error: " + err.Error())
		m.status = "plan edit failed"
		return m, nil, true
	}
	if err := coding.ValidatePlan(plan); err != nil {
		m.addSystemNote("Plan edit validation error: " + err.Error())
		m.status = "plan edit failed"
		return m, nil, true
	}
	if err := m.store.SavePlan(plan); err != nil {
		m.addSystemNote("Plan edit save error: " + err.Error())
		m.status = "plan edit failed"
		return m, nil, true
	}
	if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
		plan = saved
	}
	m.writeRunArtifact("plan", plan)
	m.addSystemNote(renderPlan(plan))
	m.status = "plan edited"
	return m, nil, true
}

func (m model) attachFileCommand(raw string) (tea.Model, tea.Cmd, bool) {
	taskSelector, path, lineRange, ok := splitAttachArgs(raw)
	if !ok {
		m.addSystemNote("Usage: /attach [task] <path> [start-end]")
		m.status = "attach usage"
		return m, nil, true
	}
	clean, err := cleanPreviewPath(path)
	if err != nil {
		m.addSystemNote("Attach error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	}
	if info, err := os.Stat(filepath.Join(m.project.Root, clean)); err != nil {
		m.addSystemNote("Attach error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	} else if info.IsDir() {
		m.addSystemNote("Attach error: path is a directory")
		m.status = "attach failed"
		return m, nil, true
	}
	plan, found, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Attach error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	}
	if !found {
		m.addSystemNote("No plan. Use `/plan draft`, `/plan generate`, or `/plan import` first.")
		m.status = "no plan"
		return m, nil, true
	}
	if plan.Status != coding.PlanStatusDraft {
		m.addSystemNote("Only draft plans can be edited. Use `/plan` to inspect the current status.")
		m.status = "attach blocked"
		return m, nil, true
	}
	_, index, ok := selectTask(plan.Tasks, taskSelector)
	if !ok {
		m.addSystemNote(fmt.Sprintf("Task %q not found. Use `/tasks` to list task numbers and ids.", taskSelector))
		m.status = "task not found"
		return m, nil, true
	}
	spec := clean
	if lineRange != "" {
		spec += "#L" + lineRange
	}
	task := &plan.Tasks[index]
	task.ContextFiles = appendUnique(task.ContextFiles, spec)
	task.AllowedPaths = appendUnique(task.AllowedPaths, clean)
	if err := coding.ValidatePlan(plan); err != nil {
		m.addSystemNote("Attach validation error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	}
	if err := m.store.SavePlan(plan); err != nil {
		m.addSystemNote("Attach save error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	}
	if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
		plan = saved
	}
	m.writeRunArtifact("plan", plan)
	m.addSystemNote(fmt.Sprintf("Attached %s to task %s.", spec, plan.Tasks[index].Title))
	m.status = "file attached"
	return m, nil, true
}

func splitAttachArgs(raw string) (taskSelector, path, lineRange string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", "", "", false
	}
	if len(fields) == 1 {
		return "", fields[0], "", true
	}
	if looksTaskSelector(fields[0]) {
		taskSelector = fields[0]
		path = fields[1]
		if len(fields) > 2 {
			lineRange = normalizeLineRange(fields[2])
		}
		return taskSelector, path, lineRange, path != ""
	}
	path = fields[0]
	lineRange = normalizeLineRange(fields[1])
	return "", path, lineRange, path != ""
}

func looksTaskSelector(raw string) bool {
	if _, err := strconv.Atoi(raw); err == nil {
		return true
	}
	return strings.Contains(raw, "-")
}

func normalizeLineRange(raw string) string {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "L"))
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "-")
	if len(parts) == 2 {
		parts[0] = strings.TrimPrefix(strings.TrimSpace(parts[0]), "L")
		parts[1] = strings.TrimPrefix(strings.TrimSpace(parts[1]), "L")
		return parts[0] + "-L" + parts[1]
	}
	return raw
}

func splitPlanEditArgs(raw string) (taskSelector, field, value string, ok bool) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) < 3 {
		return "", "", "", false
	}
	taskSelector = parts[0]
	field = strings.ToLower(parts[1])
	valueStart := strings.Index(raw, parts[1])
	if valueStart < 0 {
		return "", "", "", false
	}
	value = strings.TrimSpace(raw[valueStart+len(parts[1]):])
	return taskSelector, field, value, value != ""
}

func applyPlanTaskEdit(task *coding.Task, field, value string) error {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(field)), "-", "_") {
	case "title":
		task.Title = strings.TrimSpace(value)
	case "goal":
		task.Goal = strings.TrimSpace(value)
	case "allowed", "allowed_paths", "paths":
		task.AllowedPaths = splitPlanEditList(value)
	case "forbidden", "forbidden_paths":
		task.ForbiddenPaths = splitPlanEditList(value)
	case "context", "context_files":
		task.ContextFiles = splitPlanEditList(value)
	case "skill", "skills":
		task.Skills = splitPlanEditList(value)
	case "depends", "depends_on", "dependencies":
		task.DependsOn = splitPlanEditList(value)
	case "verify", "verification":
		task.Verification = splitPlanEditList(value)
	case "checks", "acceptance", "acceptance_checks":
		items := splitPlanEditList(value)
		checks := make([]coding.AcceptanceCheck, 0, len(items))
		for _, item := range items {
			checks = append(checks, coding.AcceptanceCheck{Description: item})
		}
		task.AcceptanceChecks = checks
	default:
		return fmt.Errorf("unknown field %q", field)
	}
	return nil
}

func appendUnique(items []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return items
	}
	for _, existing := range items {
		if strings.TrimSpace(existing) == item {
			return items
		}
	}
	return append(items, item)
}

func splitPlanEditList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	sep := ","
	if strings.Contains(value, ";;") {
		sep = ";;"
	}
	parts := strings.Split(value, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (m model) generatePlanCommand(request string) (tea.Model, tea.Cmd, bool) {
	if strings.TrimSpace(request) == "" {
		m.addSystemNote("Usage: /plan generate <request>")
		m.status = "plan generate usage"
		return m, nil, true
	}
	m.thinking = true
	m.working.Spinner = spinner.Jump
	m.status = "generating plan"
	m.streamAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	m.modelRunID++
	m.activeModelRunID = m.modelRunID
	m.cancelModel = cancel
	return m, tea.Batch(m.generatePlanCmd(ctx, m.activeModelRunID, request), m.working.Tick), true
}

func (m model) replanCommand(guidance string) (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Plan replan error: " + err.Error())
		m.status = "plan replan failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan. Use `/plan generate <request>` first.")
		m.status = "no plan"
		return m, nil, true
	}
	request, err := m.replanRequest(plan, guidance)
	if err != nil {
		m.addSystemNote("Plan replan error: " + err.Error())
		m.status = "plan replan failed"
		return m, nil, true
	}
	m.thinking = true
	m.working.Spinner = spinner.Jump
	m.status = "replanning"
	m.streamAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	m.modelRunID++
	m.activeModelRunID = m.modelRunID
	m.cancelModel = cancel
	return m, tea.Batch(m.generatePlanCmd(ctx, m.activeModelRunID, request), m.working.Tick), true
}

func (m model) generatePlanCmd(ctx context.Context, runID int, request string) tea.Cmd {
	return func() tea.Msg {
		raw, err := m.generatePlanJSON(ctx, request)
		if err != nil {
			return planGenerateMsg{runID: runID, request: request, err: err}
		}
		plan, err := m.planFromGeneratedJSON(raw)
		if err != nil {
			initialErr := err
			repaired, repairErr := m.repairPlanJSON(ctx, request, raw, initialErr)
			if repairErr != nil {
				return planGenerateMsg{runID: runID, request: request, raw: raw, err: fmt.Errorf("parse error: %v; repair error: %w", initialErr, repairErr)}
			}
			plan, err = m.planFromGeneratedJSON(repaired)
			if err != nil {
				return planGenerateMsg{runID: runID, request: request, raw: raw, err: fmt.Errorf("parse error: %v; repair parse error: %w", initialErr, err)}
			}
		}
		return planGenerateMsg{runID: runID, request: request, raw: raw, plan: plan}
	}
}

func (m model) generatePlanJSON(ctx context.Context, request string) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("orchestrator"))
	return client.Complete(ctx, m.planGenerateMessages(request), maxPlanGenerateTokens)
}

func (m model) repairPlanJSON(ctx context.Context, request, raw string, parseErr error) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("orchestrator"))
	return client.Complete(ctx, m.planRepairMessages(request, raw, parseErr), maxPlanGenerateTokens)
}

func (m model) planGenerateMessages(request string) []llm.ChatMessage {
	instructions, _, _ := project.LoadInstructions(m.project.Root)
	memories, _ := m.store.ProjectMemories(m.project.Root, 10)
	commands := project.DiscoverCommands(m.project.Root)
	skills, _ := project.DiscoverSkills(m.project.Root, m.cfg.Skills.Paths)
	var contextText strings.Builder
	fmt.Fprintf(&contextText, "Project root: %s\n", m.project.Root)
	if len(m.project.Languages) > 0 {
		fmt.Fprintf(&contextText, "Languages: %s\n", strings.Join(m.project.Languages, ", "))
	}
	worker := m.cfg.ProviderForRole("worker")
	workerProfile := m.workerCapacityProfile()
	fmt.Fprintf(&contextText, "\nConfigured worker:\nprovider: %s\nmodel: %s/%s\ncapacity: %s\nplanning constraint: %s\n",
		m.providerNameForRole("worker"),
		worker.Type,
		worker.Model,
		workerProfile.Label,
		workerProfile.Instruction,
	)
	if strings.TrimSpace(instructions.Content) != "" {
		fmt.Fprintf(&contextText, "\nProject instructions:\n%s\n", strings.TrimSpace(instructions.Content))
	}
	if len(commands) > 0 {
		contextText.WriteString("\nDiscovered commands:\n")
		for _, command := range commands {
			fmt.Fprintf(&contextText, "- %s\n", command)
		}
	}
	if files, err := projectFiles(m.project.Root, 80); err == nil && len(files) > 0 {
		contextText.WriteString("\nProject files:\n")
		for _, file := range files {
			fmt.Fprintf(&contextText, "- %s (%d bytes)\n", file.Path, file.Size)
		}
	}
	if previews := m.planReferencedFilePreviews(request); strings.TrimSpace(previews) != "" {
		fmt.Fprintf(&contextText, "\nReferenced file previews:\n%s\n", previews)
	}
	if len(memories) > 0 {
		contextText.WriteString("\nProject memory:\n")
		for _, memory := range memories {
			fmt.Fprintf(&contextText, "- %s: %s\n", memory.Key, memory.Value)
		}
	}
	if m.cfg.Skills.SkillsEnabled() && len(skills) > 0 {
		contextText.WriteString("\nDiscovered skills:\n")
		for _, skill := range skills {
			fmt.Fprintf(&contextText, "- %s", skill.Name)
			if strings.TrimSpace(skill.Description) != "" {
				fmt.Fprintf(&contextText, ": %s", skill.Description)
			}
			fmt.Fprintf(&contextText, " [%s]\n", skill.Source)
		}
	}
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You are the WeazlCode orchestrator.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"title\":\"...\",\"summary\":\"...\",\"tasks\":[{\"id\":\"task-1\",\"title\":\"...\",\"goal\":\"...\",\"allowed_paths\":[\"...\"],\"forbidden_paths\":[\"...\"],\"context_files\":[\"...\"],\"skills\":[\"...\"],\"depends_on\":[],\"verification\":[\"...\"],\"acceptance_checks\":[{\"description\":\"...\",\"command\":\"...\"}]}]}",
				"Every task must include a stable unique id such as task-1, task-2, task-3. depends_on must reference those exact ids only.",
				"Every task must be small enough for the configured local worker capacity and must include explicit allowed_paths.",
				"Allowed paths must be explicit files or narrow directories. Do not use '.', '*', repo-wide globs, or broad repository scopes.",
				"When decomposing large files, prefer tasks that create new modules/files without editing shared source files; add a later wiring task for shared files so independent work can run in parallel.",
				"Goals must be concrete and describe the exact code or doc change expected. Do not return placeholder goals like 'do work', 'make changes', or 'implement feature'.",
				"Every task must include concrete acceptance_checks that can be reviewed against the diff.",
				"If a discovered skill is directly relevant, include its exact skill name in the task skills array. Otherwise leave skills empty.",
				"Use depends_on with task ids only when a task must wait for another task; leave it empty for independent work that can run in parallel.",
				"Use only discovered verification commands, or these allowlisted forms: go test/build/vet, npm test/run, python -m pytest/unittest/compileall, pytest, cargo test/build/check/clippy, shellcheck, make test/check/lint/build.",
				"If no allowlisted verification applies, leave verification empty.",
			}, "\n"),
		},
		{
			Role:    "user",
			Content: strings.TrimSpace(contextText.String()) + "\n\nUser request:\n" + strings.TrimSpace(request),
		},
	}
}

func (m model) replanRequest(plan coding.Plan, guidance string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Replan the unfinished work for this existing WeazlCode plan.\n")
	fmt.Fprintf(&b, "Original plan title: %s\n", plan.Title)
	if strings.TrimSpace(plan.Summary) != "" {
		fmt.Fprintf(&b, "Original summary: %s\n", strings.TrimSpace(plan.Summary))
	}
	if strings.TrimSpace(guidance) != "" {
		fmt.Fprintf(&b, "\nUser replan guidance:\n%s\n", strings.TrimSpace(guidance))
	}
	b.WriteString("\nCompleted tasks are already done; do not repeat them except as context for later wiring tasks:\n")
	completed := 0
	for _, task := range plan.Tasks {
		if task.Status != coding.TaskStatusDone {
			continue
		}
		completed++
		fmt.Fprintf(&b, "- %s (%s): %s\n", task.ID, task.Title, task.Goal)
	}
	if completed == 0 {
		b.WriteString("- none\n")
	}
	b.WriteString("\nBlocked tasks and failure evidence; replace these with better-scoped tasks or omit impossible work:\n")
	blocked := 0
	for _, task := range plan.Tasks {
		if task.Status != coding.TaskStatusBlocked {
			continue
		}
		blocked++
		fmt.Fprintf(&b, "- %s (%s): %s\n", task.ID, task.Title, task.Goal)
		events, err := m.store.TaskEvents(task.ID)
		if err != nil {
			return "", err
		}
		for _, event := range latestPlanningRelevantEvents(events, 5) {
			fmt.Fprintf(&b, "  - %s: %s\n", event.Type, strings.TrimSpace(event.Message))
		}
	}
	if blocked == 0 {
		b.WriteString("- none\n")
	}
	b.WriteString("\nPending tasks from the old plan; keep only tasks that still make sense after accounting for completed and blocked work:\n")
	pending := 0
	for _, task := range plan.Tasks {
		if task.Status != coding.TaskStatusPending {
			continue
		}
		pending++
		fmt.Fprintf(&b, "- %s (%s): %s\n  allowed_paths: %s\n  depends_on: %s\n", task.ID, task.Title, task.Goal, strings.Join(task.AllowedPaths, ", "), strings.Join(task.DependsOn, ", "))
	}
	if pending == 0 {
		b.WriteString("- none\n")
	}
	b.WriteString("\nReturn a fresh draft plan containing only remaining useful work. Keep tasks small for the configured worker. Do not include tasks that require extracting code that does not exist. If a dark theme or design task is needed, specify concrete readable color goals instead of strict mathematical inversion.")
	return b.String(), nil
}

func latestPlanningRelevantEvents(events []coding.TaskEvent, limit int) []coding.TaskEvent {
	if limit <= 0 {
		limit = 5
	}
	relevant := map[string]bool{
		"worker_blocker":    true,
		"worker_error":      true,
		"worker_timeout":    true,
		"worker_json_error": true,
		"worker_rejected":   true,
		"reviewer_verdict":  true,
		"repair_requested":  true,
		"repair_limit":      true,
		"review_guardrail":  true,
	}
	var out []coding.TaskEvent
	for i := len(events) - 1; i >= 0 && len(out) < limit; i-- {
		if relevant[events[i].Type] {
			out = append(out, events[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (m model) planReferencedFilePreviews(request string) string {
	files, err := projectFiles(m.project.Root, 120)
	if err != nil {
		return ""
	}
	requestText := strings.ToLower(request)
	var matches []filePick
	for _, file := range files {
		path := strings.ToLower(file.Path)
		base := strings.ToLower(filepath.Base(file.Path))
		if strings.Contains(requestText, path) || (base != "" && strings.Contains(requestText, base)) {
			matches = append(matches, file)
		}
		if len(matches) >= 4 {
			break
		}
	}
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	for i, file := range matches {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(m.previewFileText(file.Path, 140))
	}
	return b.String()
}

func (m model) planRepairMessages(request, raw string, parseErr error) []llm.ChatMessage {
	worker := m.cfg.ProviderForRole("worker")
	workerProfile := m.workerCapacityProfile()
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You repair WeazlCode plan JSON.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"title\":\"...\",\"summary\":\"...\",\"tasks\":[{\"id\":\"task-1\",\"title\":\"...\",\"goal\":\"...\",\"allowed_paths\":[\"...\"],\"forbidden_paths\":[\"...\"],\"context_files\":[\"...\"],\"skills\":[\"...\"],\"depends_on\":[],\"verification\":[\"...\"],\"acceptance_checks\":[{\"description\":\"...\",\"command\":\"...\"}]}]}",
				"Do not add unknown fields. Every task must include id, title, goal, and allowed_paths. depends_on must reference exact task ids in the same plan.",
				"Allowed paths must be explicit files or narrow directories. Goals and acceptance checks must be concrete enough for the configured local worker capacity.",
				workerProfile.Instruction,
				"Verification commands must be allowlisted; leave verification empty if unsure.",
			}, "\n"),
		},
		{
			Role: "user",
			Content: fmt.Sprintf("Configured worker:\nprovider: %s\nmodel: %s/%s\ncapacity: %s\n\nUser request:\n%s\n\nParser error:\n%s\n\nRaw response to repair:\n%s",
				m.providerNameForRole("worker"),
				worker.Type,
				worker.Model,
				workerProfile.Label,
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
	normalizePlanVerification(&plan)
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

func (m model) taskDetailCommandText(selector string) string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Task detail error: " + err.Error()
	}
	if !ok || len(plan.Tasks) == 0 {
		return "No task detail yet. Use `/plan draft <title>` or `/plan generate <request>`."
	}
	task, index, ok := selectTask(plan.Tasks, selector)
	if !ok {
		return fmt.Sprintf("Task %q not found. Use `/tasks` to list task numbers and ids.", selector)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Task %d/%d: %s\n", index+1, len(plan.Tasks), task.Title)
	fmt.Fprintf(&b, "id: %s\nstatus: %s\nplan: %s\n\nGoal:\n%s\n", task.ID, task.Status, plan.Title, task.Goal)
	if len(task.AllowedPaths) > 0 {
		fmt.Fprintf(&b, "\nAllowed paths:\n%s", bulletList(task.AllowedPaths))
	}
	if len(task.ForbiddenPaths) > 0 {
		fmt.Fprintf(&b, "\nForbidden paths:\n%s", bulletList(task.ForbiddenPaths))
	}
	if len(task.ContextFiles) > 0 {
		fmt.Fprintf(&b, "\nContext files:\n%s", bulletList(task.ContextFiles))
	}
	if len(task.Skills) > 0 {
		fmt.Fprintf(&b, "\nSkills:\n%s", bulletList(task.Skills))
	}
	if len(task.DependsOn) > 0 {
		fmt.Fprintf(&b, "\nDepends on:\n%s", bulletList(task.DependsOn))
	}
	if len(task.Verification) > 0 {
		fmt.Fprintf(&b, "\nVerification:\n%s", bulletList(task.Verification))
	}
	if len(task.AcceptanceChecks) > 0 {
		b.WriteString("\nAcceptance checks:")
		for _, check := range task.AcceptanceChecks {
			label := strings.TrimSpace(check.Description)
			if label == "" {
				label = strings.TrimSpace(check.Command)
			}
			fmt.Fprintf(&b, "\n- %s", label)
		}
		b.WriteString("\n")
	}
	if packet, err := m.buildWorkerPacketForRun(task); err == nil {
		fmt.Fprintf(&b, "\nWorker packet:\n%s\n", renderJSON(packet))
	} else {
		fmt.Fprintf(&b, "\nWorker packet error: %s\n", err)
	}
	events, err := m.store.TaskEvents(task.ID)
	if err != nil {
		fmt.Fprintf(&b, "\nEvents error: %s", err)
		return strings.TrimRight(b.String(), "\n")
	}
	if len(events) == 0 {
		b.WriteString("\nEvents:\nNo events yet.")
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("\nEvents:")
	for _, event := range events {
		fmt.Fprintf(&b, "\n- %s %s: %s", event.CreatedAt.Format(time.RFC3339), event.Type, event.Message)
		if len(event.Payload) > 0 {
			fmt.Fprintf(&b, "\n  payload: %s", string(event.Payload))
		}
	}
	if verdict, ok := latestReviewVerdict(events); ok {
		fmt.Fprintf(&b, "\n\nLatest review: %s - %s", verdict.Verdict, verdict.Summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

func selectTask(tasks []coding.Task, selector string) (coding.Task, int, bool) {
	selector = strings.TrimSpace(selector)
	if selector != "" {
		if n, err := strconv.Atoi(selector); err == nil && n >= 1 && n <= len(tasks) {
			return tasks[n-1], n - 1, true
		}
		for i, task := range tasks {
			if task.ID == selector {
				return task, i, true
			}
		}
		return coding.Task{}, 0, false
	}
	for i, task := range tasks {
		if task.Status == coding.TaskStatusRunning || task.Status == coding.TaskStatusReviewing || task.Status == coding.TaskStatusBlocked {
			return task, i, true
		}
	}
	return tasks[0], 0, true
}

func bulletList(items []string) string {
	var b strings.Builder
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(item))
		}
	}
	return b.String()
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
			if len(task.Skills) > 0 {
				fmt.Fprintf(&b, "\n   skills: %s", strings.Join(task.Skills, ", "))
			}
			if len(task.DependsOn) > 0 {
				fmt.Fprintf(&b, "\n   depends_on: %s", strings.Join(task.DependsOn, ", "))
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

func (m model) skillsViewText() string {
	var b strings.Builder
	b.WriteString("Skills:\n")
	if !m.cfg.Skills.SkillsEnabled() {
		b.WriteString("disabled in config")
		return b.String()
	}
	if len(m.cfg.Skills.Paths) > 0 {
		fmt.Fprintf(&b, "precedence: %s\n\n", strings.Join(m.cfg.Skills.Paths, " > "))
	}
	skills, err := project.DiscoverSkills(m.project.Root, m.cfg.Skills.Paths)
	if err != nil {
		return "Skills error: " + err.Error()
	}
	if len(skills) == 0 {
		b.WriteString("none discovered")
		return b.String()
	}
	for _, skill := range skills {
		fmt.Fprintf(&b, "- %s [%s]\n", skill.Name, skill.Source)
		if strings.TrimSpace(skill.Description) != "" {
			fmt.Fprintf(&b, "  %s\n", skill.Description)
		}
		fmt.Fprintf(&b, "  path: %s\n", skill.Path)
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
	fmt.Fprintf(&b, "\nSkills:\nenabled: %t\npaths: %s\n", m.cfg.Skills.SkillsEnabled(), strings.Join(m.cfg.Skills.Paths, ", "))
	workerProfile := m.workerCapacityProfile()
	fmt.Fprintf(&b, "\nWorkers:\nconcurrency: %d\nrequest_timeout_seconds: %d\ncapacity: %s\n", m.workerConcurrency(), int(m.workerRequestTimeout().Seconds()), workerProfile.Label)
	fmt.Fprintf(&b, "\nHooks:\nenabled: %t\ntimeout_seconds: %d\nevents: %d\n", m.cfg.Hooks.Enabled, m.cfg.Hooks.TimeoutSeconds, len(m.cfg.Hooks.Events))
	bell := m.cfg.Notifications.Bell != nil && *m.cfg.Notifications.Bell
	fmt.Fprintf(&b, "\nNotifications:\nenabled: %t\nbell: %t\nevents: %s\n", m.cfg.Notifications.Enabled, bell, strings.Join(m.cfg.Notifications.Events, ", "))
	fmt.Fprintf(&b, "\nEditor:\ncommand: %s\nargs: %s\nwait: %t\n", emptyFallback(m.cfg.Editor.Command, "VISUAL/EDITOR"), strings.Join(m.cfg.Editor.Args, " "), m.cfg.Editor.Wait)
	fmt.Fprintf(&b, "\nDebug:\nadapters: %d\nconfigurations: %d\ntimeout_seconds: %d\n", len(m.cfg.Debug.Adapters), len(m.cfg.Debug.Configurations), m.debugTimeoutSeconds())
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
