package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/project"
)

const maxRepairAttempts = 2

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
