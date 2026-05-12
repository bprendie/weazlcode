# AGENT HANDOVER: WeazlCode

## STATUS
- Repo root: `/home/bobp/Code/weazlcode`
- Current intended branch state: latest committed/pushed work includes large-file refactor, MIT license, README updates, installer context presets, mouse-scroll/copy-mode toggle, context checkpointing, tool suite.
- Latest known commit before this file: `c9679e5 Split large implementation files`
- Current uncommitted runtime change: Glamour-powered Markdown rendering for assistant transcript output, plus workspace replay through session messages up to the saved message ID.
- This file is intentionally untracked unless the user explicitly asks to commit it.
- Known unrelated untracked path: `.bob/`; do not stage it.
- Installed binary path used by the user: `/home/bobp/.weazlcode/bin/weazlcode`

## PRODUCT IDENTITY
- WeazlCode is a private, local-first AI chat TUI for vLLM and Ollama servers.
- Keep the app fun, personal, and nostalgic without turning copy into forced comedy.
- Core vibe: direct 90s terminal utility, local AI workflows, honest telemetry, minimal fluff.
- User explicitly likes the wrench icon/tool indicator and playful spinner phrases.
- User explicitly rejected adding hosted OpenAI/Grok/Gemini provider presets. vLLM is OpenAI-compatible, but frame support as local/custom endpoint support, not hosted provider marketing.
- License is MIT. README may ask substantially modified forks to use different name/visual branding, but do not add restrictive code license terms beyond MIT unless the user asks.

## NON-NEGOTIABLE IMPLEMENTATION RULES
- Preserve local-first defaults and privacy posture.
- Assistant Markdown rendering is default-on, config-backed, and must fall back to plain wrapping if rendering fails.
- Do not expose full tool/API payloads in the chat transcript; display compact `🔧 using tools` and feed tool results back to the model internally.
- Tool execution must stay bounded by configured safety rules.
- File-writing tool must remain create-only and must refuse overwrites.
- Workspace file/shell/sqlite tools must remain confined to configured `workspace_roots`.
- Fetch URL must reject private/local network targets.
- Shell tool must remain allowlisted and must not execute through a raw shell.
- Context autotrim threshold is 97% of configured provider context window.
- Manual context trim is `ctrl+t`.
- Mouse wheel scrolling is default. `ctrl+m` toggles copy mode by disabling mouse capture; pressing again restores mouse scroll.
- Preserve large paste behavior: store full prompt payload, show compact `[PASTED n lines]` in input.
- Keep source files near the soft 400-line ceiling when practical. Split by responsibility before adding large blocks.
- Do not revert user changes unless explicitly asked.
- Do not stage `.bob/`.
- Do not read or print user API keys from `~/.config/weazlcode/config.json` unless necessary for the task.

## REPO TREE MAP
- `cmd/weazlcode/main.go`: runtime entrypoint. Loads config, opens/migrates encrypted SQLite storage, registers tools, starts Bubble Tea with alt screen and mouse cell motion.
- `cmd/weazlcode-setup/main.go`: setup CLI used by install script. Prompts provider type, base URL, model, context preset, optional tool API keys, workspace roots. Writes config.
- `cmd/weazlcode-setup/main_test.go`: setup tests.
- `internal/config/config.go`: JSON config model, defaults, path resolution, provider/tool config.
- `internal/llm/client.go`: vLLM/Ollama client, streaming, message conversion, tool call streaming and summarization entrypoint.
- `internal/llm/requests.go`: non-streaming requests, summarization completions, HTTP POST, base URL normalization.
- `internal/llm/client_test.go`: LLM client tests.
- `internal/storage/storage.go`: SQLite store setup, migrations, vault/session helpers, AES-GCM encryption/decryption.
- `internal/storage/messages.go`: add/load encrypted messages, tool metadata, context checkpoint persistence.
- `internal/storage/workspace_memory.go`: workspace saves and encrypted local memories.
- `internal/storage/storage_test.go`: storage tests.
- `internal/tools/tools.go`: registry, definitions, safe-level dispatch.
- `internal/tools/limits.go`: output/file limits and truncation.
- `internal/tools/calculator.go`: calculator tool.
- `internal/tools/datetime.go`: current time/timezone tool.
- `internal/tools/weather.go`: Open-Meteo keyless current weather/forecast tool.
- `internal/tools/stock.go`: Alpha Vantage stock quote tool.
- `internal/tools/websearch.go`: Brave Search tool.
- `internal/tools/fetch.go`: HTTP fetch with local/private network rejection.
- `internal/tools/files.go`: list/search/read/create local files under workspace roots.
- `internal/tools/command.go`: read-only allowlisted command execution; args only, no raw shell.
- `internal/tools/sqlite.go`: read-only SQLite query tool.
- `internal/tools/memory.go`: encrypted local memory operations.
- `internal/tools/*_test.go`: tool tests.
- `internal/tui/model.go`: Bubble Tea model state, central Update loop, message state, spinner state.
- `internal/tui/chat.go`: chat submit flow, vault/server states, prompt history, paste handling, mouse/copy toggle.
- `internal/tui/context.go`: manual/autotrim context checkpointing and summarization orchestration.
- `internal/tui/render.go`: view rendering, wrapping, metrics, token estimates, truncation.
- `internal/tui/markdown.go`: Glamour Markdown renderer wrapper with width-aware caching and plain-wrap fallback.
- `internal/tui/sessions.go`: session list/resume/delete and workspace save/list UI.
- `internal/tui/stream.go`: starts LLM streaming with tool definitions.
- `internal/tui/tool_execution.go`: executes tool calls, stores assistant/tool messages, continues model stream.
- `internal/tui/styles.go`: Lip Gloss styles.
- `internal/tui/gradient.go`: logo/gradient rendering.
- `scripts/install.sh`: builds setup/app, installs to `~/.weazlcode/bin`, handles PATH, runs setup unless skipped.
- `README.md`: user-facing docs. Keep tone fun, nostalgic, accurate, and not sterile.
- `LICENSE`: MIT.
- `config.example.json`: full example config with tools.
- `weazlcode.png`: README screenshot.
- `weazlcode`: local binary artifact, gitignored.

