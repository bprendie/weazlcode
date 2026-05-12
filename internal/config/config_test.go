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
}
