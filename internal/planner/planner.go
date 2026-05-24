package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/providers"
)

// Planner creates implementation plans from user directives.
type Planner struct {
	provider providers.Provider
	project  *project.Summary
}

// NewPlanner creates a new planner with the given provider.
func NewPlanner(provider providers.Provider, proj *project.Summary) *Planner {
	return &Planner{
		provider: provider,
		project:  proj,
	}
}

// CreatePlan generates an implementation plan from a directive.
func (p *Planner) CreatePlan(ctx context.Context, directive string) (*coding.Plan, error) {
	prompt := p.buildPlanPrompt(directive)

	req := providers.CompletionRequest{
		Messages: []providers.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
		MaxTokens:   4000,
	}

	resp, err := p.provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}

	plan, err := p.parsePlanResponse(resp.Content, directive)
	if err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}

	return plan, nil
}

// buildPlanPrompt constructs the planning prompt.
func (p *Planner) buildPlanPrompt(directive string) string {
	var b strings.Builder

	b.WriteString("You are a technical planner for WeazlCode, a split-brain AI coding system.\n\n")
	b.WriteString("Your role is to create a structured implementation plan with bounded tasks.\n")
	b.WriteString("Each task will be executed by a local worker model, so tasks must be:\n")
	b.WriteString("- Concrete and specific\n")
	b.WriteString("- Self-contained with clear boundaries\n")
	b.WriteString("- Sized appropriately for an 8B-70B parameter model\n")
	b.WriteString("- Include exact file paths and context\n\n")

	b.WriteString("Project context:\n")
	b.WriteString(fmt.Sprintf("- Root: %s\n", p.project.Root))
	b.WriteString(fmt.Sprintf("- Name: %s\n", p.project.Name))
	if p.project.IsGitRepo {
		b.WriteString("- Git repository: yes\n")
	}
	b.WriteString("\n")

	b.WriteString("User directive:\n")
	b.WriteString(directive)
	b.WriteString("\n\n")

	b.WriteString("Create a plan with the following JSON structure:\n")
	b.WriteString("```json\n")
	b.WriteString(`{
  "tasks": [
    {
      "id": "task-1",
      "goal": "Clear description of what to build",
      "output_files": ["path/to/file.py"],
      "context_files": ["path/to/context.py"],
      "allowed_paths": ["path/to/directory/"],
      "acceptance_checks": ["File exists", "Contains function X"],
      "verify_command": "python -m py_compile path/to/file.py",
      "prefer_full_file": true,
      "prefer_patch": false,
      "dependencies": []
    }
  ]
}
`)
	b.WriteString("```\n\n")

	b.WriteString("Guidelines:\n")
	b.WriteString("- Use 1 task for a small standalone app, script, game, or page unless the user explicitly asks for multiple modules\n")
	b.WriteString("- Use 1-5 tasks for small features in existing codebases\n")
	b.WriteString("- Use 5-15 tasks for medium features\n")
	b.WriteString("- Split by stable interfaces, not arbitrary lines\n")
	b.WriteString("- Use project-relative paths only; never include the absolute project root in output_files, context_files, or allowed_paths\n")
	b.WriteString("- If the task requires third-party packages, include the project dependency manifest as an output file when one is appropriate\n")
	b.WriteString("- For Python apps with external imports, include requirements.txt or pyproject.toml unless the existing project already has dependency management\n")
	b.WriteString("- For new standalone apps, use allowed_paths [\".\"] when the worker may need source files plus dependency/config files\n")
	b.WriteString("- Include verification commands where applicable\n")
	b.WriteString("- For interactive tasks, require a non-interactive smoke/test path and set verify_command to run that path when possible\n")
	b.WriteString("- For graphical apps, the smoke verifier must be truly headless: use the platform's offscreen/dummy display env when needed\n")
	b.WriteString("- Verification commands must not install dependencies or modify system state; they should only check the generated project\n")
	b.WriteString("- Prefer full files for new code, patches for modifications\n")
	b.WriteString("- List dependencies between tasks (task IDs)\n")
	b.WriteString("- Make tasks parallelizable where possible\n\n")

	b.WriteString("Respond with ONLY the JSON plan, no additional text.")

	return b.String()
}

