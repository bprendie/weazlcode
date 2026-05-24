package patcher

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/validation"
)

// Patcher applies file changes and patches with safety guardrails.
type Patcher struct {
	project   *project.Summary
	validator *validation.Validator
	dryRun    bool
}

// Snapshot captures current output-file contents so a failed task can roll back.
func (p *Patcher) Snapshot(task *coding.Task) (map[string]*string, error) {
	snapshot := make(map[string]*string)
	for _, path := range task.OutputFiles {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("get absolute path: %w", err)
		}
		if !p.project.IsPathInProject(absPath) {
			return nil, fmt.Errorf("path %s is outside project root", path)
		}

		content, err := os.ReadFile(absPath)
		if os.IsNotExist(err) {
			snapshot[path] = nil
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read snapshot file %s: %w", path, err)
		}
		text := string(content)
		snapshot[path] = &text
	}
	return snapshot, nil
}

// RestoreSnapshot restores files captured by Snapshot.
func (p *Patcher) RestoreSnapshot(snapshot map[string]*string) error {
	for path, content := range snapshot {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("get absolute path: %w", err)
		}
		if !p.project.IsPathInProject(absPath) {
			return fmt.Errorf("path %s is outside project root", path)
		}
		if content == nil {
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove new file %s: %w", path, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return fmt.Errorf("create restore directory: %w", err)
		}
		if err := os.WriteFile(absPath, []byte(*content), 0644); err != nil {
			return fmt.Errorf("restore file %s: %w", path, err)
		}
	}
	return nil
}

// NewPatcher creates a new patcher for the given project.
func NewPatcher(proj *project.Summary, dryRun bool) *Patcher {
	return &Patcher{
		project:   proj,
		validator: validation.NewValidator(proj),
		dryRun:    dryRun,
	}
}

// ApplyTaskResult applies all file changes from a task result.
func (p *Patcher) ApplyTaskResult(task *coding.Task, result *coding.TaskResult) error {
	// Validate first
	vr := p.validator.ValidateTaskResult(task, result)
	if !vr.Valid {
		return fmt.Errorf("validation failed: %v", vr.Errors)
	}

	// Apply full file writes
	for path, content := range result.Files {
		if err := p.WriteFile(path, content); err != nil {
			return fmt.Errorf("write file %s: %w", path, err)
		}
	}

	// Apply patches
	for path, patch := range result.Patches {
		if err := p.ApplyPatch(path, patch); err != nil {
			return fmt.Errorf("apply patch %s: %w", path, err)
		}
	}

	return nil
}

// WriteFile writes content to a file, creating directories as needed.
func (p *Patcher) WriteFile(path, content string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("get absolute path: %w", err)
	}

	// Check path is in project
	if !p.project.IsPathInProject(absPath) {
		return fmt.Errorf("path %s is outside project root", path)
	}

	if p.dryRun {
		fmt.Printf("[DRY RUN] Would write %d bytes to %s\n", len(content), path)
		return nil
	}

	// Create directory if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// ApplyPatch applies a unified diff patch to a file.
func (p *Patcher) ApplyPatch(path, patch string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("get absolute path: %w", err)
	}

	// Check path is in project
	if !p.project.IsPathInProject(absPath) {
		return fmt.Errorf("path %s is outside project root", path)
	}

	if p.dryRun {
		fmt.Printf("[DRY RUN] Would apply patch to %s\n", path)
		return nil
	}

	// Read existing file
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, treat patch as new file content
			return p.WriteFile(path, patch)
		}
		return fmt.Errorf("read file: %w", err)
	}

	// Apply patch
	patched, err := applyUnifiedDiff(string(content), patch)
	if err != nil {
		return fmt.Errorf("apply diff: %w", err)
	}

	// Write patched content
	if err := os.WriteFile(absPath, []byte(patched), 0644); err != nil {
		return fmt.Errorf("write patched file: %w", err)
	}

	return nil
}

// applyUnifiedDiff applies a unified diff patch to content.
// This is a simplified implementation that handles basic unified diff format.
func applyUnifiedDiff(original, patch string) (string, error) {
	lines := strings.Split(original, "\n")
	patchLines := strings.Split(patch, "\n")

	result := make([]string, 0, len(lines))
	lineIdx := 0

	for i := 0; i < len(patchLines); i++ {
		line := patchLines[i]

		// Skip header lines
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}

		// Parse hunk header
		if strings.HasPrefix(line, "@@") {
			// Extract line numbers from @@ -start,count +start,count @@
			parts := strings.Fields(line)
			if len(parts) < 3 {
				continue
			}

			// Parse old range
			oldRange := strings.TrimPrefix(parts[1], "-")
			oldParts := strings.Split(oldRange, ",")
			if len(oldParts) == 0 {
				continue
			}

			var oldStart int
			fmt.Sscanf(oldParts[0], "%d", &oldStart)
			oldStart-- // Convert to 0-based

			// Copy lines before hunk
			for lineIdx < oldStart && lineIdx < len(lines) {
				result = append(result, lines[lineIdx])
				lineIdx++
			}

			// Process hunk
			i++
			for i < len(patchLines) {
				hunkLine := patchLines[i]

				if strings.HasPrefix(hunkLine, "@@") {
					i-- // Back up to process next hunk
					break
				}

				if strings.HasPrefix(hunkLine, " ") {
					// Context line - keep it
					result = append(result, hunkLine[1:])
					lineIdx++
				} else if strings.HasPrefix(hunkLine, "-") {
					// Deletion - skip original line
					lineIdx++
				} else if strings.HasPrefix(hunkLine, "+") {
					// Addition - add new line
					result = append(result, hunkLine[1:])
				}

				i++
			}
			continue
		}
	}

	// Copy remaining lines
	for lineIdx < len(lines) {
		result = append(result, lines[lineIdx])
		lineIdx++
	}

	return strings.Join(result, "\n"), nil
}

// BackupFile creates a backup of a file before modification.
func (p *Patcher) BackupFile(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("get absolute path: %w", err)
	}

	if !validation.FileExists(absPath) {
		return "", fmt.Errorf("file does not exist: %s", path)
	}

	backupPath := absPath + ".backup"
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}

	return backupPath, nil
}

// RestoreBackup restores a file from its backup.
func (p *Patcher) RestoreBackup(backupPath string) error {
	if !validation.FileExists(backupPath) {
		return fmt.Errorf("backup does not exist: %s", backupPath)
	}

	originalPath := strings.TrimSuffix(backupPath, ".backup")
	content, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}

	if err := os.WriteFile(originalPath, content, 0644); err != nil {
		return fmt.Errorf("restore file: %w", err)
	}

	return nil
}

// DeleteBackup removes a backup file.
func (p *Patcher) DeleteBackup(backupPath string) error {
	if !validation.FileExists(backupPath) {
		return nil
	}
	return os.Remove(backupPath)
}

// ReadFile reads a file's content.
func ReadFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// ReadFileLines reads a file and returns its lines.
func ReadFileLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	lines := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}
