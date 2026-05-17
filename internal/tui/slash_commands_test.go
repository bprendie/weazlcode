package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bprendie/weazlcode/internal/config"
)

func TestSlashHelpCommand(t *testing.T) {
	m := commandTestModel()
	m.input.SetValue("/help")
	updated, _, handled := m.handleSlashCommand("/help")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if got.input.Value() != "" {
		t.Fatalf("input = %q, want empty", got.input.Value())
	}
	if !strings.Contains(got.viewport.View(), "/files [query] - fuzzy-find project files") {
		t.Fatalf("viewport missing help: %q", got.viewport.View())
	}
}

func TestSlashCommandsShowsPalette(t *testing.T) {
	m := commandTestModel()
	updated, _, handled := m.handleSlashCommand("/commands")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	view := got.viewport.View()
	for _, want := range []string{"Command palette:", "Plan:", "/plan edit", "Worker:", "/run-worker"} {
		if !strings.Contains(view, want) {
			t.Fatalf("palette missing %q:\n%s", want, view)
		}
	}
	raw := commandPaletteText()
	for _, want := range []string{"Review:", "/review-diff", "Skills:", "/skills", "/run-workers"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("raw palette missing %q:\n%s", want, raw)
		}
	}
}

func TestSlashProjectCommand(t *testing.T) {
	m := commandTestModel()
	updated, _, handled := m.handleSlashCommand("/project")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if !strings.Contains(got.viewport.View(), "root: /tmp/weazlcode") {
		t.Fatalf("viewport missing project root: %q", got.viewport.View())
	}
	if got.status != "view project" || got.mode != modeIDEView {
		t.Fatalf("status/mode = %q/%v, want view project/%v", got.status, got.mode, modeIDEView)
	}
}

func TestSlashModelsCommand(t *testing.T) {
	m := commandTestModel()
	updated, _, handled := m.handleSlashCommand("/models")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if !strings.Contains(got.viewport.View(), "worker: local-ollama -> ollama/llama3.1") {
		t.Fatalf("viewport missing model roles: %q", got.viewport.View())
	}
}

func TestModelProviderBarShowsSplitBrainProviders(t *testing.T) {
	m := commandTestModel()
	m.cfg.Providers["planning-llm"] = config.Provider{Type: "anthropic", Model: "claude-test"}
	m.cfg.ModelRoles.Orchestrator = "planning-llm"
	m.cfg.ModelRoles.Reviewer = "planning-llm"
	bar := m.modelProviderBar()
	for _, want := range []string{"plan:anthropic/claude-test", "worker:ollama/llama3.1", "review:anthropic/claude-test"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("bar missing %q: %s", want, bar)
		}
	}
}

func TestThinkingViewShowsProcessingModel(t *testing.T) {
	m := commandTestModel()
	m.cfg.Providers["planning-llm"] = config.Provider{Type: "anthropic", Model: "claude-test"}
	m.cfg.ModelRoles.Orchestrator = "planning-llm"
	m.thinking = true
	m.status = "streaming orchestrator"
	m.streamAt = time.Now()
	view := m.thinkingView()
	if !strings.Contains(view, "[orchestrator:anthropic/claude-test]") {
		t.Fatalf("thinking view missing processing model: %q", view)
	}
}

func TestThinkingViewPrefersActiveWorkerOverStatus(t *testing.T) {
	m := commandTestModel()
	m.cfg.Providers["local-worker"] = config.Provider{Type: "vllm", Model: "granite-8b"}
	m.cfg.ModelRoles.Worker = "local-worker"
	m.thinking = true
	m.status = "task reviewing"
	m.workerRuns = map[int]string{7: "task-1"}
	m.streamAt = time.Now()
	view := m.thinkingView()
	if !strings.Contains(view, "[worker:vllm/granite-8b]") {
		t.Fatalf("thinking view missing active worker: %q", view)
	}
}

func TestCodeChangeChatAutoRoutesToPlanGenerate(t *testing.T) {
	m := commandTestModel(t)
	m.input.SetValue("implement a settings page")
	updated, cmd := m.handleEnter()
	got := updated.(model)
	if cmd == nil || !got.thinking || got.status != "generating plan" {
		t.Fatalf("state = cmd:%v thinking:%t status:%q", cmd, got.thinking, got.status)
	}
	if strings.TrimSpace(got.input.Value()) != "" {
		t.Fatalf("input was not cleared: %q", got.input.Value())
	}
}

func TestReadOnlyChatDoesNotAutoRouteToPlanGenerate(t *testing.T) {
	for _, prompt := range []string{"look at ../weazl.world and summarize it", "explain how routing works"} {
		if shouldAutoGeneratePlan(prompt) {
			t.Fatalf("prompt should stay chat: %q", prompt)
		}
	}
	for _, prompt := range []string{"fix the routing bug", "write code for a settings page", "look at the repo and fix the failing test"} {
		if !shouldAutoGeneratePlan(prompt) {
			t.Fatalf("prompt should route to plan: %q", prompt)
		}
	}
}

