package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicProvider implements the Provider interface for Anthropic's Claude API.
type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	reqBody := p.buildRequest(req, false)

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &ProviderError{Provider: p.Name(), Message: "marshal request", Err: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, &ProviderError{Provider: p.Name(), Message: "create request", Err: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, &ProviderError{Provider: p.Name(), Message: "send request", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &ProviderError{
			Provider: p.Name(),
			Message:  fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(bodyBytes)),
			Code:     fmt.Sprintf("%d", resp.StatusCode),
		}
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &ProviderError{Provider: p.Name(), Message: "decode response", Err: err}
	}

	content := ""
	for _, block := range result.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &CompletionResponse{
		Content:      content,
		Model:        result.Model,
		StopReason:   result.StopReason,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		FinishedAt:   time.Now(),
	}, nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, req CompletionRequest, handler StreamHandler) error {
	reqBody := p.buildRequest(req, true)

	body, err := json.Marshal(reqBody)
	if err != nil {
		return &ProviderError{Provider: p.Name(), Message: "marshal request", Err: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return &ProviderError{Provider: p.Name(), Message: "create request", Err: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return &ProviderError{Provider: p.Name(), Message: "send request", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &ProviderError{
			Provider: p.Name(),
			Message:  fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(bodyBytes)),
			Code:     fmt.Sprintf("%d", resp.StatusCode),
		}
	}

	reader := NewSSEReader(resp.Body)
	for {
		event, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return &ProviderError{Provider: p.Name(), Message: "read stream", Err: err}
		}

		if event.Event == "message_stop" {
			break
		}

		if event.Event != "content_block_delta" {
			continue
		}

		var delta struct {
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}

		if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
			continue
		}

		if delta.Delta.Type == "text_delta" && delta.Delta.Text != "" {
			if err := handler(delta.Delta.Text); err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *AnthropicProvider) buildRequest(req CompletionRequest, stream bool) map[string]interface{} {
	messages := make([]map[string]string, 0, len(req.Messages))

	for _, msg := range req.Messages {
		// Anthropic doesn't support system role in messages array
		if msg.Role == "system" {
			continue
		}
		messages = append(messages, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	model := req.Model
	if model == "" {
		model = p.model
	}

	result := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}

	// Add system prompt if provided
	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		// Check if first message is system role
		for _, msg := range req.Messages {
			if msg.Role == "system" {
				systemPrompt = msg.Content
				break
			}
		}
	}
	if systemPrompt != "" {
		result["system"] = systemPrompt
	}

	if req.Temperature > 0 {
		result["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		result["max_tokens"] = req.MaxTokens
	}
	if len(req.StopWords) > 0 {
		result["stop_sequences"] = req.StopWords
	}

	return result
}

// NormalizeModelName converts common model names to Anthropic's format.
func (p *AnthropicProvider) NormalizeModelName(name string) string {
	raw := strings.TrimSpace(name)
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "claude-") {
		return raw
	}

	// Map common names to Anthropic model IDs
	switch {
	case strings.Contains(lower, "opus"):
		return "claude-3-opus-20240229"
	case strings.Contains(lower, "sonnet"):
		return "claude-3-5-sonnet-20241022"
	case strings.Contains(lower, "haiku"):
		return "claude-haiku-4-5-20251001"
	default:
		return raw
	}
}
