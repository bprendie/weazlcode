package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/llm"
	"github.com/bprendie/weazlcode/internal/storage"
)

func (m model) startStream(ch chan<- streamEvent, prompt string, history []storage.Message) tea.Cmd {
	return func() tea.Msg {
		go func() {
			defer close(ch)
			client := llm.New(m.cfg.ProviderForRole("orchestrator"))

			if m.cfg.Tools.Enabled && m.toolRegistry != nil {
				toolDefs := m.toolRegistry.ToOpenAIFormat()
				if strings.ToLower(m.cfg.ProviderForRole("orchestrator").Type) == "ollama" {
					toolDefs = m.toolRegistry.ToOllamaFormat()
				}
				client = client.WithTools(toolDefs)
			}

			usage, err := client.Stream(context.Background(), history, prompt, func(event llm.StreamEvent) {
				switch event.Type {
				case "content":
					ch <- streamEvent{eventType: "content", chunk: event.Content}
				case "tool_call":
					ch <- streamEvent{eventType: "tool_call", toolCalls: event.ToolCalls}
				}
			})
			ch <- streamEvent{eventType: "done", usage: usage, err: err, done: true}
		}()
		return nil
	}
}

func waitStream(ch <-chan streamEvent) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}
