package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/orchestrator"
	"github.com/bprendie/weazlcode/internal/planner"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/providers"
	"github.com/bprendie/weazlcode/internal/reviewer"
	"github.com/bprendie/weazlcode/internal/tui"
	"github.com/bprendie/weazlcode/internal/worker"
)

// App represents the WeazlCode application.
type App struct {
	config       *config.Config
	project      *project.Summary
	planner      *planner.Planner
	worker       *worker.Worker
	reviewer     *reviewer.Reviewer
	orchestrator *orchestrator.Orchestrator
}

// New creates a new application instance.
func New(cfg *config.Config, proj *project.Summary) (*App, error) {
	// Create providers
	plannerProvider, err := providers.NewPlannerProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("create planner provider: %w", err)
	}

	workerProvider, err := providers.NewWorkerProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("create worker provider: %w", err)
	}

	reviewerProvider, err := providers.NewReviewerProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("create reviewer provider: %w", err)
	}

	// Create components
	plan := planner.NewPlanner(plannerProvider, proj)
	work := worker.NewWorker(workerProvider, proj)
	rev := reviewer.NewReviewer(reviewerProvider, proj)

	// Create orchestrator
	orchConfig := orchestrator.Config{
		WorkerConcurrency: cfg.Limits.WorkerConcurrency,
		ModelTimeout:      time.Duration(cfg.Limits.ModelTimeoutSeconds) * time.Second,
		RunSoftLimit:      time.Duration(cfg.Limits.RunSoftLimitMinutes) * time.Minute,
		MaxRepairAttempts: cfg.Limits.RepairAttempts,
	}
	orch := orchestrator.NewOrchestrator(plan, work, rev, proj, orchConfig)

	// Ensure project directories
	if err := proj.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("ensure project directories: %w", err)
	}

	return &App{
		config:       cfg,
		project:      proj,
		planner:      plan,
		worker:       work,
		reviewer:     rev,
		orchestrator: orch,
	}, nil
}

// Run starts the TUI application.
func (a *App) Run() error {
	model := tui.New(a.config, a.project, a.orchestrator)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}

	return nil
}

// GetOrchestrator returns the orchestrator for external use.
func (a *App) GetOrchestrator() *orchestrator.Orchestrator {
	return a.orchestrator
}
