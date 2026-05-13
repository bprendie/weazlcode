package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesCommandTextFiltersAndPreviews(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "# WeazlCode\n")
	writeTestFile(t, root, "internal/tui/files_view.go", "package tui\n")
	writeTestFile(t, root, ".weazlcode/logs/tool_calls.jsonl", "{}\n")
	m := commandTestModel(t)
	m.project.Root = root
	text := m.filesCommandText("fvg")
	if !strings.Contains(text, "files_view.go") || !strings.Contains(text, "Preview: internal/tui/files_view.go") {
		t.Fatalf("files view missing match/preview:\n%s", text)
	}
	if strings.Contains(text, "tool_calls.jsonl") {
		t.Fatalf("files view included .weazlcode state:\n%s", text)
	}
}

func TestPreviewCommandText(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "one\ntwo\n")
	m := commandTestModel(t)
	m.project.Root = root
	text := m.previewCommandText("README.md")
	if !strings.Contains(text, "Preview: README.md") || !strings.Contains(text, "1  one") || !strings.Contains(text, "2  two") {
		t.Fatalf("preview = %q", text)
	}
}

func TestPreviewCommandRejectsTraversal(t *testing.T) {
	m := commandTestModel(t)
	text := m.previewCommandText("../secret")
	if !strings.Contains(text, "path must stay inside project root") {
		t.Fatalf("preview traversal = %q", text)
	}
}

func writeTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
