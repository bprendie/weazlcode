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

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
func NewOpenAIProvider(baseURL, apiKey, model string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAIProvider{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	reqBody := p.buildRequest(req, false)
	
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &ProviderError{Provider: p.Name(), Message: "marshal request", Err: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, &ProviderError{Provider: p.Name(), Message: "create request", Err: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

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
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &ProviderError{Provider: p.Name(), Message: "decode response", Err: err}
	}

	if len(result.Choices) == 0 {
		return nil, &ProviderError{Provider: p.Name(), Message: "no choices in response"}
	}

	return &CompletionResponse{
		Content:      result.Choices[0].Message.Content,
		Model:        result.Model,
		StopReason:   result.Choices[0].FinishReason,
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
		FinishedAt:   time.Now(),
	}, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, req CompletionRequest, handler StreamHandler) error {
	reqBody := p.buildRequest(req, true)
	
	body, err := json.Marshal(reqBody)
	if err != nil {
		return &ProviderError{Provider: p.Name(), Message: "marshal request", Err: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return &ProviderError{Provider: p.Name(), Message: "create request", Err: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

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

		if event.Data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			if err := handler(chunk.Choices[0].Delta.Content); err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *OpenAIProvider) buildRequest(req CompletionRequest, stream bool) map[string]interface{} {
	messages := make([]map[string]string, 0, len(req.Messages)+1)
	
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}
	
	for _, msg := range req.Messages {
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

	if req.Temperature > 0 {
		result["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		result["max_tokens"] = req.MaxTokens
	}
	if len(req.StopWords) > 0 {
		result["stop"] = req.StopWords
	}

	return result
}
