# Phase 3 Smoke

Date: 2026-05-13

Runtime endpoint configuration stayed outside the repository.

## Deterministic Smoke

`TestPhase3GeneratedPlanVerificationSmoke` covers generated-plan verification behavior:

- Go repo: discovered verification defaults to allowlisted Go build/test commands.
- Python repo: discovered verification defaults to `python -m pytest`.
- No-build repo: no speculative verification command is injected.
- Generated/imported verification commands are filtered through the execution allowlist.

## Runtime Smoke

- The first sandboxed runtime smoke failed with `lookup ... Temporary failure in name resolution`, which was a sandbox network restriction rather than a client transport bug.
- The same Go LLM client passed a runtime-only `CompleteWithUsage` smoke when network access was allowed, including returned token usage.
- With network access allowed, the TUI completed `/plan generate` against the runtime vLLM-compatible endpoint.
- The TUI completed `/run-worker`; the worker returned structured full-file edits, WeazlCode applied them, and verification ran.
- The TUI completed `/run-reviewer`; the reviewer returned an approving verdict and moved the task to done.

## Observations

- Structured full-file edits are useful for small/local models, but broad task packets can still produce plausible irrelevant documentation. The orchestrator and plan editing workflow need to keep tasks narrow and concrete.
- The live run wrote artifacts under `.weazlcode/runs/` for plan, worker packet, worker output, diff, verification, and review.
- Token usage is now captured for non-stream completion calls where providers expose usage.
