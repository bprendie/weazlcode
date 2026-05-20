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
		if htmlFragmentTaskNeedsDocumentWrapperGuard(task) {
			add("HTML fragment/module task must explicitly forbid doctype/html/head/body document wrappers")
		}
	}
	for path, serialCount := range serialTaskCountsByAllowedPath(plan.Tasks) {
		if serialCount >= 4 {
			issues = append(issues, PlanQualityIssue{
				Severity: "error",
				Message:  fmt.Sprintf("too many serial tasks mutate %q; split work into separate modules/files plus a later integration task for small workers", path),
			})
		}
	}
	if staticSinglePagePlanOverfragmented(plan.Tasks) {
		issues = append(issues, PlanQualityIssue{
			Severity: "error",
			Message:  "single-page static website plan has too many fragment/module tasks; prefer 2-4 cohesive tasks that own index.html, styles.css, and optional documentation/validation",
		})
	}
	if interactivePythonPlanSingleFile(plan.Tasks) {
		issues = append(issues, PlanQualityIssue{
			Severity: "error",
			Message:  "generated interactive Python app/game plan is single-file; split into module tasks for domain state, rendering/entities, entrypoint, and smoke verification unless the user explicitly requested one file",
		})
	}
	issues = append(issues, integrationTaskScopeIssues(plan.Tasks)...)
	return issues
}

func integrationTaskScopeIssues(tasks []Task) []PlanQualityIssue {
	byID := map[string]Task{}
	for _, task := range tasks {
		if strings.TrimSpace(task.ID) != "" {
			byID[task.ID] = task
		}
	}
	var issues []PlanQualityIssue
	for _, task := range tasks {
		if len(task.DependsOn) < 2 || !integrationLikeTask(task) {
			if issue, ok := dependentCodeTaskScopeIssue(task, byID); ok {
				issues = append(issues, issue)
			}
			continue
		}
		allowed := map[string]bool{}
		for _, path := range normalizedTaskPaths(task.AllowedPaths) {
			allowed[path] = true
		}
		var missing []string
		for _, depID := range task.DependsOn {
			dep, ok := byID[strings.TrimSpace(depID)]
			if !ok || !singleCodeFileTask(dep) {
				continue
			}
			depPath := normalizedTaskPaths(dep.AllowedPaths)[0]
			if !allowed[depPath] {
				missing = append(missing, depPath)
			}
		}
		if len(missing) == 0 {
			continue
		}
		issues = append(issues, PlanQualityIssue{
			TaskID:    task.ID,
			TaskTitle: task.Title,
			Severity:  "error",
			Message:   fmt.Sprintf("final wiring/smoke task depends on module drafts but cannot edit their integration surfaces; add dependency module paths to allowed_paths: %s", strings.Join(missing, ", ")),
		})
	}
	return issues
}

func dependentCodeTaskScopeIssue(task Task, byID map[string]Task) (PlanQualityIssue, bool) {
	if len(task.DependsOn) < 2 || !singleCodeFileTask(task) {
		return PlanQualityIssue{}, false
	}
	allowed := map[string]bool{}
	for _, path := range normalizedTaskPaths(task.AllowedPaths) {
		allowed[path] = true
	}
	var missing []string
	for _, depID := range task.DependsOn {
		dep, ok := byID[strings.TrimSpace(depID)]
		if !ok || !singleCodeFileTask(dep) {
			continue
		}
		depPath := normalizedTaskPaths(dep.AllowedPaths)[0]
		if !allowed[depPath] {
			missing = append(missing, depPath)
		}
	}
	if len(missing) < 2 {
		return PlanQualityIssue{}, false
	}
	return PlanQualityIssue{
		TaskID:    task.ID,
		TaskTitle: task.Title,
		Severity:  "error",
		Message:   fmt.Sprintf("dependent code task composes multiple module drafts but cannot edit their integration surfaces; add dependency module paths to allowed_paths: %s", strings.Join(missing, ", ")),
	}, true
}

