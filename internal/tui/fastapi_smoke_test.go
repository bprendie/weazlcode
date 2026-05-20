package tui

import (
	"context"
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

func TestLiveFastAPISQLiteE2E(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("WEAZLCODE_E2E_FASTAPI_ROOT"))
	if root == "" {
		t.Skip("set WEAZLCODE_E2E_FASTAPI_ROOT to run live FastAPI SQLite E2E")
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
	tracePath := filepath.Join(m.project.StateDir, "live-fastapi-sqlite-e2e-trace.txt")
	appendTrace := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		f, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			_, _ = f.WriteString(line + "\n")
			_ = f.Close()
		}
	}
	appendTrace("live FastAPI SQLite e2e started at %s", runStarted.Format(time.RFC3339))
	appendTrace("root: %s", root)
	appendTrace("orchestrator: %s/%s", m.cfg.ProviderForRole("orchestrator").Type, m.cfg.ProviderForRole("orchestrator").Model)
	appendTrace("worker: %s/%s concurrency=%d", m.cfg.ProviderForRole("worker").Type, m.cfg.ProviderForRole("worker").Model, m.workerConcurrency())
	appendTrace("reviewer: %s/%s", m.cfg.ProviderForRole("reviewer").Type, m.cfg.ProviderForRole("reviewer").Model)

	request := strings.Join([]string{
		"Create a small FastAPI task tracker API in Python.",
		"Use SQLite for persistence and Pydantic models for request/response validation.",
		"Expose /health plus CRUD endpoints for tasks: create, list, get by id, update, and delete.",
		"Keep it runnable from the terminal with `uvicorn app:app --host 127.0.0.1 --port 8000`.",
		"Include a non-interactive smoke path: `python app.py --smoke` must create a temporary SQLite database, exercise create/list/get/update/delete through FastAPI's TestClient or equivalent in-process client, print `SMOKE PASS`, and exit 0.",
		"If you include tests, keep them small and runnable with pytest. Do not add README, docs, examples, or companion files unless needed for the runnable code or tests.",
		"Use a compact right-sized plan. This is a cohesive small backend artifact; prefer one task that owns app.py and optional focused tests rather than many microtasks.",
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
		t.Fatalf("generated plan quality issues:\n%s\nplan:\n%s", renderPlanQualityIssues(issues), renderJSON(plan))
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
			t.Fatalf("live FastAPI SQLite E2E exceeded 10 minute hard cap; elapsed=%s", time.Since(runStarted).Round(time.Second))
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
				t.Fatalf("live FastAPI SQLite E2E exceeded 10 minute hard cap before reviewer; elapsed=%s", time.Since(runStarted).Round(time.Second))
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
	assertFastAPIOutput(t, root)
	appendTrace("final FastAPI SQLite output passed smoke checks")
}

func assertFastAPIOutput(t *testing.T, root string) {
	t.Helper()
	python := smokePythonCommand()
	path := filepath.Join(root, "app.py")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("app.py missing: %v", err)
	}
	content := string(data)
	for _, fragment := range []string{"FastAPI", "sqlite", "--smoke", "SMOKE PASS", "/health", "TestClient"} {
		if !strings.Contains(strings.ToLower(content), strings.ToLower(fragment)) {
			t.Fatalf("app.py missing %q", fragment)
		}
	}
	cmd := exec.Command(python, "-m", "py_compile", "app.py")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("py_compile failed: %v\n%s", err, out)
	}
	cmd = exec.Command(python, "app.py", "--smoke")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastAPI smoke failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "SMOKE PASS") {
		t.Fatalf("FastAPI smoke missing SMOKE PASS:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "test_app.py")); err == nil {
		cmd = exec.Command(python, "-m", "pytest", "-q")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pytest failed: %v\n%s", err, out)
		}
	}
}
