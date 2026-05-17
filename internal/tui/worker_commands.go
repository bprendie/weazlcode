package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/project"
)

const maxWorkerPatchDiffRepairAttempts = 2

var modelSizePattern = regexp.MustCompile(`(?i)(?:^|[^0-9])([0-9]+(?:\.[0-9]+)?)\s*b(?:[^a-z]|$)`)

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

func firstRunningTask(tasks []coding.Task) (coding.Task, bool) {
	for _, task := range tasks {
		if task.Status == coding.TaskStatusRunning {
			return task, true
		}
	}
	return coding.Task{}, false
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
func latestRepairRequest(events []coding.TaskEvent) (string, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "repair_requested" && strings.TrimSpace(events[i].Message) != "" {
			return events[i].Message, true
		}
	}
	return "", false
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
