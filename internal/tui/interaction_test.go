package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMouseCopyCommandsToggleMouseScroll(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/copy")
	if !handled {
		t.Fatal("/copy handled = false")
	}
	got := updated.(model)
	if got.mouseScroll || got.status != "copy mode enabled" {
		t.Fatalf("after /copy mouse/status = %t/%q", got.mouseScroll, got.status)
	}
	updated, _, handled = got.handleSlashCommand("/mouse")
	if !handled {
		t.Fatal("/mouse handled = false")
	}
	got = updated.(model)
	if !got.mouseScroll || got.status != "mouse scroll enabled" {
		t.Fatalf("after /mouse mouse/status = %t/%q", got.mouseScroll, got.status)
	}
}

func TestViewportScrollKeysWorkInChatAndIDEView(t *testing.T) {
	m := commandTestModel(t)
	m.width = 80
	m.height = 30
	m.viewport.Width = 74
	m.viewport.Height = 14
	m.viewport.SetContent(strings.Repeat("line\n", 100))
	m.viewport.GotoBottom()
	before := m.viewport.YOffset
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	got := updated.(model)
	if got.viewport.YOffset >= before {
		t.Fatalf("chat pgup offset = %d, before %d", got.viewport.YOffset, before)
	}
	got.setIDEView("test", strings.Repeat("view\n", 100))
	got.viewport.GotoBottom()
	before = got.viewport.YOffset
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	got = updated.(model)
	if got.viewport.YOffset >= before {
		t.Fatalf("ide pgup offset = %d, before %d", got.viewport.YOffset, before)
	}
}
