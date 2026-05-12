package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultMaxOutputChars = 12000

type Limits struct {
	WorkspaceRoots []string
	MaxOutputChars int
	MaxFileBytes   int64
}

func (l Limits) outputLimit() int {
	if l.MaxOutputChars <= 0 {
		return defaultMaxOutputChars
	}
	return l.MaxOutputChars
}

func (l Limits) fileLimit() int64 {
	if l.MaxFileBytes <= 0 {
		return 1024 * 1024
	}
	return l.MaxFileBytes
}

func (l Limits) Truncate(s string) string {
	limit := l.outputLimit()
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("\n\n[truncated: %d chars omitted]", len(s)-limit)
}

func (l Limits) CleanRoots() []string {
	roots := make([]string, 0, len(l.WorkspaceRoots))
	for _, root := range l.WorkspaceRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		roots = append(roots, abs)
	}
	return roots
}

func (l Limits) ResolveAllowed(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}

	for _, root := range l.CleanRoots() {
		rel, err := filepath.Rel(root, abs)
		if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
			return abs, nil
		}
		if abs == root {
			return abs, nil
		}
	}
	return "", fmt.Errorf("%s is outside configured workspace roots", path)
}

func (l Limits) ResolveCreateAllowed(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(abs)
	if real, err := filepath.EvalSymlinks(parent); err == nil {
		abs = filepath.Join(real, filepath.Base(abs))
	}

	for _, root := range l.CleanRoots() {
		rel, err := filepath.Rel(root, abs)
		if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
			return abs, nil
		}
	}
	return "", fmt.Errorf("%s is outside configured workspace roots", path)
}

func (l Limits) RequireRoots() error {
	if len(l.CleanRoots()) == 0 {
		return fmt.Errorf("no workspace_roots configured")
	}
	return nil
}
