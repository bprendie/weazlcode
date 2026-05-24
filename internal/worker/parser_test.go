package worker

import (
	"testing"

	"github.com/bprendie/weazlcode/internal/coding"
)

func TestParseTaskResponseUnwrapsFencedFileBlock(t *testing.T) {
	task := &coding.Task{OutputFiles: []string{"app.py"}}
	response := "FILE: app.py\n---\n```python\nimport sys\nprint(sys.version)\n```\n---"

	result, err := parseTaskResponse(response, task)
	if err != nil {
		t.Fatalf("parseTaskResponse returned error: %v", err)
	}
	if got := result.Files["app.py"]; got != "import sys\nprint(sys.version)" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestParseTaskResponseRejectsPythonNarration(t *testing.T) {
	task := &coding.Task{OutputFiles: []string{"app.py"}}
	response := "FILE: app.py\n---\n# Assuming the original file is correct\n```python\nimport sys\n```\n---"

	if _, err := parseTaskResponse(response, task); err == nil {
		t.Fatal("expected parser to reject narrated Python artifact")
	}
}
