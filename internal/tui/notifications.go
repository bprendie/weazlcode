package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) notificationCmd(event, title, body string) tea.Cmd {
	if !m.cfg.Notifications.Enabled || !notificationEventEnabled(event, m.cfg.Notifications.Events) {
		return nil
	}
	if m.cfg.Notifications.Bell != nil && !*m.cfg.Notifications.Bell {
		return nil
	}
	return func() tea.Msg {
		fmt.Print("\a")
		return nil
	}
}

func notificationEventEnabled(event string, events []string) bool {
	event = strings.TrimSpace(event)
	if event == "" {
		return false
	}
	for _, configured := range events {
		configured = strings.TrimSpace(configured)
		if configured == "*" || configured == event {
			return true
		}
	}
	return false
}
