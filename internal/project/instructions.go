package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	PrimaryInstructionsFile = "WEAZLCODE.md"
	AgentInstructionsFile   = "AGENTS.md"
)

type Instructions struct {
	Path    string
	Content string
}

func LoadInstructions(root string) (Instructions, bool, error) {
	for _, name := range []string{PrimaryInstructionsFile, AgentInstructionsFile} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Instructions{}, false, err
		}
		return Instructions{Path: path, Content: string(data)}, true, nil
	}
	return Instructions{}, false, nil
}

func InitInstructions(root string, summary Summary) (string, error) {
	path := filepath.Join(root, PrimaryInstructionsFile)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	body := GenerateInstructions(summary)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func GenerateInstructions(summary Summary) string {
	commands := DiscoverCommands(summary.Root)
	if len(commands) == 0 {
		commands = []string{"Add project-specific build, test, and lint commands here."}
	}
	langs := "none detected"
	if len(summary.Languages) > 0 {
		langs = strings.Join(summary.Languages, ", ")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# WeazlCode Project Instructions\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "## Project\n\n")
	fmt.Fprintf(&b, "- Root: `%s`\n", filepath.ToSlash(summary.Root))
	fmt.Fprintf(&b, "- Languages: %s\n", langs)
	if summary.GitRoot {
		fmt.Fprintf(&b, "- Git branch: `%s`\n", empty(summary.Branch, "unknown"))
	}
	fmt.Fprintf(&b, "\n## Commands\n\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "- `%s`\n", command)
	}
	fmt.Fprintf(&b, "\n## Worker Rules\n\n")
	fmt.Fprintf(&b, "- Local workers receive bounded task packets, allowed paths, diagnostics, and approved tools only.\n")
	fmt.Fprintf(&b, "- Request missing context with `read_file`, `read_file_range`, or `search_files` instead of guessing.\n")
	fmt.Fprintf(&b, "- Return patches or blockers; frontier reviewer approval is required before considering work complete.\n")
	return b.String()
}

func DiscoverCommands(root string) []string {
	seen := map[string]bool{}
	add := func(command string) {
		command = strings.TrimSpace(command)
		if command != "" {
			seen[command] = true
		}
	}
	if exists(root, "go.mod") {
		add("go test ./...")
		add("go build ./...")
	}
	if exists(root, "package.json") {
		add("npm test")
		add("npm run build")
	}
	if exists(root, "pyproject.toml") || exists(root, "pytest.ini") {
		add("python -m pytest")
	}
	if exists(root, "Cargo.toml") {
		add("cargo test")
		add("cargo check")
	}
	if exists(root, "Makefile") || exists(root, "makefile") {
		add("make test")
		add("make build")
	}
	out := make([]string, 0, len(seen))
	for command := range seen {
		out = append(out, command)
	}
	sort.Strings(out)
	return out
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

func empty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
