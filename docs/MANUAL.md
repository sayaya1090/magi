# magi User Manual

[English](MANUAL.md) · [한국어](MANUAL.ko.md)

An extensible terminal AI coding agent client. Provider-agnostic (OpenAI-compatible),
multi-agent, with Lua plugins, MCP, and shared memory.

---

## 1. Installation & Requirements

- **LLM backend**: an OpenAI-compatible endpoint ([Ollama] recommended). The default model is
  **`gpt-oss:120b-cloud`** — Ollama's **free cloud tier**, so no GPU is needed; just sign in once.
  ```sh
  ollama signin                 # free tier; the default gpt-oss:120b-cloud runs in the cloud
  # to run fully local:
  ollama pull qwen3-coder:30b   # strongest local coder → ./magi --model qwen3-coder:30b
  ```
- **Build**: `make build` or `CGO_ENABLED=0 go build -o magi ./cmd/magi` (pure-Go single binary)
- **Pre-built**: `curl -fsSL .../scripts/install.sh | bash` or `brew install sayaya1090/tap/magi`

## 2. Running

### Interactive TUI
```sh
./magi                 # auto-detects dark/light
./magi --theme light   # force theme (auto|dark|light)
```

### Headless (scripts/CI)
```sh
./magi -p "list the go files and summarize"
./magi -p "create hello.txt with: hi" --output json   # JSONL events
echo "explain main.go" | ./magi -p -                   # stdin
```

### Version / self-update
```sh
./magi --version
./magi --update        # self-update to the latest release (checksum-verified)
```

## 3. Configuration

Flags / environment variables (precedence: flag > env > default):

| Flag | Env | Default | Description |
|---|---|---|---|
| `--model` | `MAGI_MODEL` | `gpt-oss:120b-cloud` | model id (Ollama free cloud; `ollama signin`) |
| `--base-url` | `MAGI_BASE_URL` | `http://localhost:11434/v1` | OpenAI-compatible base URL |
| `--permission` | `MAGI_PERMISSION` | TUI=`ask` / headless=`allow` | `ask`\|`auto`\|`allow`\|`deny` |
| `--theme` | `MAGI_THEME` | `auto` | `auto`\|`dark`\|`light` |
| `--plugins` | `MAGI_PLUGINS` | (none) | additional plugin directory |
| `--no-harness` | — | (off = harness on) | disable the built-in harness (format/diagnostics/Stop hooks) |
| `--output` | — | `text` | `text`\|`json` (headless) |
| — | `MAGI_API_KEY` | (none) | key for remote backends (not needed for Ollama) |

Permission modes: `ask` = confirm every time · `auto` = **edits auto-approved, only commands (bash)/network confirmed** · `allow` = everything auto · `deny` = blocked. Cycle in the TUI with `Shift+Tab` (or `/permission`).

