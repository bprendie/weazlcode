package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/patcher"
	"github.com/bprendie/weazlcode/internal/planner"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/reviewer"
	"github.com/bprendie/weazlcode/internal/validation"
	"github.com/bprendie/weazlcode/internal/worker"
)

// Orchestrator coordinates the split-brain workflow.
type Orchestrator struct {
	planner   *planner.Planner
	worker    *worker.Worker
	reviewer  *reviewer.Reviewer
	validator *validation.Validator
	patcher   *patcher.Patcher
	project   *project.Summary
	config    Config

	mu          sync.Mutex
	currentPlan *coding.Plan
	runState    *coding.RunState
	taskStates  map[string]*TaskState
}

// Config holds orchestrator configuration.
type Config struct {
	WorkerConcurrency int
	ModelTimeout      time.Duration
	RunSoftLimit      time.Duration
	MaxRepairAttempts int
}

// TaskState tracks the execution state of a task.
type TaskState struct {
	Task             *coding.Task
	Status           string // pending, running, completed, failed, blocked
	Result           *coding.TaskResult
	Review           *coding.Review
	ValidationResult *coding.ValidationResult
	RepairAttempts   int
	StartedAt        time.Time
	CompletedAt      time.Time
	Error            error
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(
	plan *planner.Planner,
	work *worker.Worker,
	rev *reviewer.Reviewer,
	proj *project.Summary,
	cfg Config,
) *Orchestrator {
	return &Orchestrator{
		planner:    plan,
		worker:     work,
		reviewer:   rev,
		validator:  validation.NewValidator(proj),
		patcher:    patcher.NewPatcher(proj, false),
		project:    proj,
		config:     cfg,
		taskStates: make(map[string]*TaskState),
	}
}

// CreatePlan generates a new implementation plan.
func (o *Orchestrator) CreatePlan(ctx context.Context, directive string) (*coding.Plan, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	plan, err := o.planner.CreatePlan(ctx, directive)
	if err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}

	if err := o.planner.ValidatePlan(plan); err != nil {
		return nil, fmt.Errorf("validate plan: %w", err)
	}

	o.currentPlan = plan
	o.runState = &coding.RunState{
		PlanID:           plan.ID,
		Status:           "approved",
		SoftLimitSeconds: o.config.RunSoftLimit.Seconds(),
	}

	// Initialize task states
	o.taskStates = make(map[string]*TaskState)
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		o.taskStates[task.ID] = &TaskState{
			Task:   task,
			Status: "pending",
		}
	}

	return plan, nil
}

// ExecutePlan runs all tasks in the plan.
func (o *Orchestrator) ExecutePlan(ctx context.Context) error {
	o.mu.Lock()
	if o.currentPlan == nil {
		o.mu.Unlock()
		return fmt.Errorf("no plan to execute")
	}
	o.runState.Status = "running"
	o.runState.StartedAt = time.Now()
	o.mu.Unlock()

	// Create worker pool
	workerSem := make(chan struct{}, o.config.WorkerConcurrency)
	var wg sync.WaitGroup
	errChan := make(chan error, len(o.currentPlan.Tasks))

	// Execute tasks respecting dependencies
	for {
		readyTasks := o.getReadyTasks()
		if len(readyTasks) == 0 {
			break
		}

		for _, task := range readyTasks {
			wg.Add(1)
			go func(t *coding.Task) {
				defer wg.Done()

				// Acquire worker slot
				workerSem <- struct{}{}
				defer func() { <-workerSem }()

				if err := o.executeTask(ctx, t); err != nil {
					errChan <- fmt.Errorf("task %s: %w", t.ID, err)
				}
			}(task)
		}

		// Wait for current batch
		wg.Wait()

		// Check for errors
		select {
		case err := <-errChan:
			return err
		default:
		}

		// Check soft limit
		elapsed := time.Since(o.runState.StartedAt)
		if elapsed > o.config.RunSoftLimit {
			return fmt.Errorf("run exceeded soft limit of %v", o.config.RunSoftLimit)
		}
	}

	o.mu.Lock()
	o.runState.Status = "completed"
	o.runState.CompletedAt = time.Now()
	o.runState.ElapsedSeconds = time.Since(o.runState.StartedAt).Seconds()
	o.mu.Unlock()

	return nil
}

