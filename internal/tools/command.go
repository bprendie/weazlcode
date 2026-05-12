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
}

func NewRunCommandTool(limits Limits) *RunCommandTool {
	return &RunCommandTool{limits: limits}
}

func (t *RunCommandTool) Name() string { return "run_command" }
func (t *RunCommandTool) Description() string {
	return "Run a read-only allowlisted command under a configured workspace root. Pass command and args separately; shell syntax is not supported"
}
func (t *RunCommandTool) SafetyLevel() SafetyLevel { return SafetyLevelSafe }
func (t *RunCommandTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "command", Type: "string", Description: "Allowlisted command: pwd, ls, find, rg, cat, git, go, npm", Required: true},
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
	if err := validateReadOnlyCommand(name, args); err != nil {
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
	base := filepath.Base(name)
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
	case "go":
		if len(args) > 0 && args[0] == "test" {
			return nil
		}
		return fmt.Errorf("only go test is allowlisted")
	case "npm":
		if len(args) > 0 && args[0] == "test" {
			return nil
		}
		return fmt.Errorf("only npm test is allowlisted")
	default:
		return fmt.Errorf("%s is not allowlisted", name)
	}
}
