package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/project"
)

func (m model) buildWorkerPacket(task coding.Task) (coding.TaskPacket, error) {
	diagnostics := m.codingDiagnostics()
	allowedTools := []string{"read_file", "read_file_range", "search_files", "apply_patch"}
	skills, err := m.skillContexts(task.Skills)
	if err != nil {
		return coding.TaskPacket{}, err
	}
	packet, err := coding.BuildTaskPacket(task, coding.ContextPackOptions{
		ProjectRoot: m.project.Root,
		MaxFileChars: workerContextFileCharBudget(
			m.cfg.ProviderForRole("worker").ContextWindow,
		),
		DefaultAllowed: []string{
			".",
		},
		DefaultTools:  allowedTools,
		DefaultVerify: m.defaultVerificationCommands(),
		Diagnostics:   diagnostics,
		Skills:        skills,
	})
	if err != nil {
		return coding.TaskPacket{}, err
	}
	profile := m.workerCapacityProfile()
	packet.WorkerProfile = profile.Label + ": " + profile.Instruction
	return packet, nil
}

func workerContextFileCharBudget(contextWindow int) int {
	switch {
	case contextWindow >= 32768:
		return 48000
	case contextWindow >= 16384:
		return 24000
	default:
		return 12000
	}
}

func (m model) skillContexts(names []string) ([]coding.SkillContext, error) {
	if !m.cfg.Skills.SkillsEnabled() || len(names) == 0 {
		return nil, nil
	}
	skills, err := project.LoadSkillContents(m.project.Root, m.cfg.Skills.Paths, names, 12000)
	if err != nil {
		return nil, err
	}
	out := make([]coding.SkillContext, 0, len(skills))
	for _, skill := range skills {
		out = append(out, coding.SkillContext{
			Name:        skill.Name,
			Description: skill.Description,
			Path:        skill.Path,
			Content:     skill.Content,
			Truncated:   skill.Truncated,
		})
	}
	return out, nil
}

func (m model) codingDiagnostics() []coding.Diagnostic {
	diagnostics, err := m.lspManager().Diagnostics(context.Background())
	if err != nil || len(diagnostics) == 0 {
		return nil
	}
	out := make([]coding.Diagnostic, 0, min(len(diagnostics), 25))
	for i, diagnostic := range diagnostics {
		if i >= 25 {
			break
		}
		out = append(out, coding.Diagnostic{
			File:     diagnostic.File,
			Line:     diagnostic.Line,
			Column:   diagnostic.Column,
			Severity: diagnostic.Severity,
			Message:  diagnostic.Message,
			Source:   diagnostic.Source,
		})
	}
	return out
}

func (m model) buildWorkerPacketForRun(task coding.Task) (coding.TaskPacket, error) {
	packet, err := m.buildWorkerPacket(task)
	if err != nil {
		return coding.TaskPacket{}, err
	}
	if task.Status != coding.TaskStatusBlocked {
		return packet, nil
	}
	events, err := m.store.TaskEvents(task.ID)
	if err != nil {
		return coding.TaskPacket{}, err
	}
	repair, ok := latestRepairRequest(events)
	if !ok {
		if workerErr, retry := latestWorkerError(events); retry {
			packet.Goal = strings.TrimSpace(packet.Goal + "\n\nRetry note:\nPrevious worker attempt failed before producing a patch: " + workerErr)
			return packet, nil
		}
		return packet, nil
	}
	packet.Goal = strings.TrimSpace(packet.Goal + "\n\nRepair focus:\n" + repair)
	packet.AcceptanceChecks = append(packet.AcceptanceChecks, coding.AcceptanceCheck{Description: "Reviewer needs_fix issues are addressed without broadening the task scope."})
	return packet, nil
}

func renderJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("JSON render error: %v", err)
	}
	return string(b)
}
