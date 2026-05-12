package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/project"
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

func commandTestModel() model {
	cfg := config.Default()
	registry := tools.NewRegistry()
	registry.Register(tools.NewCalculatorTool())
	ti := textinput.New()
	ti.Focus()
	return model{
		cfg:          cfg,
		project:      project.Summary{Root: "/tmp/weazlcode", GitRoot: true, Branch: "main", Dirty: true, StateDir: "/tmp/weazlcode/.weazlcode", LogDir: "/tmp/weazlcode/.weazlcode/logs", Languages: []string{"go"}, FileCount: 42},
		toolRegistry: registry,
		styles:       newStyles(),
		mode:         modeChat,
		input:        ti,
		viewport:     viewport.New(80, 20),
		mouseScroll:  true,
	}
}
