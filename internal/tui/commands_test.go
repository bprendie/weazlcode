package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/storage"
	"github.com/bprendie/weazlcode/internal/tools"
)

func TestSlashHelpCommand(t *testing.T) {
	m := commandTestModel()
	m.input.SetValue("/help")
	updated, _, handled := m.handleSlashCommand("/help")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if got.input.Value() != "" {
		t.Fatalf("input = %q, want empty", got.input.Value())
	}
	if !strings.Contains(got.viewport.View(), "/files [query] - fuzzy-find project files") {
		t.Fatalf("viewport missing help: %q", got.viewport.View())
	}
}

func TestSlashCommandsShowsPalette(t *testing.T) {
	m := commandTestModel()
	updated, _, handled := m.handleSlashCommand("/commands")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	view := got.viewport.View()
	for _, want := range []string{"Command palette:", "Plan:", "/plan edit", "Worker:", "/run-worker"} {
		if !strings.Contains(view, want) {
			t.Fatalf("palette missing %q:\n%s", want, view)
		}
	}
	raw := commandPaletteText()
	for _, want := range []string{"Review:", "/review-diff", "Skills:", "/skills", "/run-workers"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("raw palette missing %q:\n%s", want, raw)
		}
	}
}

func TestSlashProjectCommand(t *testing.T) {
	m := commandTestModel()
	updated, _, handled := m.handleSlashCommand("/project")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if !strings.Contains(got.viewport.View(), "root: /tmp/weazlcode") {
		t.Fatalf("viewport missing project root: %q", got.viewport.View())
	}
	if got.status != "view project" || got.mode != modeIDEView {
		t.Fatalf("status/mode = %q/%v, want view project/%v", got.status, got.mode, modeIDEView)
	}
}

func TestSlashModelsCommand(t *testing.T) {
	m := commandTestModel()
	updated, _, handled := m.handleSlashCommand("/models")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if !strings.Contains(got.viewport.View(), "worker: local-ollama -> ollama/llama3.1") {
		t.Fatalf("viewport missing model roles: %q", got.viewport.View())
	}
}

func TestModelProviderBarShowsSplitBrainProviders(t *testing.T) {
	m := commandTestModel()
	m.cfg.Providers["planning-llm"] = config.Provider{Type: "anthropic", Model: "claude-test"}
	m.cfg.ModelRoles.Orchestrator = "planning-llm"
	m.cfg.ModelRoles.Reviewer = "planning-llm"
	bar := m.modelProviderBar()
	for _, want := range []string{"plan:anthropic/claude-test", "worker:ollama/llama3.1", "review:anthropic/claude-test"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("bar missing %q: %s", want, bar)
		}
	}
}

func TestThinkingViewShowsProcessingModel(t *testing.T) {
	m := commandTestModel()
	m.cfg.Providers["planning-llm"] = config.Provider{Type: "anthropic", Model: "claude-test"}
	m.cfg.ModelRoles.Orchestrator = "planning-llm"
	m.thinking = true
	m.status = "streaming orchestrator"
	m.streamAt = time.Now()
	view := m.thinkingView()
	if !strings.Contains(view, "[orchestrator:anthropic/claude-test]") {
		t.Fatalf("thinking view missing processing model: %q", view)
	}
}

func TestCodeChangeChatAutoRoutesToPlanGenerate(t *testing.T) {
	m := commandTestModel(t)
	m.input.SetValue("implement a settings page")
	updated, cmd := m.handleEnter()
	got := updated.(model)
	if cmd == nil || !got.thinking || got.status != "generating plan" {
		t.Fatalf("state = cmd:%v thinking:%t status:%q", cmd, got.thinking, got.status)
	}
	if strings.TrimSpace(got.input.Value()) != "" {
		t.Fatalf("input was not cleared: %q", got.input.Value())
	}
}

func TestReadOnlyChatDoesNotAutoRouteToPlanGenerate(t *testing.T) {
	for _, prompt := range []string{"look at ../weazl.world and summarize it", "explain how routing works"} {
		if shouldAutoGeneratePlan(prompt) {
			t.Fatalf("prompt should stay chat: %q", prompt)
		}
	}
	for _, prompt := range []string{"fix the routing bug", "write code for a settings page", "look at the repo and fix the failing test"} {
		if !shouldAutoGeneratePlan(prompt) {
			t.Fatalf("prompt should route to plan: %q", prompt)
		}
	}
}

func TestSlashDurableIDEViews(t *testing.T) {
	m := commandTestModel(t)
	for _, command := range []string{"/tools", "/skills", "/config", "/outputs", "/files"} {
		updated, _, handled := m.handleSlashCommand(command)
		if !handled {
			t.Fatalf("%s handled = false, want true", command)
		}
		got := updated.(model)
		if got.mode != modeIDEView {
			t.Fatalf("%s mode = %v, want modeIDEView", command, got.mode)
		}
		if !strings.HasPrefix(got.status, "view ") {
			t.Fatalf("%s status = %q", command, got.status)
		}
		if strings.TrimSpace(got.viewport.View()) == "" {
			t.Fatalf("%s viewport empty", command)
		}
		m = got
	}
	updated, _, handled := m.handleSlashCommand("/chat")
	if !handled {
		t.Fatal("/chat handled = false, want true")
	}
	got := updated.(model)
	if got.mode != modeChat || got.status != "chat" {
		t.Fatalf("mode/status = %v/%q, want chat", got.mode, got.status)
	}
}

