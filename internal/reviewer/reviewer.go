package reviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/providers"
)

// Reviewer assesses task results and provides feedback.
type Reviewer struct {
	provider providers.Provider
	project  *project.Summary
}

// NewReviewer creates a new reviewer with the given provider.
func NewReviewer(provider providers.Provider, proj *project.Summary) *Reviewer {
	return &Reviewer{
		provider: provider,
		project:  proj,
	}
}

// ReviewTask reviews a completed task and returns a verdict.
func (r *Reviewer) ReviewTask(ctx context.Context, task *coding.Task, result *coding.TaskResult, validationResult *coding.ValidationResult) (*coding.Review, error) {
	prompt := r.buildReviewPrompt(task, result, validationResult)

	req := providers.CompletionRequest{
		Messages: []providers.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   2000,
	}

	resp, err := r.provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}

	review, err := r.parseReviewResponse(resp.Content, task.ID)
	if err != nil {
		return nil, fmt.Errorf("parse review: %w", err)
	}

	review.ReviewedAt = time.Now()

	return review, nil
}

// buildReviewPrompt constructs the review prompt.
func (r *Reviewer) buildReviewPrompt(task *coding.Task, result *coding.TaskResult, validationResult *coding.ValidationResult) string {
	var b strings.Builder

	b.WriteString("You are a code reviewer for WeazlCode. Review this task implementation.\n\n")

	b.WriteString("TASK GOAL:\n")
	b.WriteString(task.Goal)
	b.WriteString("\n\n")

	b.WriteString("EXPECTED OUTPUTS:\n")
	for _, file := range task.OutputFiles {
		b.WriteString(fmt.Sprintf("- %s\n", file))
	}
	b.WriteString("\n")

	if len(task.AcceptanceChecks) > 0 {
		b.WriteString("ACCEPTANCE CRITERIA:\n")
		for _, check := range task.AcceptanceChecks {
			b.WriteString(fmt.Sprintf("- %s\n", check))
		}
		b.WriteString("\n")
	}

	b.WriteString("IMPLEMENTATION:\n")
	if len(result.Files) > 0 {
		b.WriteString("Files created/modified:\n")
		for path, content := range result.Files {
			b.WriteString(fmt.Sprintf("- %s\n", path))
			b.WriteString("```")
			b.WriteString(fileFenceLanguage(path))
			b.WriteString("\n")
			b.WriteString(trimReviewContent(content))
			b.WriteString("\n```\n")
		}
	}
	if len(result.Patches) > 0 {
		b.WriteString("Patches applied:\n")
		for path := range result.Patches {
			b.WriteString(fmt.Sprintf("- %s\n", path))
		}
	}
	b.WriteString("\n")

	if validationResult != nil {
		b.WriteString("VALIDATION RESULTS:\n")
		if validationResult.Valid {
			b.WriteString("✓ Validation passed\n")
		} else {
			b.WriteString("✗ Validation failed\n")
			for _, err := range validationResult.Errors {
				b.WriteString(fmt.Sprintf("  - %s\n", err))
			}
		}
		if len(validationResult.Warnings) > 0 {
			b.WriteString("Warnings:\n")
			for _, warn := range validationResult.Warnings {
				b.WriteString(fmt.Sprintf("  - %s\n", warn))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("REVIEW CRITERIA:\n")
	b.WriteString("- Does it meet the task goal?\n")
	b.WriteString("- Are all expected outputs present?\n")
	b.WriteString("- Does it pass acceptance checks?\n")
	b.WriteString("- Is the code quality acceptable?\n")
	b.WriteString("- Are there any critical issues?\n\n")
	b.WriteString("If deterministic validation and verification passed, approve unless you can cite a concrete issue in the shown file contents.\n")
	b.WriteString("Warnings are informational; do not request repair for warnings alone.\n")
	b.WriteString("For needs_fix, include at least one actionable issue with a path and specific description.\n\n")

	b.WriteString("Respond with a JSON verdict:\n")
	b.WriteString("```json\n")
	b.WriteString(`{
  "verdict": "approved|needs_fix|blocked",
  "issues": [
    {
      "path": "file/path.ext",
      "description": "Issue description",
      "severity": "critical|major|minor"
    }
  ],
  "repair_scope": "Brief description of what needs fixing",
  "escalate": false,
  "message": "Summary of review"
}
`)
	b.WriteString("```\n\n")

	b.WriteString("Verdicts:\n")
	b.WriteString("- approved: Implementation is acceptable, proceed\n")
	b.WriteString("- needs_fix: Has fixable issues, attempt local repair\n")
	b.WriteString("- blocked: Critical issues that need escalation or user input\n\n")

	b.WriteString("Set escalate=true only if the local worker cannot fix the issues.\n")
	b.WriteString("Respond with ONLY the JSON verdict, no additional text.")

	return b.String()
}

func trimReviewContent(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 12000 {
		return content
	}
	return content[:12000] + "\n...[truncated]"
}

func fileFenceLanguage(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".py"):
		return "python"
	case strings.HasSuffix(lower, ".go"):
		return "go"
	case strings.HasSuffix(lower, ".js"):
		return "javascript"
	case strings.HasSuffix(lower, ".ts"):
		return "typescript"
	case strings.HasSuffix(lower, ".html"):
		return "html"
	case strings.HasSuffix(lower, ".css"):
		return "css"
	case strings.HasSuffix(lower, ".json"):
		return "json"
	default:
		return ""
	}
}

// parseReviewResponse extracts the review from the model's response.
func (r *Reviewer) parseReviewResponse(response, taskID string) (*coding.Review, error) {
	// Extract JSON from response
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var reviewData struct {
		Verdict string `json:"verdict"`
		Issues  []struct {
			Path        string `json:"path"`
			Description string `json:"description"`
			Severity    string `json:"severity"`
		} `json:"issues"`
		RepairScope string `json:"repair_scope"`
		Escalate    bool   `json:"escalate"`
		Message     string `json:"message"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &reviewData); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}

	// Validate verdict
	verdict := strings.ToLower(reviewData.Verdict)
	if verdict != "approved" && verdict != "needs_fix" && verdict != "blocked" {
		return nil, fmt.Errorf("invalid verdict: %s", reviewData.Verdict)
	}

	// Convert issues
	issues := make([]coding.Issue, len(reviewData.Issues))
	for i, issueData := range reviewData.Issues {
		severity := strings.ToLower(issueData.Severity)
		if severity != "critical" && severity != "major" && severity != "minor" {
			severity = "major" // Default
		}

		issues[i] = coding.Issue{
			Path:        issueData.Path,
			Description: issueData.Description,
			Severity:    severity,
		}
	}

	return &coding.Review{
		TaskID:      taskID,
		Verdict:     verdict,
		Issues:      issues,
		RepairScope: reviewData.RepairScope,
		Escalate:    reviewData.Escalate,
		Message:     reviewData.Message,
	}, nil
}

// extractJSON extracts JSON from a response that may contain markdown code blocks.
func extractJSON(response string) string {
	// Try to find JSON in markdown code block
	if start := strings.Index(response, "```json"); start != -1 {
		start += 7 // len("```json")
		if end := strings.Index(response[start:], "```"); end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
	}

	// Try to find JSON in generic code block
	if start := strings.Index(response, "```"); start != -1 {
		start += 3
		if end := strings.Index(response[start:], "```"); end != -1 {
			content := strings.TrimSpace(response[start : start+end])
			// Check if it looks like JSON
			if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") {
				return content
			}
		}
	}

	// Try to find raw JSON
	trimmed := strings.TrimSpace(response)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return trimmed
	}

	return ""
}

