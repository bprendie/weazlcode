package tui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/tools"
)

func TestOutputsCommandTextIncludesTaskEventsAndToolLogs(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	raw := `{"title":"Outputs","summary":"Track outputs","tasks":[{"title":"Task","goal":"Do work","allowed_paths":["README.md"]}]}`
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
	payload, _ := json.Marshal([]verificationResult{{Command: "go test ./...", Output: "ok"}})
	_, _ = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:    plan.Tasks[0].ID,
		Type:      "verification",
		Message:   "Ran 1 verification command.",
		Payload:   payload,
		CreatedAt: time.Now(),
	})
	m.logToolCall("call-1", "git_status", tools.SafetyLevelSafe, `{"cwd":"."}`, map[string]any{"cwd": "."}, "## main", 12*time.Millisecond, true)
	m.logHook(hookLogEntry{
		Time:       time.Now().Format(time.RFC3339Nano),
		SessionID:  m.session.ID,
		Event:      "task_done",
		Command:    "notify-send",
		Success:    true,
		DurationMS: 4,
		Output:     "sent",
	})
	text := m.outputsCommandText()
	for _, want := range []string{"Outputs:", "Task: verification", "go test ./...", "git_status", "## main", "task_done: notify-send", "sent"} {
		if !strings.Contains(text, want) {
			t.Fatalf("outputs missing %q:\n%s", want, text)
		}
	}
}

func TestOutputsCommandTextEmpty(t *testing.T) {
	m := commandTestModel(t)
	m.project.LogDir = filepath.Join(t.TempDir(), "logs")
	got := m.outputsCommandText()
	if got != "Outputs:\nNo task events or tool outputs yet." {
		t.Fatalf("outputs empty = %q", got)
	}
}
