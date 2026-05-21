package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
)

const (
	maxWorkerPatchDiffRepairAttempts       = 2
	maxLocalArtifactValidationRepairPasses = 2
	maxDeterministicArtifactRepairPasses   = 2
)

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
	events, _ := m.store.TaskEvents(task.ID)
	if repairCycleActive(events) && detectIdenticalRepair(patch, events) {
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Worker patch error: " + err.Error())
			m.status = "worker patch failed"
			return m, nil, true
		}
		message := "Worker patch rejected: identical repair detected. Worker generated the same code changes as a previous attempt without making progress."
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "worker_rejected",
			Message: message,
		})
		m.addSystemNote(message)
		m.status = "identical repair detected"
		return m, m.notificationCmd("worker_rejected", task.Title, message), true
	}
	if blocker := workerBlockerText(patch.Blocker); blocker != "" {
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
			Message: blocker,
			Payload: payload,
		})
		m.addSystemNote(fmt.Sprintf("Worker reported blocker for %s:\n%s", task.Title, blocker))
		m.status = "worker reported blocker"
		return m, m.notificationCmd("worker_blocker", task.Title, blocker), true
	}
	paths := coding.PatchPaths(patch.Patch)
	if len(patch.Files) > 0 {
		paths = coding.WorkerFileEditPaths(patch.Files)
	}
	allowedPaths := taskAllowedPaths(task)
	if err := coding.ValidatePatchPaths(paths, allowedPaths, task.ForbiddenPaths); err != nil {
		originalPaths := append([]string{}, paths...)
		if corrected, ok := m.tryAutoCorrectPathMismatch(task, patch, paths, allowedPaths); ok {
			correctedPaths := workerPatchPaths(corrected)
			if validateErr := coding.ValidatePatchPaths(correctedPaths, allowedPaths, task.ForbiddenPaths); validateErr == nil {
				patch = corrected
				paths = correctedPaths
				payload, _ := json.Marshal(struct {
					OriginalPaths  []string `json:"original_paths"`
					CorrectedPaths []string `json:"corrected_paths"`
				}{
					OriginalPaths:  originalPaths,
					CorrectedPaths: correctedPaths,
				})
				_, _ = m.store.AddTaskEvent(coding.TaskEvent{
					TaskID:  task.ID,
					Type:    "worker_path_corrected",
					Message: fmt.Sprintf("Auto-corrected worker path from %q to %q.", strings.Join(originalPaths, ", "), strings.Join(correctedPaths, ", ")),
					Payload: payload,
				})
				m.addSystemNote(fmt.Sprintf("Auto-corrected worker path from %q to %q.", strings.Join(originalPaths, ", "), strings.Join(correctedPaths, ", ")))
			} else {
				err = validateErr
			}
		}
	}
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
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Worker patch error: " + err.Error())
			m.status = "worker patch failed"
			return m, nil, true
		}
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
	if repairCycleActive(events) {
		if validationPaths := latestArtifactValidationPaths(events); len(validationPaths) > 0 && !pathsIntersect(paths, validationPaths) {
			message := fmt.Sprintf("Worker patch rejected: artifact validation failed in %s, but this repair touched %s. Repair the files named by validation first.", strings.Join(validationPaths, ", "), strings.Join(paths, ", "))
			payload, _ := json.Marshal(struct {
				Paths           []string `json:"paths"`
				ValidationPaths []string `json:"validation_paths"`
			}{Paths: paths, ValidationPaths: validationPaths})
			_, _ = m.store.AddTaskEvent(coding.TaskEvent{
				TaskID:  task.ID,
				Type:    "worker_rejected",
				Message: message,
				Payload: payload,
			})
			if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
				m.addSystemNote("Worker patch error: " + err.Error())
				m.status = "worker patch failed"
				return m, nil, true
			}
			m.writeRunArtifact("worker_rejected", struct {
				TaskID          string   `json:"task_id"`
				Paths           []string `json:"paths"`
				ValidationPaths []string `json:"validation_paths"`
			}{TaskID: task.ID, Paths: paths, ValidationPaths: validationPaths})
			m.addSystemNote(message)
			m.status = "worker patch rejected"
			return m, nil, true
		}
	}
	if rewrites := m.suspiciousWorkerRewrites(task, patch.Files); len(rewrites) > 0 {
		message := formatSuspiciousRewriteRejection(rewrites)
		payload, _ := json.Marshal(rewrites)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "worker_rejected",
			Message: message,
			Payload: payload,
		})
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Worker patch error: " + err.Error())
			m.status = "worker patch failed"
			return m, nil, true
		}
		m.writeRunArtifact("worker_rejected", struct {
			TaskID   string                     `json:"task_id"`
			Rewrites []coding.SuspiciousRewrite `json:"rewrites"`
		}{TaskID: task.ID, Rewrites: rewrites})
		m.addSystemNote(message)
		m.status = "worker patch rejected"
		return m, nil, true
	}
	if singleFileGeneratedArtifactTask(task) && len(patch.Files) == 0 {
		message := "Worker patch rejected: single-file generated artifact tasks must return files[] with complete content for the allowed file and an empty patch field."
		payload, _ := json.Marshal(struct {
			AllowedPaths []string `json:"allowed_paths"`
			PatchChars   int      `json:"patch_chars"`
		}{AllowedPaths: task.AllowedPaths, PatchChars: len(patch.Patch)})
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "worker_rejected",
			Message: message,
			Payload: payload,
		})
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Worker patch error: " + err.Error())
			m.status = "worker patch failed"
			return m, nil, true
		}
		m.writeRunArtifact("worker_rejected", struct {
			TaskID       string   `json:"task_id"`
			AllowedPaths []string `json:"allowed_paths"`
			PatchChars   int      `json:"patch_chars"`
		}{TaskID: task.ID, AllowedPaths: task.AllowedPaths, PatchChars: len(patch.Patch)})
		m.addSystemNote(message)
		m.status = "worker patch rejected"
		return m, nil, true
	}
	m.recordWorkerPatchAttempt(task.ID, patch)
	result, err := applyWorkerPatchContent(m.project.Root, patch)
	if err != nil {
		applyErrors = append(applyErrors, err)
		if len(patch.Files) == 0 && repairInvalidDiff && repairAttempts < maxWorkerPatchDiffRepairAttempts {
			timeout := m.boundedWorkerRepairTimeout()
			if timeout <= 0 {
				message := formatWorkerPatchApplyFailure(applyErrors, fmt.Errorf("run SLA expired before patch repair"))
				return m.blockTaskAfterWorkerApplyFailure(task, message)
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	if issues := m.validateArtifactTaskOutput(task); len(issues) > 0 {
		events, _ := m.store.TaskEvents(task.ID)
		message := formatArtifactValidationIssues(issues)
		payload, _ := json.Marshal(artifactValidationPayload(issues))
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "artifact_validation",
			Message: message,
			Payload: payload,
		})
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusBlocked); err != nil {
			m.addSystemNote("Worker patch error: " + err.Error())
			m.status = "worker patch failed"
			return m, nil, true
		}
		m.writeRunArtifact("artifact_validation", struct {
			TaskID string                    `json:"task_id"`
			Issues []artifactValidationIssue `json:"issues"`
		}{TaskID: task.ID, Issues: issues})
		if artifactValidationFailureCount(events)+1 >= localArtifactValidationRepairLimit(task, issues) {
			_, _ = m.store.AddTaskEvent(coding.TaskEvent{
				TaskID:  task.ID,
				Type:    "artifact_validation_escalated",
				Message: "Repeated local artifact validation failures; escalating current output to frontier reviewer for a focused repair brief.",
			})
			if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusReviewing); err != nil {
				m.addSystemNote("Worker patch error: " + err.Error())
				m.status = "worker patch failed"
				return m, nil, true
			}
			m.addSystemNote(message + "\n\nRepeated local validation failures; escalating to reviewer for focused repair guidance.")
			m.status = "artifact validation escalated"
			return m, nil, true
		}
		m.addSystemNote(message)
		m.status = "artifact validation failed"
		return m, nil, true
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
	if diff, err := m.taskGitDiff(task); err == nil {
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

func localArtifactValidationRepairLimit(task coding.Task, issues []artifactValidationIssue) int {
	if singleFileGeneratedArtifactTask(task) && syntaxArtifactValidationIssues(issues) {
		return 1
	}
	if generatedCodeArtifactTask(task) && deterministicArtifactValidationIssues(issues) {
		return maxDeterministicArtifactRepairPasses
	}
	return maxLocalArtifactValidationRepairPasses
}

func syntaxArtifactValidationIssues(issues []artifactValidationIssue) bool {
	if len(issues) == 0 {
		return false
	}
	for _, issue := range issues {
		text := strings.ToLower(issue.Message)
		if strings.Contains(text, "syntax error") {
			return true
		}
	}
	return false
}

func deterministicArtifactValidationIssues(issues []artifactValidationIssue) bool {
	if len(issues) == 0 {
		return false
	}
	for _, issue := range issues {
		text := strings.ToLower(issue.Message)
		switch {
		case strings.Contains(text, "syntax error"),
			strings.Contains(text, "compile failed"),
			strings.Contains(text, "references undefined name"),
			strings.Contains(text, "references missing local export"),
			strings.Contains(text, "shadows method"),
			strings.Contains(text, "--smoke branch"),
			strings.Contains(text, "unconditional interactive loop"),
			strings.Contains(text, "nested while true"),
			strings.Contains(text, "missing or unreadable"):
			continue
		default:
			return false
		}
	}
	return true
}

func (m model) suspiciousWorkerRewrites(task coding.Task, files []coding.WorkerFileEdit) []coding.SuspiciousRewrite {
	rewrites := coding.DetectSuspiciousFileRewrites(m.project.Root, files)
	if len(rewrites) == 0 || !cohesiveWholeFileArtifactTask(task) {
		return rewrites
	}
	filtered := rewrites[:0]
	for _, rewrite := range rewrites {
		reason := strings.ToLower(strings.TrimSpace(rewrite.Reason))
		if strings.Contains(reason, "placeholder sentinel") || strings.Contains(reason, "diff marker residue") {
			filtered = append(filtered, rewrite)
		}
	}
	return filtered
}

func workerBlockerText(blocker string) string {
	blocker = strings.TrimSpace(blocker)
	switch strings.ToLower(blocker) {
	case "", "none", "no", "n/a", "na", "null", "nil", "no blocker", "not applicable":
		return ""
	default:
		return blocker
	}
}

func hashWorkerPatch(patch coding.WorkerPatch) string {
	h := sha256.New()
	h.Write([]byte(patch.Patch))
	for _, file := range patch.Files {
		h.Write([]byte{0})
		h.Write([]byte(file.Path))
		h.Write([]byte{0})
		h.Write([]byte(file.Content))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func repairCycleActive(events []coding.TaskEvent) bool {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "repair_start", "repair_requested", "artifact_validation":
			return true
		case "approval", "worker_start":
			return false
		}
	}
	return false
}

func detectIdenticalRepair(currentPatch coding.WorkerPatch, events []coding.TaskEvent) bool {
	currentHash := hashWorkerPatch(currentPatch)
	if currentHash == "" {
		return false
	}
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "worker_patch_attempt":
			if workerPatchAttemptHash(events[i]) == currentHash {
				return true
			}
		case "approval", "worker_start":
			return false
		}
	}
	return false
}

