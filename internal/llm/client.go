package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/storage"
)

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type OutputGuard func(content string) error

type StreamEvent struct {
	Type      string     // "content", "tool_call", "done"
	Content   string     // Text content chunk
	ToolCalls []ToolCall // Tool calls from assistant
	Usage     Usage      // Token usage (only on done)
	Error     error      // Error if any
}

const markdownResponseSystemPrompt = "Format normal responses as Markdown so headings, lists, code blocks, quotes, links, and tables render cleanly in the terminal. If the user explicitly requests a different raw format such as JSON, Python, SQL, CSV, or plain text, honor that requested format exactly."

type Client struct {
	provider config.Provider
	http     *http.Client
	tools    []map[string]any
}

const defaultHTTPTimeout = 5 * time.Minute

func New(provider config.Provider) Client {
	return NewWithTimeout(provider, defaultHTTPTimeout)
}

func NewWithTimeout(provider config.Provider, timeout time.Duration) Client {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return Client{
		provider: provider,
		http:     &http.Client{Timeout: timeout},
	}
}

// WithTools adds tool definitions to the client
func (c Client) WithTools(tools []map[string]any) Client {
	c.tools = tools
	return c
}

func (c Client) Stream(ctx context.Context, history []storage.Message, prompt string, onEvent func(StreamEvent)) (Usage, error) {
	switch strings.ToLower(c.provider.Type) {
	case "vllm":
		messages := chatMessages(history, prompt)
		return c.streamOpenAICompat(ctx, messages, onEvent)
	case "anthropic":
		messages := chatMessages(history, prompt)
		content, usage, err := c.CompleteWithUsage(ctx, messages, 2048)
		if err != nil {
			return usage, err
		}
		if strings.TrimSpace(content) != "" {
			onEvent(StreamEvent{Type: "content", Content: content})
		}
		return usage, nil
	case "ollama":
		messages := ollamaChatMessages(history, prompt)
		return c.streamOllama(ctx, messages, onEvent)
	default:
		return Usage{}, fmt.Errorf("unsupported provider type %q", c.provider.Type)
	}
}

func (c Client) Complete(ctx context.Context, messages []ChatMessage, maxTokens int) (string, error) {
	content, _, err := c.CompleteWithUsage(ctx, messages, maxTokens)
	return content, err
}

func (c Client) CompleteWithUsage(ctx context.Context, messages []ChatMessage, maxTokens int) (string, Usage, error) {
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	switch strings.ToLower(c.provider.Type) {
	case "vllm":
		return c.completeOpenAICompat(ctx, messages, maxTokens)
	case "anthropic":
		return c.completeAnthropic(ctx, messages, maxTokens)
	case "ollama":
		return c.completeOllama(ctx, messages, maxTokens)
	default:
		return "", Usage{}, fmt.Errorf("unsupported provider type %q", c.provider.Type)
	}
}

func (c Client) CompleteWithUsageGuard(ctx context.Context, messages []ChatMessage, maxTokens int, guard OutputGuard) (string, Usage, error) {
	if guard == nil {
		return c.CompleteWithUsage(ctx, messages, maxTokens)
	}
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	switch strings.ToLower(c.provider.Type) {
	case "vllm":
		return c.completeOpenAICompatStreamGuard(ctx, messages, maxTokens, guard)
	case "ollama":
		return c.completeOllamaStreamGuard(ctx, messages, maxTokens, guard)
	default:
		return c.CompleteWithUsage(ctx, messages, maxTokens)
	}
}

func (c Client) Summarize(ctx context.Context, transcript string, targetTokens int) (string, error) {
	if targetTokens <= 0 {
		targetTokens = 500
	}
	messages := []ChatMessage{
		{
			Role:    "system",
			Content: "You summarize conversation history for future context. Preserve user goals, decisions, durable facts, tool results, file paths, commands, unresolved tasks, and important constraints. Drop routine back-and-forth, duplicated text, and stale details. Be concise and do not invent details.",
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Create a compact checkpoint summary of this conversation in about %d tokens. This summary will replace the earlier messages in context. Use short sections for current objective, decisions/constraints, important files or tool results, and open next steps when applicable.\n\n%s", targetTokens, transcript),
		},
	}
	return c.Complete(ctx, messages, targetTokens+200)
}

func chatMessages(history []storage.Message, prompt string) []ChatMessage {
	messages := make([]ChatMessage, 0, len(history)+2)
	messages = append(messages, ChatMessage{Role: "system", Content: systemPrompt()})
	for _, msg := range history {
		cm := ChatMessage{Role: msg.Role, Content: msg.Content}
		// Parse tool calls if present in metadata
		if msg.Role == "assistant" && msg.ToolCalls != "" {
			var toolCalls []ToolCall
			if err := json.Unmarshal([]byte(msg.ToolCalls), &toolCalls); err == nil {
				cm.ToolCalls = toolCalls
				cm.Content = "" // Content is empty when there are tool calls
			}
		}
		// Add tool results
		if msg.Role == "tool" {
			cm.ToolCallID = msg.ToolCallID
		}
		messages = append(messages, cm)
	}
	if strings.TrimSpace(prompt) != "" {
		messages = append(messages, ChatMessage{Role: "user", Content: prompt})
	}
	return messages
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}

