package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bprendie/weazlcode/internal/coding"
)

// View renders the TUI.
func (m Model) View() string {
	if !m.ready {
		return m.styles.Spinner.Render("⚡ Initializing WeazlCode...")
	}

	screenW := max(20, m.width)
	screenH := max(8, m.height)
	header := m.renderHeader()
	status := m.renderStatus()
	input := m.renderInput()
	help := m.renderHelp()
	content := renderPanel(m.styles.Panel, screenW, m.bodyHeight(), m.viewport.View())

	out := lipgloss.JoinVertical(lipgloss.Left, header, status, content, input, help)
	return m.styles.Frame.
		Width(screenW).
		Height(screenH).
		MaxWidth(screenW).
		MaxHeight(screenH).
		Render(out)
}

func (m Model) bodyHeight() int {
	header := m.renderHeader()
	status := m.renderStatus()
	input := m.renderInput()
	help := m.renderHelp()
	used := lineCount(header) + lineCount(status) + lineCount(input) + lineCount(help)
	return max(1, max(8, m.height)-used)
}

func renderPanel(style lipgloss.Style, outerW, outerH int, content string) string {
	return style.
		Width(contentWidth(style, outerW)).
		Height(contentHeight(style, outerH)).
		MaxWidth(outerW).
		MaxHeight(outerH).
		Render(content)
}

func contentWidth(style lipgloss.Style, outerW int) int {
	return max(1, outerW-style.GetHorizontalFrameSize())
}

