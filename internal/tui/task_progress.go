package tui

import (
	"fmt"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
)

type taskProgress struct {
	Total     int
	Done      int
	Running   int
	Reviewing int
	Blocked   int
	Pending   int
}

func taskProgressForPlan(plan coding.Plan) taskProgress {
	var progress taskProgress
	progress.Total = len(plan.Tasks)
	for _, task := range plan.Tasks {
		switch task.Status {
		case coding.TaskStatusDone:
			progress.Done++
		case coding.TaskStatusRunning:
			progress.Running++
		case coding.TaskStatusReviewing:
			progress.Reviewing++
		case coding.TaskStatusBlocked:
			progress.Blocked++
		case coding.TaskStatusPending:
			progress.Pending++
		}
	}
	return progress
}

func taskProgressSummary(plan coding.Plan) string {
	progress := taskProgressForPlan(plan)
	if progress.Total == 0 {
		return "Progress: no tasks"
	}
	return fmt.Sprintf("Progress: %d/%d done | %s", progress.Done, progress.Total, taskProgressStatusParts(progress))
}

func taskProgressBadge(plan coding.Plan) string {
	return taskProgressBadgeWithSpinner(plan, "")
}

func taskProgressBadgeWithSpinner(plan coding.Plan, spinnerFrame string) string {
	progress := taskProgressForPlan(plan)
	if progress.Total == 0 {
		return ""
	}
	return fmt.Sprintf("task %d/%d %s", progress.Done, progress.Total, taskProgressStatusPartsWithSpinner(progress, spinnerFrame))
}

func taskProgressStatusParts(progress taskProgress) string {
	return taskProgressStatusPartsWithSpinner(progress, "")
}

func taskProgressStatusPartsWithSpinner(progress taskProgress, spinnerFrame string) string {
	parts := make([]string, 0, 4)
	if progress.Running > 0 {
		label := fmt.Sprintf("%d running", progress.Running)
		if strings.TrimSpace(spinnerFrame) != "" {
			label = fmt.Sprintf("%s %s", spinnerFrame, label)
		}
		parts = append(parts, label)
	}
	if progress.Reviewing > 0 {
		label := fmt.Sprintf("%d reviewing", progress.Reviewing)
		if strings.TrimSpace(spinnerFrame) != "" && progress.Running == 0 {
			label = fmt.Sprintf("%s %s", spinnerFrame, label)
		}
		parts = append(parts, label)
	}
	if progress.Blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", progress.Blocked))
	}
	if progress.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", progress.Pending))
	}
	if len(parts) == 0 {
		return "complete"
	}
	return strings.Join(parts, ", ")
}
