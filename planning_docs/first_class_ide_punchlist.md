# WeazlCode First-Class IDE Punchlist

North star: frontier models plan/review; local models execute bounded task packets. Keep `planning_docs/north_star_architecture.md` aligned with this punchlist.

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
- [x] Remove copied WeazlChat handover/history docs that are not current WeazlCode planning material.
- [ ] Make first baseline commit.

## Milestone 1: Project Awareness

- [x] Add `internal/project`.
- [x] Detect git repo root from the current working directory.
- [x] Fall back to current directory for non-git projects.
- [x] Create project-local `.weazlcode/` for logs, transient state, and metadata.
- [x] Add `.weazlcodeignore` support using gitignore-style patterns.
- [x] Add project summary data: root, language hints, file counts, git branch, dirty state.
- [x] Store active project on each session.
- [x] Show project root and git branch in the TUI status area.

## Milestone 2: Coding Tools

- [x] Add `git_status` tool.
- [x] Add `git_diff` tool.
- [x] Add `git_log` tool.
- [x] Add `git_show` tool for commit/file inspection.
- [x] Add `apply_patch` tool with workspace-root restrictions.
- [x] Add `read_file_range` tool for targeted context.
- [x] Add `list_changed_files` tool.
- [x] Split command execution into `run_readonly_command` and `run_verification_command`.
- [x] Add command policy presets for Go, Node, Python, Rust, and shell projects.
- [x] Log every coding tool call under `.weazlcode/logs/`.

## Milestone 3: Model Roles

- [x] Extend config with model roles: `orchestrator`, `worker`, `reviewer`, `summarizer`.
- [x] Preserve existing `active_provider` behavior as a compatibility default.
- [x] Add role-to-provider resolution in config.
- [ ] Let setup configure local worker first.
- [ ] Let config manually point orchestrator/reviewer to API-compatible frontier endpoints.
- [ ] Add role labels to status output and model run logs.
- [ ] Keep worker model access restricted to task packets and approved tools.

## Milestone 4: Structured Plans

- [x] Add `internal/coding`.
- [x] Define `Plan`, `Task`, `AcceptanceCheck`, and `ReviewVerdict` structs.
- [x] Add JSON schema or strict parser for orchestrator-produced plans.
- [x] Store plans and tasks in SQLite.
- [x] Add plan status values: `draft`, `approved`, `running`, `blocked`, `done`, plus task `reviewing`.
- [x] Add local `/plan` and `/tasks` commands in the TUI.
- [x] Add user approval before the first worker task runs.
- [x] Add task event history for model runs, tool calls, file edits, and checks.

## Milestone 5: Single-Worker Loop

- [x] Create task packet format for the worker model.
- [x] Include goal, allowed paths, forbidden paths, relevant files, command limits, and acceptance checks.
- [ ] Worker can request context through tools instead of receiving the whole repo.
- [x] Worker produces a patch, not a free-form final answer.
- [x] Gate task dispatch on approved plans and mark selected tasks running.
- [x] Apply patch only after path validation.
- [x] Run configured verification commands.
- [x] Reviewer receives diff, test output, and task requirements.
- [x] Reviewer returns `approve`, `needs_fix`, or `blocked`.
- [x] On `needs_fix`, send one focused repair task back to the same worker.
- [x] Cap repair loops with a small limit, probably 2.

## Milestone 6: IDE TUI

- [x] Add slash-command mode for local app commands.
- [x] Add durable view modes: chat, plan, diff, tools, sessions, config.
- [x] Add diff viewer with file list and hunks.
- [ ] Add tool-call approval modal.
- [x] Add task progress indicator.
- [ ] Add recent command output panel.
- [ ] Add fuzzy file picker.
- [ ] Add file preview pane.
- [ ] Add status badges for dirty repo, current branch, active role, and context usage.
- [ ] Keep existing copy/mouse/scroll behavior working.

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
