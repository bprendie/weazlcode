package coding

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type PatchApplyResult struct {
	Paths  []string `json:"paths"`
	Output string   `json:"output"`
}

type SuspiciousRewrite struct {
	Path          string `json:"path"`
	OldLineCount  int    `json:"old_line_count"`
	NewLineCount  int    `json:"new_line_count"`
	CommonLinePct int    `json:"common_line_pct"`
	Reason        string `json:"reason"`
}

func PatchPaths(patch string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "+++ ") && !strings.HasPrefix(line, "--- ") {
			continue
		}
		p := strings.TrimSpace(line[4:])
		if p == "/dev/null" {
			continue
		}
		if i := strings.IndexAny(p, "\t "); i >= 0 {
			p = p[:i]
		}
		p = trimDiffPrefix(p)
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths
}

func trimDiffPrefix(path string) string {
	for _, prefix := range []string{"a/", "b/", "i/", "w/", "c/", "o/"} {
		if strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func WorkerFileEditPaths(files []WorkerFileEdit) []string {
	seen := map[string]bool{}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func ValidatePatchPaths(paths, allowed, forbidden []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("patch does not contain file paths")
	}
	cleanAllowed, err := cleanPathSet(allowed)
	if err != nil {
		return fmt.Errorf("allowed path: %w", err)
	}
	if len(cleanAllowed) == 0 {
		return fmt.Errorf("allowed paths are required")
	}
	cleanForbidden, err := cleanPathSet(forbidden)
	if err != nil {
		return fmt.Errorf("forbidden path: %w", err)
	}
	for _, raw := range paths {
		path, err := cleanRelativePath(raw)
		if err != nil {
			return err
		}
		for _, forbiddenPath := range cleanForbidden {
			if pathWithin(path, forbiddenPath) {
				return fmt.Errorf("patch path %q is forbidden by %q", path, forbiddenPath)
			}
		}
		allowedMatch := false
		for _, allowedPath := range cleanAllowed {
			if pathWithin(path, allowedPath) {
				allowedMatch = true
				break
			}
		}
		if !allowedMatch {
			return fmt.Errorf("patch path %q is outside allowed paths", path)
		}
	}
	return nil
}

func ApplyFileEdits(projectRoot string, files []WorkerFileEdit) (PatchApplyResult, error) {
	paths := WorkerFileEditPaths(files)
	if len(paths) == 0 {
		return PatchApplyResult{}, fmt.Errorf("file edits do not contain paths")
	}
	if strings.TrimSpace(projectRoot) == "" {
		return PatchApplyResult{}, fmt.Errorf("project root is required")
	}
	for _, file := range files {
		path, err := cleanRelativePath(file.Path)
		if err != nil {
			return PatchApplyResult{}, err
		}
		full := filepath.Join(projectRoot, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return PatchApplyResult{}, err
		}
		if err := os.WriteFile(full, []byte(file.Content), 0o644); err != nil {
			return PatchApplyResult{}, err
		}
	}
	return PatchApplyResult{Paths: paths, Output: "File edits applied."}, nil
}

func DetectSuspiciousFileRewrites(projectRoot string, files []WorkerFileEdit) []SuspiciousRewrite {
	var rewrites []SuspiciousRewrite
	for _, file := range files {
		path, err := cleanRelativePath(file.Path)
		if err != nil {
			continue
		}
		newLines := significantLines(file.Content)
		oldData, err := os.ReadFile(filepath.Join(projectRoot, path))
		if placeholder, ok := placeholderRewriteReason(file.Content); ok {
			rewrite := SuspiciousRewrite{
				Path:         path,
				NewLineCount: len(newLines),
				Reason:       placeholder,
			}
			if err == nil {
				oldLines := significantLines(string(oldData))
				rewrite.OldLineCount = len(oldLines)
				rewrite.CommonLinePct = commonLinePercentage(oldLines, newLines)
			}
			rewrites = append(rewrites, rewrite)
			continue
		}
		if err != nil {
			continue
		}
		oldLines := significantLines(string(oldData))
		if len(oldLines) < 80 {
			continue
		}
		commonPct := commonLinePercentage(oldLines, newLines)
		if len(newLines) < len(oldLines)/2 || commonPct < 25 {
			rewrites = append(rewrites, SuspiciousRewrite{
				Path:          path,
				OldLineCount:  len(oldLines),
				NewLineCount:  len(newLines),
				CommonLinePct: commonPct,
				Reason:        "large existing file was replaced with substantially different full-file content",
			})
		}
	}
	return rewrites
}

func placeholderRewriteReason(content string) (string, bool) {
	lower := strings.ToLower(content)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "+<") || strings.HasPrefix(line, "-<") {
			return "file edit contains diff marker residue before markup", true
		}
	}
	sentinels := []string{
		"existing content of",
		"existing content omitted",
		"rest of the file",
		"existing styles omitted",
		"styles omitted for brevity",
		"omitted for brevity",
		"previous content here",
		"remaining content unchanged",
	}
	for _, sentinel := range sentinels {
		if strings.Contains(lower, sentinel) {
			return fmt.Sprintf("file edit contains placeholder sentinel %q instead of real content", sentinel), true
		}
	}
	return "", false
}

func ApplyPatch(projectRoot, patch string) (PatchApplyResult, error) {
	paths := PatchPaths(patch)
	if len(paths) == 0 {
		return PatchApplyResult{}, fmt.Errorf("patch does not contain file paths")
	}
	if strings.TrimSpace(projectRoot) == "" {
		return PatchApplyResult{}, fmt.Errorf("project root is required")
	}
	args := []string{"apply"}
	if _, err := runGitApply(projectRoot, []string{"apply", "--check"}, patch); err != nil {
		if _, recountErr := runGitApply(projectRoot, []string{"apply", "--check", "--recount"}, patch); recountErr != nil {
			return PatchApplyResult{}, err
		}
		args = []string{"apply", "--recount"}
	}
	out, err := runGitApply(projectRoot, args, patch)
	if err != nil {
		return PatchApplyResult{}, err
	}
	if strings.TrimSpace(out) == "" {
		out = "Patch applied."
	}
	return PatchApplyResult{Paths: paths, Output: out}, nil
}

func significantLines(content string) []string {
	raw := strings.Split(content, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func commonLinePercentage(oldLines, newLines []string) int {
	if len(oldLines) == 0 {
		return 100
	}
	newSet := map[string]int{}
	for _, line := range newLines {
		newSet[line]++
	}
	common := 0
	for _, line := range oldLines {
		if newSet[line] > 0 {
			common++
			newSet[line]--
		}
	}
	return common * 100 / len(oldLines)
}

func cleanPathSet(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		clean, err := cleanRelativePath(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, clean)
	}
	return out, nil
}

func cleanRelativePath(path string) (string, error) {
	if strings.Contains(path, "\x00") {
		return "", fmt.Errorf("path %q contains a null byte", path)
	}
	path = filepath.ToSlash(strings.TrimSpace(path))
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path %q must be relative to project root", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q must stay inside project root", path)
	}
	if clean == "" {
		clean = "."
	}
	return clean, nil
}

func pathWithin(path, root string) bool {
	if root == "." {
		return true
	}
	return path == root || strings.HasPrefix(path, root+"/")
}

func runGitApply(projectRoot string, args []string, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("%s", text)
	}
	return text, nil
}
