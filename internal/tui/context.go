package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/storage"
)

func (m model) trimContext(auto bool, prompt string, currentPromptID, throughID int64) (tea.Model, tea.Cmd) {
	if m.session.ID == "" || len(m.messages) == 0 {
		m.status = "nothing to trim"
		return m, nil
	}
	if throughID == 0 {
		throughID = m.messages[len(m.messages)-1].ID
	}
	if throughID <= 0 {
		m.status = "nothing to trim"
		return m, nil
	}
	toSummarize := make([]storage.Message, 0, len(m.messages))
	if m.hasCheckpoint {
		if throughID <= m.checkpoint.ThroughMessageID {
			m.status = "context already trimmed"
			return m, nil
		}
		toSummarize = append(toSummarize, storage.Message{
			SessionID: m.session.ID,
			Role:      "system",
			Content:   "Previous checkpoint summary:\n" + m.checkpoint.Summary,
		})
	}
	for _, msg := range m.messages {
		if msg.ID <= throughID && (!m.hasCheckpoint || msg.ID > m.checkpoint.ThroughMessageID) {
			toSummarize = append(toSummarize, msg)
		}
	}
	if len(toSummarize) == 0 {
		m.status = "nothing to trim"
		return m, nil
	}
	m.thinking = true
	m.trimming = true
	m.working.Spinner = contextTrimSpinner
	m.streamText = ""
	m.streamAt = time.Now()
	m.reqIn = estimateMessages(toSummarize)
	m.reqOut = 0
	m.err = ""
	m.status = "trimming context"
	return m, tea.Batch(
		summarizeContext(m.cfg.Active(), m.store, m.session.ID, toSummarize, auto, prompt, currentPromptID, throughID),
		m.working.Tick,
	)
}

func summarizeContext(provider config.Provider, store *storage.Store, sessionID string, messages []storage.Message, auto bool, prompt string, currentPromptID, throughID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		client := llm.New(provider)
		summary, err := client.Summarize(ctx, transcriptForSummary(messages), summaryTargetTokens(provider.ContextWindow))
		if err == nil {
			err = store.SaveContextCheckpoint(sessionID, throughID, summary)
		}
		return contextTrimMsg{
			auto:            auto,
			prompt:          prompt,
			currentPromptID: currentPromptID,
			throughID:       throughID,
			summary:         summary,
			err:             err,
		}
	}
}

func (m *model) refreshCheckpoint() {
	m.checkpoint = storage.ContextCheckpoint{}
	m.hasCheckpoint = false
	if m.session.ID == "" {
		return
	}
	cp, ok, err := m.store.LatestContextCheckpoint(m.session.ID)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.checkpoint = cp
	m.hasCheckpoint = ok
}

func (m model) contextHistoryForPrompt(currentPromptID int64) ([]storage.Message, error) {
	history, err := m.contextHistory()
	if err != nil {
		return nil, err
	}
	filtered := history[:0]
	for _, msg := range history {
		if msg.ID != currentPromptID {
			filtered = append(filtered, msg)
		}
	}
	return filtered, nil
}

func (m model) contextHistoryForContinuation() ([]storage.Message, error) {
	return m.contextHistory()
}

func (m model) contextHistory() ([]storage.Message, error) {
	if !m.hasCheckpoint {
		return append([]storage.Message(nil), m.messages...), nil
	}
	after, err := m.store.MessagesAfter(m.session.ID, m.checkpoint.ThroughMessageID)
	if err != nil {
		return nil, err
	}
	history := make([]storage.Message, 0, len(after)+1)
	history = append(history, storage.Message{
		SessionID: m.session.ID,
		Role:      "system",
		Content:   "Conversation checkpoint summary:\n" + m.checkpoint.Summary,
	})
	history = append(history, after...)
	return history, nil
}

func (m model) shouldAutoTrim(history []storage.Message) bool {
	if len(history) < 2 {
		return false
	}
	return m.contextTokenEstimateFor(history) >= autoCompactThreshold(m.contextBudget())
}

func (m model) contextTokenEstimate() int {
	return m.contextTokenEstimateFor(m.messages)
}

func (m model) contextTokenEstimateFor(messages []storage.Message) int {
	total := 0
	if m.hasCheckpoint {
		total += estimateTokens(m.checkpoint.Summary)
		for _, msg := range messages {
			if msg.ID > m.checkpoint.ThroughMessageID {
				total += estimateTokens(msg.Content)
				total += estimateTokens(msg.ToolCalls)
			}
		}
		return total
	}
	return estimateMessages(messages)
}

func (m model) contextBudget() int {
	if p := m.cfg.Active(); p.ContextWindow > 0 {
		return p.ContextWindow
	}
	return 32768
}

func summaryTargetTokens(contextWindow int) int {
	if contextWindow <= 0 {
		contextWindow = 32768
	}
	return min(6000, max(500, contextWindow/24))
}

func autoCompactThreshold(contextWindow int) int {
	if contextWindow <= 0 {
		contextWindow = 32768
	}
	soft := 0
	switch {
	case contextWindow <= 8192:
		soft = int(float64(contextWindow) * 0.92)
	case contextWindow <= 16384:
		soft = int(float64(contextWindow) * 0.85)
	case contextWindow <= 32768:
		soft = int(float64(contextWindow) * 0.75)
	default:
		soft = min(49152, int(float64(contextWindow)*0.50))
	}
	hard := int(float64(contextWindow) * 0.97)
	return min(soft, hard)
}

func transcriptForSummary(messages []storage.Message) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(strings.ToUpper(msg.Role))
		if msg.ToolCallID != "" {
			b.WriteString(" tool_call_id=")
			b.WriteString(msg.ToolCallID)
		}
		b.WriteString(":\n")
		if msg.Content != "" {
			b.WriteString(msg.Content)
			b.WriteString("\n")
		}
		if msg.ToolCalls != "" {
			b.WriteString("tool_calls: ")
			b.WriteString(msg.ToolCalls)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