## RUNTIME FLOW
1. `cmd/weazlcode/main.go` loads `~/.config/weazlcode/config.json`, creating defaults if absent.
2. It opens SQLite storage at configured/default DB path.
3. It asks for/validates local vault password when needed.
4. It creates the LLM client from active provider.
5. It registers tools from config.
6. It starts Bubble Tea in alt screen with mouse support.
7. User sends prompt.
8. TUI builds history from checkpoint plus newer messages.
9. LLM streams answer and may emit tool calls.
10. Tool calls execute locally, results are encrypted/stored, then fed back to LLM for final response.
11. Transcript displays natural model text plus compact tool indicator only.

## CONFIG CONTRACT
- Main config path: `~/.config/weazlcode/config.json`
- Default providers:
  - `local-vllm`: type `vllm`, server `http://localhost:8000`, model `local-model`
  - `local-ollama`: type `ollama`, server `http://localhost:11434`, model `llama3.1`
- Active provider read at runtime; endpoints/models must not be hardcoded in the client.
- Provider base URL normalization:
  - vLLM base URL strips accidental `/v1`
  - Ollama base URL strips accidental `/api`
- Provider `context_window` default: `32768`
- Installer context presets:
  - `small`: `8192`
  - `medium`: `16384`
  - `large`: `32768` default
  - `xl`: `128000` with OOM warning
- Tool config fields:
  - `enabled`
  - `auto_execute_safe`
  - `alpha_vantage_api_key`
  - `brave_api_key`
  - `workspace_roots`
  - `max_output_chars`
  - `max_file_bytes`
- Go field for `auto_execute_safe` may be named `AutoExecute`; preserve JSON name.
- UI config fields:
  - `resume_last_session`
  - `render_markdown`
  - `markdown_style`
- `render_markdown` defaults to true. Use a pointer bool in Go so explicit JSON `false` survives defaults.
- `markdown_style` defaults to `dark`; `auto` must be treated as `dark`, not `glamour.WithAutoStyle`, because terminal color-query replies can leak into Bubble Tea input as text like `]11;rgb:...`.
- Default DB path is under XDG data or `~/.local/share/weazlcode/weazlcode.sqlite3`.

## DATABASE AND SECURITY CONTRACT
- SQLite is local.
- Payloads are AES-GCM encrypted.
- Vault password hash uses bcrypt for password check.
- Encryption key currently derives from sha3 of `"weazlcode/local-only/"+password`.
- Security copy must stay honest: this protects local privacy and prying eyes; it is only as strong as the vault password and is not a high-assurance hardware-backed secret system.
- Tables:
  - `vault`
  - `sessions`
  - `messages`
- `workspace_saves`
  - `memories`
  - `context_checkpoints`
- `messages` stores encrypted role/content plus tool metadata where needed.
- Assistant messages with `tool_calls` and tool-role messages with `tool_call_id` must survive persistence/load because OpenAI-compatible tool continuation requires them.
- Workspace saves include encrypted snapshot text for compatibility plus `through_message_id` for point-in-time replay through current rendering.
- Deleting a session deletes dependent messages/workspace saves and relies on FK/cascade for context checkpoints where applicable.

