# Artifact Mode Remediation Plan

Goal: a single-page static website with supplied copy and local assets should complete end-to-end in five minutes or less on the split-brain loop.

The benchmark is not "can WeazlCode eventually repair its way into a page." The benchmark is "can the frontier model create a usable implementation brief, can the local worker generate the coherent artifact, and can review/validation close the loop with at most one focused repair." Codex can produce this class of page in about three minutes; WeazlCode should land it in no more than five.

## Current Diagnosis

- [x] The split-brain architecture is viable: frontier planning/review plus local/vLLM worker execution.
- [x] The planner can now choose one cohesive task for a single landing page.
- [x] The old 4096 output-token cap was a hard blocker for whole-file artifacts.
- [x] Granite can produce complete `index.html` and `styles.css` when given a larger artifact budget.
- [x] Claude review is useful and caught real defects.
- [ ] WorkerPatch JSON is still fragile for large file payloads.
- [x] Review `needs_fix` previously restored the task baseline; for create-only tasks this deleted the generated file before repair.
- [ ] Repair passes are still too broad and can fight local rewrite guardrails.
- [x] Local validators now catch deterministic Python syntax/runtime-shape issues before Claude review.
- [ ] Local validators are still incomplete for full runtime behavior, so reviewer/harness checks remain necessary.
- [ ] Whole-file artifact tasks still share too much machinery with surgical patch tasks.

## Target Loop

For cohesive artifacts such as one-page sites, generated documents, README-heavy pages, or small standalone tools:

1. Planner creates one implementation brief.
2. Worker generates complete allowed files in one call using `files[]`.
3. Local validators run before frontier review.
4. Reviewer reviews the full deliverable once.
5. If needed, one focused repair runs against validator/reviewer issues.
6. Local validators run again.
7. Final review or local pass completes the task.

Five-minute target budget:

- Planner: 15-30 seconds.
- Worker artifact generation: 60-180 seconds.
- Local validation: under 2 seconds.
- Reviewer: 5-20 seconds.
- One focused repair, if needed: 60-120 seconds.

Runtime budget defaults:

- Artifact Mode: 5 minutes is a soft target for trace warnings and performance review.
- Patch Mode: 3 minutes is a soft target for trace warnings and performance review.
- Parallel Worker Mode: 8 minutes is a soft target for trace warnings and performance review.
- Autonomous run hard cap: 15 minutes by default (`workers.run_timeout_seconds`).
- Planner call: 60 seconds.
- Reviewer call: 60 seconds.

Runs that exceed the soft target are not automatically failed, especially on vLLM workers running around 18-25 tokens/sec. WeazlCode should surface target overages clearly, keep tracing convergence, and only hard-fail when the configured run timeout is exceeded.

## Phase 1: Separate Artifact Mode From Patch Mode

- [x] Add a default autonomous run hard cap (`workers.run_timeout_seconds`, default 900).
- [x] Stop autonomous runs when the SLA is exceeded and record `run_timeout`.
- [x] Add an explicit task mode/classification heuristic for cohesive whole-file artifact tasks.
- [x] Classify tasks owning `index.html`, `styles.css`, generated docs, or whole-file static deliverables as `artifact`.
- [x] Keep artifact mode whole-file oriented: prefer `files[]`, empty `patch`, complete content for every allowed output file.
- [ ] Keep patch mode diff oriented: focused unified diffs for existing code edits.
- [x] Make rewrite guardrails mode-aware so full-file artifact tasks can rewrite their owned files while still rejecting placeholders and scope violations.

## Phase 2: Frontier Implementation Brief

- [ ] Planner should emit a worker brief, not just a task goal, for artifact mode.
- [ ] Brief must include:
  - exact source copy block or copy manifest
  - asset manifest
  - allowed output files
  - required page sections or deliverable structure
  - design direction
  - forbidden shortcuts: ellipses, paraphrase, TODOs, placeholders, inline CSS when CSS file is required
  - deterministic acceptance checks
- [ ] Attach the brief to the worker packet as first-class context.
- [ ] Add tests proving supplied copy and asset filenames survive into the artifact worker packet.

## Phase 3: Deterministic Validators

- [x] Add artifact validators before reviewer approval.
- [x] Static site validators:
  - `index.html` exists and has doctype/html/head/body.
  - linked CSS file exists.
  - no `<style>` tags when CSS is assigned to a `.css` file.
  - supplied asset filenames are referenced when the task asks for assets/images.
  - required copy fragments are present exactly.
  - no TODO/omitted/unchanged/placeholder sentinels.
  - CSS braces are balanced and obvious selector corruption is rejected.
- [x] Validator failures create a retryable worker failure before calling the reviewer.
- [x] Reviewer only runs after deterministic validators pass.
- [ ] Add command/repo URL-specific validators beyond the source-copy fragment check.
- [x] Add Python static validation for syntax, obvious undefined names, method/instance shadowing, closure-aware nested helpers, and broken `--smoke` branch shape.

## Phase 4: Robust WorkerPatch Transport

- [x] Recover common malformed JSON escapes.
- [x] Recover loose `path`/`content` file pairs from malformed large WorkerPatch JSON.
- [ ] Prefer a simpler artifact response schema for artifact mode:
  - `task_id`
  - `summary`
  - `files`
  - `blocker`
  - no `patch` field required
- [ ] Consider accepting fenced file blocks as a fallback transport for artifact mode.
- [ ] Add tests for multi-file artifact output larger than normal chat responses.

## Phase 5: Focused Repair

- [ ] Artifact repairs should receive:
  - [x] current generated files, preserved in the workspace after review rejection
  - [x] validator failures
  - [x] reviewer issues, if any
  - instruction to edit only the failing parts
- [x] Review rejection no longer performs automatic baseline cleanup; the failed output becomes the next repair target.
- [x] Repair packets explicitly tell the worker to perform a surgical repair against the current workspace.
- [x] Single-file generated artifacts must use full `files[]` content rather than fragile unified diffs.
- [ ] Repair should not regenerate unrelated content.
- [x] Invalid diff/patch repair must respect the remaining run hard cap.
- [x] Repeated local validator failures should escalate to a frontier-generated repair brief instead of letting the same small-model repair loop spin until timeout.
- [ ] If the issue is CSS-only, repair packet should allow only `styles.css`.
- [ ] If the issue is inline CSS, repair packet should explicitly remove `<style>` from HTML and move needed rules to CSS.
- [ ] Repair loop limit for artifact mode: one automatic validator repair, then reviewer repair only if needed.

## Phase 6: Smoke Target

Use `/home/bobp/test/site.weazl` with local PNG assets and supplied copy. Do not give the planner the live URL.

Pass criteria:

- [ ] Plan has one artifact task for `index.html` and `styles.css`.
- [ ] Worker produces both files in one call.
- [ ] Deterministic validators pass before reviewer.
- [ ] Reviewer either approves or requests one focused repair.
- [ ] End-to-end runtime is under five minutes.
- [ ] Manual inspection: page is coherent, browser-openable, image-backed, and not a thin summary.

## Non-Goals

- Do not optimize for microtask parallelism on a single cohesive artifact.
- Do not hardcode the Weazl Suite smoke copy or asset names into production logic.
- Do not remove parallel workers; they remain key for genuinely independent deliverables.
- Do not make Claude write the code. Frontier models plan, validate, and review; local workers implement.
