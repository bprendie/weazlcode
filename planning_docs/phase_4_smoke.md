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

Result: setup/config path passed; local-only runtime blocked by environment.

- Checked local Ollama on `localhost:11434`: no service was listening.
- Checked local vLLM on `localhost:8000`: no service was listening.
- Because no local worker endpoint was available, the pure localhost north-star split mode could not be completed in this environment.
- The setup path for custom planning/review plus a configured vLLM-compatible worker was rerun with runtime-only temp config and passed. It wrote distinct planning/review roles while keeping worker/summarizer on the configured vLLM provider.

### Same Endpoint For All Roles

Result: passed.

- Used a user-provided runtime-only vLLM-compatible temp config for all roles.
- Ran the LLM completion smoke against the configured provider/model.
- The provider returned the expected JSON response and token usage.
- Ran a full TUI loop in a disposable git repo:
  - drafted a bounded README task,
  - edited allowed paths and acceptance checks,
  - validated and approved the plan,
  - dispatched `/run-task`,
  - ran `/run-worker`,
  - persisted worker packet, worker output, and diff artifacts,
  - ran `/run-reviewer`,
  - persisted reviewer rationale,
  - reached task complete.
- The worker changed only the allowed `README` path. The reviewer approved because the diff satisfied the acceptance check and stayed within scope.

### None/Local Fallback

Result: passed with runtime vLLM-compatible provider.

- The setup path for `none` already writes planning/review/worker/summarizer roles to the configured local provider and prints the robustness warning.
- The same TUI smoke used this fallback shape: all roles resolved to the configured vLLM-compatible provider through `primary-vllm`.
- The warning remains useful: without a stronger planning/review provider, plan quality depends entirely on the configured worker model.

## Remaining Reliability Gaps

- Run the full TUI loop for true local-worker split mode once a local worker endpoint is running on this machine.
- The TUI display did not visibly refresh while `/run-worker` was finishing, even though artifacts and the diff were written. Track this as a UX polish issue for long-running async commands.
- The small worker model satisfied the acceptance check but made a broader README replacement than a human would for the tiny prompt. Existing path/reviewer guardrails allowed it because scope and acceptance were valid; suspicious-rewrite heuristics should continue to be tuned with real repo tasks.