func integrationLikeTask(task Task) bool {
	text := strings.ToLower(task.Title + " " + task.Goal + " " + acceptanceCheckText(task.AcceptanceChecks))
	for _, marker := range []string{"wire", "wiring", "integrate", "integration", "entrypoint", "main.py", "smoke", "assemble"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func overSerializedDistinctCodeModuleChain(tasks []Task) []string {
	byID := map[string]Task{}
	for _, task := range tasks {
		if strings.TrimSpace(task.ID) != "" {
			byID[task.ID] = task
		}
	}
	var longest []string
	for _, task := range tasks {
		chain := distinctCodeModuleChainEndingAt(task, byID, map[string]bool{})
		if len(chain) > len(longest) {
			longest = chain
		}
	}
	return longest
}

func distinctCodeModuleChainEndingAt(task Task, byID map[string]Task, visiting map[string]bool) []string {
	if !singleCodeFileTask(task) || strings.TrimSpace(task.ID) == "" {
		return nil
	}
	if visiting[task.ID] {
		return []string{task.ID}
	}
	visiting[task.ID] = true
	best := []string{task.ID}
	taskPath := normalizedTaskPaths(task.AllowedPaths)[0]
	for _, depID := range task.DependsOn {
		dep, ok := byID[strings.TrimSpace(depID)]
		if !ok || !singleCodeFileTask(dep) {
			continue
		}
		depPath := normalizedTaskPaths(dep.AllowedPaths)[0]
		if depPath == taskPath {
			continue
		}
		chain := distinctCodeModuleChainEndingAt(dep, byID, visiting)
		if len(chain)+1 > len(best) {
			best = append(append([]string{}, chain...), task.ID)
		}
	}
	visiting[task.ID] = false
	return best
}

func singleCodeFileTask(task Task) bool {
	paths := normalizedTaskPaths(task.AllowedPaths)
	if len(paths) != 1 {
		return false
	}
	lower := strings.ToLower(paths[0])
	switch {
	case strings.HasSuffix(lower, ".py"),
		strings.HasSuffix(lower, ".js"),
		strings.HasSuffix(lower, ".ts"),
		strings.HasSuffix(lower, ".tsx"),
		strings.HasSuffix(lower, ".jsx"),
		strings.HasSuffix(lower, ".go"),
		strings.HasSuffix(lower, ".rs"),
		strings.HasSuffix(lower, ".java"),
		strings.HasSuffix(lower, ".cs"),
		strings.HasSuffix(lower, ".rb"),
		strings.HasSuffix(lower, ".php"),
		strings.HasSuffix(lower, ".swift"),
		strings.HasSuffix(lower, ".kt"),
		strings.HasSuffix(lower, ".kts"):
		return true
	default:
		return false
	}
}

func interactivePythonPlanSingleFile(tasks []Task) bool {
	pythonFiles := map[string]bool{}
	var text strings.Builder
	for _, task := range tasks {
		text.WriteString(" ")
		text.WriteString(task.Title)
		text.WriteString(" ")
		text.WriteString(task.Goal)
		text.WriteString(" ")
		text.WriteString(acceptanceCheckText(task.AcceptanceChecks))
		for _, path := range normalizedTaskPaths(task.AllowedPaths) {
			if strings.HasSuffix(strings.ToLower(path), ".py") {
				pythonFiles[path] = true
			}
		}
	}
	if len(pythonFiles) != 1 {
		return false
	}
	lower := strings.ToLower(text.String())
	if strings.Contains(lower, "single file") || strings.Contains(lower, "one file") {
		return false
	}
	if !strings.Contains(lower, "pygame") {
		return false
	}
	for _, marker := range []string{"game", "interactive", "flappy", "blackjack", "player", "render"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func staticSinglePagePlanOverfragmented(tasks []Task) bool {
	if len(tasks) <= 8 {
		return false
	}
	hasIndex := false
	hasOtherPage := false
	fragmentLike := 0
	for _, task := range tasks {
		for _, path := range normalizedTaskPaths(task.AllowedPaths) {
			lower := strings.ToLower(path)
			if strings.HasSuffix(lower, ".html") {
				if lower == "index.html" || strings.HasSuffix(lower, "/index.html") {
					hasIndex = true
				} else if strings.HasPrefix(lower, "sections/") || strings.HasPrefix(lower, "components/") || strings.HasPrefix(filepath.Base(lower), "_") {
					fragmentLike++
				} else {
					hasOtherPage = true
				}
			}
		}
	}
	return hasIndex && !hasOtherPage && fragmentLike >= 5
}

func htmlFragmentTaskNeedsDocumentWrapperGuard(task Task) bool {
	if !htmlFragmentLikeTask(task) {
		return false
	}
	text := strings.ToLower(task.Title + " " + task.Goal + " " + acceptanceCheckText(task.AcceptanceChecks))
	for _, required := range []string{"doctype", "html", "head", "body"} {
		if !strings.Contains(text, required) {
			return true
		}
	}
	negative := []string{"do not include", "without", "exclude", "must not include", "no document"}
	for _, phrase := range negative {
		if strings.Contains(text, phrase) {
			return false
		}
	}
	return true
}

func htmlFragmentLikeTask(task Task) bool {
	goal := strings.ToLower(task.Title + " " + task.Goal)
	if strings.Contains(goal, "assemble") || strings.Contains(goal, "integrate") || strings.Contains(goal, "wire") || strings.Contains(goal, "final page") || strings.Contains(goal, "final output") {
		return false
	}
	if !(strings.Contains(goal, "fragment") || strings.Contains(goal, "module") || strings.Contains(goal, "section") || strings.Contains(goal, "card") || strings.Contains(goal, "component")) {
		return false
	}
	hasNonIndexHTML := false
	for _, path := range normalizedTaskPaths(task.AllowedPaths) {
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".html") {
			if lower == "index.html" || strings.HasSuffix(lower, "/index.html") {
				return false
			}
			hasNonIndexHTML = true
		}
	}
	return hasNonIndexHTML
}

func acceptanceCheckText(checks []AcceptanceCheck) string {
	var b strings.Builder
	for _, check := range checks {
		b.WriteString(" ")
		b.WriteString(check.Description)
		b.WriteString(" ")
		b.WriteString(check.Command)
	}
	return b.String()
}

func serialTaskCountsByAllowedPath(tasks []Task) map[string]int {
	byID := map[string]Task{}
	paths := map[string]bool{}
	for _, task := range tasks {
		if strings.TrimSpace(task.ID) != "" {
			byID[task.ID] = task
		}
		for _, path := range normalizedTaskPaths(task.AllowedPaths) {
			paths[path] = true
		}
	}
	out := map[string]int{}
	for path := range paths {
		memo := map[string]int{}
		maxCount := 0
		for _, task := range tasks {
			if !taskAllowsPath(task, path) {
				continue
			}
			count := serialTaskCountForPath(task, path, byID, memo, map[string]bool{})
			if count > maxCount {
				maxCount = count
			}
		}
		out[path] = maxCount
	}
	return out
}

func serialTaskCountForPath(task Task, path string, byID map[string]Task, memo map[string]int, visiting map[string]bool) int {
	if strings.TrimSpace(task.ID) == "" {
		return 1
	}
	if count, ok := memo[task.ID]; ok {
		return count
	}
	if visiting[task.ID] {
		return 1
	}
	visiting[task.ID] = true
	maxDep := 0
	for _, depID := range task.DependsOn {
		dep, ok := byID[strings.TrimSpace(depID)]
		if !ok || !taskAllowsPath(dep, path) {
			continue
		}
		count := serialTaskCountForPath(dep, path, byID, memo, visiting)
		if count > maxDep {
			maxDep = count
		}
	}
	visiting[task.ID] = false
	memo[task.ID] = maxDep + 1
	return memo[task.ID]
}

func taskAllowsPath(task Task, path string) bool {
	for _, allowed := range normalizedTaskPaths(task.AllowedPaths) {
		if allowed == path {
			return true
		}
	}
	return false
}

func normalizedTaskPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if clean == "." || clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
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
