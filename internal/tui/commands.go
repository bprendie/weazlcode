package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/bprendie/weazlcode/internal/coding"
)

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
		m.addSystemNote(slashHelp())
		m.status = "slash commands"
	case "project":
		m.addSystemNote(m.projectCommandText())
		m.status = "project summary"
	case "models":
		m.addSystemNote(m.modelRolesText())
		m.status = "model roles"
	case "tools":
		m.addSystemNote("Tools:\n" + strings.Join(m.getToolNames(), "\n"))
		m.status = "tool list"
	case "plan":
		return m.handlePlanCommand(fields[1:], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "tasks":
		m.addSystemNote(m.tasksCommandText())
		m.status = "task list"
	case "packet":
		m.addSystemNote(m.packetCommandText())
		m.status = "task packet"
	case "approve":
		return m.approveLatestPlan()
	case "reject":
		return m.rejectLatestPlan(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "run-task":
		return m.runNextTask()
	case "worker-patch":
		return m.importWorkerPatch(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0])))
	case "reviewer-input":
		m.addSystemNote(m.reviewerInputCommandText())
		m.status = "reviewer input"
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
		"/plan - show latest plan",
		"/plan draft <title> - create a draft plan with one seed task",
		"/plan import <json> - validate and store a structured plan JSON payload",
		"/tasks - list latest plan tasks",
		"/packet - show local-worker packet for the first pending task",
		"/approve - approve the latest draft plan",
		"/reject [reason] - block the latest plan",
		"/run-task - mark first pending task running and show its worker packet",
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
	task, ok := firstRunnableTask(plan.Tasks)
	if !ok {
		return "No pending task found for packet generation."
	}
	packet, err := m.buildWorkerPacket(task)
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
	task, ok := firstPendingTask(plan.Tasks)
	if !ok {
		m.addSystemNote("No pending task to run.")
		m.status = "no pending task"
		return m, nil, true
	}
	packet, err := m.buildWorkerPacket(task)
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
		Type:    "worker_start",
		Message: "Task marked running; worker packet prepared for local model dispatch.",
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
	return coding.BuildTaskPacket(task, coding.ContextPackOptions{
		ProjectRoot: m.project.Root,
		DefaultAllowed: []string{
			".",
		},
		DefaultVerify: []string{
			"go test ./...",
		},
	})
}

func renderJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("JSON render error: %v", err)
	}
	return string(b)
}

func firstRunnableTask(tasks []coding.Task) (coding.Task, bool) {
	for _, task := range tasks {
		if task.Status == coding.TaskStatusPending || task.Status == coding.TaskStatusBlocked {
			return task, true
		}
	}
	return coding.Task{}, false
}

func firstPendingTask(tasks []coding.Task) (coding.Task, bool) {
	for _, task := range tasks {
		if task.Status == coding.TaskStatusPending {
			return task, true
		}
	}
	return coding.Task{}, false
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
	return coding.ReviewerInput{
		Plan:              plan,
		TaskPacket:        packet,
		Diff:              diff,
		VerificationOut:   verificationOutputFromEvents(events),
		TaskEventsSummary: taskEventsSummary(events),
		Constraints: []string{
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
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Review error: " + err.Error())
			m.status = "review failed"
			return m, nil, true
		}
		m.addSystemNote("Reviewer requested fixes:\n" + renderJSON(verdict))
		m.status = "review needs fix"
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

func (m model) handlePlanCommand(args []string, rawArgs string) (tea.Model, tea.Cmd, bool) {
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
	m.addSystemNote(m.planCommandText())
	m.status = "latest plan"
	return m, nil, true
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

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
