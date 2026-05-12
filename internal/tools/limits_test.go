package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLimitsResolveAllowed(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "note.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	limits := Limits{WorkspaceRoots: []string{root}}
	got, err := limits.ResolveAllowed(file)
	if err != nil {
		t.Fatalf("ResolveAllowed returned error: %v", err)
	}
	if got != file {
		t.Fatalf("ResolveAllowed = %q, want %q", got, file)
	}
}

func TestLimitsRejectsOutsideRoot(t *testing.T) {
	limits := Limits{WorkspaceRoots: []string{t.TempDir()}}
	_, err := limits.ResolveAllowed("/tmp/not-inside-root")
	if err == nil {
		t.Fatal("ResolveAllowed returned nil error for outside path")
	}
}

func TestLimitsResolveCreateAllowedForNewFile(t *testing.T) {
	root := t.TempDir()
	limits := Limits{WorkspaceRoots: []string{root}}
	path := filepath.Join(root, "new.md")
	got, err := limits.ResolveCreateAllowed(path)
	if err != nil {
		t.Fatalf("ResolveCreateAllowed returned error: %v", err)
	}
	if got != path {
		t.Fatalf("ResolveCreateAllowed = %q, want %q", got, path)
	}
}

func TestLimitsTruncate(t *testing.T) {
	got := Limits{MaxOutputChars: 5}.Truncate("hello world")
	if !strings.Contains(got, "truncated") {
		t.Fatalf("Truncate = %q, want truncation marker", got)
	}
}

func TestValidateReadOnlyCommand(t *testing.T) {
	if err := validateReadOnlyCommand("git", []string{"status"}); err != nil {
		t.Fatalf("git status rejected: %v", err)
	}
	if err := validateReadOnlyCommand("git", []string{"commit"}); err == nil {
		t.Fatal("git commit was accepted")
	}
}

func TestValidateReadOnlySQL(t *testing.T) {
	if err := validateReadOnlySQL("select * from messages"); err != nil {
		t.Fatalf("select rejected: %v", err)
	}
	if err := validateReadOnlySQL("delete from messages"); err == nil {
		t.Fatal("delete was accepted")
	}
	if err := validateReadOnlySQL("select 1; select 2"); err == nil {
		t.Fatal("multiple statements were accepted")
	}
}

func TestReadableTextExtractsHTML(t *testing.T) {
	html := `<html><head><title>Hello</title><style>.x{}</style></head><body><p>One &amp; two</p><script>x()</script></body></html>`
	got := readableText(html, "text/html")
	if !strings.Contains(got, "Title: Hello") || !strings.Contains(got, "One & two") || strings.Contains(got, "x()") {
		t.Fatalf("readableText = %q", got)
	}
}

func TestCreateFileToolCreatesNewFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "answer.md")
	tool := NewCreateFileTool(Limits{WorkspaceRoots: []string{root}, MaxFileBytes: 1024})
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "# Answer\n",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result, "Created") {
		t.Fatalf("result = %q, want Created", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "# Answer\n" {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestCreateFileToolRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "answer.md")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	tool := NewCreateFileTool(Limits{WorkspaceRoots: []string{root}, MaxFileBytes: 1024})
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "new",
	})
	if err == nil {
		t.Fatal("Execute returned nil error for existing file")
	}
}
