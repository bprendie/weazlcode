package tui

import (
	"strings"
	"testing"
)

func TestRenderDiffViewGroupsFilesAndHunks(t *testing.T) {
	raw := `diff --git i/README.md w/README.md
index 3367afd..3e75765 100644
--- i/README.md
+++ w/README.md
@@ -1 +1 @@
-old
+new
diff --git i/internal/a.go w/internal/a.go
new file mode 100644
--- /dev/null
+++ w/internal/a.go
@@ -0,0 +1,2 @@
+package internal
+`
	view := renderDiffView(raw)
	for _, want := range []string{
		"Diff: 2 file(s)",
		"1. README.md  +1 -1",
		"2. internal/a.go  +2 -0",
		"--- README.md  +1 -1",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"new file mode 100644",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderDiffView missing %q:\n%s", want, view)
		}
	}
}

func TestRenderDiffViewNoChanges(t *testing.T) {
	got := renderDiffView("")
	if got != "Diff:\nNo changes." {
		t.Fatalf("renderDiffView empty = %q", got)
	}
}
