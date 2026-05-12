package llm

import (
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/storage"
)

func TestChatMessagesDoesNotAppendEmptyPrompt(t *testing.T) {
	history := []storage.Message{
		{Role: "assistant", ToolCalls: `[{"id":"call_1","type":"function","function":{"name":"calculate","arguments":"{\"operation\":\"add\",\"a\":1,\"b\":2}"}}]`},
		{Role: "tool", Content: "1 + 2 = 3", ToolCallID: "call_1"},
	}

	messages := chatMessages(history, "")

	if len(messages) != 3 {
		t.Fatalf("message count = %d, want 3: %#v", len(messages), messages)
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, markdownResponseSystemPrompt) {
		t.Fatalf("system markdown prompt missing: %#v", messages[0])
	}
	if !strings.Contains(messages[0].Content, "Current local date/time") {
		t.Fatalf("current date system prompt missing: %#v", messages[0])
	}
	if messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool calls were not preserved: %#v", messages[1])
	}
	if messages[1].Content != "" {
		t.Fatalf("assistant tool-call content = %q, want empty", messages[1].Content)
	}
	if messages[2].Role != "tool" || messages[2].ToolCallID != "call_1" {
		t.Fatalf("tool result metadata was not preserved: %#v", messages[2])
	}
}

func TestChatMessagesAppendsNonEmptyPrompt(t *testing.T) {
	messages := chatMessages(nil, "hello")

	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if messages[0].Role != "system" {
		t.Fatalf("first message = %#v, want system", messages[0])
	}
	if !strings.Contains(messages[0].Content, "Current local date/time") {
		t.Fatalf("current date system prompt missing: %#v", messages[0])
	}
	if messages[1].Role != "user" || messages[1].Content != "hello" {
		t.Fatalf("message = %#v, want user hello", messages[1])
	}
}

func TestOllamaChatMessagesUseToolNameAndObjectArguments(t *testing.T) {
	history := []storage.Message{
		{Role: "assistant", ToolCalls: `[{"id":"call_1","type":"function","function":{"name":"calculate","arguments":"{\"operation\":\"add\",\"a\":1,\"b\":2}"}}]`},
		{Role: "tool", Content: "1 + 2 = 3", ToolCallID: "call_1"},
	}

	messages := ollamaChatMessages(history, "")

	if len(messages) != 3 {
		t.Fatalf("message count = %d, want 3: %#v", len(messages), messages)
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, markdownResponseSystemPrompt) {
		t.Fatalf("system markdown prompt missing: %#v", messages[0])
	}
	if !strings.Contains(messages[0].Content, "Current local date/time") {
		t.Fatalf("current date system prompt missing: %#v", messages[0])
	}
	if messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool calls were not converted: %#v", messages[1])
	}
	call := messages[1].ToolCalls[0]
	if call.Function.Name != "calculate" {
		t.Fatalf("tool name = %q, want calculate", call.Function.Name)
	}
	if call.Function.Arguments["operation"] != "add" || call.Function.Arguments["a"] != float64(1) {
		t.Fatalf("tool arguments were not decoded as an object: %#v", call.Function.Arguments)
	}
	if messages[2].Role != "tool" || messages[2].ToolName != "calculate" || messages[2].Content != "1 + 2 = 3" {
		t.Fatalf("tool result was not converted for Ollama: %#v", messages[2])
	}
}
