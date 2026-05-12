# WeazlCode

> Work in progress: WeazlCode currently starts from the WeazlChat codebase and is being reshaped into a first-class local-first AI coding IDE.

![WeazlCode screenshot](weazlcode.png)

WeazlCode is a private, local-first AI coding TUI built from the WeazlChat foundation. The first cut keeps the straightforward vLLM/Ollama chat workflow intact while the project grows toward a coding platform where frontier models plan and review, and local models handle bounded implementation work.

The design target is simple: project-aware terminal sessions, local tools, clear permissions, resumable context, and enough Crush-inspired agent workflow to be useful without turning the app into a giant framework.

## Defaults

On first launch, WeazlCode drops a fresh `config.json` into `~/.config/weazlcode/` with sensible local defaults:

- `local-vllm`: `http://localhost:8000`
- model: `local-model`
- `local-ollama`: `http://localhost:11434`

Because hardcoding endpoints into a client is a terrible idea, WeazlCode reads the endpoint and model from the config at runtime.

## Run

```sh
go run ./cmd/weazlcode
```

## Install

```sh
./scripts/install.sh
```

The installer takes care of the heavy lifting. It builds `weazlcode`, tucks it into `~/.weazlcode/bin`, and adds that directory to your shell `PATH` if it is not already present.

During setup, you will be prompted for your provider type and URL. The script queries the provider for available models, optionally takes your tool API keys, writes `~/.config/weazlcode/config.json`, and boots straight into the TUI.

Provider URL rules: base URLs only, please.

- vLLM: `https://host:port` or `https://host`, without `/v1`
- Ollama: `http://host:11434`, without `/api`

If you accidentally paste the `/v1` or `/api` suffixes, the installer quietly fixes them for you. Tool API keys are optional: leave a prompt blank to keep an existing key, or type `-` to clear it.

The installer also asks for a context window preset:

- `small`: `8192` tokens
- `medium`: `16384` tokens
- `large`: `32768` tokens
- `xl`: `128000` tokens; heads up, this may cause out-of-memory errors on smaller local servers

Finally, the first run asks you to set a local history password. Session history and workspace saves are stored in SQLite with a password-protected vault and AES-GCM encrypted payloads.

Markdown rendering is enabled by default with Charmbracelet Glamour, so model output keeps its terminal-native shape: headings, lists, code blocks, links, quotes, and tables all get a little polish without turning the app into a browser.

## Build From Source

WeazlCode is a Go app, but it uses SQLite through `go-sqlite3`, so builds need Go 1.25 or newer, CGO, and a working C compiler. That is the one little bit of yak hair.

### macOS

Install Go and the Xcode command line tools:

```sh
xcode-select --install
```

Then build:

```sh
go build -o weazlcode ./cmd/weazlcode
go build -o weazlcode-setup ./cmd/weazlcode-setup
```

The install script also works on macOS-style shells:

```sh
./scripts/install.sh
```

### Windows

Install Go for Windows, then install a C compiler that Go can use with CGO. MSYS2 works well:

1. Install MSYS2 from https://www.msys2.org/
2. Open the MSYS2 UCRT64 shell.
3. Install the compiler:

```sh
pacman -S --needed mingw-w64-ucrt-x86_64-gcc
```

Make sure the UCRT64 `bin` directory is on your `PATH`, then build from PowerShell or the MSYS2 shell:

```sh
go build -o weazlcode.exe ./cmd/weazlcode
go build -o weazlcode-setup.exe ./cmd/weazlcode-setup
```

Run setup first if you want the guided config flow:

```sh
.\weazlcode-setup.exe
.\weazlcode.exe
```

## Keys

- `enter`: send message / select session
- `/`: start a local command such as `/help`, `/project`, `/models`, `/tools`, `/sessions`, `/workspaces`, `/new`, `/clear`, `/trim`, `/copy`, or `/mouse`
- `/plan draft <title>`: create a local draft coding plan; `/plan`, `/tasks`, and `/packet` inspect the latest plan and local-worker task packet
- `up` / `down`: recall previous prompts in the current session
- mouse wheel: scroll chat history
- `pgup` / `pgdown`: scroll chat history
- `home` / `end`: jump to top or bottom of chat history
- `ctrl+m`: toggle between copy mode and mouse scroll mode
- `ctrl+n`: start a new session
- `ctrl+r`: open workspace saves
- `ctrl+d`: delete the selected workspace save from the picker
- `ctrl+e`: rename the active or selected workspace save; from chat, this creates the save first if needed
- `ctrl+s`: save current workspace view
- `ctrl+w`: open workspace saves
- `ctrl+t`: trim context into a summary checkpoint
- `ctrl+u`: clear the active session context after confirmation
- `esc`: back to chat
- `ctrl+c`: quit

