package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/tools"
)

func (m model) executeTools(inputTokens, outputTokens int) (tea.Model, tea.Cmd) {
	toolCallsJSON, _ := json.Marshal(m.pendingTools)
	if err := m.store.AddMessageWithTools(m.session.ID, "assistant", strings.TrimSpace(m.streamText), string(toolCallsJSON), ""); err != nil {
		m.err = err.Error()
		return m, nil
	}

	m.toolResults = make([]string, 0, len(m.pendingTools))
	for _, call := range m.pendingTools {
		tool, ok := m.toolRegistry.Get(call.Function.Name)
		if !ok {
			result := fmt.Sprintf("Tool %q not found", call.Function.Name)
			m.toolResults = append(m.toolResults, result)
			if err := m.store.AddMessageWithTools(m.session.ID, "tool", result, "", call.ID); err != nil {
				m.err = err.Error()
			}
			continue
		}

		if !m.cfg.Tools.AutoExecute && tool.SafetyLevel() != tools.SafetyLevelSafe {
			result := fmt.Sprintf("Tool %q requires manual approval (auto-execute disabled)", call.Function.Name)
			m.toolResults = append(m.toolResults, result)
			if err := m.store.AddMessageWithTools(m.session.ID, "tool", result, "", call.ID); err != nil {
				m.err = err.Error()
			}
			continue
		}

		var args map[string]any
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			result := fmt.Sprintf("Failed to parse arguments: %v", err)
			m.toolResults = append(m.toolResults, result)
			if err := m.store.AddMessageWithTools(m.session.ID, "tool", result, "", call.ID); err != nil {
				m.err = err.Error()
			}
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result, err := tool.Execute(ctx, args)
		cancel()
		if err != nil {
			result = fmt.Sprintf("Tool error: %v", err)
		}
		result = limitToolOutput(result, m.cfg.Tools.MaxOutputChars)

		m.toolResults = append(m.toolResults, result)
		if err := m.store.AddMessageWithTools(m.session.ID, "tool", result, "", call.ID); err != nil {
			m.err = err.Error()
		}
	}

	if err := m.store.AddSessionTokens(m.session.ID, inputTokens, outputTokens); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.session.InputTokens += inputTokens
	m.session.OutputTokens += outputTokens

	m.messages, _ = m.store.Messages(m.session.ID)
	contextHistory, err := m.contextHistoryForContinuation()
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.streamText = ""
	m.reqIn = estimateMessages(contextHistory)
	m.reqOut = 0
	m.renderMessages()

	m.thinking = true
	m.working.Spinner = spinner.Points
	m.streamAt = time.Now()
	m.status = "processing tool results"
	ch := make(chan streamEvent, 64)
	m.stream = ch
	m.pendingTools = nil

	return m, tea.Batch(m.startStream(ch, "", contextHistory), waitStream(ch), m.working.Tick)
}
