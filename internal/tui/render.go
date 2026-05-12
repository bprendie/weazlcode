package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/bprendie/weazlcode/internal/storage"
)

func (m model) View() string {
	header := renderLogo(ansiHeader(), max(20, m.width-6))
	status := m.styles.status.Render(m.status)
	if m.err != "" {
		status = m.styles.system.Render("! " + m.err)
	}
	body := ""
	switch m.mode {
	case modeVault:
		body = m.styles.panel.Render("Encrypted history password\n\n" + m.input.View())
	case modeServer:
		p := m.cfg.Active()
		body = m.styles.panel.Render(fmt.Sprintf("Server URL for %s / %s\n\n%s", p.Type, p.Model, m.input.View()))
	case modeLoading:
		body = m.styles.panel.Render(fmt.Sprintf("%s loading previous session", m.working.View()))
	case modeRenameWorkspace:
		body = m.renameWorkspaceView()
	case modeClearContext:
		body = m.clearContextView()
	case modeSessions:
		body = m.sessions.View()
	case modeWorkspace:
		body = m.workspaces.View()
	default:
		input := m.inputView()
		if m.thinking {
			input = m.thinkingView()
		}
		body = m.viewport.View() + "\n" + m.metricsView() + "\n" + m.styles.input.Width(max(20, m.width-6)).Render(input)
	}
	help := m.styles.help.Render(m.helpText())
	return m.styles.frame.Width(m.width).Height(m.height).Render(strings.Join([]string{header, status, body, help}, "\n"))
}

