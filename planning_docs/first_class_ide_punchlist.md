# WeazlCode First-Class IDE Punchlist

## Direction

WeazlCode should become a first-class terminal IDE for AI-assisted coding, not just a chat app with file tools. The first production workflow should use one worker at a time:

1. User asks for a repo change.
2. Orchestrator creates a structured plan.
3. User approves or edits the plan.
4. One local worker performs one bounded task.
5. Reviewer checks the diff and command output.
6. WeazlCode either asks the worker for a focused fix or presents the final diff.

Single-worker execution is the right first target. It keeps state, permissions, diffs, and review understandable while we build the IDE substrate.

## Milestone 0: Baseline Hygiene

- [x] Copy WeazlChat source into WeazlCode.
- [x] Rename module/import paths to `github.com/bprendie/weazlcode`.
- [x] Rename commands to `weazlcode` and `weazlcode-setup`.
- [x] Rename config, data, env vars, and installer paths.
- [x] Add `.gitignore` for local caches and copied binaries.
- [x] Mark README as work in progress.
- [x] Keep the same license as WeazlChat.
- [x] Verify tests pass after the rename.
- [ ] Decide whether `handover.md` stays, moves under `planning_docs/`, or becomes a fresh `AGENTS.md`.
- [ ] Make first baseline commit.

## Milestone 1: Project Awareness

- [ ] Add `internal/project`.
- [ ] Detect git repo root from the current working directory.
- [ ] Fall back to current directory for non-git projects.
- [ ] Create project-local `.weazlcode/` for logs, transient state, and metadata.
- [ ] Add `.weazlcodeignore` support using gitignore-style patterns.
- [ ] Add project summary data: root, language hints, file counts, git branch, dirty state.
- [ ] Store active project on each session.
- [ ] Show project root and git branch in the TUI status area.

## Milestone 2: Coding Tools

- [ ] Add `git_status` tool.
- [ ] Add `git_diff` tool.
- [ ] Add `git_log` tool.
- [ ] Add `git_show` tool for commit/file inspection.
- [ ] Add `apply_patch` tool with workspace-root restrictions.
- [ ] Add `read_file_range` tool for targeted context.
- [ ] Add `list_changed_files` tool.
- [ ] Split command execution into `run_readonly_command` and `run_verification_command`.
- [ ] Add command policy presets for Go, Node, Python, Rust, and shell projects.
- [ ] Log every coding tool call under `.weazlcode/logs/`.

## Milestone 3: Model Roles

- [ ] Extend config with model roles: `orchestrator`, `worker`, `reviewer`, `summarizer`.
- [ ] Preserve existing `active_provider` behavior as a compatibility default.
- [ ] Add role-to-provider resolution in `internal/llm`.
- [ ] Let setup configure local worker first.
- [ ] Let config manually point orchestrator/reviewer to API-compatible frontier endpoints.
- [ ] Add role labels to status output and model run logs.
- [ ] Keep worker model access restricted to task packets and approved tools.

## Milestone 4: Structured Plans

- [ ] Add `internal/coding`.
- [ ] Define `Plan`, `Task`, `AcceptanceCheck`, and `ReviewVerdict` structs.
- [ ] Add JSON schema or strict parser for orchestrator-produced plans.
- [ ] Store plans and tasks in SQLite.
- [ ] Add plan status values: `draft`, `approved`, `running`, `blocked`, `reviewing`, `done`.
- [ ] Add a plan view in the TUI.
- [ ] Add user approval before the first worker task runs.
- [ ] Add task event history for model runs, tool calls, file edits, and checks.

## Milestone 5: Single-Worker Loop

- [ ] Create task packet format for the worker model.
- [ ] Include goal, allowed paths, forbidden paths, relevant files, command limits, and acceptance checks.
- [ ] Worker can request context through tools instead of receiving the whole repo.
- [ ] Worker produces a patch, not a free-form final answer.
- [ ] Apply patch only after path validation.
- [ ] Run configured verification commands.
- [ ] Reviewer receives diff, test output, and task requirements.
- [ ] Reviewer returns `approve`, `needs_fix`, or `blocked`.
- [ ] On `needs_fix`, send one focused repair task back to the same worker.
- [ ] Cap repair loops with a small limit, probably 2.

## Milestone 6: IDE TUI

- [ ] Add durable view modes: chat, plan, diff, tools, sessions, config.
- [ ] Add diff viewer with file list and hunks.
- [ ] Add tool-call approval modal.
- [ ] Add task progress indicator.
- [ ] Add recent command output panel.
- [ ] Add fuzzy file picker.
- [ ] Add file preview pane.
- [ ] Add status badges for dirty repo, current branch, active role, and context usage.
- [ ] Keep existing WeazlChat copy/mouse/scroll behavior working.

## Milestone 7: LSP Foundation

- [ ] Add LSP process manager.
- [ ] Detect language servers from project files.
- [ ] Start with Go via `gopls`.
- [ ] Collect diagnostics.
- [ ] Add symbol search.
- [ ] Add definition lookup.
- [ ] Add references lookup.
- [ ] Include diagnostics in task packets.
- [ ] Show diagnostics in a TUI panel.

## Milestone 8: First-Class Project Instructions

- [ ] Decide primary instruction file: `AGENTS.md`, `WEAZLCODE.md`, or both.
- [ ] Add `weazlcode init`.
- [ ] Generate project instructions from detected commands, layout, and conventions.
- [ ] Load project instructions into orchestrator context.
- [ ] Add command discovery for common test/build/lint commands.
- [ ] Add project memory records distinct from chat memories.

## Milestone 9: Review And Commit Workflow

- [ ] Add final review summary.
- [ ] Add generated commit message.
- [ ] Add optional `git add`/`git commit` flow behind confirmation.
- [ ] Add rollback guidance using patch reverse or git checkout instructions.
- [ ] Add session artifact export under `.weazlcode/runs/`.

## Later Extensions

- [ ] MCP client support.
- [ ] Skills discovery.
- [ ] Hooks before/after tool calls and task completion.
- [ ] Notifications.
- [ ] Parallel workers after single-worker reliability is proven.
- [ ] External editor integration.
- [ ] Debug adapter protocol support.

## Immediate Next Build Order

1. Move or rewrite `handover.md` into project-native guidance.
2. Add `internal/project` and project root detection.
3. Add git status/diff/log tools.
4. Add `apply_patch` with path restrictions.
5. Add model roles in config.
6. Add plan structs and SQLite tables.
7. Build the first single-worker task loop.
