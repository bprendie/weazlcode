package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitStatusTool(t *testing.T) {
	root := gitRepo(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewGitStatusTool(Limits{WorkspaceRoots: []string{root}})
	got, err := tool.Execute(context.Background(), map[string]any{"cwd": root})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(got, "main.go") {
		t.Fatalf("status = %q, want main.go", got)
	}
}

func TestReadFileRangeTool(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewReadFileRangeTool(Limits{WorkspaceRoots: []string{root}})
	got, err := tool.Execute(context.Background(), map[string]any{
		"path":       path,
		"start_line": float64(2),
		"line_count": float64(2),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "2: two\n3: three\n" {
		t.Fatalf("range = %q", got)
	}
}

func TestApplyPatchTool(t *testing.T) {
	root := gitRepo(t)
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "file.txt")
	runGitTest(t, root, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "initial")
	patch := strings.Join([]string{
		"diff --git a/file.txt b/file.txt",
		"index 3e75765..7c0b1bb 100644",
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"",
	}, "\n")
	tool := NewApplyPatchTool(Limits{WorkspaceRoots: []string{root}})
	if _, err := tool.Execute(context.Background(), map[string]any{"cwd": root, "patch": patch}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new\n" {
		t.Fatalf("file = %q, want new", got)
	}
}

func TestApplyPatchRejectsOutsidePath(t *testing.T) {
	root := gitRepo(t)
	tool := NewApplyPatchTool(Limits{WorkspaceRoots: []string{root}})
	patch := "--- a/../bad.txt\n+++ b/../bad.txt\n@@ -1 +1 @@\n-old\n+new\n"
	_, err := tool.Execute(context.Background(), map[string]any{"cwd": root, "patch": patch})
	if err == nil {
		t.Fatal("Execute returned nil error for outside path")
	}
}

func TestGitShowRejectsOptionLikeRevision(t *testing.T) {
	root := gitRepo(t)
	tool := NewGitShowTool(Limits{WorkspaceRoots: []string{root}})
	_, err := tool.Execute(context.Background(), map[string]any{"cwd": root, "rev": "--help"})
	if err == nil {
		t.Fatal("Execute returned nil error for option-like revision")
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitTest(t, root, "init")
	return root
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