## Telemetry And Context Trimming

The status line at the bottom of the viewport gives you the vitals on your local inference. Alongside a Bubble Charm progress bar showing estimated context usage, you get token counts (`in` and `out`) and current generation speed in tokens per second (`t/s`).

The provider's `context_window` lives in `~/.config/weazlcode/config.json`; it defaults to `32768` tokens, or whatever preset you chose during install.

Running out of room? Press `ctrl+t` to have the active model summarize the current conversation into a compact checkpoint. The summary target scales with your configured context window, bounded between 500 and 6000 tokens. Future requests send that checkpoint summary plus only the new messages, saving your hardware from replaying the entire session from the top.

If you forget, WeazlCode has your back. It automatically trims context using opinionated working-context thresholds: small windows run close to the edge, while large windows compact much earlier so 128k context stays useful headroom instead of a giant prompt tax. Your current prompt stays outside the checkpoint and is sent normally right after the trim finishes.

Need a truly clean slate inside the current session? Press `ctrl+u` and confirm. That clears the active messages, tool-call history, and context checkpoints from SQLite for the current session, resets the context meter, and leaves you at a fresh prompt without guessing whether old history is still in play.

## TUI Feedback

While you wait for inference, WeazlCode uses Bubble spinner animations with rotating status phrases such as `hacking_the_gibson`, `jacking_into_the_matrix`, `wheezing_the_juice`, and `chilling_the_tokens`. The phrases favor active `-ing` wording, stay stable for short responses, and swap just a couple of times during longer generations to keep the screen quiet.

Tool calls stay neatly tucked away in the transcript as `🔧 using tools`. WeazlCode keeps the raw tool-call bookkeeping in encrypted history so the model can continue correctly, but hides empty assistant/tool scaffolding from the visible chat. When WeazlCode is summarizing older history into a checkpoint, it uses a distinct compaction animation so you know it is trimming context rather than hanging on a standard response.

Assistant responses are rendered with Glamour-powered Markdown once they land in the transcript, including when you resume a session or replay a saved workspace. Streaming text stays simple while it is still arriving, then gets cleaned up after the response is saved.

Workspace saves are meant to feel like quick snapshots, not a filing chore. Press `ctrl+s` to save or update the current workspace view, then use `ctrl+r` or `ctrl+w` to open the picker. In the picker, saves are ordered by creation time but displayed as `workspace name: timestamp` so the useful part is first. Press `ctrl+e` from chat to create/rename the active workspace, or press `ctrl+e` in the picker to rename the selected save. Press `ctrl+d` in the picker to delete a workspace save from SQLite without deleting the underlying chat session.

You can tune or disable Markdown rendering in `~/.config/weazlcode/config.json`:

```json
{
  "ui": {
    "resume_last_session": true,
    "render_markdown": true,
    "markdown_style": "dark"
  }
}
```

`markdown_style` accepts Glamour standard style names. WeazlCode defaults to `dark`; `auto` is treated as `dark` to avoid terminal color-query responses leaking into the input box in some terminals.

## Scrolling And Copy/Paste

Mouse wheel scrolling is enabled by default to make reviewing long conversations easy. Because the TUI has to capture the mouse to do this, standard terminal text selection can be intercepted.

You have two ways to grab text:

1. Toggle mode with `ctrl+m`: hit `ctrl+m` to enter copy mode. This releases mouse capture so your terminal can highlight and copy normally. Hit it again to go back to scrolling. The help line updates dynamically to show `ctrl+m copy` or `ctrl+m mouse`.
2. Use `shift` + drag: depending on your terminal emulator, holding `shift` while dragging often bypasses TUI mouse capture entirely, letting you highlight text without switching modes.

Large pasted blocks are stored as the full prompt payload under the hood, but displayed compactly in the input bar as `[PASTED n lines]` to keep your view tidy.

## Tool Support

WeazlCode is not just a static chat window. It supports function calling tools that let the AI model interact with external services and your local workspace. Tools execute automatically when allowed by their safety level.

Important: tools only work with models that understand function/tool calling. If your model does not support tool calls, normal chat still works, but WeazlCode cannot reliably ask it to run web search, weather, file, shell, SQLite, memory, or other tools. Use a tool-capable local model for the fun stuff.

### Enabling Tools

The installer can write this section for you, but to edit it manually, update `~/.config/weazlcode/config.json`:

