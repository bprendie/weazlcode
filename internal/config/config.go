package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the application configuration.
type Config struct {
	Planner  ProviderConfig `json:"planner"`
	Worker   ProviderConfig `json:"worker"`
	Reviewer ProviderConfig `json:"reviewer"`
	Limits   LimitsConfig   `json:"limits"`
}

// ProviderConfig represents configuration for a model provider.
type ProviderConfig struct {
	Provider  string `json:"provider"` // anthropic, openai, vllm, ollama
	Model     string `json:"model"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
}

// LimitsConfig represents execution limits and constraints.
type LimitsConfig struct {
	WorkerConcurrency   int `json:"worker_concurrency"`
	ModelTimeoutSeconds int `json:"model_timeout_seconds"`
	RunSoftLimitMinutes int `json:"run_soft_limit_minutes"`
	RepairAttempts      int `json:"repair_attempts"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Planner: ProviderConfig{
			Provider: "vllm",
			Model:    "cyankiwi/granite-4.1-8b-AWQ-INT4",
			BaseURL:  "https://granite.prendie.io",
		},
		Worker: ProviderConfig{
			Provider: "vllm",
			Model:    "cyankiwi/granite-4.1-8b-AWQ-INT4",
			BaseURL:  "https://granite.prendie.io",
		},
		Reviewer: ProviderConfig{
			Provider: "vllm",
			Model:    "cyankiwi/granite-4.1-8b-AWQ-INT4",
			BaseURL:  "https://granite.prendie.io",
		},
		Limits: LimitsConfig{
			WorkerConcurrency:   6,
			ModelTimeoutSeconds: 90,
			RunSoftLimitMinutes: 5,
			RepairAttempts:      2,
		},
	}
}

// Load loads configuration from the default location or creates a default config.
func Load() (*Config, string, error) {
	configPath, err := DefaultConfigPath()
	if err != nil {
		return nil, "", fmt.Errorf("get config path: %w", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := Save(cfg, configPath); err != nil {
			return nil, "", fmt.Errorf("save default config: %w", err)
		}
		return cfg, configPath, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, "", fmt.Errorf("parse config: %w", err)
	}

	if applyDefaults(&cfg) {
		if err := Save(&cfg, configPath); err != nil {
			return nil, configPath, fmt.Errorf("save migrated config: %w", err)
		}
	}

	return &cfg, configPath, nil
}

func applyDefaults(cfg *Config) bool {
	changed := false
	defaults := DefaultConfig()
	if cfg.Planner.Provider == "" && cfg.Planner.Model == "" {
		cfg.Planner = defaults.Planner
		changed = true
	}
	if cfg.Worker.Provider == "" && cfg.Worker.Model == "" {
		cfg.Worker = defaults.Worker
		changed = true
	}
	if cfg.Reviewer.Provider == "" && cfg.Reviewer.Model == "" {
		cfg.Reviewer = defaults.Reviewer
		changed = true
	}
	if cfg.Limits.WorkerConcurrency == 0 {
		cfg.Limits.WorkerConcurrency = defaults.Limits.WorkerConcurrency
		changed = true
	}
	if cfg.Limits.ModelTimeoutSeconds == 0 {
		cfg.Limits.ModelTimeoutSeconds = defaults.Limits.ModelTimeoutSeconds
		changed = true
	}
	if cfg.Limits.RunSoftLimitMinutes == 0 {
		cfg.Limits.RunSoftLimitMinutes = defaults.Limits.RunSoftLimitMinutes
		changed = true
	}
	if cfg.Limits.RepairAttempts == 0 {
		cfg.Limits.RepairAttempts = defaults.Limits.RepairAttempts
		changed = true
	}
	return changed
}

// Save writes the configuration to the specified path.
func Save(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// DefaultConfigPath returns the default configuration file path.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "weazlcode", "config.json"), nil
}

// GetAPIKey retrieves an API key from the environment variable specified in the config.
func (p *ProviderConfig) GetAPIKey() string {
	if p.APIKeyEnv != "" {
		if key := os.Getenv(p.APIKeyEnv); key != "" {
			return key
		}
	}
	return p.APIKey
}

// IsConfigured returns true if the provider has required configuration.
func (p *ProviderConfig) IsConfigured() bool {
	if p.Provider == "" || p.Model == "" {
		return false
	}
	// For local providers (vllm, ollama), base_url is required
	if p.Provider == "vllm" || p.Provider == "ollama" {
		return p.BaseURL != ""
	}
	// For cloud providers, an env var or local-only config key is required.
	return p.APIKeyEnv != "" || p.APIKey != ""
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if !c.Planner.IsConfigured() {
		return fmt.Errorf("planner provider not configured")
	}
	if !c.Worker.IsConfigured() {
		return fmt.Errorf("worker provider not configured")
	}
	if !c.Reviewer.IsConfigured() {
		return fmt.Errorf("reviewer provider not configured")
	}
	if c.Limits.WorkerConcurrency < 1 {
		return fmt.Errorf("worker_concurrency must be at least 1")
	}
	if c.Limits.ModelTimeoutSeconds < 1 {
		return fmt.Errorf("model_timeout_seconds must be at least 1")
	}
	if c.Limits.RepairAttempts < 0 {
		return fmt.Errorf("repair_attempts must be non-negative")
	}
	return nil
}
