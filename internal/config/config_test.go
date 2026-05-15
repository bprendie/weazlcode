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