// executeTask executes a single task with validation and review.
func (o *Orchestrator) executeTask(ctx context.Context, task *coding.Task) error {
	o.mu.Lock()
	state := o.taskStates[task.ID]
	state.Status = "running"
	state.StartedAt = time.Now()
	task.Status = "running"
	task.StartedAt = time.Now()
	o.runState.ActiveWorkers++
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		o.runState.ActiveWorkers--
		o.mu.Unlock()
	}()

	snapshot, err := o.patcher.Snapshot(task)
	if err != nil {
		return o.handleTaskError(task, fmt.Errorf("snapshot: %w", err))
	}
	shouldRestore := true
	defer func() {
		if shouldRestore {
			_ = o.patcher.RestoreSnapshot(snapshot)
		}
	}()

	// Create timeout context
	taskCtx, cancel := context.WithTimeout(ctx, o.config.ModelTimeout)
	defer cancel()

	// Execute worker
	result, err := o.worker.ExecuteTask(taskCtx, task)
	if err != nil {
		if result != nil {
			o.mu.Lock()
			state.Result = result
			o.mu.Unlock()
		}
		return o.handleTaskError(task, err)
	}

	// Validate result
	validationResult := o.validator.ValidateTaskResult(task, result)

	o.mu.Lock()
	state.Result = result
	state.ValidationResult = validationResult
	o.mu.Unlock()

	// If validation failed, attempt repair
	if !validationResult.Valid {
		if err := o.attemptRepair(ctx, task, result, validationResult); err != nil {
			return o.handleTaskError(task, err)
		}
		// Get updated result after repair
		o.mu.Lock()
		result = state.Result
		validationResult = state.ValidationResult
		o.mu.Unlock()
	}

	// Apply changes
	if err := o.patcher.ApplyTaskResult(task, result); err != nil {
		return o.handleTaskError(task, fmt.Errorf("apply changes: %w", err))
	}

	verificationResult := o.verifyTask(ctx, task)
	validationResult = mergeValidation(validationResult, verificationResult)
	o.mu.Lock()
	state.ValidationResult = validationResult
	o.mu.Unlock()

	for !validationResult.Valid {
		o.mu.Lock()
		attempts := state.RepairAttempts
		o.mu.Unlock()
		if attempts >= o.config.MaxRepairAttempts {
			return o.handleTaskError(task, fmt.Errorf("verification failed after repair: %v", validationResult.Errors))
		}
		if err := o.attemptRepair(ctx, task, result, validationResult); err != nil {
			return o.handleTaskError(task, err)
		}
		o.mu.Lock()
		result = state.Result
		validationResult = state.ValidationResult
		o.mu.Unlock()
		if err := o.patcher.ApplyTaskResult(task, result); err != nil {
			return o.handleTaskError(task, fmt.Errorf("apply repair: %w", err))
		}
		verificationResult = o.verifyTask(ctx, task)
		validationResult = mergeValidation(validationResult, verificationResult)
		o.mu.Lock()
		state.ValidationResult = validationResult
		o.mu.Unlock()
	}

	// Review
	reviewCtx, reviewCancel := context.WithTimeout(ctx, o.config.ModelTimeout)
	defer reviewCancel()

	review, err := o.reviewer.ReviewTask(reviewCtx, task, result, validationResult)
	if err != nil {
		return o.handleTaskError(task, fmt.Errorf("review: %w", err))
	}

	o.mu.Lock()
	state.Review = review
	o.mu.Unlock()

	// Handle review verdict
	switch review.Verdict {
	case "approved":
		shouldRestore = false
		return o.completeTask(task)
	case "needs_fix":
		if validationResult.Valid && len(review.Issues) == 0 {
			shouldRestore = false
			return o.completeTask(task)
		}
		if o.reviewer.ShouldEscalate(review, state.RepairAttempts, o.config.MaxRepairAttempts) {
			return o.handleTaskError(task, fmt.Errorf("escalation required: %s", review.Message))
		}
		if err := o.attemptReviewRepair(ctx, task, review); err != nil {
			return o.handleTaskError(task, err)
		}
		shouldRestore = false
		return nil
	case "blocked":
		return o.handleTaskError(task, fmt.Errorf("blocked: %s", review.Message))
	default:
		return o.handleTaskError(task, fmt.Errorf("unknown verdict: %s", review.Verdict))
	}
}

