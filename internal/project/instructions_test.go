package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitInstructionsWritesWEAZLCODE(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	summary := Summary{Root: root, Languages: []string{"go"}, GitRoot: true, Branch: "main"}
	path, err := InitInstructions(root, summary)
	if err != nil {
		t.Fatalf("InitInstructions: %v", err)
	}
	if filepath.Base(path) != PrimaryInstructionsFile {
		t.Fatalf("path = %q", path)
	}
	loaded, ok, err := LoadInstructions(root)
	if err != nil {
		t.Fatalf("LoadInstructions: %v", err)
	}
	if !ok || !strings.Contains(loaded.Content, "go test ./...") || !strings.Contains(loaded.Content, "Worker Rules") {
		t.Fatalf("instructions = %#v ok=%v", loaded, ok)
	}
}

func TestDiscoverCommands(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"go.mod", "package.json", "Cargo.toml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	commands := strings.Join(DiscoverCommands(root), "\n")
	for _, want := range []string{"go test ./...", "npm test", "cargo check"} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q: %s", want, commands)
		}
	}
}
