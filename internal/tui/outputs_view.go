package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
)

type outputEntry struct {
	Time    time.Time
	Source  string
	Title   string
	Summary string
	Detail  string
	Success *bool
}

func (m model) outputsCommandText() string {
	entries := make([]outputEntry, 0, 24)
	if m.store != nil && strings.TrimSpace(m.session.ID) != "" {
		plan, ok, err := m.store.LatestPlan(m.session.ID)
		if err != nil {
			return "Outputs error: " + err.Error()
		}
		if ok {
			for _, task := range plan.Tasks {
				events, err := m.store.TaskEvents(task.ID)
				if err != nil {
					return "Outputs error: " + err.Error()
				}
				entries = append(entries, taskEventOutputEntries(task, events)...)
			}
		}
	}
	toolEntries, err := m.toolLogOutputEntries(20)
	if err != nil {
		entries = append(entries, outputEntry{Source: "tool_log", Title: "Tool log read error", Summary: err.Error()})
	} else {
		entries = append(entries, toolEntries...)
	}
	hookEntries, err := m.hookLogOutputEntries(20)
	if err != nil {
		entries = append(entries, outputEntry{Source: "hook_log", Title: "Hook log read error", Summary: err.Error()})
	} else {
		entries = append(entries, hookEntries...)
	}
	editorEntries, err := m.editorLogOutputEntries(20)
	if err != nil {
		entries = append(entries, outputEntry{Source: "editor_log", Title: "Editor log read error", Summary: err.Error()})
	} else {
		entries = append(entries, editorEntries...)
	}
	debugEntries, err := m.debugLogOutputEntries(20)
	if err != nil {
		entries = append(entries, outputEntry{Source: "debug_log", Title: "Debug log read error", Summary: err.Error()})
	} else {
		entries = append(entries, debugEntries...)
	}
	sortOutputEntries(entries)
	if len(entries) == 0 {
		return "Outputs:\nNo task events or tool outputs yet."
	}
	if len(entries) > 30 {
		entries = entries[:30]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Outputs: %d recent item(s)\n", len(entries))
	for i, entry := range entries {
		status := ""
		if entry.Success != nil {
			if *entry.Success {
				status = " ok"
			} else {
				status = " failed"
			}
		}
		when := ""
		if !entry.Time.IsZero() {
			when = " " + entry.Time.Format("15:04:05")
		}
		fmt.Fprintf(&b, "\n%d. [%s%s]%s %s\n", i+1, entry.Source, status, when, entry.Title)
		if strings.TrimSpace(entry.Summary) != "" {
			fmt.Fprintf(&b, "   %s\n", oneLine(entry.Summary, 160))
		}
		if strings.TrimSpace(entry.Detail) != "" {
			fmt.Fprintf(&b, "%s\n", indentBlock(trimOutputDetail(entry.Detail), "   "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func taskEventOutputEntries(task coding.Task, events []coding.TaskEvent) []outputEntry {
	entries := make([]outputEntry, 0, len(events))
	for _, event := range events {
		if !visibleTaskEvent(event.Type) {
			continue
		}
		detail := ""
		if len(event.Payload) > 0 {
			detail = prettyJSON(event.Payload)
		}
		entries = append(entries, outputEntry{
			Time:    event.CreatedAt,
			Source:  "task",
			Title:   fmt.Sprintf("%s: %s", task.Title, event.Type),
			Summary: event.Message,
			Detail:  detail,
		})
	}
	return entries
}

func visibleTaskEvent(eventType string) bool {
	switch eventType {
	case "task_baseline", "worker_patch", "worker_blocker", "worker_error", "worker_timeout", "worker_json_error", "worker_rejected", "verification", "verification_error", "reviewer_verdict", "review_guardrail", "repair_requested", "repair_start", "repair_limit", "output_cleanup":
		return true
	default:
		return false
	}
}

func (m model) toolLogOutputEntries(limit int) ([]outputEntry, error) {
	if strings.TrimSpace(m.project.LogDir) == "" {
		return nil, nil
	}
	path := filepath.Join(m.project.LogDir, "tool_calls.jsonl")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var logs []toolCallLog
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		var log toolCallLog
		if err := json.Unmarshal(scanner.Bytes(), &log); err == nil {
			logs = append(logs, log)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}
	entries := make([]outputEntry, 0, len(logs))
	for _, log := range logs {
		t, _ := time.Parse(time.RFC3339Nano, log.Time)
		success := log.Success
		entries = append(entries, outputEntry{
			Time:    t,
			Source:  "tool",
			Title:   log.Tool,
			Summary: fmt.Sprintf("%s in %dms", log.Safety, log.DurationMS),
			Detail:  log.Result,
			Success: &success,
		})
	}
	return entries, nil
}

func (m model) hookLogOutputEntries(limit int) ([]outputEntry, error) {
	if strings.TrimSpace(m.project.LogDir) == "" {
		return nil, nil
	}
	path := filepath.Join(m.project.LogDir, "hooks.jsonl")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var logs []hookLogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		var log hookLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &log); err == nil {
			logs = append(logs, log)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}
	entries := make([]outputEntry, 0, len(logs))
	for _, log := range logs {
		t, _ := time.Parse(time.RFC3339Nano, log.Time)
		success := log.Success
		detail := log.Output
		if strings.TrimSpace(log.Error) != "" {
			detail = strings.TrimSpace(detail + "\n" + log.Error)
		}
		entries = append(entries, outputEntry{
			Time:    t,
			Source:  "hook",
			Title:   fmt.Sprintf("%s: %s", log.Event, log.Command),
			Summary: fmt.Sprintf("in %dms", log.DurationMS),
			Detail:  detail,
			Success: &success,
		})
	}
	return entries, nil
}

func (m model) editorLogOutputEntries(limit int) ([]outputEntry, error) {
	if strings.TrimSpace(m.project.LogDir) == "" {
		return nil, nil
	}
	path := filepath.Join(m.project.LogDir, "editor.jsonl")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var logs []editorLogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		var log editorLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &log); err == nil {
			logs = append(logs, log)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}
	entries := make([]outputEntry, 0, len(logs))
	for _, log := range logs {
		t, _ := time.Parse(time.RFC3339Nano, log.Time)
		success := log.Success
		detail := log.Output
		if strings.TrimSpace(log.Error) != "" {
			detail = strings.TrimSpace(detail + "\n" + log.Error)
		}
		entries = append(entries, outputEntry{
			Time:    t,
			Source:  "editor",
			Title:   log.Path,
			Summary: fmt.Sprintf("%s %s", log.Command, strings.Join(log.Args, " ")),
			Detail:  detail,
			Success: &success,
		})
	}
	return entries, nil
}

func (m model) debugLogOutputEntries(limit int) ([]outputEntry, error) {
	if strings.TrimSpace(m.project.LogDir) == "" {
		return nil, nil
	}
	path := filepath.Join(m.project.LogDir, "debug.jsonl")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var logs []debugLogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		var log debugLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &log); err == nil {
			logs = append(logs, log)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}
	entries := make([]outputEntry, 0, len(logs))
	for _, log := range logs {
		t, _ := time.Parse(time.RFC3339Nano, log.Time)
		success := log.Success
		detail := log.Output
		if strings.TrimSpace(log.Error) != "" {
			detail = strings.TrimSpace(detail + "\n" + log.Error)
		}
		entries = append(entries, outputEntry{
			Time:    t,
			Source:  "debug",
			Title:   fmt.Sprintf("%s: %s", log.Name, log.Request),
			Summary: fmt.Sprintf("%s via %s in %dms", log.Adapter, log.Command, log.DurationMS),
			Detail:  detail,
			Success: &success,
		})
	}
	return entries, nil
}

func sortOutputEntries(entries []outputEntry) {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if outputAfter(entries[j], entries[i]) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}

func outputAfter(a, b outputEntry) bool {
	if a.Time.IsZero() {
		return false
	}
	if b.Time.IsZero() {
		return true
	}
	return a.Time.After(b.Time)
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func trimOutputDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	const limit = 1200
	if len(detail) <= limit {
		return detail
	}
	return detail[:limit] + fmt.Sprintf("\n[output truncated: %d chars omitted]", len(detail)-limit)
}

func indentBlock(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if limit > 0 && len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