func TestSlashSkillsCommandShowsDiscoveredSkills(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	skillDir := filepath.Join(root, ".weazlcode", "skills", "repo-style")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("name: repo-style\ndescription: Follow repo conventions.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.cfg.Skills.Paths = []string{".weazlcode/skills"}

	updated, _, handled := m.handleSlashCommand("/skills")
	if !handled {
		t.Fatal("skills handled = false")
	}
	view := updated.(model).viewport.View()
	for _, want := range []string{"repo-style", "Follow repo conventions.", ".weazlcode/skills"} {
		if !strings.Contains(view, want) {
			t.Fatalf("skills view missing %q:\n%s", want, view)
		}
	}
}

func TestSlashDiffCommand(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	updated, _, handled := m.handleSlashCommand("/diff")
	if !handled {
		t.Fatal("diff handled = false, want true")
	}
	got := updated.(model)
	if got.mode != modeIDEView || got.status != "view diff" {
		t.Fatalf("mode/status = %v/%q", got.mode, got.status)
	}
	view := got.viewport.View()
	if !strings.Contains(view, "Diff: 1 file(s)") || !strings.Contains(view, "1. README.md") {
		t.Fatalf("viewport missing diff: %q", view)
	}
}

func TestNonSlashCommandNotHandled(t *testing.T) {
	m := commandTestModel()
	_, _, handled := m.handleSlashCommand("hello")
	if handled {
		t.Fatal("handled = true, want false")
	}
}

func TestSlashPlanDraftCommand(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Add planner")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if got.status != "draft plan created" {
		t.Fatalf("status = %q, want draft plan created", got.status)
	}
	if !strings.Contains(got.viewport.View(), "Plan: Add planner") {
		t.Fatalf("viewport missing plan: %q", got.viewport.View())
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Title != "Add planner" || len(plan.Tasks) != 1 {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
}

func TestGeneratedPlanParsing(t *testing.T) {
	m := commandTestModel(t)
	raw := "```json\n{\"title\":\"Generated\",\"summary\":\"From model\",\"tasks\":[{\"title\":\"Task\",\"goal\":\"Do work\",\"allowed_paths\":[\"README.md\"],\"verification\":[\"go test ./...\"],\"acceptance_checks\":[{\"description\":\"checks pass\"}]}]}\n```"
	plan, err := m.planFromGeneratedJSON(raw)
	if err != nil {
		t.Fatalf("planFromGeneratedJSON: %v", err)
	}
	if plan.Title != "Generated" || plan.Status != coding.PlanStatusDraft || len(plan.Tasks) != 1 || plan.Tasks[0].Status != coding.TaskStatusPending {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestGeneratedPlanFiltersNonAllowlistedVerification(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Generated","summary":"From model","tasks":[{"title":"Task","goal":"Do work","allowed_paths":["README.md"],"verification":["grep -q ok README.md","go test ./..."],"acceptance_checks":[{"description":"checks pass"}]}]}`
	plan, err := m.planFromGeneratedJSON(raw)
	if err != nil {
		t.Fatalf("planFromGeneratedJSON: %v", err)
	}
	if !reflect.DeepEqual(plan.Tasks[0].Verification, []string{"go test ./..."}) {
		t.Fatalf("verification = %#v", plan.Tasks[0].Verification)
	}
}

func TestPhase3GeneratedPlanVerificationSmoke(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name:  "go",
			files: map[string]string{"go.mod": "module example.test\n"},
			want:  []string{"go build ./...", "go test ./..."},
		},
		{
			name:  "python",
			files: map[string]string{"pyproject.toml": "[project]\nname = \"example\"\n"},
			want:  []string{"python -m pytest"},
		},
		{
			name:  "no-build",
			files: map[string]string{"README.md": "# Example\n"},
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
					t.Fatalf("WriteFile %s: %v", name, err)
				}
			}
			m := commandTestModel(t)
			m.project.Root = root
			if got := m.defaultVerificationCommands(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("defaultVerificationCommands = %#v, want %#v", got, tt.want)
			}

			raw := `{"title":"Generated","summary":"From model","tasks":[{"title":"Task","goal":"Do work","allowed_paths":["README.md"],"verification":["grep -q ok README.md"],"acceptance_checks":[{"description":"checks pass"}]}]}`
			plan, err := m.planFromGeneratedJSON(raw)
			if err != nil {
				t.Fatalf("planFromGeneratedJSON: %v", err)
			}
			if len(plan.Tasks[0].Verification) != 0 {
				t.Fatalf("verification = %#v, want filtered empty", plan.Tasks[0].Verification)
			}
		})
	}
}

func TestGeneratedPlanRepairPrompt(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":123,"tasks":[{"name":"bad"}]}`
	_, parseErr := m.planFromGeneratedJSON(raw)
	if parseErr == nil {
		t.Fatal("planFromGeneratedJSON returned nil error for malformed response")
	}
	messages := m.planRepairMessages("change README", raw, parseErr)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"Return only valid JSON", "Do not add unknown fields", "Parser error", raw, "change README"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("repair messages missing %q:\n%s", want, combined)
		}
	}
	repaired := `{"title":"Generated","summary":"Repaired","tasks":[{"title":"Task","goal":"Do work","allowed_paths":["README.md"],"acceptance_checks":[{"description":"checks pass"}]}]}`
	plan, err := m.planFromGeneratedJSON(repaired)
	if err != nil {
		t.Fatalf("repaired plan parse: %v", err)
	}
	if plan.Title != "Generated" || len(plan.Tasks) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanGenerateMessagesIncludeProjectContext(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WEAZLCODE.md"), []byte("# Rules\n\nUse small tasks.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(root, ".weazlcode", "skills", "go-tests")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("name: go-tests\ndescription: Write focused Go tests.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.Languages = []string{"go"}
	m.cfg.Skills.Paths = []string{".weazlcode/skills"}
	if err := m.store.RememberProject(root, "style", "small patches", "project"); err != nil {
		t.Fatalf("RememberProject: %v", err)
	}
	messages := m.planGenerateMessages("change README")
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"Return only valid JSON", `"skills"`, "Use small tasks", "go test ./...", "style: small patches", "go-tests", "Write focused Go tests.", "change README"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("messages missing %q:\n%s", want, combined)
		}
	}
}

func TestPlanGenerateMessagesIncludeReferencedFilePreview(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html>\n<title>Weazl</title>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.project.Root = root
	messages := m.planGenerateMessages("modularize index.html")
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"Project files:", "index.html", "Referenced file previews:", "<title>Weazl</title>"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("messages missing %q:\n%s", want, combined)
		}
	}
}

func TestSlashPacketCommand(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Add packet")
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/packet")
	if !handled {
		t.Fatal("packet handled = false")
	}
	got := updated.(model)
	view := got.packetCommandText()
	if !strings.Contains(view, `"tools_allowed"`) || !strings.Contains(view, `"apply_patch"`) {
		t.Fatalf("viewport missing packet JSON: %q", view)
	}
	if !strings.Contains(view, `"context_policy"`) || !strings.Contains(view, `"tool_requested"`) {
		t.Fatalf("viewport missing context policy: %q", view)
	}
}

