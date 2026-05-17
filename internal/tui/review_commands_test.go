package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/coding"
)

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
	m, root := commandTestModelWithReviewingTask(t)
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
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "old\n" {
		t.Fatalf("README was not restored after needs-fix: %q", data)
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
	if len(events) != 8 || events[6].Type != "output_cleanup" || events[7].Type != "repair_requested" {
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
	if len(events) != 10 || events[8].Type != "task_baseline" || events[9].Type != "repair_start" {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(string(events[9].Payload), "Repair focus") || !strings.Contains(string(events[9].Payload), "Use the requested wording only") {
		t.Fatalf("repair payload = %s", events[9].Payload)
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
