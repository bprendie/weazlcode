package coding

import (
	"fmt"
	"path/filepath"
	"strings"
)

type PlanQualityIssue struct {
	TaskID    string `json:"task_id,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}

func ValidatePlanQuality(plan Plan) []PlanQualityIssue {
	var issues []PlanQualityIssue
	if len(plan.Tasks) == 0 {
		return []PlanQualityIssue{{Severity: "error", Message: "plan has no tasks"}}
	}
	taskIDs := map[string]bool{}
	for _, task := range plan.Tasks {
		id := strings.TrimSpace(task.ID)
		if id != "" {
			taskIDs[id] = true
		}
	}
	for i, task := range plan.Tasks {
		label := strings.TrimSpace(task.Title)
		if label == "" {
			label = fmt.Sprintf("task %d", i+1)
		}
		add := func(message string) {
			issues = append(issues, PlanQualityIssue{
				TaskID:    task.ID,
				TaskTitle: label,
				Severity:  "error",
				Message:   message,
			})
		}
		if vagueGoal(task.Goal) {
			add("goal is too vague; describe the exact code or doc change expected")
		}
		if len(nonEmptyStrings(task.AllowedPaths)) == 0 {
			add("allowed_paths is empty; constrain the worker to explicit files or directories")
		} else if broadPathScope(task.AllowedPaths) {
			add("allowed_paths is too broad; use explicit files or narrow directories")
		}
		if len(task.AllowedPaths) > 8 {
			add("allowed_paths has too many entries; split this into smaller tasks")
		}
		if len(task.AcceptanceChecks) == 0 {
			add("acceptance_checks is empty; add concrete review criteria")
		}
		for _, dep := range task.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep != "" && !taskIDs[dep] {
				add(fmt.Sprintf("depends_on references unknown task id %q", dep))
			}
		}
	}
	return issues
}

func vagueGoal(goal string) bool {
	normalized := strings.Join(strings.Fields(strings.ToLower(goal)), " ")
	if normalized == "" {
		return true
	}
	vague := []string{
		"do work",
		"do the work",
		"make changes",
		"change things",
		"fix stuff",
		"implement feature",
		"update project",
		"improve code",
		"clean up",
		"finish task",
	}
	for _, phrase := range vague {
		if normalized == phrase || strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func broadPathScope(paths []string) bool {
	for _, path := range paths {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		switch clean {
		case ".", "/", "*", "./*", "/*":
			return true
		}
		if strings.HasSuffix(clean, "/...") || strings.Contains(clean, "*") {
			return true
		}
	}
	return false
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
