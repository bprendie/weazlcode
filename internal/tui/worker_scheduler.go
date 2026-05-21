package tui

import (
	"encoding/json"
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
		if repairableTask(task, events) {
			return task, true, nil
		}
		if m.retryableWorkerErrorTask(task, events) {
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
		return m.retryableWorkerErrorTask(task, events) || repairableTask(task, events), nil
	})
}

func parallelRunnableTasksWithRetry(tasks []coding.Task, limit int, retryable func(coding.Task) (bool, error)) ([]coding.Task, error) {
	if limit <= 0 {
		return nil, nil
	}
	done := map[string]bool{}
	activeOrSelected := map[string]bool{}
	for _, task := range tasks {
		if task.Status == coding.TaskStatusDone || task.Status == coding.TaskStatusReviewing {
			done[task.ID] = true
		}
		if task.Status == coding.TaskStatusRunning {
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

func repairableTask(task coding.Task, events []coding.TaskEvent) bool {
	if repairAttemptCount(events) >= repairAttemptLimit(task, events) {
		return false
	}
	if repairAttemptCount(events) >= 2 && !repairEffectiveness(events) && !latestReviewerNeedsDeterministicRepair(events) {
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

func repairAttemptLimit(task coding.Task, events []coding.TaskEvent) int {
	limit := maxRepairAttempts
	if singleFileGeneratedArtifactTask(task) && artifactValidationFailureCount(events) > 0 {
		limit = maxArtifactRepairAttempts
	}
	if latestReviewerNeedsDeterministicRepair(events) {
		return limit + 1
	}
	return limit
}

func latestReviewerNeedsDeterministicRepair(events []coding.TaskEvent) bool {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "reviewer_verdict" {
			continue
		}
		verdict, ok := reviewVerdictFromEvent(events[i])
		if !ok || verdict.Verdict != coding.ReviewNeedsFix {
			return false
		}
		text := strings.ToLower(verdict.Summary + "\n" + strings.Join(verdict.Issues, "\n"))
		for _, marker := range []string{
			"syntax error",
			"undefined name",
			"missing import",
			"not imported",
			"read before assignment",
			"reads local name",
			"referenced before assignment",
			"local variable",
			"unboundlocalerror",
			"typo",
			"attributeerror",
			"attribute error",
			"has no attribute",
			"typeerror",
			"missing required positional",
			"missing required argument",
			"positional argument",
		} {
			if strings.Contains(text, marker) {
				return true
			}
		}
		return false
	}
	return false
}

func retryableWorkerErrorTask(events []coding.TaskEvent) bool {
	event, ok := latestWorkerErrorEvent(events)
	if !ok {
		return false
	}
	switch event.Type {
	case "worker_rejected":
		return workerRejectionRetryable(events)
	case "artifact_validation":
		return artifactValidationFailureCount(events) < maxArtifactRepairAttempts && repairEffectiveness(events)
	default:
		return workerErrorFailureCount(events, event.Type) < maxWorkerPatchDiffRepairAttempts
	}
}

func (m model) retryableWorkerErrorTask(task coding.Task, events []coding.TaskEvent) bool {
	if retryableWorkerErrorTask(events) {
		return true
	}
	event, ok := latestWorkerErrorEvent(events)
	if !ok {
		return false
	}
	message := nonRetryableWorkerErrorMessage(event, events)
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "repair_limit",
		Message: message,
	})
	return false
}

func nonRetryableWorkerErrorMessage(event coding.TaskEvent, events []coding.TaskEvent) string {
	switch event.Type {
	case "worker_rejected":
		fingerprints := workerRejectionFingerprints(events)
		if len(fingerprints) >= 2 && fingerprints[len(fingerprints)-1] != "" && fingerprints[len(fingerprints)-1] == fingerprints[len(fingerprints)-2] {
			return "Repair limit reached: repeated worker rejection with the same returned/allowed path mismatch or contract failure."
		}
		return "Repair limit reached: worker patch rejection retry budget exhausted."
	case "artifact_validation":
		return "Repair limit reached: deterministic artifact validation did not converge."
	default:
		return "Repair limit reached: worker error retry budget exhausted."
	}
}

func artifactValidationFailureCount(events []coding.TaskEvent) int {
	count := 0
	for _, event := range events {
		if event.Type == "artifact_validation" {
			count++
		}
	}
	return count
}

func latestWorkerError(events []coding.TaskEvent) (string, bool) {
	event, ok := latestWorkerErrorEvent(events)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(event.Message), true
}

func latestWorkerErrorEvent(events []coding.TaskEvent) (coding.TaskEvent, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "worker_error", "worker_timeout", "worker_json_error", "worker_rejected", "artifact_validation":
			return events[i], true
		case "worker_model", "worker_patch", "worker_blocker", "repair_start", "repair_limit", "reviewer_verdict":
			return coding.TaskEvent{}, false
		}
	}
	return coding.TaskEvent{}, false
}

