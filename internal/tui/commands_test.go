package tui

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	for _, want := range []string{"Command palette:", "Plan:", "/plan edit", "Worker:", "/run-worker", "Review:", "/review-diff"} {
		if !strings.Contains(view, want) {
			t.Fatalf("palette missing %q:\n%s", want, view)
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

func TestSlashDurableIDEViews(t *testing.T) {
	m := commandTestModel(t)
	for _, command := range []string{"/tools", "/config", "/outputs", "/files"} {
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
	m.project.Root = root
	m.project.Languages = []string{"go"}
	if err := m.store.RememberProject(root, "style", "small patches", "project"); err != nil {
		t.Fatalf("RememberProject: %v", err)
	}
	messages := m.planGenerateMessages("change README")
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"Return only valid JSON", "Use small tasks", "go test ./...", "style: small patches", "change README"} {
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
	if !reflect.DeepEqual(task.Verification, []string{"go test ./..."}) {
		t.Fatalf("verification = %#v", task.Verification)
	}
	if len(task.AcceptanceChecks) != 2 || task.AcceptanceChecks[0].Description != "README updated" || task.AcceptanceChecks[1].Description != "tests pass" {
		t.Fatalf("checks = %#v", task.AcceptanceChecks)
	}
}

func TestSlashPlanEditCommandRequiresDraftPlan(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Locked")
	if !handled {
		t.Fatal("plan draft handled = false")
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
	updated, _, handled := m.handleSlashCommand("/plan draft Run me")
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
	if len(events) != 2 || events[1].Type != "worker_start" || len(events[1].Payload) == 0 {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(got.viewport.View(), `"tools_allowed"`) {
		t.Fatalf("viewport missing worker packet: %q", got.viewport.View())
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
	updated, _, handled := m.handleSlashCommand("/plan draft Async worker")
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

func TestSlashWorkerPatchBlockerMarksTaskBlocked(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Needs context")
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
	if got.status != "task blocked" {
		t.Fatalf("status = %q, want task blocked", got.status)
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
	if len(events) != 3 || events[2].Type != "worker_blocker" {
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
	if len(events) != 4 || events[2].Type != "worker_patch" || events[3].Type != "verification" || len(events[3].Payload) == 0 {
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
	for _, want := range []string{"Review diff: Update README", "Changed files:", "README.md", "/review approve", "/review needs-fix", "Diff:"} {
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
	if len(events) != 5 || events[4].Type != "reviewer_verdict" {
		t.Fatalf("events = %#v", events)
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
	if len(events) != 6 || events[5].Type != "repair_requested" {
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
	if len(events) != 7 || events[6].Type != "repair_start" {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(string(events[6].Payload), "Repair focus") || !strings.Contains(string(events[6].Payload), "Use the requested wording only") {
		t.Fatalf("repair payload = %s", events[6].Payload)
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
