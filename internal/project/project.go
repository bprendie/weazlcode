package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Summary contains information about the detected project.
type Summary struct {
	Root          string `json:"root"`
	Name          string `json:"name"`
	IsGitRepo     bool   `json:"is_git_repo"`
	WeazlCodeDir  string `json:"weazlcode_dir"`
}

// Detect finds the project root and returns a summary.
// If dir is empty, uses the current working directory.
func Detect(dir string) (*Summary, error) {
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
		dir = cwd
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("get absolute path: %w", err)
	}

	// Try to find git root first
	gitRoot, isGit := findGitRoot(absDir)
	
	root := absDir
	if isGit {
		root = gitRoot
	}

	name := filepath.Base(root)
	weazlCodeDir := filepath.Join(root, ".weazlcode")

	return &Summary{
		Root:         root,
		Name:         name,
		IsGitRepo:    isGit,
		WeazlCodeDir: weazlCodeDir,
	}, nil
}

// findGitRoot walks up the directory tree to find the git repository root.
func findGitRoot(dir string) (string, bool) {
	current := dir
	for {
		gitDir := filepath.Join(current, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			return current, true
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root
			break
		}
		current = parent
	}
	return "", false
}

// EnsureWeazlCodeDir creates the .weazlcode directory if it doesn't exist.
func (s *Summary) EnsureWeazlCodeDir() error {
	if err := os.MkdirAll(s.WeazlCodeDir, 0755); err != nil {
		return fmt.Errorf("create .weazlcode directory: %w", err)
	}
	return nil
}

// PlansDir returns the path to the plans directory.
func (s *Summary) PlansDir() string {
	return filepath.Join(s.WeazlCodeDir, "plans")
}

// TasksDir returns the path to the tasks directory.
func (s *Summary) TasksDir() string {
	return filepath.Join(s.WeazlCodeDir, "tasks")
}

// RunsDir returns the path to the runs directory.
func (s *Summary) RunsDir() string {
	return filepath.Join(s.WeazlCodeDir, "runs")
}

// EnsureDirs creates all necessary subdirectories within .weazlcode.
func (s *Summary) EnsureDirs() error {
	dirs := []string{
		s.PlansDir(),
		s.TasksDir(),
		s.RunsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

// GetGitStatus returns the current git status if this is a git repository.
func (s *Summary) GetGitStatus() (string, error) {
	if !s.IsGitRepo {
		return "", fmt.Errorf("not a git repository")
	}

	cmd := exec.Command("git", "status", "--short")
	cmd.Dir = s.Root
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetGitBranch returns the current git branch if this is a git repository.
func (s *Summary) GetGitBranch() (string, error) {
	if !s.IsGitRepo {
		return "", fmt.Errorf("not a git repository")
	}

	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = s.Root
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git branch: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// IsPathInProject checks if a path is within the project root.
func (s *Summary) IsPathInProject(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	
	rel, err := filepath.Rel(s.Root, absPath)
	if err != nil {
		return false
	}
	
	// Path is outside if it starts with ".."
	return !strings.HasPrefix(rel, "..")
}
