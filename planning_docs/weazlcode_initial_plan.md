# WeazlCode Initial Plan

## Goal

WeazlCode is a local-first terminal AI coding platform built from the WeazlChat foundation. The goal is to keep the interface and codebase simple while adding the core coding-agent workflow people expect from tools like Crush:

- project-aware chat sessions
- workspace file inspection and editing
- command execution with permissions
- coding plans and task breakdowns
- multi-model routing
- resumable work logs
- lightweight project memory
- optional LSP, MCP, hooks, and skills over time

The twist: frontier subscription models act as the orchestrator, reviewer, and planner, while cheaper local models served by vLLM or Ollama do bounded implementation work.

Reference: Charmbracelet Crush, https://github.com/charmbracelet/crush

## Product Shape

WeazlCode should feel closer to WeazlChat than to a large IDE agent framework:

- one terminal app
- one config file
- one local SQLite store
- simple defaults
- explicit permission prompts for risky actions
- compact project/session picker
- no cloud backend
- local model endpoints first

The main loop:

1. User opens WeazlCode in a repo.
2. WeazlCode loads project context, saved instructions, git status, and relevant session state.
3. User asks for a change.
4. Orchestrator model creates a plan and splits work into small modules.
5. Worker model handles bounded implementation tasks using restricted tools.
6. Orchestrator reviews diffs, runs verification, asks worker for fixes, or presents result.
7. User approves final changes, commit message, or next task.

## Starting Point From WeazlChat

Reusable pieces:

- Go module and command layout
- Bubble Tea TUI structure
- streaming LLM client
- vLLM/OpenAI-compatible provider support
- Ollama provider support
- encrypted SQLite session storage
- workspace saves
- context compaction
- tool registry
- file/search/read tools
- command execution guardrails
- Markdown rendering with Glamour
- installer/config flow

Likely replacements:

- package/module name: `github.com/bprendie/weazlcode`
- binary names: `weazlcode`, maybe `weazlcode-setup`
- config path: `~/.config/weazlcode/config.json`
- data path: `~/.local/share/weazlcode/` or existing storage pattern renamed
- system prompt: from general chat to coding-agent orchestration
- TUI views: from chat/workspace saves to project/session/task/diff views

## Crush-Like Capability Map

Implement early:

- multi-provider and multi-model config
- model switching by role
- project sessions
- workspace-aware context
- restricted file tools
- restricted command tools
- edit/apply patch tool
- git diff/status/log tools
- initialization file, likely `AGENTS.md` or `WEAZLCODE.md`
- permission policy for tool classes
- logs under project-local `.weazlcode/logs/`

Implement after MVP:

- LSP context for symbols, definitions, diagnostics
- MCP server support
- skill discovery from `.agents/skills`, `.weazlcode/skills`, and global paths
- hooks before/after tool calls, before/after submit, after task completion
- custom providers beyond vLLM/Ollama/OpenAI-compatible
- project ignore file, likely `.weazlcodeignore`
- notifications

Do not copy early unless needed:

- a huge provider catalog
- deeply nested config schema
- broad tool/plugin ecosystem
- autonomous background task daemon
- multi-user or remote service behavior

## Model Roles

Define model roles instead of treating every model as interchangeable:

- `orchestrator`: frontier model used for planning, decomposition, final review, risk assessment, and user-facing explanations.
- `worker`: local vLLM/Ollama model used for bounded code changes.
- `reviewer`: frontier model used for diff review, test failure analysis, and final quality gate. This can default to the orchestrator.
- `summarizer`: cheap local model or frontier fallback for session compaction.
- `router`: simple local rules first; optional model-driven routing later.

Example config shape:

```json
{
  "models": {
    "orchestrator": {
      "provider": "codex",
      "model": "subscription-default"
    },
    "worker": {
      "provider": "ollama",
      "base_url": "http://localhost:11434",
      "model": "qwen2.5-coder:14b"
    },
    "reviewer": {
      "provider": "claude",
      "model": "subscription-default"
    }
  }
}
```

Subscription providers may not expose normal API access. We need to decide whether WeazlCode integrates them through official APIs, command-line tools, browser/session bridges, or manual copy/paste workflows. The clean MVP is API-based orchestration plus local worker execution.

## Orchestration Design