func (m model) renameWorkspaceView() string {
	w := max(20, m.width-6)
	popupWidth := min(64, max(24, w-4))
	prompt := m.styles.help.Render("shown in picker as <name>: timestamp")
	return lipgloss.PlaceHorizontal(w, lipgloss.Center, lipgloss.NewStyle().
		Width(popupWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(crushPink).
		Background(panel).
		Padding(1, 2).
		Render("Rename workspace\n\n"+m.input.View()+"\n"+prompt))
}

func (m model) clearContextView() string {
	w := max(20, m.width-6)
	popupWidth := min(66, max(30, w-4))
	count := len(m.messages)
	copy := fmt.Sprintf("Clear this session's active context?\n\nThis deletes %d message(s), tool-call history, and context checkpoints from SQLite for the current session.\n\n[ enter yes ]   [ esc no ]", count)
	return lipgloss.PlaceHorizontal(w, lipgloss.Center, lipgloss.NewStyle().
		Width(popupWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(crushPink).
		Background(panel).
		Padding(1, 2).
		Render(copy))
}

// renderMessages updates the viewport with the current message history and streaming state
func (m *model) renderMessages() {
	var b strings.Builder
	b.WriteString(m.renderTranscript(m.messages))
	if m.thinking {
		b.WriteString(m.styles.assistant.Render("ai"))
		b.WriteString("\n")
		if len(m.pendingTools) > 0 {
			b.WriteString(m.styles.system.Render("🔧 using tools"))
			b.WriteString("\n")
		}
		if m.streamText == "" {
			b.WriteString(m.thinkingView())
		} else {
			b.WriteString(wrapText(m.streamText, m.viewport.Width))
		}
		b.WriteString("\n\n")
	}
	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

func (m *model) renderTranscript(messages []storage.Message) string {
	var b strings.Builder
	if len(messages) == 0 {
		b.WriteString(m.styles.system.Render("WeazlCode is ready. Local providers only."))
		if m.cfg.Tools.Enabled {
			b.WriteString("\n")
			b.WriteString(m.styles.system.Render("Tools enabled: " + strings.Join(m.getToolNames(), ", ")))
		}
	} else {
		for _, msg := range messages {
			if msg.Role == "tool" {
				continue
			}
			if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" {
				if label := toolCallLabel(msg.ToolCalls); label != "" {
					b.WriteString(m.styles.system.Render(label))
					b.WriteString("\n\n")
				}
				continue
			}

			label := m.styles.user.Render("you")
			if msg.Role == "assistant" {
				label = m.styles.assistant.Render("ai")
			}
			b.WriteString(label)
			b.WriteString("\n")

			if msg.Content != "" {
				b.WriteString(m.renderContent(msg.Role, msg.Content))
			}
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func toolCallLabel(raw string) string {
	if raw == "" {
		return ""
	}
	var calls []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal([]byte(raw), &calls); err != nil || len(calls) == 0 {
		return "🔧 used tools"
	}
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.Function.Name != "" {
			names = append(names, call.Function.Name)
		}
	}
	if len(names) == 0 {
		return "🔧 used tools"
	}
	return "🔧 used " + strings.Join(names, ", ")
}

// getToolNames returns a list of registered tool names
func (m model) getToolNames() []string {
	if m.toolRegistry == nil {
		return nil
	}
	tools := m.toolRegistry.List()
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name()
	}
	return names
}

// thinkingView returns the spinner view with appropriate status text
func (m model) thinkingView() string {
	if m.trimming {
		return fmt.Sprintf("%s compacting_context_checkpoint", m.working.View())
	}
	return fmt.Sprintf("%s %s", m.working.View(), m.thinkingPhrase())
}

// thinkingPhrase returns a cyberpunk-themed status phrase that changes slowly during generation
func (m model) thinkingPhrase() string {
	if len(modelThinkingPhrases) == 0 || m.streamAt.IsZero() {
		return "model_is_thinking"
	}
	phase := min(2, int(time.Since(m.streamAt)/(20*time.Second)))
	start := int((m.streamAt.UnixNano() / int64(time.Millisecond)) % int64(len(modelThinkingPhrases)))
	idx := (start + phase) % len(modelThinkingPhrases)
	return modelThinkingPhrases[idx]
}

// metricsView returns the formatted metrics bar showing context usage and token counts
func (m model) metricsView() string {
	totalIn := m.session.InputTokens + m.reqIn
	totalOut := m.session.OutputTokens + m.reqOut
	tps := 0.0
	if m.thinking && !m.streamAt.IsZero() {
		elapsed := time.Since(m.streamAt).Seconds()
		if elapsed > 0 {
			tps = float64(m.reqOut) / elapsed
		}
	}
	budget := m.contextBudget()
	contextTokens := m.contextTokenEstimate()
	pct := min(1.0, float64(contextTokens)/float64(budget))
	text := fmt.Sprintf("%s  ctx %s %d/%d  in %d  out %d  %.1f t/s", m.project.StatusLabel(), m.contextBar.ViewAs(pct), contextTokens, budget, totalIn, totalOut, tps)
	width := max(20, m.width-6)
	if len(text) < width {
		text = strings.Repeat(" ", width-len(text)) + text
	}
	return m.styles.help.Render(text)
}

// helpText returns context-appropriate help text for the current mode
func (m model) helpText() string {
	if m.mode == modeLoading {
		return "loading previous session | ctrl+c quit"
	}
	if m.mode == modeRenameWorkspace {
		return "enter save rename | esc cancel | ctrl+c quit"
	}
	if m.mode == modeClearContext {
		return "enter clear context | esc cancel | ctrl+c quit"
	}
	if m.mode == modeSessions {
		return "enter resume | ctrl+d delete session | esc back | ctrl+c quit"
	}
	if m.mode == modeWorkspace {
		return "enter replay | ctrl+e rename | ctrl+d delete | esc back | ctrl+c quit"
	}
	mouseHelp := "ctrl+m copy"
	if !m.mouseScroll {
		mouseHelp = "ctrl+m mouse"
	}
	renameHelp := ""
	if m.activeWorkspaceID != 0 {
		renameHelp = " | ctrl+e rename"
	}
	return "enter send/select | wheel/pgup/pgdn scroll | " + mouseHelp + " | ctrl+n new | ctrl+t trim | ctrl+u clear | ctrl+r workspaces | ctrl+s save" + renameHelp + " | ctrl+c quit"
}

// inputView returns the input field view with paste indicator if applicable
func (m model) inputView() string {
	if m.pasteText == "" {
		return m.input.View()
	}
	prefix := strings.TrimSpace(m.input.Value())
	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0D0D12")).
		Background(lipgloss.Color("#F3E600")).
		Bold(true).
		Padding(0, 1).
		Render(fmt.Sprintf("[PASTED %d lines]", m.pasteLines))
	if prefix == "" {
		return badge
	}
	return m.input.View() + " " + badge
}

// resize updates component dimensions based on terminal size
func (m *model) resize() {
	w := max(20, m.width-6)
	h := max(5, m.height-16)
	m.viewport.Width = w
	m.viewport.Height = h
	m.sessions.SetSize(w, h+4)
	m.workspaces.SetSize(w, h+4)
	m.contextBar.Width = min(28, max(10, w/4))
	m.markdown.Resize(w)
}

func (m *model) renderContent(role, content string) string {
	if role == "assistant" {
		return m.markdown.Render(content, m.viewport.Width)
	}
	return wrapText(content, m.viewport.Width)
}

// ansiHeader returns the ASCII art header
func ansiHeader() string {
	return ` __      __          _______________.__    .____  _______       .___________  
/  \    /  \ ____   /  |  \____    /|  |   |   _| \   _  \    __| _/\_____  \ 
\   \/\/   // __ \ /   |  |_/     / |  |   |  |   /  /_\  \  / __ |   _(__  < 
 \        /\  ___//    ^   /     /_ |  |__ |  |   \  \_/   \/ /_/ |  /       \
  \__/\  /  \___  >____   /_______ \|____/ |  |_   \_____  /\____ | /______  /
       \/       \/     |__|       \/       |____|        \/      \/        \/ `
}

// trimTitle shortens a title to fit display constraints
func trimTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 48 {
		return s
	}
	return s[:45] + "..."
}

// wrapText wraps text to fit within the specified width, preserving paragraphs
func wrapText(s string, width int) string {
	width = max(10, width)
	var out strings.Builder
	paragraphs := strings.Split(s, "\n")
	for i, paragraph := range paragraphs {
		if paragraph == "" {
			if i < len(paragraphs)-1 {
				out.WriteByte('\n')
			}
			continue
		}
		out.WriteString(wrapLine(paragraph, width))
		if i < len(paragraphs)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// wrapLine wraps a single line of text to fit within the specified width
func wrapLine(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var out strings.Builder
	lineWidth := 0
	for _, word := range words {
		wordWidth := lipgloss.Width(word)
		if lineWidth > 0 && lineWidth+1+wordWidth > width {
			out.WriteByte('\n')
			lineWidth = 0
		}
		if lineWidth > 0 {
			out.WriteByte(' ')
			lineWidth++
		}
		if wordWidth <= width {
			out.WriteString(word)
			lineWidth += wordWidth
			continue
		}
		for _, r := range word {
			rw := lipgloss.Width(string(r))
			if lineWidth > 0 && lineWidth+rw > width {
				out.WriteByte('\n')
				lineWidth = 0
			}
			out.WriteRune(r)
			lineWidth += rw
		}
	}
	return out.String()
}

// estimateMessages estimates total tokens for a slice of messages
func estimateMessages(messages []storage.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateTokens(msg.Content)
		total += estimateTokens(msg.ToolCalls)
	}
	return total
}

// estimateTokens provides a rough token count estimate for text
func estimateTokens(s string) int {
	words := len(strings.Fields(s))
	if words == 0 {
		return 0
	}
	return max(1, int(float64(words)*1.33))
}

// countLines counts the number of lines in a string
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// limitToolOutput truncates tool output to the specified maximum character count
func limitToolOutput(s string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 12000
	}
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + fmt.Sprintf("\n\n[truncated: %d chars omitted]", len(s)-maxChars)
}

// Made with Bob
