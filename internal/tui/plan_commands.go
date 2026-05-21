package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/project"
)

const maxPlanGenerateTokens = 8192

func (m model) approveLatestPlan(runAfterApproval bool) (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Approve error: " + err.Error())
		m.status = "approve failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan to approve. Use `/plan draft <title>` or `/plan import <json>` first.")
		m.status = "no plan"
		return m, nil, true
	}
	if plan.Status != coding.PlanStatusDraft {
		m.addSystemNote(fmt.Sprintf("Plan %s is %s, not draft.", plan.Title, plan.Status))
		m.status = "plan not draft"
		return m, nil, true
	}
	if issues := coding.ValidatePlanQuality(plan); len(issues) > 0 {
		m.addSystemNote("Plan quality check failed:\n" + renderPlanQualityIssues(issues) + "\n\nUse `/plan edit`, `/attach`, or `/plan validate` before approving.")
		m.status = "approval blocked"
		return m, nil, true
	}
	if err := m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusApproved); err != nil {
		m.addSystemNote("Approve error: " + err.Error())
		m.status = "approve failed"
		return m, nil, true
	}
	for _, task := range plan.Tasks {
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "approval",
			Message: "Plan approved by user; task is eligible for worker packet generation.",
		})
	}
	plan.Status = coding.PlanStatusApproved
	m.addSystemNote(renderPlan(plan))
	m.status = "plan approved"
	if runAfterApproval {
		return m.runParallelWorkers()
	}
	return m, nil, true
}

func approveShouldRun(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "run", "workers", "run-workers", "start", "go":
		return true
	default:
		return false
	}
}

func (m model) planValidationCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Plan validation error: " + err.Error()
	}
	if !ok {
		return "No plan. Use `/plan draft`, `/plan generate`, or `/plan import` first."
	}
	issues := coding.ValidatePlanQuality(plan)
	if len(issues) == 0 {
		return "Plan validation passed.\n\n" + renderPlan(plan)
	}
	return "Plan validation failed:\n" + renderPlanQualityIssues(issues)
}

func renderPlanQualityIssues(issues []coding.PlanQualityIssue) string {
	var b strings.Builder
	for i, issue := range issues {
		title := strings.TrimSpace(issue.TaskTitle)
		if title == "" {
			title = strings.TrimSpace(issue.TaskID)
		}
		if title == "" {
			title = "plan"
		}
		fmt.Fprintf(&b, "%d. [%s] %s: %s\n", i+1, issue.Severity, title, issue.Message)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) rejectLatestPlan(reason string) (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Reject error: " + err.Error())
		m.status = "reject failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan to reject.")
		m.status = "no plan"
		return m, nil, true
	}
	if reason == "" {
		reason = "Plan rejected by user."
	}
	if err := m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusBlocked); err != nil {
		m.addSystemNote("Reject error: " + err.Error())
		m.status = "reject failed"
		return m, nil, true
	}
	for _, task := range plan.Tasks {
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{TaskID: task.ID, Type: "rejection", Message: reason})
	}
	plan.Status = coding.PlanStatusBlocked
	m.addSystemNote(renderPlan(plan) + "\n\nRejection: " + reason)
	m.status = "plan rejected"
	return m, nil, true
}