```json
{
  "tools": {
    "enabled": true,
    "auto_execute_safe": true,
    "alpha_vantage_api_key": "YOUR_ALPHA_VANTAGE_API_KEY_HERE",
    "brave_api_key": "YOUR_BRAVE_API_KEY_HERE",
    "workspace_roots": ["/home/user/Code", "/home/user/Notes"],
    "max_output_chars": 12000,
    "max_file_bytes": 1048576
  }
}
```

Configuration options:

- `enabled`: flip to `true` to turn on tool support; default is `false`
- `auto_execute_safe`: automatically run safe tools without asking for confirmation; default is `true`
- `alpha_vantage_api_key`: API key for stock price lookups; optional
- `brave_api_key`: API key for Brave web search lookups; optional
- `workspace_roots`: restricted directories that file, shell, and SQLite tools are permitted to read from
- `max_output_chars`: maximum characters returned by tools before they are truncated
- `max_file_bytes`: maximum file size for local search/read tools

### Available Tools

#### General Utilities

- Calculator: standard math: add, subtract, multiply, divide, power, sqrt, percentage. Always available when tools are enabled.
- Current time: local machine date/time or specific IANA timezones. Always available when tools are enabled.
- Weather: current weather and short forecasts with Open-Meteo. Always available when tools are enabled, no API key required.
- Markdown checker: renders supplied Markdown with Glamour and returns a short pass/fail preview. Always available when tools are enabled.
- Stock price: current stock prices and market data. Requires Alpha Vantage API key.
- Web search: Brave Search queries returning titles, URLs, snippets, and dates. Requires Brave API key.
- Fetch URL: grabs HTTP/HTTPS URLs and returns readable text. Private and local network addresses are rejected.

#### Local Workspace

Workspace tools operate under configured `workspace_roots`; WeazlCode also adds the detected project root automatically at startup.

- Local files: `list_files`, `search_files`, `read_file`, `read_file_range`, `create_file`.
- Git inspection: `git_status`, `git_diff`, `git_log`, `git_show`, `list_changed_files`.
- Patch editing: `apply_patch` applies unified diffs after validating affected paths against workspace roots.
- Read-only command: `run_readonly_command` runs a tight allowlist of inspection commands such as `pwd`, `ls`, `find`, `rg`, `cat`, and read-only `git` subcommands. Commands are passed safely as args, never as raw shell strings.
- Verification command: `run_verification_command` runs approved test/build/lint checks for Go, Node, Python, Rust, shell, and Make projects. It requires prompt-level approval.
- SQLite query: executes read-only queries against local database files. Allowed SQL starts with `SELECT`, `WITH`, `EXPLAIN`, or `PRAGMA table_info`.
- Local memory: encrypted local memory storage with `remember`, `recall`, `list_memories`, and `forget`.

`create_file` only creates new text files under `workspace_roots`; it flat out refuses to overwrite existing files.
Tool calls are logged as JSONL under `.weazlcode/logs/tool_calls.jsonl` for project-local auditability.

### How It Works

1. You ask a question that requires a tool, and the AI model automatically calls the right function.
2. Tool payloads stay hidden from the main chat transcript.
3. The result is fed back to the model to synthesize a natural language response.
4. Calls, results, history, and memories are stored in your local SQLite vault.

### Model Requirements

- vLLM: the loaded model must support function calling, such as models fine-tuned for tool use.
- Ollama: you need a model with native tool support. Good starting points include `llama3.1`, `mistral-nemo`, and `qwen2.5`.

### Security

We take local privacy seriously, but this is still a small local app, not a hardware security module. Your vault is only as good as the password you choose. The bcrypt password check and encrypted payloads are there to keep casual prying eyes out; they are not a promise that a weak password will survive a determined offline attack against your database.

- Safe tools are strictly read-only, create-only for text files, or explicit local memory operations.
- File, shell, and SQLite tools are boxed into configured `workspace_roots`.
- `create_file` will never overwrite existing files.
- Shell commands are allowlisted and do not execute through an actual shell.
- URL fetching actively blocks private and local network IP addresses.
- Tool output is truncated before returning to the model to prevent massive context floods.
- Tool execution happens locally inside the WeazlCode process.
- API keys live in your local config file and are never shared by WeazlCode.
- Chat history, tool interactions, and memories are encrypted in your local database.

### Example Config

Check out `config.example.json` for a complete configuration template with tools enabled.

## License And Branding

WeazlCode is released under the MIT License. Use it, fork it, ship it, learn from it.

The `WeazlCode` name, screenshot, and project branding are part of this project identity. If you publish a substantially modified fork, please use a different name and visual branding so users can tell the projects apart.
