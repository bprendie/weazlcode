package validation

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"

	"github.com/bprendie/weazlcode/internal/coding"
)

const maxCommandOutput = 6000

// RunCommand executes a verified command in the project root without a shell.
func (v *Validator) RunCommand(ctx context.Context, command string) (*coding.CommandResult, error) {
	if err := v.ValidateCommand(command); err != nil {
		return nil, err
	}

	parts, err := splitCommand(command)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	env := os.Environ()
	for len(parts) > 0 && isEnvAssignment(parts[0]) {
		env = append(env, parts[0])
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("missing executable")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = v.project.Root
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	result := &coding.CommandResult{
		Command:  command,
		Stdout:   trimOutput(stdout.String()),
		Stderr:   trimOutput(stderr.String()),
		ExitCode: exitCode(err),
	}
	if err != nil {
		return result, fmt.Errorf("command failed with exit code %d", result.ExitCode)
	}
	return result, nil
}

func splitCommand(command string) ([]string, error) {
	var parts []string
	var b strings.Builder
	var quote rune
	escaped := false

	for _, r := range command {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			if b.Len() > 0 {
				parts = append(parts, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}

	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts, nil
}

func isEnvAssignment(part string) bool {
	idx := strings.Index(part, "=")
	if idx <= 0 {
		return false
	}
	name := part[:idx]
	for i, r := range name {
		if !(r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r))) {
			return false
		}
	}
	return true
}

func trimOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= maxCommandOutput {
		return output
	}
	return output[:maxCommandOutput] + "\n...[truncated]"
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
