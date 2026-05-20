package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/config"
)

func TestLivePygameBlackjackE2E(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("WEAZLCODE_E2E_PYGAME_ROOT"))
	if root == "" {
		t.Skip("set WEAZLCODE_E2E_PYGAME_ROOT to run live pygame blackjack E2E")
	}
	runStarted := time.Now()
	runTarget := runStarted.Add(5 * time.Minute)
	runDeadline := runStarted.Add(10 * time.Minute)
	cfg, _, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Workers.Concurrency = 8
	cfg.Workers.RequestTimeoutSeconds = 300
	cfg.Workers.RunTimeoutSeconds = int(time.Until(runDeadline).Seconds())

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll root: %v", err)
	}
	m := commandTestModel(t)
	m.cfg = cfg
	m.project.Root = root
	m.project.GitRoot = true
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.project.Languages = []string{"python"}
	m.session.ProjectRoot = root
	m.toolRegistry = smokeToolRegistry(root)
	m.autonomousRun = true
	m.autonomousRunStarted = runStarted
	if err := os.MkdirAll(m.project.LogDir, 0o755); err != nil {
		t.Fatalf("MkdirAll logs: %v", err)
	}
	tracePath := filepath.Join(m.project.StateDir, "live-pygame-blackjack-e2e-trace.txt")
	appendTrace := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		f, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			_, _ = f.WriteString(line + "\n")
			_ = f.Close()
		}
	}
	appendTrace("live pygame blackjack e2e started at %s", runStarted.Format(time.RFC3339))
	appendTrace("root: %s", root)
	appendTrace("orchestrator: %s/%s", m.cfg.ProviderForRole("orchestrator").Type, m.cfg.ProviderForRole("orchestrator").Model)
	appendTrace("worker: %s/%s concurrency=%d", m.cfg.ProviderForRole("worker").Type, m.cfg.ProviderForRole("worker").Model, m.workerConcurrency())
	appendTrace("reviewer: %s/%s", m.cfg.ProviderForRole("reviewer").Type, m.cfg.ProviderForRole("reviewer").Model)

	request := strings.Join([]string{
		"Create a player-vs-dealer blackjack game in Python using pygame.",
		"The game should render a basic graphical table, visible cards, player/dealer hands, scores, chip/bet or status area, and buttons or keyboard controls for Hit, Stand, Deal/New Round, and Quit.",
		"Use pygame drawing primitives to render card rectangles, suits/ranks, table background, and UI controls. Do not require external image assets.",
		"Implement standard simplified blackjack rules: shuffled deck, ace values 1 or 11, blackjack/bust detection, dealer hits until 17, win/loss/push messages, and resettable rounds.",
		"Use maintainable modular Python files. Keep each generated code file around 300 lines or less. Prefer separate files for card/deck logic, game rules/state, pygame rendering/UI, and the entrypoint.",
		"Keep it runnable from the terminal as `python main.py`.",
		"Interactive mode must render a visible first frame with table, cards, scores/status, and controls before waiting for user input. A blank or black initial window is a failure.",
		"Also add a non-interactive smoke path: `python main.py --smoke` must initialize pygame with the dummy video driver if needed, draw at least one frame, then exit successfully without user input.",
		"Use a compact right-sized module-first plan. Do not add README, docs, examples, or companion files unless required to run the game.",
	}, "\n\n")
	ctx, cancel := context.WithTimeout(context.Background(), minDuration(90*time.Second, time.Until(runDeadline)))
	defer cancel()
	appendTrace("generating plan")
	rawPlan, err := m.generatePlanJSON(ctx, request)
	if err != nil {
		t.Fatalf("generatePlanJSON: %v", err)
	}
	plan, err := m.planFromGeneratedJSON(rawPlan)
	if err != nil {
		repaired, repairErr := m.repairPlanJSON(ctx, request, rawPlan, err)
		if repairErr != nil {
			t.Fatalf("plan parse failed: %v; repair failed: %v\nraw:\n%s", err, repairErr, rawPlan)
		}
		plan, err = m.planFromGeneratedJSON(repaired)
		if err != nil {
			t.Fatalf("repaired plan parse failed: %v\nraw:\n%s", err, repaired)
		}
	}
	if issues := coding.ValidatePlanQuality(plan); len(issues) > 0 {
		appendTrace("plan quality repair needed:\n%s", renderPlanQualityIssues(issues))
		repaired, repairErr := m.repairPlanJSON(ctx, request, rawPlan, fmt.Errorf("plan quality check failed:\n%s", renderPlanQualityIssues(issues)))
		if repairErr != nil {
			t.Fatalf("generated plan quality issues:\n%s\nrepair failed: %v\nplan:\n%s", renderPlanQualityIssues(issues), repairErr, renderJSON(plan))
		}
		plan, err = m.planFromGeneratedJSON(repaired)
		if err != nil {
			t.Fatalf("repaired plan parse failed after quality repair: %v\nraw:\n%s", err, repaired)
		}
		if issues := coding.ValidatePlanQuality(plan); len(issues) > 0 {
			t.Fatalf("repaired plan quality issues:\n%s\nplan:\n%s", renderPlanQualityIssues(issues), renderJSON(plan))
		}
	}
	if err := m.store.SavePlan(plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	appendTrace("plan generated: %s (%d tasks)", plan.Title, len(plan.Tasks))
	for _, task := range plan.Tasks {
		appendTrace("plan task: %s | allowed=%s | depends=%s", task.ID, strings.Join(task.AllowedPaths, ","), strings.Join(task.DependsOn, ","))
	}
	updated, _, handled := m.approveLatestPlan(false)
	if !handled {
		t.Fatal("approveLatestPlan handled=false")
	}
	m = updated.(model)

	reportedTarget := false
	for cycle := 1; cycle <= 16; cycle++ {
		if !reportedTarget && time.Now().After(runTarget) {
			appendTrace("soft 5 minute target exceeded; continuing to hard cap for convergence trace, elapsed=%s", time.Since(runStarted).Round(time.Second))
			reportedTarget = true
		}
		if time.Now().After(runDeadline) {
			t.Fatalf("live pygame blackjack E2E exceeded 10 minute hard cap; elapsed=%s", time.Since(runStarted).Round(time.Second))
		}
		plan, ok, err := m.store.LatestPlan(m.session.ID)
		if err != nil {
			t.Fatalf("LatestPlan: %v", err)
		}
		if !ok {
			t.Fatal("LatestPlan ok=false")
		}
		appendTrace("cycle %d status: %s | %s", cycle, plan.Status, taskStatusSummary(plan.Tasks))
		if plan.Status == coding.PlanStatusDone {
			break
		}
		candidates, err := m.parallelRunnableTasks(plan.Tasks, m.workerConcurrency())
		if err != nil {
			t.Fatalf("parallelRunnableTasks: %v", err)
		}
		if len(candidates) > 0 {
			appendTrace("cycle %d dispatching workers: %s", cycle, taskIDs(candidates))
			results := runLiveWorkerBatch(t, m, candidates, runDeadline)
			sort.Slice(results, func(i, j int) bool { return results[i].task.ID < results[j].task.ID })
			for _, result := range results {
				if result.err != nil {
					eventType, status, note := classifyWorkerRunError(result.err)
					_ = m.store.UpdateTaskStatus(result.task.ID, coding.TaskStatusBlocked)
					_, _ = m.store.AddTaskEvent(coding.TaskEvent{TaskID: result.task.ID, Type: eventType, Message: result.err.Error()})
					appendTrace("worker %s error: %s (%s: %v)", result.task.ID, status, note, result.err)
					continue
				}
				appendTrace("worker %s patch: %s", result.task.ID, result.patch.Summary)
				updated, _, handled := m.applyWorkerPatchWithTelemetry(result.patch, true, &result.telemetry)
				if !handled {
					t.Fatalf("applyWorkerPatch handled=false for %s", result.task.ID)
				}
				m = updated.(model)
				appendTrace("worker %s applied, status=%s", result.task.ID, m.status)
			}
		}
		reviewed := false
		for {
			plan, ok, err := m.store.LatestPlan(m.session.ID)
			if err != nil || !ok {
				t.Fatalf("LatestPlan before review: %v ok=%v", err, ok)
			}
			task, ok := firstReviewingTask(plan.Tasks)
			if !ok {
				break
			}
			reviewed = true
			appendTrace("reviewing task: %s", task.ID)
			input, err := m.buildReviewerInput()
			if err != nil {
				t.Fatalf("buildReviewerInput %s: %v", task.ID, err)
			}
			reviewerTimeout := minDuration(m.reviewerRequestTimeout(), time.Until(runDeadline))
			if reviewerTimeout <= 0 {
				t.Fatalf("live pygame blackjack E2E exceeded 10 minute hard cap before reviewer; elapsed=%s", time.Since(runStarted).Round(time.Second))
			}
			ctx, cancel := context.WithTimeout(context.Background(), reviewerTimeout)
			raw, usage, err := m.generateReviewVerdictJSON(ctx, input)
			cancel()
			telemetry := m.modelTelemetry("reviewer", 0, len(raw), 0, usage)
			if err != nil {
				t.Fatalf("reviewer %s failed: %v", task.ID, err)
			}
			verdict, err := coding.ParseReviewVerdictJSON([]byte(extractJSONObject(raw)))
			if err != nil {
				t.Fatalf("reviewer %s parse failed: %v\nraw:\n%s", task.ID, err, raw)
			}
			appendTrace("reviewer %s verdict=%s summary=%s", task.ID, verdict.Verdict, verdict.Summary)
			updated, _, handled := m.applyReviewVerdict(verdict, &telemetry)
			if !handled {
				t.Fatalf("applyReviewVerdict handled=false for %s", task.ID)
			}
			m = updated.(model)
		}
		if len(candidates) == 0 && !reviewed {
			t.Fatalf("no runnable or reviewing tasks remain; %s", taskStatusSummary(plan.Tasks))
		}
	}
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil || !ok || plan.Status != coding.PlanStatusDone {
		preserveLatestWorkerFiles(t, root, filepath.Join(root, ".weazlcode", "runs", m.session.ID))
		t.Fatalf("final plan status ok=%v status=%q tasks=%s err=%v", ok, plan.Status, taskStatusSummary(plan.Tasks), err)
	}
	assertPygameBlackjackOutput(t, root)
	appendTrace("final pygame blackjack output passed smoke checks")
}

