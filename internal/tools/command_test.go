package tools

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidateReadOnlyCommandRejectsVerification(t *testing.T) {
	if err := validateCommand(commandModeReadOnly, "go", []string{"test", "./..."}); err == nil {
		t.Fatal("go test was allowed as read-only command")
	}
}

func TestValidateVerificationCommandAllowsPolicyPresets(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		args []string
	}{
		{name: "go test", cmd: "go", args: []string{"test", "./..."}},
		{name: "go build", cmd: "go", args: []string{"build", "./cmd/weazlcode"}},
		{name: "npm test", cmd: "npm", args: []string{"test"}},
		{name: "python pytest", cmd: "python3", args: []string{"-m", "pytest"}},
		{name: "python smoke", cmd: "python3", args: []string{"app.py", "--smoke"}},
		{name: "cargo check", cmd: "cargo", args: []string{"check"}},
		{name: "shellcheck", cmd: "shellcheck", args: []string{"script.sh"}},
		{name: "make test", cmd: "make", args: []string{"test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCommand(commandModeVerification, tt.cmd, tt.args); err != nil {
				t.Fatalf("validateCommand returned error: %v", err)
			}
		})
	}
}

func TestValidateVerificationCommandRejectsUnsafeTargets(t *testing.T) {
	tests := []struct {
		cmd  string
		args []string
	}{
		{cmd: "git", args: []string{"status"}},
		{cmd: "go", args: []string{"env"}},
		{cmd: "npm", args: []string{"install"}},
		{cmd: "python3", args: []string{"setup.py", "install"}},
		{cmd: "make", args: []string{"deploy"}},
	}
	for _, tt := range tests {
		if err := validateCommand(commandModeVerification, tt.cmd, tt.args); err == nil {
			t.Fatalf("%s %v was allowed", tt.cmd, tt.args)
		}
	}
}

func TestRunVerificationCommandResolvesConfiguredPython(t *testing.T) {
	python := filepath.Join(t.TempDir(), "python")
	tool := NewRunVerificationCommandTool(Limits{PythonBin: python})

	gotName, gotArgs := tool.resolveExecutable(t.TempDir(), "python3", []string{"app.py", "--smoke"})
	if gotName != python || !reflect.DeepEqual(gotArgs, []string{"app.py", "--smoke"}) {
		t.Fatalf("python resolution = %q %#v", gotName, gotArgs)
	}

	gotName, gotArgs = tool.resolveExecutable(t.TempDir(), "pytest", []string{"-q"})
	if gotName != python || !reflect.DeepEqual(gotArgs, []string{"-m", "pytest", "-q"}) {
		t.Fatalf("pytest resolution = %q %#v", gotName, gotArgs)
	}
}
