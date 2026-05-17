package tui

import (
	"testing"

	"github.com/bprendie/weazlcode/internal/config"
)

func TestNotificationCmdHonorsConfig(t *testing.T) {
	m := commandTestModel(t)
	bell := true
	m.cfg.Notifications = config.Notifications{
		Enabled: true,
		Bell:    &bell,
		Events:  []string{"task_done"},
	}
	if cmd := m.notificationCmd("task_done", "Task", "done"); cmd == nil {
		t.Fatal("notification cmd = nil, want bell command")
	}
	if cmd := m.notificationCmd("repair_requested", "Task", "fix"); cmd != nil {
		t.Fatal("notification cmd for disabled event was not nil")
	}
}

func TestNotificationEventEnabledSupportsWildcard(t *testing.T) {
	if !notificationEventEnabled("task_blocked", []string{"*"}) {
		t.Fatal("wildcard notification event not enabled")
	}
	if notificationEventEnabled("", []string{"*"}) {
		t.Fatal("empty notification event enabled")
	}
}
