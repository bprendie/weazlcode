package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectInstructionsAndMemoryCommands(t *testing.T) {
	m := commandTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WEAZLCODE.md"), []byte("# Rules\n\nRun tests.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.session.ProjectRoot = root
	updated, _, handled := m.handleSlashCommand("/memory test=use focused packets")
	if !handled {
		t.Fatal("memory handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/instructions")
	if !handled {
		t.Fatal("instructions handled = false")
	}
	got := updated.(model)
	view := got.viewport.View()
	if !strings.Contains(view, "WEAZLCODE.md") || !strings.Contains(view, "use focused packets") {
		t.Fatalf("instructions view = %q", view)
	}
}

func TestFinalReviewCommitMessageAndExport(t *testing.T) {
	m, root := commandTestModelWithReviewingTask(t)
	updated, _, handled := m.handleSlashCommand(`/review {"verdict":"approve","summary":"Looks good"}`)
	if !handled {
		t.Fatal("review handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/final-review")
	if !handled {
		t.Fatal("final-review handled = false")
	}
	if !strings.Contains(updated.(model).viewport.View(), "Rollback guidance") {
		t.Fatalf("final review = %q", updated.(model).viewport.View())
	}
	updated, _, handled = m.handleSlashCommand("/commit-message")
	if !handled {
		t.Fatal("commit-message handled = false")
	}
	if !strings.Contains(updated.(model).viewport.View(), "Complete Patch README") {
		t.Fatalf("commit message = %q", updated.(model).viewport.View())
	}
	updated, _, handled = m.handleSlashCommand("/export-run")
	if !handled {
		t.Fatal("export-run handled = false")
	}
	if !strings.Contains(updated.(model).viewport.View(), filepath.Join(root, ".weazlcode", "runs")) {
		t.Fatalf("export view = %q", updated.(model).viewport.View())
	}
}
