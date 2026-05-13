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
	if !ok || !strings.Contains(loaded.Content, "go test ./...") || !strings.Contains(loaded.Content, "Worker Rules") || !strings.Contains(loaded.Content, "Reviewer Checklist") {
		t.Fatalf("instructions = %#v ok=%v", loaded, ok)
	}
}

func TestInitInstructionsForceOverwrites(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, PrimaryInstructionsFile)
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	summary := Summary{Root: root, Languages: []string{"go"}}
	if _, err := InitInstructionsWithOptions(root, summary, false); err != nil {
		t.Fatalf("InitInstructionsWithOptions false: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old\n" {
		t.Fatalf("file was overwritten without force: %q", data)
	}
	if _, err := InitInstructionsWithOptions(root, summary, true); err != nil {
		t.Fatalf("InitInstructionsWithOptions true: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "WeazlCode Project Instructions") {
		t.Fatalf("file was not regenerated: %q", data)
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

func TestDiscoverLayout(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	layout := strings.Join(DiscoverLayout(root), "\n")
	for _, want := range []string{"internal/", "README.md"} {
		if !strings.Contains(layout, want) {
			t.Fatalf("layout missing %q: %s", want, layout)
		}
	}
}
