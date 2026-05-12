package coding

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type PatchApplyResult struct {
	Paths  []string `json:"paths"`
	Output string   `json:"output"`
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
		p = strings.TrimPrefix(p, "a/")
		p = strings.TrimPrefix(p, "b/")
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
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
		cleanAllowed = []string{"."}
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

func ApplyPatch(projectRoot, patch string) (PatchApplyResult, error) {
	paths := PatchPaths(patch)
	if len(paths) == 0 {
		return PatchApplyResult{}, fmt.Errorf("patch does not contain file paths")
	}
	if strings.TrimSpace(projectRoot) == "" {
		return PatchApplyResult{}, fmt.Errorf("project root is required")
	}
	if _, err := runGitApply(projectRoot, []string{"apply", "--check"}, patch); err != nil {
		return PatchApplyResult{}, err
	}
	out, err := runGitApply(projectRoot, []string{"apply"}, patch)
	if err != nil {
		return PatchApplyResult{}, err
	}
	if strings.TrimSpace(out) == "" {
		out = "Patch applied."
	}
	return PatchApplyResult{Paths: paths, Output: out}, nil
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
