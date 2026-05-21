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

	"github.com/bprendie/weazlcode/internal/coding"
)

func TestWorkerContextFileCharBudgetScalesWithContextWindow(t *testing.T) {
	tests := []struct {
		contextWindow int
		want          int
	}{
		{8192, 12000},
		{16384, 24000},
		{32768, 48000},
	}
	for _, tt := range tests {
		if got := workerContextFileCharBudget(tt.contextWindow); got != tt.want {
			t.Fatalf("workerContextFileCharBudget(%d) = %d, want %d", tt.contextWindow, got, tt.want)
		}
	}
}

func TestWorkerOutputTokensUseArtifactBudgetForWholeFileTasks(t *testing.T) {
	m := commandTestModel(t)
	m.cfg.Workers.OutputTokens = 4096
	m.cfg.Workers.ArtifactOutputTokens = 24576
	worker := m.cfg.Providers[m.cfg.ModelRoles.Worker]
	worker.ContextWindow = 32768
	m.cfg.Providers[m.cfg.ModelRoles.Worker] = worker

	normal := coding.TaskPacket{TaskID: "task-1", Goal: "Update README.", AllowedPaths: []string{"README.md"}}
	if got := m.workerOutputTokens(normal); got != 4096 {
		t.Fatalf("normal output tokens = %d, want 4096", got)
	}
	artifact := coding.TaskPacket{TaskID: "task-2", Goal: "Create a complete landing page website.", AllowedPaths: []string{"index.html", "styles.css"}}
	if got := m.workerOutputTokens(artifact); got != 24576 {
		t.Fatalf("artifact output tokens = %d, want 24576", got)
	}
}