func TestSlashTasksCommandShowsProgress(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Progress Plan","summary":"Track work","tasks":[{"title":"Done","goal":"Finished","status":"done"},{"title":"Run","goal":"Running","status":"running"},{"title":"Next","goal":"Pending","status":"pending"}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/tasks")
	if !handled {
		t.Fatal("tasks handled = false")
	}
	got := updated.(model)
	view := got.viewport.View()
	if !strings.Contains(view, "Progress: 1/3 done") || !strings.Contains(view, "1 running") || !strings.Contains(view, "1 pending") {
		t.Fatalf("tasks missing progress: %q", view)
	}
}

func TestSlashTaskCommandShowsDetailPacketAndEvents(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	raw := `{"title":"Detail Plan","summary":"Track work","tasks":[{"title":"Update README","goal":"Change the README","allowed_paths":["README.md"],"context_files":["README.md"],"verification":["go test ./..."],"acceptance_checks":[{"description":"README changed"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/task 1")
	if !handled {
		t.Fatal("task handled = false")
	}
	got := updated.(model)
	view := got.taskDetailCommandText("1")
	for _, want := range []string{"Task 1/1: Update README", "Allowed paths:", "Worker packet:", `"context_files"`, "Events:", "worker_start"} {
		if !strings.Contains(view, want) {
			t.Fatalf("task detail missing %q:\n%s", want, view)
		}
	}
}

func TestSlashPlanImportCommand(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Imported","summary":"From orchestrator","tasks":[{"title":"Task","goal":"Do imported work","allowed_paths":["internal/coding"],"verification":["grep -q no README.md"],"acceptance_checks":[{"description":"checks pass"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if got.status != "plan imported" {
		t.Fatalf("status = %q, want plan imported", got.status)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Title != "Imported" || len(plan.Tasks) != 1 || plan.Tasks[0].PlanID != plan.ID {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	if len(plan.Tasks[0].Verification) != 0 {
		t.Fatalf("verification = %#v, want filtered empty", plan.Tasks[0].Verification)
	}
}

func TestSlashPlanEditCommandUpdatesDraftTask(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Editable","summary":"From orchestrator","tasks":[{"title":"Task","goal":"Do work","allowed_paths":["README.md"],"acceptance_checks":[{"description":"checks pass"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	commands := []string{
		"/plan edit 1 title Update README",
		"/plan edit 1 goal Replace old wording with Phase 2 wording",
		"/plan edit 1 allowed_paths README.md, docs/README.md",
		"/plan edit 1 context_files README.md",
		"/plan edit 1 skills go-tests,repo-style",
		"/plan edit 1 verification go test ./...",
		"/plan edit 1 checks README updated;;tests pass",
	}
	for _, command := range commands {
		updated, _, handled = m.handleSlashCommand(command)
		if !handled {
			t.Fatalf("%s handled = false", command)
		}
		m = updated.(model)
		if m.status != "plan edited" {
			t.Fatalf("%s status = %q, want plan edited", command, m.status)
		}
	}
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	task := plan.Tasks[0]
	if task.Title != "Update README" || task.Goal != "Replace old wording with Phase 2 wording" {
		t.Fatalf("task text = %#v", task)
	}
	if !reflect.DeepEqual(task.AllowedPaths, []string{"README.md", "docs/README.md"}) {
		t.Fatalf("allowed = %#v", task.AllowedPaths)
	}
	if !reflect.DeepEqual(task.ContextFiles, []string{"README.md"}) {
		t.Fatalf("context = %#v", task.ContextFiles)
	}
	if !reflect.DeepEqual(task.Skills, []string{"go-tests", "repo-style"}) {
		t.Fatalf("skills = %#v", task.Skills)
	}
	if !reflect.DeepEqual(task.Verification, []string{"go test ./..."}) {
		t.Fatalf("verification = %#v", task.Verification)
	}
	if len(task.AcceptanceChecks) != 2 || task.AcceptanceChecks[0].Description != "README updated" || task.AcceptanceChecks[1].Description != "tests pass" {
		t.Fatalf("checks = %#v", task.AcceptanceChecks)
	}
}

func TestSlashPlanEditCommandRequiresDraftPlan(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Locked","summary":"Lock plan","tasks":[{"title":"Update README","goal":"Update README.md to document the locked plan behavior.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README documents locked plan behavior"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/plan edit 1 title Should not edit")
	if !handled {
		t.Fatal("plan edit handled = false")
	}
	got := updated.(model)
	if got.status != "plan edit blocked" {
		t.Fatalf("status = %q, want plan edit blocked", got.status)
	}
}

func TestSlashApproveCommand(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Approve me","summary":"Approve plan","tasks":[{"title":"Update README","goal":"Update README.md to document the approval workflow.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README documents the approval workflow"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	got := updated.(model)
	if got.status != "plan approved" {
		t.Fatalf("status = %q, want plan approved", got.status)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Status != "approved" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "approval" {
		t.Fatalf("events = %#v", events)
	}
}

func TestSlashApproveBlocksLowQualityPlan(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Approve me")
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	got := updated.(model)
	if got.status != "approval blocked" {
		t.Fatalf("status = %q, want approval blocked", got.status)
	}
	if !strings.Contains(got.viewport.View(), "Plan quality check failed") || !strings.Contains(got.viewport.View(), "allowed_paths is empty") {
		t.Fatalf("viewport missing quality warning: %q", got.viewport.View())
	}
}

func TestSlashPlanValidateCommand(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Validate me")
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/plan validate")
	if !handled {
		t.Fatal("plan validate handled = false")
	}
	got := updated.(model)
	if got.status != "view plan validation" || !strings.Contains(got.viewport.View(), "Plan validation failed") {
		t.Fatalf("status/view = %q/%q", got.status, got.viewport.View())
	}
}

func TestSlashRejectCommand(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Reject me")
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/reject needs better scoping")
	if !handled {
		t.Fatal("reject handled = false")
	}
	got := updated.(model)
	if got.status != "plan rejected" {
		t.Fatalf("status = %q, want plan rejected", got.status)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Status != "blocked" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "rejection" || !strings.Contains(events[0].Message, "better scoping") {
		t.Fatalf("events = %#v", events)
	}
}

func TestSlashRunTaskRequiresApprovedPlan(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Needs approval")
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	got := updated.(model)
	if got.status != "plan not approved" {
		t.Fatalf("status = %q, want plan not approved", got.status)
	}
}

func TestSlashRunTaskMarksTaskRunning(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Run me","summary":"Run task","tasks":[{"title":"Update README","goal":"Update README.md to document the worker packet flow.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README documents the worker packet flow"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	got := updated.(model)
	if got.status != "task running" {
		t.Fatalf("status = %q, want task running", got.status)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Tasks[0].Status != "running" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 3 || events[1].Type != "task_baseline" || events[2].Type != "worker_start" || len(events[2].Payload) == 0 {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(got.viewport.View(), `"tools_allowed"`) {
		t.Fatalf("viewport missing worker packet: %q", got.viewport.View())
	}
}

func TestBuildWorkerPacketIncludesAttachedSkills(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	skillDir := filepath.Join(root, ".weazlcode", "skills", "repo-style")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("name: repo-style\ndescription: Follow repo conventions.\n\nPrefer local helpers."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.cfg.Skills.Paths = []string{".weazlcode/skills"}
	packet, err := m.buildWorkerPacket(coding.Task{
		ID:           "task-1",
		PlanID:       "plan-1",
		Title:        "Task",
		Goal:         "Update README.md to mention skills.",
		Status:       coding.TaskStatusPending,
		AllowedPaths: []string{"README.md"},
		Skills:       []string{"repo-style"},
	})
	if err != nil {
		t.Fatalf("buildWorkerPacket: %v", err)
	}
	if len(packet.Skills) != 1 || packet.Skills[0].Name != "repo-style" || !strings.Contains(packet.Skills[0].Content, "Prefer local helpers.") {
		t.Fatalf("skills = %#v", packet.Skills)
	}
}

func TestRunParallelWorkersStartsIndependentTasks(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Parallel","summary":"Run independent tasks","tasks":[{"id":"task-a","title":"A","goal":"Update README.md to mention A.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README mentions A"}]},{"id":"task-b","title":"B","goal":"Update docs/guide.md to mention B.","allowed_paths":["docs/guide.md"],"acceptance_checks":[{"description":"guide mentions B"}]},{"id":"task-c","title":"C","goal":"Update internal/app.go to mention C.","allowed_paths":["internal/app.go"],"depends_on":["task-a"],"acceptance_checks":[{"description":"app mentions C"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, cmd, handled := m.handleSlashCommand("/run-workers")
	if !handled {
		t.Fatal("run-workers handled = false")
	}
	got := updated.(model)
	if cmd == nil {
		t.Fatal("cmd = nil, want worker batch")
	}
	if got.status != "running 2 worker(s)" || len(got.workerRuns) != 2 {
		t.Fatalf("status/runs = %q/%#v", got.status, got.workerRuns)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	statusByTitle := map[string]string{}
	for _, task := range plan.Tasks {
		statusByTitle[task.Title] = task.Status
	}
	if statusByTitle["A"] != coding.TaskStatusRunning || statusByTitle["B"] != coding.TaskStatusRunning || statusByTitle["C"] != coding.TaskStatusPending {
		t.Fatalf("task statuses = %#v", statusByTitle)
	}
}

func TestParallelRunnableTasksRespectsDependenciesAndOverlap(t *testing.T) {
	tasks := []coding.Task{
		{ID: "done", Status: coding.TaskStatusDone, AllowedPaths: []string{"README.md"}},
		{ID: "a", Status: coding.TaskStatusPending, AllowedPaths: []string{"internal/a.go"}, DependsOn: []string{"done"}},
		{ID: "b", Status: coding.TaskStatusPending, AllowedPaths: []string{"internal"}, DependsOn: []string{"done"}},
		{ID: "c", Status: coding.TaskStatusPending, AllowedPaths: []string{"docs/c.md"}, DependsOn: []string{"missing"}},
		{ID: "d", Status: coding.TaskStatusPending, AllowedPaths: []string{"docs/d.md"}},
	}
	got := parallelRunnableTasks(tasks, 3)
	ids := make([]string, 0, len(got))
	for _, task := range got {
		ids = append(ids, task.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a", "d"}) {
		t.Fatalf("selected ids = %#v", ids)
	}
}

func TestWorkerPatchMessages(t *testing.T) {
	packet := coding.TaskPacket{Role: "worker", TaskID: "task-1", PlanID: "plan-1", Goal: "Edit README", AllowedPaths: []string{"README.md"}, ToolsAllowed: []string{"apply_patch"}}
	messages := workerPatchMessages(packet)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"Return only valid JSON", "WorkerPatch", "files", "unified diff", `"task_id": "task-1"`, "README.md"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("worker messages missing %q:\n%s", want, combined)
		}
	}
}

func TestWorkerPatchRepairMessages(t *testing.T) {
	packet := coding.TaskPacket{Role: "worker", TaskID: "task-1", PlanID: "plan-1", Goal: "Edit README", AllowedPaths: []string{"README.md"}, ToolsAllowed: []string{"apply_patch"}}
	raw := "This task is done."
	messages := workerPatchRepairMessages(packet, raw, errors.New("bad worker json"))
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"repair WeazlCode WorkerPatch", "Return only valid JSON", "task-1", "Parser error", raw} {
		if !strings.Contains(combined, want) {
			t.Fatalf("repair messages missing %q:\n%s", want, combined)
		}
	}
}

func TestWorkerPatchDiffRepairMessages(t *testing.T) {
	packet := coding.TaskPacket{Role: "worker", TaskID: "task-1", PlanID: "plan-1", Goal: "Edit README", AllowedPaths: []string{"README.md"}, ToolsAllowed: []string{"apply_patch"}}
	patch := coding.WorkerPatch{TaskID: "task-1", Summary: "Updated README", Patch: "not a diff"}
	messages := workerPatchDiffRepairMessages(packet, patch, errors.New("invalid hunk"))
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"repair invalid unified diffs", "Return only valid JSON", "Keep the same task_id", "files", "invalid hunk", "not a diff"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("diff repair messages missing %q:\n%s", want, combined)
		}
	}
}

func TestRunWorkerRequiresRunningTask(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/run-worker")
	if !handled {
		t.Fatal("run-worker handled = false")
	}
	got := updated.(model)
	if got.status != "no plan" {
		t.Fatalf("status = %q, want no plan", got.status)
	}
}

func TestRunWorkerStartsAsync(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Async worker","summary":"Run async worker","tasks":[{"title":"Update README","goal":"Update README.md to document asynchronous worker dispatch.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README documents asynchronous worker dispatch"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	updated, cmd, handled := m.handleSlashCommand("/run-worker")
	if !handled {
		t.Fatal("run-worker handled = false")
	}
	got := updated.(model)
	if cmd == nil {
		t.Fatal("cmd = nil, want async worker command")
	}
	if !got.thinking || got.status != "running worker" {
		t.Fatalf("thinking/status = %t/%q, want true/running worker", got.thinking, got.status)
	}
	if len(got.workerRuns) != 1 || len(got.cancelWorkerRuns) != 1 {
		t.Fatalf("worker run state = %#v cancels=%#v", got.workerRuns, got.cancelWorkerRuns)
	}
}

func TestCancelModelCommandCancelsActiveRun(t *testing.T) {
	m := commandTestModel(t)
	cancelled := false
	m.thinking = true
	m.activeModelRunID = 7
	m.cancelModel = func() { cancelled = true }
	updated, _, handled := m.handleSlashCommand("/cancel")
	if !handled {
		t.Fatal("cancel handled = false")
	}
	got := updated.(model)
	if !cancelled {
		t.Fatal("cancel func was not called")
	}
	if got.thinking || got.activeModelRunID != 0 || got.cancelModel != nil || got.status != "model cancelled" {
		t.Fatalf("model state = thinking:%t run:%d cancel:%v status:%q", got.thinking, got.activeModelRunID, got.cancelModel, got.status)
	}
}

func TestCancelModelCommandCancelsOneWorkerTask(t *testing.T) {
	m := commandTestModel(t)
	cancelled := false
	m.thinking = true
	m.workerRuns = map[int]string{7: "task-1"}
	m.cancelWorkerRuns = map[int]context.CancelFunc{7: func() { cancelled = true }}
	updated, _, handled := m.handleSlashCommand("/cancel task-1")
	if !handled {
		t.Fatal("cancel handled = false")
	}
	got := updated.(model)
	if !cancelled {
		t.Fatal("worker cancel func was not called")
	}
	if got.thinking || len(got.workerRuns) != 0 || got.status != "worker cancelled" {
		t.Fatalf("worker cancel state: thinking=%t runs=%#v status=%q", got.thinking, got.workerRuns, got.status)
	}
}

func TestEnterAllowsCancelWhileThinking(t *testing.T) {
	m := commandTestModel(t)
	cancelled := false
	m.thinking = true
	m.workerRuns = map[int]string{7: "task-1"}
	m.cancelWorkerRuns = map[int]context.CancelFunc{7: func() { cancelled = true }}
	m.input.SetValue("/cancel workers")
	updated, _ := m.handleEnter()
	got := updated.(model)
	if !cancelled || got.thinking || len(got.workerRuns) != 0 {
		t.Fatalf("cancel while thinking failed: cancelled=%t thinking=%t runs=%#v", cancelled, got.thinking, got.workerRuns)
	}
}

func TestStaleModelMessagesAreIgnored(t *testing.T) {
	m := commandTestModel(t)
	m.activeModelRunID = 2
	m.thinking = true
	updated, _ := m.Update(workerRunMsg{runID: 1, err: errors.New("late")})
	got := updated.(model)
	if !got.thinking || got.status != "" || got.activeModelRunID != 2 {
		t.Fatalf("stale message changed model: thinking=%t status=%q run=%d", got.thinking, got.status, got.activeModelRunID)
	}
}

func TestWorkerRunErrorBlocksTask(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Worker error","summary":"Block on error","tasks":[{"title":"Update README","goal":"Update README.md to document worker errors.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README documents worker errors"}]}]}`
	updated, _, _ := m.handleSlashCommand("/plan import " + raw)
	m = updated.(model)
	updated, _, _ = m.handleSlashCommand("/approve")
	m = updated.(model)
	updated, _, _ = m.handleSlashCommand("/run-task")
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	taskID := plan.Tasks[0].ID
	m.workerRuns = map[int]string{9: taskID}
	m.cancelWorkerRuns = map[int]context.CancelFunc{9: func() {}}
	updated, _ = m.Update(workerRunMsg{runID: 9, err: errors.New("context deadline exceeded")})
	got := updated.(model)
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan after error: %v ok=%v", err, ok)
	}
	if plan.Tasks[0].Status != coding.TaskStatusBlocked {
		t.Fatalf("task status = %q, want blocked", plan.Tasks[0].Status)
	}
	events, err := got.store.TaskEvents(taskID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if events[len(events)-1].Type != "worker_timeout" {
		t.Fatalf("events = %#v", events)
	}
	packet := got.packetCommandText()
	if !strings.Contains(packet, "Previous worker attempt failed before producing a patch") {
		t.Fatalf("retry packet missing worker error context:\n%s", packet)
	}
	updated, _, _ = got.handleSlashCommand("/run-task")
	got = updated.(model)
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan after retry: %v ok=%v", err, ok)
	}
	if plan.Tasks[0].Status != coding.TaskStatusRunning {
		t.Fatalf("task status after retry = %q, want running", plan.Tasks[0].Status)
	}
}