Config file `<config>/config.toml` (macOS `~/Library/Application Support/magi`, Linux `~/.config/magi`):
```toml
model = "gpt-oss:120b-cloud"   # default: Ollama free cloud (ollama signin). For local: "qwen3-coder:30b"
base_url = "http://localhost:11434/v1"
permission = "ask"
experience_dir = "/path/to/team/experience"   # shared brain (a git repo → shared with the team)

[routing]                  # per-agent routing (profile name or model name)
explore = "fast"           # → [llm.profiles.fast] (different endpoint/key)
planner = "fast"
coder   = "qwen3-coder:30b"  # just the model, on the default backend

[llm.profiles.fast]        # named backend (endpoint/key/model/headers, ${ENV} expansion)
base_url = "https://fast.gateway/v1"
api_key  = "${FAST_KEY}"
model    = "gpt-oss:20b"
[llm.profiles.fast.headers]
X-CLIENT-API-KEY = "${FAST_CLIENT_KEY}"

[orchestration]            # pre-flight procedure planner (on by default): decomposes a request into an
planner = true             # ordered procedure → a strategy per step (solo|parallel|scout); scout obtains
                           # a list then runs each item in parallel. For multi-step plans, before execution
                           # the council audits the plan: only a CRITICAL flaw blocks (re-plan); warn/info notes are
                           # accepted as non-blocking advice (injected for the executor). It also derives completion
                           # criteria (deliverables · test guidance) as that turn's termination contract.
                           # plan_absorb = true → fold the advice into the plan via one extra planner pass (default off)

[mcp.filesystem]           # MCP server (stdio, or HTTP via url=)
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "."]

[[hooks]]                  # lifecycle hook (see §harness below)
event = "Stop"             # just before turn ends
command = "go test ./... >/dev/null || echo 'tests failing' >&2"

[council]                  # consensus termination gate (D14): off → the model just stops when it halts (default). On → council votes done/continue
enabled    = true
rule       = "majority"    # unanimous | majority | quorum:2 | weighted:0.6 | veto:Balthasar
max_rounds = 3             # round cap (with no-progress/cancel safeguards, prevents infinite loops)
# [[council.member]]       # if omitted, the default 3 MAGI members are used
# name = "Melchior"; lens = "correctness"   # lens: correctness|verification|completeness

[theme.dark]               # color theme overrides (per mode). Unspecified roles keep NERV/MAGI defaults
primary = "#FF7A1A"        # role: primary·accent·muted·outline·error·success·
accent  = "#5CD8E6"        #       surface·primaryContainer·outlineVariant·warn
[theme.light]
primary = "#B45309"
```
> The `[routing]` / `[llm.profiles.*]` / `model` above can **also be edited via the `/route` editor** and are saved to this file.
> **Consensus council (D14, the signature feature · on by default)**: when the model is about to end a turn, instead of terminating immediately a **3-member council (Melchior · Balthasar · Casper)** looks at the task (goal) · agent report · diff and votes done/continue. (The diff here **includes the contents of newly created untracked files**, so freshly created files are shown as evidence.) A member votes continue only when its lens identifies a **concrete defect**, done when satisfied, and **abstains** when there's no evidence to judge by (it does not reflexively continue just because it's uncertain). A **pure conversational turn that used no tools (greetings · questions) does not convene the council**. On continue, the merged feedback is injected and the loop carries on = "the termination decision is taken away from any single model." `rule` sets the consensus method, `max_rounds` the round cap (with no-progress/cancel safeguards). With `criteria=true`, **completion criteria are derived once per turn** from the task (one extra LLM call) and used as the council's contract, making the verdict sharper. It is **on by default** and adds an LLM round at each termination point, so to turn it off set `[council] enabled=false`. It is inactive in workflow mode (the pipeline uses its own verify gate). Give each `[[council.member]]` a `provider` (= an `[llm.profiles.*]` backend) and `model` so **each member can deliberate on a different model (a mix of cheap + strong)** (defaults to the session model / default backend if unspecified). In the TUI, deliberation is shown live as a **header chip** (`⚖ council rN: <member>`) and **transcript lines** (convene / per-member **compact one-liner** (member-colored `●` + name + verdict icon ✓done · ↻continue · ∅abstain) / verdict · tally). **Clicking a member line opens a detail modal** (lens · rationale · feedback · confidence) so you can see why each member voted as it did (close with esc/click). Per-member colors can be changed via the theme (`[theme.dark] melchior/balthasar/casper`). Giving `verify = "<command>"` (shorthand) or `[[council.signal]]` (name/command, e.g. test · lint · typecheck — **multiple allowed**) makes magi (opt-in) run those commands each round and feed **the results as council evidence** (judging on evidence, not vibes = blocks false success).
> Color themes can be defined externally per role in `[theme.dark]` / `[theme.light]` (default = NERV/MAGI). Pick the mode (auto/dark/light) with `--theme`.
> On first run a commented default `config.toml` is generated automatically (left untouched if it already exists).

### Harness (on by default)

Even with no configuration, an "understand → plan → implement → verify → summarize" procedure naturally applies. It has two layers:

1. **Operating guidance prompt (always)** — for multi-step work, plan with `todowrite` then do items one at a time, verify with build/test after edits, don't end in a broken state, and summarize the changes at the end. Applies just by chatting, even if you don't know how to use it.
2. **Built-in hooks (always, off via `--no-harness`)** — right after a file edit, run auto-format (gofmt) + language diagnostics (gofmt -e / go vet / py_compile) and feed errors back to the model → self-correction.

**Team sharing**: commit `.magi/config.toml` at the project root and the workflow travels with the repo. It is merged with the global (`<config>/config.toml`), and `[[hooks]]` accumulate.

Hook events:
| event | when | effect of exit code ≠ 0 |
|---|---|---|
| `PreToolUse` | before a tool runs | **block** the tool (stderr passed to the model; e.g. path protection) |
| `PostToolUse` | after a file edit | output passed as feedback |
| `Stop` | just before turn ends | block termination and force continued work (e.g. require tests to pass) |