func workerPatchAttemptHash(event coding.TaskEvent) string {
	if len(event.Payload) == 0 {
		return ""
	}
	var payload struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Hash)
}

func (m model) recordWorkerPatchAttempt(taskID string, patch coding.WorkerPatch) {
	payload, _ := json.Marshal(struct {
		Hash    string `json:"hash"`
		Summary string `json:"summary,omitempty"`
	}{
		Hash:    hashWorkerPatch(patch),
		Summary: strings.TrimSpace(patch.Summary),
	})
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  taskID,
		Type:    "worker_patch_attempt",
		Message: "Worker patch attempt fingerprint recorded.",
		Payload: payload,
	})
}

func workerPatchPaths(patch coding.WorkerPatch) []string {
	if len(patch.Files) > 0 {
		return coding.WorkerFileEditPaths(patch.Files)
	}
	return coding.PatchPaths(patch.Patch)
}

func (m model) tryAutoCorrectPathMismatch(task coding.Task, patch coding.WorkerPatch, returnedPaths, allowedPaths []string) (coding.WorkerPatch, bool) {
	if len(returnedPaths) != 1 || len(allowedPaths) != 1 {
		return coding.WorkerPatch{}, false
	}
	if !singleFileGeneratedArtifactTask(task) && !generatedCodeArtifactTask(task) {
		return coding.WorkerPatch{}, false
	}
	returnedPath := strings.TrimSpace(filepath.ToSlash(returnedPaths[0]))
	allowedPath := strings.TrimSpace(filepath.ToSlash(allowedPaths[0]))
	if returnedPath == "" || allowedPath == "" || filepath.Ext(returnedPath) != filepath.Ext(allowedPath) {
		return coding.WorkerPatch{}, false
	}
	returnedBase := strings.TrimSuffix(filepath.Base(returnedPath), filepath.Ext(returnedPath))
	allowedBase := strings.TrimSuffix(filepath.Base(allowedPath), filepath.Ext(allowedPath))
	if !pathsSimilar(returnedBase, allowedBase) {
		return coding.WorkerPatch{}, false
	}
	fullAllowedPath := filepath.Join(m.project.Root, filepath.FromSlash(allowedPath))
	if info, err := os.Stat(fullAllowedPath); err == nil && info.Size() > 0 {
		events, _ := m.store.TaskEvents(task.ID)
		if !repairCycleActive(events) {
			return coding.WorkerPatch{}, false
		}
	}

	corrected := patch
	if len(patch.Files) > 0 {
		corrected.Files = make([]coding.WorkerFileEdit, len(patch.Files))
		for i, file := range patch.Files {
			corrected.Files[i] = file
			if filepath.ToSlash(strings.TrimSpace(file.Path)) == returnedPath {
				corrected.Files[i].Path = allowedPath
			}
		}
		return corrected, true
	}
	if strings.TrimSpace(patch.Patch) != "" {
		corrected.Patch = strings.ReplaceAll(patch.Patch, returnedPath, allowedPath)
		corrected.Patch = strings.ReplaceAll(corrected.Patch, "a/"+returnedPath, "a/"+allowedPath)
		corrected.Patch = strings.ReplaceAll(corrected.Patch, "b/"+returnedPath, "b/"+allowedPath)
		return corrected, true
	}
	return coding.WorkerPatch{}, false
}