func contentHeight(style lipgloss.Style, outerH int) int {
	return max(1, outerH-style.GetVerticalFrameSize())
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func (m *Model) resize() {
	screenW := max(20, m.width)
	bodyH := m.bodyHeight()
	m.viewport.Width = contentWidth(m.styles.Panel, screenW)
	m.viewport.Height = contentHeight(m.styles.Panel, bodyH)
	m.input.SetWidth(max(20, screenW-m.styles.Input.GetHorizontalFrameSize()))
	m.renderMessages()
}

func (m Model) renderHeader() string {
	screenW := max(20, m.width)
	title := renderLogoBanner(asciiLogo(), screenW)
	if m.height > 0 && m.height < 24 {
		title = compactLogoBanner(screenW)
	}
	banner := m.styles.Header.Render(title)

	projectLabel := lipgloss.NewStyle().Foreground(neonCyan).Bold(true).Render("PROJECT")
	projectName := lipgloss.NewStyle().Foreground(neonPink).Bold(true).Render(m.project.Name)
	rootLabel := lipgloss.NewStyle().Foreground(textDim).Render("root:")
	fixedWidth := lipgloss.Width(projectLabel) + 1 + lipgloss.Width(projectName) + 2 + lipgloss.Width(rootLabel) + 1
	rootPath := lipgloss.NewStyle().Foreground(textGlow).Render(truncateMiddle(m.project.Root, max(8, screenW-fixedWidth)))
	projectInfo := lipgloss.JoinHorizontal(lipgloss.Left, projectLabel, " ", projectName, "  ", rootLabel, " ", rootPath)

	return lipgloss.JoinVertical(lipgloss.Left, banner, m.styles.Status.MaxWidth(screenW).Render(projectInfo))
}

func (m Model) renderStatus() string {
	screenW := max(20, m.width)
	parts := m.statusParts(screenW)

	if m.runState != nil && m.currentPlan != nil {
		progress := m.styles.RenderTaskProgress(m.runState.CompletedTasks, len(m.currentPlan.Tasks), m.runState.ActiveWorkers)
		elapsed := lipgloss.NewStyle().Foreground(neonOrange).Render(fmt.Sprintf("%.1fs", m.runState.ElapsedSeconds))
		parts = append(parts, progress, elapsed)
	}

	status := m.styles.RenderStatusLine(parts...)
	if lipgloss.Width(status) <= screenW {
		return status
	}
	return m.styles.Status.Render(strings.Join(m.compactStatusParts(screenW), "\n"))
}

func (m Model) statusParts(width int) []string {
	if width < 110 {
		return m.compactStatusParts(width)
	}
	return []string{
		m.styles.RenderProviderBadge("PLAN", m.config.Planner.Provider, m.config.Planner.Model),
		m.styles.RenderProviderBadge("WORK", m.config.Worker.Provider, m.config.Worker.Model),
		m.styles.RenderProviderBadge("REVW", m.config.Reviewer.Provider, m.config.Reviewer.Model),
	}
}

func (m Model) compactStatusParts(width int) []string {
	partWidth := max(18, width-2)
	return []string{
		m.compactProvider("PLAN", m.config.Planner.Provider, m.config.Planner.Model, partWidth),
		m.compactProvider("WORK", m.config.Worker.Provider, m.config.Worker.Model, partWidth),
		m.compactProvider("REVW", m.config.Reviewer.Provider, m.config.Reviewer.Model, partWidth),
	}
}

func (m Model) compactProvider(role, provider, model string, width int) string {
	roleText := lipgloss.NewStyle().Foreground(neonPurple).Bold(true).Render(role)
	providerText := lipgloss.NewStyle().Foreground(neonCyan).Bold(true).Render(provider)
	fixed := lipgloss.Width(roleText) + 1 + lipgloss.Width(providerText) + 1
	modelText := lipgloss.NewStyle().Foreground(textGlow).Render(truncateMiddle(model, max(6, width-fixed)))
	return lipgloss.JoinHorizontal(lipgloss.Left, roleText, " ", providerText, "/", modelText)
}

func (m Model) renderInput() string {
	if m.mode == "planning" {
		return m.styles.RenderSpinner(m.spinner.View(), "PLANNING IMPLEMENTATION...")
	}
	if m.mode == "running" {
		return m.styles.RenderSpinner(m.spinner.View(), "EXECUTING TASKS...")
	}

	inputStyle := m.styles.Input
	if m.input.Focused() {
		inputStyle = m.styles.InputFocus
	}
	return inputStyle.Width(max(20, m.width-6)).Render(m.input.View())
}

func (m Model) renderHelp() string {
	helpParts := []string{
		m.styles.HelpKey.Render("esc") + m.styles.HelpValue.Render(" back"),
		m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpValue.Render(" quit"),
	}
	switch m.mode {
	case "input":
		helpParts = []string{
			m.styles.HelpKey.Render("enter") + m.styles.HelpValue.Render(" send"),
			m.styles.HelpKey.Render("/plan") + m.styles.HelpValue.Render(" <directive>"),
			m.styles.HelpKey.Render("/help") + m.styles.HelpValue.Render(" commands"),
			m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpValue.Render(" quit"),
		}
	case "planning":
		helpParts = []string{m.styles.Info.Render("⚡ Planning..."), m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpValue.Render(" abort")}
	case "running":
		helpParts = []string{m.styles.Info.Render("⚡ Executing..."), m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpValue.Render(" abort")}
	}

	separator := lipgloss.NewStyle().Foreground(neonPurple).Render(" ▸ ")
	help := strings.Join(helpParts, separator)
	if lipgloss.Width(help) > max(20, m.width) {
		help = m.compactHelpText()
	}
	return m.styles.Help.MaxWidth(max(20, m.width)).Render(help)
}

func (m Model) compactHelpText() string {
	switch m.mode {
	case "planning":
		return m.styles.Info.Render("⚡ Planning") + " " + m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpValue.Render(" abort")
	case "running":
		return m.styles.Info.Render("⚡ Executing") + " " + m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpValue.Render(" abort")
	default:
		return m.styles.HelpKey.Render("enter") + m.styles.HelpValue.Render(" send") + " " +
			m.styles.HelpKey.Render("/plan") + " " +
			m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpValue.Render(" quit")
	}
}

func truncateMiddle(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	runes := []rune(s)
	left := max(1, (width-3)/2)
	right := max(1, width-3-left)
	if left+right >= len(runes) {
		return s
	}
	return string(runes[:left]) + "..." + string(runes[len(runes)-right:])
}

func (m *Model) renderMessages() {
	var b strings.Builder
	if len(m.messages) == 0 {
		welcome := lipgloss.NewStyle().Foreground(neonCyan).Bold(true).Render("⬢ WEAZLCODE V2 ONLINE")
		info := lipgloss.NewStyle().Foreground(textGlow)
		b.WriteString(welcome)
		b.WriteString("\n\n")
		b.WriteString(info.Render("Split-brain AI coding system ready."))
		b.WriteString("\n")
		b.WriteString(info.Render("Enter a directive to begin, or use "))
		b.WriteString(m.styles.HelpKey.Render("/help"))
		b.WriteString(info.Render(" for commands."))
	} else {
		for _, msg := range m.messages {
			b.WriteString(m.messageLabel(msg.Role))
			b.WriteString("\n")
			b.WriteString(lipgloss.NewStyle().Foreground(textGlow).Render(msg.Content))
			b.WriteString("\n\n")
		}
	}
	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

func (m Model) messageLabel(role string) string {
	switch role {
	case "user":
		return m.styles.User.Render("▸ YOU")
	case "assistant":
		return m.styles.Assistant.Render("◂ AI")
	default:
		return m.styles.System.Render("⬢ SYS")
	}
}

func renderPlanSummary(plan *coding.Plan) string {
	if plan == nil {
		return "No plan."
	}
	var b strings.Builder
	b.WriteString("PLAN READY\n\nDirective: ")
	b.WriteString(plan.Directive)
	b.WriteString("\n\nTasks:\n")
	for i, task := range plan.Tasks {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, task.ID))
		b.WriteString("   Goal: ")
		b.WriteString(task.Goal)
		b.WriteString("\n")
		if len(task.OutputFiles) > 0 {
			b.WriteString("   Files: ")
			b.WriteString(strings.Join(task.OutputFiles, ", "))
			b.WriteString("\n")
		}
		if task.VerifyCommand != "" {
			b.WriteString("   Verify: ")
			b.WriteString(task.VerifyCommand)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nUse /approve to run this plan.")
	return b.String()
}
