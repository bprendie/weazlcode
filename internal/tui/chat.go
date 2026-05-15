package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/storage"
)

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeVault:
		pw := m.input.Value()
		if pw == "" {
			return m, nil
		}
		has, err := m.store.HasVault()
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		if has {
			err = m.store.Unlock(pw)
		} else {
			err = m.store.CreateVault(pw)
		}
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.err = ""
		m.input.Reset()
		m.input.EchoMode = textinput.EchoNormal
		m.input.Placeholder = m.cfg.Active().ServerURL
		m.mode = modeServer
		m.status = "confirm server url"
	case modeServer:
		if v := strings.TrimSpace(m.input.Value()); v != "" {
			p := m.cfg.Active()
			p.ServerURL = v
			m.cfg.Providers[m.cfg.ActiveProvider] = p
			if err := config.Save(m.cfgPath, m.cfg); err != nil {
				m.err = err.Error()
				return m, nil
			}
		}
		m.input.Reset()
		m.input.Placeholder = "message " + m.cfg.ProviderForRole("orchestrator").Model
		return m.startChat()
	case modeSessions:
		item, ok := m.sessions.SelectedItem().(sessionItem)
		if ok {
			return m.loadSession(item.session)
		}
	case modeWorkspace:
		item, ok := m.workspaces.SelectedItem().(workspaceItem)
		if ok {
			return m.loadWorkspace(storage.WorkspaceSave(item))
		}
	case modeRenameWorkspace:
		return m.finishRenameWorkspace()
	case modeClearContext:
		return m.confirmClearContext()
	case modeToolApproval:
		return m.approveToolCalls()
	case modeChat, modeIDEView:
		if m.thinking {
			prompt := strings.TrimSpace(m.chatPrompt())
			if strings.HasPrefix(prompt, "/cancel") {
				updated, cmd, _ := m.handleSlashCommand(prompt)
				return updated, cmd
			}
			return m, nil
		}
		prompt := strings.TrimSpace(m.chatPrompt())
		if prompt == "" {
			return m, nil
		}
		if updated, cmd, handled := m.handleSlashCommand(prompt); handled {
			return updated, cmd
		}
		if m.mode == modeIDEView {
			m.mode = modeChat
			m.renderMessages()
		}
		if shouldAutoGeneratePlan(prompt) {
			m.input.Reset()
			m.pasteText = ""
			m.pasteLines = 0
			m.historyIdx = 0
			m.historyDraft = ""
			m.addSystemNote("Coding request detected. Generating a bounded worker plan instead of asking the planning model to write code directly. Review and approve the plan, then run `/run-workers`.")
			updated, cmd, _ := m.generatePlanCommand(prompt)
			return updated, cmd
		}
		m.input.Reset()
		m.pasteText = ""
		m.pasteLines = 0
		m.historyIdx = 0
		m.historyDraft = ""
		if err := m.store.AddMessage(m.session.ID, "user", prompt); err != nil {
			m.err = err.Error()
			return m, nil
		}
		title := m.session.Title
		if title == "New session" {
			title = trimTitle(prompt)
			m.session.Title = title
		}
		_ = m.store.TouchSession(m.session.ID, title)
		history, err := m.store.Messages(m.session.ID)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.messages = history
		m.renderMessages()
		currentPromptID := history[len(history)-1].ID
		if m.shouldAutoTrim(history) {
			return m.trimContext(true, prompt, currentPromptID, history[len(history)-2].ID)
		}
		contextHistory, err := m.contextHistoryForPrompt(currentPromptID)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.thinking = true
		m.working.Spinner = spinner.Jump
		m.streamText = ""
		m.streamAt = time.Now()
		modelPrompt := m.orchestratorPrompt(prompt)
		m.reqIn = estimateMessages(contextHistory) + estimateTokens(modelPrompt)
		m.reqOut = 0
		m.err = ""
		m.status = "streaming orchestrator"
		ch := make(chan streamEvent, 64)
		m.stream = ch
		return m, tea.Batch(m.startStream(ch, modelPrompt, contextHistory), waitStream(ch), m.working.Tick)
	}
	return m, nil
}

func (m model) orchestratorPrompt(prompt string) string {
	var sections []string
	worker := m.cfg.ProviderForRole("worker")
	profile := m.workerCapacityProfile()
	sections = append(sections, fmt.Sprintf("WeazlCode execution model:\n- Planning/review runs through the orchestrator/reviewer roles.\n- Code changes should be broken into bounded local worker tasks when implementation is requested.\n- Configured worker: %s/%s (%s).\n- Worker constraint: %s",
		worker.Type,
		worker.Model,
		profile.Label,
		profile.Instruction,
	))
	if instructions, ok, err := project.LoadInstructions(m.project.Root); err == nil && ok && strings.TrimSpace(instructions.Content) != "" {
		sections = append(sections, "Project instructions from "+project.PrimaryInstructionsFile+":\n"+strings.TrimSpace(instructions.Content))
	}
	if memories, err := m.store.ProjectMemories(m.project.Root, 10); err == nil && len(memories) > 0 {
		var b strings.Builder
		b.WriteString("Project memory:")
		for _, memory := range memories {
			fmt.Fprintf(&b, "\n- %s: %s", memory.Key, memory.Value)
		}
		sections = append(sections, b.String())
	}
	return strings.Join(sections, "\n\n") + "\n\nUser request:\n" + prompt
}

