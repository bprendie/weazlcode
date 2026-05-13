# Stage 3 Worker Model Smoke

Date: 2026-05-13

## Goal

Exercise the single-worker loop against a runtime vLLM-compatible worker model.

## Result

Partially passed with useful findings.

Implemented and tested:

- `/run-worker` calls the configured `worker` role after `/run-task`.
- Worker prompts require a `WorkerPatch` JSON object.
- Worker responses are parsed with the strict worker patch decoder.
- Non-JSON worker responses trigger one repair request.
- Valid blockers are imported through the existing worker blocker path.

Runtime smoke:

- A temporary project under `/tmp` was used so no repository files were modified.
- `/plan draft` -> `/approve` -> `/run-task` -> `/run-worker` reached the runtime worker model.
- Patch-shaped response path reached patch application, but the model produced an invalid unified diff and apply rejected it.
- Blocker-shaped response path succeeded: the model returned a valid blocker and WeazlCode marked the task blocked.

## Notes

The endpoint URL was used only through a disposable `/tmp` config and is intentionally not stored in repository code or documentation.

The invalid patch result is expected at this stage and confirms the next reliability work: worker patch repair or stricter patch-only prompting before applying. The safety layer behaved correctly by rejecting the invalid patch.

## Next Step

Add worker patch repair and/or preflight validation:

- validate the patch before applying
- if invalid, ask the worker to repair only the unified diff
- retry once
- keep blockers as the preferred escape hatch when the local model is uncertain
