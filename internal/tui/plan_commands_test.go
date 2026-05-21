package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/coding"
)

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
	raw := `{"title":"Generated","summary":"From model","tasks":[{"title":"Task","goal":"Do work","allowed_paths":["README.md"],"verification":["grep -q ok README.md","go test ./...","python app.py --smoke"],"acceptance_checks":[{"description":"checks pass"}]}]}`
	plan, err := m.planFromGeneratedJSON(raw)
	if err != nil {
		t.Fatalf("planFromGeneratedJSON: %v", err)
	}
	if !reflect.DeepEqual(plan.Tasks[0].Verification, []string{"go test ./...", "python app.py --smoke"}) {
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
	for _, want := range []string{"Return only valid JSON", "Do not add unknown fields", "Configured worker:", "capacity:", "Parser error", raw, "change README"} {
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
	worker := m.cfg.Providers[m.cfg.ModelRoles.Worker]
	worker.Model = "granite-4.1-8b-awq"
	m.cfg.Providers[m.cfg.ModelRoles.Worker] = worker
	if err := m.store.RememberProject(root, "style", "small patches", "project"); err != nil {
		t.Fatalf("RememberProject: %v", err)
	}
	messages := m.planGenerateMessages("change README")
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"Return only valid JSON", "WeazlCode BRAID planning contract", "semi-serial dependencies", "Artifact:", `"interface_contract"`, `"constructors"`, `"methods"`, `"skills"`, "Use small tasks", "go test ./...", "Configured worker:", "small 8.0B-class local worker", "Critical efficiency rule", "one whole-file artifact task", "validation-only worker tasks", "fewest tasks", "Modularization north star", "300 lines", "interactive apps, games, APIs", "parallel draft work", "interface_contract with exact exported names", "final wiring/smoke task", "dependency module files", "read-only contracts", "context_files", "explicit imports", "not wildcard imports", "non-interactive --smoke path", "python main.py --smoke", "single static landing page", "one cohesive artifact task", "worker output budget", "module-first plans", "sections/*.html", "styles/*.css", "must explicitly say the worker must not include doctype", "plain browser CSS only", "preserve the exact required strings", "Never use ellipses", "Final assembly tasks", "dependency outputs as source material", "Do not create long serial chains", "style: small patches", "go-tests", "Write focused Go tests.", "change README"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("messages missing %q:\n%s", want, combined)
		}
	}
}

func TestAttachSourceCopyContractAddsExactCopyCheck(t *testing.T) {
	plan := coding.Plan{
		Tasks: []coding.Task{
			{ID: "html", Title: "HTML", Goal: "Create page", AllowedPaths: []string{"index.html"}},
			{ID: "css", Title: "CSS", Goal: "Style page", AllowedPaths: []string{"styles.css"}},
		},
	}
	attachSourceCopyContract(&plan, "Build page.\n\nRequired copy:\nExact product copy that should not be paraphrased.")
	if len(plan.Tasks[0].AcceptanceChecks) != 1 {
		t.Fatalf("html checks = %#v", plan.Tasks[0].AcceptanceChecks)
	}
	if !strings.Contains(plan.Tasks[0].AcceptanceChecks[0].Description, "Exact product copy") {
		t.Fatalf("html check = %#v", plan.Tasks[0].AcceptanceChecks[0])
	}
	if len(plan.Tasks[1].AcceptanceChecks) != 0 {
		t.Fatalf("css checks = %#v", plan.Tasks[1].AcceptanceChecks)
	}
}

func TestWorkerCapacityProfileInfersModelSize(t *testing.T) {
	m := commandTestModel(t)
	worker := m.cfg.Providers[m.cfg.ModelRoles.Worker]
	worker.Model = "cyankiwi/granite-4.1-8b-AWQ-INT4"
	m.cfg.Providers[m.cfg.ModelRoles.Worker] = worker
	profile := m.workerCapacityProfile()
	if profile.SizeBillions != 8 || !strings.Contains(profile.Instruction, "cohesive artifact") {
		t.Fatalf("profile = %#v", profile)
	}
	worker.Model = "unknown-local-model"
	m.cfg.Providers[m.cfg.ModelRoles.Worker] = worker
	profile = m.workerCapacityProfile()
	if profile.SizeBillions != 0 || !strings.Contains(profile.Label, "unknown-size") {
		t.Fatalf("unknown profile = %#v", profile)
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

func TestSlashApproveRunDispatchesWorkers(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Approve and run","summary":"Approve plan and start workers","tasks":[{"id":"task-a","title":"A","goal":"Update README.md to mention A.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README mentions A"}]},{"id":"task-b","title":"B","goal":"Update docs/guide.md to mention B.","allowed_paths":["docs/guide.md"],"acceptance_checks":[{"description":"guide mentions B"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan handled = false")
	}
	m = updated.(model)
	if !strings.Contains(m.viewport.View(), "/approve run") {
		t.Fatalf("plan output missing approve-run hint: %q", m.viewport.View())
	}
	updated, cmd, handled := m.handleSlashCommand("/approve run")
	if !handled {
		t.Fatal("approve run handled = false")
	}
	got := updated.(model)
	if cmd == nil {
		t.Fatal("cmd = nil, want worker batch")
	}
	if got.status != "running 2 worker(s)" || len(got.workerRuns) != 2 {
		t.Fatalf("status/runs = %q/%#v", got.status, got.workerRuns)
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

func TestPlanReplanStartsAsyncWithBlockedEvidence(t *testing.T) {
	m := commandTestModel(t)
	raw := `{"title":"Replan me","summary":"Original work","tasks":[{"id":"task-a","title":"Done","goal":"Already completed.","allowed_paths":["done.css"]},{"id":"task-b","title":"Blocked","goal":"Extract JavaScript that may not exist.","allowed_paths":["app.js"]},{"id":"task-c","title":"Pending","goal":"Continue useful CSS extraction.","allowed_paths":["next.css"],"depends_on":["task-b"]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + raw)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	if err := m.store.UpdateTaskStatus("task-a", coding.TaskStatusDone); err != nil {
		t.Fatalf("UpdateTaskStatus done: %v", err)
	}
	if err := m.store.UpdateTaskStatus("task-b", coding.TaskStatusBlocked); err != nil {
		t.Fatalf("UpdateTaskStatus blocked: %v", err)
	}
	if _, err := m.store.AddTaskEvent(coding.TaskEvent{TaskID: "task-b", Type: "worker_blocker", Message: "No JavaScript code exists in index.html to extract."}); err != nil {
		t.Fatalf("AddTaskEvent: %v", err)
	}
	plan, ok, err = m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found after updates")
	}
	request, err := m.replanRequest(plan, "keep the modular CSS direction")
	if err != nil {
		t.Fatalf("replanRequest: %v", err)
	}
	for _, want := range []string{"Already completed", "No JavaScript code exists", "keep the modular CSS direction", "Continue useful CSS extraction"} {
		if !strings.Contains(request, want) {
			t.Fatalf("request missing %q:\n%s", want, request)
		}
	}
	updated, cmd, handled := m.handleSlashCommand("/plan replan keep the modular CSS direction")
	if !handled {
		t.Fatal("plan replan handled = false")
	}
	got := updated.(model)
	if cmd == nil || got.status != "replanning" {
		t.Fatalf("cmd/status = %v/%q, want async replanning", cmd, got.status)
	}
}
