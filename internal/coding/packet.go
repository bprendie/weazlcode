package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type TaskPacket struct {
	Role                string               `json:"role"`
	TaskID              string               `json:"task_id"`
	PlanID              string               `json:"plan_id"`
	Goal                string               `json:"goal"`
	InterfaceContract   InterfaceContract    `json:"interface_contract,omitempty"`
	DependencyContracts []DependencyContract `json:"dependency_contracts,omitempty"`
	AllowedPaths        []string             `json:"allowed_paths"`
	ForbiddenPaths      []string             `json:"forbidden_paths,omitempty"`
	ContextFiles        []ContextFile        `json:"context_files,omitempty"`
	Skills              []SkillContext       `json:"skills,omitempty"`
	WorkerProfile       string               `json:"worker_profile,omitempty"`
	ContextPolicy       ContextPolicy        `json:"context_policy"`
	Diagnostics         []Diagnostic         `json:"diagnostics,omitempty"`
	ToolsAllowed        []string             `json:"tools_allowed"`
	Verification        []string             `json:"verification,omitempty"`
	AcceptanceChecks    []AcceptanceCheck    `json:"acceptance_checks,omitempty"`
}

type DependencyContract struct {
	TaskID            string            `json:"task_id"`
	Title             string            `json:"title,omitempty"`
	AllowedPaths      []string          `json:"allowed_paths,omitempty"`
	InterfaceContract InterfaceContract `json:"interface_contract,omitempty"`
}

type ContextFile struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

type SkillContext struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	Truncated   bool   `json:"truncated,omitempty"`
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
	TaskID  string           `json:"task_id"`
	Summary string           `json:"summary"`
	Patch   string           `json:"patch"`
	Files   []WorkerFileEdit `json:"files,omitempty"`
	Blocker string           `json:"blocker,omitempty"`
}

type WorkerFileEdit struct {
	Path    string `json:"path"`
	Content string `json:"content"`
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
	Skills          []SkillContext
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
		Role:              "worker",
		TaskID:            task.ID,
		PlanID:            task.PlanID,
		Goal:              task.Goal,
		InterfaceContract: task.InterfaceContract,
		AllowedPaths:      allowed,
		ForbiddenPaths:    task.ForbiddenPaths,
		ContextFiles:      contextFiles,
		Skills:            opts.Skills,
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
	if strings.TrimSpace(patch.Patch) == "" && len(patch.Files) == 0 {
		return fmt.Errorf("worker patch requires patch, files, or blocker")
	}
	for _, file := range patch.Files {
		if strings.TrimSpace(file.Path) == "" {
			return fmt.Errorf("worker file edit path is required")
		}
	}
	return nil
}

func packContextFiles(paths []string, opts ContextPackOptions) ([]ContextFile, error) {
	files := make([]ContextFile, 0, len(paths))
	lineRanges := map[string]LineRange{}
	for path, lineRange := range opts.LineRangeByFile {
		lineRanges[filepath.ToSlash(filepath.Clean(path))] = lineRange
	}
	for _, spec := range paths {
		rel, specRange, err := parseContextFileSpec(spec)
		if err != nil {
			return nil, err
		}
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
		r := specRange
		if r.StartLine == 0 && r.EndLine == 0 {
			r = lineRanges[filepath.ToSlash(clean)]
		}
		if r.StartLine > 0 && r.EndLine >= r.StartLine {
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

func parseContextFileSpec(spec string) (string, LineRange, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", LineRange{}, nil
	}
	path := spec
	var lineRange LineRange
	if hash := strings.LastIndex(spec, "#L"); hash >= 0 {
		path = strings.TrimSpace(spec[:hash])
		rangeSpec := strings.TrimSpace(spec[hash+2:])
		parts := strings.Split(rangeSpec, "-L")
		if len(parts) != 2 {
			parts = strings.Split(rangeSpec, "-")
		}
		if len(parts) != 2 {
			return "", LineRange{}, fmt.Errorf("invalid context file range %q", spec)
		}
		start, err := parsePositiveInt(parts[0])
		if err != nil {
			return "", LineRange{}, fmt.Errorf("invalid context file range %q", spec)
		}
		end, err := parsePositiveInt(parts[1])
		if err != nil || end < start {
			return "", LineRange{}, fmt.Errorf("invalid context file range %q", spec)
		}
		lineRange = LineRange{StartLine: start, EndLine: end}
	}
	return path, lineRange, nil
}

func parsePositiveInt(raw string) (int, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "L")
	if raw == "" {
		return 0, fmt.Errorf("empty integer")
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid integer")
		}
		n = n*10 + int(r-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("integer must be positive")
	}
	return n, nil
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
