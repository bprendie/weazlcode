package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/config"
)

var postRetryDelay = func(attempt int) time.Duration {
	return time.Duration(attempt) * 250 * time.Millisecond
}

func (c Client) completeOpenAICompat(ctx context.Context, messages []ChatMessage, maxTokens int) (string, Usage, error) {
	reqBody := map[string]any{
		"model":       c.provider.Model,
		"messages":    messages,
		"temperature": 0.2,
		"stream":      false,
		"max_tokens":  maxTokens,
	}
	resp, err := c.post(ctx, "/v1/chat/completions", reqBody)
	if err != nil {
		return "", Usage{}, err
	}
	defer resp.Body.Close()
	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", Usage{}, err
	}
	if body.Error != nil {
		return "", Usage{}, errors.New(body.Error.Message)
	}
	if len(body.Choices) == 0 {
		return "", Usage{}, errors.New("empty completion response")
	}
	var usage Usage
	if body.Usage != nil {
		usage.InputTokens = body.Usage.PromptTokens
		usage.OutputTokens = body.Usage.CompletionTokens
	}
	return strings.TrimSpace(body.Choices[0].Message.Content), usage, nil
}

func (c Client) completeOpenAICompatStreamGuard(ctx context.Context, messages []ChatMessage, maxTokens int, guard OutputGuard) (string, Usage, error) {
	reqBody := map[string]any{
		"model":       c.provider.Model,
		"messages":    messages,
		"temperature": 0.2,
		"stream":      true,
		"max_tokens":  maxTokens,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}
	resp, err := c.post(ctx, "/v1/chat/completions", reqBody)
	if err != nil {
		return "", Usage{}, err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var usage Usage
	var content strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
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
			return strings.TrimSpace(content.String()), usage, err
		}
		if chunk.Error != nil {
			return strings.TrimSpace(content.String()), usage, errors.New(chunk.Error.Message)
		}
		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			content.WriteString(choice.Delta.Content)
			if err := guard(content.String()); err != nil {
				return strings.TrimSpace(content.String()), usage, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return strings.TrimSpace(content.String()), usage, err
	}
	if strings.TrimSpace(content.String()) == "" {
		return "", usage, errors.New("empty completion response")
	}
	return strings.TrimSpace(content.String()), usage, nil
}

func (c Client) completeOllama(ctx context.Context, messages []ChatMessage, maxTokens int) (string, Usage, error) {
	reqBody := map[string]any{
		"model":    c.provider.Model,
		"messages": messages,
		"stream":   false,
		"options": map[string]any{
			"num_predict": maxTokens,
			"temperature": 0.2,
		},
	}
	resp, err := c.post(ctx, "/api/chat", reqBody)
	if err != nil {
		return "", Usage{}, err
	}
	defer resp.Body.Close()
	var body struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int    `json:"prompt_eval_count"`
		EvalCount       int    `json:"eval_count"`
		Error           string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", Usage{}, err
	}
	if body.Error != "" {
		return "", Usage{}, errors.New(body.Error)
	}
	usage := Usage{
		InputTokens:  body.PromptEvalCount,
		OutputTokens: body.EvalCount,
	}
	return strings.TrimSpace(body.Message.Content), usage, nil
}

func (c Client) completeOllamaStreamGuard(ctx context.Context, messages []ChatMessage, maxTokens int, guard OutputGuard) (string, Usage, error) {
	reqBody := map[string]any{
		"model":    c.provider.Model,
		"messages": ollamaChatMessagesFromChat(messages),
		"stream":   true,
		"options": map[string]any{
			"num_predict": maxTokens,
			"temperature": 0.2,
		},
	}
	resp, err := c.post(ctx, "/api/chat", reqBody)
	if err != nil {
		return "", Usage{}, err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	var content strings.Builder
	var usage Usage
	for {
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done            bool   `json:"done"`
			PromptEvalCount int    `json:"prompt_eval_count"`
			EvalCount       int    `json:"eval_count"`
			Error           string `json:"error"`
		}
		if err := dec.Decode(&chunk); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return strings.TrimSpace(content.String()), usage, err
		}
		if chunk.Error != "" {
			return strings.TrimSpace(content.String()), usage, errors.New(chunk.Error)
		}
		if chunk.Message.Content != "" {
			content.WriteString(chunk.Message.Content)
			if err := guard(content.String()); err != nil {
				return strings.TrimSpace(content.String()), usage, err
			}
		}
		if chunk.Done {
			usage.InputTokens = chunk.PromptEvalCount
			usage.OutputTokens = chunk.EvalCount
			break
		}
	}
	if strings.TrimSpace(content.String()) == "" {
		return "", usage, errors.New("empty completion response")
	}
	return strings.TrimSpace(content.String()), usage, nil
}