func (m model) handlePlanCommand(args []string, rawArgs string) (tea.Model, tea.Cmd, bool) {
	if len(args) > 0 && strings.ToLower(args[0]) == "generate" {
		request := strings.TrimSpace(strings.TrimPrefix(rawArgs, args[0]))
		return m.generatePlanCommand(request)
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "replan" {
		guidance := strings.TrimSpace(strings.TrimPrefix(rawArgs, args[0]))
		return m.replanCommand(guidance)
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "edit" {
		return m.editPlanCommand(strings.TrimSpace(strings.TrimPrefix(rawArgs, args[0])))
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "validate" {
		m.setIDEView("plan validation", m.planValidationCommandText())
		return m, nil, true
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "draft" {
		title := strings.TrimSpace(strings.Join(args[1:], " "))
		if title == "" {
			title = "Draft coding plan"
		}
		plan := coding.Plan{
			ID:          uuid.NewString(),
			SessionID:   m.session.ID,
			ProjectRoot: m.project.Root,
			Title:       title,
			Summary:     "Manual draft plan. Replace this with orchestrator-generated steps in the next phase increment.",
			Status:      coding.PlanStatusDraft,
			Tasks: []coding.Task{
				{
					ID:     uuid.NewString(),
					Title:  "Define implementation steps",
					Goal:   title,
					Status: coding.TaskStatusPending,
					AcceptanceChecks: []coding.AcceptanceCheck{
						{Description: "Plan is reviewed and approved before worker execution."},
					},
				},
			},
		}
		plan.Tasks[0].PlanID = plan.ID
		if err := m.store.SavePlan(plan); err != nil {
			m.err = err.Error()
			return m, nil, true
		}
		if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
			plan = saved
		}
		m.writeRunArtifact("plan", plan)
		m.addSystemNote(renderPlan(plan))
		m.status = "draft plan created"
		return m, nil, true
	}
	if len(args) > 0 && strings.ToLower(args[0]) == "import" {
		raw := strings.TrimSpace(strings.TrimPrefix(rawArgs, args[0]))
		if raw == "" {
			m.addSystemNote("Usage: /plan import <json>")
			m.status = "plan import usage"
			return m, nil, true
		}
		plan, err := coding.DecodePlanJSON([]byte(raw))
		if err != nil {
			m.addSystemNote("Plan import error: " + err.Error())
			m.status = "plan import failed"
			return m, nil, true
		}
		plan = coding.PrepareImportedPlan(plan, m.session.ID, m.project.Root, uuid.NewString)
		normalizePlanVerification(&plan)
		if err := coding.ValidatePlan(plan); err != nil {
			m.addSystemNote("Plan import error: " + err.Error())
			m.status = "plan import failed"
			return m, nil, true
		}
		if err := m.store.SavePlan(plan); err != nil {
			m.err = err.Error()
			return m, nil, true
		}
		if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
			plan = saved
		}
		m.writeRunArtifact("plan", plan)
		m.addSystemNote(renderPlan(plan))
		m.status = "plan imported"
		return m, nil, true
	}
	m.setIDEView("plan", m.planCommandText())
	return m, nil, true
}

func (m model) editPlanCommand(raw string) (tea.Model, tea.Cmd, bool) {
	taskSelector, field, value, ok := splitPlanEditArgs(raw)
	if !ok {
		m.addSystemNote("Usage: /plan edit <task> <field> <value>\nFields: title, goal, allowed_paths, forbidden_paths, context_files, skills, depends_on, verification, checks.")
		m.status = "plan edit usage"
		return m, nil, true
	}
	plan, found, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Plan edit error: " + err.Error())
		m.status = "plan edit failed"
		return m, nil, true
	}
	if !found {
		m.addSystemNote("No plan. Use `/plan draft`, `/plan generate`, or `/plan import` first.")
		m.status = "no plan"
		return m, nil, true
	}
	if plan.Status != coding.PlanStatusDraft {
		m.addSystemNote("Only draft plans can be edited. Use `/plan` to inspect the current status.")
		m.status = "plan edit blocked"
		return m, nil, true
	}
	_, index, ok := selectTask(plan.Tasks, taskSelector)
	if !ok {
		m.addSystemNote(fmt.Sprintf("Task %q not found. Use `/tasks` to list task numbers and ids.", taskSelector))
		m.status = "task not found"
		return m, nil, true
	}
	if err := applyPlanTaskEdit(&plan.Tasks[index], field, value); err != nil {
		m.addSystemNote("Plan edit error: " + err.Error())
		m.status = "plan edit failed"
		return m, nil, true
	}
	if err := coding.ValidatePlan(plan); err != nil {
		m.addSystemNote("Plan edit validation error: " + err.Error())
		m.status = "plan edit failed"
		return m, nil, true
	}
	if err := m.store.SavePlan(plan); err != nil {
		m.addSystemNote("Plan edit save error: " + err.Error())
		m.status = "plan edit failed"
		return m, nil, true
	}
	if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
		plan = saved
	}
	m.writeRunArtifact("plan", plan)
	m.addSystemNote(renderPlan(plan))
	m.status = "plan edited"
	return m, nil, true
}

func (m model) attachFileCommand(raw string) (tea.Model, tea.Cmd, bool) {
	taskSelector, path, lineRange, ok := splitAttachArgs(raw)
	if !ok {
		m.addSystemNote("Usage: /attach [task] <path> [start-end]")
		m.status = "attach usage"
		return m, nil, true
	}
	clean, err := cleanPreviewPath(path)
	if err != nil {
		m.addSystemNote("Attach error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	}
	if info, err := os.Stat(filepath.Join(m.project.Root, clean)); err != nil {
		m.addSystemNote("Attach error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	} else if info.IsDir() {
		m.addSystemNote("Attach error: path is a directory")
		m.status = "attach failed"
		return m, nil, true
	}
	plan, found, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Attach error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	}
	if !found {
		m.addSystemNote("No plan. Use `/plan draft`, `/plan generate`, or `/plan import` first.")
		m.status = "no plan"
		return m, nil, true
	}
	if plan.Status != coding.PlanStatusDraft {
		m.addSystemNote("Only draft plans can be edited. Use `/plan` to inspect the current status.")
		m.status = "attach blocked"
		return m, nil, true
	}
	_, index, ok := selectTask(plan.Tasks, taskSelector)
	if !ok {
		m.addSystemNote(fmt.Sprintf("Task %q not found. Use `/tasks` to list task numbers and ids.", taskSelector))
		m.status = "task not found"
		return m, nil, true
	}
	spec := clean
	if lineRange != "" {
		spec += "#L" + lineRange
	}
	task := &plan.Tasks[index]
	task.ContextFiles = appendUnique(task.ContextFiles, spec)
	task.AllowedPaths = appendUnique(task.AllowedPaths, clean)
	if err := coding.ValidatePlan(plan); err != nil {
		m.addSystemNote("Attach validation error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	}
	if err := m.store.SavePlan(plan); err != nil {
		m.addSystemNote("Attach save error: " + err.Error())
		m.status = "attach failed"
		return m, nil, true
	}
	if saved, ok, err := m.store.LatestPlan(m.session.ID); err == nil && ok {
		plan = saved
	}
	m.writeRunArtifact("plan", plan)
	m.addSystemNote(fmt.Sprintf("Attached %s to task %s.", spec, plan.Tasks[index].Title))
	m.status = "file attached"
	return m, nil, true
}

func splitAttachArgs(raw string) (taskSelector, path, lineRange string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", "", "", false
	}
	if len(fields) == 1 {
		return "", fields[0], "", true
	}
	if looksTaskSelector(fields[0]) {
		taskSelector = fields[0]
		path = fields[1]
		if len(fields) > 2 {
			lineRange = normalizeLineRange(fields[2])
		}
		return taskSelector, path, lineRange, path != ""
	}
	path = fields[0]
	lineRange = normalizeLineRange(fields[1])
	return "", path, lineRange, path != ""
}

func looksTaskSelector(raw string) bool {
	if _, err := strconv.Atoi(raw); err == nil {
		return true
	}
	return strings.Contains(raw, "-")
}

func normalizeLineRange(raw string) string {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "L"))
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "-")
	if len(parts) == 2 {
		parts[0] = strings.TrimPrefix(strings.TrimSpace(parts[0]), "L")
		parts[1] = strings.TrimPrefix(strings.TrimSpace(parts[1]), "L")
		return parts[0] + "-L" + parts[1]
	}
	return raw
}

