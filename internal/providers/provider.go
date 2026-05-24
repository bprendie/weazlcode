package providers

import (
	"context"
	"time"
)

// Provider defines the interface for LLM providers.
type Provider interface {
	// Name returns the provider name (e.g., "anthropic", "openai", "vllm", "ollama")
	Name() string
	
	// Complete sends a completion request and returns the response.
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	
	// Stream sends a completion request and streams the response.
	Stream(ctx context.Context, req CompletionRequest, handler StreamHandler) error
}

// CompletionRequest represents a request to generate a completion.
type CompletionRequest struct {
	Messages    []Message `json:"messages"`
	Model       string    `json:"model"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	StopWords   []string  `json:"stop,omitempty"`
	SystemPrompt string   `json:"system,omitempty"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string `json:"role"`    // system, user, assistant
	Content string `json:"content"`
}

// CompletionResponse represents the response from a completion request.
type CompletionResponse struct {
	Content      string    `json:"content"`
	Model        string    `json:"model"`
	StopReason   string    `json:"stop_reason,omitempty"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	FinishedAt   time.Time `json:"finished_at"`
}

// StreamHandler is called for each chunk of streamed content.
type StreamHandler func(chunk string) error

// Usage tracks token usage for a request.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ProviderError represents an error from a provider.
type ProviderError struct {
	Provider string
	Message  string
	Code     string
	Err      error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return e.Provider + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Provider + ": " + e.Message
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}
