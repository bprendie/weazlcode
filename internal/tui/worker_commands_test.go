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
