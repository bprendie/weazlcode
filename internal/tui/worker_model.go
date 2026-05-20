package tui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/llm"
)

var modelSizePattern = regexp.MustCompile(`(?i)(?:^|[^0-9])([0-9]+(?:\.[0-9]+)?)\s*b(?:[^a-z]|$)`)

const (
	minWorkerOutputKillChars = 32768
	maxWorkerOutputKillChars = 196608
)

func (m model) runWorkerModelCmd(ctx context.Context, runID int, packet coding.TaskPacket) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		raw, usage, err := m.generateWorkerPatchJSON(ctx, packet)
		telemetry := m.modelTelemetry("worker", time.Since(start), len(raw), 0, usage)
		if err != nil {
			return workerRunMsg{runID: runID, telemetry: telemetry, err: err}
		}
		patch, repairAttempts, err := m.workerPatchFromGeneratedJSON(ctx, packet, raw)
		telemetry.JSONRepairAttempts = repairAttempts
		if err != nil {
			return workerRunMsg{runID: runID, raw: raw, telemetry: telemetry, err: fmt.Errorf("%w\n\nRaw response:\n%s", err, raw)}
		}
		return workerRunMsg{runID: runID, raw: raw, patch: patch, telemetry: telemetry}
	}
}

func (m model) modelTelemetry(role string, latency time.Duration, rawChars, repairAttempts int, usage llm.Usage) modelTelemetry {
	providerName := m.providerNameForRole(role)
	provider := m.cfg.ProviderForRole(role)
	return modelTelemetry{
		Role:               role,
		Provider:           providerName,
		Model:              provider.Model,
		LatencyMS:          latency.Milliseconds(),
		RawChars:           rawChars,
		InputTokens:        usage.InputTokens,
		OutputTokens:       usage.OutputTokens,
		JSONRepairAttempts: repairAttempts,
	}
}

func (m *model) ensureWorkerRunMaps() {
	if m.workerRuns == nil {
		m.workerRuns = map[int]string{}
	}
	if m.cancelWorkerRuns == nil {
		m.cancelWorkerRuns = map[int]context.CancelFunc{}
	}
}

func (m model) workerConcurrency() int {
	if m.cfg.Workers.Concurrency <= 0 {
		return 1
	}
	return m.cfg.Workers.Concurrency
}

func (m model) workerRequestTimeout() time.Duration {
	seconds := m.cfg.Workers.RequestTimeoutSeconds
	if seconds <= 0 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func (m model) workerOutputGuard(maxTokens int) llm.OutputGuard {
	limit := maxTokens * 8
	if limit < minWorkerOutputKillChars {
		limit = minWorkerOutputKillChars
	}
	if limit > maxWorkerOutputKillChars {
		limit = maxWorkerOutputKillChars
	}
	return func(content string) error {
		if len(content) > limit {
			return fmt.Errorf("worker output killed: response exceeded %d characters before producing a valid bounded patch", limit)
		}
		if repeated, count := runawayRepeatedSuffix(content); repeated != "" {
			return fmt.Errorf("worker output killed: repeated output block detected %d times; regenerate a compact complete patch instead of repeating content", count)
		}
		return nil
	}
}

func runawayRepeatedSuffix(content string) (string, int) {
	if len(content) < 800 {
		return "", 0
	}
	if line, count := repeatedMeaningfulLine(content); count >= 8 {
		return line, count
	}
	window := content
	if len(window) > 24000 {
		window = window[len(window)-24000:]
	}
	for blockLen := 200; blockLen <= 2000; blockLen += 100 {
		count := repeatedSuffixCount(window, blockLen)
		if count >= 4 {
			block := window[len(window)-blockLen:]
			if meaningfulRepeatedBlock(block) {
				return block, count
			}
		}
	}
	return "", 0
}

func repeatedMeaningfulLine(content string) (string, int) {
	lines := strings.Split(content, "\n")
	if len(lines) > 240 {
		lines = lines[len(lines)-240:]
	}
	counts := map[string]int{}
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if len(line) < 24 {
			continue
		}
		counts[line]++
		if counts[line] >= 8 {
			return line, counts[line]
		}
	}
	return "", 0
}

func repeatedSuffixCount(content string, blockLen int) int {
	if blockLen <= 0 || len(content) < blockLen*2 {
		return 0
	}
	block := content[len(content)-blockLen:]
	count := 1
	for offset := len(content) - blockLen*2; offset >= 0; offset -= blockLen {
		if content[offset:offset+blockLen] != block {
			break
		}
		count++
	}
	return count
}

func meaningfulRepeatedBlock(block string) bool {
	trimmed := strings.TrimSpace(block)
	if len(trimmed) < 80 {
		return false
	}
	lines := strings.Split(trimmed, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	return nonEmpty >= 3
}

type workerCapacityProfile struct {
	Label        string
	SizeBillions float64
	Instruction  string
}

func (m model) workerCapacityProfile() workerCapacityProfile {
	worker := m.cfg.ProviderForRole("worker")
	modelName := strings.TrimSpace(worker.Model)
	size := inferModelSizeBillions(modelName)
	switch {
	case size > 0 && size <= 10:
		return workerCapacityProfile{
			Label:        fmt.Sprintf("small %.1fB-class local worker", size),
			SizeBillions: size,
			Instruction:  "Assume the worker has limited reasoning and output budget. Keep tasks concrete with narrow allowed_paths, minimal context_files, and explicit acceptance checks. For cohesive artifact packets, complete the whole allowed artifact in one pass instead of inventing smaller subtasks.",
		}
	case size > 0 && size <= 16:
		return workerCapacityProfile{
			Label:        fmt.Sprintf("mid %.1fB-class local worker", size),
			SizeBillions: size,
			Instruction:  "Keep worker tasks compact and bounded. Avoid broad rewrites, keep allowed_paths narrow, and separate planning/design choices from implementation tasks.",
		}
	case size > 0:
		return workerCapacityProfile{
			Label:        fmt.Sprintf("large %.1fB-class local worker", size),
			SizeBillions: size,
			Instruction:  "The worker can handle larger packets than small local models, but tasks must still be bounded, reviewable, and constrained to explicit paths.",
		}
	default:
		return workerCapacityProfile{
			Label:       "unknown-size local worker",
			Instruction: "Worker size could not be inferred from the configured model name. Plan conservatively as if the worker is small: narrow files, small patches, explicit acceptance checks, and no broad rewrites.",
		}
	}
}

func inferModelSizeBillions(modelName string) float64 {
	match := modelSizePattern.FindStringSubmatch(modelName)
	if len(match) != 2 {
		return 0
	}
	size, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0
	}
	return size
}

func (m model) providerNameForRole(role string) string {
	switch role {
	case "orchestrator":
		return emptyFallback(m.cfg.ModelRoles.Orchestrator, m.cfg.ActiveProvider)
	case "worker":
		return emptyFallback(m.cfg.ModelRoles.Worker, m.cfg.ActiveProvider)
	case "reviewer":
		return emptyFallback(m.cfg.ModelRoles.Reviewer, m.cfg.ActiveProvider)
	case "summarizer":
		return emptyFallback(m.cfg.ModelRoles.Summarizer, m.cfg.ActiveProvider)
	default:
		return m.cfg.ActiveProvider
	}
}