func TestOrchestratorPromptIncludesWorkerCapacity(t *testing.T) {
	m := commandTestModel(t)
	worker := m.cfg.Providers[m.cfg.ModelRoles.Worker]
	worker.Model = "granite-8b"
	m.cfg.Providers[m.cfg.ModelRoles.Worker] = worker
	prompt := m.orchestratorPrompt("how should we split this work?")
	for _, want := range []string{"WeazlCode execution model:", "granite-8b", "small 8.0B-class local worker", "bounded local worker tasks"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSlashDurableIDEViews(t *testing.T) {
	m := commandTestModel(t)
	for _, command := range []string{"/tools", "/skills", "/config", "/outputs", "/files"} {
		updated, _, handled := m.handleSlashCommand(command)
		if !handled {
			t.Fatalf("%s handled = false, want true", command)
		}
		got := updated.(model)
		if got.mode != modeIDEView {
			t.Fatalf("%s mode = %v, want modeIDEView", command, got.mode)
		}
		if !strings.HasPrefix(got.status, "view ") {
			t.Fatalf("%s status = %q", command, got.status)
		}
		if strings.TrimSpace(got.viewport.View()) == "" {
			t.Fatalf("%s viewport empty", command)
		}
		m = got
	}
	updated, _, handled := m.handleSlashCommand("/chat")
	if !handled {
		t.Fatal("/chat handled = false, want true")
	}
	got := updated.(model)
	if got.mode != modeChat || got.status != "chat" {
		t.Fatalf("mode/status = %v/%q, want chat", got.mode, got.status)
	}
}

func TestSlashSkillsCommandShowsDiscoveredSkills(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	skillDir := filepath.Join(root, ".weazlcode", "skills", "repo-style")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("name: repo-style\ndescription: Follow repo conventions.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.project.Root = root
	m.cfg.Skills.Paths = []string{".weazlcode/skills"}

	updated, _, handled := m.handleSlashCommand("/skills")
	if !handled {
		t.Fatal("skills handled = false")
	}
	view := updated.(model).viewport.View()
	for _, want := range []string{"repo-style", "Follow repo conventions.", ".weazlcode/skills"} {
		if !strings.Contains(view, want) {
			t.Fatalf("skills view missing %q:\n%s", want, view)
		}
	}
}

func TestSlashDiffCommand(t *testing.T) {
	m, _ := commandTestModelWithReviewingTask(t)
	updated, _, handled := m.handleSlashCommand("/diff")
	if !handled {
		t.Fatal("diff handled = false, want true")
	}
	got := updated.(model)
	if got.mode != modeIDEView || got.status != "view diff" {
		t.Fatalf("mode/status = %v/%q", got.mode, got.status)
	}
	view := got.viewport.View()
	if !strings.Contains(view, "Diff: 1 file(s)") || !strings.Contains(view, "1. README.md") {
		t.Fatalf("viewport missing diff: %q", view)
	}
}

func TestNonSlashCommandNotHandled(t *testing.T) {
	m := commandTestModel()
	_, _, handled := m.handleSlashCommand("hello")
	if handled {
		t.Fatal("handled = true, want false")
	}
}

func TestCancelModelCommandCancelsActiveRun(t *testing.T) {
	m := commandTestModel(t)
	cancelled := false
	m.thinking = true
	m.activeModelRunID = 7
	m.cancelModel = func() { cancelled = true }
	updated, _, handled := m.handleSlashCommand("/cancel")
	if !handled {
		t.Fatal("cancel handled = false")
	}
	got := updated.(model)
	if !cancelled {
		t.Fatal("cancel func was not called")
	}
	if got.thinking || got.activeModelRunID != 0 || got.cancelModel != nil || got.status != "model cancelled" {
		t.Fatalf("model state = thinking:%t run:%d cancel:%v status:%q", got.thinking, got.activeModelRunID, got.cancelModel, got.status)
	}
}

func TestCancelModelCommandCancelsOneWorkerTask(t *testing.T) {
	m := commandTestModel(t)
	cancelled := false
	m.thinking = true
	m.workerRuns = map[int]string{7: "task-1"}
	m.cancelWorkerRuns = map[int]context.CancelFunc{7: func() { cancelled = true }}
	updated, _, handled := m.handleSlashCommand("/cancel task-1")
	if !handled {
		t.Fatal("cancel handled = false")
	}
	got := updated.(model)
	if !cancelled {
		t.Fatal("worker cancel func was not called")
	}
	if got.thinking || len(got.workerRuns) != 0 || got.status != "worker cancelled" {
		t.Fatalf("worker cancel state: thinking=%t runs=%#v status=%q", got.thinking, got.workerRuns, got.status)
	}
}

func TestEnterAllowsCancelWhileThinking(t *testing.T) {
	m := commandTestModel(t)
	cancelled := false
	m.thinking = true
	m.workerRuns = map[int]string{7: "task-1"}
	m.cancelWorkerRuns = map[int]context.CancelFunc{7: func() { cancelled = true }}
	m.input.SetValue("/cancel workers")
	updated, _ := m.handleEnter()
	got := updated.(model)
	if !cancelled || got.thinking || len(got.workerRuns) != 0 {
		t.Fatalf("cancel while thinking failed: cancelled=%t thinking=%t runs=%#v", cancelled, got.thinking, got.workerRuns)
	}
}

func TestStaleModelMessagesAreIgnored(t *testing.T) {
	m := commandTestModel(t)
	m.activeModelRunID = 2
	m.thinking = true
	updated, _ := m.Update(workerRunMsg{runID: 1, err: errors.New("late")})
	got := updated.(model)
	if !got.thinking || got.status != "" || got.activeModelRunID != 2 {
		t.Fatalf("stale message changed model: thinking=%t status=%q run=%d", got.thinking, got.status, got.activeModelRunID)
	}
}
