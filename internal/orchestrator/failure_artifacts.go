package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
)

func (o *Orchestrator) saveFailureArtifacts(task *coding.Task, state *TaskState, taskErr error) {
	if state == nil || state.Result == nil {
		return
	}

	dir := filepath.Join(
		o.project.RunsDir(),
		"failures",
		fmt.Sprintf("%s-%s", task.ID, time.Now().Format("20060102-150405")),
	)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	_ = os.WriteFile(filepath.Join(dir, "error.txt"), []byte(taskErr.Error()), 0644)
	if state.Result.RawResponse != "" {
		_ = os.WriteFile(filepath.Join(dir, "raw_response.txt"), []byte(state.Result.RawResponse), 0644)
	}
	for path, content := range state.Result.Files {
		out := filepath.Join(dir, "files", filepath.Clean(path))
		if err := os.MkdirAll(filepath.Dir(out), 0755); err == nil {
			_ = os.WriteFile(out, []byte(content), 0644)
		}
	}
	for path, content := range state.Result.Patches {
		name := strings.ReplaceAll(filepath.Clean(path), string(filepath.Separator), "__")
		_ = os.WriteFile(filepath.Join(dir, name+".patch"), []byte(content), 0644)
	}
}
