package providers

import (
	"fmt"
	"strings"

	"github.com/bprendie/weazlcode/internal/config"
)

// NewProvider creates a provider based on the configuration.
func NewProvider(cfg config.ProviderConfig) (Provider, error) {
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("provider not configured")
	}

	switch cfg.Provider {
	case "anthropic":
		apiKey := cfg.GetAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("anthropic API key not found in environment variable %s", cfg.APIKeyEnv)
		}
		provider := NewAnthropicProvider(apiKey, cfg.Model)
		// Normalize model name for common aliases
		normalizedModel := provider.NormalizeModelName(cfg.Model)
		return NewAnthropicProvider(apiKey, normalizedModel), nil

	case "openai":
		apiKey := cfg.GetAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("openai API key not found in environment variable %s", cfg.APIKeyEnv)
		}
		return NewOpenAIProvider(cfg.BaseURL, apiKey, cfg.Model), nil

	case "vllm", "ollama":
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("%s requires base_url to be configured", cfg.Provider)
		}
		// vLLM and Ollama both use OpenAI-compatible API
		return NewOpenAIProvider(openAICompatibleBaseURL(cfg.BaseURL), "", cfg.Model), nil

	default:
		return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}

func openAICompatibleBaseURL(raw string) string {
	baseURL := strings.TrimRight(raw, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

// NewPlannerProvider creates a provider for the planner role.
func NewPlannerProvider(cfg *config.Config) (Provider, error) {
	return NewProvider(cfg.Planner)
}

// NewWorkerProvider creates a provider for the worker role.
func NewWorkerProvider(cfg *config.Config) (Provider, error) {
	return NewProvider(cfg.Worker)
}

// NewReviewerProvider creates a provider for the reviewer role.
func NewReviewerProvider(cfg *config.Config) (Provider, error) {
	return NewProvider(cfg.Reviewer)
}
