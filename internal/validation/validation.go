package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/project"
)

// Validator performs deterministic validation checks.
type Validator struct {
	project *project.Summary
}

// NewValidator creates a new validator for the given project.
func NewValidator(proj *project.Summary) *Validator {
	return &Validator{project: proj}
}

// ValidateTaskResult performs all validation checks on a task result.
func (v *Validator) ValidateTaskResult(task *coding.Task, result *coding.TaskResult) *coding.ValidationResult {
	vr := &coding.ValidationResult{Valid: true}

	// Check path guardrails
	if err := v.checkPathGuardrails(task, result); err != nil {
		vr.Valid = false
		vr.Errors = append(vr.Errors, err.Error())
	}

	// Check file existence for expected outputs
	if err := v.checkExpectedFiles(task, result); err != nil {
		vr.Valid = false
		vr.Errors = append(vr.Errors, err.Error())
	}

	// Check syntax for known file types
	warnings := v.checkSyntax(result)
	vr.Warnings = append(vr.Warnings, warnings...)

	return vr
}

// checkPathGuardrails ensures all file operations are within allowed paths.
func (v *Validator) checkPathGuardrails(task *coding.Task, result *coding.TaskResult) error {
	allowedPaths := task.AllowedPaths
	if len(allowedPaths) == 0 {
		// If no allowed paths specified, default to project root
		allowedPaths = []string{v.project.Root}
	}

	// Check all file paths
	allPaths := make([]string, 0)
	for path := range result.Files {
		allPaths = append(allPaths, path)
	}
	for path := range result.Patches {
		allPaths = append(allPaths, path)
	}

	for _, path := range allPaths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("invalid path %s: %w", path, err)
		}

		// Check if path is within project
		if !v.project.IsPathInProject(absPath) {
			return fmt.Errorf("path %s is outside project root", path)
		}

		// Check if path is within allowed paths
		allowed := false
		for _, allowedPath := range allowedPaths {
			absAllowed, err := filepath.Abs(allowedPath)
			if err != nil {
				continue
			}

			rel, err := filepath.Rel(absAllowed, absPath)
			if err != nil {
				continue
			}

			// Path is allowed if it's under the allowed path (doesn't start with ..)
			if !strings.HasPrefix(rel, "..") {
				allowed = true
				break
			}
		}

		if !allowed {
			return fmt.Errorf("path %s is not within allowed paths: %v", path, allowedPaths)
		}
	}

	return nil
}

// checkExpectedFiles verifies that expected output files were created.
func (v *Validator) checkExpectedFiles(task *coding.Task, result *coding.TaskResult) error {
	if len(task.OutputFiles) == 0 {
		return nil
	}

	missing := make([]string, 0)
	for _, expectedFile := range task.OutputFiles {
		found := false

		// Check if file is in result.Files or result.Patches
		if _, ok := result.Files[expectedFile]; ok {
			found = true
		}
		if _, ok := result.Patches[expectedFile]; ok {
			found = true
		}

		if !found {
			missing = append(missing, expectedFile)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing expected output files: %v", missing)
	}

	return nil
}

// checkSyntax performs basic syntax checks for known file types.
func (v *Validator) checkSyntax(result *coding.TaskResult) []string {
	warnings := make([]string, 0)

	for path, content := range result.Files {
		ext := strings.ToLower(filepath.Ext(path))

		switch ext {
		case ".py":
			if warns := checkPythonSyntax(path, content); len(warns) > 0 {
				warnings = append(warnings, warns...)
			}
		case ".html":
			if warns := checkHTMLSyntax(path, content); len(warns) > 0 {
				warnings = append(warnings, warns...)
			}
		case ".json":
			if warns := checkJSONSyntax(path, content); len(warns) > 0 {
				warnings = append(warnings, warns...)
			}
		}
	}

	return warnings
}

// ValidateCommand checks if a command is safe to execute.
func (v *Validator) ValidateCommand(cmd string) error {
	// Disallow dangerous commands
	dangerous := []string{
		"rm -rf /",
		"dd if=",
		"mkfs",
		":(){ :|:& };:",
		"> /dev/sda",
	}

	cmdLower := strings.ToLower(cmd)
	for _, danger := range dangerous {
		if strings.Contains(cmdLower, danger) {
			return fmt.Errorf("dangerous command detected: %s", danger)
		}
	}

	parts, err := splitCommand(cmd)
	if err != nil {
		return fmt.Errorf("parse command: %w", err)
	}
	for len(parts) > 0 && isEnvAssignment(parts[0]) {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return fmt.Errorf("missing executable")
	}

	blockedExecutables := map[string]bool{
		"rm": true, "dd": true, "mkfs": true, "sudo": true, "su": true,
		"chmod": true, "chown": true, "mount": true, "umount": true,
	}
	if blockedExecutables[filepath.Base(parts[0])] {
		return fmt.Errorf("blocked executable in verification command: %s", parts[0])
	}

	// Disallow commands that modify system files
	systemPaths := []string{
		"/etc/",
		"/usr/",
		"/bin/",
		"/sbin/",
		"/boot/",
		"/sys/",
		"/proc/",
	}

	for _, sysPath := range systemPaths {
		if strings.Contains(cmd, sysPath) {
			return fmt.Errorf("command attempts to access system path: %s", sysPath)
		}
	}

	return nil
}

// FileExists checks if a file exists at the given path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDirectory checks if a path is a directory.
func IsDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
