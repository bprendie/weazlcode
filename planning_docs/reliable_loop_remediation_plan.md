# Reliable Loop Remediation Plan

## Context

WeazlCode is not yet MVP. The split-brain architecture is in place and the local worker loop can make real progress, but live smoke tests still require supervision. The current reliability gap is not one missing feature; it is the coordination between planner shape, local-worker repair behavior, deterministic validators, and reviewer escalation.

The next pass should improve the generic coding loop, not tune WeazlCode to any one Flappy/Pygame smoke test.

## Current Findings

- Planner quality improved when integration tasks are allowed to edit dependency module files.
- Semi-serial plans are often better than maximum parallelism when later modules depend on generated interfaces.
- Local workers can draft usable modules, but often fail at final wiring and entrypoint structure.
- Repair packets need to force material progress on the file implicated by the latest failure.
- Static validators are useful only when they catch high-confidence issues; noisy heuristics waste repair cycles.
- Reviewer feedback should guide semantic repairs, but deterministic validator failures should not become the whole product.
- The loop needs a clear stop condition when a worker no-ops, repeats, or edits unrelated files.

## Generic Fixes Already Landed

- Runtime plan quality repair for integration scopes and coordinator dependencies.
- Worker output guard for runaway streaming or repeated generated blocks.
- Path rejection guidance and conservative singular/plural path auto-correction.
- Stale path-rejection guidance no longer overrides newer reviewer repair requests.
- Worker repair guard rejects patches that do not touch files named by artifact validation.
- Task diff artifacts are scoped to the task instead of leaking parent repository diffs.
- Deterministic bonus repair pass for syntax, missing imports, undefined names, attribute errors, and constructor/type errors.
- Python artifact validator fixes for loop targets, conditional function definitions, top-level loops, and previously initialized locals.

## Next Remediation Goals

### 1. Demote Noisy Validators

Audit artifact validation and divide checks into:

- Blocking: syntax errors, missing imports/exports, undefined names, missing smoke path for interactive entrypoints, obvious path violations.
- Advisory: style, heuristic branch-scope warnings, gameplay semantics, rendering polish, subjective completeness.

Only blocking checks should stop the worker loop before review. Advisory checks should be passed to the reviewer as context.

### 2. Require Material Repair Progress

A repair should be rejected early when:

- It does not touch the file named by the latest validator/reviewer blocker.
- The normalized diff for the implicated file is empty or only whitespace/comments.
- The same failure fingerprint appears after the same effective file content.

The fingerprint should be based on task diff/content state, not only the worker JSON hash.

### 3. Improve Entrypoint Contracts

For generated apps, planners should state the entrypoint contract explicitly:

- Define `smoke_test()` and `main()` at module scope.
- Dispatch under `if __name__ == "__main__"`.
- Run `--smoke` before entering interactive loops.
- Keep event-loop state inside `main()`.
- Avoid lambda-heavy event handling in generated code.

This should be generic for Python interactive apps, not Pygame-specific.

### 4. Reduce Frontier Rescue Drift

The reviewer should not become the coder by accident. When a local worker cannot repair after bounded attempts:

- Mark the task blocked with exact failure evidence.
- Ask the planner for a smaller replacement task or interface clarification.
- Do not let the reviewer silently rewrite code unless the user explicitly escalates.

### 5. Add Smoke Matrix

Use multiple unrelated smoke tasks so we do not overfit:

- Pygame game from scratch.
- Small FastAPI service with one endpoint and tests.
- Static website from copy/assets.
- Existing Go repo feature change.
- Existing Python repo bug fix.

Pass criteria:

- Completes without manual intervention.
- Local worker writes code.
- Frontier roles plan/review, but do not write most implementation.
- Soft target under 5 minutes for small greenfield tasks.
- If it fails, failure is bounded, specific, and actionable.

## Stop Conditions

Stop live smoke runs when any of these happen:

- GPU remains busy after the task has no visible progress.
- The same validation fingerprint appears three times with no material file diff.
- A single task exceeds the repair budget without touching the implicated file.
- Total run exceeds the soft ceiling and the trace shows no convergence.

## Next Session Checklist

- [ ] Audit validators and demote noisy checks.
- [ ] Add content-based material-progress detection for repair attempts.
- [ ] Strengthen generic Python entrypoint packet guidance.
- [ ] Add a planner replan path for blocked final wiring tasks.
- [ ] Run a non-Pygame smoke before retrying the Flappy/Pygame smoke.
- [ ] Update MVP criteria based on smoke matrix results.
