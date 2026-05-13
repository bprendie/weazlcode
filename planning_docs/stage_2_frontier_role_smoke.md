# Stage 2 Frontier Role Smoke

Date: 2026-05-13

## Goal

Validate that WeazlCode can point orchestrator and reviewer roles at an API-compatible vLLM endpoint while keeping worker execution separate.

## Result

Runtime-only role smoke passed:

- disposable config under `/tmp` mapped roles as expected:
  - `orchestrator` -> frontier provider
  - `worker` -> primary local-compatible provider
  - `reviewer` -> frontier provider
  - `summarizer` -> primary local-compatible provider
- API-compatible chat completions endpoint was reachable
- strict planner prompt returned compact valid plan JSON
- reviewer prompt returned valid verdict JSON with `approve`
- TUI started successfully with the disposable role config

## Notes

The endpoint URL was used only as a runtime value during smoke testing. It is intentionally not stored in repository code or documentation.

The first broad planner prompt produced plausible content but drifted from the exact WeazlCode plan schema and hit the token cap. A stricter prompt returned the expected JSON shape. This confirms the next implementation step should use tighter structured prompts, schema validation, and repair prompts for orchestrator-produced plans.

## Next Step

Wire a first-class `/plan generate <request>` command that:

- builds orchestrator context from project instructions and memories
- asks the configured orchestrator role for strict plan JSON
- validates with the existing plan parser
- stores valid plans as drafts
- returns parser errors with a repair prompt path instead of silently accepting loose JSON
