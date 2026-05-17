package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/config"
)

func TestDebugCommandTextShowsEmptyState(t *testing.T) {
	m := commandTestModel(t)
	updated, _, handled := m.handleSlashCommand("/debug")
	if !handled {
		t.Fatal("debug handled = false")
	}
	got := updated.(model)
	view := got.viewport.View()
	for _, want := range []string{"Debug:", "adapters: 0", "No debug adapters configured."} {
		if !strings.Contains(view, want) {
			t.Fatalf("debug view missing %q:\n%s", want, view)
		}
	}
}

func TestDebugLaunchRunsConfiguredAdapter(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	script := filepath.Join(root, "dap.sh")
	captured := filepath.Join(root, "dap-input.json")
	scriptBody := "#!/bin/sh\ncat > " + captured + "\nprintf 'adapter-ok\\n'\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("WriteFile script: %v", err)
	}
	m.project.Root = root
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	m.cfg.Debug = config.Debug{
		Adapters: map[string]config.DebugAdapter{
			"go": {Command: script},
		},
		Configurations: []config.DebugConfiguration{
			{Name: "unit", Type: "go", Request: "launch", Program: "main.go", Cwd: "."},
		},
		TimeoutSeconds: 5,
	}

	updated, _, handled := m.handleSlashCommand("/debug launch unit")
	if !handled {
		t.Fatal("debug launch handled = false")
	}
	got := updated.(model)
	if got.status != "debug launched" {
		t.Fatalf("status = %q, want debug launched", got.status)
	}
	data, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("ReadFile captured: %v", err)
	}
	for _, want := range []string{`"command":"launch"`, `"name":"unit"`, `"type":"go"`, `"program":"` + filepath.Join(root, "main.go") + `"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("debug payload missing %q:\n%s", want, data)
		}
	}
	logData, err := os.ReadFile(filepath.Join(root, ".weazlcode", "logs", "debug.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile debug log: %v", err)
	}
	if !strings.Contains(string(logData), `"name":"unit"`) || !strings.Contains(string(logData), `"success":true`) || !strings.Contains(string(logData), "adapter-ok") {
		t.Fatalf("debug log incomplete:\n%s", logData)
	}
}

func TestDebugLaunchRejectsOutsideProject(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.cfg.Debug = config.Debug{
		Adapters: map[string]config.DebugAdapter{
			"go": {Command: "true"},
		},
		Configurations: []config.DebugConfiguration{
			{Name: "bad", Type: "go", Program: "../outside.go"},
		},
	}
	updated, _, handled := m.handleSlashCommand("/debug launch bad")
	if !handled {
		t.Fatal("debug launch handled = false")
	}
	got := updated.(model)
	if got.status != "debug failed" {
		t.Fatalf("status = %q, want debug failed", got.status)
	}
}
