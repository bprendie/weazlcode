package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bprendie/weazlcode/internal/config"
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

func TestCompleteWithUsageOpenAICompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["stream"] != false {
			t.Fatalf("stream = %#v, want false", req["stream"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" done "}}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`))
	}))
	defer server.Close()

	client := New(config.Provider{Type: "vllm", Model: "test-model", ServerURL: server.URL})
	content, usage, err := client.CompleteWithUsage(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}}, 64)
	if err != nil {
		t.Fatalf("CompleteWithUsage returned error: %v", err)
	}
	if content != "done" {
		t.Fatalf("content = %q, want done", content)
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 5 {
		t.Fatalf("usage = %#v, want 12/5", usage)
	}
}

func TestCompleteWithUsageOllama(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path = %q, want /api/chat", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["stream"] != false {
			t.Fatalf("stream = %#v, want false", req["stream"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":" ok "},"prompt_eval_count":21,"eval_count":8}`))
	}))
	defer server.Close()

	client := New(config.Provider{Type: "ollama", Model: "test-model", ServerURL: server.URL})
	content, usage, err := client.CompleteWithUsage(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}}, 64)
	if err != nil {
		t.Fatalf("CompleteWithUsage returned error: %v", err)
	}
	if content != "ok" {
		t.Fatalf("content = %q, want ok", content)
	}
	if usage.InputTokens != 21 || usage.OutputTokens != 8 {
		t.Fatalf("usage = %#v, want 21/8", usage)
	}
}

func TestCompleteWithUsageAnthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatal("anthropic-version header missing")
		}
		if got := r.Header.Get("authorization"); got != "" {
			t.Fatalf("authorization header = %q, want empty", got)
		}
		var req struct {
			Model     string              `json:"model"`
			System    string              `json:"system"`
			Messages  []map[string]string `json:"messages"`
			MaxTokens int                 `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.System == "" || len(req.Messages) != 1 || req.Messages[0]["role"] != "user" {
			t.Fatalf("request = %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":" done "}],"usage":{"input_tokens":31,"output_tokens":7}}`))
	}))
	defer server.Close()

	client := New(config.Provider{Type: "anthropic", Model: "claude-test", ServerURL: server.URL, APIKey: "test-key"})
	content, usage, err := client.CompleteWithUsage(context.Background(), []ChatMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "hello"}}, 64)
	if err != nil {
		t.Fatalf("CompleteWithUsage returned error: %v", err)
	}
	if content != "done" {
		t.Fatalf("content = %q, want done", content)
	}
	if usage.InputTokens != 31 || usage.OutputTokens != 7 {
		t.Fatalf("usage = %#v, want 31/7", usage)
	}
}

func TestCompleteWithUsageRetriesTransientHTTPFailures(t *testing.T) {
	oldDelay := postRetryDelay
	postRetryDelay = func(int) time.Duration { return 0 }
	defer func() { postRetryDelay = oldDelay }()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&attempts, 1)
		if attempt < 3 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`))
	}))
	defer server.Close()

	client := New(config.Provider{Type: "vllm", Model: "test-model", ServerURL: server.URL})
	content, usage, err := client.CompleteWithUsage(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}}, 64)
	if err != nil {
		t.Fatalf("CompleteWithUsage returned error: %v", err)
	}
	if content != "recovered" {
		t.Fatalf("content = %q, want recovered", content)
	}
	if usage.InputTokens != 3 || usage.OutputTokens != 2 {
		t.Fatalf("usage = %#v, want 3/2", usage)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestCompleteWithUsageDoesNotRetryNonTransientHTTPFailures(t *testing.T) {
	oldDelay := postRetryDelay
	postRetryDelay = func(int) time.Duration { return 0 }
	defer func() { postRetryDelay = oldDelay }()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(config.Provider{Type: "vllm", Model: "test-model", ServerURL: server.URL})
	_, _, err := client.CompleteWithUsage(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}}, 64)
	if err == nil {
		t.Fatal("CompleteWithUsage returned nil error, want HTTP error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestCompleteWithUsageRuntimeSmoke(t *testing.T) {
	serverURL := os.Getenv("WEAZLCODE_LLM_SMOKE_URL")
	model := os.Getenv("WEAZLCODE_LLM_SMOKE_MODEL")
	if serverURL == "" || model == "" {
		t.Skip("set WEAZLCODE_LLM_SMOKE_URL and WEAZLCODE_LLM_SMOKE_MODEL to run runtime LLM smoke")
	}
	providerType := os.Getenv("WEAZLCODE_LLM_SMOKE_PROVIDER")
	if providerType == "" {
		providerType = "vllm"
	}

	client := New(config.Provider{Type: providerType, Model: model, ServerURL: serverURL})
	content, usage, err := client.CompleteWithUsage(context.Background(), []ChatMessage{{Role: "user", Content: `Return exactly {"ok":true}`}}, 64)
	if err != nil {
		t.Fatalf("CompleteWithUsage runtime smoke failed: %T: %[1]v", err)
	}
	if !strings.Contains(content, `"ok"`) {
		t.Fatalf("content = %q, want JSON containing ok", content)
	}
	t.Logf("runtime smoke content=%q usage=%+v", content, usage)
}
