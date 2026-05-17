package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/config"
)

type debugLogEntry struct {
	Time       string   `json:"time"`
	SessionID  string   `json:"session_id"`
	Name       string   `json:"name"`
	Adapter    string   `json:"adapter"`
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	Request    string   `json:"request"`
	Program    string   `json:"program,omitempty"`
	Cwd        string   `json:"cwd,omitempty"`
	Success    bool     `json:"success"`
	DurationMS int64    `json:"duration_ms"`
	Output     string   `json:"output,omitempty"`
	Error      string   `json:"error,omitempty"`
	ExitCode   int      `json:"exit_code,omitempty"`
}

type debugLaunchPayload struct {
	Seq       int                    `json:"seq"`
	Type      string                 `json:"type"`
	Command   string                 `json:"command"`
	Arguments debugLaunchPayloadArgs `json:"arguments"`
}

type debugLaunchPayloadArgs struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Request string   `json:"request"`
	Program string   `json:"program,omitempty"`
	Args    []string `json:"args,omitempty"`
	Cwd     string   `json:"cwd"`
}

func (m model) handleDebugCommand(raw string) (tea.Model, tea.Cmd, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		m.setIDEView("debug", m.debugCommandText())
		return m, nil, true
	}
	if strings.HasPrefix(raw, "launch ") {
		return m.launchDebugConfiguration(strings.TrimSpace(strings.TrimPrefix(raw, "launch ")))
	}
	m.addSystemNote("Usage: /debug or /debug launch <name>")
	m.status = "debug usage"
	return m, nil, true
}

func (m model) debugCommandText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Debug:\nadapters: %d\nconfigurations: %d\ntimeout_seconds: %d\n", len(m.cfg.Debug.Adapters), len(m.cfg.Debug.Configurations), m.debugTimeoutSeconds())
	if len(m.cfg.Debug.Adapters) == 0 && len(m.cfg.Debug.Configurations) == 0 {
		b.WriteString("\nNo debug adapters configured.")
		return b.String()
	}
	if len(m.cfg.Debug.Adapters) > 0 {
		b.WriteString("\nAdapters:\n")
		for name, adapter := range m.cfg.Debug.Adapters {
			fmt.Fprintf(&b, "- %s: %s %s\n", name, adapter.Command, strings.Join(adapter.Args, " "))
		}
	}
	if len(m.cfg.Debug.Configurations) > 0 {
		b.WriteString("\nConfigurations:\n")
		for _, cfg := range m.cfg.Debug.Configurations {
			request := emptyFallback(cfg.Request, "launch")
			cwd := emptyFallback(cfg.Cwd, ".")
			program := emptyFallback(cfg.Program, "not set")
			fmt.Fprintf(&b, "- %s [%s/%s] program=%s cwd=%s\n", cfg.Name, cfg.Type, request, program, cwd)
		}
		b.WriteString("\nUse `/debug launch <name>` to run a launch handshake.")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) launchDebugConfiguration(name string) (tea.Model, tea.Cmd, bool) {
	if strings.TrimSpace(name) == "" {
		m.addSystemNote("Usage: /debug launch <name>")
		m.status = "debug usage"
		return m, nil, true
	}
	debugCfg, ok := m.findDebugConfiguration(name)
	if !ok {
		m.addSystemNote(fmt.Sprintf("Debug error: configuration %q was not found.", name))
		m.status = "debug failed"
		return m, nil, true
	}
	if strings.TrimSpace(debugCfg.Type) == "" {
		m.addSystemNote(fmt.Sprintf("Debug error: configuration %q is missing type.", debugCfg.Name))
		m.status = "debug failed"
		return m, nil, true
	}
	adapter, ok := m.cfg.Debug.Adapters[debugCfg.Type]
	if !ok || strings.TrimSpace(adapter.Command) == "" {
		m.addSystemNote(fmt.Sprintf("Debug error: adapter %q is not configured.", debugCfg.Type))
		m.status = "debug failed"
		return m, nil, true
	}
	cwd, err := m.debugWorkingDirectory(debugCfg)
	if err != nil {
		m.addSystemNote("Debug error: " + err.Error())
		m.status = "debug failed"
		return m, nil, true
	}
	program, err := m.debugProgram(debugCfg)
	if err != nil {
		m.addSystemNote("Debug error: " + err.Error())
		m.status = "debug failed"
		return m, nil, true
	}
	request := emptyFallback(debugCfg.Request, "launch")
	payload := debugLaunchPayload{
		Seq:     1,
		Type:    "request",
		Command: request,
		Arguments: debugLaunchPayloadArgs{
			Name:    debugCfg.Name,
			Type:    debugCfg.Type,
			Request: request,
			Program: program,
			Args:    debugCfg.Args,
			Cwd:     cwd,
		},
	}
	input, err := json.Marshal(payload)
	if err != nil {
		m.addSystemNote("Debug error: " + err.Error())
		m.status = "debug failed"
		return m, nil, true
	}
	timeout := time.Duration(m.debugTimeoutSeconds()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, adapter.Command, adapter.Args...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(dapFrame(input))
	start := time.Now()
	out, err := cmd.CombinedOutput()
	entry := debugLogEntry{
		Time:       nowRFC3339Nano(),
		SessionID:  m.session.ID,
		Name:       debugCfg.Name,
		Adapter:    debugCfg.Type,
		Command:    adapter.Command,
		Args:       adapter.Args,
		Request:    request,
		Program:    program,
		Cwd:        cwd,
		Success:    err == nil && ctx.Err() == nil,
		DurationMS: time.Since(start).Milliseconds(),
		Output:     truncateLogText(string(out)),
		ExitCode:   0,
	}
	if ctx.Err() != nil {
		entry.Success = false
		entry.Error = ctx.Err().Error()
		entry.ExitCode = -1
	} else if err != nil {
		entry.Error = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			entry.ExitCode = exitErr.ExitCode()
		}
	}
	m.logDebug(entry)
	if !entry.Success {
		m.addSystemNote("Debug launch failed: " + emptyFallback(entry.Error, "adapter exited unsuccessfully"))
		m.status = "debug failed"
		return m, nil, true
	}
	m.addSystemNote(fmt.Sprintf("Debug launch completed for %s.", debugCfg.Name))
	m.status = "debug launched"
	return m, nil, true
}