func TestTaskGitDiffIncludesUntrackedAllowedFiles(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", root).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles.css"), []byte("body { color: black; }\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	m := commandTestModel(t)
	m.project.Root = root
	m.session.ProjectRoot = root
	diff, err := m.taskGitDiff(coding.Task{AllowedPaths: []string{"styles.css"}})
	if err != nil {
		t.Fatalf("taskGitDiff: %v", err)
	}
	for _, want := range []string{"diff --git a/styles.css b/styles.css", "new file mode", "+body { color: black; }"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
}

func TestAsyncModelSpinnerPreservesIDEView(t *testing.T) {
	m := commandTestModel(t)
	m.working = spinner.New()
	m.setIDEView("plan", "important plan view")
	m.thinking = true
	m.status = "running worker"

	updated, _ := m.Update(spinner.TickMsg{})
	got := updated.(model)
	if !strings.Contains(got.viewport.View(), "important plan view") {
		t.Fatalf("viewport changed during async spinner tick:\n%s", got.viewport.View())
	}
}

func TestSlashWorkerPatchBlockerMarksTaskBlocked(t *testing.T) {
	m := commandTestModel(t)
	rawPlan := `{"title":"Needs context","summary":"Block task","tasks":[{"title":"Update README","goal":"Update README.md to document blocker handling.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README documents blocker handling"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	raw, err := json.Marshal(coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Blocker: "Need README context"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(raw))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "worker reported blocker" {
		t.Fatalf("status = %q, want worker reported blocker", got.status)
	}
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Tasks[0].Status != "blocked" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 4 || events[3].Type != "worker_blocker" {
		t.Fatalf("events = %#v", events)
	}
}

func TestSlashWorkerPatchAppliesPatchAndMarksReviewing(t *testing.T) {
	got, root := commandTestModelWithReviewingTask(t)
	if got.status != "task reviewing" {
		t.Fatalf("status = %q, want task reviewing", got.status)
	}
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("README = %q, want new", data)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Tasks[0].Status != "reviewing" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 5 || events[3].Type != "worker_patch" || events[4].Type != "verification" || len(events[4].Payload) == 0 {
		t.Fatalf("events = %#v", events)
	}
	kinds := runArtifactKinds(t, filepath.Join(root, ".weazlcode", "runs", got.session.ID))
	for _, want := range []string{"plan", "worker_packet", "worker_output", "diff", "verification"} {
		if !kinds[want] {
			t.Fatalf("artifact %q missing from %#v", want, kinds)
		}
	}
}

func TestSlashWorkerPatchAppliesFileEditsAndMarksReviewing(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch README","summary":"Apply worker edit","tasks":[{"title":"Update README","goal":"Change README text","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README changed"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Summary: "Updated README", Files: []coding.WorkerFileEdit{{Path: "README.md", Content: "new\n"}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "task reviewing" {
		t.Fatalf("status = %q, want task reviewing", got.status)
	}
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("README = %q, want new", data)
	}
}

func TestSlashWorkerPatchRejectsPathsOutsideTaskScope(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch README","summary":"Apply worker edit","tasks":[{"title":"Update README","goal":"Change README text","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README changed"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Summary: "Wrong file", Files: []coding.WorkerFileEdit{{Path: "docs/README.md", Content: "new\n"}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "worker patch rejected" {
		t.Fatalf("status = %q, want worker patch rejected", got.status)
	}
	if !strings.Contains(got.viewport.View(), "Returned paths:") || !strings.Contains(got.viewport.View(), "Allowed paths:") {
		t.Fatalf("viewport missing rejection detail: %q", got.viewport.View())
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if events[len(events)-1].Type != "worker_rejected" {
		t.Fatalf("events = %#v", events)
	}
	kinds := runArtifactKinds(t, filepath.Join(root, ".weazlcode", "runs", got.session.ID))
	if !kinds["worker_rejected"] {
		t.Fatalf("worker_rejected artifact missing from %#v", kinds)
	}
}

func TestSlashWorkerPatchRejectsSuspiciousFullFileRewrite(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	var oldContent string
	for i := 0; i < 100; i++ {
		oldContent += fmt.Sprintf("line %03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(oldContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch README","summary":"Apply worker edit","tasks":[{"title":"Update README","goal":"Change README text","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README changed"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Summary: "Rewrite", Files: []coding.WorkerFileEdit{{Path: "README.md", Content: "# replacement\n"}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "worker patch rejected" {
		t.Fatalf("status = %q, want worker patch rejected", got.status)
	}
	if !strings.Contains(got.viewport.View(), "suspicious full-file rewrite") {
		t.Fatalf("viewport missing rewrite rejection: %q", got.viewport.View())
	}
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != oldContent {
		t.Fatalf("README changed despite rejection")
	}
}

func TestWorkerPatchTelemetryAddsTaskEvent(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch README","summary":"Apply worker edit","tasks":[{"title":"Update README","goal":"Change README text","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README changed"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	patch := coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Summary: "Updated README", Files: []coding.WorkerFileEdit{{Path: "README.md", Content: "new\n"}}}
	updated, _, _ = m.applyWorkerPatchWithTelemetry(patch, true, &modelTelemetry{Role: "worker", Provider: "local-vllm", Model: "test-model", LatencyMS: 123, RawChars: 456, JSONRepairAttempts: 1})
	got := updated.(model)
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	modelEvent := coding.TaskEvent{}
	for _, event := range events {
		if event.Type == "worker_model" {
			modelEvent = event
			break
		}
	}
	if modelEvent.Type == "" {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(string(modelEvent.Payload), `"latency_ms":123`) || !strings.Contains(string(modelEvent.Payload), `"json_repair_attempts":1`) {
		t.Fatalf("telemetry payload = %s", modelEvent.Payload)
	}
}

func TestSlashReviewerInputCommand(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	updated, _, handled := m.handleSlashCommand("/reviewer-input")
	if !handled {
		t.Fatal("reviewer-input handled = false")
	}
	got := updated.(model)
	if got.status != "view reviewer-input" {
		t.Fatalf("status = %q, want view reviewer-input", got.status)
	}
	view := got.viewport.View()
	if !strings.Contains(view, "Reviewer input:") {
		t.Fatalf("viewport missing reviewer input: %q", view)
	}
	input, err := m.buildReviewerInput()
	if err != nil {
		t.Fatalf("buildReviewerInput: %v", err)
	}
	if !strings.Contains(input.Diff, "-old") || !strings.Contains(input.Diff, "+new") || !strings.Contains(input.VerificationOut, "python -m compileall .") {
		t.Fatalf("reviewer input = %#v", input)
	}
}

func TestReviewerInputScopesDiffToCurrentTask(t *testing.T) {
	m, root := commandTestModelWithReviewingTask(t)
	if err := os.WriteFile(filepath.Join(root, "other.txt"), []byte("old other\n"), 0o644); err != nil {
		t.Fatalf("WriteFile other: %v", err)
	}
	runTestGit(t, root, "add", "other.txt")
	runTestGit(t, root, "commit", "-m", "add other")
	if err := os.WriteFile(filepath.Join(root, "other.txt"), []byte("new other\n"), 0o644); err != nil {
		t.Fatalf("WriteFile other changed: %v", err)
	}
	input, err := m.buildReviewerInput()
	if err != nil {
		t.Fatalf("buildReviewerInput: %v", err)
	}
	if !strings.Contains(input.Diff, "README.md") || strings.Contains(input.Diff, "other.txt") {
		t.Fatalf("reviewer diff was not task scoped:\n%s", input.Diff)
	}
}

func TestReviewerInputUsesTaskBaselineForDirtyAllowedFile(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("committed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile committed: %v", err)
	}
	runTestGit(t, root, "add", "README.md")
	runTestGit(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("committed\npreexisting dirty\n"), 0o644); err != nil {
		t.Fatalf("WriteFile dirty: %v", err)
	}
	m := commandTestModel(t)
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch README","summary":"Apply worker patch","tasks":[{"title":"Update README","goal":"Add worker line","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README has worker line"}]}]}`
	updated, _, _ := m.handleSlashCommand("/plan import " + rawPlan)
	m = updated.(model)
	updated, _, _ = m.handleSlashCommand("/approve")
	m = updated.(model)
	updated, _, _ = m.handleSlashCommand("/run-task")
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	patch := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1,2 +1,3 @@
 committed
 preexisting dirty
+worker line
`
	rawPatch, err := json.Marshal(coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Summary: "Added worker line", Patch: patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, _ = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	m = updated.(model)
	input, err := m.buildReviewerInput()
	if err != nil {
		t.Fatalf("buildReviewerInput: %v", err)
	}
	if strings.Contains(input.Diff, "-committed") || strings.Contains(input.Diff, "-preexisting dirty") {
		t.Fatalf("reviewer diff included baseline dirty content as removals:\n%s", input.Diff)
	}
	if !strings.Contains(input.Diff, "+worker line") {
		t.Fatalf("reviewer diff missing worker change:\n%s", input.Diff)
	}
}

func TestReviewerVerdictMessages(t *testing.T) {
	input := coding.ReviewerInput{
		TaskPacket: coding.TaskPacket{TaskID: "task-1", Goal: "Edit README", AllowedPaths: []string{"README.md"}},
		Diff:       "diff --git a/README.md b/README.md",
	}
	messages := reviewerVerdictMessages(input)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"frontier reviewer", "Return only valid JSON", "approve|needs_fix|blocked", "task-1", "README.md"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("reviewer messages missing %q:\n%s", want, combined)
		}
	}
}

func TestRunReviewerStartsAsync(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	updated, cmd, handled := m.handleSlashCommand("/run-reviewer")
	if !handled {
		t.Fatal("run-reviewer handled = false")
	}
	got := updated.(model)
	if cmd == nil {
		t.Fatal("cmd = nil, want async reviewer command")
	}
	if !got.thinking || got.status != "running reviewer" || got.activeModelRunID == 0 || got.cancelModel == nil {
		t.Fatalf("state = thinking:%t status:%q run:%d cancel:%v", got.thinking, got.status, got.activeModelRunID, got.cancelModel)
	}
}

func TestReviewerTelemetryAddsTaskEvent(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	updated, _, _ := m.applyReviewVerdict(coding.ReviewVerdict{Verdict: coding.ReviewApprove, Summary: "ok"}, &modelTelemetry{Role: "reviewer", Provider: "frontier", Model: "review-model", LatencyMS: 77, RawChars: 100})
	got := updated.(model)
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type == "reviewer_model" && strings.Contains(string(event.Payload), `"latency_ms":77`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("reviewer telemetry missing from %#v", events)
	}
}

func TestSlashReviewDiffCommandShowsChangedFilesAndCommands(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	updated, _, handled := m.handleSlashCommand("/review-diff")
	if !handled {
		t.Fatal("review-diff handled = false")
	}
	got := updated.(model)
	if got.status != "view review-diff" {
		t.Fatalf("status = %q, want view review-diff", got.status)
	}
	view := got.reviewDiffCommandText()
	for _, want := range []string{"Review diff: Update README", "Changed files:", "README.md", "/review approve", "/review needs-fix", "Task-scoped diff:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("review diff missing %q:\n%s", want, view)
		}
	}
}

func TestProjectInstructionsAndMemoryCommands(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WEAZLCODE.md"), []byte("# Rules\n\nRun tests.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.session.ProjectRoot = root
	updated, _, handled := m.handleSlashCommand("/memory test=use focused packets")
	if !handled {
		t.Fatal("memory handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/instructions")
	if !handled {
		t.Fatal("instructions handled = false")
	}
	got := updated.(model)
	view := got.viewport.View()
	if !strings.Contains(view, "WEAZLCODE.md") || !strings.Contains(view, "use focused packets") {
		t.Fatalf("instructions view = %q", view)
	}
}

func TestFinalReviewCommitMessageAndExport(t *testing.T) {
	m, root := commandTestModelWithReviewingTask(t)
	updated, _, handled := m.handleSlashCommand(`/review {"verdict":"approve","summary":"Looks good"}`)
	if !handled {
		t.Fatal("review handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/final-review")
	if !handled {
		t.Fatal("final-review handled = false")
	}
	if !strings.Contains(updated.(model).viewport.View(), "Rollback guidance") {
		t.Fatalf("final review = %q", updated.(model).viewport.View())
	}
	updated, _, handled = m.handleSlashCommand("/commit-message")
	if !handled {
		t.Fatal("commit-message handled = false")
	}
	if !strings.Contains(updated.(model).viewport.View(), "Complete Patch README") {
		t.Fatalf("commit message = %q", updated.(model).viewport.View())
	}
	updated, _, handled = m.handleSlashCommand("/export-run")
	if !handled {
		t.Fatal("export-run handled = false")
	}
	if !strings.Contains(updated.(model).viewport.View(), filepath.Join(root, ".weazlcode", "runs")) {
		t.Fatalf("export view = %q", updated.(model).viewport.View())
	}
}

func TestSlashReviewApproveMarksTaskDone(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	updated, _, handled := m.handleSlashCommand("/review approve Looks good")
	if !handled {
		t.Fatal("review handled = false")
	}
	got := updated.(model)
	if got.status != "task done" {
		t.Fatalf("status = %q, want task done", got.status)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Status != "done" || plan.Tasks[0].Status != "done" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	kinds := runArtifactKinds(t, filepath.Join(got.project.StateDir, "runs", got.session.ID))
	if !kinds["review"] {
		t.Fatalf("review artifact missing from %#v", kinds)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 6 || events[5].Type != "reviewer_verdict" {
		t.Fatalf("events = %#v", events)
	}
}

func TestReviewApproveBlockedByLocalGuardrail(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Review guardrail","summary":"Check empty diff","tasks":[{"title":"Update README","goal":"Change README text","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README changed"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	if err := m.store.UpdateTaskStatus(plan.Tasks[0].ID, coding.TaskStatusReviewing); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/review approve Looks good")
	if !handled {
		t.Fatal("review handled = false")
	}
	got := updated.(model)
	if got.status != "review guardrail" {
		t.Fatalf("status = %q, want review guardrail", got.status)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if events[len(events)-1].Type != "review_guardrail" {
		t.Fatalf("events = %#v", events)
	}
}

func TestReviewApproveIgnoresDiffOutsideCurrentTaskScope(t *testing.T) {
	m, root := commandTestModelWithReviewingTask(t)
	if err := os.WriteFile(filepath.Join(root, "other.txt"), []byte("old other\n"), 0o644); err != nil {
		t.Fatalf("WriteFile other: %v", err)
	}
	runTestGit(t, root, "add", "other.txt")
	runTestGit(t, root, "commit", "-m", "add other")
	if err := os.WriteFile(filepath.Join(root, "other.txt"), []byte("new other\n"), 0o644); err != nil {
		t.Fatalf("WriteFile other changed: %v", err)
	}
	updated, _, handled := m.handleSlashCommand("/review approve Looks good")
	if !handled {
		t.Fatal("review handled = false")
	}
	got := updated.(model)
	if got.status != "task done" {
		t.Fatalf("status = %q, want task done\n%s", got.status, got.viewport.View())
	}
}

func TestReviewNeedsFixShorthandRequestsRepair(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	updated, _, handled := m.handleSlashCommand(`/review needs-fix missing docs;;tests not run`)
	if !handled {
		t.Fatal("review handled = false")
	}
	got := updated.(model)
	if got.status != "repair requested" {
		t.Fatalf("status = %q, want repair requested", got.status)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Tasks[0].Status != "blocked" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if events[len(events)-1].Type != "repair_requested" || !strings.Contains(string(events[len(events)-1].Payload), "tests not run") {
		t.Fatalf("events = %#v", events)
	}
}

func TestSlashReviewNeedsFixCreatesRepairPacket(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	raw := `{"verdict":"needs_fix","summary":"Tighten the change","issues":["Use the requested wording only"]}`
	updated, _, handled := m.handleSlashCommand("/review " + raw)
	if !handled {
		t.Fatal("review handled = false")
	}
	m = updated.(model)
	if m.status != "repair requested" {
		t.Fatalf("status = %q, want repair requested", m.status)
	}
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Tasks[0].Status != "blocked" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	events, err := m.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 7 || events[6].Type != "repair_requested" {
		t.Fatalf("events = %#v", events)
	}
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	got := updated.(model)
	if got.status != "task running" {
		t.Fatalf("status = %q, want task running", got.status)
	}
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Tasks[0].Status != "running" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	events, err = got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 9 || events[7].Type != "task_baseline" || events[8].Type != "repair_start" {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(string(events[8].Payload), "Repair focus") || !strings.Contains(string(events[8].Payload), "Use the requested wording only") {
		t.Fatalf("repair payload = %s", events[8].Payload)
	}
	kinds := runArtifactKinds(t, filepath.Join(got.project.StateDir, "runs", got.session.ID))
	for _, want := range []string{"repair_request", "repair_packet"} {
		if !kinds[want] {
			t.Fatalf("artifact %q missing from %#v", want, kinds)
		}
	}
}

func TestSlashReviewNeedsFixCapsRepairLoop(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	for i := 0; i < maxRepairAttempts; i++ {
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{TaskID: plan.Tasks[0].ID, Type: "repair_requested", Message: "existing repair"})
	}
	raw := `{"verdict":"needs_fix","summary":"Still wrong","issues":["No more repairs"]}`
	updated, _, handled := m.handleSlashCommand("/review " + raw)
	if !handled {
		t.Fatal("review handled = false")
	}
	got := updated.(model)
	if got.status != "repair limit reached" {
		t.Fatalf("status = %q, want repair limit reached", got.status)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if events[len(events)-1].Type != "repair_limit" {
		t.Fatalf("events = %#v", events)
	}
	updated, _, handled = got.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	got = updated.(model)
	if got.status != "no runnable task" {
		t.Fatalf("status = %q, want no runnable task", got.status)
	}
}

func commandTestModelWithReviewingTask(t *testing.T) (model, string) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runTestGit(t, root, "init")
	runTestGit(t, root, "add", "README.md")
	m := commandTestModel(t)
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch README","summary":"Apply worker patch","tasks":[{"title":"Update README","goal":"Change README text","allowed_paths":["README.md"],"verification":["python -m compileall ."],"acceptance_checks":[{"description":"README changed"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	patch := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old
+new
`
	rawPatch, err := json.Marshal(coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Summary: "Updated README", Patch: patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	return updated.(model), root
}

func runTestGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runArtifactKinds(t *testing.T, dir string) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	kinds := map[string]bool{}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		var artifact struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(data, &artifact); err != nil {
			t.Fatalf("Unmarshal %s: %v", path, err)
		}
		kinds[artifact.Kind] = true
	}
	return kinds
}

