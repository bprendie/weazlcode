package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bprendie/weazlcode/internal/llm"
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

func TestExecuteToolsShowsApprovalModalWhenAutoExecuteDisabled(t *testing.T) {
	m := commandTestModel(t)
	if err := m.store.CreateVault("pw"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	m.cfg.Tools.Enabled = true
	m.cfg.Tools.AutoExecute = false
	m.toolRegistry.Register(promptTool{name: "write_file"})
	m.streamText = "I need to edit a file."
	m.pendingTools = []llm.ToolCall{toolCall("call-1", "write_file", `{"path":"README.md"}`)}
	if calls := m.toolApprovalCalls(); len(calls) != 1 {
		t.Fatalf("approval calls = %#v", calls)
	}

	updated, _ := m.executeTools(10, 5)
	got := updated.(model)
	if got.err != "" {
		t.Fatalf("executeTools error: %s", got.err)
	}
	if got.mode != modeToolApproval {
		t.Fatalf("mode = %v, want modeToolApproval", got.mode)
	}
	if got.status != "approve tool calls" {
		t.Fatalf("status = %q, want approve tool calls", got.status)
	}
	if got.pendingToolInput != 10 || got.pendingToolOutput != 5 {
		t.Fatalf("pending tokens = %d/%d", got.pendingToolInput, got.pendingToolOutput)
	}
	if view := got.toolApprovalView(); !strings.Contains(view, "write_file") || !strings.Contains(view, "README.md") {
		t.Fatalf("approval view = %q", view)
	}
}

func TestRejectToolCallsRecordsToolResult(t *testing.T) {
	m := commandTestModel(t)
	if err := m.store.CreateVault("pw"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	m.cfg.Tools.Enabled = true
	m.cfg.Tools.AutoExecute = false
	m.toolRegistry.Register(promptTool{name: "write_file"})
	m.streamText = "I need to edit a file."
	m.pendingTools = []llm.ToolCall{toolCall("call-1", "write_file", `{"path":"README.md"}`)}
	updated, _ := m.executeTools(10, 5)
	m = updated.(model)

	updated, _ = m.rejectToolCalls()
	got := updated.(model)
	if got.mode != modeChat || !got.thinking {
		t.Fatalf("mode/thinking = %v/%t", got.mode, got.thinking)
	}
	messages, err := got.store.Messages(got.session.ID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) < 2 || messages[len(messages)-1].Role != "tool" || !strings.Contains(messages[len(messages)-1].Content, "rejected by user") {
		t.Fatalf("messages = %#v", messages)
	}
}

type promptTool struct {
	name string
}

func (t promptTool) Name() string { return t.name }
func (t promptTool) Description() string {
	return "Test prompt tool"
}
func (t promptTool) Parameters() []tools.Parameter { return nil }
func (t promptTool) SafetyLevel() tools.SafetyLevel {
	return tools.SafetyLevelPrompt
}
func (t promptTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	return "executed", nil
}

func toolCall(id, name, args string) llm.ToolCall {
	var call llm.ToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}
