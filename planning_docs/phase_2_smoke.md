# Phase 2 Smoke

Date: 2026-05-13

Runtime config stayed outside the repository.

## Result

- Created a temporary git-backed repo with `README.md`.
- Ran `/plan generate` through the configured vLLM-compatible endpoint.
- Ran `/attach README.md 1-1` to attach a file range to the draft task.
- Inspected `/task 1`.
- Approved the generated plan.
- Ran `/run-task` and `/run-worker`.
- Worker changed `README.md` and moved the task to review.
- Ran `/review-diff`.
- Approved with `/review approve Phase 2 smoke diff reviewed`.
- Ran `/final-review`.
- Ran `/export-run`.

## Observations

- The README diff was produced:
  - `old wording`
  - `Phase 2 smoke testing has passed.`
- Run artifacts were written for plan, worker packet, worker output, diff, verification error, review, and final export.
- The generated verification command used `grep`, which is outside the current verification allowlist. WeazlCode captured that as a `verification_error` artifact while preserving the review flow.

## Follow-Up

- Completed in Phase 3: generated and imported verification commands are filtered through the same allowlist used by execution, and default worker verification now comes from discovered project commands.
