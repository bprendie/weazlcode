package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/config"
	"github.com/bprendie/weazlcode/internal/tools"
)

func TestStaticSiteSmokeHarnessUsesModuleFirstWorkerFlow(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "hero.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile hero: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "product.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile product: %v", err)
	}
	runTestGit(t, root, "init")
	runTestGit(t, root, "add", "assets")
	runTestGit(t, root, "commit", "-m", "fixtures")

	m := commandTestModel(t)
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	m.cfg.Providers = map[string]config.Provider{
		"local-vllm": {Type: "vllm", ServerURL: "http://localhost:8000", Model: "test-8b-worker", ContextWindow: 8192},
	}
	m.cfg.ModelRoles.Worker = "local-vllm"
	m.cfg.Workers.Concurrency = 8

	plan := staticSiteSmokePlan(m.session.ID, root)
	if issues := coding.ValidatePlanQuality(plan); len(issues) > 0 {
		t.Fatalf("plan quality issues = %#v", issues)
	}
	if err := m.store.SavePlan(plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	updated, cmd, handled := m.approveLatestPlan(false)
	if !handled {
		t.Fatal("approve handled = false")
	}
	if cmd != nil {
		t.Fatal("approve command = non-nil")
	}
	m = updated.(model)

	updated, cmd, handled = m.runParallelWorkers()
	if !handled {
		t.Fatal("runParallelWorkers handled = false")
	}
	if cmd == nil {
		t.Fatal("runParallelWorkers cmd = nil")
	}
	m = updated.(model)
	if len(m.workerRuns) != 4 {
		t.Fatalf("first batch worker runs = %d, want 4", len(m.workerRuns))
	}
	m.workerRuns = nil
	m.cancelWorkerRuns = nil

	m = applyAndApproveStaticPatch(t, m, "section-hero", []coding.WorkerFileEdit{{
		Path: "sections/hero.html",
		Content: `<section class="hero" aria-labelledby="hero-title">
  <div>
    <p class="eyebrow">Local-first coding</p>
    <h1 id="hero-title">Build small, review hard, ship clean.</h1>
    <p>Split planning from implementation so bounded workers can produce reliable pieces.</p>
  </div>
  <img src="../assets/hero.png" alt="Abstract workspace preview">
</section>
`,
	}})
	m = applyAndApproveStaticPatch(t, m, "section-product", []coding.WorkerFileEdit{{
		Path: "sections/product.html",
		Content: `<section class="product" aria-labelledby="product-title">
  <img src="../assets/product.png" alt="Product interface preview">
  <div>
    <h2 id="product-title">A coding loop that respects context</h2>
    <p>Workers receive small files, explicit acceptance checks, and a reviewer gate.</p>
  </div>
</section>
`,
	}})
	m = applyAndApproveStaticPatch(t, m, "style-base", []coding.WorkerFileEdit{{
		Path: "styles/base.css",
		Content: `:root {
  color: #111827;
  background: #f7f7f2;
  font-family: Inter, ui-sans-serif, system-ui, sans-serif;
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
}

img {
  max-width: 100%;
  display: block;
}
`,
	}})
	m = applyAndApproveStaticPatch(t, m, "style-components", []coding.WorkerFileEdit{{
		Path: "styles/components.css",
		Content: `.site-header,
.site-footer {
  padding: 24px clamp(20px, 5vw, 72px);
  background: #111827;
  color: white;
}

.hero,
.product {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(220px, 420px);
  gap: 32px;
  align-items: center;
  padding: 48px clamp(20px, 5vw, 72px);
}

.hero {
  background: #e7f0ea;
}

.product {
  background: #fff;
}

.eyebrow {
  color: #0f766e;
  font-weight: 700;
  text-transform: uppercase;
}

@media (max-width: 720px) {
  .hero,
  .product {
    grid-template-columns: 1fr;
  }
}
`,
	}})

	assemblyTask := latestTaskByID(t, m, "assemble-site")
	packet, err := m.buildWorkerPacket(assemblyTask)
	if err != nil {
		t.Fatalf("buildWorkerPacket assembly: %v", err)
	}
	packetContext := map[string]string{}
	for _, file := range packet.ContextFiles {
		packetContext[file.Path] = file.Content
	}
	for _, path := range []string{"sections/hero.html", "sections/product.html", "styles/base.css", "styles/components.css"} {
		if strings.TrimSpace(packetContext[path]) == "" {
			t.Fatalf("assembly packet missing context for %s: %#v", path, packet.ContextFiles)
		}
	}

	updated, cmd, handled = m.runParallelWorkers()
	if !handled {
		t.Fatal("second runParallelWorkers handled = false")
	}
	if cmd == nil {
		t.Fatal("second runParallelWorkers cmd = nil")
	}
	m = updated.(model)
	if len(m.workerRuns) != 1 {
		t.Fatalf("second batch worker runs = %d, want 1", len(m.workerRuns))
	}
	m.workerRuns = nil
	m.cancelWorkerRuns = nil

	m = applyAndApproveStaticPatch(t, m, "assemble-site", []coding.WorkerFileEdit{{
		Path: "index.html",
		Content: `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Local-first Coding Loop</title>
  <link rel="stylesheet" href="styles/base.css">
  <link rel="stylesheet" href="styles/components.css">
</head>
<body>
  <header class="site-header">
    <strong>WeazlCode Demo</strong>
  </header>
  <main>
    <section class="hero" aria-labelledby="hero-title">
      <div>
        <p class="eyebrow">Local-first coding</p>
        <h1 id="hero-title">Build small, review hard, ship clean.</h1>
        <p>Split planning from implementation so bounded workers can produce reliable pieces.</p>
      </div>
      <img src="assets/hero.png" alt="Abstract workspace preview">
    </section>
    <section class="product" aria-labelledby="product-title">
      <img src="assets/product.png" alt="Product interface preview">
      <div>
        <h2 id="product-title">A coding loop that respects context</h2>
        <p>Workers receive small files, explicit acceptance checks, and a reviewer gate.</p>
      </div>
    </section>
  </main>
  <footer class="site-footer">
    <span>Generated through bounded worker tasks.</span>
  </footer>
</body>
</html>
`,
	}})

	updated, cmd, handled = m.runParallelWorkers()
	if !handled {
		t.Fatal("third runParallelWorkers handled = false")
	}
	if cmd == nil {
		t.Fatal("third runParallelWorkers cmd = nil")
	}
	m = updated.(model)
	if len(m.workerRuns) != 1 {
		t.Fatalf("third batch worker runs = %d, want 1", len(m.workerRuns))
	}
	m.workerRuns = nil
	m.cancelWorkerRuns = nil
	m = applyAndApproveStaticPatch(t, m, "docs", []coding.WorkerFileEdit{{
		Path: "README.md",
		Content: `# Static Site Smoke

Open index.html in a browser. The page references the committed image assets and local CSS files.
`,
	}})

	assertStaticSiteSmokeOutput(t, root)
	latest, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("LatestPlan ok = false")
	}
	if latest.Status != coding.PlanStatusDone {
		t.Fatalf("plan status = %q, want done", latest.Status)
	}
}

