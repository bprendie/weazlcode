package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/project"
)

func TestViewFitsHalfWidthTerminal(t *testing.T) {
	m := Model{
		config: &config.Config{
			Planner:  config.ProviderConfig{Provider: "anthropic", Model: "claude-haiku-4-5-20251001"},
			Worker:   config.ProviderConfig{Provider: "vllm", Model: "cyankiwi/granite-4.1-8b-AWQ-INT4"},
			Reviewer: config.ProviderConfig{Provider: "vllm", Model: "cyankiwi/granite-4.1-8b-AWQ-INT4"},
		},
		project: &project.Summary{
			Name: "weazlcode-smoke-final",
			Root: "/tmp/some/very/long/path/that/should/not/wrap/the/header/or/footer/weazlcode-smoke-final",
		},
		viewport: viewport.New(1, 1),
		input:    textarea.New(),
		mode:     "input",
		width:    80,
		height:   24,
		ready:    true,
		styles:   NewCyberpunkStyles(),
	}
	m.input.Focus()
	m.input.SetHeight(3)
	m.resize()

	view := m.View()
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line %d width = %d, want <= %d: %q", i+1, got, m.width, line)
		}
	}
	if got := lipgloss.Height(view); got > m.height {
		t.Fatalf("height = %d, want <= %d", got, m.height)
	}
}
