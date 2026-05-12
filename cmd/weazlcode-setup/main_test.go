package main

import (
	"bufio"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/config"
)

func TestConfigureToolsKeepsExistingKeysOnBlank(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n\n\n\n"))
	cfg := config.Config{
		Tools: config.Tools{
			Enabled:         true,
			AutoExecute:     true,
			AlphaVantageKey: "alpha",
			BraveAPIKey:     "brave",
			WorkspaceRoots:  []string{"/tmp/work"},
		},
	}

	got := configureTools(reader, cfg)

	if got.Tools.AlphaVantageKey != "alpha" {
		t.Fatalf("AlphaVantageKey = %q, want existing key", got.Tools.AlphaVantageKey)
	}
	if got.Tools.BraveAPIKey != "brave" {
		t.Fatalf("BraveAPIKey = %q, want existing key", got.Tools.BraveAPIKey)
	}
	if len(got.Tools.WorkspaceRoots) != 1 || got.Tools.WorkspaceRoots[0] != "/tmp/work" {
		t.Fatalf("WorkspaceRoots = %#v, want existing roots", got.Tools.WorkspaceRoots)
	}
	if !got.Tools.Enabled {
		t.Fatal("Tools.Enabled = false, want true")
	}
}

func TestAskContextWindowPresets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "blank defaults large", input: "\n", want: 32768},
		{name: "small name", input: "small\n", want: 8192},
		{name: "medium number", input: "2\n", want: 16384},
		{name: "xl name", input: "xl\n", want: 128000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			if got := askContextWindow(reader); got != tt.want {
				t.Fatalf("askContextWindow() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWriteConfigStoresContextWindow(t *testing.T) {
	t.Setenv("WEAZLCODE_DATA", t.TempDir())
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "weazlcode.sqlite3")
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	if err := writeConfig(cfgPath, cfg, "vllm", "http://localhost:8000", "model", 16384); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	got, err := config.LoadPath(cfgPath)
	if err != nil {
		t.Fatalf("LoadPath: %v", err)
	}
	if got.Active().ContextWindow != 16384 {
		t.Fatalf("ContextWindow = %d, want 16384", got.Active().ContextWindow)
	}
}

func TestConfigureToolsClearsKeysWithDash(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("-\n-\n-\n2\n"))
	cfg := config.Config{
		Tools: config.Tools{
			Enabled:         true,
			AutoExecute:     true,
			AlphaVantageKey: "alpha",
			BraveAPIKey:     "brave",
			WorkspaceRoots:  []string{"/tmp/work"},
		},
	}

	got := configureTools(reader, cfg)

	if got.Tools.AlphaVantageKey != "" {
		t.Fatalf("AlphaVantageKey = %q, want empty", got.Tools.AlphaVantageKey)
	}
	if got.Tools.BraveAPIKey != "" {
		t.Fatalf("BraveAPIKey = %q, want empty", got.Tools.BraveAPIKey)
	}
	if len(got.Tools.WorkspaceRoots) != 0 {
		t.Fatalf("WorkspaceRoots = %#v, want empty", got.Tools.WorkspaceRoots)
	}
	if got.Tools.Enabled {
		t.Fatal("Tools.Enabled = true, want false")
	}
}
