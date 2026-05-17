package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
)

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
