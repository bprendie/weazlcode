package llm

import (
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

func (c Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
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
		if c.provider.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.provider.APIKey)
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
	}
	return u
}