func splitPlanEditArgs(raw string) (taskSelector, field, value string, ok bool) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) < 3 {
		return "", "", "", false
	}
	taskSelector = parts[0]
	field = strings.ToLower(parts[1])
	valueStart := strings.Index(raw, parts[1])
	if valueStart < 0 {
		return "", "", "", false
	}
	value = strings.TrimSpace(raw[valueStart+len(parts[1]):])
	return taskSelector, field, value, value != ""
}

func applyPlanTaskEdit(task *coding.Task, field, value string) error {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(field)), "-", "_") {
	case "title":
		task.Title = strings.TrimSpace(value)
	case "goal":
		task.Goal = strings.TrimSpace(value)
	case "allowed", "allowed_paths", "paths":
		task.AllowedPaths = splitPlanEditList(value)
	case "forbidden", "forbidden_paths":
		task.ForbiddenPaths = splitPlanEditList(value)
	case "context", "context_files":
		task.ContextFiles = splitPlanEditList(value)
	case "skill", "skills":
		task.Skills = splitPlanEditList(value)
	case "depends", "depends_on", "dependencies":
		task.DependsOn = splitPlanEditList(value)
	case "verify", "verification":
		task.Verification = splitPlanEditList(value)
	case "checks", "acceptance", "acceptance_checks":
		items := splitPlanEditList(value)
		checks := make([]coding.AcceptanceCheck, 0, len(items))
		for _, item := range items {
			checks = append(checks, coding.AcceptanceCheck{Description: item})
		}
		task.AcceptanceChecks = checks
	default:
		return fmt.Errorf("unknown field %q", field)
	}
	return nil
}

func appendUnique(items []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return items
	}
	for _, existing := range items {
		if strings.TrimSpace(existing) == item {
			return items
		}
	}
	return append(items, item)
}

func splitPlanEditList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	sep := ","
	if strings.Contains(value, ";;") {
		sep = ";;"
	}
	parts := strings.Split(value, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (m model) generatePlanCommand(request string) (tea.Model, tea.Cmd, bool) {
	if strings.TrimSpace(request) == "" {
		m.addSystemNote("Usage: /plan generate <request>")
		m.status = "plan generate usage"
		return m, nil, true
	}
	m.thinking = true
	m.working.Spinner = spinner.Jump
	m.status = "generating plan"
	m.streamAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	m.modelRunID++
	m.activeModelRunID = m.modelRunID
	m.cancelModel = cancel
	return m, tea.Batch(m.generatePlanCmd(ctx, m.activeModelRunID, request), m.working.Tick), true
}

func (m model) replanCommand(guidance string) (tea.Model, tea.Cmd, bool) {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		m.addSystemNote("Plan replan error: " + err.Error())
		m.status = "plan replan failed"
		return m, nil, true
	}
	if !ok {
		m.addSystemNote("No plan. Use `/plan generate <request>` first.")
		m.status = "no plan"
		return m, nil, true
	}
	request, err := m.replanRequest(plan, guidance)
	if err != nil {
		m.addSystemNote("Plan replan error: " + err.Error())
		m.status = "plan replan failed"
		return m, nil, true
	}
	m.thinking = true
	m.working.Spinner = spinner.Jump
	m.status = "replanning"
	m.streamAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	m.modelRunID++
	m.activeModelRunID = m.modelRunID
	m.cancelModel = cancel
	return m, tea.Batch(m.generatePlanCmd(ctx, m.activeModelRunID, request), m.working.Tick), true
}

func (m model) generatePlanCmd(ctx context.Context, runID int, request string) tea.Cmd {
	return func() tea.Msg {
		raw, err := m.generatePlanJSON(ctx, request)
		if err != nil {
			return planGenerateMsg{runID: runID, request: request, err: err}
		}
		plan, err := m.planFromGeneratedJSON(raw)
		if err != nil {
			initialErr := err
			repaired, repairErr := m.repairPlanJSON(ctx, request, raw, initialErr)
			if repairErr != nil {
				return planGenerateMsg{runID: runID, request: request, raw: raw, err: fmt.Errorf("parse error: %v; repair error: %w", initialErr, repairErr)}
			}
			plan, err = m.planFromGeneratedJSON(repaired)
			if err != nil {
				return planGenerateMsg{runID: runID, request: request, raw: raw, err: fmt.Errorf("parse error: %v; repair parse error: %w", initialErr, err)}
			}
		}
		attachSourceCopyContract(&plan, request)
		if issues := coding.ValidatePlanQuality(plan); len(issues) > 0 {
			initialErr := fmt.Errorf("plan quality check failed:\n%s", renderPlanQualityIssues(issues))
			repaired, repairErr := m.repairPlanJSON(ctx, request, raw, initialErr)
			if repairErr != nil {
				return planGenerateMsg{runID: runID, request: request, raw: raw, err: fmt.Errorf("quality error: %v; repair error: %w", initialErr, repairErr)}
			}
			plan, err = m.planFromGeneratedJSON(repaired)
			if err != nil {
				return planGenerateMsg{runID: runID, request: request, raw: raw, err: fmt.Errorf("quality error: %v; repair parse error: %w", initialErr, err)}
			}
			attachSourceCopyContract(&plan, request)
			if issues := coding.ValidatePlanQuality(plan); len(issues) > 0 {
				return planGenerateMsg{runID: runID, request: request, raw: raw, err: fmt.Errorf("quality error: %v; repaired plan still has quality issues:\n%s", initialErr, renderPlanQualityIssues(issues))}
			}
		}
		return planGenerateMsg{runID: runID, request: request, raw: raw, plan: plan}
	}
}