// CreateRepairPacket creates a repair packet from review feedback.
func (r *Reviewer) CreateRepairPacket(task *coding.Task, review *coding.Review, attempt int) *coding.RepairPacket {
	// Extract target paths from issues
	targetPaths := make([]string, 0)
	pathSet := make(map[string]bool)

	for _, issue := range review.Issues {
		if issue.Path != "" && !pathSet[issue.Path] {
			targetPaths = append(targetPaths, issue.Path)
			pathSet[issue.Path] = true
		}
	}

	// If no specific paths, use all output files
	if len(targetPaths) == 0 {
		targetPaths = task.OutputFiles
	}

	return &coding.RepairPacket{
		TaskID:       task.ID,
		Issues:       review.Issues,
		TargetPaths:  targetPaths,
		ContextFiles: task.ContextFiles,
		Instructions: review.RepairScope,
		Attempt:      attempt,
	}
}

// ShouldEscalate determines if a task should be escalated based on review and attempts.
func (r *Reviewer) ShouldEscalate(review *coding.Review, attempts int, maxAttempts int) bool {
	// Escalate if reviewer explicitly requests it
	if review.Escalate {
		return true
	}

	// Escalate if blocked
	if review.Verdict == "blocked" {
		return true
	}

	// Escalate if max repair attempts reached
	if attempts >= maxAttempts {
		return true
	}

	// Check for critical issues
	for _, issue := range review.Issues {
		if issue.Severity == "critical" {
			return true
		}
	}

	return false
}
