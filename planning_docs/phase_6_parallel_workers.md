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
- Worker patch validation and reviewer guardrails are unchanged.

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

The remaining unchecked Phase 6 item is a successful live worker completion/review pass once the runtime endpoint is healthy.
