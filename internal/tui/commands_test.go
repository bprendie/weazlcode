package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/storage"
	"github.com/bprendie/weazlcode/internal/tools"
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
	if !strings.Contains(got.viewport.View(), "/project - show active project") {
		t.Fatalf("viewport missing help: %q", got.viewport.View())
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
	if got.status != "project summary" {
		t.Fatalf("status = %q, want project summary", got.status)
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

func TestNonSlashCommandNotHandled(t *testing.T) {
	m := commandTestModel()
	_, _, handled := m.handleSlashCommand("hello")
	if handled {
		t.Fatal("handled = true, want false")
	}
}

func TestSlashPlanDraftCommand(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/plan draft Add planner")
	if !handled {
		t.Fatal("handled = false, want true")
	}
	got := updated.(model)
	if got.status != "draft plan created" {
		t.Fatalf("status = %q, want draft plan created", got.status)
	}
	if !strings.Contains(got.viewport.View(), "Plan: Add planner") {
		t.Fatalf("viewport missing plan: %q", got.viewport.View())
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || plan.Title != "Add planner" || len(plan.Tasks) != 1 {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
}

func commandTestModel(t ...*testing.T) model {
	cfg := config.Default()
	registry := tools.NewRegistry()
	registry.Register(tools.NewCalculatorTool())
	ti := textinput.New()
	ti.Focus()
	var store *storage.Store
	session := storage.Session{ID: "session-1", Title: "New session", Provider: "local-vllm", Model: "local-model", ProjectRoot: "/tmp/weazlcode"}
	if len(t) > 0 && t[0] != nil {
		var err error
		store, err = storage.Open(filepath.Join(t[0].TempDir(), "test.sqlite3"))
		if err != nil {
			t[0].Fatal(err)
		}
		t[0].Cleanup(func() { _ = store.Close() })
		if err := store.Migrate(); err != nil {
			t[0].Fatal(err)
		}
		if err := store.CreateProjectSession(session.ID, session.Title, session.Provider, session.Model, session.ProjectRoot); err != nil {
			t[0].Fatal(err)
		}
	}
	return model{
		cfg:          cfg,
		project:      project.Summary{Root: "/tmp/weazlcode", GitRoot: true, Branch: "main", Dirty: true, StateDir: "/tmp/weazlcode/.weazlcode", LogDir: "/tmp/weazlcode/.weazlcode/logs", Languages: []string{"go"}, FileCount: 42},
		store:        store,
		toolRegistry: registry,
		styles:       newStyles(),
		mode:         modeChat,
		input:        ti,
		viewport:     viewport.New(80, 20),
		mouseScroll:  true,
		session:      session,
	}
}
