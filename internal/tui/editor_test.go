package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/config"
)

func TestOpenExternalEditorCommandUsesConfiguredTemplate(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	script := filepath.Join(root, "editor.sh")
	captured := filepath.Join(root, "editor-args.txt")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$1\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile script: %v", err)
	}
	m.project.Root = root
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	m.cfg.Editor = config.Editor{
		Command: script,
		Args:    []string{captured, "--goto", "{file}:{line}"},
		Wait:    true,
	}

	updated, _, handled := m.handleSlashCommand("/edit README.md 12")
	if !handled {
		t.Fatal("edit handled = false")
	}
	got := updated.(model)
	if got.status != "editor closed" {
		t.Fatalf("status = %q, want editor closed", got.status)
	}
	data, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("ReadFile captured: %v", err)
	}
	if !strings.Contains(string(data), "README.md:12") {
		t.Fatalf("editor args missing file line template:\n%s", data)
	}
	logData, err := os.ReadFile(filepath.Join(root, ".weazlcode", "logs", "editor.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile editor log: %v", err)
	}
	if !strings.Contains(string(logData), `"path":"README.md"`) || !strings.Contains(string(logData), `"success":true`) {
		t.Fatalf("editor log incomplete:\n%s", logData)
	}
}

func TestOpenExternalEditorRejectsOutsideProject(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	m.project.Root = root
	m.cfg.Editor = config.Editor{Command: "true"}
	updated, _, handled := m.handleSlashCommand("/edit ../outside.md")
	if !handled {
		t.Fatal("edit handled = false")
	}
	got := updated.(model)
	if got.status != "editor failed" {
		t.Fatalf("status = %q, want editor failed", got.status)
	}
}

func TestEditorArgsDefaultLineForm(t *testing.T) {
	got := editorArgs(nil, "README.md", "7")
	if len(got) != 2 || got[0] != "+7" || got[1] != "README.md" {
		t.Fatalf("editorArgs = %#v", got)
	}
}