func (m model) findDebugConfiguration(name string) (config.DebugConfiguration, bool) {
	for _, debugCfg := range m.cfg.Debug.Configurations {
		if strings.EqualFold(strings.TrimSpace(debugCfg.Name), strings.TrimSpace(name)) {
			return debugCfg, true
		}
	}
	return config.DebugConfiguration{}, false
}

func (m model) debugWorkingDirectory(debugCfg config.DebugConfiguration) (string, error) {
	if strings.TrimSpace(debugCfg.Cwd) == "" {
		return m.project.Root, nil
	}
	clean, err := cleanPreviewPath(debugCfg.Cwd)
	if err != nil {
		return "", err
	}
	full := filepath.Join(m.project.Root, filepath.FromSlash(clean))
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd is not a directory")
	}
	return full, nil
}

func (m model) debugProgram(debugCfg config.DebugConfiguration) (string, error) {
	if strings.TrimSpace(debugCfg.Program) == "" {
		return "", nil
	}
	clean, err := cleanPreviewPath(debugCfg.Program)
	if err != nil {
		return "", err
	}
	full := filepath.Join(m.project.Root, filepath.FromSlash(clean))
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("program is a directory")
	}
	return full, nil
}

func (m model) debugTimeoutSeconds() int {
	if m.cfg.Debug.TimeoutSeconds <= 0 {
		return 30
	}
	return m.cfg.Debug.TimeoutSeconds
}

func dapFrame(payload []byte) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

func (m model) logDebug(entry debugLogEntry) {
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
	path := filepath.Join(m.project.LogDir, "debug.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
