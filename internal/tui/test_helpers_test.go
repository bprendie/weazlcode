package tui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/storage"
	"github.com/bprendie/weazlcode/internal/tools"
)

func commandTestModelWithReviewingTask(t *testing.T) (model, string) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runTestGit(t, root, "init")
	runTestGit(t, root, "add", "README.md")
	m := commandTestModel(t)
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	rawPlan := `{"title":"Patch README","summary":"Apply worker patch","tasks":[{"title":"Update README","goal":"Change README text","allowed_paths":["README.md"],"verification":["python -m compileall ."],"acceptance_checks":[{"description":"README changed"}]}]}`
	updated, _, handled := m.handleSlashCommand("/plan import " + rawPlan)
	if !handled {
		t.Fatal("plan import handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/approve")
	if !handled {
		t.Fatal("approve handled = false")
	}
	m = updated.(model)
	updated, _, handled = m.handleSlashCommand("/run-task")
	if !handled {
		t.Fatal("run-task handled = false")
	}
	m = updated.(model)
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("plan not found")
	}
	patch := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old
+new
`
	rawPatch, err := json.Marshal(coding.WorkerPatch{TaskID: plan.Tasks[0].ID, Summary: "Updated README", Patch: patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	updated, _, handled = m.handleSlashCommand("/worker-patch " + string(rawPatch))
	if !handled {
		t.Fatal("worker-patch handled = false")
	}
	return updated.(model), root
}

func runTestGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runArtifactKinds(t *testing.T, dir string) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	kinds := map[string]bool{}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		var artifact struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(data, &artifact); err != nil {
			t.Fatalf("Unmarshal %s: %v", path, err)
		}
		kinds[artifact.Kind] = true
	}
	return kinds
}

func commandTestModel(t ...*testing.T) model {
	cfg := config.Default()
	registry := tools.NewRegistry()
	registry.Register(tools.NewCalculatorTool())
	registry.Register(tools.NewRunVerificationCommandTool(tools.Limits{WorkspaceRoots: []string{"/tmp"}}))
	registry.Register(tools.NewGitDiffTool(tools.Limits{WorkspaceRoots: []string{"/tmp"}}))
	registry.Register(tools.NewListChangedFilesTool(tools.Limits{WorkspaceRoots: []string{"/tmp"}}))
	ti := textinput.New()
	ti.Focus()
	var store *storage.Store
	session := storage.Session{ID: "session-1", Title: "New session", Provider: "local-vllm", Model: "local-model", ProjectRoot: "/tmp/weazlcode"}
	if len(t) > 0 && t[0] != nil {
		var err error
		store, err = storage.Open(filepath.Join(t[0].TempDir(), "test.sqlite3"))
		if err != nil {
			t[0].Fatal(err)
		}
		t[0].Cleanup(func() { _ = store.Close() })
		if err := store.Migrate(); err != nil {
			t[0].Fatal(err)
		}
		if err := store.CreateProjectSession(session.ID, session.Title, session.Provider, session.Model, session.ProjectRoot); err != nil {
			t[0].Fatal(err)
		}
	}
	return model{
		cfg:          cfg,
		project:      project.Summary{Root: "/tmp/weazlcode", GitRoot: true, Branch: "main", Dirty: true, StateDir: "/tmp/weazlcode/.weazlcode", LogDir: "/tmp/weazlcode/.weazlcode/logs", Languages: []string{"go"}, FileCount: 42},
		store:        store,
		toolRegistry: registry,
		styles:       newStyles(),
		mode:         modeChat,
		input:        ti,
		viewport:     viewport.New(80, 20),
		mouseScroll:  true,
		session:      session,
	}
}
