# WeazlCode Context Compaction Plan

## Product Stance

WeazlCode uses opinionated context management. The app should preserve enough detail for real coding, tool use, and long-running local-model sessions without turning configuration into a cockpit.

Chosen direction:

- Optimize for balanced fidelity and responsiveness.
- Use multi-checkpoints as durable timeline markers.
- Keep behavior opinionated; no user-facing compaction knobs for now.
- Treat large context windows as headroom, not as a target to fill.

## Current Behavior

- Chat messages are stored from zero in encrypted SQLite.
- Context compaction writes rows to `context_checkpoints`.
- Runtime context uses only the latest checkpoint summary plus messages after that checkpoint.
- Original messages remain in SQLite and can be replayed or queried later.
- Auto-compaction currently triggers at 97% of the configured context window.
- Summary target currently scales as `context_window / 32`, bounded to 500-2000 tokens.

## Current Lossiness

Current lossiness is acceptable for small windows, but too aggressive at large windows:

- 8k window: roughly 8k into 500 tokens.
- 32k window: roughly 24k-31k into about 1000 tokens.
- 128k window: potentially 120k+ into 2000 tokens.

The 128k case is too lossy if compaction waits until the hard limit, and too slow because large contexts are repeatedly replayed before compaction.

## Target Design

Use two limits:

- Soft working-context threshold: triggers normal auto-compaction early enough to keep chat responsive.
- Hard safety threshold: prevents running into the actual model context wall.

Use multiple checkpoint rows as a timeline:

- Keep every checkpoint row in SQLite.
- Use the latest checkpoint for normal prompt context.
- Preserve older checkpoint rows for future replay, inspection, and possible retrieval.

Keep recent context verbatim:

- Auto-compaction should summarize older messages through a selected boundary.
- The current user prompt must stay outside the checkpoint and be sent normally.
- A later phase should retain a recent verbatim tail instead of always compacting right up to the previous message.

## Phase 1: Safer Defaults

Implement now:

- Replace fixed 97% auto-trigger with opinionated thresholds.
- Keep a 97% hard guard as a final safety net.
- Increase summary target for large windows.
- Count tool-call metadata in token estimates consistently.

Proposed thresholds:

- <= 8k: compact around 92%.
- <= 16k: compact around 85%.
- <= 32k: compact around 75%.
- > 32k: compact around 50%, capped near 49k tokens.

Proposed summary target:

- Minimum: 500 tokens.
- Scale with context window.
- Allow large windows to use more than 2000 tokens, capped around 6000.

## Phase 2: Recent Tail Retention

Goal:

- When auto-compacting, summarize older context but keep a recent verbatim tail.

Reasoning:

- The last several turns often contain active task details, exact file names, and current constraints.
- Keeping a tail reduces summary pressure and avoids losing fresh local context.

Implementation shape:

- Pick `throughID` by walking backward until a target recent-token budget remains.
- Manual `ctrl+t` can still compact through the latest message.
- Auto-compaction should not summarize the current user prompt.

Opinionated starting point:

- Keep about 8k recent tokens for 32k+ windows.
- Keep about 4k recent tokens for 16k windows.
- Keep about 2k recent tokens for 8k windows.

## Phase 3: Checkpoint Timeline Use

Goal:

- Use multiple checkpoint rows for inspection and possible recovery, while still sending only the latest checkpoint during normal chat.

Potential UI:

- Workspace/session replay can show checkpoint markers.
- A future command could list checkpoints for a session.

Potential model context:

- Normal prompts keep latest checkpoint only.
- Future advanced mode could stitch selected older checkpoints when the user asks about long-past work.

## Phase 4: Auditability

Goal:

- Make compaction visible and explainable.

Possible additions:

- Status text with summarized token estimate and retained token estimate.
- Checkpoint metadata such as source token estimate and summary token estimate.
- Optional internal-only debug view for checkpoint rows.

## Non-Goals

- No user-facing compaction tuning knobs yet.
- No vector store.
- No automatic retrieval from all historical messages.
- No deletion of old messages during compaction.

