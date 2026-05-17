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

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/project"
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

func repairAttemptCount(events []coding.TaskEvent) int {
	count := 0
	for _, event := range events {
		if event.Type == "repair_requested" {
			count++
		}
	}
	return count
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
