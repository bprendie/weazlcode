package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
)

const (
	maxWorkerPatchDiffRepairAttempts       = 2
	maxLocalArtifactValidationRepairPasses = 2
	maxDeterministicArtifactRepairPasses   = 4
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
	if diff, err := m.gitDiff(); err == nil {
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
	return client.CompleteWithUsage(ctx, workerPatchMessages(packet), m.workerOutputTokens(packet))
}

func (m model) repairWorkerPatchJSON(ctx context.Context, packet coding.TaskPacket, raw string, parseErr error) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("worker"))
	return client.Complete(ctx, workerPatchRepairMessages(packet, raw, parseErr), m.workerOutputTokens(packet))
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
	raw, err := client.Complete(ctx, workerPatchDiffRepairMessages(packet, patch, applyErr), m.workerOutputTokens(packet))
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
				profile,
				"Return a WorkerPatch JSON object.",
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"If context_files includes a file you are editing, treat that file as existing and preserve all unrelated content.",
				"For cohesive whole-file artifact tasks, prefer files with complete content for every allowed output file. Use patch only for small edits to existing files. When using files, set patch to an empty string.",
				"For single-file generated artifact tasks, files[] with complete content for the one allowed file is required and patch must be empty. Do not produce unified diffs for standalone generated outputs.",
				"Maintainability matters: keep generated code modular and readable. Prefer files around 300 lines or less. If a requested implementation will be much larger and the task allows multiple files, split responsibilities across the allowed files. If the task only allows one file and the result would be oversized, return a blocker asking for the task to be split unless the task explicitly requires one file.",
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
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"Keep the same task_id.",
				"For cohesive whole-file artifact tasks, prefer files with complete content for every allowed output file. Use patch only for small edits to existing files. When using files, set patch to an empty string.",
				"For single-file generated artifact tasks, files[] with complete content for the one allowed file is required and patch must be empty. Do not repair or return a unified diff.",
				"Maintainability matters: keep generated code modular and readable. Prefer files around 300 lines or less. If a requested implementation will be much larger and the task allows multiple files, split responsibilities across the allowed files. If the task only allows one file and the result would be oversized, return a blocker asking for the task to be split unless the task explicitly requires one file.",
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
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"task_id\":\"...\",\"summary\":\"...\",\"patch\":\"...\",\"files\":[{\"path\":\"...\",\"content\":\"...\"}],\"blocker\":\"...\"}.",
				"Use the task_id from the task packet.",
				"For cohesive whole-file artifact tasks, prefer files with complete content for every allowed output file. Use patch only for small edits to existing files. When using files, set patch to an empty string.",
				"For single-file generated artifact tasks, files[] with complete content for the one allowed file is required and patch must be empty, especially during repair.",
				"Maintainability matters: keep generated code modular and readable. Prefer files around 300 lines or less. If a requested implementation will be much larger and the task allows multiple files, split responsibilities across the allowed files. If the task only allows one file and the result would be oversized, return a blocker asking for the task to be split unless the task explicitly requires one file.",
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