The orchestrator should produce structured task packets for workers:

```json
{
  "task_id": "task-001",
  "goal": "Add a workspace diff viewer",
  "allowed_paths": ["internal/tui", "internal/tools"],
  "forbidden_paths": ["internal/storage"],
  "context_files": ["internal/tui/model.go", "internal/tools/files.go"],
  "commands_allowed": ["go test ./..."],
  "acceptance_checks": [
    "go test ./...",
    "manual TUI renders without panic"
  ],
  "instructions": "Keep the existing Bubble Tea style and avoid changing storage."
}
```

Workers should receive small tasks with constrained write sets. They should not independently redesign the app. If the worker cannot complete the task within constraints, it returns a blocker instead of improvising.

## Tooling Needed For MVP

Core tools:

- `list_files`
- `search_files`
- `read_file`
- `write_file` or `apply_patch`
- `git_status`
- `git_diff`
- `git_log`
- `run_command`
- `run_tests`
- `project_summary`

Safety policy:

- read-only tools can auto-run
- file edits require an active task and allowed paths
- commands run through an allowlist first
- destructive commands require explicit user confirmation
- network commands require explicit user confirmation
- worker models never get unrestricted shell access

## Storage

Extend the WeazlCode storage model rather than replacing it:

- projects table
- sessions table
- messages table
- tasks table
- task_events table
- tool_calls table
- model_runs table
- checkpoints table
- memories table

Important: store enough metadata to audit why a code change happened:

- model role
- model name
- prompt hash or summary
- tool calls
- files touched
- command results
- reviewer verdict

## TUI Views

Initial views:

- Chat: primary interaction
- Plan: current task tree and statuses
- Diff: pending file changes
- Tools: recent tool calls and approvals
- Sessions: saved project sessions
- Config: active model roles and endpoints

Keybindings can follow the WeazlCode style:

- `ctrl+n`: new session
- `ctrl+p`: plan view
- `ctrl+d`: diff view
- `ctrl+t`: trim context
- `ctrl+r`: sessions
- `ctrl+s`: save session/workspace
- `ctrl+e`: edit/rename
- `esc`: back to chat

## MVP Milestones

### Milestone 1: Rename And Baseline

- copy WeazlCode into WeazlCode
- rename module, binary, config paths, README, installer
- keep existing chat, tools, storage, and context compaction working
- add project-root detection
- add `.weazlcode/` project data/log directory

### Milestone 2: Coding Tools

- add patch/edit tool
- add git status/diff/log tools
- tighten command execution around coding workflows
- add project ignore support
- show tool approvals clearly in TUI

### Milestone 3: Plan/Worker Loop

- add structured plan generation
- store tasks and task events
- call worker model for bounded implementation tasks
- review worker patches with orchestrator/reviewer
- run verification commands
- present final diff and review summary

### Milestone 4: Project Context

- add project initialization command
- generate `WEAZLCODE.md` or `AGENTS.md`
- include git status, file map, language detection, and build/test commands
- add context packing for relevant files

### Milestone 5: Crush-Inspired Extensions

- LSP diagnostics and symbol context
- skill discovery
- MCP client support
- hooks
- notifications
- richer provider configuration

## Open Decisions

- Should the copied WeazlChat handover/history docs stay in the repo, move under planning docs, or be replaced with fresh WeazlCode docs?
- Should the project instruction file be `AGENTS.md`, `WEAZLCODE.md`, or both?
- Should frontier orchestration use official APIs first, CLI subscriptions first, or support both?
- Should workers edit directly, or should all worker output be patches reviewed before applying?
- Should WeazlCode support parallel worker tasks in the MVP, or start with one worker at a time?
- Should task execution be fully automatic after approval, or step through each module interactively?

## Recommended First Build

Start conservative:

1. Finish the baseline rename from copied WeazlChat sources.
2. Add a `coding_agent` package that can create structured plans but does not execute them yet.
3. Add git diff/status tools.
4. Add an `apply_patch` tool with path restrictions.
5. Add model roles to config.
6. Wire the first loop: user request -> orchestrator plan -> user approval -> local worker patch -> reviewer verdict -> final diff.

This keeps the first version shippable while proving the core idea: expensive models supervise, local models produce bounded code, and the app stays small enough to understand.