Hook commands run in a shell and receive the `MAGI_TOOL`/`MAGI_PATH` environment variables + JSON stdin. Filter by tool name with `match` (`"*"` = all).

## 4. Using the TUI

### Slash commands (typing `/` opens an autocomplete palette — prefix filter, ↑/↓ to select, Tab to complete)
| Command | Description |
|---|---|
| `/help` | help |
| `/route` (=`/model`=`/agents`) | **model & routing editor** (one screen): **(session)** default model, per-agent model/backend, **add/edit backends (profiles)**. ↑/↓ select · Enter edit/open · empty value = reset to default · Esc close. While editing an agent, **pick a profile with ←/→** (or type a model name). Use `+ add profile` to define a profile (endpoint/key/model/headers); in the form, Enter edits a field · **Tab saves**. **All edits are persisted to `config.toml`** (comments preserved) |
| `/tools` | available tools |
| `/sessions` | session list for this directory |
| `/resume [n]` | resume a session (no arg = list, `/resume 2` to switch) |
| `/rewind [n]` | roll back the last n user turns (default 1) |
| `/image <path>` | show an image inline |
| `/diff` | working-tree git diff |
| `/loop` | **Loop map** — projects turns · steps · tool activity · council rounds as structure (visualizes the *shape* of the loop) |
| `/context` | **context window** visualization — usage / window size · message count · compaction history (tokens before→after) |
| `/fork` | **branch** the current session to explore an alternative attempt (original preserved). Switches to the branch |
| `/replay` | **re-run the previous turn on the branch** (reproduce the same input). Compare with `/loopdiff` |
| `/loopdiff` | **structurally compare the current branch against the fork origin** (turns · steps · tools · council · tokens) |
| `/init` | analyze the project then write AGENTS.md |
| `/permission` | cycle permission mode (ask→auto→allow→deny) |
| `/compact` | summarize/shrink the context |
| `/clear` | clear the screen |
| `/quit` (=`/exit`) | exit |

### Keyboard shortcuts
| Key | Action |
|---|---|
| Enter | send (**if a turn is running, inject the message into the in-progress turn = steering**) |
| ↑ / ↓ | input history (recall previous prompts — includes prior turns when resuming a session) |
| Tab | autocomplete the input prefix from history (shared with slash / subagent focus) |
| PgUp/PgDn · Ctrl+U/Ctrl+D · Shift+↑/↓ | scroll (page / half-page / one line) |
| Tab | cycle subagent panel focus (when panels are present) |
| Ctrl+O | zoom in/out on the focused subagent panel |
| Esc | release zoom → release focus → interrupt the running work |
| mouse wheel | scroll the transcript (even while dragging) |
| mouse drag | select text → copy to clipboard on release |
| mouse click | focus a subagent panel |
| Ctrl+L | clear the screen |
| Shift+Tab | switch permission mode |
| Ctrl+C | exit |
| mouse wheel | scroll · click a panel → focus (click again → zoom) |
| permission modal: y/a/n | allow/always/deny |

Typing keys go only to the input box and scrolling happens only via the dedicated keys above — so typing body text (including spaces) doesn't scroll the screen. While you're scrolled up reading, streaming doesn't yank the view down (auto-follow only when at the bottom).

### Mouse / text copy (no modes)
Wheel scroll · drag select · click focus all work **without any mode switch** — because the app handles selection/copy itself. **Dragging highlights that range (character/cell granularity, partial-line selection allowed), and releasing copies it to the clipboard** (tries both the OS clipboard `pbcopy`/`wl-copy`/`xclip` and OSC52). Wheel scrolling during a drag works too (the selection is pinned to content position, so it persists across scrolling).

### Transcript rendering
- **edit/write appear as colored diffs** — syntax highlighting (language detected by file extension) + a line-number gutter, additions/deletions as `+`/`-`.
- **bash · read · grep · glob · list · webfetch · websearch results appear as collapsed blocks** inline (read shows line numbers + syntax highlighting). Long ones get a `… +N more` footer and **clicking the block expands it**.
- **Council rejection reasons** are shown wrapped below the one-line verdict (the full reason is in the detail modal via member click).