func workerRejectionRetryable(events []coding.TaskEvent) bool {
	fingerprints := workerRejectionFingerprints(events)
	if len(fingerprints) == 0 {
		return false
	}
	// Check for repeated identical rejections
	if len(fingerprints) >= maxWorkerPatchDiffRepairAttempts {
		last := fingerprints[len(fingerprints)-1]
		prev := fingerprints[len(fingerprints)-2]
		if last != "" && last == prev {
			// Repeated identical rejection detected - not retryable
			// The caller should record a repair_limit event
			return false
		}
	}
	// Cap total rejection retries
	if len(fingerprints) >= maxArtifactRepairAttempts {
		// Hit max retries - not retryable
		// The caller should record a repair_limit event
		return false
	}
	return true
}

func workerRejectionFingerprints(events []coding.TaskEvent) []string {
	var out []string
	for _, event := range events {
		if event.Type != "worker_rejected" {
			continue
		}
		out = append(out, workerRejectionFingerprint(event))
	}
	return out
}

func workerRejectionFingerprint(event coding.TaskEvent) string {
	if len(event.Payload) > 0 {
		var payload struct {
			Paths        []string `json:"paths"`
			AllowedPaths []string `json:"allowed_paths"`
			Error        string   `json:"error"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err == nil {
			parts := append([]string{}, payload.Paths...)
			parts = append(parts, payload.AllowedPaths...)
			parts = append(parts, payload.Error)
			return normalizedEventFingerprint(strings.Join(parts, "\n"))
		}
	}
	return normalizedEventFingerprint(event.Message)
}

func workerErrorFailureCount(events []coding.TaskEvent, eventType string) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func repairEffectiveness(events []coding.TaskEvent) bool {
	fingerprints := artifactValidationFingerprints(events)
	if len(fingerprints) < 2 {
		return true
	}
	last := fingerprints[len(fingerprints)-1]
	prev := fingerprints[len(fingerprints)-2]
	return last != "" && last != prev
}

func artifactValidationFingerprints(events []coding.TaskEvent) []string {
	var out []string
	for _, event := range events {
		if event.Type != "artifact_validation" {
			continue
		}
		out = append(out, artifactValidationFingerprint(event.Message))
	}
	return out
}

func artifactValidationFingerprint(message string) string {
	return normalizedEventFingerprint(strings.Join(filteredArtifactValidationLines(message), "\n"))
}

func filteredArtifactValidationLines(message string) []string {
	lines := strings.Split(message, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "artifact validation failed") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func normalizedEventFingerprint(message string) string {
	lines := strings.Split(message, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if line == "" {
			continue
		}
		normalized = append(normalized, strings.ToLower(line))
	}
	return strings.Join(normalized, "\n")
}

func latestArtifactValidation(events []coding.TaskEvent) (string, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "artifact_validation":
			return strings.TrimSpace(events[i].Message), true
		case "worker_patch", "worker_blocker", "repair_limit":
			return "", false
		}
	}
	return "", false
}

func latestArtifactValidationPaths(events []coding.TaskEvent) []string {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "artifact_validation":
			var payload struct {
				Issues []struct {
					Path string `json:"path"`
				} `json:"issues"`
			}
			if len(events[i].Payload) == 0 || json.Unmarshal(events[i].Payload, &payload) != nil {
				return nil
			}
			var paths []string
			seen := map[string]bool{}
			for _, issue := range payload.Issues {
				path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(issue.Path)))
				if path == "." || path == "" || seen[path] {
					continue
				}
				seen[path] = true
				paths = append(paths, path)
			}
			return paths
		case "worker_patch", "worker_blocker", "repair_limit":
			return nil
		}
	}
	return nil
}

func pathsIntersect(a, b []string) bool {
	seen := map[string]bool{}
	for _, path := range a {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if clean != "." && clean != "" {
			seen[clean] = true
		}
	}
	for _, path := range b {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if seen[clean] {
			return true
		}
	}
	return false
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
