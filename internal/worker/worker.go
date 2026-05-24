package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/providers"
)

// Worker executes bounded task packets.
type Worker struct {
	provider providers.Provider
	project  *project.Summary
}

// NewWorker creates a new worker with the given provider.
func NewWorker(provider providers.Provider, proj *project.Summary) *Worker {
	return &Worker{
		provider: provider,
		project:  proj,
	}
}

// ExecuteTask executes a single task and returns the result.
func (w *Worker) ExecuteTask(ctx context.Context, task *coding.Task) (*coding.TaskResult, error) {
	prompt := w.buildTaskPrompt(task)
	content, err := w.generate(ctx, prompt, 0.3, 8000)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}

	result, err := parseTaskResponse(content, task)
	if err != nil {
		return &coding.TaskResult{
			TaskID:      task.ID,
			Success:     false,
			Files:       map[string]string{},
			Patches:     map[string]string{},
			RawResponse: content,
			CompletedAt: time.Now(),
		}, fmt.Errorf("parse response: %w", err)
	}

	result.TaskID = task.ID
	result.RawResponse = content
	result.CompletedAt = time.Now()

	return result, nil
}

// buildTaskPrompt constructs the worker task prompt.
func (w *Worker) buildTaskPrompt(task *coding.Task) string {
	var b strings.Builder

	b.WriteString("You are a code worker for WeazlCode. Execute this bounded task precisely.\n\n")

	b.WriteString("TASK GOAL:\n")
	b.WriteString(task.Goal)
	b.WriteString("\n\n")

	b.WriteString("OUTPUT FILES:\n")
	for _, file := range task.OutputFiles {
		b.WriteString(fmt.Sprintf("- %s\n", file))
	}
	b.WriteString("\n")

	if len(task.ContextFiles) > 0 {
		b.WriteString("CONTEXT FILES (read these for reference):\n")
		for _, file := range task.ContextFiles {
			b.WriteString(fmt.Sprintf("- %s\n", file))
		}
		b.WriteString("\n")
	}

	b.WriteString("ALLOWED PATHS:\n")
	if len(task.AllowedPaths) > 0 {
		for _, path := range task.AllowedPaths {
			b.WriteString(fmt.Sprintf("- %s\n", path))
		}
	} else {
		b.WriteString(fmt.Sprintf("- %s (project root)\n", w.project.Root))
	}
	b.WriteString("\n")

	if len(task.AcceptanceChecks) > 0 {
		b.WriteString("ACCEPTANCE CRITERIA:\n")
		for _, check := range task.AcceptanceChecks {
			b.WriteString(fmt.Sprintf("- %s\n", check))
		}
		b.WriteString("\n")
	}

	if task.VerifyCommand != "" {
		b.WriteString("VERIFICATION COMMAND:\n")
		b.WriteString(task.VerifyCommand)
		b.WriteString("\n\n")
	}

	b.WriteString("CONSTRAINTS:\n")
	b.WriteString("- Only modify files within allowed paths\n")
	b.WriteString("- Preserve existing interfaces unless explicitly changing them\n")
	b.WriteString("- Write clean, idiomatic code\n")
	b.WriteString("- Include necessary imports and dependencies\n")
	b.WriteString("- Add basic error handling\n\n")
	b.WriteString("- If adding a smoke/test mode for a graphical app, make it safe in headless CI or terminal environments\n\n")

	if task.PreferFullFile {
		b.WriteString("OUTPUT FORMAT: Full file content\n")
		b.WriteString("Provide complete file content for each output file.\n\n")
	} else if task.PreferPatch {
		b.WriteString("OUTPUT FORMAT: Unified diff patches\n")
		b.WriteString("Provide unified diff patches for modifications.\n\n")
	} else {
		b.WriteString("OUTPUT FORMAT: Full file content (default)\n\n")
	}

	b.WriteString("Respond with files in this format:\n")
	b.WriteString("FILE: path/to/file.ext\n")
	b.WriteString("---\n")
	b.WriteString("file content here\n")
	b.WriteString("---\n")
	b.WriteString("\n")

	b.WriteString("Rules: start every file with FILE:, include the exact project-relative path, and provide ONLY file outputs with no markdown fences or explanation.")

	return b.String()
}

