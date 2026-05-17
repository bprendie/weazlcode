package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
)

const maxTaskBaselineBytes = 2 * 1024 * 1024

type taskBaselinePayload struct {
	Files     []taskBaselineFile `json:"files"`
	Truncated bool               `json:"truncated,omitempty"`
}

type taskBaselineFile struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Content string `json:"content,omitempty"`
}

func (m model) gitDiff() (string, error) {
	tool, ok := m.toolRegistry.Get("git_diff")
	if !ok {
		return "", fmt.Errorf("git_diff tool is not registered")
	}
	return tool.Execute(context.Background(), map[string]any{"cwd": m.project.Root})
}

func (m model) taskGitDiff(task coding.Task) (string, error) {
	allowed := taskAllowedPaths(task)
	if len(allowed) == 0 {
		return "", fmt.Errorf("task has no allowed paths")
	}
	tool, ok := m.toolRegistry.Get("git_diff")
	if !ok {
		return "", fmt.Errorf("git_diff tool is not registered")
	}
	var parts []string
	for _, path := range allowed {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		diff, err := tool.Execute(context.Background(), map[string]any{"cwd": m.project.Root, "path": path})
		if err != nil {
			return "", err
		}
		diff = strings.TrimSpace(diff)
		if diff != "" && !gitDiffOutputEmpty(diff) {
			parts = append(parts, diff)
			continue
		}
		untracked, err := m.untrackedFileDiff(path)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(untracked) != "" {
			parts = append(parts, untracked)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func (m model) recordTaskBaseline(task coding.Task) error {
	baseline, err := m.captureTaskBaseline(task)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(baseline)
	if err != nil {
		return err
	}
	message := fmt.Sprintf("Captured baseline for %d allowed file(s).", len(baseline.Files))
	if baseline.Truncated {
		message += " Baseline was truncated."
	}
	_, err = m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "task_baseline",
		Message: message,
		Payload: payload,
	})
	return err
}

func (m model) captureTaskBaseline(task coding.Task) (taskBaselinePayload, error) {
	var baseline taskBaselinePayload
	total := 0
	seen := map[string]bool{}
	for _, rawPath := range taskAllowedPaths(task) {
		path := strings.TrimSpace(filepath.ToSlash(rawPath))
		if path == "" || path == "." || strings.Contains(path, "\x00") || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") || seen[path] {
			continue
		}
		seen[path] = true
		fullPath := filepath.Join(m.project.Root, filepath.FromSlash(path))
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				baseline.Files = append(baseline.Files, taskBaselineFile{Path: path, Exists: false})
				continue
			}
			return baseline, err
		}
		if info.IsDir() {
			continue
		}
		if total+int(info.Size()) > maxTaskBaselineBytes {
			baseline.Truncated = true
			continue
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return baseline, err
		}
		total += len(content)
		baseline.Files = append(baseline.Files, taskBaselineFile{
			Path:    path,
			Exists:  true,
			Content: string(content),
		})
	}
	return baseline, nil
}

func (m model) taskReviewDiff(task coding.Task, events []coding.TaskEvent) (string, error) {
	if baseline, ok := latestTaskBaseline(events); ok && len(baseline.Files) > 0 {
		diff, err := m.taskBaselineDiff(task, baseline)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(diff) != "" {
			return diff, nil
		}
	}
	return m.taskGitDiff(task)
}

func latestTaskBaseline(events []coding.TaskEvent) (taskBaselinePayload, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "task_baseline" || len(events[i].Payload) == 0 {
			continue
		}
		var baseline taskBaselinePayload
		if err := json.Unmarshal(events[i].Payload, &baseline); err != nil {
			continue
		}
		return baseline, true
	}
	return taskBaselinePayload{}, false
}

func (m model) taskBaselineDiff(task coding.Task, baseline taskBaselinePayload) (string, error) {
	var parts []string
	for _, file := range baseline.Files {
		if err := coding.ValidatePatchPaths([]string{file.Path}, taskAllowedPaths(task), task.ForbiddenPaths); err != nil {
			return "", err
		}
		current, exists, err := m.readTaskFile(file.Path)
		if err != nil {
			return "", err
		}
		if file.Exists == exists && file.Content == current {
			continue
		}
		parts = append(parts, renderFullFileDiff(file.Path, file.Exists, file.Content, exists, current))
	}
	return strings.Join(parts, "\n\n"), nil
}

