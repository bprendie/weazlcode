package config

import "testing"

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
	if cfg.Workers.Concurrency != 2 {
		t.Fatalf("Workers.Concurrency = %d, want 2", cfg.Workers.Concurrency)
	}
	if cfg.Workers.RequestTimeoutSeconds != 300 {
		t.Fatalf("Workers.RequestTimeoutSeconds = %d, want 300", cfg.Workers.RequestTimeoutSeconds)
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
	if cfg.Workers.Concurrency != 2 {
		t.Fatalf("Workers.Concurrency = %d, want 2", cfg.Workers.Concurrency)
	}
	if cfg.Workers.RequestTimeoutSeconds != 300 {
		t.Fatalf("Workers.RequestTimeoutSeconds = %d, want 300", cfg.Workers.RequestTimeoutSeconds)
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
