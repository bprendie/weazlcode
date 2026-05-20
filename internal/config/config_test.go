package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultModelRoles(t *testing.T) {
	cfg := Default()
	if cfg.ModelRoles.Orchestrator == "" || cfg.ModelRoles.Worker == "" || cfg.ModelRoles.Reviewer == "" || cfg.ModelRoles.Summarizer == "" {
		t.Fatalf("ModelRoles not fully defaulted: %#v", cfg.ModelRoles)
	}
	if got := cfg.ProviderForRole("worker"); got.Model != "llama3.1" {
		t.Fatalf("worker model = %q, want llama3.1", got.Model)
	}
	if !cfg.Skills.SkillsEnabled() || len(cfg.Skills.Paths) == 0 {
		t.Fatalf("Skills = %#v, want enabled defaults", cfg.Skills)
	}
	if cfg.Workers.Concurrency != RecommendedWorkerConcurrency("ollama") {
		t.Fatalf("Workers.Concurrency = %d, want %d", cfg.Workers.Concurrency, RecommendedWorkerConcurrency("ollama"))
	}
	if cfg.Workers.RequestTimeoutSeconds != 300 {
		t.Fatalf("Workers.RequestTimeoutSeconds = %d, want 300", cfg.Workers.RequestTimeoutSeconds)
	}
	if cfg.Workers.OutputTokens != 4096 || cfg.Workers.ArtifactOutputTokens != 24576 {
		t.Fatalf("Workers output tokens = %d/%d, want 4096/24576", cfg.Workers.OutputTokens, cfg.Workers.ArtifactOutputTokens)
	}
	if cfg.Workers.RunTimeoutSeconds != 900 {
		t.Fatalf("Workers.RunTimeoutSeconds = %d, want 900", cfg.Workers.RunTimeoutSeconds)
	}
	if cfg.Hooks.Enabled || cfg.Hooks.TimeoutSeconds != 10 || cfg.Hooks.Events == nil {
		t.Fatalf("Hooks = %#v, want disabled with defaults", cfg.Hooks)
	}
	if cfg.Notifications.Enabled || cfg.Notifications.Bell == nil || !*cfg.Notifications.Bell || len(cfg.Notifications.Events) == 0 {
		t.Fatalf("Notifications = %#v, want disabled with defaults", cfg.Notifications)
	}
	if cfg.Debug.TimeoutSeconds != 30 || cfg.Debug.Adapters == nil || cfg.Debug.Configurations == nil {
		t.Fatalf("Debug = %#v, want empty defaults with timeout", cfg.Debug)
	}
}

func TestModelRolesDefaultToActiveProvider(t *testing.T) {
	cfg := Config{
		ActiveProvider: "primary",
		Providers: map[string]Provider{
			"primary": {Type: "vllm", ServerURL: "http://localhost:8000", Model: "model"},
		},
	}
	cfg.withDefaults()
	if cfg.ModelRoles.Orchestrator != "primary" || cfg.ModelRoles.Worker != "primary" {
		t.Fatalf("ModelRoles = %#v, want primary defaults", cfg.ModelRoles)
	}
	if !cfg.Skills.SkillsEnabled() || len(cfg.Skills.Paths) == 0 {
		t.Fatalf("Skills = %#v, want default paths", cfg.Skills)
	}
	if cfg.Workers.Concurrency != RecommendedWorkerConcurrency("vllm") {
		t.Fatalf("Workers.Concurrency = %d, want %d", cfg.Workers.Concurrency, RecommendedWorkerConcurrency("vllm"))
	}
	if cfg.Workers.RequestTimeoutSeconds != 300 {
		t.Fatalf("Workers.RequestTimeoutSeconds = %d, want 300", cfg.Workers.RequestTimeoutSeconds)
	}
	if cfg.Workers.OutputTokens != 4096 || cfg.Workers.ArtifactOutputTokens != 24576 {
		t.Fatalf("Workers output tokens = %d/%d, want 4096/24576", cfg.Workers.OutputTokens, cfg.Workers.ArtifactOutputTokens)
	}
	if cfg.Workers.RunTimeoutSeconds != 900 {
		t.Fatalf("Workers.RunTimeoutSeconds = %d, want 900", cfg.Workers.RunTimeoutSeconds)
	}
	if cfg.Hooks.TimeoutSeconds != 10 || cfg.Hooks.Events == nil {
		t.Fatalf("Hooks = %#v, want defaults", cfg.Hooks)
	}
	if cfg.Notifications.Bell == nil || !*cfg.Notifications.Bell || len(cfg.Notifications.Events) == 0 {
		t.Fatalf("Notifications = %#v, want defaults", cfg.Notifications)
	}
	if cfg.Debug.TimeoutSeconds != 30 || cfg.Debug.Adapters == nil || cfg.Debug.Configurations == nil {
		t.Fatalf("Debug = %#v, want defaults", cfg.Debug)
	}
}

func TestSkillsEnabledCanBeDisabledWithoutCustomPaths(t *testing.T) {
	disabled := false
	cfg := Config{Skills: Skills{Enabled: &disabled}}
	cfg.withDefaults()
	if cfg.Skills.SkillsEnabled() {
		t.Fatalf("SkillsEnabled = true, want false")
	}
	if len(cfg.Skills.Paths) == 0 {
		t.Fatalf("Skills paths were not defaulted")
	}
}

func TestWindowsAppDataRootUsesWeazlcodeHomeThenAppData(t *testing.T) {
	t.Setenv("WEAZLCODE_HOME", filepath.Join("C:", "custom", "weazlcode"))
	t.Setenv("APPDATA", filepath.Join("C:", "Users", "bob", "AppData", "Roaming"))
	if got := windowsAppDataRoot(); got != filepath.Join("C:", "custom", "weazlcode") {
		t.Fatalf("windowsAppDataRoot = %q", got)
	}

	t.Setenv("WEAZLCODE_HOME", "")
	if got := windowsAppDataRoot(); got != filepath.Join("C:", "Users", "bob", "AppData", "Roaming", "weazlcode") {
		t.Fatalf("windowsAppDataRoot = %q", got)
	}
}
