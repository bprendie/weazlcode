package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
)

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
	return m.dispatchParallelWorkers(candidates, "Parallel worker dispatch prepared", true)
}

func (m model) dispatchParallelWorkers(candidates []coding.Task, message string, note bool) (tea.Model, tea.Cmd, bool) {
	type dispatch struct {
		task   coding.Task
		packet coding.TaskPacket
	}
	dispatches := make([]dispatch, 0, len(candidates))
	for _, task := range candidates {
		packet, err := m.buildWorkerPacketForRun(task)
		if err != nil {
			m.addSystemNote(message + " error: " + err.Error())
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
			m.addSystemNote(message + " error: " + err.Error())
			m.status = "parallel workers failed"
			return m, nil, true
		}
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusRunning); err != nil {
			m.addSystemNote(message + " error: " + err.Error())
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
	m.startAutonomousRun()
	m.working.Spinner = spinner.Jump
	m.streamAt = time.Now()
	m.status = fmt.Sprintf("running %d worker(s)", len(candidates))
	if note {
		m.addSystemNote(message + ":\n" + renderJSON(packets))
	}
	return m, tea.Batch(append(cmds, m.working.Tick)...), true
}

func (m model) continueAutonomousRun(existing tea.Cmd) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 2)
	if existing != nil {
		cmds = append(cmds, existing)
	}
	if !m.autonomousRun {
		return m, tea.Batch(cmds...)
	}
	if expired, cmd := m.failAutonomousRunIfExpired(); expired {
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}
	if len(m.workerRuns) > 0 {
		m.thinking = true
		return m, tea.Batch(cmds...)
	}
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Autonomous run error: " + err.Error())
		m.status = "autonomous run failed"
		m.stopAutonomousRun()
		return m, tea.Batch(cmds...)
	}
	if !ok {
		m.status = "no plan"
		m.stopAutonomousRun()
		return m, tea.Batch(cmds...)
	}
	candidates, err := m.parallelRunnableTasks(plan.Tasks, m.workerConcurrency())
	if err != nil {
		m.addSystemNote("Autonomous run error: " + err.Error())
		m.status = "autonomous run failed"
		m.stopAutonomousRun()
		return m, tea.Batch(cmds...)
	}
	if len(candidates) > 0 {
		updated, cmd, _ := m.dispatchParallelWorkers(candidates, "Continuing worker dispatch", false)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return updated, tea.Batch(cmds...)
	}
	if _, ok := firstReviewingTask(plan.Tasks); ok {
		updated, cmd, _ := m.runReviewerModel()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return updated, tea.Batch(cmds...)
	}
	m.stopAutonomousRun()
	m.thinking = false
	if plan.Status == coding.PlanStatusDone {
		m.status = "plan done"
	} else {
		m.status = "autonomous run paused"
	}
	return m, tea.Batch(cmds...)
}

func (m *model) startAutonomousRun() {
	m.autonomousRun = true
	if m.autonomousRunStarted.IsZero() {
		m.autonomousRunStarted = time.Now()
	}
}

func (m *model) stopAutonomousRun() {
	m.autonomousRun = false
	m.autonomousRunStarted = time.Time{}
}

func (m model) autonomousRunTimeout() time.Duration {
	seconds := m.cfg.Workers.RunTimeoutSeconds
	if seconds <= 0 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func (m model) remainingAutonomousRunBudget() (time.Duration, bool) {
	if !m.autonomousRun || m.autonomousRunStarted.IsZero() {
		return 0, false
	}
	remaining := m.autonomousRunTimeout() - time.Since(m.autonomousRunStarted)
	if remaining <= 0 {
		return 0, true
	}
	return remaining, true
}

func (m model) boundedWorkerRepairTimeout() time.Duration {
	timeout := m.workerRequestTimeout()
	if remaining, ok := m.remainingAutonomousRunBudget(); ok {
		if remaining <= 0 {
			return 0
		}
		if remaining < timeout {
			return remaining
		}
	}
	return timeout
}

func (m *model) failAutonomousRunIfExpired() (bool, tea.Cmd) {
	if !m.autonomousRun {
		return false, nil
	}
	if m.autonomousRunStarted.IsZero() {
		m.autonomousRunStarted = time.Now()
		return false, nil
	}
	timeout := m.autonomousRunTimeout()
	elapsed := time.Since(m.autonomousRunStarted)
	if elapsed < timeout {
		return false, nil
	}
	for _, cancel := range m.cancelWorkerRuns {
		if cancel != nil {
			cancel()
		}
	}
	if m.cancelModel != nil {
		m.cancelModel()
	}
	m.cancelModel = nil
	m.activeModelRunID = 0
	m.workerRuns = map[int]string{}
	m.cancelWorkerRuns = map[int]context.CancelFunc{}
	m.thinking = false
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err == nil && ok {
		message := fmt.Sprintf("Autonomous run exceeded %s SLA after %s.", timeout.Round(time.Second), elapsed.Round(time.Second))
		for _, task := range plan.Tasks {
			switch task.Status {
			case coding.TaskStatusPending, coding.TaskStatusRunning, coding.TaskStatusReviewing, coding.TaskStatusBlocked:
				_ = m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked)
				_, _ = m.store.AddTaskEvent(coding.TaskEvent{
					TaskID:  task.ID,
					Type:    "run_timeout",
					Message: message,
				})
			}
		}
		_ = m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusBlocked)
		m.writeRunArtifact("run_timeout", struct {
			PlanID         string `json:"plan_id"`
			ElapsedSeconds int64  `json:"elapsed_seconds"`
			TimeoutSeconds int64  `json:"timeout_seconds"`
			Message        string `json:"message"`
		}{
			PlanID:         plan.ID,
			ElapsedSeconds: int64(elapsed.Seconds()),
			TimeoutSeconds: int64(timeout.Seconds()),
			Message:        message,
		})
		m.addSystemNote(message)
	}
	m.stopAutonomousRun()
	m.status = "run timeout"
	return true, m.notificationCmd("task_blocked", "Autonomous run timeout", m.status)
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