func (c Client) completeAnthropic(ctx context.Context, messages []ChatMessage, maxTokens int) (string, Usage, error) {
	system, chat := anthropicMessages(messages)
	reqBody := map[string]any{
		"model":       c.provider.Model,
		"messages":    chat,
		"temperature": 0.2,
		"max_tokens":  maxTokens,
	}
	if system != "" {
		reqBody["system"] = system
	}
	resp, err := c.postAnthropic(ctx, "/v1/messages", reqBody)
	if err != nil {
		return "", Usage{}, err
	}
	defer resp.Body.Close()
	var body struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", Usage{}, err
	}
	if body.Error != nil {
		return "", Usage{}, errors.New(body.Error.Message)
	}
	var content strings.Builder
	for _, block := range body.Content {
		if block.Type == "text" && block.Text != "" {
			content.WriteString(block.Text)
		}
	}
	if strings.TrimSpace(content.String()) == "" {
		return "", Usage{}, errors.New("empty completion response")
	}
	var usage Usage
	if body.Usage != nil {
		usage.InputTokens = body.Usage.InputTokens
		usage.OutputTokens = body.Usage.OutputTokens
	}
	return strings.TrimSpace(content.String()), usage, nil
}

func anthropicMessages(messages []ChatMessage) (string, []map[string]string) {
	var system []string
	chat := make([]map[string]string, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		switch message.Role {
		case "system":
			system = append(system, content)
		case "assistant":
			chat = append(chat, map[string]string{"role": "assistant", "content": content})
		default:
			chat = append(chat, map[string]string{"role": "user", "content": content})
		}
	}
	return strings.Join(system, "\n\n"), chat
}

func (c Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.postJSON(ctx, path, body, nil)
}

func (c Client) postAnthropic(ctx context.Context, path string, body any) (*http.Response, error) {
	headers := map[string]string{
		"anthropic-version": "2023-06-01",
	}
	if c.provider.APIKey != "" {
		headers["x-api-key"] = c.provider.APIKey
	}
	return c.postJSON(ctx, path, body, headers)
}

func (c Client) postJSON(ctx context.Context, path string, body any, headers map[string]string) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := baseURL(c.provider) + path

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.provider.APIKey != "" && strings.ToLower(c.provider.Type) != "anthropic" {
			req.Header.Set("Authorization", "Bearer "+c.provider.APIKey)
		}
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxAttempts && ctx.Err() == nil {
				if sleepErr := sleepBeforeRetry(ctx, attempt); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			return resp, nil
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		detail := strings.TrimSpace(string(body))
		lastErr = formatHTTPError(url, resp.Status, detail)
		if attempt < maxAttempts && retryableHTTPStatus(resp.StatusCode) && ctx.Err() == nil {
			if sleepErr := sleepBeforeRetry(ctx, attempt); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
		return nil, lastErr
	}
	return nil, lastErr
}

func formatHTTPError(url, status, detail string) error {
	if detail == "" {
		return fmt.Errorf("%s returned %s", url, status)
	}
	return fmt.Errorf("%s returned %s: %s", url, status, detail)
}

func retryableHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

func sleepBeforeRetry(ctx context.Context, attempt int) error {
	delay := postRetryDelay(attempt)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func baseURL(provider config.Provider) string {
	u := strings.TrimRight(strings.TrimSpace(provider.ServerURL), "/")
	switch strings.ToLower(provider.Type) {
	case "vllm":
		u = strings.TrimSuffix(u, "/v1")
	case "ollama":
		u = strings.TrimSuffix(u, "/api")
	case "anthropic":
		u = strings.TrimSuffix(u, "/v1")
	}
	return u
}
