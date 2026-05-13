package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type TaskPacket struct {
	Role             string            `json:"role"`
	TaskID           string            `json:"task_id"`
	PlanID           string            `json:"plan_id"`
	Goal             string            `json:"goal"`
	AllowedPaths     []string          `json:"allowed_paths"`
	ForbiddenPaths   []string          `json:"forbidden_paths,omitempty"`
	ContextFiles     []ContextFile     `json:"context_files,omitempty"`
	ContextPolicy    ContextPolicy     `json:"context_policy"`
	Diagnostics      []Diagnostic      `json:"diagnostics,omitempty"`
	ToolsAllowed     []string          `json:"tools_allowed"`
	Verification     []string          `json:"verification,omitempty"`
	AcceptanceChecks []AcceptanceCheck `json:"acceptance_checks,omitempty"`
}

type ContextFile struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

type ContextPolicy struct {
	Mode         string   `json:"mode"`
	RequestTools []string `json:"request_tools"`
	Instruction  string   `json:"instruction"`
}

type Diagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

type WorkerPatch struct {
	TaskID  string `json:"task_id"`
	Summary string `json:"summary"`
	Patch   string `json:"patch"`
	Blocker string `json:"blocker,omitempty"`
}

type ReviewerInput struct {
	UserRequest       string     `json:"user_request,omitempty"`
	Plan              Plan       `json:"plan"`
	TaskPacket        TaskPacket `json:"task_packet"`
	Diff              string     `json:"diff"`
	VerificationOut   string     `json:"verification_output"`
	TaskEventsSummary string     `json:"task_events_summary,omitempty"`
	Constraints       []string   `json:"constraints,omitempty"`
}

type ContextPackOptions struct {
	ProjectRoot     string
	MaxFileChars    int
	DefaultTools    []string
	DefaultVerify   []string
	DefaultAllowed  []string
	Diagnostics     []Diagnostic
	LineRangeByFile map[string]LineRange
}

type LineRange struct {
	StartLine int
	EndLine   int
}

func BuildTaskPacket(task Task, opts ContextPackOptions) (TaskPacket, error) {
	if err := ValidateTask(task); err != nil {
		return TaskPacket{}, err
	}
	if opts.MaxFileChars <= 0 {
		opts.MaxFileChars = 12000
	}
	allowed := task.AllowedPaths
	if len(allowed) == 0 {
		allowed = opts.DefaultAllowed
	}
	verification := task.Verification
	if len(verification) == 0 {
		verification = opts.DefaultVerify
	}
	tools := opts.DefaultTools
	if len(tools) == 0 {
		tools = []string{"read_file", "read_file_range", "search_files", "apply_patch"}
	}
	contextFiles, err := packContextFiles(task.ContextFiles, opts)
	if err != nil {
		return TaskPacket{}, err
	}
	packet := TaskPacket{
		Role:           "worker",
		TaskID:         task.ID,
		PlanID:         task.PlanID,
		Goal:           task.Goal,
		AllowedPaths:   allowed,
		ForbiddenPaths: task.ForbiddenPaths,
		ContextFiles:   contextFiles,
		ContextPolicy: ContextPolicy{
			Mode:         "tool_requested",
			RequestTools: []string{"read_file", "read_file_range", "search_files"},
			Instruction:  "Use approved context tools for missing details; do not assume repo-wide context.",
		},
		Diagnostics:      opts.Diagnostics,
		ToolsAllowed:     tools,
		Verification:     verification,
		AcceptanceChecks: task.AcceptanceChecks,
	}
	if err := ValidateTaskPacket(packet); err != nil {
		return TaskPacket{}, err
	}
	return packet, nil
}

func ValidateTaskPacket(packet TaskPacket) error {
	if strings.TrimSpace(packet.TaskID) == "" {
		return fmt.Errorf("task packet task_id is required")
	}
	if strings.TrimSpace(packet.PlanID) == "" {
		return fmt.Errorf("task packet plan_id is required")
	}
	if strings.TrimSpace(packet.Goal) == "" {
		return fmt.Errorf("task packet goal is required")
	}
	if len(packet.AllowedPaths) == 0 {
		return fmt.Errorf("task packet requires at least one allowed path")
	}
	if len(packet.ToolsAllowed) == 0 {
		return fmt.Errorf("task packet requires at least one allowed tool")
	}
	return nil
}

func ValidateWorkerPatch(patch WorkerPatch) error {
	if strings.TrimSpace(patch.TaskID) == "" {
		return fmt.Errorf("worker patch task_id is required")
	}
	if strings.TrimSpace(patch.Blocker) != "" {
		return nil
	}
	if strings.TrimSpace(patch.Patch) == "" {
		return fmt.Errorf("worker patch requires patch or blocker")
	}
	return nil
}

func packContextFiles(paths []string, opts ContextPackOptions) ([]ContextFile, error) {
	files := make([]ContextFile, 0, len(paths))
	for _, rel := range paths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		clean := filepath.Clean(rel)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return nil, fmt.Errorf("context file %q must be relative to project root", rel)
		}
		full := filepath.Join(opts.ProjectRoot, clean)
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		content := string(data)
		contextFile := ContextFile{Path: filepath.ToSlash(clean), StartLine: 1}
		if r, ok := opts.LineRangeByFile[filepath.ToSlash(clean)]; ok && r.StartLine > 0 && r.EndLine >= r.StartLine {
			content, contextFile.StartLine, contextFile.EndLine = sliceLines(content, r.StartLine, r.EndLine)
		} else {
			contextFile.EndLine = countPacketLines(content)
		}
		if len(content) > opts.MaxFileChars {
			content = content[:opts.MaxFileChars]
			contextFile.Truncated = true
		}
		contextFile.Content = content
		files = append(files, contextFile)
	}
	return files, nil
}

func sliceLines(content string, start, end int) (string, int, int) {
	lines := strings.Split(content, "\n")
	if start > len(lines) {
		return "", start, start
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n"), start, end
}

func countPacketLines(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}
