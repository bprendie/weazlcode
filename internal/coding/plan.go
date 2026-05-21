package coding

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	PlanStatusDraft    = "draft"
	PlanStatusApproved = "approved"
	PlanStatusRunning  = "running"
	PlanStatusBlocked  = "blocked"
	PlanStatusDone     = "done"

	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusReviewing = "reviewing"
	TaskStatusBlocked   = "blocked"
	TaskStatusDone      = "done"

	ReviewApprove  = "approve"
	ReviewNeedsFix = "needs_fix"
	ReviewBlocked  = "blocked"
)

type Plan struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	ProjectRoot string    `json:"project_root"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Status      string    `json:"status"`
	Tasks       []Task    `json:"tasks,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Task struct {
	ID                string            `json:"id"`
	PlanID            string            `json:"plan_id"`
	Title             string            `json:"title"`
	Goal              string            `json:"goal"`
	Status            string            `json:"status"`
	InterfaceContract InterfaceContract `json:"interface_contract,omitempty"`
	AllowedPaths      []string          `json:"allowed_paths,omitempty"`
	ForbiddenPaths    []string          `json:"forbidden_paths,omitempty"`
	ContextFiles      []string          `json:"context_files,omitempty"`
	Skills            []string          `json:"skills,omitempty"`
	DependsOn         []string          `json:"depends_on,omitempty"`
	Verification      []string          `json:"verification,omitempty"`
	AcceptanceChecks  []AcceptanceCheck `json:"acceptance_checks,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type InterfaceContract struct {
	Summary      string   `json:"summary,omitempty"`
	Exports      []string `json:"exports,omitempty"`
	Imports      []string `json:"imports,omitempty"`
	Constructors []string `json:"constructors,omitempty"`
	Methods      []string `json:"methods,omitempty"`
	Attributes   []string `json:"attributes,omitempty"`
	Commands     []string `json:"commands,omitempty"`
}

type AcceptanceCheck struct {
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
}

type TaskEvent struct {
	ID        int64           `json:"id"`
	TaskID    string          `json:"task_id"`
	Type      string          `json:"type"`
	Message   string          `json:"message"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type ReviewVerdict struct {
	Verdict string   `json:"verdict"`
	Summary string   `json:"summary"`
	Issues  []string `json:"issues,omitempty"`
}

func ValidatePlan(plan Plan) error {
	if strings.TrimSpace(plan.ID) == "" {
		return fmt.Errorf("plan id is required")
	}
	if strings.TrimSpace(plan.SessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(plan.Title) == "" {
		return fmt.Errorf("plan title is required")
	}
	if !validPlanStatus(plan.Status) {
		return fmt.Errorf("invalid plan status %q", plan.Status)
	}
	for _, task := range plan.Tasks {
		if err := ValidateTask(task); err != nil {
			return err
		}
	}
	return nil
}

func ValidateTask(task Task) error {
	if strings.TrimSpace(task.ID) == "" {
		return fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(task.PlanID) == "" {
		return fmt.Errorf("plan id is required")
	}
	if strings.TrimSpace(task.Title) == "" {
		return fmt.Errorf("task title is required")
	}
	if strings.TrimSpace(task.Goal) == "" {
		return fmt.Errorf("task goal is required")
	}
	if !validTaskStatus(task.Status) {
		return fmt.Errorf("invalid task status %q", task.Status)
	}
	for _, check := range task.AcceptanceChecks {
		if strings.TrimSpace(check.Description) == "" && strings.TrimSpace(check.Command) == "" {
			return fmt.Errorf("acceptance check requires description or command")
		}
	}
	return nil
}

func ValidateReviewVerdict(verdict ReviewVerdict) error {
	switch verdict.Verdict {
	case ReviewApprove, ReviewNeedsFix, ReviewBlocked:
		return nil
	default:
		return fmt.Errorf("invalid review verdict %q", verdict.Verdict)
	}
}

func validPlanStatus(status string) bool {
	return ValidPlanStatus(status)
}

func ValidPlanStatus(status string) bool {
	switch status {
	case PlanStatusDraft, PlanStatusApproved, PlanStatusRunning, PlanStatusBlocked, PlanStatusDone:
		return true
	default:
		return false
	}
}

func validTaskStatus(status string) bool {
	return ValidTaskStatus(status)
}

func ValidTaskStatus(status string) bool {
	switch status {
	case TaskStatusPending, TaskStatusRunning, TaskStatusReviewing, TaskStatusBlocked, TaskStatusDone:
		return true
	default:
		return false
	}
}