func (m model) generatePlanJSON(ctx context.Context, request string) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("orchestrator"))
	return client.Complete(ctx, m.planGenerateMessages(request), maxPlanGenerateTokens)
}

func (m model) repairPlanJSON(ctx context.Context, request, raw string, parseErr error) (string, error) {
	client := llm.New(m.cfg.ProviderForRole("orchestrator"))
	return client.Complete(ctx, m.planRepairMessages(request, raw, parseErr), maxPlanGenerateTokens)
}

func (m model) planGenerateMessages(request string) []llm.ChatMessage {
	instructions, _, _ := project.LoadInstructions(m.project.Root)
	memories, _ := m.store.ProjectMemories(m.project.Root, 10)
	commands := project.DiscoverCommands(m.project.Root)
	skills, _ := project.DiscoverSkills(m.project.Root, m.cfg.Skills.Paths)
	var contextText strings.Builder
	fmt.Fprintf(&contextText, "Project root: %s\n", m.project.Root)
	if len(m.project.Languages) > 0 {
		fmt.Fprintf(&contextText, "Languages: %s\n", strings.Join(m.project.Languages, ", "))
	}
	worker := m.cfg.ProviderForRole("worker")
	workerProfile := m.workerCapacityProfile()
	fmt.Fprintf(&contextText, "\nConfigured worker:\nprovider: %s\nmodel: %s/%s\ncapacity: %s\nplanning constraint: %s\n",
		m.providerNameForRole("worker"),
		worker.Type,
		worker.Model,
		workerProfile.Label,
		workerProfile.Instruction,
	)
	if strings.TrimSpace(instructions.Content) != "" {
		fmt.Fprintf(&contextText, "\nProject instructions:\n%s\n", strings.TrimSpace(instructions.Content))
	}
	if len(commands) > 0 {
		contextText.WriteString("\nDiscovered commands:\n")
		for _, command := range commands {
			fmt.Fprintf(&contextText, "- %s\n", command)
		}
	}
	if files, err := projectFiles(m.project.Root, 80); err == nil && len(files) > 0 {
		contextText.WriteString("\nProject files:\n")
		for _, file := range files {
			fmt.Fprintf(&contextText, "- %s (%d bytes)\n", file.Path, file.Size)
		}
	}
	if previews := m.planReferencedFilePreviews(request); strings.TrimSpace(previews) != "" {
		fmt.Fprintf(&contextText, "\nReferenced file previews:\n%s\n", previews)
	}
	if len(memories) > 0 {
		contextText.WriteString("\nProject memory:\n")
		for _, memory := range memories {
			fmt.Fprintf(&contextText, "- %s: %s\n", memory.Key, memory.Value)
		}
	}
	if m.cfg.Skills.SkillsEnabled() && len(skills) > 0 {
		contextText.WriteString("\nDiscovered skills:\n")
		for _, skill := range skills {
			fmt.Fprintf(&contextText, "- %s", skill.Name)
			if strings.TrimSpace(skill.Description) != "" {
				fmt.Fprintf(&contextText, ": %s", skill.Description)
			}
			fmt.Fprintf(&contextText, " [%s]\n", skill.Source)
		}
	}
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You are the WeazlCode orchestrator.",
				strings.Join(orchestratorBraidContract(), "\n"),
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"title\":\"...\",\"summary\":\"...\",\"tasks\":[{\"id\":\"task-1\",\"title\":\"...\",\"goal\":\"...\",\"interface_contract\":{\"summary\":\"...\",\"exports\":[\"ClassName\"],\"imports\":[\"from path import ClassName\"],\"constructors\":[\"ClassName(arg: type)\"],\"methods\":[\"method(arg: type) -> type\"],\"attributes\":[\"name: type\"],\"commands\":[\"python main.py --smoke\"]},\"allowed_paths\":[\"...\"],\"forbidden_paths\":[\"...\"],\"context_files\":[\"...\"],\"skills\":[\"...\"],\"depends_on\":[],\"verification\":[\"...\"],\"acceptance_checks\":[{\"description\":\"...\",\"command\":\"...\"}]}]}",
				"Critical efficiency rule: if the request is a single static landing page, brochure page, one-page website, README, generated document, or other cohesive non-interactive artifact, return one whole-file artifact task unless the expected output clearly exceeds the configured worker output budget.",
				"Do not create microtasks for sections, cards, components, CSS modules, or validation just to create parallelism. Parallelism is only useful for genuinely independent deliverables.",
				"Do not create validation-only worker tasks with empty allowed_paths. Put validation requirements in acceptance_checks on the implementation tasks instead.",
				"Do not add README, docs, examples, tests, or companion files unless the user explicitly asks for them or they are required to run the requested artifact.",
				"Modularization north star: generated code should be maintainable modules with clear contracts. Prefer files around 300 lines or less. Files over 300 lines require a concrete reason in the task goal or acceptance_checks; files over 500 lines should usually be split into modules unless the user explicitly requests a single file or the artifact is inherently single-file.",
				"For interactive apps, games, APIs, CLIs, and tools, prefer module-first plans even when the request sounds like one cohesive artifact: separate domain logic, rendering/UI, persistence/adapters, entrypoint, and smoke/verification paths into small files that independent workers can own. Avoid giant all-in-one files for convenience.",
				"For generated interactive Python apps/games using pygame, do not return a single main.py task unless the user explicitly asks for one file. Use separate module tasks for entities/rendering/state plus an entrypoint task with --smoke.",
				"For generated module-first code, every code task must include interface_contract with exact exported names, constructor signatures, method signatures, attributes, import statements downstream modules should use, and smoke/verification commands when relevant. Keep the contract compact but precise enough that an 8B worker does not have to infer interfaces.",
				"Task goals should restate the most important interface contract details in plain language. Tell workers to use explicit imports, not wildcard imports such as from module import *, so validation and integration can see stable names.",
				"For module-first plans, maximize safe parallel draft work: create independent draft module tasks that can rely on explicit interface contracts in their goals, and use depends_on only when a task truly needs completed dependency output. Add a final wiring/smoke task that depends on the drafted modules and fixes integration mismatches.",
				"Final wiring/smoke tasks must be allowed to edit the entrypoint and the dependency module files they integrate. Do not restrict final integration to only main.py or an entrypoint when interface mismatches may require small edits in drafted modules.",
				"Intermediate dependent code tasks that compose generated modules should list dependency module files in context_files, not allowed_paths. They must consume the dependency interface_contracts as read-only contracts. Reserve cross-module edit rights for the final wiring/smoke task.",
				"For generated interactive Python apps/games, include a non-interactive --smoke path in the entrypoint and an acceptance check such as python main.py --smoke. The smoke path should initialize the app, perform one lightweight update/render or health check, and exit before the interactive loop.",
				"Every task must include a stable unique id such as task-1, task-2, task-3. depends_on must reference those exact ids only.",
				"Every task must be small enough for the configured local worker capacity and must include explicit allowed_paths.",
				"Allowed paths must be explicit files or narrow directories. Do not use '.', '*', repo-wide globs, or broad repository scopes.",
				"Choose the fewest tasks that still fit the configured worker. Do not split work just to create parallelism.",
				"For a single static landing page, brochure page, or content-heavy one-page site, prefer one cohesive artifact task that owns complete index.html and complete styles.css together, plus README.md only if requested. Split by file only when one task would exceed the worker output budget. Do not create separate validation-only tasks; use acceptance_checks. Do not create separate tasks for every section/card unless the user explicitly asks for separate partial files or the page is too large for one worker.",
				"When decomposing genuinely large files or multi-page apps, prefer tasks that create new modules/files without editing shared source files; add a later wiring task for shared files so independent work can run in parallel.",
				"For multi-file static websites, generated pages, dashboards, and other document-style outputs, use module-first plans only when the page is large enough to require it: create independent section/component/style files first, then use a final assembly task for the shared output file.",
				"For multi-file static websites, put HTML fragments under sections/*.html or components/*.html and CSS modules under styles/*.css; avoid root-level throwaway partials like _hero.html unless the existing project already uses that convention.",
				"HTML fragment/module/card/section tasks must explicitly say the worker must not include doctype, html, head, or body tags. Full index.html tasks must request a complete HTML document.",
				"CSS module tasks must request plain browser CSS only: no preprocessor-style nested rule blocks, no Sass/Less/PostCSS-only syntax, and no inline style blocks in HTML fragments. Normal descendant selectors, pseudo-classes, and pseudo-elements are allowed.",
				"Final assembly tasks should own only their final output path such as index.html or styles.css, depend on the fragment/module tasks, and verify the assembled output references the existing assets.",
				"Do not create long serial chains where many tasks repeatedly edit the same file; that is hostile to small local worker models.",
				"Goals must be concrete and describe the exact code or doc change expected. Do not return placeholder goals like 'do work', 'make changes', or 'implement feature'.",
				"When the user supplies copy, labels, asset filenames, repo URLs, commands, or other literal text, task goals and acceptance_checks must preserve the exact required strings instead of paraphrasing or summarizing them.",
				"Never use ellipses or shortened placeholder copy in generated site tasks unless the user explicitly supplied the ellipsis.",
				"Every task must include concrete acceptance_checks that can be reviewed against the diff. Final assembly tasks must include checks against the assembled output, not only fragment files.",
				"Final assembly tasks must use completed dependency outputs as source material and must preserve exact copy, image filenames, commands, and URLs from those dependency outputs.",
				"If a discovered skill is directly relevant, include its exact skill name in the task skills array. Otherwise leave skills empty.",
				"Use depends_on with task ids only when a task must wait for another task; leave it empty for independent work that can run in parallel.",
				"Use only discovered verification commands, or these allowlisted forms: go test/build/vet, npm test/run, python -m pytest/unittest/compileall, python script.py --smoke, pytest, cargo test/build/check/clippy, shellcheck, make test/check/lint/build.",
				"If no allowlisted verification applies, leave verification empty.",
			}, "\n"),
		},
		{
			Role:    "user",
			Content: strings.TrimSpace(contextText.String()) + "\n\nUser request:\n" + strings.TrimSpace(request),
		},
	}
}

