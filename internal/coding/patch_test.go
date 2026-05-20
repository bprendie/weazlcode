package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPatchPaths(t *testing.T) {
	patch := `diff --git a/internal/coding/a.go b/internal/coding/a.go
--- a/internal/coding/a.go
+++ b/internal/coding/a.go
@@ -1 +1 @@
-old
+new
diff --git a/README.md b/README.md
--- /dev/null
+++ b/README.md
@@ -0,0 +1 @@
+hello
`
	got := PatchPaths(patch)
	want := []string{"internal/coding/a.go", "README.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PatchPaths = %#v, want %#v", got, want)
	}
}

func TestPatchPathsTrimsMnemonicDiffPrefixes(t *testing.T) {
	patch := `diff --git i/README.md w/README.md
--- i/README.md
+++ w/README.md
@@ -1 +1 @@
-old
+new
`
	got := PatchPaths(patch)
	want := []string{"README.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PatchPaths = %#v, want %#v", got, want)
	}
}

func TestValidatePatchPathsAllowedForbidden(t *testing.T) {
	if err := ValidatePatchPaths([]string{"internal/coding/a.go"}, []string{"internal"}, []string{"internal/secrets"}); err != nil {
		t.Fatalf("ValidatePatchPaths allowed: %v", err)
	}
	if err := ValidatePatchPaths([]string{"README.md"}, []string{"internal"}, nil); err == nil {
		t.Fatal("ValidatePatchPaths allowed README.md")
	}
	if err := ValidatePatchPaths([]string{"internal/secrets/key.txt"}, []string{"internal"}, []string{"internal/secrets"}); err == nil {
		t.Fatal("ValidatePatchPaths allowed forbidden path")
	}
	if err := ValidatePatchPaths([]string{"README.md"}, nil, nil); err == nil {
		t.Fatal("ValidatePatchPaths allowed missing allowed paths")
	}
	if err := ValidatePatchPaths([]string{"../outside.go"}, []string{"."}, nil); err == nil {
		t.Fatal("ValidatePatchPaths allowed path traversal")
	}
}

func TestApplyPatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	patch := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old
+new
`
	result, err := ApplyPatch(root, patch)
	if err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "README.md" {
		t.Fatalf("paths = %#v", result.Paths)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("file content = %q, want new", data)
	}
}

func TestApplyPatchRecountsBadHunkLineCounts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	patch := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1,99 +1,99 @@
-old
+new
`
	if _, err := ApplyPatch(root, patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("file content = %q, want new", data)
	}
}

func TestApplyFileEdits(t *testing.T) {
	root := t.TempDir()
	result, err := ApplyFileEdits(root, []WorkerFileEdit{{Path: "docs/README.md", Content: "new\n"}})
	if err != nil {
		t.Fatalf("ApplyFileEdits: %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "docs/README.md" {
		t.Fatalf("paths = %#v", result.Paths)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "README.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("file content = %q, want new", data)
	}
	if _, err := ApplyFileEdits(root, []WorkerFileEdit{{Path: "../outside.md", Content: "bad"}}); err == nil {
		t.Fatal("ApplyFileEdits allowed path traversal")
	}
}

func TestDetectSuspiciousFileRewrites(t *testing.T) {
	root := t.TempDir()
	var oldContent string
	for i := 0; i < 100; i++ {
		oldContent += fmt.Sprintf("line %03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(root, "large.md"), []byte(oldContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rewrites := DetectSuspiciousFileRewrites(root, []WorkerFileEdit{{Path: "large.md", Content: "# replacement\n"}})
	if len(rewrites) != 1 || rewrites[0].Path != "large.md" {
		t.Fatalf("rewrites = %#v, want one large.md rewrite", rewrites)
	}

	rewrites = DetectSuspiciousFileRewrites(root, []WorkerFileEdit{{Path: "large.md", Content: oldContent + "new line\n"}})
	if len(rewrites) != 0 {
		t.Fatalf("rewrites = %#v, want none for additive edit", rewrites)
	}
}

func TestDetectSuspiciousFileRewritesRejectsPlaceholders(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>real content</main>\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rewrites := DetectSuspiciousFileRewrites(root, []WorkerFileEdit{{
		Path: "index.html",
		Content: `<!-- Existing content of index.html -->
<section id="features"></section>
<!-- Rest of the file content -->`,
	}})
	if len(rewrites) != 1 || !strings.Contains(rewrites[0].Reason, "placeholder sentinel") {
		t.Fatalf("rewrites = %#v, want placeholder rejection", rewrites)
	}

	rewrites = DetectSuspiciousFileRewrites(root, []WorkerFileEdit{{
		Path:    "new.css",
		Content: "/* Existing styles omitted for brevity */\n.feature-grid { display: grid; }\n",
	}})
	if len(rewrites) != 1 || rewrites[0].Path != "new.css" {
		t.Fatalf("rewrites = %#v, want new.css placeholder rejection", rewrites)
	}

	rewrites = DetectSuspiciousFileRewrites(root, []WorkerFileEdit{{
		Path:    "component.html",
		Content: "<article>\n+</article>\n",
	}})
	if len(rewrites) != 1 || !strings.Contains(rewrites[0].Reason, "diff marker residue") {
		t.Fatalf("rewrites = %#v, want diff marker residue rejection", rewrites)
	}
}
