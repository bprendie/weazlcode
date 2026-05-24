package coding

import "time"

// Plan represents a structured implementation plan created by the planner model.
type Plan struct {
	ID          string    `json:"id"`
	Directive   string    `json:"directive"`
	Tasks       []Task    `json:"tasks"`
	CreatedAt   time.Time `json:"created_at"`
	ApprovedAt  time.Time `json:"approved_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// Task represents a single bounded work unit for the worker model.
type Task struct {
	ID               string    `json:"id"`
	PlanID           string    `json:"plan_id"`
	Goal             string    `json:"goal"`
	OutputFiles      []string  `json:"output_files"`
	ContextFiles     []string  `json:"context_files"`
	AllowedPaths     []string  `json:"allowed_paths"`
	AcceptanceChecks []string  `json:"acceptance_checks"`
	VerifyCommand    string    `json:"verify_command,omitempty"`
	PreferFullFile   bool      `json:"prefer_full_file"`
	PreferPatch      bool      `json:"prefer_patch"`
	Dependencies     []string  `json:"dependencies,omitempty"`
	Status           string    `json:"status"` // pending, running, completed, failed, blocked
	StartedAt        time.Time `json:"started_at,omitempty"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
	Attempts         int       `json:"attempts"`
}

// TaskResult represents the output from a worker execution.
type TaskResult struct {
	TaskID           string            `json:"task_id"`
	Success          bool              `json:"success"`
	Files            map[string]string `json:"files"`   // path -> content
	Patches          map[string]string `json:"patches"` // path -> patch
	Message          string            `json:"message"`
	RawResponse      string            `json:"raw_response,omitempty"`
	ValidationErrors []string          `json:"validation_errors,omitempty"`
	CompletedAt      time.Time         `json:"completed_at"`
}

// Review represents a reviewer's assessment of task results.
type Review struct {
	TaskID      string    `json:"task_id"`
	Verdict     string    `json:"verdict"` // approved, needs_fix, blocked
	Issues      []Issue   `json:"issues,omitempty"`
	RepairScope string    `json:"repair_scope,omitempty"`
	Escalate    bool      `json:"escalate"`
	Message     string    `json:"message"`
	ReviewedAt  time.Time `json:"reviewed_at"`
}

// Issue represents a specific problem identified during review.
type Issue struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // critical, major, minor
}

// RepairPacket represents a bounded repair task for fixing issues.
type RepairPacket struct {
	TaskID       string   `json:"task_id"`
	Issues       []Issue  `json:"issues"`
	TargetPaths  []string `json:"target_paths"`
	ContextFiles []string `json:"context_files"`
	Instructions string   `json:"instructions"`
	Attempt      int      `json:"attempt"`
}

// ValidationResult represents the outcome of deterministic validation.
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// CommandResult records deterministic command execution output.
type CommandResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// RunState tracks the overall execution state of a plan.
type RunState struct {
	PlanID           string    `json:"plan_id"`
	Status           string    `json:"status"` // planning, approved, running, completed, failed
	ActiveWorkers    int       `json:"active_workers"`
	CompletedTasks   int       `json:"completed_tasks"`
	FailedTasks      int       `json:"failed_tasks"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
	ElapsedSeconds   float64   `json:"elapsed_seconds"`
	SoftLimitSeconds float64   `json:"soft_limit_seconds"`
}
