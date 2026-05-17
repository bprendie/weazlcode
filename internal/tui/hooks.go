package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/config"
)

type hookLogEntry struct {
	Time       string         `json:"time"`
	SessionID  string         `json:"session_id"`
	Event      string         `json:"event"`
	Command    string         `json:"command"`
	Args       []string       `json:"args,omitempty"`
	Success    bool           `json:"success"`
	DurationMS int64          `json:"duration_ms"`
	Output     string         `json:"output,omitempty"`
	Error      string         `json:"error,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

func (m model) runHooks(event string, payload map[string]any) {
	if !m.cfg.Hooks.Enabled || strings.TrimSpace(event) == "" {
		return
	}
	commands := m.cfg.Hooks.Events[event]
	if len(commands) == 0 {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["event"] = event
	payload["session_id"] = m.session.ID
	payload["project_root"] = m.project.Root
	payload["timestamp"] = time.Now().Format(time.RFC3339Nano)
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for _, hook := range commands {
		m.runHookCommand(event, hook, payload, body)
	}
}

func (m model) runHookCommand(event string, hook config.HookCommand, payload map[string]any, body []byte) {
	command := strings.TrimSpace(hook.Command)
	if command == "" {
		return
	}
	timeout := time.Duration(m.cfg.Hooks.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, command, hook.Args...)
	if strings.TrimSpace(m.project.Root) != "" {
		cmd.Dir = m.project.Root
	}
	cmd.Stdin = bytes.NewReader(body)
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)
	entry := hookLogEntry{
		Time:       time.Now().Format(time.RFC3339Nano),
		SessionID:  m.session.ID,
		Event:      event,
		Command:    command,
		Args:       hook.Args,
		Success:    err == nil,
		DurationMS: duration.Milliseconds(),
		Output:     truncateLogText(string(out)),
		Payload:    payload,
	}
	if err != nil {
		entry.Error = err.Error()
		if ctx.Err() != nil {
			entry.Error = ctx.Err().Error()
		}
	}
	m.logHook(entry)
}

func (m model) logHook(entry hookLogEntry) {
	if m.project.LogDir == "" {
		return
	}
	if err := os.MkdirAll(m.project.LogDir, 0o700); err != nil {
		return
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := filepath.Join(m.project.LogDir, "hooks.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
