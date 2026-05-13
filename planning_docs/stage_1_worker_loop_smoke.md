# Stage 1 Worker Loop Smoke

Date: 2026-05-13

## Goal

Exercise the single-worker loop before moving on to frontier planner/reviewer validation.

## Result

Deterministic WeazlCode loop smoke passed:

- plan approval gates worker dispatch
- worker packet generation works
- worker patch import applies a path-validated patch
- verification command output is captured
- reviewer input includes diff and verification output
- reviewer approval marks the task and plan done
- final review, commit message, and run artifact export paths work

Full test suite passed with the same code path covered by focused TUI tests.

## Live Model Status

Live local-worker dispatch is blocked in this environment because no default local model endpoint is currently reachable:

- Ollama `http://localhost:11434/api/tags`: connection refused
- vLLM `http://localhost:8000/v1/models`: connection refused

Neither `ollama` nor `vllm` is available on `PATH` from this shell.

## Commands Run

```sh
curl -fsS http://localhost:11434/api/tags
curl -fsS http://localhost:8000/v1/models
go test ./internal/tui -run 'TestSlashRunTaskMarksTaskRunning|TestSlashWorkerPatchAppliesPatchAndMarksReviewing|TestSlashReviewerInputCommand|TestSlashReviewApproveMarksTaskDone|TestFinalReviewCommitMessageAndExport' -count=1 -v
go test ./...
```

## Next Requirement

Start a local worker server, then rerun Stage 1 with an actual local model:

- Ollama: `ollama serve` with a tool-capable model available
- vLLM: OpenAI-compatible server on `http://localhost:8000`

After that passes, move to Stage 2: point orchestrator/reviewer roles at an API-compatible frontier endpoint and validate the review packet end to end.
