package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectServersFindsGo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\nfunc main() {}\n")
	manager := NewManager(root, nil)
	servers := manager.DetectServers()
	if len(servers) != 1 || servers[0].Language != "go" || servers[0].Command != "gopls" {
		t.Fatalf("servers = %#v", servers)
	}
}

func TestDiagnosticsReportsGoSyntaxError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "bad.go", "package main\nfunc broken( {\n")
	manager := NewManager(root, nil)
	diagnostics, err := manager.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(diagnostics) == 0 || diagnostics[0].File != "bad.go" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestSymbolsDefinitionReferences(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n\ntype App struct{}\nfunc Run() { _ = App{} }\n")
	manager := NewManager(root, nil)
	symbols, err := manager.Symbols(context.Background(), "Run")
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	if len(symbols) != 1 || symbols[0].Name != "Run" || symbols[0].Kind != "func" {
		t.Fatalf("symbols = %#v", symbols)
	}
	def, ok, err := manager.Definition(context.Background(), "App")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !ok || def.Kind != "type" {
		t.Fatalf("definition = %#v ok=%v", def, ok)
	}
	refs, err := manager.References(context.Background(), "App")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(refs) < 2 {
		t.Fatalf("refs = %#v", refs)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