func preserveLatestWorkerFiles(t *testing.T, root, runsDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(runsDir, "*-worker_output.json"))
	if err != nil || len(matches) == 0 {
		return
	}
	sort.Strings(matches)
	data, err := os.ReadFile(matches[len(matches)-1])
	if err != nil {
		return
	}
	var artifact struct {
		Payload struct {
			Patch coding.WorkerPatch `json:"patch"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		return
	}
	for _, file := range artifact.Payload.Patch.Files {
		path := strings.TrimSpace(filepath.ToSlash(file.Path))
		if path == "" || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
			continue
		}
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			continue
		}
		_ = os.WriteFile(fullPath, []byte(file.Content), 0o644)
	}
}

func assertPygameBlackjackOutput(t *testing.T, root string) {
	t.Helper()
	python := smokePythonCommand()
	mainPath := filepath.Join(root, "main.py")
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("main.py missing: %v", err)
	}
	content := string(data) + "\n" + readGeneratedPython(t, root)
	for _, fragment := range []string{"import pygame", "--smoke", "Hit", "Stand", "dealer", "player", "pygame.display.flip"} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("generated blackjack files missing %q", fragment)
		}
	}
	pythonFiles := generatedPythonFiles(t, root)
	if len(pythonFiles) < 3 {
		t.Fatalf("python files = %#v, want modular output with at least 3 files", pythonFiles)
	}
	for _, path := range pythonFiles {
		if countFileLines(t, path) > 320 {
			t.Fatalf("%s exceeds modular smoke line budget: %d lines", filepath.Base(path), countFileLines(t, path))
		}
	}
	args := append([]string{"-m", "py_compile"}, relativePaths(t, root, pythonFiles)...)
	cmd := exec.Command(python, args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("py_compile failed: %v\n%s", err, out)
	}
	if pygameAvailable(python) {
		cmd = exec.Command(python, "main.py", "--smoke")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pygame smoke failed: %v\n%s", err, out)
		}
	}
}

func readGeneratedPython(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	for _, path := range generatedPythonFiles(t, root) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", path, err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

func generatedPythonFiles(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".weazlcode", "__pycache__", ".venv":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".py") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir python files: %v", err)
	}
	sort.Strings(paths)
	return paths
}

func relativePaths(t *testing.T, root string, paths []string) []string {
	t.Helper()
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("Rel %s: %v", path, err)
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func countFileLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	text := string(data)
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func smokePythonCommand() string {
	if python := strings.TrimSpace(os.Getenv("WEAZLCODE_E2E_PYTHON")); python != "" {
		return python
	}
	return "python3"
}

func pygameAvailable(python string) bool {
	cmd := exec.Command(python, "-c", "import pygame")
	return cmd.Run() == nil
}
