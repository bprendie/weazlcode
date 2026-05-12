package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/storage"
	"github.com/bprendie/weazlcode/internal/tools"
)

func TestLogToolCallWritesJSONL(t *testing.T) {
	root := t.TempDir()
	m := model{
		project: project.Summary{
			Root:   root,
			LogDir: filepath.Join(root, ".weazlcode", "logs"),
		},
		session: storage.Session{ID: "session-1"},
	}

	m.logToolCall("call-1", "git_status", tools.SafetyLevelSafe, `{"cwd":"."}`, map[string]any{"cwd": "."}, "ok", 12*time.Millisecond, true)

	path := filepath.Join(root, ".weazlcode", "logs", "tool_calls.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got struct {
		SessionID string `json:"session_id"`
		CallID    string `json:"call_id"`
		Tool      string `json:"tool"`
		Safety    string `json:"safety"`
		Success   bool   `json:"success"`
	}
	if err := json.Unmarshal(data[:len(data)-1], &got); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, data)
	}
	if got.SessionID != "session-1" || got.CallID != "call-1" || got.Tool != "git_status" || got.Safety != "safe" || !got.Success {
		t.Fatalf("log entry = %#v", got)
	}
}