func TestWorkerPacketMarksSingleFileGeneratedArtifact(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	packet, err := m.buildWorkerPacket(coding.Task{
		ID:           "task-1",
		PlanID:       "plan",
		Title:        "Create pygame blackjack",
		Goal:         "Build a standalone Python game.",
		Status:       coding.TaskStatusPending,
		AllowedPaths: []string{"blackjack.py"},
	})
	if err != nil {
		t.Fatalf("buildWorkerPacket: %v", err)
	}
	combined := packet.WorkerProfile + "\n" + packet.ContextPolicy.Instruction
	if !strings.Contains(combined, "single-file generated artifact") || !strings.Contains(combined, "files[] with complete content") {
		t.Fatalf("packet did not include single-file artifact guidance:\n%s", combined)
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
	if !strings.Contains(view, `"context_policy"`) || !strings.Contains(view, `"provided"`) {
		t.Fatalf("viewport missing context policy: %q", view)
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

func TestBuildWorkerPacketIncludesExistingAllowedFileContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>old</main>\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := commandTestModel(t)
	m.project.Root = root
	packet, err := m.buildWorkerPacket(coding.Task{
		ID:           "task-1",
		PlanID:       "plan-1",
		Title:        "Task",
		Goal:         "Edit index.html.",
		Status:       coding.TaskStatusPending,
		AllowedPaths: []string{"index.html"},
	})
	if err != nil {
		t.Fatalf("buildWorkerPacket: %v", err)
	}
	if len(packet.ContextFiles) != 1 || packet.ContextFiles[0].Path != "index.html" || packet.ContextFiles[0].Content != "<main>old</main>\n" {
		t.Fatalf("context files = %#v", packet.ContextFiles)
	}
	if packet.ContextPolicy.Mode != "provided" || len(packet.ContextPolicy.RequestTools) != 0 {
		t.Fatalf("context policy = %#v", packet.ContextPolicy)
	}
}

func TestBuildWorkerPacketIgnoresMissingAllowedFileContext(t *testing.T) {
	root := t.TempDir()
	m := commandTestModel(t)
	m.project.Root = root
	packet, err := m.buildWorkerPacket(coding.Task{
		ID:           "task-1",
		PlanID:       "plan-1",
		Title:        "Task",
		Goal:         "Create index.html.",
		Status:       coding.TaskStatusPending,
		AllowedPaths: []string{"index.html"},
	})
	if err != nil {
		t.Fatalf("buildWorkerPacket: %v", err)
	}
	if len(packet.ContextFiles) != 0 {
		t.Fatalf("context files = %#v", packet.ContextFiles)
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

func TestParallelRunnableTasksTreatsReviewingAsWorkerComplete(t *testing.T) {
	tasks := []coding.Task{
		{ID: "a", Status: coding.TaskStatusReviewing, AllowedPaths: []string{"index.html"}},
		{ID: "b", Status: coding.TaskStatusPending, AllowedPaths: []string{"index.html"}, DependsOn: []string{"a"}},
		{ID: "c", Status: coding.TaskStatusPending, AllowedPaths: []string{"styles.css"}},
	}
	got := parallelRunnableTasks(tasks, 3)
	ids := make([]string, 0, len(got))
	for _, task := range got {
		ids = append(ids, task.ID)
	}
	if !reflect.DeepEqual(ids, []string{"b", "c"}) {
		t.Fatalf("selected ids = %#v", ids)
	}
}

func TestContinueAutonomousRunDispatchesWorkersBeforeReview(t *testing.T) {
	root := t.TempDir()
	m := commandTestModel(t)
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	m.cfg.Workers.Concurrency = 3
	raw := `{"title":"Pipeline","summary":"Keep workers moving","tasks":[{"id":"task-a","title":"A","goal":"Create index.html base.","allowed_paths":["index.html"],"acceptance_checks":[{"description":"index exists"}]},{"id":"task-b","title":"B","goal":"Extend index.html.","allowed_paths":["index.html"],"depends_on":["task-a"],"acceptance_checks":[{"description":"index extended"}]},{"id":"task-c","title":"C","goal":"Create styles.css.","allowed_paths":["styles.css"],"acceptance_checks":[{"description":"styles exist"}]}]}`
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
	if err := m.store.UpdateTaskStatus("task-a", coding.TaskStatusReviewing); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	m.startAutonomousRun()
	updated, cmd := m.continueAutonomousRun(nil)
	got := updated.(model)
	if cmd == nil {
		t.Fatal("cmd = nil, want worker dispatch")
	}
	if got.status != "running 2 worker(s)" || len(got.workerRuns) != 2 {
		t.Fatalf("status/runs = %q/%#v", got.status, got.workerRuns)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	status := map[string]string{}
	for _, task := range plan.Tasks {
		status[task.ID] = task.Status
	}
	if status["task-b"] != coding.TaskStatusRunning || status["task-c"] != coding.TaskStatusRunning {
		t.Fatalf("task statuses = %#v", status)
	}
}

func TestContinueAutonomousRunFailsAfterRunTimeout(t *testing.T) {
	m := commandTestModel(t)
	m.cfg.Workers.RunTimeoutSeconds = 1
	raw := `{"title":"Timeout","summary":"Stop slow runs","tasks":[{"id":"task-a","title":"A","goal":"Update README.md to mention A.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README mentions A"}]}]}`
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
	m.startAutonomousRun()
	m.autonomousRunStarted = time.Now().Add(-2 * time.Second)
	updated, _ = m.continueAutonomousRun(nil)
	got := updated.(model)
	if got.autonomousRun || got.status != "run timeout" {
		t.Fatalf("autonomous/status = %v/%q", got.autonomousRun, got.status)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	if plan.Status != coding.PlanStatusBlocked || plan.Tasks[0].Status != coding.TaskStatusBlocked {
		t.Fatalf("plan status = %s task = %s", plan.Status, plan.Tasks[0].Status)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	foundTimeout := false
	for _, event := range events {
		if event.Type == "run_timeout" {
			foundTimeout = true
			break
		}
	}
	if !foundTimeout {
		t.Fatalf("events = %#v", events)
	}
}

func TestWorkerPatchMessages(t *testing.T) {
	packet := coding.TaskPacket{Role: "worker", TaskID: "task-1", PlanID: "plan-1", Goal: "Edit README", AllowedPaths: []string{"README.md"}, WorkerProfile: "Artifact contract: HTML fragments must not include doctype/html/head/body.", ToolsAllowed: []string{"apply_patch"}}
	messages := workerPatchMessages(packet)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{"Return only valid JSON", "WeazlCode BRAID worker contract", "touch the file named by the latest failure first", "WorkerPatch", "files", "unified diff", "For existing files", "preserve all unrelated content", "Follow the task packet artifact contract exactly", "HTML fragments must not include doctype/html/head/body", "preserve their exact copy", "Do not shorten user-provided copy with ellipses", "replace them with real task output", "diff marker residue", "Never replace real existing file content with placeholders", "explicit imports between local modules", "Do not use wildcard imports", `"task_id": "task-1"`, "README.md"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("worker messages missing %q:\n%s", want, combined)
		}
	}
}

func TestBuildWorkerPacketAddsArtifactContracts(t *testing.T) {
	m := commandTestModel(t)
	tests := []struct {
		name string
		task coding.Task
		want []string
	}{
		{
			name: "html fragment",
			task: coding.Task{ID: "task-1", PlanID: "plan-1", Title: "Hero", Goal: "Create hero fragment.", Status: coding.TaskStatusPending, AllowedPaths: []string{"sections/hero.html"}, AcceptanceChecks: []coding.AcceptanceCheck{{Description: "hero exists"}}},
			want: []string{"HTML fragment/module only", "Do not include <!doctype>, <html>, <head>, or <body>"},
		},
		{
			name: "final html",
			task: coding.Task{ID: "task-2", PlanID: "plan-1", Title: "Assemble", Goal: "Assemble final page.", Status: coding.TaskStatusPending, AllowedPaths: []string{"index.html"}, AcceptanceChecks: []coding.AcceptanceCheck{{Description: "index exists"}}},
			want: []string{"index.html is the final assembled page", "complete browser-openable HTML document"},
		},
		{
			name: "css",
			task: coding.Task{ID: "task-3", PlanID: "plan-1", Title: "CSS", Goal: "Create styles.", Status: coding.TaskStatusPending, AllowedPaths: []string{"styles/cards.css"}, AcceptanceChecks: []coding.AcceptanceCheck{{Description: "css exists"}}},
			want: []string{"plain browser CSS only", "Do not use preprocessor-style nested rule blocks", "pseudo-elements are allowed"},
		},
		{
			name: "generated python module",
			task: coding.Task{ID: "task-4", PlanID: "plan-1", Title: "Bird module", Goal: "Create a pygame bird module.", Status: coding.TaskStatusPending, AllowedPaths: []string{"bird.py"}, AcceptanceChecks: []coding.AcceptanceCheck{{Description: "bird exists"}}},
			want: []string{"Maintainability contract", "Module contract", "use explicit imports", "Do not use wildcard imports"},
		},
		{
			name: "generated python entrypoint",
			task: coding.Task{ID: "task-5", PlanID: "plan-1", Title: "Main entrypoint", Goal: "Create a pygame game loop with smoke mode.", Status: coding.TaskStatusPending, AllowedPaths: []string{"main.py"}, AcceptanceChecks: []coding.AcceptanceCheck{{Description: "python main.py --smoke exits"}}},
			want: []string{"Maintainability contract", "Module contract", "Smoke contract", "Entrypoint contract", "smoke_test()", "event-loop variables"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet, err := m.buildWorkerPacket(tt.task)
			if err != nil {
				t.Fatalf("buildWorkerPacket: %v", err)
			}
			combined := packet.WorkerProfile + "\n" + packet.ContextPolicy.Instruction
			for _, want := range tt.want {
				if !strings.Contains(combined, want) {
					t.Fatalf("packet missing %q:\n%s", want, combined)
				}
			}
		})
	}
}

func TestBuildWorkerPacketIncludesDoneDependencyOutputsAsContext(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sections"), 0o755); err != nil {
		t.Fatalf("MkdirAll sections: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "styles"), 0o755); err != nil {
		t.Fatalf("MkdirAll styles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sections", "hero.html"), []byte("<section>Exact hero copy</section>\n"), 0o644); err != nil {
		t.Fatalf("WriteFile hero: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles", "base.css"), []byte(":root { color: white; }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile css: %v", err)
	}
	m := commandTestModel(t)
	m.project.Root = root
	m.session.ProjectRoot = root
	plan := coding.Plan{
		ID:          "plan-1",
		SessionID:   m.session.ID,
		ProjectRoot: root,
		Title:       "Assemble site",
		Summary:     "Assemble dependency outputs.",
		Status:      coding.PlanStatusApproved,
		Tasks: []coding.Task{
			{ID: "hero", PlanID: "plan-1", Title: "Hero", Goal: "Create hero fragment without doctype, html, head, or body.", Status: coding.TaskStatusDone, AllowedPaths: []string{"sections/hero.html"}, AcceptanceChecks: []coding.AcceptanceCheck{{Description: "hero exists"}}},
			{ID: "base-css", PlanID: "plan-1", Title: "Base CSS", Goal: "Create base CSS.", Status: coding.TaskStatusDone, InterfaceContract: coding.InterfaceContract{Summary: "Exports base stylesheet variables.", Exports: []string{":root variables"}}, AllowedPaths: []string{"styles/base.css"}, AcceptanceChecks: []coding.AcceptanceCheck{{Description: "css exists"}}},
			{ID: "assemble", PlanID: "plan-1", Title: "Assemble page", Goal: "Assemble final page from dependency outputs.", Status: coding.TaskStatusPending, AllowedPaths: []string{"index.html"}, DependsOn: []string{"hero", "base-css"}, AcceptanceChecks: []coding.AcceptanceCheck{{Description: "index includes dependency outputs"}}},
		},
	}
	if err := m.store.SavePlan(plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	packet, err := m.buildWorkerPacket(plan.Tasks[2])
	if err != nil {
		t.Fatalf("buildWorkerPacket: %v", err)
	}
	contextByPath := map[string]string{}
	for _, file := range packet.ContextFiles {
		contextByPath[file.Path] = file.Content
	}
	if contextByPath["sections/hero.html"] != "<section>Exact hero copy</section>\n" {
		t.Fatalf("hero dependency context missing: %#v", packet.ContextFiles)
	}
	if contextByPath["styles/base.css"] != ":root { color: white; }\n" {
		t.Fatalf("css dependency context missing: %#v", packet.ContextFiles)
	}
	if _, ok := contextByPath["index.html"]; ok {
		t.Fatalf("missing create target should not be included as context: %#v", packet.ContextFiles)
	}
	if len(packet.DependencyContracts) != 2 {
		t.Fatalf("dependency contracts = %#v", packet.DependencyContracts)
	}
	contractsByID := map[string]coding.InterfaceContract{}
	for _, contract := range packet.DependencyContracts {
		contractsByID[contract.TaskID] = contract.InterfaceContract
	}
	if !strings.Contains(contractsByID["hero"].Summary, "Create hero fragment") {
		t.Fatalf("derived hero contract = %#v", contractsByID["hero"])
	}
	if contractsByID["base-css"].Summary != "Exports base stylesheet variables." {
		t.Fatalf("base css contract = %#v", contractsByID["base-css"])
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
	if len(events) != 5 || events[3].Type != "output_cleanup" || events[4].Type != "worker_blocker" {
		t.Fatalf("events = %#v", events)
	}
}

func TestSlashWorkerPatchIgnoresNoneBlocker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := commandTestModel(t)
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"None blocker","summary":"Handle placeholder blocker","tasks":[{"title":"Update README","goal":"Update README.md content.","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README changed"}]}]}`
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
	raw, err := json.Marshal(coding.WorkerPatch{
		TaskID:  plan.Tasks[0].ID,
		Summary: "Updated README",
		Blocker: "None",
		Files:   []coding.WorkerFileEdit{{Path: "README.md", Content: "new\n"}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(raw))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "task reviewing" {
		t.Fatalf("status = %q, want task reviewing", got.status)
	}
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Tasks[0].Status != coding.TaskStatusReviewing {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
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
	if len(events) != 6 || events[3].Type != "worker_patch_attempt" || events[4].Type != "worker_patch" || events[5].Type != "verification" || len(events[5].Payload) == 0 {
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

func TestSlashWorkerPatchApplyFailureBlocksAndRestoresBaseline(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch README","summary":"Apply worker patch","tasks":[{"title":"Update README","goal":"Change README text","allowed_paths":["README.md"],"acceptance_checks":[{"description":"README changed"}]}]}`
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
@@ -10,1 +10,1 @@
-missing
+new
`
	rawPatch, err := json.Marshal(coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Summary: "Bad hunk", Patch: patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "worker patch apply failed" {
		t.Fatalf("status = %q, want worker patch apply failed", got.status)
	}
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan after patch: %v", err)
	}
	if !ok || plan.Tasks[0].Status != coding.TaskStatusBlocked {
		t.Fatalf("task status = %#v ok=%v", plan.Tasks, ok)
	}
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "old\n" {
		t.Fatalf("README was not restored after apply failure: %q", data)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if events[len(events)-2].Type != "output_cleanup" || events[len(events)-1].Type != "worker_error" {
		t.Fatalf("events = %#v", events)
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
		t.Fatalf("status = %q, want worker patch rejected; view:\n%s", got.status, got.viewport.View())
	}
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan after rejection: %v", err)
	}
	if !ok || plan.Tasks[0].Status != coding.TaskStatusBlocked {
		t.Fatalf("task status after rejection = %#v ok=%v, want blocked", plan.Tasks, ok)
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
	if !retryableWorkerErrorTask(events) {
		t.Fatalf("worker_rejected task is not retryable: %#v", events)
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
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan after rejection: %v", err)
	}
	if !ok || plan.Tasks[0].Status != coding.TaskStatusBlocked {
		t.Fatalf("task status after rejection = %#v ok=%v, want blocked", plan.Tasks, ok)
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

func TestSlashWorkerPatchAllowsArtifactFullFileRewrite(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	var oldContent string
	for i := 0; i < 100; i++ {
		oldContent += fmt.Sprintf("line %03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(oldContent), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles.css"), []byte("body { color: black; }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile styles: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Landing page","summary":"Create artifact","tasks":[{"id":"artifact","title":"Build landing page","goal":"Create a complete cohesive landing page artifact.","allowed_paths":["index.html","styles.css"],"acceptance_checks":[{"description":"index and styles are complete"}]}]}`
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
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{
		TaskID:  plan.Tasks[0].ID,
		Summary: "Rewrite artifact",
		Files: []coding.WorkerFileEdit{
			{Path: "index.html", Content: "<!doctype html>\n<html><head><link rel=\"stylesheet\" href=\"styles.css\"></head><body><main>new artifact</main></body></html>\n"},
			{Path: "styles.css", Content: "body { color: white; background: black; }\n"},
		},
	})
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
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan after patch: %v ok=%v", err, ok)
	}
	if plan.Tasks[0].Status != coding.TaskStatusReviewing {
		t.Fatalf("task status = %s, want reviewing", plan.Tasks[0].Status)
	}
	data, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatalf("ReadFile index: %v", err)
	}
	if !strings.Contains(string(data), "new artifact") {
		t.Fatalf("index was not rewritten: %q", data)
	}
}

func TestSlashWorkerPatchRunsArtifactValidationBeforeReview(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Landing page","summary":"Create artifact","tasks":[{"id":"artifact","title":"Build landing page","goal":"Create a complete cohesive landing page artifact.","allowed_paths":["index.html","styles.css"],"acceptance_checks":[{"description":"index and styles are complete"}]}]}`
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
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{
		TaskID:  plan.Tasks[0].ID,
		Summary: "Invalid artifact",
		Files: []coding.WorkerFileEdit{
			{Path: "index.html", Content: "<!doctype html>\n<html><head><style>body{}</style><link rel=\"stylesheet\" href=\"styles.css\"></head><body><main>new artifact</main></body></html>\n"},
			{Path: "styles.css", Content: ".card: hover { color: red; }\n"},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "artifact validation failed" {
		t.Fatalf("status = %q, want artifact validation failed", got.status)
	}
	plan, ok, err = got.store.LatestPlan(got.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan after patch: %v ok=%v", err, ok)
	}
	if plan.Tasks[0].Status != coding.TaskStatusBlocked {
		t.Fatalf("task status = %s, want blocked", plan.Tasks[0].Status)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	foundValidation := false
	for _, event := range events {
		if event.Type == "artifact_validation" {
			foundValidation = true
			if !strings.Contains(event.Message, "inline <style>") || !strings.Contains(event.Message, "pseudo-selector") {
				t.Fatalf("validation message = %q", event.Message)
			}
		}
	}
	if !foundValidation {
		t.Fatalf("events = %#v", events)
	}
	if !retryableWorkerErrorTask(events) {
		t.Fatalf("artifact validation should be retryable: %#v", events)
	}
}

func TestArtifactValidationEscalatesAfterRepeatedLocalFailures(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Landing page","summary":"Create artifact","tasks":[{"id":"artifact","title":"Build landing page","goal":"Create a complete cohesive landing page artifact.","allowed_paths":["index.html","styles.css"],"acceptance_checks":[{"description":"index and styles are complete"}]}]}`
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
	invalidPatch := func(taskID string, attempt int) string {
		rawPatch, err := json.Marshal(coding.WorkerPatch{
			TaskID:  taskID,
			Summary: "Invalid artifact",
			Files: []coding.WorkerFileEdit{
				{Path: "index.html", Content: fmt.Sprintf("<!doctype html>\n<html><head><style>body{}</style><link rel=\"stylesheet\" href=\"styles.css\"></head><body><main>new artifact %d</main></body></html>\n", attempt)},
				{Path: "styles.css", Content: ".card: hover { color: red; }\n"},
			},
		})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		return string(rawPatch)
	}
	for attempt := 1; attempt <= maxLocalArtifactValidationRepairPasses; attempt++ {
		updated, _, handled = m.handleSlashCommand("/run-task")
		if !handled {
			t.Fatalf("run-task handled = false on attempt %d", attempt)
		}
		m = updated.(model)
		updated, _, handled = m.handleSlashCommand("/worker-patch " + invalidPatch("artifact", attempt))
		if !handled {
			t.Fatalf("worker-patch handled = false on attempt %d", attempt)
		}
		m = updated.(model)
	}
	if m.status != "artifact validation escalated" {
		t.Fatalf("status = %q, want artifact validation escalated", m.status)
	}
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	if plan.Tasks[0].Status != coding.TaskStatusReviewing {
		t.Fatalf("task status = %s, want reviewing", plan.Tasks[0].Status)
	}
	events, err := m.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if artifactValidationFailureCount(events) != maxLocalArtifactValidationRepairPasses || !taskEventsContainType(events, "artifact_validation_escalated") {
		t.Fatalf("events = %#v", events)
	}
	issues := m.reviewApprovalIssues(plan.Tasks[0])
	if !containsSubstring(issues, "local artifact validation is still failing") {
		t.Fatalf("review approval issues = %#v", issues)
	}
}

func TestDeterministicArtifactValidationGetsMoreLocalRepairPasses(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Script","summary":"Create Python artifact","tasks":[{"id":"script","title":"Create script","goal":"Create a standalone Python script.","allowed_paths":["app.py"],"acceptance_checks":[{"description":"app.py compiles"}]}]}`
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
	invalidPatch := func(attempt int) string {
		rawPatch, err := json.Marshal(coding.WorkerPatch{
			TaskID:  "script",
			Summary: "Invalid Python",
			Files:   []coding.WorkerFileEdit{{Path: "app.py", Content: fmt.Sprintf("def main_%d():\n    return missing_name_%d\n", attempt, attempt)}},
		})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		return string(rawPatch)
	}
	for attempt := 1; attempt < maxDeterministicArtifactRepairPasses; attempt++ {
		updated, _, handled = m.handleSlashCommand("/run-task")
		if !handled {
			t.Fatalf("run-task handled = false on attempt %d", attempt)
		}
		m = updated.(model)
		updated, _, handled = m.handleSlashCommand("/worker-patch " + invalidPatch(attempt))
		if !handled {
			t.Fatalf("worker-patch handled = false on attempt %d", attempt)
		}
		m = updated.(model)
		if m.status != "artifact validation failed" {
			t.Fatalf("attempt %d status = %q, want local artifact validation failed", attempt, m.status)
		}
		plan, ok, err := m.store.LatestPlan(m.session.ID)
		if err != nil || !ok {
			t.Fatalf("LatestPlan: %v ok=%v", err, ok)
		}
		if plan.Tasks[0].Status != coding.TaskStatusBlocked {
			t.Fatalf("attempt %d task status = %s, want blocked", attempt, plan.Tasks[0].Status)
		}
	}
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false on final attempt")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/worker-patch " + invalidPatch(maxDeterministicArtifactRepairPasses))
	if !handled {
		t.Fatal("worker-patch handled = false on final attempt")
	}
	m = updated.(model)
	if m.status != "artifact validation escalated" {
		t.Fatalf("status = %q, want artifact validation escalated", m.status)
	}
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	if plan.Tasks[0].Status != coding.TaskStatusReviewing {
		t.Fatalf("task status = %s, want reviewing", plan.Tasks[0].Status)
	}
}

func TestDetectIdenticalRepairUsesPatchAttemptEvents(t *testing.T) {
	patch := coding.WorkerPatch{
		TaskID:  "script",
		Summary: "Repair script",
		Files:   []coding.WorkerFileEdit{{Path: "app.py", Content: "print('same')\n"}},
	}
	payload, err := json.Marshal(struct {
		Hash string `json:"hash"`
	}{Hash: hashWorkerPatch(patch)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	events := []coding.TaskEvent{
		{Type: "repair_start", Message: "repair started"},
		{Type: "worker_patch_attempt", Payload: payload},
	}
	if !repairCycleActive(events) {
		t.Fatalf("repairCycleActive = false")
	}
	if !detectIdenticalRepair(patch, events) {
		t.Fatalf("detectIdenticalRepair = false, want true")
	}
	changed := patch
	changed.Files = []coding.WorkerFileEdit{{Path: "app.py", Content: "print('changed')\n"}}
	if detectIdenticalRepair(changed, events) {
		t.Fatalf("detectIdenticalRepair changed patch = true, want false")
	}
}

func TestRepairEffectivenessUsesValidationFingerprints(t *testing.T) {
	repeated := []coding.TaskEvent{
		{Type: "artifact_validation", Message: "Artifact validation failed before reviewer handoff.\n- app.py: function main references undefined name event"},
		{Type: "artifact_validation", Message: "Artifact validation failed before reviewer handoff.\n- app.py: function main references undefined name event"},
	}
	if repairEffectiveness(repeated) {
		t.Fatalf("repairEffectiveness repeated issue = true, want false")
	}
	changed := []coding.TaskEvent{
		{Type: "artifact_validation", Message: "Artifact validation failed before reviewer handoff.\n- app.py: function main references undefined name event"},
		{Type: "artifact_validation", Message: "Artifact validation failed before reviewer handoff.\n- app.py: function main references undefined name font"},
	}
	if !repairEffectiveness(changed) {
		t.Fatalf("repairEffectiveness changed issue = false, want true")
	}
}

func TestDeterministicReviewerRepairDetectsPythonLocalAssignment(t *testing.T) {
	verdict := coding.ReviewVerdict{
		Verdict: coding.ReviewNeedsFix,
		Summary: "bird_flapped is still read before assignment in main(), causing an UnboundLocalError at runtime.",
	}
	payload, err := json.Marshal(verdict)
	if err != nil {
		t.Fatalf("Marshal verdict: %v", err)
	}
	events := []coding.TaskEvent{{Type: "reviewer_verdict", Payload: payload}}
	if !latestReviewerNeedsDeterministicRepair(events) {
		t.Fatalf("latestReviewerNeedsDeterministicRepair = false, want true")
	}
}

func TestPythonStateRepairGuidanceOnlyForLocalAssignmentFailures(t *testing.T) {
	if got := pythonStateRepairGuidance("function main reads local name score before assigning it"); !strings.Contains(got, "Initialize that state inside the function") {
		t.Fatalf("guidance = %q", got)
	}
	if got := pythonStateRepairGuidance("make the bird more colorful"); got != "" {
		t.Fatalf("guidance = %q, want empty", got)
	}
}

func TestArtifactValidationChecksSourceCopyAndAssets(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hero.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile hero: %v", err)
	}
	m.project.Root = root
	task := coding.Task{
		ID:           "artifact",
		PlanID:       "plan",
		Title:        "Build landing page with image assets",
		Goal:         "Create a complete static website using image assets.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"index.html", "styles.css"},
		AcceptanceChecks: []coding.AcceptanceCheck{{
			Description: sourceCopyContractPrefix + "\nExact phrase from supplied copy.",
		}},
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html><html><head><link rel=\"stylesheet\" href=\"styles.css\"></head><body><main>Different copy</main></body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles.css"), []byte("body { color: black; }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile styles: %v", err)
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "required source-copy fragment") || !strings.Contains(joined, "hero.png") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestArtifactValidationChecksPythonRuntimeShape(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `class Game:
    def __init__(self):
        self.smoke_test = False

    def draw(self):
        hit_rect = object()

    def handle_input(self):
        if hit_rect:
            print("hit")

    def smoke_test(self):
        print("smoke")
`
	if err := os.WriteFile(filepath.Join(root, "blackjack.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile blackjack: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame blackjack",
		Goal:         "Create a Python game.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"blackjack.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "undefined name hit_rect") || !strings.Contains(joined, "shadows method smoke_test") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestArtifactValidationFlagsPythonWildcardImports(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `from config import *

def draw():
    return SCREEN_WIDTH
`
	if err := os.WriteFile(filepath.Join(root, "game.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile game: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame game",
		Goal:         "Create a Python game.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"game.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "wildcard import from config cannot be statically validated") || !strings.Contains(joined, "replace it with explicit imported names") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestArtifactValidationChecksPythonInterfaceContract(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `class Bird:
    def __init__(self, x, y, color):
        self.x = x
        self.y = y

    def flap(self, force):
        pass

def draw_background(surface, width, height):
    pass
`
	if err := os.WriteFile(filepath.Join(root, "renderer.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile renderer: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build renderer module",
		Goal:         "Create Python renderer module.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"renderer.py"},
		InterfaceContract: coding.InterfaceContract{
			Exports:      []string{"Bird", "MissingExport"},
			Constructors: []string{"Bird(x: int, y: int)"},
			Methods:      []string{"flap() -> None", "draw_background(surface) -> None"},
		},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{
		"interface contract export missing: MissingExport",
		"interface contract signature mismatch for Bird: unexpected extra parameter(s) color",
		"interface contract signature mismatch for Bird.flap: unexpected extra parameter(s) force",
		"interface contract signature mismatch for draw_background: unexpected extra parameter(s) width, height",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("issues missing %q: %#v", want, issues)
		}
	}
}

func TestArtifactValidationAllowsMatchingPythonInterfaceContract(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `import pygame

class Bird:
    def __init__(self, x, y):
        self.x = x
        self.y = y

    def flap(self):
        pass

def draw_background(surface):
    pygame.draw.circle(surface, (255, 255, 0), (10, 10), 5)
    pass
`
	if err := os.WriteFile(filepath.Join(root, "renderer.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile renderer: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build renderer module",
		Goal:         "Create Python renderer module.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"renderer.py"},
		InterfaceContract: coding.InterfaceContract{
			Exports:      []string{"Bird", "draw_background"},
			Constructors: []string{"Bird(x: int, y: int)"},
			Methods:      []string{"flap() -> None", "draw_background(surface) -> None"},
		},
	}
	issues := m.validateArtifactTaskOutput(task)
	for _, issue := range issues {
		if strings.Contains(issue.Message, "interface contract") {
			t.Fatalf("unexpected interface issue: %#v", issues)
		}
	}
}

func TestArtifactValidationChecksPythonModuleLevelUndefinedAndSmokePath(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `import pygame

pygame.init()
screen = pygame.display.set_mode((800, 600))
value = random.randint(1, 10)
running = True
while running:
    for event in pygame.event.get():
        if event.type == pygame.QUIT:
            running = False
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame game",
		Goal:         "Create an interactive Python game.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "module top-level references undefined name random") || !strings.Contains(joined, "has no --smoke path") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestArtifactValidationAllowsPygameLeafModuleWithoutSmokePath(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `import pygame

class Bird:
    def draw_once(self, screen):
        running = True
        while running:
            pygame.draw.circle(screen, (255, 200, 0), (10, 10), 5)
            running = False
`
	if err := os.WriteFile(filepath.Join(root, "bird.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile bird: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build bird module",
		Goal:         "Create a Pygame bird module.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"bird.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	if strings.Contains(strings.Join(messages, "\n"), "has no --smoke path") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestArtifactValidationChecksPythonLocalImportExports(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	if err := os.WriteFile(filepath.Join(root, "config.py"), []byte("PIPE_SPAWN_INTERVAL = 1500\n"), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	source := `from config import PIPE_SPAN_INTERVAL

def main():
    return PIPE_SPAN_INTERVAL
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build Python app",
		Goal:         "Create an app.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "from config import PIPE_SPAN_INTERVAL references missing local export") || !strings.Contains(joined, "PIPE_SPAWN_INTERVAL") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestArtifactValidationChecksPythonSmokeBranch(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `import sys

class Game:
    def play(self):
        pass

    def render_smoke_frame(self):
        pass

if __name__ == "__main__":
    game = Game()
    if "--smoke" in sys.argv:
        game.smoke = True
    game.play()
    if game.smoke:
        game.render_smoke_frame()
`
	if err := os.WriteFile(filepath.Join(root, "game.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile game: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame game",
		Goal:         "Create a Python game with --smoke.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"game.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "does not call a smoke routine") || !strings.Contains(joined, "unconditional interactive loop") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestArtifactValidationChecksNestedInteractiveLoop(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `def main():
    running = True
    while running:
        if running:
            while True:
                break

if __name__ == "__main__":
    main()
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame game",
		Goal:         "Create a Python game.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	if !strings.Contains(strings.Join(messages, "\n"), "nested while True inside another loop") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestArtifactValidationChecksBranchScopedEntryPointVariables(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `import pygame

def main():
    running = True
    while running:
        for event in pygame.event.get():
            if event.type == pygame.QUIT:
                running = False
        if event.type == pygame.KEYDOWN:
            running = False
        if running:
            font = pygame.font.Font(None, 32)
        text = font.render("Game Over", True, (255, 255, 255))
        if not running:
            game_over_surface = pygame.Surface((100, 100))
        game_over_surface.fill((0, 0, 0))

if __name__ == "__main__":
    main()
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame entrypoint",
		Goal:         "Create a Python game entrypoint.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{"loop-scoped local event", "conditional-scoped local font", "conditional-scoped local game_over_surface"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("issues missing %q: %#v", want, issues)
		}
	}
}

func TestArtifactValidationAllowsLoopVariableInsideLoop(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `class Pipe:
    def update(self):
        pass

def tick(pipes):
    stale = []
    for pipe in pipes:
        pipe.update()
        if pipe in stale:
            stale.remove(pipe)
    for pipe in stale:
        pipes.remove(pipe)
`
	if err := os.WriteFile(filepath.Join(root, "state.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build state module",
		Goal:         "Create a Python game state module.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"state.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	for _, issue := range issues {
		if strings.Contains(issue.Message, "loop-scoped local pipe") {
			t.Fatalf("unexpected loop-scoped issue: %#v", issues)
		}
	}
}

func TestArtifactValidationAllowsLoopNameReassignedAfterLoop(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `class Pipe:
    def __init__(self, x):
        self.x = x
    def update(self):
        pass
    def is_off_screen(self):
        return False

class GameState:
    def __init__(self):
        self.pipes = []

    def update(self):
        for pipe in self.pipes[:]:
            pipe.update()
            if pipe.is_off_screen():
                self.pipes.remove(pipe)
        pipe = Pipe(100)
        self.pipes.append(pipe)
`
	if err := os.WriteFile(filepath.Join(root, "entities.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile entities: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build game entities",
		Goal:         "Create a Python game state module.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"entities.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	for _, issue := range issues {
		if strings.Contains(issue.Message, "loop-scoped local pipe") {
			t.Fatalf("unexpected loop-scoped issue: %#v", issues)
		}
	}
}

func TestArtifactValidationAllowsTopLevelLoopAndComprehensionTargets(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `items = [1, 2, 3]
running = True
while running:
    for event in items:
        if event == 2:
            running = False
    best = max(p for p in items if p > 0)
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build Python entrypoint",
		Goal:         "Create a Python entrypoint.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	for _, issue := range issues {
		if strings.Contains(issue.Message, "undefined name event") || strings.Contains(issue.Message, "undefined name p") {
			t.Fatalf("unexpected module target issue: %#v", issues)
		}
	}
}

func TestArtifactValidationAllowsTopLevelConditionalFunctionDefinitions(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `import sys

if "--smoke" in sys.argv:
    def smoke_test():
        return 0
    smoke_test()

if __name__ == "__main__":
    def main():
        return 0
    main()
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build entrypoint",
		Goal:         "Create a Python entrypoint.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	for _, issue := range issues {
		if strings.Contains(issue.Message, "undefined name main") || strings.Contains(issue.Message, "undefined name smoke_test") {
			t.Fatalf("unexpected module undefined issue: %#v", issues)
		}
	}
}

func TestArtifactValidationAllowsPreviouslyInitializedLocalsAssignedInBranches(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `def main():
    score = 0
    game_over = False
    running = True
    while running:
        if not game_over:
            score += 1
        if score > 10:
            game_over = True
        print(score, game_over)
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build entrypoint",
		Goal:         "Create a Python entrypoint.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	for _, issue := range issues {
		if strings.Contains(issue.Message, "conditional-scoped local score") || strings.Contains(issue.Message, "conditional-scoped local game_over") {
			t.Fatalf("unexpected conditional-scoped issue: %#v", issues)
		}
	}
}

func TestArtifactValidationChecksPythonLocalUseBeforeAssignment(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `score = 0
pipe_group = []

def main():
    print(score)
    score += 1
    print(pipe_group)
    pipe_group = [pipe for pipe in pipe_group if pipe]
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame entrypoint",
		Goal:         "Create a Python game entrypoint.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{"reads local name pipe_group before assigning", "reads local name score before assigning"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("issues missing %q: %#v", want, issues)
		}
	}
}

func TestArtifactValidationAllowsInitializedPythonLocals(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `def main():
    score = 0
    pipe_group = []
    print(score, pipe_group)
    score += 1
    pipe_group = [pipe for pipe in pipe_group if pipe]
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame entrypoint",
		Goal:         "Create a Python game entrypoint.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"main.py"},
	}
	issues := m.validateArtifactTaskOutput(task)
	for _, issue := range issues {
		if strings.Contains(issue.Message, "reads local name") {
			t.Fatalf("unexpected local assignment issue: %#v", issues)
		}
	}
}

func TestArtifactValidationAllowsPythonClosureHelpers(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	source := `def main():
    player = object()

    def draw_card():
        return player

    draw_card()

if __name__ == "__main__":
    main()
`
	if err := os.WriteFile(filepath.Join(root, "game.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile game: %v", err)
	}
	task := coding.Task{
		ID:           "python",
		PlanID:       "plan",
		Title:        "Build pygame game",
		Goal:         "Create a Python game.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"game.py"},
	}
	if issues := m.validateArtifactTaskOutput(task); len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestBuildWorkerPacketForArtifactValidationRepairFocus(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html><html><head><style>body{}</style></head><body></body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles.css"), []byte("body { color: red; }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile styles: %v", err)
	}
	rawPlan := `{"title":"Landing page","summary":"Create artifact","tasks":[{"id":"artifact","title":"Build landing page","goal":"Create a complete cohesive landing page artifact.","allowed_paths":["index.html","styles.css"],"acceptance_checks":[{"description":"index and styles are complete"}]}]}`
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
	if err := m.store.UpdateTaskStatus("artifact", coding.TaskStatusBlocked); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  "artifact",
		Type:    "artifact_validation",
		Message: "Artifact validation failed before reviewer handoff.\n- index.html: inline <style> tag",
	})
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	packet, err := m.buildWorkerPacketForRun(plan.Tasks[0])
	if err != nil {
		t.Fatalf("buildWorkerPacketForRun: %v", err)
	}
	if !strings.Contains(packet.Goal, "Artifact validation repair focus") || !strings.Contains(packet.Goal, "inline <style>") || strings.Contains(packet.Goal, "before producing a patch") {
		t.Fatalf("packet goal = %s", packet.Goal)
	}
}

func TestBuildWorkerPacketFullFileRepairForBrokenSingleFileArtifact(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("def from fastapi.testclient import TestClient\n"), 0o644); err != nil {
		t.Fatalf("WriteFile app: %v", err)
	}
	rawPlan := `{"title":"API","summary":"Create artifact","tasks":[{"id":"api","title":"Build FastAPI app","goal":"Create a standalone FastAPI SQLite task API in app.py.","allowed_paths":["app.py"],"acceptance_checks":[{"description":"app.py runs with --smoke"}]}]}`
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
	if err := m.store.UpdateTaskStatus("api", coding.TaskStatusBlocked); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  "api",
		Type:    "artifact_validation",
		Message: "Artifact validation failed before reviewer handoff.\n- app.py: python compile failed: invalid syntax",
	})
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  "api",
		Type:    "repair_requested",
		Message: "Fix invalid syntax and make the smoke path pass.",
	})
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	packet, err := m.buildWorkerPacketForRun(plan.Tasks[0])
	if err != nil {
		t.Fatalf("buildWorkerPacketForRun: %v", err)
	}
	for _, want := range []string{"complete replacement content", "leave patch empty", "reconstruct a clean complete file", "ignore the broken current content", "invalid syntax"} {
		if !strings.Contains(packet.Goal, want) {
			t.Fatalf("packet goal missing %q:\n%s", want, packet.Goal)
		}
	}
}

func TestBuildWorkerPacketKeepsArtifactValidationAfterReviewerVerdict(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("print('broken'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile app: %v", err)
	}
	rawPlan := `{"title":"App","summary":"Create artifact","tasks":[{"id":"app","title":"Build Python app","goal":"Create a standalone Python app in app.py.","allowed_paths":["app.py"],"acceptance_checks":[{"description":"app.py runs with --smoke"}]}]}`
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
	if err := m.store.UpdateTaskStatus("app", coding.TaskStatusBlocked); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{TaskID: "app", Type: "artifact_validation", Message: "Artifact validation failed before reviewer handoff.\n- app.py: syntax error at line 1: '(' was never closed"})
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{TaskID: "app", Type: "artifact_validation_escalated", Message: "Repeated local artifact validation failures; escalating current output to frontier reviewer for a focused repair brief."})
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{TaskID: "app", Type: "reviewer_verdict", Message: "needs_fix"})
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{TaskID: "app", Type: "repair_requested", Message: "The file is truncated; reconstruct it."})
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	packet, err := m.buildWorkerPacketForRun(plan.Tasks[0])
	if err != nil {
		t.Fatalf("buildWorkerPacketForRun: %v", err)
	}
	for _, want := range []string{"Latest artifact validation failure", "syntax error at line 1", "ignore the broken current content"} {
		if !strings.Contains(packet.Goal, want) {
			t.Fatalf("packet goal missing %q:\n%s", want, packet.Goal)
		}
	}
}

func TestLocalArtifactValidationRepairLimitEscalatesSyntaxImmediately(t *testing.T) {
	task := coding.Task{ID: "app", Title: "Build app", Goal: "Create app.", AllowedPaths: []string{"app.py"}}
	issues := []artifactValidationIssue{{Path: "app.py", Message: "syntax error at line 1: '(' was never closed"}}
	if got := localArtifactValidationRepairLimit(task, issues); got != 1 {
		t.Fatalf("localArtifactValidationRepairLimit = %d, want 1", got)
	}
}

func TestArtifactValidationNormalizesCopyAndCSSAssetRefs(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hero.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile hero: %v", err)
	}
	m.project.Root = root
	task := coding.Task{
		ID:           "artifact",
		PlanID:       "plan",
		Title:        "Build landing page with image assets",
		Goal:         "Create a complete static website using image assets.",
		Status:       coding.TaskStatusRunning,
		AllowedPaths: []string{"index.html", "styles.css"},
		AcceptanceChecks: []coding.AcceptanceCheck{{
			Description: sourceCopyContractPrefix + "\nlocal-first terminal apps\nWeazl mascot using a retro terminalUnapologetically local. Terminally weird.",
		}},
	}
	index := `<!doctype html>
<html><head><link rel="stylesheet" href="styles.css"></head><body>
<p>local‑first terminal apps</p>
<img src="weazl_mascot.png" alt="Weazl mascot using a retro terminal">
<p>Unapologetically local. Terminally weird.</p>
</body></html>`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles.css"), []byte("body { background: url('hero.png'); }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile styles: %v", err)
	}
	if issues := m.validateArtifactTaskOutput(task); len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestSlashWorkerPatchRejectsPlaceholderRewrite(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>real</main>\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch HTML","summary":"Apply worker edit","tasks":[{"title":"Update HTML","goal":"Add features section","allowed_paths":["index.html"],"acceptance_checks":[{"description":"features added"}]}]}`
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
	rawPatch, err := json.Marshal(coding.WorkerPatch{
		TaskID:  plan.Tasks[0].ID,
		Summary: "Placeholder rewrite",
		Files: []coding.WorkerFileEdit{{
			Path:    "index.html",
			Content: "<!-- Existing content of index.html -->\n<section id=\"features\"></section>\n<!-- Rest of the file content -->\n",
		}},
	})
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
	if !strings.Contains(got.viewport.View(), "placeholder sentinel") {
		t.Fatalf("viewport missing placeholder rejection: %q", got.viewport.View())
	}
	data, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "<main>real</main>\n" {
		t.Fatalf("index.html changed despite rejection: %q", data)
	}
}

func TestSlashWorkerPatchRejectsDiffForSingleFileGeneratedArtifact(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Game","summary":"Create game","tasks":[{"id":"game","title":"Create pygame blackjack","goal":"Build a standalone Python game.","allowed_paths":["blackjack.py"],"acceptance_checks":[{"description":"blackjack.py exists"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || len(plan.Tasks) != 1 {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	if err := m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusApproved); err != nil {
		t.Fatalf("UpdatePlanStatus: %v", err)
	}
	if err := m.store.UpdateTaskStatus(plan.Tasks[0].ID, coding.TaskStatusRunning); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{
		TaskID:  plan.Tasks[0].ID,
		Summary: "Diff output",
		Patch: `diff --git a/blackjack.py b/blackjack.py
new file mode 100644
--- /dev/null
+++ b/blackjack.py
@@ -0,0 +1 @@
+print("game")
`,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "worker patch rejected" {
		t.Fatalf("status = %q, want worker patch rejected; view:\n%s", got.status, got.viewport.View())
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if events[len(events)-1].Type != "worker_rejected" || !strings.Contains(events[len(events)-1].Message, "files[] with complete content") {
		t.Fatalf("events = %#v", events)
	}
	if !retryableWorkerErrorTask(events) {
		t.Fatalf("single-file artifact diff rejection should be retryable: %#v", events)
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

func TestRepeatedWorkerRejectionStopsRetry(t *testing.T) {
	payload := json.RawMessage(`{"paths":["src/pipe.py"],"allowed_paths":["src/pipes.py"],"error":"patch path \"src/pipe.py\" is outside allowed paths"}`)
	events := []coding.TaskEvent{
		{Type: "approval", Message: "approved"},
		{Type: "worker_start", Message: "started"},
		{Type: "worker_rejected", Message: "Worker patch rejected: returned paths are outside the task scope.", Payload: payload},
	}
	if !retryableWorkerErrorTask(events) {
		t.Fatalf("first worker rejection should allow one retry: %#v", events)
	}
	events = append(events,
		coding.TaskEvent{Type: "repair_start", Message: "retry"},
		coding.TaskEvent{Type: "worker_rejected", Message: "Worker patch rejected: returned paths are outside the task scope.", Payload: payload},
	)
	if retryableWorkerErrorTask(events) {
		t.Fatalf("repeated identical worker rejection should stop retrying: %#v", events)
	}
}

func TestDeterministicReviewerIssueGetsOneBonusRepairAttempt(t *testing.T) {
	verdictPayload, err := json.Marshal(coding.ReviewVerdict{
		Verdict: coding.ReviewNeedsFix,
		Summary: "The repair has a missing import and an undefined name.",
		Issues:  []string{"random is not imported"},
	})
	if err != nil {
		t.Fatalf("Marshal verdict: %v", err)
	}
	events := []coding.TaskEvent{
		{Type: "artifact_validation", Message: "interactive Python/Pygame artifact has no --smoke path"},
		{Type: "repair_requested"},
		{Type: "artifact_validation", Message: "interactive Python/Pygame artifact has no --smoke path"},
		{Type: "repair_requested"},
		{Type: "reviewer_verdict", Payload: verdictPayload},
		{Type: "repair_requested"},
	}
	if got := repairAttemptLimit(coding.Task{}, events); got != maxRepairAttempts+1 {
		t.Fatalf("repairAttemptLimit = %d, want %d", got, maxRepairAttempts+1)
	}
	if !repairableTask(coding.Task{}, events) {
		t.Fatalf("deterministic reviewer issue should be repairable for one bonus attempt")
	}
}

func TestRuntimeTypeErrorGetsOneBonusRepairAttempt(t *testing.T) {
	verdictPayload, err := json.Marshal(coding.ReviewVerdict{
		Verdict: coding.ReviewNeedsFix,
		Summary: "The smoke test fails with TypeError: Bird.__init__() missing 2 required positional arguments: 'x' and 'y'.",
		Issues:  []string{"Fix the constructor call in the entrypoint."},
	})
	if err != nil {
		t.Fatalf("Marshal verdict: %v", err)
	}
	events := []coding.TaskEvent{
		{Type: "repair_requested"},
		{Type: "repair_requested"},
		{Type: "reviewer_verdict", Payload: verdictPayload},
		{Type: "repair_requested"},
	}
	if got := repairAttemptLimit(coding.Task{}, events); got != maxRepairAttempts+1 {
		t.Fatalf("repairAttemptLimit = %d, want %d", got, maxRepairAttempts+1)
	}
	if !repairableTask(coding.Task{}, events) {
		t.Fatalf("runtime type error should be repairable for one bonus attempt")
	}
}

func TestBuildWorkerPacketAddsPathRejectionGuidance(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Pipe module","summary":"Create pipe module","tasks":[{"id":"pipes","title":"Create pipe module","goal":"Build a generated Python pipe module.","allowed_paths":["src/pipes.py"],"acceptance_checks":[{"description":"src/pipes.py exists"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	if err := m.store.UpdateTaskStatus(plan.Tasks[0].ID, coding.TaskStatusBlocked); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	payload := json.RawMessage(`{"paths":["src/pipe.py"],"allowed_paths":["src/pipes.py"],"error":"patch path \"src/pipe.py\" is outside allowed paths"}`)
	if _, err := m.store.AddTaskEvent(coding.TaskEvent{TaskID: plan.Tasks[0].ID, Type: "worker_rejected", Message: "Worker patch rejected: returned paths are outside the task scope.", Payload: payload}); err != nil {
		t.Fatalf("AddTaskEvent: %v", err)
	}
	plan, _, err = m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan refresh: %v", err)
	}
	packet, err := m.buildWorkerPacketForRun(plan.Tasks[0])
	if err != nil {
		t.Fatalf("buildWorkerPacketForRun: %v", err)
	}
	for _, want := range []string{"Path rejection repair focus", "src/pipe.py", "src/pipes.py", "exact allowed paths"} {
		if !strings.Contains(packet.Goal, want) {
			t.Fatalf("packet goal missing %q:\n%s", want, packet.Goal)
		}
	}
}

func TestBuildWorkerPacketIgnoresStalePathRejectionAfterRepairRequest(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Pipe module","summary":"Create pipe module","tasks":[{"id":"pipes","title":"Create pipe module","goal":"Build a generated Python pipe module.","allowed_paths":["src/pipes.py"],"acceptance_checks":[{"description":"src/pipes.py exists"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	if err := m.store.UpdateTaskStatus(plan.Tasks[0].ID, coding.TaskStatusBlocked); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	payload := json.RawMessage(`{"paths":["src/pipe.py"],"allowed_paths":["src/pipes.py"],"error":"patch path \"src/pipe.py\" is outside allowed paths"}`)
	if _, err := m.store.AddTaskEvent(coding.TaskEvent{TaskID: plan.Tasks[0].ID, Type: "worker_rejected", Message: "Worker patch rejected: returned paths are outside the task scope.", Payload: payload}); err != nil {
		t.Fatalf("AddTaskEvent worker_rejected: %v", err)
	}
	if _, err := m.store.AddTaskEvent(coding.TaskEvent{TaskID: plan.Tasks[0].ID, Type: "repair_requested", Message: "Fix the draw(self, screen) signature."}); err != nil {
		t.Fatalf("AddTaskEvent repair_requested: %v", err)
	}
	plan, _, err = m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan refresh: %v", err)
	}
	packet, err := m.buildWorkerPacketForRun(plan.Tasks[0])
	if err != nil {
		t.Fatalf("buildWorkerPacketForRun: %v", err)
	}
	if strings.Contains(packet.Goal, "Path rejection repair focus") {
		t.Fatalf("packet used stale path rejection instead of latest repair request:\n%s", packet.Goal)
	}
	if !strings.Contains(packet.Goal, "Fix the draw(self, screen) signature") {
		t.Fatalf("packet missing repair request:\n%s", packet.Goal)
	}
}

func TestWorkerPatchAutoCorrectsGeneratedPathMismatch(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Pipe module","summary":"Create pipe module","tasks":[{"id":"pipes","title":"Create pipe module","goal":"Build a generated Python pipe module.","allowed_paths":["src/pipes.py"],"acceptance_checks":[{"description":"src/pipes.py exists"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	if err := m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusApproved); err != nil {
		t.Fatalf("UpdatePlanStatus: %v", err)
	}
	if err := m.store.UpdateTaskStatus(plan.Tasks[0].ID, coding.TaskStatusRunning); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{
		TaskID:  plan.Tasks[0].ID,
		Summary: "Create pipe module",
		Files: []coding.WorkerFileEdit{{
			Path:    "src/pipe.py",
			Content: "class Pipe:\n    pass\n",
		}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "task reviewing" {
		t.Fatalf("status = %q, want task reviewing; view:\n%s", got.status, got.viewport.View())
	}
	if _, err := os.Stat(filepath.Join(root, "src", "pipes.py")); err != nil {
		t.Fatalf("corrected file not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "pipe.py")); !os.IsNotExist(err) {
		t.Fatalf("uncorrected file exists or stat failed unexpectedly: %v", err)
	}
	events, err := got.store.TaskEvents(plan.Tasks[0].ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type == "worker_path_corrected" {
			found = true
		}
	}
	if !found {
		t.Fatalf("worker_path_corrected event missing: %#v", events)
	}
}

func TestWorkerPatchAutoCorrectsRepairPathMismatchForExistingAllowedFile(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "pipes.py"), []byte("BROKEN = True\n"), 0o644); err != nil {
		t.Fatalf("WriteFile pipes: %v", err)
	}
	rawPlan := `{"title":"Pipe module","summary":"Create pipe module","tasks":[{"id":"pipes","title":"Create pipe module","goal":"Build a generated Python pipe module.","allowed_paths":["src/pipes.py"],"acceptance_checks":[{"description":"src/pipes.py exists"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	if err := m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusApproved); err != nil {
		t.Fatalf("UpdatePlanStatus: %v", err)
	}
	if err := m.store.UpdateTaskStatus(plan.Tasks[0].ID, coding.TaskStatusRunning); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	if _, err := m.store.AddTaskEvent(coding.TaskEvent{TaskID: plan.Tasks[0].ID, Type: "repair_requested", Message: "Fix existing generated file."}); err != nil {
		t.Fatalf("AddTaskEvent repair_requested: %v", err)
	}
	plan, _, err = m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan refresh: %v", err)
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{
		TaskID:  plan.Tasks[0].ID,
		Summary: "Repair pipe module",
		Files: []coding.WorkerFileEdit{{
			Path:    "src/pipe.py",
			Content: "class Pipe:\n    pass\n",
		}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "task reviewing" {
		t.Fatalf("status = %q, want task reviewing; view:\n%s", got.status, got.viewport.View())
	}
	content, err := os.ReadFile(filepath.Join(root, "src", "pipes.py"))
	if err != nil {
		t.Fatalf("ReadFile pipes: %v", err)
	}
	if !strings.Contains(string(content), "class Pipe") || strings.Contains(string(content), "BROKEN") {
		t.Fatalf("pipes.py not replaced with corrected repair content:\n%s", content)
	}
}

func TestWorkerPatchRejectsRepairThatMissesValidationPath(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	for _, file := range []string{"main.py", "src/pipes.py"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(file)), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, file), []byte("BROKEN = True\n"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", file, err)
		}
	}
	rawPlan := `{"title":"Entrypoint","summary":"Fix main","tasks":[{"id":"main","title":"Main entrypoint","goal":"Create a Python entrypoint.","allowed_paths":["main.py","src/pipes.py"],"acceptance_checks":[{"description":"main works"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: %v ok=%v", err, ok)
	}
	if err := m.store.UpdatePlanStatus(plan.ID, coding.PlanStatusApproved); err != nil {
		t.Fatalf("UpdatePlanStatus: %v", err)
	}
	if err := m.store.UpdateTaskStatus(plan.Tasks[0].ID, coding.TaskStatusRunning); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	validationPayload := json.RawMessage(`{"issues":[{"path":"main.py","message":"syntax error"}]}`)
	if _, err := m.store.AddTaskEvent(coding.TaskEvent{TaskID: plan.Tasks[0].ID, Type: "artifact_validation", Message: "main.py syntax error", Payload: validationPayload}); err != nil {
		t.Fatalf("AddTaskEvent artifact_validation: %v", err)
	}
	rawPatch, err := json.Marshal(coding.WorkerPatch{
		TaskID:  plan.Tasks[0].ID,
		Summary: "Touch unrelated file",
		Files: []coding.WorkerFileEdit{{
			Path:    "src/pipes.py",
			Content: "OK = True\n",
		}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	got := updated.(model)
	if got.status != "worker patch rejected" {
		t.Fatalf("status = %q, want worker patch rejected; view:\n%s", got.status, got.viewport.View())
	}
}

func TestWorkerOutputGuardKillsRepeatedOutput(t *testing.T) {
	m := commandTestModel(t)
	guard := m.workerOutputGuard(4096)
	repeated := strings.Repeat("        self.score = 0\n        self.bird = Bird(self.width // 2, self.height // 2)\n        self.pipe_manager = PipeManager()\n", 12)
	if err := guard(`{"task_id":"task","files":[{"path":"game.py","content":"` + repeated); err == nil {
		t.Fatal("workerOutputGuard returned nil error for repeated output")
	}
}