// parsePlanResponse extracts the plan from the model's response.
func (p *Planner) parsePlanResponse(response, directive string) (*coding.Plan, error) {
	// Extract JSON from response (handle markdown code blocks)
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var planData struct {
		Tasks []struct {
			ID               string   `json:"id"`
			Goal             string   `json:"goal"`
			OutputFiles      []string `json:"output_files"`
			ContextFiles     []string `json:"context_files"`
			AllowedPaths     []string `json:"allowed_paths"`
			AcceptanceChecks []string `json:"acceptance_checks"`
			VerifyCommand    string   `json:"verify_command"`
			PreferFullFile   bool     `json:"prefer_full_file"`
			PreferPatch      bool     `json:"prefer_patch"`
			Dependencies     []string `json:"dependencies"`
		} `json:"tasks"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &planData); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}

	if len(planData.Tasks) == 0 {
		return nil, fmt.Errorf("plan contains no tasks")
	}

	// Create plan
	plan := &coding.Plan{
		ID:        generatePlanID(),
		Directive: directive,
		Tasks:     make([]coding.Task, len(planData.Tasks)),
		CreatedAt: time.Now(),
	}

	// Convert tasks
	for i, taskData := range planData.Tasks {
		outputFiles := p.projectRelativePaths(taskData.OutputFiles)
		plan.Tasks[i] = coding.Task{
			ID:               taskData.ID,
			PlanID:           plan.ID,
			Goal:             taskData.Goal,
			OutputFiles:      outputFiles,
			ContextFiles:     p.projectRelativePaths(taskData.ContextFiles),
			AllowedPaths:     p.allowedPaths(taskData.AllowedPaths, outputFiles),
			AcceptanceChecks: taskData.AcceptanceChecks,
			VerifyCommand:    p.verifyCommand(taskData.VerifyCommand),
			PreferFullFile:   taskData.PreferFullFile,
			PreferPatch:      taskData.PreferPatch,
			Dependencies:     taskData.Dependencies,
			Status:           "pending",
			Attempts:         0,
		}
	}

	return plan, nil
}

func (p *Planner) allowedPaths(paths, outputs []string) []string {
	normalized := p.projectRelativePaths(paths)
	if len(normalized) > 0 {
		return normalized
	}
	if len(outputs) == 0 {
		return []string{"."}
	}
	return outputs
}

func (p *Planner) projectRelativePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		clean := p.projectRelativePath(path)
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

func (p *Planner) projectRelativePath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return "."
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(p.project.Root, path); err == nil {
			path = rel
		}
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if path == ".." || strings.HasPrefix(path, "../") {
		return ""
	}
	return strings.TrimPrefix(path, "./")
}

func (p *Planner) verifyCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	root := filepath.ToSlash(filepath.Clean(p.project.Root))
	prefixes := []string{
		"cd " + p.project.Root + " && ",
		"cd " + root + " && ",
		"cd \"" + p.project.Root + "\" && ",
		"cd \"" + root + "\" && ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(command, prefix) {
			command = strings.TrimSpace(strings.TrimPrefix(command, prefix))
			break
		}
	}
	return command
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

// generatePlanID generates a unique plan ID.
func generatePlanID() string {
	return fmt.Sprintf("plan-%d", time.Now().Unix())
}

// ValidatePlan checks if a plan is valid and ready for execution.
func (p *Planner) ValidatePlan(plan *coding.Plan) error {
	if plan.ID == "" {
		return fmt.Errorf("plan has no ID")
	}
	if len(plan.Tasks) == 0 {
		return fmt.Errorf("plan has no tasks")
	}

	// Check each task
	for i, task := range plan.Tasks {
		if task.ID == "" {
			return fmt.Errorf("task %d has no ID", i)
		}
		if task.Goal == "" {
			return fmt.Errorf("task %s has no goal", task.ID)
		}
		if len(task.OutputFiles) == 0 {
			return fmt.Errorf("task %s has no output files", task.ID)
		}
		if strings.Contains(task.VerifyCommand, p.project.Root) {
			return fmt.Errorf("task %s verify command must not include absolute project root", task.ID)
		}
		if strings.HasPrefix(strings.TrimSpace(task.VerifyCommand), "cd ") {
			return fmt.Errorf("task %s verify command must run from project root without cd", task.ID)
		}
		if strings.Contains(strings.ToLower(plan.Directive), "--smoke") &&
			task.VerifyCommand != "" &&
			!strings.Contains(task.VerifyCommand, "--smoke") {
			return fmt.Errorf("task %s verify command must exercise the requested smoke mode", task.ID)
		}

		// Validate dependencies exist
		for _, depID := range task.Dependencies {
			found := false
			for _, t := range plan.Tasks {
				if t.ID == depID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("task %s depends on non-existent task %s", task.ID, depID)
			}
		}
	}

	return nil
}
