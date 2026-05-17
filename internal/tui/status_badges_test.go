package tui

import (
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/config"
)

func TestStatusBadges(t *testing.T) {
	m := commandTestModel(t)
	m.cfg.ActiveProvider = "local-vllm"
	m.cfg.ModelRoles = config.ModelRoles{
		Orchestrator: "local-vllm",
		Worker:       "local-ollama",
		Reviewer:     "frontier",
		Summarizer:   "local-ollama",
	}
	badges := strings.Join(m.statusBadges(100, 1000), " ")
	for _, want := range []string{"proj:weazlcode", "git:main", "repo:dirty", "role:orchestrator", "ctx:10%"} {
		if !strings.Contains(badges, want) {
			t.Fatalf("badges missing %q: %q", want, badges)
		}
	}
}

func TestStatusBadgesNoGit(t *testing.T) {
	m := commandTestModel(t)
	m.project.GitRoot = false
	m.project.Branch = ""
	m.project.Dirty = false
	badges := strings.Join(m.statusBadges(0, 0), " ")
	for _, want := range []string{"git:none", "repo:no-git", "ctx:0%"} {
		if !strings.Contains(badges, want) {
			t.Fatalf("badges missing %q: %q", want, badges)
		}
	}
}

func TestStatusBadgesShowProcessingRoleDuringWorkerRun(t *testing.T) {
	m := commandTestModel(t)
	m.cfg.ActiveProvider = "planning-llm"
	m.cfg.ModelRoles = config.ModelRoles{
		Orchestrator: "planning-llm",
		Worker:       "local-worker",
		Reviewer:     "planning-llm",
		Summarizer:   "local-worker",
	}
	m.workerRuns = map[int]string{1: "task-1"}
	badges := strings.Join(m.statusBadges(0, 1000), " ")
	if !strings.Contains(badges, "role:worker") {
		t.Fatalf("badges missing active worker role: %q", badges)
	}
}