func (o *Orchestrator) verifyTask(ctx context.Context, task *coding.Task) *coding.ValidationResult {
	vr := &coding.ValidationResult{Valid: true}
	if task.VerifyCommand == "" {
		return vr
	}

	verifyCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, err := o.validator.RunCommand(verifyCtx, task.VerifyCommand)
	if err == nil {
		return vr
	}

	vr.Valid = false
	message := fmt.Sprintf("verify command failed: %s", task.VerifyCommand)
	if result != nil {
		message = fmt.Sprintf("%s\nexit code: %d\nstdout:\n%s\nstderr:\n%s",
			message, result.ExitCode, result.Stdout, result.Stderr)
	} else {
		message = fmt.Sprintf("%s\n%s", message, err.Error())
	}
	vr.Errors = append(vr.Errors, message)
	return vr
}

func mergeValidation(base, extra *coding.ValidationResult) *coding.ValidationResult {
	if base == nil {
		base = &coding.ValidationResult{Valid: true}
	}
	if extra == nil {
		return base
	}
	base.Valid = base.Valid && extra.Valid
	base.Errors = append(base.Errors, extra.Errors...)
	base.Warnings = append(base.Warnings, extra.Warnings...)
	return base
}

// attemptRepair attempts to repair validation failures.
func (o *Orchestrator) attemptRepair(ctx context.Context, task *coding.Task, result *coding.TaskResult, vr *coding.ValidationResult) error {
	o.mu.Lock()
	state := o.taskStates[task.ID]
	state.RepairAttempts++

	if state.RepairAttempts > o.config.MaxRepairAttempts {
		o.mu.Unlock()
		return fmt.Errorf("max repair attempts reached")
	}
	o.mu.Unlock()

	// Create repair packet from validation errors
	repair := &coding.RepairPacket{
		TaskID:       task.ID,
		TargetPaths:  task.OutputFiles,
		ContextFiles: task.ContextFiles,
		Instructions: fmt.Sprintf("Fix validation errors: %v", vr.Errors),
		Attempt:      state.RepairAttempts,
	}

	// Execute repair
	repairCtx, cancel := context.WithTimeout(ctx, o.config.ModelTimeout)
	defer cancel()

	repairResult, err := o.worker.ExecuteRepair(repairCtx, task, repair)
	if err != nil {
		if repairResult != nil {
			o.mu.Lock()
			state.Result = repairResult
			o.mu.Unlock()
		}
		return fmt.Errorf("repair execution: %w", err)
	}

	// Validate repair
	repairValidation := o.validator.ValidateTaskResult(task, repairResult)

	o.mu.Lock()
	state.Result = repairResult
	state.ValidationResult = repairValidation
	o.mu.Unlock()

	if !repairValidation.Valid {
		return fmt.Errorf("repair validation failed: %v", repairValidation.Errors)
	}

	return nil
}