func (m model) replanRequest(plan coding.Plan, guidance string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Replan the unfinished work for this existing WeazlCode plan.\n")
	fmt.Fprintf(&b, "Original plan title: %s\n", plan.Title)
	if strings.TrimSpace(plan.Summary) != "" {
		fmt.Fprintf(&b, "Original summary: %s\n", strings.TrimSpace(plan.Summary))
	}
	if strings.TrimSpace(guidance) != "" {
		fmt.Fprintf(&b, "\nUser replan guidance:\n%s\n", strings.TrimSpace(guidance))
	}
	b.WriteString("\nCompleted tasks are already done; do not repeat them except as context for later wiring tasks:\n")
	completed := 0
	for _, task := range plan.Tasks {
		if task.Status != coding.TaskStatusDone {
			continue
		}
		completed++
		fmt.Fprintf(&b, "- %s (%s): %s\n", task.ID, task.Title, task.Goal)
	}
	if completed == 0 {
		b.WriteString("- none\n")
	}
	b.WriteString("\nBlocked tasks and failure evidence; replace these with better-scoped tasks or omit impossible work:\n")
	blocked := 0
	for _, task := range plan.Tasks {
		if task.Status != coding.TaskStatusBlocked {
			continue
		}
		blocked++
		fmt.Fprintf(&b, "- %s (%s): %s\n", task.ID, task.Title, task.Goal)
		events, err := m.store.TaskEvents(task.ID)
		if err != nil {
			return "", err
		}
		for _, event := range latestPlanningRelevantEvents(events, 5) {
			fmt.Fprintf(&b, "  - %s: %s\n", event.Type, strings.TrimSpace(event.Message))
		}
	}
	if blocked == 0 {
		b.WriteString("- none\n")
	}
	b.WriteString("\nPending tasks from the old plan; keep only tasks that still make sense after accounting for completed and blocked work:\n")
	pending := 0
	for _, task := range plan.Tasks {
		if task.Status != coding.TaskStatusPending {
			continue
		}
		pending++
		fmt.Fprintf(&b, "- %s (%s): %s\n  allowed_paths: %s\n  depends_on: %s\n", task.ID, task.Title, task.Goal, strings.Join(task.AllowedPaths, ", "), strings.Join(task.DependsOn, ", "))
	}
	if pending == 0 {
		b.WriteString("- none\n")
	}
	b.WriteString("\nReturn a fresh draft plan containing only remaining useful work. Keep tasks small for the configured worker. Do not include tasks that require extracting code that does not exist. If a dark theme or design task is needed, specify concrete readable color goals instead of strict mathematical inversion.")
	return b.String(), nil
}