func shouldAutoGeneratePlan(prompt string) bool {
	text := " " + strings.ToLower(strings.TrimSpace(prompt)) + " "
	if strings.TrimSpace(text) == "" {
		return false
	}
	action := []string{
		" add ", " build ", " change ", " code ", " create ", " delete ", " edit ", " fix ",
		" implement ", " refactor ", " remove ", " rename ", " replace ", " update ", " write ",
	}
	hasAction := false
	for _, marker := range action {
		if strings.Contains(text, marker) {
			hasAction = true
			break
		}
	}
	if !hasAction {
		return false
	}
	readOnly := []string{
		" explain ", " describe ", " summarize ", " inspect ", " look at ", " analyze ", " review ",
		" what ", " why ", " how ", " where ", " list ", " show ", " find ", " search ",
	}
	for _, marker := range readOnly {
		if strings.Contains(text, marker) && !strings.Contains(text, " and ") && !strings.Contains(text, " then ") {
			return false
		}
	}
	target := []string{
		" app", " bug", " code", " component", " endpoint", " feature", " file", " function",
		" handler", " module", " package", " page", " repo", " route", " script", " test", " ui",
	}
	for _, marker := range target {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return strings.Contains(text, " in ") || strings.Contains(text, " under ") || strings.Contains(text, " for ")
}

func (m model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if msg.Paste || looksLikePaste(msg) {
		m.addPaste(string(msg.Runes))
		return m, nil, true
	}
	switch msg.String() {
	case "up":
		return m.recallHistory(-1), nil, true
	case "down":
		return m.recallHistory(1), nil, true
	case "backspace", "ctrl+h":
		if m.pasteText != "" && m.input.Value() == "" {
			m.pasteText = ""
			m.pasteLines = 0
			m.status = "paste cleared"
			return m, nil, true
		}
	case "ctrl+u":
		if m.pasteText != "" {
			m.pasteText = ""
			m.pasteLines = 0
			m.input.Reset()
			m.status = "input cleared"
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m model) startClearContextConfirm() (tea.Model, tea.Cmd) {
	m.mode = modeClearContext
	m.status = "clear context?"
	m.err = ""
	return m, nil
}

func (m model) cancelClearContext() (tea.Model, tea.Cmd) {
	m.mode = modeChat
	m.status = "clear context canceled"
	m.input.Focus()
	return m, nil
}

func (m model) confirmClearContext() (tea.Model, tea.Cmd) {
	if m.session.ID == "" {
		return m.cancelClearContext()
	}
	if err := m.store.ClearSessionContext(m.session.ID); err != nil {
		m.err = err.Error()
		m.mode = modeChat
		return m, nil
	}
	m.messages = nil
	m.checkpoint = storage.ContextCheckpoint{}
	m.hasCheckpoint = false
	m.pendingTools = nil
	m.toolResults = nil
	m.streamText = ""
	m.reqIn = 0
	m.reqOut = 0
	m.session.InputTokens = 0
	m.session.OutputTokens = 0
	m.session.Title = "New session"
	m.activeWorkspaceID = 0
	m.activeWorkspaceName = ""
	m.activeWorkspaceAt = time.Time{}
	m.historyIdx = 0
	m.historyDraft = ""
	m.pasteText = ""
	m.pasteLines = 0
	m.input.Reset()
	m.mode = modeChat
	m.status = "context cleared"
	m.err = ""
	m.input.Focus()
	m.renderMessages()
	return m, nil
}

func (m model) toggleMouseMode() (tea.Model, tea.Cmd) {
	m.mouseScroll = !m.mouseScroll
	if m.mouseScroll {
		m.status = "mouse scroll enabled"
		return m, tea.EnableMouseCellMotion
	}
	m.status = "copy mode enabled"
	return m, tea.DisableMouse
}

func (m model) recallHistory(delta int) model {
	history := m.userPrompts()
	if len(history) == 0 {
		return m
	}
	if m.historyIdx == 0 {
		m.historyDraft = m.input.Value()
	}
	next := m.historyIdx + delta
	if next < -len(history) {
		next = -len(history)
	}
	if next > 0 {
		next = 0
	}
	m.historyIdx = next
	if m.historyIdx == 0 {
		m.input.SetValue(m.historyDraft)
		m.historyDraft = ""
		return m
	}
	m.pasteText = ""
	m.pasteLines = 0
	m.input.SetValue(history[len(history)+m.historyIdx])
	m.input.CursorEnd()
	return m
}

func (m model) userPrompts() []string {
	prompts := make([]string, 0, len(m.messages))
	for _, msg := range m.messages {
		if msg.Role == "user" {
			prompts = append(prompts, msg.Content)
		}
	}
	return prompts
}

func (m *model) addPaste(s string) {
	if s == "" {
		return
	}
	if m.pasteText == "" {
		m.pasteText = s
	} else {
		m.pasteText += "\n" + s
	}
	m.pasteLines = countLines(m.pasteText)
	m.historyIdx = 0
	m.historyDraft = ""
	m.status = fmt.Sprintf("captured paste: %d lines", m.pasteLines)
}

func (m model) chatPrompt() string {
	prefix := strings.TrimSpace(m.input.Value())
	if m.pasteText == "" {
		return prefix
	}
	if prefix == "" {
		return m.pasteText
	}
	return prefix + "\n\n" + m.pasteText
}
