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

func TestAttachCommandAddsContextRangeAndAllowedPath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "one\ntwo\nthree\n")
	m := commandTestModel(t)
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.session.ProjectRoot = root
	updated, _, handled := m.handleSlashCommand("/plan draft Attach README")
	if !handled {
		t.Fatal("plan draft handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/attach README.md 2-3")
	if !handled {
		t.Fatal("attach handled = false")
	}
	got := updated.(model)
	if got.status != "file attached" {
		t.Fatalf("status = %q, want file attached", got.status)
	}
	plan, ok, err := got.store.LatestPlan(got.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	task := plan.Tasks[0]
	if len(task.ContextFiles) != 1 || task.ContextFiles[0] != "README.md#L2-L3" {
		t.Fatalf("context files = %#v", task.ContextFiles)
	}
	if len(task.AllowedPaths) != 1 || task.AllowedPaths[0] != "README.md" {
		t.Fatalf("allowed paths = %#v", task.AllowedPaths)
	}
	packet, err := got.buildWorkerPacket(task)
	if err != nil {
		t.Fatalf("buildWorkerPacket: %v", err)
	}
	if len(packet.ContextFiles) != 1 || packet.ContextFiles[0].Content != "two\nthree" {
		t.Fatalf("packet context = %#v", packet.ContextFiles)
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