### Status panel (post-it)
When there are plans (todos) · subagents · context, a **rounded-outline box (post-it) appears at the top-right** (hidden if none). The transcript uses the full width and the box is drawn overlaid on top of it (bottom-aligned, so it usually floats over empty space); **dragging the box's left edge adjusts its width**. Click a subagent line to zoom into that agent.

### Theme
At startup it detects dark/light from the terminal background color, and **if you change the OS theme while running it follows within a few seconds** (it re-queries the background color periodically). Force `auto`/`dark`/`light` with `--theme` (or `MAGI_THEME`).

### Steering (interrupting mid-work)
Input stays alive even while work (a turn) is running — you can keep typing (including inline Korean IME composition), and pressing Enter **injects the message immediately into the in-progress turn**. The main agent sees and reflects that message **at the next step** — it isn't queued to surface only after the turn fully ends; you're steering the running agent in place. The message appears in the transcript right away.
The main agent **runs subagents delegated via `task` in the background (as sidecars) and returns immediately**, so it isn't blocked — it reacts to steer messages right away.
Slash commands during work: read-only/UI-only ones (`/help` · `/route` (=`/model`=`/agents`) · `/tools` · `/sessions` · `/diff` · `/permission`) execute, while ones that change the session (`/resume` · `/rewind` · `/clear` · `/init` · `/ultra` · `/compact`) are rejected during work.

