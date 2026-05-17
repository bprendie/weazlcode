package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type editorLogEntry struct {
	Time     string   `json:"time"`
	Session  string   `json:"session_id"`
	Command  string   `json:"command"`
	Args     []string `json:"args,omitempty"`
	Path     string   `json:"path"`
	Line     string   `json:"line,omitempty"`
	Wait     bool     `json:"wait"`
	Success  bool     `json:"success"`
	Error    string   `json:"error,omitempty"`
	Output   string   `json:"output,omitempty"`
	ExitCode int      `json:"exit_code,omitempty"`
}

func (m model) openExternalEditorCommand(raw string) (tea.Model, tea.Cmd, bool) {
	path, line, ok := splitEditorArgs(raw)
	if !ok {
		m.addSystemNote("Usage: /edit <path> [line]")
		m.status = "editor usage"
		return m, nil, true
	}
	clean, err := cleanPreviewPath(path)
	if err != nil {
		m.addSystemNote("Editor error: " + err.Error())
		m.status = "editor failed"
		return m, nil, true
	}
	fullPath := filepath.Join(m.project.Root, filepath.FromSlash(clean))
	if info, err := os.Stat(fullPath); err != nil {
		m.addSystemNote("Editor error: " + err.Error())
		m.status = "editor failed"
		return m, nil, true
	} else if info.IsDir() {
		m.addSystemNote("Editor error: path is a directory")
		m.status = "editor failed"
		return m, nil, true
	}
	command := strings.TrimSpace(m.cfg.Editor.Command)
	if command == "" {
		command = strings.TrimSpace(os.Getenv("VISUAL"))
	}
	if command == "" {
		command = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if command == "" {
		m.addSystemNote("Editor error: configure editor.command or set VISUAL/EDITOR.")
		m.status = "editor not configured"
		return m, nil, true
	}
	args := editorArgs(m.cfg.Editor.Args, clean, line)
	cmd := exec.Command(command, args...)
	cmd.Dir = m.project.Root
	entry := editorLogEntry{
		Time:     nowRFC3339Nano(),
		Session:  m.session.ID,
		Command:  command,
		Args:     args,
		Path:     clean,
		Line:     line,
		Wait:     m.cfg.Editor.Wait,
		Success:  true,
		ExitCode: 0,
	}
	if m.cfg.Editor.Wait {
		out, err := cmd.CombinedOutput()
		entry.Output = truncateLogText(string(out))
		if err != nil {
			entry.Success = false
			entry.Error = err.Error()
			if exitErr, ok := err.(*exec.ExitError); ok {
				entry.ExitCode = exitErr.ExitCode()
			}
			m.logEditor(entry)
			m.addSystemNote("Editor error: " + err.Error())
			m.status = "editor failed"
			return m, nil, true
		}
		m.logEditor(entry)
		m.addSystemNote(fmt.Sprintf("Editor closed %s.", clean))
		m.status = "editor closed"
		return m, nil, true
	}
	if err := cmd.Start(); err != nil {
		entry.Success = false
		entry.Error = err.Error()
		m.logEditor(entry)
		m.addSystemNote("Editor error: " + err.Error())
		m.status = "editor failed"
		return m, nil, true
	}
	entry.ExitCode = -1
	m.logEditor(entry)
	m.addSystemNote(fmt.Sprintf("Opened %s in external editor.", clean))
	m.status = "editor opened"
	return m, nil, true
}

func splitEditorArgs(raw string) (path, line string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", "", false
	}
	path = fields[0]
	if len(fields) > 1 {
		line = strings.TrimPrefix(strings.TrimSpace(fields[1]), "L")
		if _, err := strconv.Atoi(line); err != nil {
			return "", "", false
		}
	}
	return path, line, true
}

func editorArgs(configured []string, path, line string) []string {
	if len(configured) == 0 {
		if line != "" {
			return []string{"+" + line, path}
		}
		return []string{path}
	}
	args := make([]string, 0, len(configured))
	for _, arg := range configured {
		arg = strings.ReplaceAll(arg, "{file}", path)
		arg = strings.ReplaceAll(arg, "{line}", line)
		args = append(args, arg)
	}
	return args
}

func (m model) logEditor(entry editorLogEntry) {
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
	path := filepath.Join(m.project.LogDir, "editor.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func nowRFC3339Nano() string {
	return time.Now().Format(time.RFC3339Nano)
}
