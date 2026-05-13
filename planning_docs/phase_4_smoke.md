# Phase 4 Smoke

Date: 2026-05-13

## Setup UX Smoke

The setup binary was rebuilt and run against temp config/data paths for all planning/review LLM provider choices.

Smoke paths:

- OpenAI: writes `planning-llm` and `review-llm` as OpenAI-compatible providers, normalizes the base URL without `/v1`, and keeps the local worker as `primary-vllm`.
- Claude: writes `planning-llm` and `review-llm` as Anthropic providers, uses the Claude defaults, and keeps the local worker as `primary-vllm`.
- Custom: writes OpenAI-compatible planning/review providers, trims a pasted `/v1` suffix, and preserves separate planning and review model names.
- None: prints the local-fallback warning and leaves planning/review roles resolved to the configured local provider.

Observation:

- The initial smoke run caught stale binary behavior: the old setup binary prompted for tool keys before the LLM provider. Rebuilding confirmed the fixed order, and a regression test now covers the prompt sequence.

## Live Smoke Matrix

Runtime endpoint configuration stayed outside the repository.

### Local Worker Plus Planning LLM

Result: blocked by environment.

- Checked local Ollama on `localhost:11434`: no service was listening.
- Checked local vLLM on `localhost:8000`: no service was listening.
- Because no local worker endpoint was available, the north-star split mode could not be completed in this environment.

### Same Endpoint For All Roles

Result: passed at runtime client level.

- Used the runtime-only vLLM-compatible temp config.
- Ran the LLM completion smoke against the configured provider/model.
- The provider returned the expected JSON response and token usage.

### None/Local Fallback

Result: blocked by environment.

- The setup path for `none` already writes planning/review/worker/summarizer roles to the configured local provider and prints the robustness warning.
- Runtime fallback execution requires a local Ollama or vLLM service; neither was available during this smoke pass.

## Remaining Reliability Gaps

- Run the full TUI loop for local-worker split mode once a local worker endpoint is running.
- Run the full TUI loop for none/local fallback once a local provider is running.
- Same-endpoint model connectivity is verified, but the full TUI same-endpoint task loop should be rerun after choosing a narrow safe repo edit.