### Multi-agent live view (split-pane)
While subagents are alive, **a live panel for each subagent** is tiled below the main transcript (subscribing to each child session in real time). Each subagent is assigned a **unique color** (M3 tonal palette) applied consistently to its panel border · header badge · the `⚙ task → <name>` highlight in the transcript.
- Move focus with `Tab` (or by clicking a panel) → the focused panel gets a **focus ring** in its color.
- `Ctrl+O` (or clicking the focused panel again) **zooms in** → observe that subagent's full transcript in detail. On entering zoom it jumps to the bottom (latest/conclusion). **Clicking** the top **breadcrumb `‹ back  ✦ magi › coder`** (or `Esc`) returns. In the detail view, assistant lines are shown under **the agent's name (in color)** rather than `magi`.
- **Color is the identifier**: if the main agent gives several jobs to the same kind of agent, **each job (session) gets its own panel**. The same role shares the **same hue** (matching the transcript's `task → coder` highlight); the **2nd and 3rd are distinguished by brightness**.
- **Panels are managed by role**: even if the same role (e.g. coder) is restarted/re-delegated, **the existing window is reused rather than a new one created**.
- **Lifecycle**: large tiles while working, **collapsing to a one-line compact when the turn ends** (still openable with `Ctrl+O`). They disappear when you send the next message, and are later restored via `/resume`.

### Right-side status panel
When there are plans / progress, a **status panel** appears on the right (hidden if none). **Drag its left edge with the mouse** to adjust the width (default 44 columns). Sections:
- **Plan** — the current todo checklist + progress (`done/total`): completed ✓, in-progress ◐, pending ☐, and **cancelled ✗** (a step left unfinished when the turn is aborted/stopped). Progress is driven by deterministic signals (the planner checks off steps it runs, and the turn resolves the rest on finish), so it updates even when the model doesn't call `todowrite`. Updates in real time.
- **Subagents** — list of active subagents (color · status · totals `elapsed · ↑↓ tokens`). **Click an item → zoom into that subagent's detail** (same as clicking the pane).
- **Context** — context token usage bar.
It also appears with the same layout in the subagent zoom (detail) view, where Plan shows **that subagent's todos**.

### Paste folding
Multi-line (or over-200-char) pastes are folded into a `[#N pasted L lines]` chip **only in the input box** (since the input box is narrow). On send, the **full content is shown as-is in the transcript (main)** and the full content is passed to the agent too. (↑ history recall brings it back as a chip so it doesn't clutter the input box.)

### `@` file mentions
Put `@path/file` in a message and that file's contents are attached to the agent context.

### Header display
`model <id> · ctx <%>` + **permission chip (color-coded)** + a `⛐ N: explore, coder×2` badge (name · color) while subagents are running.
- Permission chip colors: `ask` = amber (safe) · `auto` = cyan (edits auto) · `allow` = yellow (caution) · `deny` = red (blocked).

### Session resume (`/resume`)
`/resume` (no arg) → **interactive picker**: shows each session's time + first-message summary, select with `↑/↓`, resume with `Enter`, cancel with `Esc`. You can also switch directly with `/resume N`.
- The list shows **only user sessions** (child sessions created by subagents are hidden).
- Resuming a parent session **restores that session's subagents as completed panels**, so you can inspect them again with `Tab`/`Ctrl+O` (live spawn events are ephemeral, so they're restored from the child sessions on disk).

### TUI behavior checklist (screenshots)
A collection of interaction behaviors under verification — organized so screenshots can be attached under each item.

1. **Input during work + Korean IME** — you can type/compose inline in the input box even while a turn is running (cursor at the input position).
2. **Steering (interrupting mid-work)** — a message sent with Enter during work is injected immediately into the in-progress turn, and the main agent reflects it at the next step. The main reacts right away even while subagents are running.
3. **Multi-agent split-pane** — a live panel per subagent, tiled, with **a unique color per agent**. `⚙ task → <name>` is in the same color.
4. **Focus / zoom** — move focus with `Tab` (or by clicking a panel when the mouse is ON) (color focus ring), full-screen detail with `Ctrl+O` (breadcrumb · divider · left bar in that color), return with `Esc`.
5. **Per-subagent interrupt** — focus a running subagent then `Esc` → interrupt only that subagent (the rest · the main continue).
6. **Supervisor (sidecar)** — auto-restart on no-response/stall (`restarting…` in the header), inject an ERROR result on timeout/exhausted restarts. The system doesn't halt even if one dies.
7. **Paste folding** — multi-line paste → a `[#N pasted L lines]` chip in the input box, full content in the main on send.
8. **Scroll position retained** — if at the bottom, stays at the bottom after streaming / panel add-remove / **terminal resize**; if scrolled up reading, it isn't dragged along.
9. **Input history** — recall previous prompts with `↑/↓` (includes prior turns when resuming a session), autocomplete the prefix with `Tab`.
10. **Mouse/copy (no modes)** — wheel scroll · drag select+copy · click focus all work without a mode switch (the app handles selection itself). Wheel scrolling during a drag works too.
11. **Screen cleanup on startup** — clears the terminal once on launch and starts from a clean screen.

## 5. Tools (built-in)

| Tool | Description | Permission |
|---|---|---|
| `read` | read a file (line numbers, offset/limit) | — |
| `write` | create/overwrite a file | ask |
| `edit` | exact string replacement (unique match) | ask |
| `multiedit` | apply multiple hunks atomically | ask |
| `grep` | regex content search | — |
| `glob` | glob (** supported) | — |
| `list` | directory listing | — |
| `findcontext` | locate relevant code (context gathering) | — |
| `astgrep` | structural (AST) code search | — |
| `bash` | shell execution (timeout · exit code, `background` for long-running commands) | ask |
| `bash_output` | fetch new output from a background command | — |
| `bash_kill` | terminate a background command | — |
| `lsp_diagnostics` | gopls diagnostics (types/unused etc.) | — |
| `lsp_definition` | symbol definition location (Go = gopls, else LSP) | — |
| `lsp_references` | all references to a symbol (semantic) | — |
| `lsp_symbols` | file symbol outline | — |
| `webfetch` | URL → text | ask |
| `websearch` | web search (DuckDuckGo, or Brave/Tavily key) | ask |
| `todowrite` | record a plan (checklist) | — |
| `skill` | load a named skill's body | — |
| `remember` | contribute a lesson to shared memory | — |
| `task` | delegate to subagents (single/parallel) | — |

- All file tools **deny access outside the working directory** (jail).
- Read-only tools run **in parallel** within a turn.
- After a file modification, **diagnostic feedback** (Go: gofmt/go vet, Python: py_compile) → the agent self-corrects.

## 6. Multi-agent

Delegate to subagents with the `task` tool. Default agents:
- **explore** — read-only code exploration
- **reviewer** — code review (read-only)
- **coder** — implementation (read/write/edit/multiedit/grep/glob/list/bash)

Limits (D7): depth 3 · concurrency 8 · cumulative 50. Assign per-agent models with `[routing]`.

### Sidecar execution model (main = the UI thread)
When the top-level orchestrator delegates with `task`, the subagent runs as a **background sidecar** and `task` returns immediately. The main agent isn't blocked (= idle like a UI thread) and **reacts to user input right away**, while each subagent result is injected into the main session **as it finishes** and processed **incrementally**. Injected results come with **the number of subagents still running**, so the orchestrator decides for itself whether to wait for the rest (rather than re-delegating) · to **delegate a new follow-up** based on results · or to synthesize. It delegates heavy work and handles light work inline.

Each sidecar has a **supervisor** doing health checks:
- **Hard timeout** (`SubagentTimeout`, default 5 min/attempt) — abort on no response.
- **Stall detection** (`SubagentStall`, default 4 min of inactivity) — generous so it catches only true hangs (avoids false restarts from first-token latency on large prompts).
- **Auto-restart** (`SubagentMaxRestarts`, default 2) — retry on stall/timeout/transient error (reusing the same role panel), inject an ERROR result when exhausted. The rest · the main continue even if one dies.

(Nested subagents work via synchronous delegation — background is top-level only.)

### Escalation (subagent → orchestrator `ask`)
If a subagent gets stuck during execution, it **asks the orchestrator via the `ask` tool and gets an answer on the spot** (blocks then resumes). The orchestrator holds the **full context** (the user's original request · the whole plan · other subagents' results), so it can: clarify/decide intent, provide a path/constraint, **relay a follow-up question to the user**, and **coordinate with other subagents** (peer questions also go through the orchestrator — so context stays in one place). The `ask` tool's description spells out "what the orchestrator can do for you" so the subagent knows what to request. Only one ask is handled at a time (serialized), with a 2-minute timeout.

## 7. Memory & Context

- **AGENTS.md**: the contents of the working directory's (+ `.magi/AGENTS.md`, global `<config>/AGENTS.md`)
  are injected into the system prompt and **preserved even through compaction**. Auto-generate with `/init`.
- **Auto-compaction**: when real token count exceeds 80% of the model window, older turns are summarized (recent ones preserved).
- **Shared brain (D13)**: the `memories/` · `skills/` in `<config>/experience` (or `experience_dir`) are
  recalled and injected at session start. The `remember` tool contributes to `pending/` (moved to `memories/` after review).
  Make the directory a git repo and commit/pull to share with the team.
  Step-by-step bootstrap / file format / review procedure: [`EXTENDING.md`](EXTENDING.md) §2.

## 8. Skills

`<config>/skills/*.md` or `<workdir>/.magi/skills/*.md` (first line = description, rest = body).
The list is exposed in the system prompt, and the model loads a body with the `skill` tool to follow it.

## 9. Plugins (Lua)

`plugin.toml` + `init.lua` in `<config>/plugins/<name>/` or `<workdir>/.magi/plugins/<name>/`.
Capabilities: tool, etc. **Hot-reload** on file change.
Sandboxed (dangerous stdlib blocked) + manifest permissions (`fs:read`, `net`, `exec`) enforced.
Example: `plugins/examples/wordcount`.

```toml
# plugin.toml
name = "wordcount"
capabilities = ["tool"]
permissions = ["fs:read:."]
```

## 10. MCP

Declare `[mcp.<name>]` in `config.toml` and it is spawned over stdio JSON-RPC, with its tools
auto-registered. When the server shuts down, those tools are removed.

> Step-by-step add/verify/troubleshoot: [`EXTENDING.md`](EXTENDING.md) §1.

## 11. Model Recommendations

- **gpt-oss:120b-cloud** — **the default**. Ollama free cloud tier (`ollama signin`), no GPU needed. Strong general-purpose + coding.
  The free tier is "light usage" (1 concurrent · a GPU-time quota), so the heavier `qwen3-coder:480b-cloud` eats the quota fast.
- **qwen3-coder:30b** — the strongest **local** coder (24 GB GPU). Run fully local with `--model qwen3-coder:30b`.
- **gpt-oss:20b** — a lighter local alternative (shows reasoning).
- Small models (llama3.1:8b etc.) tend to leak function calls when tools are active → not recommended.
- It parses all tool-call variants of local/cloud models (JSON/XML/native).

## 12. Unsupported / Future

OS sandbox, a live LSP server (gopls), automatic context ranking, web search (a search API key),
prompt caching (hosted only), and a web UI / remote sharing are not implemented (detailed table in [FEATURES.md](FEATURES.md)).

**Loop-engineering track (signature, planned — D14~D16)**: a **consensus council** that takes the termination decision away from any single model (Melchior · Balthasar · Casper), loop macro stages (Plan→Execute→Verify→Report→Council→Finalize), a live deliberation panel · Loop map, and rewind/fork/session diff. See [PLAN.md §4.2](PLAN.md) for the design. Not yet implemented.

[Ollama]: https://ollama.com
</content>
</invoke>