func latestPlanningRelevantEvents(events []coding.TaskEvent, limit int) []coding.TaskEvent {
	if limit <= 0 {
		limit = 5
	}
	relevant := map[string]bool{
		"worker_blocker":    true,
		"worker_error":      true,
		"worker_timeout":    true,
		"worker_json_error": true,
		"worker_rejected":   true,
		"reviewer_verdict":  true,
		"repair_requested":  true,
		"repair_limit":      true,
		"review_guardrail":  true,
	}
	var out []coding.TaskEvent
	for i := len(events) - 1; i >= 0 && len(out) < limit; i-- {
		if relevant[events[i].Type] {
			out = append(out, events[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (m model) planReferencedFilePreviews(request string) string {
	files, err := projectFiles(m.project.Root, 120)
	if err != nil {
		return ""
	}
	requestText := strings.ToLower(request)
	var matches []filePick
	for _, file := range files {
		path := strings.ToLower(file.Path)
		base := strings.ToLower(filepath.Base(file.Path))
		if strings.Contains(requestText, path) || (base != "" && strings.Contains(requestText, base)) {
			matches = append(matches, file)
		}
		if len(matches) >= 4 {
			break
		}
	}
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	for i, file := range matches {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(m.previewFileText(file.Path, 140))
	}
	return b.String()
}

func (m model) planRepairMessages(request, raw string, parseErr error) []llm.ChatMessage {
	worker := m.cfg.ProviderForRole("worker")
	workerProfile := m.workerCapacityProfile()
	return []llm.ChatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You repair WeazlCode plan JSON.",
				strings.Join(planRepairBraidContract(), "\n"),
				"Return only valid JSON. Do not wrap it in markdown fences.",
				"Use this exact shape: {\"title\":\"...\",\"summary\":\"...\",\"tasks\":[{\"id\":\"task-1\",\"title\":\"...\",\"goal\":\"...\",\"interface_contract\":{\"summary\":\"...\",\"exports\":[\"ClassName\"],\"imports\":[\"from path import ClassName\"],\"constructors\":[\"ClassName(arg: type)\"],\"methods\":[\"method(arg: type) -> type\"],\"attributes\":[\"name: type\"],\"commands\":[\"python main.py --smoke\"]},\"allowed_paths\":[\"...\"],\"forbidden_paths\":[\"...\"],\"context_files\":[\"...\"],\"skills\":[\"...\"],\"depends_on\":[],\"verification\":[\"...\"],\"acceptance_checks\":[{\"description\":\"...\",\"command\":\"...\"}]}]}",
				"Do not add unknown fields. Every task must include id, title, goal, and allowed_paths. depends_on must reference exact task ids in the same plan.",
				"Allowed paths must be explicit files or narrow directories. Goals and acceptance checks must be concrete enough for the configured local worker capacity.",
				"For module-first code plans, do not serialize independent module drafts just because one module will import another later. Draft modules in parallel using explicit interface contracts, then add a final wiring/smoke task that depends on the drafted modules.",
				"For generated interactive Python apps/games using pygame, do not repair into a single main.py task unless the user explicitly asks for one file. Use separate module tasks for entities/rendering/state plus an entrypoint task with --smoke.",
				"For generated module-first code, every code task must include interface_contract with exact exports, constructors, methods, attributes, downstream imports, and verification commands when relevant. Task goals should restate the critical contract details and instruct workers to avoid wildcard imports such as from module import *.",
				"Final wiring/smoke tasks must include the entrypoint and dependency module files in allowed_paths so interface mismatches can be fixed directly instead of papered over from the entrypoint.",
				"Intermediate dependent code tasks that compose generated modules should include dependency module files in context_files and consume their interface_contracts as read-only contracts. Do not add dependency module files to allowed_paths until the final wiring/smoke task.",
				"For generated interactive Python apps/games, include a non-interactive --smoke path in the entrypoint and an acceptance check such as python main.py --smoke.",
				workerProfile.Instruction,
				"Verification commands must be allowlisted; leave verification empty if unsure.",
			}, "\n"),
		},
		{
			Role: "user",
			Content: fmt.Sprintf("Configured worker:\nprovider: %s\nmodel: %s/%s\ncapacity: %s\n\nUser request:\n%s\n\nParser error:\n%s\n\nRaw response to repair:\n%s",
				m.providerNameForRole("worker"),
				worker.Type,
				worker.Model,
				workerProfile.Label,
				strings.TrimSpace(request),
				parseErr,
				strings.TrimSpace(raw),
			),
		},
	}
}

func (m model) planFromGeneratedJSON(raw string) (coding.Plan, error) {
	plan, err := coding.DecodePlanJSON([]byte(extractJSONObject(raw)))
	if err != nil {
		return coding.Plan{}, err
	}
	plan = coding.PrepareImportedPlan(plan, m.session.ID, m.project.Root, uuid.NewString)
	normalizePlanVerification(&plan)
	plan = coding.RepairPlanQuality(plan)
	if err := coding.ValidatePlan(plan); err != nil {
		return coding.Plan{}, err
	}
	return plan, nil
}

const sourceCopyContractPrefix = "Required source copy block (preserve exact wording; layout may change):"

func attachSourceCopyContract(plan *coding.Plan, request string) {
	copyBlock := sourceCopyBlockFromRequest(request)
	if copyBlock == "" {
		return
	}
	description := sourceCopyContractPrefix + "\n" + copyBlock
	for i := range plan.Tasks {
		if !taskWritesCopyBearingFile(plan.Tasks[i]) {
			continue
		}
		if taskHasAcceptanceDescription(plan.Tasks[i], sourceCopyContractPrefix) {
			continue
		}
		plan.Tasks[i].AcceptanceChecks = append(plan.Tasks[i].AcceptanceChecks, coding.AcceptanceCheck{
			Description: description,
		})
	}
}

func sourceCopyBlockFromRequest(request string) string {
	request = strings.TrimSpace(request)
	if request == "" {
		return ""
	}
	lower := strings.ToLower(request)
	markers := []string{
		"required copy:",
		"supplied copy:",
		"source copy:",
		"copy:",
	}
	for _, marker := range markers {
		idx := strings.LastIndex(lower, marker)
		if idx < 0 {
			continue
		}
		block := strings.TrimSpace(request[idx+len(marker):])
		if block == "" {
			continue
		}
		if len(block) > 8000 {
			block = strings.TrimSpace(block[:8000])
		}
		return block
	}
	return ""
}

func taskWritesCopyBearingFile(task coding.Task) bool {
	for _, path := range task.AllowedPaths {
		lower := strings.ToLower(strings.TrimSpace(path))
		switch {
		case strings.HasSuffix(lower, ".html"),
			strings.HasSuffix(lower, ".htm"),
			strings.HasSuffix(lower, ".md"),
			strings.HasSuffix(lower, ".txt"),
			strings.HasSuffix(lower, ".json"),
			strings.HasSuffix(lower, ".yaml"),
			strings.HasSuffix(lower, ".yml"):
			return true
		}
	}
	return false
}

func taskHasAcceptanceDescription(task coding.Task, needle string) bool {
	for _, check := range task.AcceptanceChecks {
		if strings.Contains(check.Description, needle) {
			return true
		}
	}
	return false
}

func extractJSONObject(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return raw
}

func (m model) planCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Plan error: " + err.Error()
	}
	if !ok {
		return "No plan yet. Use `/plan draft <title>` to create one."
	}
	return renderPlan(plan)
}