## LLM CONTRACT
- vLLM uses OpenAI-compatible chat completions.
- Ollama uses Ollama chat API shape.
- Streaming must support normal deltas and tool call deltas.
- Empty prompt must not be appended as a user message during tool continuation.
- Non-streaming summarization lives in `internal/llm/requests.go`.
- Context checkpoint summarization should create a compact operational summary suitable for future prompts.
- Summary target tokens are computed as `clamp(context_window / 32, 500, 2000)`.
- Do not add hosted provider-specific auth/UI unless user reverses prior decision.

## TUI CONTRACT
- Bubble Tea app with alt screen.
- Mouse scrolling default via `tea.WithMouseCellMotion()`.
- `ctrl+m` toggles copy mode:
  - copy mode: `tea.DisableMouse`
  - mouse mode: `tea.EnableMouseCellMotion`
- Key map:
  - `enter`: send/select
  - `up`/`down`: prompt history in chat; selection movement in lists
  - `mouse wheel`: scroll chat history
  - `pgup`/`pgdown`: scroll chat history
  - `home`/`end`: top/bottom chat
  - `ctrl+m`: copy/mouse mode toggle
  - `ctrl+n`: new session
  - `ctrl+r`: resume session history
  - `ctrl+d`: delete selected session
  - `ctrl+s`: save workspace view
  - `ctrl+w`: list workspace saves
  - `ctrl+t`: trim context
  - `esc`: back to chat
  - `ctrl+c`: quit
- Status line displays:
  - context usage progress bar
  - input token estimate `in`
  - output token count `out`
  - generation speed `t/s`
- Spinner behavior:
  - normal thinking: Bubble spinner, currently `spinner.Jump`
  - tool continuation: `spinner.Points`
  - context compaction: distinct block/meter animation
  - phrase changes at most twice in a long response, roughly at 20s and 40s
- Spinner phrases should prefer gerund/`-ing` style. Current phrase set includes:
  - `hacking_the_gibson`
  - `jacking_into_the_matrix`
  - `breaching_corporate_ice`
  - `overclocking_neural_link`
  - `tracing_the_uplink`
  - `decrypting_sector_7`
  - `sniffing_data_packets`
  - `bypassing_firewall_01`
  - `rerouting_the_mainframe`
  - `uploading_virus_payload`
  - `accessing_black_ice`
  - `mapping_the_grid`
  - `ghosting_the_network`
  - `prying_open_the_vault`
  - `optimizing_cyberdeck`
  - `draining_the_data_well`
  - `spoofing_host_protocol`
  - `syncing_with_the_construct`
  - `scrambling_bio_signals`
  - `buuu_ddy`
  - `wheezing_the_juice`
  - `munching_the_grindage`
  - `chilling_the_tokens`
  - `chilling_up_on_here`
  - `taxing_the_gig`
- Assistant messages in the saved transcript are rendered with Glamour Markdown.
- User messages stay plain-wrapped.
- Streaming assistant text stays plain-wrapped while incomplete; after the assistant message is saved and reloaded, it renders with Glamour.
- Workspace selection reloads the linked session messages through saved `through_message_id` and renders that transcript through the active Markdown renderer instead of displaying an old encrypted viewport snapshot.
- Older workspace saves without `through_message_id` fall back to full-session replay.

## CONTEXT CHECKPOINT CONTRACT
- Manual trim: `ctrl+t`.
- Auto trim: when estimated context reaches 97% of active provider `context_window`.
- Trim should include existing checkpoint plus all older conversation content being compacted.
- Current user prompt must stay outside the checkpoint and be sent normally after trim.
- Future requests should send checkpoint summary plus only newer messages, not replay full session from zero.
- Checkpoint content should be compact but useful for ongoing coding/task context, not a generic chat recap.

## TOOL CONTRACT
- Tool registry exposes definitions to tool-capable models.
- Safe tools can auto-execute when `tools.auto_execute_safe` is true.
- Tool output is truncated by configured limits before returning to model.
- Available general tools:
  - calculator: add/subtract/multiply/divide/power/sqrt/percentage
  - current time: local machine time or IANA timezone
  - weather: Open-Meteo current weather and short forecast; keyless
  - stock price: Alpha Vantage; requires key
  - web search: Brave Search; requires key
  - fetch URL: HTTP/HTTPS readable text; rejects private/local network
- Local workspace tools:
  - `list_files`
  - `search_files`
  - `read_file`
  - `create_file`
  - read-only command
  - SQLite read-only query
  - local memory
