package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/project"
	"github.com/bprendie/weazlcode/internal/storage"
	"github.com/bprendie/weazlcode/internal/tools"
)

type mode int

const (
	modeVault mode = iota
	modeServer
	modeLoading
	modeChat
	modeSessions
	modeWorkspace
	modeRenameWorkspace
	modeClearContext
)

type model struct {
	cfg                 config.Config
	cfgPath             string
	project             project.Summary
	store               *storage.Store
	toolRegistry        *tools.Registry
	styles              styles
	mode                mode
	width               int
	height              int
	input               textinput.Model
	viewport            viewport.Model
	markdown            markdownRenderer
	sessions            list.Model
	workspaces          list.Model
	working             spinner.Model
	contextBar          progress.Model
	activeWorkspaceID   int64
	activeWorkspaceName string
	activeWorkspaceAt   time.Time
	renameWorkspaceID   int64
	renameReturnMode    mode
	renameDraft         string
	session             storage.Session
	messages            []storage.Message
	checkpoint          storage.ContextCheckpoint
	hasCheckpoint       bool
	err                 string
	status              string
	thinking            bool
	trimming            bool
	mouseScroll         bool
	stream              <-chan streamEvent
	streamText          string
	streamAt            time.Time
	reqIn               int
	reqOut              int
	pasteText           string
	pasteLines          int
	historyIdx          int
	historyDraft        string
	pendingTools        []llm.ToolCall
	toolResults         []string
}

type streamEvent struct {
	eventType string // "content", "tool_call", "done"
	chunk     string
	toolCalls []llm.ToolCall
	usage     llm.Usage
	err       error
	done      bool
}

type contextTrimMsg struct {
	auto            bool
	prompt          string
	currentPromptID int64
	throughID       int64
	summary         string
	err             error
}

func New(cfg config.Config, cfgPath string, store *storage.Store, toolRegistry *tools.Registry, projectSummary project.Summary) tea.Model {
	ti := textinput.New()
	ti.Placeholder = "database password"
	ti.EchoMode = textinput.EchoPassword
	ti.Focus()
	ti.CharLimit = 65535

	sessions := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	sessions.Title = "Sessions"
	workspaces := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	workspaces.Title = "Workspace Saves"

	s := newStyles()
	working := spinner.New(
		spinner.WithSpinner(spinner.Jump),
		spinner.WithStyle(s.assistant),
	)
	contextBar := progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage())

	return model{
		cfg:          cfg,
		cfgPath:      cfgPath,
		project:      projectSummary,
		store:        store,
		toolRegistry: toolRegistry,
		styles:       s,
		mode:         modeVault,
		input:        ti,
		viewport:     viewport.New(0, 0),
		markdown:     markdownRenderer{enabled: cfg.UI.MarkdownEnabled(), style: cfg.UI.MarkdownStyle},
		sessions:     sessions,
		workspaces:   workspaces,
		working:      working,
		contextBar:   contextBar,
		mouseScroll:  true,
		status:       "project " + projectSummary.StatusLabel(),
	}
}

