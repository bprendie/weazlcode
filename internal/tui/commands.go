package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) handleInput() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input.Value())
	if input == "" {
		return m, nil
	}

	m.messages = append(m.messages, Message{Role: "user", Content: input})
	m.input.Reset()
	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}
	return m.startPlanning(input)
}

func (m Model) handleCommand(cmd string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return m, nil
	}

	switch command := strings.ToLower(parts[0]); command {
	case "/plan":
		directive := strings.TrimSpace(strings.TrimPrefix(cmd, parts[0]))
		if directive == "" {
			m.messages = append(m.messages, Message{Role: "system", Content: m.styles.Error.Render("Usage: /plan <directive>")})
			break
		}
		return m.startPlanning(directive)
	case "/approve", "/run":
		return m.startRun()
	case "/help":
		m.messages = append(m.messages, Message{Role: "system", Content: m.helpCommandText()})
	case "/status":
		m.messages = append(m.messages, Message{Role: "system", Content: m.statusCommandText()})
	case "/config":
		m.messages = append(m.messages, Message{
			Role:    "system",
			Content: m.styles.Info.Render("⚡ Config loaded from: ") + m.styles.HelpValue.Render("~/.config/weazlcode/config.json"),
		})
	default:
		m.messages = append(m.messages, Message{
			Role:    "system",
			Content: m.styles.Error.Render("✗ Unknown command: ") + m.styles.HelpValue.Render(command) + "\nUse /help for available commands.",
		})
	}

	m.renderMessages()
	return m, nil
}

func (m Model) startPlanning(directive string) (tea.Model, tea.Cmd) {
	m.mode = "planning"
	m.messages = append(m.messages, Message{Role: "system", Content: "⚡ Creating split-brain implementation plan..."})
	m.renderMessages()
	return m, tea.Batch(m.createPlanCmd(directive), tickRunState())
}

func (m Model) startRun() (tea.Model, tea.Cmd) {
	if m.currentPlan == nil {
		m.messages = append(m.messages, Message{Role: "system", Content: m.styles.Error.Render("No plan is ready. Use /plan <directive> first.")})
		m.renderMessages()
		return m, nil
	}
	m.mode = "running"
	m.messages = append(m.messages, Message{Role: "system", Content: "⚡ Running approved plan..."})
	m.renderMessages()
	return m, tea.Batch(m.runPlanCmd(), tickRunState())
}

func (m Model) createPlanCmd(directive string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.config.Limits.ModelTimeoutSeconds)*time.Second)
		defer cancel()
		plan, err := m.orchestrator.CreatePlan(ctx, directive)
		return planCreatedMsg{plan: plan, err: err}
	}
}

func (m Model) runPlanCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.config.Limits.RunSoftLimitMinutes)*time.Minute)
		defer cancel()
		return runFinishedMsg{err: m.orchestrator.ExecutePlan(ctx)}
	}
}

func tickRunState() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return runTickMsg{} })
}

func (m Model) helpCommandText() string {
	return lipgloss.NewStyle().Foreground(neonCyan).Render("AVAILABLE COMMANDS") + "\n\n" +
		m.styles.HelpKey.Render("/help") + " - Show this help\n" +
		m.styles.HelpKey.Render("/plan") + " " + m.styles.HelpValue.Render("<directive>") + " - Create implementation plan\n" +
		m.styles.HelpKey.Render("/approve") + " - Run the current plan\n" +
		m.styles.HelpKey.Render("/run") + " - Run the current plan\n" +
		m.styles.HelpKey.Render("/status") + " - Show current status\n" +
		m.styles.HelpKey.Render("/config") + " - Show configuration"
}

func (m Model) statusCommandText() string {
	return lipgloss.NewStyle().Foreground(neonCyan).Bold(true).Render("SYSTEM STATUS") + "\n\n" +
		m.styles.HelpKey.Render("Project:") + " " + m.styles.HelpValue.Render(m.project.Name) + "\n" +
		m.styles.HelpKey.Render("Root:") + " " + m.styles.HelpValue.Render(m.project.Root) + "\n\n" +
		m.styles.HelpKey.Render("Planner:") + " " + m.styles.HelpValue.Render(m.config.Planner.Provider+"/"+m.config.Planner.Model) + "\n" +
		m.styles.HelpKey.Render("Worker:") + " " + m.styles.HelpValue.Render(m.config.Worker.Provider+"/"+m.config.Worker.Model) + "\n" +
		m.styles.HelpKey.Render("Reviewer:") + " " + m.styles.HelpValue.Render(m.config.Reviewer.Provider+"/"+m.config.Reviewer.Model)
}
