package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/tools"
)

func (m model) executeTools(inputTokens, outputTokens int) (tea.Model, tea.Cmd) {
	toolCallsJSON, _ := json.Marshal(m.pendingTools)
	if err := m.store.AddMessageWithTools(m.session.ID, "assistant", strings.TrimSpace(m.streamText), string(toolCallsJSON), ""); err != nil {
		m.err = err.Error()
		return m, nil
	}
	if len(m.toolApprovalCalls()) > 0 {
		m.pendingToolInput = inputTokens
		m.pendingToolOutput = outputTokens
		m.thinking = false
		m.mode = modeToolApproval
		m.status = "approve tool calls"
		m.renderMessages()
		return m, nil
	}
	return m.runPendingTools(inputTokens, outputTokens, true)
}

func (m model) runPendingTools(inputTokens, outputTokens int, approved bool) (tea.Model, tea.Cmd) {
	m.toolResults = make([]string, 0, len(m.pendingTools))
	for _, call := range m.pendingTools {
		tool, ok := m.toolRegistry.Get(call.Function.Name)
		if !ok {
			result := fmt.Sprintf("Tool %q not found", call.Function.Name)
			m.logToolCall(call.ID, call.Function.Name, tools.SafetyLevelSafe, call.Function.Arguments, "", result, time.Duration(0), false)
			m.toolResults = append(m.toolResults, result)
			if err := m.store.AddMessageWithTools(m.session.ID, "tool", result, "", call.ID); err != nil {
				m.err = err.Error()
			}
			continue
		}

		if !approved {
			result := fmt.Sprintf("Tool %q rejected by user", call.Function.Name)
			m.logToolCall(call.ID, call.Function.Name, tool.SafetyLevel(), call.Function.Arguments, "", result, time.Duration(0), false)
			m.toolResults = append(m.toolResults, result)
			if err := m.store.AddMessageWithTools(m.session.ID, "tool", result, "", call.ID); err != nil {
				m.err = err.Error()
			}
			continue
		}

		var args map[string]any
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			result := fmt.Sprintf("Failed to parse arguments: %v", err)
			m.logToolCall(call.ID, call.Function.Name, tool.SafetyLevel(), call.Function.Arguments, "", result, time.Duration(0), false)
			m.toolResults = append(m.toolResults, result)
			if err := m.store.AddMessageWithTools(m.session.ID, "tool", result, "", call.ID); err != nil {
				m.err = err.Error()
			}
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		start := time.Now()
		result, err := tool.Execute(ctx, args)
		duration := time.Since(start)
		cancel()
		success := err == nil
		if err != nil {
			result = fmt.Sprintf("Tool error: %v", err)
		}
		result = limitToolOutput(result, m.cfg.Tools.MaxOutputChars)
		m.logToolCall(call.ID, call.Function.Name, tool.SafetyLevel(), call.Function.Arguments, args, result, duration, success)

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
	m.pendingToolInput = 0
	m.pendingToolOutput = 0

	return m, tea.Batch(m.startStream(ch, "", contextHistory), waitStream(ch), m.working.Tick)
}

func (m model) approveToolCalls() (tea.Model, tea.Cmd) {
	m.mode = modeChat
	m.status = "tools approved"
	m.input.Focus()
	return m.runPendingTools(m.pendingToolInput, m.pendingToolOutput, true)
}

func (m model) rejectToolCalls() (tea.Model, tea.Cmd) {
	m.mode = modeChat
	m.status = "tools rejected"
	m.input.Focus()
	return m.runPendingTools(m.pendingToolInput, m.pendingToolOutput, false)
}

func (m model) toolApprovalCalls() []llm.ToolCall {
	if m.cfg.Tools.AutoExecute {
		return nil
	}
	var calls []llm.ToolCall
	for _, call := range m.pendingTools {
		tool, ok := m.toolRegistry.Get(call.Function.Name)
		if !ok || tool.SafetyLevel() == tools.SafetyLevelSafe {
			continue
		}
		calls = append(calls, call)
	}
	return calls
}

type toolCallLog struct {
	Time       string `json:"time"`
	SessionID  string `json:"session_id"`
	CallID     string `json:"call_id"`
	Tool       string `json:"tool"`
	Safety     string `json:"safety"`
	ArgsRaw    string `json:"args_raw,omitempty"`
	Args       any    `json:"args,omitempty"`
	Result     string `json:"result"`
	DurationMS int64  `json:"duration_ms"`
	Success    bool   `json:"success"`
	Project    string `json:"project"`
}

func (m model) logToolCall(callID, name string, safety tools.SafetyLevel, rawArgs string, args any, result string, duration time.Duration, success bool) {
	if m.project.LogDir == "" {
		return
	}
	if err := os.MkdirAll(m.project.LogDir, 0o700); err != nil {
		return
	}
	entry := toolCallLog{
		Time:       time.Now().Format(time.RFC3339Nano),
		SessionID:  m.session.ID,
		CallID:     callID,
		Tool:       name,
		Safety:     safetyLabel(safety),
		ArgsRaw:    truncateLogText(rawArgs),
		Args:       args,
		Result:     truncateLogText(result),
		DurationMS: duration.Milliseconds(),
		Success:    success,
		Project:    m.project.Root,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := filepath.Join(m.project.LogDir, "tool_calls.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func safetyLabel(level tools.SafetyLevel) string {
	switch level {
	case tools.SafetyLevelSafe:
		return "safe"
	case tools.SafetyLevelPrompt:
		return "prompt"
	case tools.SafetyLevelDangerous:
		return "dangerous"
	default:
		return "unknown"
	}
}

func truncateLogText(s string) string {
	const limit = 4000
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("\n[log truncated: %d chars omitted]", len(s)-limit)
}
