# Phase 6 Parallel Workers

Date: 2026-05-15

## Goal

Use vLLM/Ollama server parallelism while preserving bounded tasks, per-task artifacts, and frontier review.

## Implemented Behavior

- Config adds `workers.concurrency`, defaulting to `2`.
- Tasks support `depends_on` metadata.
- `/plan generate` asks the orchestrator to leave `depends_on` empty for independent work and set it only when a task must wait for another task id.
- `/plan edit <task> depends_on <ids>` lets users adjust dependencies before approval.
- `/run-workers` selects approved pending tasks up to configured concurrency.
- Parallel selection requires:
  - task status is `pending`,
  - all `depends_on` task ids are already `done`,
  - allowed paths do not overlap any active or selected task.
- Each dispatched task gets its own model run id, cancellation function, worker packet artifact, task events, telemetry, worker output, diff, verification, and review state.
- `/cancel workers` cancels active worker runs.
- `/cancel <task_id>` cancels one active worker task.
- `/cancel all` cancels active planner/reviewer/chat model work and all worker runs.
- `/run-reviewer` keeps reviewing the first queued reviewing task, so multiple completed worker tasks can be reviewed one at a time.

## Safety Rules

- Parallel dispatch never broadens worker scope.
- Tasks with overlapping allowed paths are serialized.
- Tasks with unmet dependencies are skipped.
- Unknown or empty allowed-path tasks are not selected for parallel execution unless manually run through the existing single-task path.
- Worker patch validation remains per task.
- Reviewer input and approval guardrails use the current task's allowed paths, so queued parallel diffs from other tasks do not block an otherwise valid review.

## Smoke Criteria

- Import or generate an approved plan with at least two independent pending tasks.
- Run `/run-workers`.
- Confirm up to `workers.concurrency` tasks move to `running`.
- Confirm dependent or overlapping tasks remain `pending`.
- Confirm each running task gets a packet artifact and start event.
- Confirm worker outputs move tasks independently to `reviewing`.
- Confirm reviewer can approve queued reviewing tasks one at a time.

## Runtime Smoke Attempt

Runtime endpoint configuration stayed outside the repository.

Result: dispatch path passed; provider completion blocked by endpoint health.

- Created a disposable git repo with two independent files.
- Imported an approved two-task plan with non-overlapping allowed paths.
- Ran `/run-workers` with concurrency `2`.
- Both tasks moved to `running`.
- Each task wrote its own `parallel_worker_start` event.
- Each task wrote its own worker packet run artifact.
- Both concurrent worker model calls reached the configured vLLM-compatible endpoint and returned `502 Bad Gateway`.
- A direct `/v1/models` health check against the same runtime endpoint also returned `502 Bad Gateway`.
- After this smoke, worker model errors were tightened to move the affected task to `blocked` instead of leaving it `running`.

## Runtime Smoke Rerun

Runtime endpoint configuration stayed outside the repository.

Result: full parallel worker and reviewer queue smoke passed.

- Created a disposable git repo with two independent files.
- Imported an approved two-task plan with non-overlapping allowed paths.
- Ran `/run-workers` with concurrency `2`.
- Both tasks moved to `reviewing` after concurrent worker model calls.
- Each task wrote its own `parallel_worker_start`, `worker_model`, and `worker_patch` events.
- Both workers edited only their allowed file.
- The first reviewer rerun exposed a task-scoping bug: local approval guardrails inspected the whole repo diff, so the first task was blocked by the second task's unrelated concurrent diff.
- Reviewer input and local approval guardrails now use a task-scoped git diff assembled from the current task's `allowed_paths`.
- Reran the live smoke against the same style of runtime-only vLLM-compatible config.
- `/run-reviewer` approved the first queued task, then approved the second queued task.
- Both tasks and the plan reached `done`.