- `create_file` creates new text files only; refuses existing paths.
- Command tool allowlist includes commands such as `pwd`, `ls`, `find`, `rg`, `cat`, `git status`, `git diff`, `git log`, `git show`, `go test`, `npm test`; preserve arg-array execution.
- SQLite tool must reject mutating statements; permit read-only `SELECT`, `WITH`, `EXPLAIN`, `PRAGMA table_info`.
- Local memory operations are explicit and encrypted: `remember`, `recall`, `list_memories`, `forget`.

## BUILD TEST SMOKE COMMANDS
- Use repo-local caches to avoid polluting user/global caches:
```sh
GOCACHE=/home/bobp/Code/weazlcode/.gocache GOMODCACHE=/home/bobp/Code/weazlcode/.gomodcache go test ./...
```
- Expected: tests pass. `github.com/mattn/go-sqlite3` may emit C warnings; those are expected/non-fatal.
- Build installed runtime binary:
```sh
GOCACHE=/home/bobp/Code/weazlcode/.gocache GOMODCACHE=/home/bobp/Code/weazlcode/.gomodcache go build -buildvcs=false -o /home/bobp/.weazlcode/bin/weazlcode ./cmd/weazlcode
```
- In this Codex sandbox, writing `/home/bobp/.weazlcode/bin/weazlcode` requires escalation/approval.
- Live smoke:
```sh
/home/bobp/.weazlcode/bin/weazlcode
```
- Expected live smoke: alt-screen TUI opens, password screen appears if vault exists/needed, `ctrl+c` exits cleanly.
- Installer command commonly used with skip launch:
```sh
WEAZLCODE_SKIP_LAUNCH=1 ./scripts/install.sh
```
- Build prerequisites:
  - Go with CGO.
  - Linux: C compiler/sqlite CGO support.
  - macOS: Xcode Command Line Tools.
  - Windows: MSYS2 UCRT64 gcc or equivalent CGO-capable toolchain.
- Cross-compilation is nontrivial because `go-sqlite3` uses CGO.

## README CONTRACT
- README should include screenshot near the top: `weazlcode.png`.
- Keep tone conversational, nostalgic, and honest.
- Mention:
  - local-first vLLM/Ollama
  - installer behavior
  - context preset choices
  - telemetry `in`, `out`, `t/s`
  - context trim/manual/autotrim
  - spinner phrases
  - compact tool display
  - mouse scroll and copy mode
  - tool support and security boundaries
  - macOS/Windows build instructions
  - MIT license and branding request
- Avoid sterile enterprise copy. Avoid overclaiming security.

## RECENT DECISIONS
- User wanted all runtime changes built into installed binary; do this after runtime edits.
- User likes `🔧 using tools`.
- User likes fun spinner phrases and gerund forms.
- User chose Open-Meteo/keyless weather rather than requiring weather.gov complexity.
- User considered hosted OpenAI/Grok/Gemini, then chose to skip them.
- User considered npm packaging, chose to skip for now.
- Android discussion outcome: if ever pursued, Termux-first is likely lowest-friction; native Android port is a larger rewrite.
- Warp terminal discussion outcome: not a direct Android solution.
- User requested line-count refactor; completed without intended functionality changes.

## KNOWN RISKS AND GAPS
- Security is local privacy, not high-assurance enterprise secret management.
- Token counting is an estimate, not tokenizer-exact for every model.
- Function calling depends on model/provider support; non-tool-capable models will not use tools correctly.
- Some local models may emit malformed tool args; keep validation strict.
- Weather geocoding/forecast APIs can fail due to network/provider issues; return useful errors.
- UI rendering is terminal-dependent, especially mouse selection/shift-drag behavior.
- Windows support requires CGO setup; README should not imply a trivial pure-Go cross-compile.

## FUTURE REFACTOR TARGETS
- If splitting more files, best next candidates:
  - `cmd/weazlcode-setup/main.go`: split provider/model fetch and prompt helpers.
  - `internal/tools/weather.go`: split geocoding/weather API parsing from tool wrapper.
  - `internal/tools/files.go`: split root validation/path handling from tool operations.
  - `internal/tui/render.go`: split metrics/status/input rendering if it grows.
- Keep changes behavior-preserving unless user asks for feature work.

## CODING AGENT OPERATING RULES
- First inspect tree/status with `git status --short` and `rg --files`.
- Prefer `rg` over grep/find where possible.
- Use `apply_patch` for manual edits.
- Do not use destructive git commands.
- Do not commit unless user asks.
- When committing, avoid staging `.bob/` or unrequested artifacts.
- After behavior changes, run `go test ./...` with repo-local cache env.
- After runtime changes, rebuild `/home/bobp/.weazlcode/bin/weazlcode` if user wants installed binary updated.
- If network or out-of-workspace writes are required in this sandbox, request escalation instead of working around it.
- Keep final reports concise: changed files, tests/builds run, installed binary status, commit/push status.