// ExecuteRepair executes a repair task based on review feedback.
func (w *Worker) ExecuteRepair(ctx context.Context, task *coding.Task, repair *coding.RepairPacket) (*coding.TaskResult, error) {
	prompt := w.buildRepairPrompt(task, repair)
	content, err := w.generate(ctx, prompt, 0.2, 6000)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}

	result, err := parseTaskResponse(content, task)
	if err != nil {
		return &coding.TaskResult{
			TaskID:      task.ID,
			Success:     false,
			Files:       map[string]string{},
			Patches:     map[string]string{},
			RawResponse: content,
			CompletedAt: time.Now(),
		}, fmt.Errorf("parse response: %w", err)
	}

	result.TaskID = task.ID
	result.RawResponse = content
	result.CompletedAt = time.Now()

	return result, nil
}

func (w *Worker) generate(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, error) {
	req := providers.CompletionRequest{
		Messages: []providers.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	var b strings.Builder
	if err := w.provider.Stream(ctx, req, func(chunk string) error {
		b.WriteString(chunk)
		return nil
	}); err != nil {
		if b.Len() > 0 {
			return b.String(), nil
		}
		return "", err
	}
	if b.Len() > 0 {
		return b.String(), nil
	}

	resp, err := w.provider.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// buildRepairPrompt constructs a repair task prompt.
func (w *Worker) buildRepairPrompt(task *coding.Task, repair *coding.RepairPacket) string {
	var b strings.Builder

	b.WriteString("You are fixing issues in a previous implementation. Make ONLY the necessary changes.\n\n")

	b.WriteString("ORIGINAL TASK:\n")
	b.WriteString(task.Goal)
	b.WriteString("\n\n")

	b.WriteString("ISSUES TO FIX:\n")
	for i, issue := range repair.Issues {
		b.WriteString(fmt.Sprintf("%d. [%s] %s: %s\n", i+1, issue.Severity, issue.Path, issue.Description))
	}
	b.WriteString("\n")

	b.WriteString("TARGET FILES:\n")
	for _, path := range repair.TargetPaths {
		b.WriteString(fmt.Sprintf("- %s\n", path))
	}
	b.WriteString("\n")

	b.WriteString("CURRENT FILE CONTENTS:\n")
	for _, path := range repair.TargetPaths {
		b.WriteString(renderCurrentFile(path, w.project.Root))
	}
	b.WriteString("\n")

	if repair.Instructions != "" {
		b.WriteString("REPAIR INSTRUCTIONS:\n")
		b.WriteString(repair.Instructions)
		b.WriteString("\n\n")
	}

	b.WriteString("Provide ONLY the corrected file content in the same format as before.\n")
	b.WriteString("Format every corrected file exactly as:\n")
	b.WriteString("FILE: path/to/file.ext\n---\ncomplete corrected file content\n---\n")
	b.WriteString("If the failure is a missing dependency, update an allowed project dependency manifest instead of pretending an import fixes installation.\n")
	b.WriteString("Make surgical changes - fix only what's broken.\n")

	return b.String()
}

func renderCurrentFile(path, root string) string {
	cleanPath := filepath.Clean(path)
	content, err := os.ReadFile(filepath.Join(root, cleanPath))
	if err != nil {
		return fmt.Sprintf("FILE: %s\n[unable to read current file: %v]\n\n", path, err)
	}
	text := string(content)
	if len(text) > 12000 {
		text = text[:12000] + "\n...[truncated]\n"
	}
	return fmt.Sprintf("FILE: %s\n---\n%s\n---\n\n", path, text)
}
