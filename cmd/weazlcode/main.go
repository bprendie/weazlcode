package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/storage"
	"github.com/bprendie/weazlcode/internal/tools"
	"github.com/bprendie/weazlcode/internal/tui"
)

func main() {
	if len(os.Args) > 1 {
		if handled, code := handleCLI(os.Args[1:]); handled {
			os.Exit(code)
		}
	}
	cfg, cfgPath, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Database.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		fmt.Fprintf(os.Stderr, "database migration: %v\n", err)
		os.Exit(1)
	}

	projectSummary, err := project.Detect("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "project: %v\n", err)
		os.Exit(1)
	}
	workspaceRoots := append([]string{}, cfg.Tools.WorkspaceRoots...)
	workspaceRoots = append(workspaceRoots, projectSummary.Root)
	toolLimits := tools.Limits{
		WorkspaceRoots: workspaceRoots,
		MaxOutputChars: cfg.Tools.MaxOutputChars,
		MaxFileBytes:   cfg.Tools.MaxFileBytes,
	}
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(tools.NewCalculatorTool())
	toolRegistry.Register(tools.NewDateTimeTool())
	toolRegistry.Register(tools.NewWeatherTool())
	toolRegistry.Register(tools.NewFetchURLTool(toolLimits))
	toolRegistry.Register(tools.NewListFilesTool(toolLimits))
	toolRegistry.Register(tools.NewReadFileTool(toolLimits))
	toolRegistry.Register(tools.NewReadFileRangeTool(toolLimits))
	toolRegistry.Register(tools.NewSearchFilesTool(toolLimits))
	toolRegistry.Register(tools.NewCreateFileTool(toolLimits))
	toolRegistry.Register(tools.NewRunCommandTool(toolLimits))
	toolRegistry.Register(tools.NewRunReadOnlyCommandTool(toolLimits))
	toolRegistry.Register(tools.NewRunVerificationCommandTool(toolLimits))
	toolRegistry.Register(tools.NewGitStatusTool(toolLimits))
	toolRegistry.Register(tools.NewGitDiffTool(toolLimits))
	toolRegistry.Register(tools.NewGitLogTool(toolLimits))
	toolRegistry.Register(tools.NewGitShowTool(toolLimits))
	toolRegistry.Register(tools.NewListChangedFilesTool(toolLimits))
	toolRegistry.Register(tools.NewApplyPatchTool(toolLimits))
	toolRegistry.Register(tools.NewSQLiteQueryTool(toolLimits))
	toolRegistry.Register(tools.NewRememberTool(store, toolLimits))
	toolRegistry.Register(tools.NewRecallTool(store, toolLimits))
	toolRegistry.Register(tools.NewListMemoriesTool(store, toolLimits))
	toolRegistry.Register(tools.NewForgetTool(store))
	if cfg.Tools.AlphaVantageKey != "" {
		toolRegistry.Register(tools.NewStockPriceTool(cfg.Tools.AlphaVantageKey))
	}
	if cfg.Tools.BraveAPIKey != "" {
		toolRegistry.Register(tools.NewWebSearchTool(cfg.Tools.BraveAPIKey))
	}

	p := tea.NewProgram(tui.New(cfg, cfgPath, store, toolRegistry, projectSummary), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}

func handleCLI(args []string) (bool, int) {
	switch strings.ToLower(args[0]) {
	case "init":
		summary, err := project.Detect("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "project: %v\n", err)
			return true, 1
		}
		path, err := project.InitInstructions(summary.Root, summary)
		if err != nil {
			fmt.Fprintf(os.Stderr, "init: %v\n", err)
			return true, 1
		}
		fmt.Printf("Wrote %s\n", path)
		return true, 0
	case "help", "--help", "-h":
		fmt.Println("WeazlCode")
		fmt.Println("")
		fmt.Println("Usage:")
		fmt.Println("  weazlcode          start the TUI")
		fmt.Println("  weazlcode init     create WEAZLCODE.md project instructions")
		return true, 0
	default:
		return false, 0
	}
}
