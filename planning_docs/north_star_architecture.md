# WeazlCode North Star

## Core Strategy

WeazlCode exists to make local coding models useful by keeping their work small, bounded, and reviewed.

The system should use frontier models such as OpenAI, Claude, or Gemini for oversight and planning, while dispatching coding work to local vLLM/Ollama models in chunks small enough for them to succeed.

## Role Split

### Frontier Models

Frontier models are responsible for judgment-heavy work:

- understand the user request
- inspect project state at a high level
- create structured plans
- split plans into small implementation tasks
- define acceptance checks
- review local-worker patches
- interpret verification failures
- decide whether work is approved, blocked, or needs a focused repair
- explain the result to the user

Frontier models should not be used for every edit by default. Their job is supervision, not bulk code generation.

### Local Models

Local models are responsible for bounded implementation:

- receive one task packet at a time
- operate only on allowed files/paths
- request narrow context through approved tools
- produce patches, not broad prose
- stay inside the assigned task
- report blockers instead of redesigning the plan

Local workers should never receive vague repo-wide prompts like "fix this project." They should receive precise, small, auditable tasks.

### Modular Code North Star

Generated code should be modular by default. This is not just style; it is how the split-brain model stays practical for small local workers.

- Prefer files around 300 lines or less.
- Files over 300 lines need a concrete reason in the plan.
- Files over 500 lines should usually be split unless the user explicitly requests a single file or the artifact is inherently single-file.
- Interactive apps, games, APIs, CLIs, and tools should usually be decomposed into focused modules: domain logic, UI/rendering, persistence/adapters, entrypoint, and smoke/verification.
- Parallelism should come from clean module boundaries, not fake microtasks.
- Local workers should own complete small files or tight patches, not sprawling all-in-one artifacts.

### WeazlCode

WeazlCode is the orchestrator and safety layer:

- stores plans, tasks, task events, and audit logs
- packs context for local workers
- enforces allowed paths and tool policy
- applies patches only after validation
- runs verification commands
- captures diffs and command output
- hands results to the frontier reviewer
- keeps the user in control through approvals and slash commands

## Task Packet Shape

Every local-worker task should eventually be reducible to a packet like this:

```json
{
  "task_id": "task-001",
  "goal": "Add plan persistence tests",
  "allowed_paths": ["internal/storage"],
  "forbidden_paths": ["cmd", "internal/tui"],
  "context_files": [
    "internal/storage/plans.go",
    "internal/storage/plans_test.go"
  ],
  "tools_allowed": [
    "read_file",
    "read_file_range",
    "search_files",
    "apply_patch"
  ],
  "verification": [
    "go test ./internal/storage"
  ],
  "acceptance_checks": [
    "Plan round trip stores tasks",
    "Task events round trip with JSON payload"
  ]
}
```

The local worker returns a patch or a blocker. It does not directly decide whether the work is complete.

## Review Packet Shape

The frontier reviewer should receive:

- original user request
- approved plan
- worker task packet
- patch/diff
- verification command output
- task event log summary
- known constraints

The reviewer returns one of:

- `approve`
- `needs_fix`
- `blocked`

If the verdict is `needs_fix`, WeazlCode should create one focused repair task for the same local worker. Repair loops should be capped.

## Non-Negotiable Constraints

- Single worker first. Parallel workers wait until the single-worker loop is reliable.
- Worker tasks must have explicit allowed paths.
- Patches must be validated before applying.
- Verification commands must be policy-controlled.
- Tool calls and task events must be auditable.
- Frontier models make the final quality call.
- Local models should be replaceable: vLLM, Ollama, or any future local provider.
- The app must stay understandable and smaller than the systems it is inspired by.

## Near-Term Build Order

1. Define `TaskPacket`, `WorkerPatch`, and reviewer input/output structs.
2. Add context-packing helpers for selected files and line ranges.
3. Add slash/debug views that show exactly what a worker would receive.
4. Add model calls for orchestrator plan generation.
5. Add model calls for local worker patch generation.
6. Add reviewer verdict flow.
7. Add approval and repair-loop controls.

This document should be treated as the architectural north star. If a feature makes local workers less bounded, weakens frontier review, or hides the audit trail, it should be reconsidered.