func (m model) Init() tea.Cmd {
	has, err := m.store.HasVault()
	if err != nil {
		m.err = err.Error()
	}
	if !has {
		m.input.Placeholder = "create database password"
		m.status = "create encrypted local history"
		return textinput.Blink
	}
	m.status = "unlock encrypted local history"
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		if m.mode == modeChat && !m.thinking {
			m.renderMessages()
		}
	case tea.MouseMsg:
		if m.mode == modeChat {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	case tea.KeyMsg:
		if m.mode == modeChat && !m.thinking {
			if updated, cmd, handled := m.handleChatKey(msg); handled {
				return updated, cmd
			}
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+n":
			if m.mode == modeChat {
				return m.newSession()
			}
		case "ctrl+s":
			if m.mode == modeChat {
				m.saveWorkspace()
			}
		case "ctrl+t":
			if m.mode == modeChat && !m.thinking {
				return m.trimContext(false, "", 0, 0)
			}
		case "ctrl+u":
			if m.mode == modeChat && !m.thinking {
				return m.startClearContextConfirm()
			}
		case "ctrl+m":
			if m.mode == modeChat {
				return m.toggleMouseMode()
			}
		case "ctrl+r":
			if m.mode == modeChat {
				return m.showWorkspaces()
			}
		case "ctrl+w":
			if m.mode == modeChat {
				return m.showWorkspaces()
			}
		case "ctrl+d":
			if m.mode == modeSessions {
				return m.deleteSelectedSession()
			}
			if m.mode == modeWorkspace {
				return m.deleteSelectedWorkspace()
			}
		case "ctrl+e":
			if (m.mode == modeChat && !m.thinking) || m.mode == modeWorkspace {
				return m.startRenameWorkspace()
			}
		case "pgup":
			if m.mode == modeChat {
				m.viewport.PageUp()
				return m, nil
			}
		case "pgdown":
			if m.mode == modeChat {
				m.viewport.PageDown()
				return m, nil
			}
		case "home":
			if m.mode == modeChat {
				m.viewport.GotoTop()
				return m, nil
			}
		case "end":
			if m.mode == modeChat {
				m.viewport.GotoBottom()
				return m, nil
			}
		case "esc":
			if m.mode == modeSessions || m.mode == modeWorkspace {
				m.mode = modeChat
				m.input.Focus()
			}
			if m.mode == modeRenameWorkspace {
				return m.cancelRenameWorkspace()
			}
			if m.mode == modeClearContext {
				return m.cancelClearContext()
			}
		case "enter":
			return m.handleEnter()
		}
	case streamEvent:
		if msg.eventType == "content" && msg.chunk != "" {
			m.streamText += msg.chunk
			m.reqOut = estimateTokens(m.streamText)
			m.renderMessages()
		}
		if msg.eventType == "tool_call" && len(msg.toolCalls) > 0 {
			m.pendingTools = msg.toolCalls
			m.status = fmt.Sprintf("executing %d tool(s)", len(msg.toolCalls))
			m.renderMessages()
		}
		if !msg.done {
			return m, waitStream(m.stream)
		}
		m.thinking = false
		m.stream = nil
		if msg.err != nil {
			m.err = msg.err.Error()
			m.status = "request failed"
			m.renderMessages()
			return m, nil
		}
		inputTokens := msg.usage.InputTokens
		outputTokens := msg.usage.OutputTokens
		if inputTokens == 0 {
			inputTokens = m.reqIn
		}
		if outputTokens == 0 {
			outputTokens = max(1, estimateTokens(m.streamText))
		}

		// Handle tool calls if present
		if len(m.pendingTools) > 0 && m.cfg.Tools.Enabled {
			return m.executeTools(inputTokens, outputTokens)
		}

		// Save assistant message
		toolCallsJSON := ""
		if len(m.pendingTools) > 0 {
			b, _ := json.Marshal(m.pendingTools)
			toolCallsJSON = string(b)
		}
		if err := m.store.AddMessageWithTools(m.session.ID, "assistant", strings.TrimSpace(m.streamText), toolCallsJSON, ""); err != nil {
			m.err = err.Error()
			return m, nil
		}
		if err := m.store.AddSessionTokens(m.session.ID, inputTokens, outputTokens); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.session.InputTokens += inputTokens
		m.session.OutputTokens += outputTokens
		m.messages, _ = m.store.Messages(m.session.ID)
		m.streamText = ""
		m.pendingTools = nil
		m.toolResults = nil
		m.reqIn = 0
		m.reqOut = 0
		m.renderMessages()
		m.status = "ready"
		return m, nil
	case contextTrimMsg:
		m.trimming = false
		m.thinking = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.status = "context trim failed"
			m.renderMessages()
			return m, nil
		}
		m.err = ""
		m.refreshCheckpoint()
		if msg.auto {
			m.status = "streaming"
			m.thinking = true
			m.working.Spinner = spinner.Jump
			m.streamText = ""
			m.streamAt = time.Now()
			m.reqIn = m.contextTokenEstimate()
			m.reqOut = 0
			ch := make(chan streamEvent, 64)
			m.stream = ch
			history, err := m.contextHistoryForPrompt(msg.currentPromptID)
			if err != nil {
				m.thinking = false
				m.err = err.Error()
				m.status = "request failed"
				return m, nil
			}
			return m, tea.Batch(m.startStream(ch, msg.prompt, history), waitStream(ch), m.working.Tick)
		}
		m.status = fmt.Sprintf("context trimmed through message %d", msg.throughID)
		m.renderMessages()
		return m, nil
	case previousSessionMsg:
		m.thinking = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m.newSession()
		}
		if msg.session.ID == "" {
			return m.newSession()
		}
		m.session = msg.session
		m.messages = msg.messages
		m.checkpoint = msg.checkpoint
		m.hasCheckpoint = msg.hasCheckpoint
		m.activeWorkspaceID = 0
		m.activeWorkspaceName = ""
		m.activeWorkspaceAt = time.Time{}
		m.historyIdx = 0
		m.historyDraft = ""
		m.mode = modeChat
		m.status = "resumed " + msg.session.Title
		m.input.Focus()
		if msg.rendered != "" && msg.renderWidth == m.viewport.Width {
			m.viewport.SetContent(msg.rendered)
			m.viewport.GotoBottom()
		} else {
			m.renderMessages()
		}
		return m, tea.ClearScreen
	case spinner.TickMsg:
		if m.thinking || m.mode == modeLoading {
			var cmd tea.Cmd
			m.working, cmd = m.working.Update(msg)
			if m.thinking {
				m.renderMessages()
			}
			return m, cmd
		}
	}

	switch m.mode {
	case modeSessions:
		var cmd tea.Cmd
		m.sessions, cmd = m.sessions.Update(msg)
		cmds = append(cmds, cmd)
	case modeWorkspace:
		var cmd tea.Cmd
		m.workspaces, cmd = m.workspaces.Update(msg)
		cmds = append(cmds, cmd)
	case modeRenameWorkspace:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	case modeClearContext:
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func looksLikePaste(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes {
		return false
	}
	return len(msg.Runes) > 16 || strings.ContainsRune(string(msg.Runes), '\n')
}
