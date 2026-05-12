package coding

import (
	"os"
	"path/filepath"
	"reflect"
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
