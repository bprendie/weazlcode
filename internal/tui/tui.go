package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/orchestrator"
	"github.com/bprendie/weazlcode/internal/project"
)

// Model represents the TUI state.
type Model struct {
	config       *config.Config
	project      *project.Summary
	orchestrator *orchestrator.Orchestrator

	// UI components
	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model

	// State
	mode   string // input, planning, running, viewing
	width  int
	height int
	ready  bool

	// Content
	messages    []Message
	currentPlan *coding.Plan
	runState    *coding.RunState

	// Styles
	styles CyberpunkStyles
}

type planCreatedMsg struct {
	plan *coding.Plan
	err  error
}

type runFinishedMsg struct {
	err error
}

type runTickMsg struct{}

// Message represents a chat-style message.
type Message struct {
	Role    string // user, system, assistant
	Content string
}

// New creates a new TUI model.
func New(cfg *config.Config, proj *project.Summary, orch *orchestrator.Orchestrator) Model {
	// Create spinner with cyberpunk style
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = lipgloss.NewStyle().Foreground(neonPurple)

	// Create input
	ti := textarea.New()
	ti.Placeholder = "▸ Enter directive or /command..."
	ti.Focus()
	ti.CharLimit = 4000
	ti.SetWidth(80)
	ti.SetHeight(3)
	ti.ShowLineNumbers = false

	// Create viewport
	vp := viewport.New(80, 20)

	return Model{
		config:       cfg,
		project:      proj,
		orchestrator: orch,
		viewport:     vp,
		input:        ti,
		spinner:      s,
		mode:         "input",
		messages:     make([]Message, 0),
		styles:       NewCyberpunkStyles(),
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
	)
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.viewport = viewport.New(1, 1)
			m.ready = true
		}
		m.resize()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.mode == "input" {
				return m.handleInput()
			}
		case "esc":
			if m.mode != "input" {
				m.mode = "input"
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case planCreatedMsg:
		m.mode = "input"
		if msg.err != nil {
			m.messages = append(m.messages, Message{Role: "system", Content: m.styles.Error.Render("✗ Plan failed: ") + msg.err.Error()})
			m.renderMessages()
			return m, nil
		}
		m.currentPlan = msg.plan
		m.runState = m.orchestrator.GetRunState()
		m.messages = append(m.messages, Message{Role: "assistant", Content: renderPlanSummary(msg.plan)})
		m.renderMessages()
		return m, nil

	case runFinishedMsg:
		m.mode = "input"
		m.runState = m.orchestrator.GetRunState()
		m.currentPlan = m.orchestrator.GetCurrentPlan()
		if msg.err != nil {
			m.messages = append(m.messages, Message{Role: "system", Content: m.styles.Error.Render("✗ Run failed: ") + msg.err.Error()})
		} else {
			m.messages = append(m.messages, Message{Role: "system", Content: m.styles.Success.Render("✓ Run completed")})
		}
		m.renderMessages()
		return m, nil

	case runTickMsg:
		if m.orchestrator != nil {
			m.runState = m.orchestrator.GetRunState()
			m.currentPlan = m.orchestrator.GetCurrentPlan()
		}
		if m.mode == "planning" || m.mode == "running" {
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return runTickMsg{} })
		}
	}

	// Update components based on mode
	if m.mode == "input" {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}
