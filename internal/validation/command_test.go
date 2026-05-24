package validation

import (
	"context"
	"testing"

	"github.com/bprendie/weazlcode/internal/project"
)

func TestRunCommandExecutesWithoutShell(t *testing.T) {
	validator := NewValidator(&project.Summary{Root: t.TempDir()})

	result, err := validator.RunCommand(context.Background(), "python -c 'print(123)'")
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if result.Stdout != "123" {
		t.Fatalf("unexpected stdout: %q", result.Stdout)
	}
}

func TestValidateCommandBlocksDangerousExecutable(t *testing.T) {
	validator := NewValidator(&project.Summary{Root: t.TempDir()})

	if err := validator.ValidateCommand("rm generated.py"); err == nil {
		t.Fatal("expected rm command to be blocked")
	}
}
