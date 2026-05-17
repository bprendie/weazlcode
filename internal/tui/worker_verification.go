package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/project"
)

type verificationResult struct {
	Command string `json:"command"`
	Output  string `json:"output"`
}

func (m model) runTaskVerification(task coding.Task) ([]verificationResult, error) {
	commands := m.taskVerification(task)
	if len(commands) == 0 {
		return nil, nil
	}
	tool, ok := m.toolRegistry.Get("run_verification_command")
	if !ok {
		return nil, fmt.Errorf("run_verification_command tool is not registered")
	}
	results := make([]verificationResult, 0, len(commands))
	for _, commandText := range commands {
		name, args, err := splitVerificationCommand(commandText)
		if err != nil {
			return nil, err
		}
		rawArgs := make([]any, 0, len(args))
		for _, arg := range args {
			rawArgs = append(rawArgs, arg)
		}
		output, err := tool.Execute(context.Background(), map[string]any{
			"command": name,
			"args":    rawArgs,
			"cwd":     m.project.Root,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", commandText, err)
		}
		results = append(results, verificationResult{Command: commandText, Output: output})
	}
	return results, nil
}

func (m model) taskVerification(task coding.Task) []string {
	if len(task.Verification) == 0 {
		return m.defaultVerificationCommands()
	}
	return task.Verification
}

func (m model) defaultVerificationCommands() []string {
	commands := project.DiscoverCommands(m.project.Root)
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		if allowlistedVerificationCommand(command) {
			out = append(out, strings.TrimSpace(command))
		}
	}
	return out
}

func normalizePlanVerification(plan *coding.Plan) {
	for i := range plan.Tasks {
		plan.Tasks[i].Verification = filterVerificationCommands(plan.Tasks[i].Verification)
	}
}

func filterVerificationCommands(commands []string) []string {
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		if allowlistedVerificationCommand(command) {
			out = append(out, strings.TrimSpace(command))
		}
	}
	return out
}

func allowlistedVerificationCommand(commandText string) bool {
	name, args, err := splitVerificationCommand(commandText)
	if err != nil {
		return false
	}
	switch name {
	case "go":
		return len(args) > 0 && (args[0] == "test" || args[0] == "build" || args[0] == "vet")
	case "npm":
		return len(args) > 0 && (args[0] == "test" || args[0] == "run")
	case "python", "python3":
		if len(args) < 2 || args[0] != "-m" {
			return false
		}
		return args[1] == "pytest" || args[1] == "unittest" || args[1] == "compileall"
	case "pytest", "shellcheck":
		return true
	case "cargo":
		return len(args) > 0 && (args[0] == "test" || args[0] == "build" || args[0] == "check" || args[0] == "clippy")
	case "make":
		return len(args) > 0 && (args[0] == "test" || args[0] == "check" || args[0] == "lint" || args[0] == "build")
	default:
		return false
	}
}

func splitVerificationCommand(commandText string) (string, []string, error) {
	fields := strings.Fields(commandText)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("verification command is empty")
	}
	return fields[0], fields[1:], nil
}