func commandTestModel(t ...*testing.T) model {
	cfg := config.Default()
	registry := tools.NewRegistry()
	registry.Register(tools.NewCalculatorTool())
	registry.Register(tools.NewRunVerificationCommandTool(tools.Limits{WorkspaceRoots: []string{"/tmp"}}))
	registry.Register(tools.NewGitDiffTool(tools.Limits{WorkspaceRoots: []string{"/tmp"}}))
	registry.Register(tools.NewListChangedFilesTool(tools.Limits{WorkspaceRoots: []string{"/tmp"}}))
	ti := textinput.New()
	ti.Focus()
	var store *storage.Store
	session := storage.Session{ID: "session-1", Title: "New session", Provider: "local-vllm", Model: "local-model", ProjectRoot: "/tmp/weazlcode"}
	if len(t) > 0 && t[0] != nil {
		var err error
		store, err = storage.Open(filepath.Join(t[0].TempDir(), "test.sqlite3"))
		if err != nil {
			t[0].Fatal(err)
		}
		t[0].Cleanup(func() { _ = store.Close() })
		if err := store.Migrate(); err != nil {
			t[0].Fatal(err)
		}
		if err := store.CreateProjectSession(session.ID, session.Title, session.Provider, session.Model, session.ProjectRoot); err != nil {
			t[0].Fatal(err)
		}
	}
	return model{
		cfg:          cfg,
		project:      project.Summary{Root: "/tmp/weazlcode", GitRoot: true, Branch: "main", Dirty: true, StateDir: "/tmp/weazlcode/.weazlcode", LogDir: "/tmp/weazlcode/.weazlcode/logs", Languages: []string{"go"}, FileCount: 42},
		store:        store,
		toolRegistry: registry,
		styles:       newStyles(),
		mode:         modeChat,
		input:        ti,
		viewport:     viewport.New(80, 20),
		mouseScroll:  true,
		session:      session,
	}
}