type ollamaToolCall struct {
	Type     string `json:"type,omitempty"`
	Function struct {
		Index     int            `json:"index,omitempty"`
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

func ollamaChatMessages(history []storage.Message, prompt string) []ollamaMessage {
	messages := make([]ollamaMessage, 0, len(history)+2)
	messages = append(messages, ollamaMessage{Role: "system", Content: systemPrompt()})
	toolNames := make(map[string]string)
	for _, msg := range history {
		cm := ollamaMessage{Role: msg.Role, Content: msg.Content}
		if msg.Role == "assistant" && msg.ToolCalls != "" {
			var toolCalls []ToolCall
			if err := json.Unmarshal([]byte(msg.ToolCalls), &toolCalls); err == nil {
				cm.Content = ""
				cm.ToolCalls = make([]ollamaToolCall, 0, len(toolCalls))
				for i, call := range toolCalls {
					toolNames[call.ID] = call.Function.Name
					var args map[string]any
					if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
						args = map[string]any{}
					}
					var tc ollamaToolCall
					tc.Type = "function"
					tc.Function.Index = i
					tc.Function.Name = call.Function.Name
					tc.Function.Arguments = args
					cm.ToolCalls = append(cm.ToolCalls, tc)
				}
			}
		}
		if msg.Role == "tool" {
			cm.ToolName = toolNames[msg.ToolCallID]
			if cm.ToolName == "" {
				cm.ToolName = msg.ToolCallID
			}
		}
		messages = append(messages, cm)
	}
	if strings.TrimSpace(prompt) != "" {
		messages = append(messages, ollamaMessage{Role: "user", Content: prompt})
	}
	return messages
}

func ollamaChatMessagesFromChat(messages []ChatMessage) []ollamaMessage {
	out := make([]ollamaMessage, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		role := message.Role
		if role == "" {
			role = "user"
		}
		out = append(out, ollamaMessage{Role: role, Content: content})
	}
	return out
}

func systemPrompt() string {
	return markdownResponseSystemPrompt + "\n\n" + currentDateSystemPrompt()
}

func currentDateSystemPrompt() string {
	now := time.Now()
	location := time.Local.String()
	if location == "" {
		location = "local"
	}
	return fmt.Sprintf("Current local date/time for this request: %s (%s). Treat words like today, tomorrow, yesterday, latest, and current relative to this timestamp unless tool output says otherwise.", now.Format(time.RFC1123Z), location)
}

func (c Client) streamOpenAICompat(ctx context.Context, messages []ChatMessage, onEvent func(StreamEvent)) (Usage, error) {
	reqBody := map[string]any{
		"model":       c.provider.Model,
		"messages":    messages,
		"temperature": 0.7,
		"stream":      true,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}

	// Add tools if available
	if len(c.tools) > 0 {
		reqBody["tools"] = c.tools
		reqBody["tool_choice"] = "auto"
	}

	resp, err := c.post(ctx, "/v1/chat/completions", reqBody)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var usage Usage
	toolCallsMap := make(map[int]*ToolCall) // Track tool calls by index

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return usage, err
		}
		if chunk.Error != nil {
			return usage, errors.New(chunk.Error.Message)
		}
		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			// Handle content
			if choice.Delta.Content != "" {
				onEvent(StreamEvent{Type: "content", Content: choice.Delta.Content})
			}

			// Handle tool calls
			for _, tc := range choice.Delta.ToolCalls {
				if _, exists := toolCallsMap[tc.Index]; !exists {
					toolCallsMap[tc.Index] = &ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
					}
				}
				call := toolCallsMap[tc.Index]
				if tc.Function.Name != "" {
					call.Function.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					call.Function.Arguments += tc.Function.Arguments
				}
			}

			// Send tool calls when finish_reason is tool_calls
			if choice.FinishReason == "tool_calls" && len(toolCallsMap) > 0 {
				toolCalls := make([]ToolCall, 0, len(toolCallsMap))
				for i := 0; i < len(toolCallsMap); i++ {
					if call, ok := toolCallsMap[i]; ok {
						toolCalls = append(toolCalls, *call)
					}
				}
				onEvent(StreamEvent{Type: "tool_call", ToolCalls: toolCalls})
			}
		}
	}
	return usage, scanner.Err()
}

func (c Client) streamOllama(ctx context.Context, messages []ollamaMessage, onEvent func(StreamEvent)) (Usage, error) {
	reqBody := map[string]any{
		"model":    c.provider.Model,
		"messages": messages,
		"stream":   true,
	}

	// Add tools if available
	if len(c.tools) > 0 {
		reqBody["tools"] = c.tools
	}

	resp, err := c.post(ctx, "/api/chat", reqBody)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	var usage Usage

	for {
		var chunk struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string                 `json:"name"`
						Arguments map[string]interface{} `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			Done            bool   `json:"done"`
			PromptEvalCount int    `json:"prompt_eval_count"`
			EvalCount       int    `json:"eval_count"`
			Error           string `json:"error"`
		}
		if err := dec.Decode(&chunk); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return usage, err
		}
		if chunk.Error != "" {
			return usage, errors.New(chunk.Error)
		}

		// Handle content
		if chunk.Message.Content != "" {
			onEvent(StreamEvent{Type: "content", Content: chunk.Message.Content})
		}

		// Handle tool calls
		if len(chunk.Message.ToolCalls) > 0 {
			toolCalls := make([]ToolCall, len(chunk.Message.ToolCalls))
			for i, tc := range chunk.Message.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Function.Arguments)
				toolCalls[i] = ToolCall{
					ID:   fmt.Sprintf("call_%d", i),
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Function.Name,
						Arguments: string(argsJSON),
					},
				}
			}
			onEvent(StreamEvent{Type: "tool_call", ToolCalls: toolCalls})
		}

		if chunk.Done {
			usage.InputTokens = chunk.PromptEvalCount
			usage.OutputTokens = chunk.EvalCount
			break
		}
	}
	return usage, nil
}
