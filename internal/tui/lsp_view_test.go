package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/coding"
)

func TestLSPCommands(t *testing.T) {
	root := t.TempDir()
	writeLSPTestFile(t, root, "main.go", "package main\n\ntype App struct{}\nfunc Run() { _ = App{} }\n")
	m := commandTestModel(t)
	m.project.Root = root
	m.project.Languages = []string{"go"}
	for _, tc := range []struct {
		command string
		want    string
	}{
		{"/lsp", "gopls"},
		{"/diagnostics", "No diagnostics"},
		{"/symbols App", "type App"},
		{"/definition Run", "func Run"},
		{"/references App", "References:"},
	} {
		updated, _, handled := m.handleSlashCommand(tc.command)
		if !handled {
			t.Fatalf("%s handled = false", tc.command)
		}
		got := updated.(model)
		if !strings.Contains(got.viewport.View(), tc.want) {
			t.Fatalf("%s missing %q:\n%s", tc.command, tc.want, got.viewport.View())
		}
		m = got
	}
}

func TestWorkerPacketIncludesDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeLSPTestFile(t, root, "bad.go", "package main\nfunc broken( {\n")
	m := commandTestModel(t)
	m.project.Root = root
	task := coding.Task{ID: "task-1", PlanID: "plan-1", Title: "Fix syntax", Goal: "Fix syntax", Status: coding.TaskStatusPending}
	packet, err := m.buildWorkerPacket(task)
	if err != nil {
		t.Fatalf("buildWorkerPacket: %v", err)
	}
	if len(packet.Diagnostics) == 0 || packet.Diagnostics[0].File != "bad.go" {
		t.Fatalf("diagnostics = %#v", packet.Diagnostics)
	}
}

func writeLSPTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
