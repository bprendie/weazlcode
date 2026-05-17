package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/config"
)

func TestRunHooksExecutesConfiguredCommandAndLogs(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	logDir := filepath.Join(root, ".weazlcode", "logs")
	script := filepath.Join(root, "capture-hook.sh")
	captured := filepath.Join(root, "captured.jsonl")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >> \"$1\"\nprintf '\\n' >> \"$1\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile script: %v", err)
	}
	m.project.Root = root
	m.project.LogDir = logDir
	m.session.ProjectRoot = root
	m.cfg.Hooks = config.Hooks{
		Enabled:        true,
		TimeoutSeconds: 5,
		Events: map[string][]config.HookCommand{
			"task_done": {{Command: script, Args: []string{captured}}},
		},
	}

	m.runHooks("task_done", map[string]any{"task_id": "task-1", "title": "Done"})

	data, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("ReadFile captured: %v", err)
	}
	for _, want := range []string{`"event":"task_done"`, `"task_id":"task-1"`, `"project_root":"` + root + `"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("captured hook payload missing %q:\n%s", want, data)
		}
	}
	logData, err := os.ReadFile(filepath.Join(logDir, "hooks.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile hook log: %v", err)
	}
	for _, want := range []string{`"event":"task_done"`, `"success":true`, script} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("hook log missing %q:\n%s", want, logData)
		}
	}
}

func TestRunHooksIgnoresDisabledHooks(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	captured := filepath.Join(root, "captured.jsonl")
	m.project.Root = root
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.cfg.Hooks = config.Hooks{
		Enabled: false,
		Events: map[string][]config.HookCommand{
			"task_done": {{Command: "/bin/sh", Args: []string{"-c", "cat >> " + captured}}},
		},
	}
	m.runHooks("task_done", nil)
	if _, err := os.Stat(captured); !os.IsNotExist(err) {
		t.Fatalf("disabled hook wrote file, stat err=%v", err)
	}
}
