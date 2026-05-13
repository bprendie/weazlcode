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
