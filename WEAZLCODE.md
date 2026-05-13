# WeazlCode Project Instructions

Generated: 2026-05-13

## Project

- Root: `/home/bobp/Code/weazlcode`
- Languages: go, shell, markdown
- Git branch: `main`
- Files detected: 95

## Layout

- `cmd/`
- `internal/`
- `scripts/`
- `planning_docs/`
- `README.md`
- `LICENSE`
- `go.mod`

## Commands

- `go build ./...` - compile Go packages
- `go test ./...` - run Go tests

## Coding Conventions

- Prefer existing project patterns over new abstractions.
- Keep changes scoped to the approved task and allowed paths.
- Add or update focused tests when behavior changes.
- Preserve user edits and unrelated local changes.

## Worker Rules

- Local workers receive bounded task packets, allowed paths, diagnostics, and approved tools only.
- Request missing context with `read_file`, `read_file_range`, or `search_files` instead of guessing.
- Return patches or blockers; frontier reviewer approval is required before considering work complete.

## Reviewer Checklist

- Confirm the diff satisfies the task goal and acceptance checks.
- Check verification output before approving.
- Use `needs_fix` for narrow repairable issues and `blocked` only when more user input or context is required.