func (m model) tasksCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Task error: " + err.Error()
	}
	if !ok {
		return "No plan tasks yet. Use `/plan draft <title>` to create a draft plan."
	}
	if len(plan.Tasks) == 0 {
		return "Plan has no tasks."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Tasks for %s:\n", plan.Title)
	fmt.Fprintf(&b, "%s\n\n", taskProgressSummary(plan))
	for i, task := range plan.Tasks {
		fmt.Fprintf(&b, "%d. [%s] %s\n   %s\n", i+1, m.taskStatusLabel(task.Status), task.Title, task.Goal)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) taskDetailCommandText(selector string) string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Task detail error: " + err.Error()
	}
	if !ok || len(plan.Tasks) == 0 {
		return "No task detail yet. Use `/plan draft <title>` or `/plan generate <request>`."
	}
	task, index, ok := selectTask(plan.Tasks, selector)
	if !ok {
		return fmt.Sprintf("Task %q not found. Use `/tasks` to list task numbers and ids.", selector)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Task %d/%d: %s\n", index+1, len(plan.Tasks), task.Title)
	fmt.Fprintf(&b, "id: %s\nstatus: %s\nplan: %s\n\nGoal:\n%s\n", task.ID, m.taskStatusLabel(task.Status), plan.Title, task.Goal)
	if len(task.AllowedPaths) > 0 {
		fmt.Fprintf(&b, "\nAllowed paths:\n%s", bulletList(task.AllowedPaths))
	}
	if len(task.ForbiddenPaths) > 0 {
		fmt.Fprintf(&b, "\nForbidden paths:\n%s", bulletList(task.ForbiddenPaths))
	}
	if len(task.ContextFiles) > 0 {
		fmt.Fprintf(&b, "\nContext files:\n%s", bulletList(task.ContextFiles))
	}
	if len(task.Skills) > 0 {
		fmt.Fprintf(&b, "\nSkills:\n%s", bulletList(task.Skills))
	}
	if len(task.DependsOn) > 0 {
		fmt.Fprintf(&b, "\nDepends on:\n%s", bulletList(task.DependsOn))
	}
	if len(task.Verification) > 0 {
		fmt.Fprintf(&b, "\nVerification:\n%s", bulletList(task.Verification))
	}
	if len(task.AcceptanceChecks) > 0 {
		b.WriteString("\nAcceptance checks:")
		for _, check := range task.AcceptanceChecks {
			label := strings.TrimSpace(check.Description)
			if label == "" {
				label = strings.TrimSpace(check.Command)
			}
			fmt.Fprintf(&b, "\n- %s", label)
		}
		b.WriteString("\n")
	}
	if packet, err := m.buildWorkerPacketForRun(task); err == nil {
		fmt.Fprintf(&b, "\nWorker packet:\n%s\n", renderJSON(packet))
	} else {
		fmt.Fprintf(&b, "\nWorker packet error: %s\n", err)
	}
	events, err := m.store.TaskEvents(task.ID)
	if err != nil {
		fmt.Fprintf(&b, "\nEvents error: %s", err)
		return strings.TrimRight(b.String(), "\n")
	}
	if len(events) == 0 {
		b.WriteString("\nEvents:\nNo events yet.")
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("\nEvents:")
	for _, event := range events {
		fmt.Fprintf(&b, "\n- %s %s: %s", event.CreatedAt.Format(time.RFC3339), event.Type, event.Message)
		if len(event.Payload) > 0 {
			fmt.Fprintf(&b, "\n  payload: %s", string(event.Payload))
		}
	}
	if verdict, ok := latestReviewVerdict(events); ok {
		fmt.Fprintf(&b, "\n\nLatest review: %s - %s", verdict.Verdict, verdict.Summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

func selectTask(tasks []coding.Task, selector string) (coding.Task, int, bool) {
	selector = strings.TrimSpace(selector)
	if selector != "" {
		if n, err := strconv.Atoi(selector); err == nil && n >= 1 && n <= len(tasks) {
			return tasks[n-1], n - 1, true
		}
		for i, task := range tasks {
			if task.ID == selector {
				return task, i, true
			}
		}
		return coding.Task{}, 0, false
	}
	for i, task := range tasks {
		if task.Status == coding.TaskStatusRunning || task.Status == coding.TaskStatusReviewing || task.Status == coding.TaskStatusBlocked {
			return task, i, true
		}
	}
	return tasks[0], 0, true
}

func bulletList(items []string) string {
	var b strings.Builder
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(item))
		}
	}
	return b.String()
}

func (m model) taskStatusLabel(status string) string {
	if status == coding.TaskStatusRunning || status == coding.TaskStatusReviewing {
		if spinnerFrame := strings.TrimSpace(m.working.View()); spinnerFrame != "" {
			return spinnerFrame + " " + status
		}
	}
	return status
}

func renderPlan(plan coding.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan: %s\nstatus: %s\nid: %s\nupdated: %s\n\n%s",
		plan.Title,
		plan.Status,
		plan.ID,
		plan.UpdatedAt.Format(time.RFC3339),
		plan.Summary,
	)
	if len(plan.Tasks) > 0 {
		b.WriteString("\n\nTasks:")
		for i, task := range plan.Tasks {
			fmt.Fprintf(&b, "\n%d. [%s] %s\n   %s", i+1, task.Status, task.Title, task.Goal)
			if len(task.AcceptanceChecks) > 0 {
				b.WriteString("\n   checks:")
				for _, check := range task.AcceptanceChecks {
					label := check.Description
					if label == "" {
						label = check.Command
					}
					fmt.Fprintf(&b, "\n   - %s", label)
				}
			}
			if len(task.Skills) > 0 {
				fmt.Fprintf(&b, "\n   skills: %s", strings.Join(task.Skills, ", "))
			}
			if len(task.DependsOn) > 0 {
				fmt.Fprintf(&b, "\n   depends_on: %s", strings.Join(task.DependsOn, ", "))
			}
		}
	}
	next := planNextActionText(plan)
	if next != "" {
		b.WriteString("\n\nNext:\n")
		b.WriteString(next)
	}
	return b.String()
}

func planNextActionText(plan coding.Plan) string {
	switch plan.Status {
	case coding.PlanStatusDraft:
		return "- Review the task scope, then run `/approve run` to approve and dispatch eligible local workers.\n- Use `/approve` if you want to approve without starting workers yet, or `/plan edit` / `/plan replan` if the plan needs adjustment."
	case coding.PlanStatusApproved:
		if hasRunnablePlanTask(plan) {
			return "- Run `/run-workers` to dispatch eligible local workers, or `/run-task` then `/run-worker` for one task at a time."
		}
	case coding.PlanStatusRunning:
		return "- Use `/tasks`, `/outputs`, `/diff`, and `/review-diff` to inspect progress."
	}
	return ""
}

func hasRunnablePlanTask(plan coding.Plan) bool {
	for _, task := range plan.Tasks {
		if task.Status == coding.TaskStatusPending || task.Status == coding.TaskStatusBlocked {
			return true
		}
	}
	return false
}
