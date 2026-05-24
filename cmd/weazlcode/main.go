package main

import (
	"fmt"
	"os"

	"github.com/bprendie/weazlcode/internal/app"
	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/project"
)

func main() {
	// Load configuration
	cfg, cfgPath, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config will be created at: %s\n", cfgPath)
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration validation failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please edit: %s\n", cfgPath)
		fmt.Fprintf(os.Stderr, "\nRequired configuration:\n")
		fmt.Fprintf(os.Stderr, "- planner.provider and planner.model\n")
		fmt.Fprintf(os.Stderr, "- worker.provider and worker.model\n")
		fmt.Fprintf(os.Stderr, "- reviewer.provider and reviewer.model\n")
		fmt.Fprintf(os.Stderr, "\nFor cloud providers (anthropic, openai):\n")
		fmt.Fprintf(os.Stderr, "- Set api_key_env to environment variable name\n")
		fmt.Fprintf(os.Stderr, "\nFor local providers (vllm, ollama):\n")
		fmt.Fprintf(os.Stderr, "- Set base_url to the API endpoint\n")
		os.Exit(1)
	}

	// Detect project
	projectSummary, err := project.Detect("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error detecting project: %v\n", err)
		os.Exit(1)
	}

	// Create application
	application, err := app.New(cfg, projectSummary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing application: %v\n", err)
		os.Exit(1)
	}

	// Run TUI
	if err := application.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
		os.Exit(1)
	}
}