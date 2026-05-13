# Plan Generate Smoke

Date: 2026-05-13

## Goal

Validate the first orchestrator-backed plan generation path.

## Result

Passed.

- Added `/plan generate <request>`.
- The command calls the configured `orchestrator` role with strict JSON instructions.
- Generated model output is parsed with the existing strict plan decoder.
- Valid generated plans are prepared with session/project IDs and stored as draft plans.
- TUI smoke generated and displayed a pending draft plan using a disposable runtime config.

## Notes

The smoke used a runtime-only vLLM-compatible endpoint from a `/tmp` config. No endpoint URL was added to repository files.

This is a synchronous first pass. It validates the command path and schema handling, but the TUI blocks while waiting for the orchestrator response. A later hardening pass should move plan generation onto an async command path with progress state and cancellation.

## Next Step

Add repair handling for invalid generated JSON:

- capture parser error
- ask the orchestrator to repair the raw response into the exact schema
- retry once
- show raw response and parser error if repair fails