// attemptReviewRepair attempts to repair issues found in review.
func (o *Orchestrator) attemptReviewRepair(ctx context.Context, task *coding.Task, review *coding.Review) error {
	o.mu.Lock()
	state := o.taskStates[task.ID]
	state.RepairAttempts++
	o.mu.Unlock()

	repair := o.reviewer.CreateRepairPacket(task, review, state.RepairAttempts)

	repairCtx, cancel := context.WithTimeout(ctx, o.config.ModelTimeout)
	defer cancel()

	repairResult, err := o.worker.ExecuteRepair(repairCtx, task, repair)
	if err != nil {
		if repairResult != nil {
			o.mu.Lock()
			state.Result = repairResult
			o.mu.Unlock()
		}
		return fmt.Errorf("repair execution: %w", err)
	}

	// Validate and apply
	repairValidation := o.validator.ValidateTaskResult(task, repairResult)
	if !repairValidation.Valid {
		return fmt.Errorf("repair validation failed: %v", repairValidation.Errors)
	}

	if err := o.patcher.ApplyTaskResult(task, repairResult); err != nil {
		return fmt.Errorf("apply repair: %w", err)
	}

	verificationResult := o.verifyTask(ctx, task)
	repairValidation = mergeValidation(repairValidation, verificationResult)
	if !repairValidation.Valid {
		return fmt.Errorf("repair verification failed: %v", repairValidation.Errors)
	}

	// Re-review
	reviewCtx, reviewCancel := context.WithTimeout(ctx, o.config.ModelTimeout)
	defer reviewCancel()

	newReview, err := o.reviewer.ReviewTask(reviewCtx, task, repairResult, repairValidation)
	if err != nil {
		return fmt.Errorf("re-review: %w", err)
	}

	o.mu.Lock()
	state.Result = repairResult
	state.ValidationResult = repairValidation
	state.Review = newReview
	o.mu.Unlock()

	if newReview.Verdict == "approved" {
		return o.completeTask(task)
	}

	return fmt.Errorf("repair unsuccessful: %s", newReview.Message)
}

// getReadyTasks returns tasks that are ready to execute.
func (o *Orchestrator) getReadyTasks() []*coding.Task {
	o.mu.Lock()
	defer o.mu.Unlock()

	ready := make([]*coding.Task, 0)

	for _, state := range o.taskStates {
		if state.Status != "pending" {
			continue
		}

		// Check if all dependencies are completed
		allDepsComplete := true
		for _, depID := range state.Task.Dependencies {
			depState := o.taskStates[depID]
			if depState.Status != "completed" {
				allDepsComplete = false
				break
			}
		}

		if allDepsComplete {
			ready = append(ready, state.Task)
		}
	}

	return ready
}

// completeTask marks a task as completed.
func (o *Orchestrator) completeTask(task *coding.Task) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	state := o.taskStates[task.ID]
	state.Status = "completed"
	state.CompletedAt = time.Now()
	task.Status = "completed"
	task.CompletedAt = time.Now()
	o.runState.CompletedTasks++

	return nil
}

// handleTaskError marks a task as failed.
func (o *Orchestrator) handleTaskError(task *coding.Task, err error) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	state := o.taskStates[task.ID]
	o.saveFailureArtifacts(task, state, err)
	state.Status = "failed"
	state.Error = err
	task.Status = "failed"
	o.runState.FailedTasks++

	return err
}

// GetRunState returns the current run state.
func (o *Orchestrator) GetRunState() *coding.RunState {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.runState == nil {
		return nil
	}

	// Create a copy
	state := *o.runState
	if !state.StartedAt.IsZero() && state.CompletedAt.IsZero() {
		state.ElapsedSeconds = time.Since(state.StartedAt).Seconds()
	}
	return &state
}

// GetCurrentPlan returns a copy of the active plan.
func (o *Orchestrator) GetCurrentPlan() *coding.Plan {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.currentPlan == nil {
		return nil
	}
	plan := *o.currentPlan
	plan.Tasks = append([]coding.Task(nil), o.currentPlan.Tasks...)
	return &plan
}

// GetTaskState returns the state of a specific task.
func (o *Orchestrator) GetTaskState(taskID string) *TaskState {
	o.mu.Lock()
	defer o.mu.Unlock()

	state, ok := o.taskStates[taskID]
	if !ok {
		return nil
	}

	// Return a copy
	stateCopy := *state
	return &stateCopy
}
