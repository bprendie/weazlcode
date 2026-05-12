package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectFallsBackToDirectoryWithoutGit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if got.Root != root {
		t.Fatalf("Root = %q, want %q", got.Root, root)
	}
	if got.GitRoot {
		t.Fatal("GitRoot = true, want false")
	}
	if got.StateDir != filepath.Join(root, ".weazlcode") {
		t.Fatalf("StateDir = %q", got.StateDir)
	}
	if _, err := os.Stat(got.LogDir); err != nil {
		t.Fatalf("LogDir was not created: %v", err)
	}
	if len(got.Languages) != 1 || got.Languages[0] != "go" {
		t.Fatalf("Languages = %#v, want go", got.Languages)
	}
}

func TestDetectUsesGitRoot(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "checkout", "-b", "main")
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "app.py"), []byte("print('hi')\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Detect(filepath.Join(root, "sub"))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if got.Root != root {
		t.Fatalf("Root = %q, want %q", got.Root, root)
	}
	if !got.GitRoot {
		t.Fatal("GitRoot = false, want true")
	}
	if got.Branch != "main" {
		t.Fatalf("Branch = %q, want main", got.Branch)
	}
	if !got.Dirty {
		t.Fatal("Dirty = false, want true for untracked file")
	}
}

func TestDetectHonorsWeazlcodeIgnore(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".weazlcodeignore": "ignored/\n*.py\n",
		"main.go":          "package main\n",
		"skip.py":          "print('skip')\n",
		"ignored/app.rs":   "fn main() {}\n",
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if got.IgnoreFile == "" {
		t.Fatal("IgnoreFile is empty")
	}
	if len(got.Languages) != 1 || got.Languages[0] != "go" {
		t.Fatalf("Languages = %#v, want only go", got.Languages)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
