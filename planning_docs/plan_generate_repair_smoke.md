# Plan Generate Repair Smoke

Date: 2026-05-13

## Goal

Add a one-shot repair path for invalid orchestrator plan JSON.

## Result

Implemented.

- `/plan generate <request>` first parses the raw orchestrator response with the strict plan decoder.
- If parsing fails, WeazlCode sends the raw response and parser error back to the configured orchestrator role.
- The repair prompt asks for the exact plan schema and forbids unknown fields.
- The repaired response is parsed once.
- If repair still fails, the TUI shows the initial parse error, repair parse error, raw response, and repaired response.

## Test Coverage

Focused tests cover:

- fenced generated JSON extraction and parsing
- malformed generated JSON triggering repair prompt construction
- repaired JSON parsing into a draft plan
- project instructions, discovered commands, project memory, and user request appearing in the generation prompt

## Next Smoke

Live `/plan generate` was rerun with a runtime-only vLLM-compatible config after this change. It generated and displayed a pending draft plan successfully.