func pathsSimilar(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if a == b || a+"s" == b || a == b+"s" {
		return true
	}
	return levenshteinDistance(a, b) <= 2
}

func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minInt(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func minInt(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
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

func (m model) generateWorkerPatchJSON(ctx context.Context, packet coding.TaskPacket) (string, llm.Usage, error) {
	client := llm.New(m.cfg.ProviderForRole("worker"))
	maxTokens := m.workerOutputTokens(packet)
	return client.CompleteWithUsageGuard(ctx, workerPatchMessages(packet), maxTokens, m.workerOutputGuard(maxTokens))
}

func (m model) repairWorkerPatchJSON(ctx context.Context, packet coding.TaskPacket, raw string, parseErr error) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("worker"))
	maxTokens := m.workerOutputTokens(packet)
	content, _, err := client.CompleteWithUsageGuard(ctx, workerPatchRepairMessages(packet, raw, parseErr), maxTokens, m.workerOutputGuard(maxTokens))
	return content, err
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
	maxTokens := m.workerOutputTokens(packet)
	raw, _, err := client.CompleteWithUsageGuard(ctx, workerPatchDiffRepairMessages(packet, patch, applyErr), maxTokens, m.workerOutputGuard(maxTokens))
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
				strings.Join(workerBraidContract(), "\n"),
				profile,
				"Return a WorkerPatch JSON object.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"If context_files includes a file you are editing, treat that file as existing and preserve all unrelated content.",
				"For cohesive whole-file artifact tasks, prefer files with complete content for every allowed output file. Use patch only for small edits to existing files. When using files, set patch to an empty string.",
				"For single-file generated artifact tasks, files[] with complete content for the one allowed file is required and patch must be empty. Do not produce unified diffs for standalone generated outputs.",
				"Maintainability matters: keep generated code modular and readable. Prefer files around 300 lines or less. If a requested implementation will be much larger and the task allows multiple files, split responsibilities across the allowed files. If the task only allows one file and the result would be oversized, return a blocker asking for the task to be split unless the task explicitly requires one file.",
				"Treat interface_contract and dependency_contracts in the task packet as binding API specs. Implement your own interface_contract exactly, and when importing dependency modules, use the dependency_contracts instead of inventing constructor arguments, methods, attributes, or import paths.",
				"Do not repeat code blocks or state-reset assignments. Each method body should contain each logical statement once unless repetition is explicitly required by the task. If you catch yourself repeating the same block, stop and return a blocker instead of continuing.",
				"For generated module code, use explicit imports between local modules. Do not use wildcard imports such as from module import *; they hide interfaces from static validation and downstream workers.",
				"Every file edit must be an object inside the files array: {\"path\":\"relative/path\",\"content\":\"full file content\"}. Do not put path/content pairs outside an object.",
				"For existing files, prefer a focused unified diff in patch and leave files empty.",
				"Use files with full replacement content only for new files, create-only tasks, or explicit whole-file rewrites.",
				"Follow the task packet artifact contract exactly. HTML fragments must not include doctype/html/head/body. Final index.html tasks must include a complete document. CSS must be plain browser CSS; normal descendant selectors, pseudo-classes, and pseudo-elements are allowed.",
				"For assembly/wiring tasks, use provided context_files from dependency outputs as source material and preserve their exact copy, asset filenames, commands, and URLs.",
				"Do not shorten user-provided copy with ellipses or substitute invented repo URLs, filenames, or commands.",
				"Never replace real existing file content with placeholders such as existing content, rest of file, omitted for brevity, previous content here, or unchanged content comments.",
				"If existing context contains placeholder sentinel comments, replace them with real task output instead of preserving them.",
				"File content must not include diff marker residue such as leading + or - characters before HTML tags.",
				"If the task is complete, set blocker to an empty string.",
				"If you need missing context or cannot safely complete the task, set blocker to a clear explanation and leave patch/files empty.",
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
				strings.Join(workerDiffRepairBraidContract(), "\n"),
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"Keep the same task_id.",
				"For cohesive whole-file artifact tasks, prefer files with complete content for every allowed output file. Use patch only for small edits to existing files. When using files, set patch to an empty string.",
				"For single-file generated artifact tasks, files[] with complete content for the one allowed file is required and patch must be empty. Do not repair or return a unified diff.",
				"Maintainability matters: keep generated code modular and readable. Prefer files around 300 lines or less. If a requested implementation will be much larger and the task allows multiple files, split responsibilities across the allowed files. If the task only allows one file and the result would be oversized, return a blocker asking for the task to be split unless the task explicitly requires one file.",
				"Do not repeat code blocks or state-reset assignments. Each method body should contain each logical statement once unless repetition is explicitly required by the task. If you catch yourself repeating the same block, stop and return a blocker instead of continuing.",
				"For generated module code, use explicit imports between local modules. Do not use wildcard imports such as from module import *; they hide interfaces from static validation and downstream workers.",
				"Every file edit must be an object inside the files array: {\"path\":\"relative/path\",\"content\":\"full file content\"}. Do not put path/content pairs outside an object.",
				"For existing files, prefer a corrected unified diff in patch and leave files empty.",
				"Use files with full replacement content only for new files, create-only tasks, or explicit whole-file rewrites.",
				"Follow the task packet artifact contract exactly. HTML fragments must not include doctype/html/head/body. Final index.html tasks must include a complete document. CSS must be plain browser CSS; normal descendant selectors, pseudo-classes, and pseudo-elements are allowed.",
				"For assembly/wiring tasks, use provided context_files from dependency outputs as source material and preserve their exact copy, asset filenames, commands, and URLs.",
				"Do not shorten user-provided copy with ellipses or substitute invented repo URLs, filenames, or commands.",
				"Never replace real existing file content with placeholders such as existing content, rest of file, omitted for brevity, previous content here, or unchanged content comments.",
				"If existing context contains placeholder sentinel comments, replace them with real task output instead of preserving them.",
				"File content must not include diff marker residue such as leading + or - characters before HTML tags.",
				"If the task is complete, set blocker to an empty string.",
				"Set blocker to a clear explanation if you cannot safely repair it.",
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
				strings.Join(workerBraidContract(), "\n"),
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"Use the task_id from the task packet.",
				"For cohesive whole-file artifact tasks, prefer files with complete content for every allowed output file. Use patch only for small edits to existing files. When using files, set patch to an empty string.",
				"For single-file generated artifact tasks, files[] with complete content for the one allowed file is required and patch must be empty, especially during repair.",
				"Maintainability matters: keep generated code modular and readable. Prefer files around 300 lines or less. If a requested implementation will be much larger and the task allows multiple files, split responsibilities across the allowed files. If the task only allows one file and the result would be oversized, return a blocker asking for the task to be split unless the task explicitly requires one file.",
				"Do not repeat code blocks or state-reset assignments. Each method body should contain each logical statement once unless repetition is explicitly required by the task. If you catch yourself repeating the same block, stop and return a blocker instead of continuing.",
				"For generated module code, use explicit imports between local modules. Do not use wildcard imports such as from module import *; they hide interfaces from static validation and downstream workers.",
				"Every file edit must be an object inside the files array: {\"path\":\"relative/path\",\"content\":\"full file content\"}. Do not put path/content pairs outside an object.",
				"For existing files, prefer a focused unified diff in patch and leave files empty.",
				"Use files with full replacement content only for new files, create-only tasks, or explicit whole-file rewrites.",
				"Follow the task packet artifact contract exactly. HTML fragments must not include doctype/html/head/body. Final index.html tasks must include a complete document. CSS must be plain browser CSS; normal descendant selectors, pseudo-classes, and pseudo-elements are allowed.",
				"For assembly/wiring tasks, use provided context_files from dependency outputs as source material and preserve their exact copy, asset filenames, commands, and URLs.",
				"Do not shorten user-provided copy with ellipses or substitute invented repo URLs, filenames, or commands.",
				"Never replace real existing file content with placeholders such as existing content, rest of file, omitted for brevity, previous content here, or unchanged content comments.",
				"If existing context contains placeholder sentinel comments, replace them with real task output instead of preserving them.",
				"File content must not include diff marker residue such as leading + or - characters before HTML tags.",
				"If the task is complete, set blocker to an empty string.",
				"If a safe patch is not possible, set blocker to a clear explanation and leave patch/files empty.",
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