func (m model) restoreTaskBaseline(task coding.Task, events []coding.TaskEvent, reason string) error {
	baseline, ok := latestTaskBaseline(events)
	if !ok || len(baseline.Files) == 0 {
		return nil
	}
	var restored []string
	for _, file := range baseline.Files {
		if err := coding.ValidatePatchPaths([]string{file.Path}, taskAllowedPaths(task), task.ForbiddenPaths); err != nil {
			return err
		}
		fullPath := filepath.Join(m.project.Root, filepath.FromSlash(file.Path))
		if file.Exists {
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(fullPath, []byte(file.Content), 0o644); err != nil {
				return err
			}
		} else if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		restored = append(restored, file.Path)
	}
	payload, _ := json.Marshal(struct {
		Reason string   `json:"reason"`
		Paths  []string `json:"paths"`
	}{
		Reason: reason,
		Paths:  restored,
	})
	_, err := m.store.AddTaskEvent(coding.TaskEvent{
		TaskID:  task.ID,
		Type:    "output_cleanup",
		Message: fmt.Sprintf("Restored %d allowed path(s) to task baseline after %s.", len(restored), reason),
		Payload: payload,
	})
	return err
}

func (m model) readTaskFile(path string) (string, bool, error) {
	if strings.TrimSpace(path) == "" || strings.Contains(path, "\x00") || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
		return "", false, fmt.Errorf("invalid task path %q", path)
	}
	content, err := os.ReadFile(filepath.Join(m.project.Root, filepath.FromSlash(path)))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(content), true, nil
}

func renderFullFileDiff(path string, oldExists bool, oldContent string, newExists bool, newContent string) string {
	if diff := renderNoIndexDiff(path, oldExists, oldContent, newExists, newContent); strings.TrimSpace(diff) != "" {
		return diff
	}
	oldLines := splitDiffLines(oldContent)
	newLines := splitDiffLines(newContent)
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	if !oldExists && newExists {
		b.WriteString("new file mode 100644\n")
		b.WriteString("--- /dev/null\n")
		fmt.Fprintf(&b, "+++ b/%s\n", path)
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(newLines))
		for _, line := range newLines {
			fmt.Fprintf(&b, "+%s\n", line)
		}
		return strings.TrimRight(b.String(), "\n")
	}
	if oldExists && !newExists {
		b.WriteString("deleted file mode 100644\n")
		fmt.Fprintf(&b, "--- a/%s\n", path)
		b.WriteString("+++ /dev/null\n")
		fmt.Fprintf(&b, "@@ -1,%d +0,0 @@\n", len(oldLines))
		for _, line := range oldLines {
			fmt.Fprintf(&b, "-%s\n", line)
		}
		return strings.TrimRight(b.String(), "\n")
	}
	fmt.Fprintf(&b, "--- a/%s\n", path)
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines))
	for _, line := range oldLines {
		fmt.Fprintf(&b, "-%s\n", line)
	}
	for _, line := range newLines {
		fmt.Fprintf(&b, "+%s\n", line)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderNoIndexDiff(path string, oldExists bool, oldContent string, newExists bool, newContent string) string {
	dir, err := os.MkdirTemp("", "weazlcode-taskdiff-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	oldPath := filepath.Join(dir, "old")
	newPath := filepath.Join(dir, "new")
	if err := os.WriteFile(oldPath, []byte(oldContent), 0o600); err != nil {
		return ""
	}
	if err := os.WriteFile(newPath, []byte(newContent), 0o600); err != nil {
		return ""
	}
	cmd := exec.Command("git", "diff", "--no-index", "--", oldPath, newPath)
	out, err := cmd.CombinedOutput()
	if len(out) == 0 || err == nil {
		return ""
	}
	return normalizeNoIndexDiff(string(out), path, oldExists, newExists)
}

func normalizeNoIndexDiff(diff, path string, oldExists, newExists bool) string {
	var out []string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			out = append(out, fmt.Sprintf("diff --git a/%s b/%s", path, path))
		case strings.HasPrefix(line, "--- "):
			if oldExists {
				out = append(out, "--- a/"+path)
			} else {
				out = append(out, "--- /dev/null")
			}
		case strings.HasPrefix(line, "+++ "):
			if newExists {
				out = append(out, "+++ b/"+path)
			} else {
				out = append(out, "+++ /dev/null")
			}
		default:
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func splitDiffLines(content string) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func gitDiffOutputEmpty(diff string) bool {
	diff = strings.TrimSpace(diff)
	return strings.HasPrefix(diff, "$ git diff") && !strings.Contains(diff, "\ndiff --git ")
}

func (m model) untrackedFileDiff(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.Contains(path, "\x00") || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
		return "", nil
	}
	tracked := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
	tracked.Dir = m.project.Root
	if err := tracked.Run(); err == nil {
		return "", nil
	}
	fullPath := filepath.Join(m.project.Root, filepath.FromSlash(path))
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return "", nil
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	if strings.Contains(string(content), "\x00") {
		return fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\nBinary files /dev/null and b/%s differ", path, path, path), nil
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	b.WriteString("new file mode 100644\n")
	b.WriteString("index 0000000..0000000\n")
	b.WriteString("--- /dev/null\n")
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (m model) changedFiles() (string, error) {
	tool, ok := m.toolRegistry.Get("list_changed_files")
	if !ok {
		return "", fmt.Errorf("list_changed_files tool is not registered")
	}
	return tool.Execute(context.Background(), map[string]any{"cwd": m.project.Root})
}
