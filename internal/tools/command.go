package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type RunCommandTool struct {
	limits Limits
	mode   commandMode
}

func NewRunCommandTool(limits Limits) *RunCommandTool {
	return &RunCommandTool{limits: limits, mode: commandModeReadOnly}
}

func NewRunReadOnlyCommandTool(limits Limits) *RunCommandTool {
	return &RunCommandTool{limits: limits, mode: commandModeReadOnly}
}

func NewRunVerificationCommandTool(limits Limits) *RunCommandTool {
	return &RunCommandTool{limits: limits, mode: commandModeVerification}
}

type commandMode int

const (
	commandModeReadOnly commandMode = iota
	commandModeVerification
)

func (t *RunCommandTool) Name() string {
	if t.mode == commandModeVerification {
		return "run_verification_command"
	}
	if t.mode == commandModeReadOnly {
		return "run_readonly_command"
	}
	return "run_command"
}
func (t *RunCommandTool) Description() string {
	if t.mode == commandModeVerification {
		return "Run an allowlisted verification command such as tests, build, vet, or lint under a configured workspace root. Pass command and args separately; shell syntax is not supported"
	}
	return "Run a read-only allowlisted command under a configured workspace root. Pass command and args separately; shell syntax is not supported"
}
func (t *RunCommandTool) SafetyLevel() SafetyLevel {
	if t.mode == commandModeVerification {
		return SafetyLevelPrompt
	}
	return SafetyLevelSafe
}
func (t *RunCommandTool) Parameters() []Parameter {
	desc := "Allowlisted read-only command: pwd, ls, find, rg, cat, git"
	if t.mode == commandModeVerification {
		desc = "Allowlisted verification command: go, npm, python, pytest, cargo, shellcheck, make"
	}
	return []Parameter{
		{Name: "command", Type: "string", Description: desc, Required: true},
		{Name: "args", Type: "array", Description: "Command arguments as an array of strings", Required: false},
		{Name: "cwd", Type: "string", Description: "Working directory under a configured workspace root", Required: true},
	}
}

func (t *RunCommandTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if err := t.limits.RequireRoots(); err != nil {
		return "", err
	}
	name, _ := params["command"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("command parameter is required")
	}
	args, err := stringSliceParam(params["args"])
	if err != nil {
		return "", err
	}
	if err := validateCommand(t.mode, name, args); err != nil {
		return "", err
	}
	cwdParam, _ := params["cwd"].(string)
	cwd, err := t.limits.ResolveAllowed(cwdParam)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		text += "\n" + err.Error()
	}
	return t.limits.Truncate(fmt.Sprintf("$ %s %s\n%s", name, strings.Join(args, " "), text)), nil
}

func stringSliceParam(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("args must be an array of strings")
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("args must be an array of strings")
		}
		if strings.ContainsAny(s, "\x00") {
			return nil, fmt.Errorf("args cannot contain NUL bytes")
		}
		out = append(out, s)
	}
	return out, nil
}

func validateReadOnlyCommand(name string, args []string) error {
	return validateCommand(commandModeReadOnly, name, args)
}

func validateCommand(mode commandMode, name string, args []string) error {
	base := filepath.Base(name)
	if mode == commandModeVerification {
		return validateVerificationCommand(base, args)
	}
	switch base {
	case "pwd", "ls", "find", "rg", "cat":
		return nil
	case "git":
		if len(args) == 0 {
			return fmt.Errorf("git subcommand is required")
		}
		switch args[0] {
		case "status", "diff", "log", "show", "branch":
			return nil
		default:
			return fmt.Errorf("git %s is not allowlisted", args[0])
		}
	default:
		return fmt.Errorf("%s is not allowlisted", name)
	}
}

func validateVerificationCommand(base string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s verification command requires arguments", base)
	}
	switch base {
	case "go":
		switch args[0] {
		case "test", "build", "vet":
			return nil
		}
		return fmt.Errorf("go %s is not an allowlisted verification command", args[0])
	case "npm":
		switch args[0] {
		case "test", "run":
			return nil
		}
		return fmt.Errorf("npm %s is not an allowlisted verification command", args[0])
	case "python", "python3":
		if len(args) >= 2 && args[0] == "-m" {
			switch args[1] {
			case "pytest", "unittest", "compileall":
				return nil
			}
		}
		return fmt.Errorf("only python -m pytest|unittest|compileall is allowlisted")
	case "pytest":
		return nil
	case "cargo":
		switch args[0] {
		case "test", "build", "check", "clippy":
			return nil
		}
		return fmt.Errorf("cargo %s is not an allowlisted verification command", args[0])
	case "shellcheck":
		return nil
	case "make":
		switch args[0] {
		case "test", "check", "lint", "build":
			return nil
		}
		return fmt.Errorf("make %s is not an allowlisted verification target", args[0])
	default:
		return fmt.Errorf("%s is not an allowlisted verification command", base)
	}
}