func TestStaticSiteRuntimeWorkerSmoke(t *testing.T) {
	serverURL := strings.TrimSpace(os.Getenv("WEAZLCODE_STATIC_SMOKE_URL"))
	modelName := strings.TrimSpace(os.Getenv("WEAZLCODE_STATIC_SMOKE_MODEL"))
	if serverURL == "" || modelName == "" {
		t.Skip("set WEAZLCODE_STATIC_SMOKE_URL and WEAZLCODE_STATIC_SMOKE_MODEL to run live worker smoke")
	}
	providerType := strings.TrimSpace(os.Getenv("WEAZLCODE_STATIC_SMOKE_PROVIDER"))
	if providerType == "" {
		providerType = "vllm"
	}

	root := t.TempDir()
	runTestGit(t, root, "init")
	m := commandTestModel(t)
	m.project.Root = root
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.session.ProjectRoot = root
	m.cfg.Providers = map[string]config.Provider{
		"runtime-worker": {Type: providerType, ServerURL: serverURL, Model: modelName, ContextWindow: 8192},
	}
	m.cfg.ModelRoles.Worker = "runtime-worker"
	m.cfg.Workers.RequestTimeoutSeconds = 120

	now := time.Now()
	plan := coding.Plan{
		ID:          "runtime-static-smoke",
		SessionID:   m.session.ID,
		ProjectRoot: root,
		Title:       "Runtime static smoke",
		Summary:     "Live model smoke for a bounded static-site worker task.",
		Status:      coding.PlanStatusApproved,
		CreatedAt:   now,
		UpdatedAt:   now,
		Tasks: []coding.Task{{
			ID:           "feature-card",
			PlanID:       "runtime-static-smoke",
			Title:        "Create feature card module",
			Goal:         "Create sections/feature-card.html containing exactly one section with class feature-card, an h2 that says Fast local workers, and a paragraph that says Frontier review keeps the output honest.",
			Status:       coding.TaskStatusRunning,
			AllowedPaths: []string{"sections/feature-card.html"},
			AcceptanceChecks: []coding.AcceptanceCheck{
				{Description: "The file contains class feature-card, the exact h2 text, and the exact paragraph text."},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}},
	}
	if err := m.store.SavePlan(plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	if err := m.recordTaskBaseline(plan.Tasks[0]); err != nil {
		t.Fatalf("recordTaskBaseline: %v", err)
	}
	packet, err := m.buildWorkerPacket(plan.Tasks[0])
	if err != nil {
		t.Fatalf("buildWorkerPacket: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	raw, _, err := m.generateWorkerPatchJSON(ctx, packet)
	if err != nil {
		t.Fatalf("generateWorkerPatchJSON: %T: %[1]v", err)
	}
	patch, _, err := m.workerPatchFromGeneratedJSON(ctx, packet, raw)
	if err != nil {
		t.Fatalf("workerPatchFromGeneratedJSON: %v\nraw:\n%s", err, raw)
	}
	if blocker := workerBlockerText(patch.Blocker); blocker != "" {
		t.Fatalf("worker returned blocker: %s\nraw:\n%s", blocker, raw)
	}
	updated, _, handled := m.applyWorkerPatch(patch, true)
	if !handled {
		t.Fatal("applyWorkerPatch handled = false")
	}
	m = updated.(model)
	if m.status != "task reviewing" {
		t.Fatalf("status = %q, want task reviewing\nraw:\n%s", m.status, raw)
	}
	content := readSmokeFile(t, root, "sections/feature-card.html")
	assertNoPlaceholderSentinels(t, "sections/feature-card.html", content)
	for _, fragment := range []string{
		"class=\"feature-card\"",
		"Fast local workers",
		"Frontier review keeps the output honest.",
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("runtime smoke output missing %q\nraw:\n%s\nfile:\n%s", fragment, raw, content)
		}
	}
}

func TestLiveFullStaticSiteE2E(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("WEAZLCODE_E2E_SITE_ROOT"))
	if root == "" {
		t.Skip("set WEAZLCODE_E2E_SITE_ROOT to run live full static-site E2E")
	}
	copyText := strings.TrimSpace(os.Getenv("WEAZLCODE_E2E_SITE_COPY"))
	if copyFile := strings.TrimSpace(os.Getenv("WEAZLCODE_E2E_SITE_COPY_FILE")); copyText == "" && copyFile != "" {
		data, err := os.ReadFile(copyFile)
		if err != nil {
			t.Fatalf("read copy file: %v", err)
		}
		copyText = strings.TrimSpace(string(data))
	}
	if copyText == "" {
		t.Fatal("WEAZLCODE_E2E_SITE_COPY or WEAZLCODE_E2E_SITE_COPY_FILE is required")
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

	m := commandTestModel(t)
	m.cfg = cfg
	m.project.Root = root
	m.project.GitRoot = true
	m.project.StateDir = filepath.Join(root, ".weazlcode")
	m.project.LogDir = filepath.Join(root, ".weazlcode", "logs")
	m.project.Languages = []string{"html", "css"}
	m.session.ProjectRoot = root
	m.toolRegistry = smokeToolRegistry(root)
	m.autonomousRun = true
	m.autonomousRunStarted = runStarted
	if err := os.MkdirAll(m.project.LogDir, 0o755); err != nil {
		t.Fatalf("MkdirAll logs: %v", err)
	}
	tracePath := filepath.Join(m.project.StateDir, "live-static-site-e2e-trace.txt")
	trace := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		_ = os.WriteFile(tracePath, []byte(line+"\n"), 0o644)
	}
	appendTrace := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		f, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			_, _ = f.WriteString(line + "\n")
			_ = f.Close()
		}
	}
	trace("live static-site e2e started at %s", runStarted.Format(time.RFC3339))
	appendTrace("root: %s", root)
	appendTrace("orchestrator: %s/%s", m.cfg.ProviderForRole("orchestrator").Type, m.cfg.ProviderForRole("orchestrator").Model)
	appendTrace("worker: %s/%s concurrency=%d", m.cfg.ProviderForRole("worker").Type, m.cfg.ProviderForRole("worker").Model, m.workerConcurrency())
	appendTrace("reviewer: %s/%s", m.cfg.ProviderForRole("reviewer").Type, m.cfg.ProviderForRole("reviewer").Model)

	request := strings.Join([]string{
		"Create a polished static landing page for Weazl Suite using the existing image assets in this project.",
		"Use the supplied copy exactly where practical, but format it into a coherent page instead of dumping raw text.",
		"Produce browser-openable HTML and CSS. Preserve the existing PNG assets.",
		"Use a compact right-sized plan suitable for the configured local worker. For this single landing page, prefer one cohesive artifact task that owns complete index.html and complete styles.css over many section fragments or separate microtasks.",
		"Required copy:",
		copyText,
	}, "\n\n")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	attachSourceCopyContract(&plan, request)
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
	for cycle := 1; cycle <= 24; cycle++ {
		if !reportedTarget && time.Now().After(runTarget) {
			appendTrace("soft 5 minute target exceeded; continuing to hard cap for convergence trace, elapsed=%s", time.Since(runStarted).Round(time.Second))
			reportedTarget = true
		}
		if time.Now().After(runDeadline) {
			t.Fatalf("live static-site E2E exceeded 10 minute hard cap; elapsed=%s", time.Since(runStarted).Round(time.Second))
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
					_, _ = m.store.AddTaskEvent(coding.TaskEvent{
						TaskID:  result.task.ID,
						Type:    eventType,
						Message: result.err.Error(),
					})
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
			if err != nil {
				t.Fatalf("LatestPlan before review: %v", err)
			}
			if !ok {
				t.Fatal("LatestPlan before review ok=false")
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
				t.Fatalf("live static-site E2E exceeded 10 minute hard cap before reviewer; elapsed=%s", time.Since(runStarted).Round(time.Second))
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
	if err != nil {
		t.Fatalf("LatestPlan final: %v", err)
	}
	if !ok || plan.Status != coding.PlanStatusDone {
		t.Fatalf("final plan status ok=%v status=%q tasks=%s", ok, plan.Status, taskStatusSummary(plan.Tasks))
	}
	assertLiveStaticSiteOutput(t, root, copyText)
	appendTrace("final output passed structural checks")
}

func staticSiteSmokePlan(sessionID, root string) coding.Plan {
	now := time.Now()
	tasks := []coding.Task{
		{
			ID:           "section-hero",
			PlanID:       "static-site-smoke",
			Title:        "Create hero section module",
			Goal:         "Create a reusable hero section HTML module that uses the provided hero image asset and direct marketing copy without doctype, html, head, or body document wrapper tags.",
			Status:       coding.TaskStatusPending,
			AllowedPaths: []string{"sections/hero.html"},
			AcceptanceChecks: []coding.AcceptanceCheck{
				{Description: "Hero module includes a heading, supporting copy, and a provided image asset reference."},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:           "section-product",
			PlanID:       "static-site-smoke",
			Title:        "Create product section module",
			Goal:         "Create a product section HTML module that uses the provided product image asset and explains the coding loop without doctype, html, head, or body document wrapper tags.",
			Status:       coding.TaskStatusPending,
			AllowedPaths: []string{"sections/product.html"},
			AcceptanceChecks: []coding.AcceptanceCheck{
				{Description: "Product module includes explanatory copy and a provided image asset reference."},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:           "style-base",
			PlanID:       "static-site-smoke",
			Title:        "Create base styles",
			Goal:         "Create base CSS with document-level typography, colors, box sizing, and image behavior for a static website.",
			Status:       coding.TaskStatusPending,
			AllowedPaths: []string{"styles/base.css"},
			AcceptanceChecks: []coding.AcceptanceCheck{
				{Description: "Base stylesheet defines root colors, typography, body reset, and responsive image defaults."},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:           "style-components",
			PlanID:       "static-site-smoke",
			Title:        "Create component styles",
			Goal:         "Create component CSS for the header, footer, hero section, product section, and mobile layout.",
			Status:       coding.TaskStatusPending,
			AllowedPaths: []string{"styles/components.css"},
			AcceptanceChecks: []coding.AcceptanceCheck{
				{Description: "Component stylesheet includes layout rules and a mobile breakpoint."},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:           "assemble-site",
			PlanID:       "static-site-smoke",
			Title:        "Assemble static site",
			Goal:         "Create browser-openable index.html that integrates the completed section modules, stylesheets, and provided image assets.",
			Status:       coding.TaskStatusPending,
			AllowedPaths: []string{"index.html", "sections/hero.html", "sections/product.html", "styles/base.css", "styles/components.css"},
			DependsOn:    []string{"section-hero", "section-product", "style-base", "style-components"},
			AcceptanceChecks: []coding.AcceptanceCheck{
				{Description: "index.html has doctype, html, head, linked CSS, body, main, and image asset references."},
				{Description: "index.html includes the section copy from the completed modules without placeholder sentinels."},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:           "docs",
			PlanID:       "static-site-smoke",
			Title:        "Document static site usage",
			Goal:         "Create README.md with concise instructions for opening the static website and understanding the generated files.",
			Status:       coding.TaskStatusPending,
			AllowedPaths: []string{"README.md"},
			DependsOn:    []string{"assemble-site"},
			AcceptanceChecks: []coding.AcceptanceCheck{
				{Description: "README explains how to open index.html and mentions the local CSS and image assets."},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	return coding.Plan{
		ID:          "static-site-smoke",
		SessionID:   sessionID,
		ProjectRoot: root,
		Title:       "Static site smoke",
		Summary:     "Module-first static website plan for bounded workers.",
		Status:      coding.PlanStatusDraft,
		Tasks:       tasks,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func applyAndApproveStaticPatch(t *testing.T, m model, taskID string, files []coding.WorkerFileEdit) model {
	t.Helper()
	updated, _, handled := m.applyWorkerPatch(coding.WorkerPatch{
		TaskID:  taskID,
		Summary: "Applied " + taskID,
		Files:   files,
	}, false)
	if !handled {
		t.Fatalf("applyWorkerPatch handled = false for %s", taskID)
	}
	m = updated.(model)
	if m.status != "task reviewing" {
		t.Fatalf("status after patch %s = %q", taskID, m.status)
	}
	updated, _, handled = m.applyReviewVerdict(coding.ReviewVerdict{
		Verdict: coding.ReviewApprove,
		Summary: "Approved " + taskID,
	}, nil)
	if !handled {
		t.Fatalf("applyReviewVerdict handled = false for %s", taskID)
	}
	m = updated.(model)
	if m.status != "task done" {
		t.Fatalf("status after review %s = %q", taskID, m.status)
	}
	return m
}

func latestTaskByID(t *testing.T, m model, taskID string) coding.Task {
	t.Helper()
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("LatestPlan ok = false")
	}
	task, ok := taskByID(plan.Tasks, taskID)
	if !ok {
		t.Fatalf("task %q not found", taskID)
	}
	return task
}

func assertStaticSiteSmokeOutput(t *testing.T, root string) {
	t.Helper()
	required := []string{
		"index.html",
		"styles/base.css",
		"styles/components.css",
		"sections/hero.html",
		"sections/product.html",
		"README.md",
	}
	for _, path := range required {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("required output %s: %v", path, err)
		}
	}
	html := readSmokeFile(t, root, "index.html")
	for _, fragment := range []string{
		"<!doctype html>",
		"<html lang=\"en\">",
		"<head>",
		"<main>",
		"styles/base.css",
		"styles/components.css",
		"assets/hero.png",
		"assets/product.png",
		"Build small, review hard, ship clean.",
		"A coding loop that respects context",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("index.html missing %q\n%s", fragment, html)
		}
	}
	for _, path := range required {
		assertNoPlaceholderSentinels(t, path, readSmokeFile(t, root, path))
	}
}

func readSmokeFile(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(data)
}

func assertNoPlaceholderSentinels(t *testing.T, path, content string) {
	t.Helper()
	lower := strings.ToLower(content)
	for _, sentinel := range []string{
		"existing content",
		"rest of the file",
		"existing styles",
		"omitted for brevity",
		"previous content here",
		"remaining content unchanged",
	} {
		if strings.Contains(lower, sentinel) {
			t.Fatalf("%s contains placeholder sentinel %q", path, sentinel)
		}
	}
}

type liveWorkerResult struct {
	task      coding.Task
	patch     coding.WorkerPatch
	telemetry modelTelemetry
	err       error
}

func runLiveWorkerBatch(t *testing.T, m model, tasks []coding.Task, deadline time.Time) []liveWorkerResult {
	t.Helper()
	results := make([]liveWorkerResult, len(tasks))
	for i, task := range tasks {
		packet, err := m.buildWorkerPacketForRun(task)
		if err != nil {
			results[i] = liveWorkerResult{task: task, err: err}
			continue
		}
		if err := m.recordTaskBaseline(task); err != nil {
			results[i] = liveWorkerResult{task: task, err: err}
			continue
		}
		if err := m.store.UpdateTaskStatus(task.ID, coding.TaskStatusRunning); err != nil {
			results[i] = liveWorkerResult{task: task, err: err}
			continue
		}
		payload, _ := json.Marshal(packet)
		_, _ = m.store.AddTaskEvent(coding.TaskEvent{
			TaskID:  task.ID,
			Type:    "parallel_worker_start",
			Message: "Live E2E worker dispatch prepared.",
			Payload: payload,
		})
		results[i].task = task
		results[i].patch.TaskID = packet.TaskID
	}

	var wg sync.WaitGroup
	for i, task := range tasks {
		if results[i].err != nil {
			continue
		}
		packet, err := m.buildWorkerPacketForRun(task)
		if err != nil {
			results[i].err = err
			continue
		}
		wg.Add(1)
		go func(i int, task coding.Task, packet coding.TaskPacket) {
			defer wg.Done()
			timeout := minDuration(m.workerRequestTimeout(), time.Until(deadline))
			if timeout <= 0 {
				results[i].err = fmt.Errorf("live E2E run SLA exceeded before worker dispatch")
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			start := time.Now()
			raw, usage, err := m.generateWorkerPatchJSON(ctx, packet)
			telemetry := m.modelTelemetry("worker", time.Since(start), len(raw), 0, usage)
			if err != nil {
				results[i].err = err
				return
			}
			if time.Now().After(deadline) {
				results[i].err = fmt.Errorf("live E2E run SLA exceeded after worker response")
				return
			}
			patch, repairAttempts, err := m.workerPatchFromGeneratedJSON(ctx, packet, raw)
			telemetry.JSONRepairAttempts = repairAttempts
			if err != nil {
				results[i].err = fmt.Errorf("%w\nraw:\n%s", err, raw)
				return
			}
			results[i] = liveWorkerResult{task: task, patch: patch, telemetry: telemetry}
		}(i, task, packet)
	}
	wg.Wait()
	return results
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return b
	}
	if a < b {
		return a
	}
	return b
}

func smokeToolRegistry(root string) *tools.Registry {
	registry := tools.NewRegistry()
	limits := tools.Limits{
		WorkspaceRoots: []string{root},
		PythonBin:      strings.TrimSpace(os.Getenv("WEAZLCODE_E2E_PYTHON")),
	}
	registry.Register(tools.NewCalculatorTool())
	registry.Register(tools.NewRunVerificationCommandTool(limits))
	registry.Register(tools.NewGitDiffTool(limits))
	registry.Register(tools.NewListChangedFilesTool(limits))
	return registry
}

func taskIDs(tasks []coding.Task) string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return strings.Join(ids, ",")
}

func taskStatusSummary(tasks []coding.Task) string {
	counts := map[string]int{}
	for _, task := range tasks {
		counts[task.Status]++
	}
	parts := make([]string, 0, len(counts))
	for status, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", status, count))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func assertLiveStaticSiteOutput(t *testing.T, root, copyText string) {
	t.Helper()
	index := readSmokeFile(t, root, "index.html")
	cssFiles, err := filepath.Glob(filepath.Join(root, "**", "*.css"))
	if err != nil {
		t.Fatalf("glob css: %v", err)
	}
	if len(cssFiles) == 0 {
		if _, err := os.Stat(filepath.Join(root, "styles.css")); err != nil {
			t.Fatalf("no CSS output found")
		}
	}
	lower := strings.ToLower(index)
	for _, fragment := range []string{
		"<!doctype html",
		"<html",
		"<head",
		"<main",
		"weazl suite",
		"neon soul",
		"weazlchat",
		"weazltunes",
		"weazlwrite",
		"weazlcode",
		"github.com/bprendie",
		".png",
	} {
		if !strings.Contains(lower, fragment) {
			t.Fatalf("index.html missing %q\n%s", fragment, index)
		}
	}
	if !strings.Contains(lower, "stylesheet") && !strings.Contains(lower, "<style") {
		t.Fatalf("index.html has no stylesheet or inline style\n%s", index)
	}
	for _, path := range []string{"index.html", "styles.css", "README.md"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err == nil {
			assertNoPlaceholderSentinels(t, path, string(data))
		}
	}
	if len(index) < 5000 {
		t.Fatalf("index.html is too small to contain the supplied site copy coherently: %d bytes", len(index))
	}
	for _, fragment := range smokeRequiredFragmentsFromCopy(copyText) {
		if !strings.Contains(index, fragment) {
			t.Fatalf("index.html missing exact supplied fragment %q\n%s", fragment, index)
		}
	}
	if strings.Contains(index, "...") {
		t.Fatalf("index.html contains shortened ellipsis copy\n%s", index)
	}
}

func smokeRequiredFragmentsFromCopy(copyText string) []string {
	candidates := []string{
		"Sovereign AI with a neon soul. A completely local TUI suite for chat, code, writing, and tuneage.",
		"Because your shower thoughts shouldn't end up in someone else's training cluster.",
		"Free internet radio for long nights at the keyboard.",
		"A sovereign text editor for a paranoid age.",
		"The serious one, allegedly. Still a work in progress.",
		"git clone https://github.com/bprendie/WeazlChat",
		"git clone https://github.com/bprendie/WeazlTunes",
		"git clone https://github.com/bprendie/WeazlWrite",
		"git clone https://github.com/bprendie/WeazlCode",
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.Contains(copyText, candidate) {
			out = append(out, candidate)
		}
	}
	return out
}
