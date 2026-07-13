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

Headless output contract (stable — scripts, CI, and the bench adapters key off it):

- **Exit codes**: `0` = the turn finished · `1` = the turn ended on an agent-level
  error (`loop_guard`, `stall_guard`, `max_steps`, provider failure) · `2` = magi
  itself could not run the prompt (setup/submit failure).
- **stdout** = the transcript: the model's text (the final answer), tool call/result
  lines, council/compaction notes. With `--output json`, one fact event per line
  (JSONL), decodable as `event.Event`.
- **stderr** = errors only. Agent-level errors use the greppable form
  `error[<code>]: <message>`.

**Headless permission denials are honest, not a fake user decision.** Under `--permission auto`/`ask` in headless mode there is no one to answer a prompt, so `bash`/`webfetch` are unavailable. The tool result the agent receives says so **categorically** — "not available this run (this mode can't approve without a prompt), don't retry; proceed without it or report why you couldn't" — rather than the misleading `denied by user` an interactive deny would send. The distinction matters: `denied by user` reads as "the human said no to *this* call", so the agent retries variations and thrashes; the categorical message tells it the capability is simply off for the whole run, so it adapts once. Use `--permission allow` (or run interactively) if the task needs those tools. A one-line stderr note also flags the mismatch at startup.

### Environment check
```sh
./magi --doctor
```
`--doctor` runs a one-shot diagnostic of everything magi needs and exits — use it
first when a fresh machine misbehaves. It checks, and prints an `ok` / `warn` /
`fail` line for each:

- **LLM endpoint** — reachability of `--base-url` and whether the configured
  `--model` is present on it (for Ollama, whether you are signed in for cloud models).
- **Optional external tools** — `gopls`, `ast-grep`, `rg` (ripgrep): present ones
  sharpen search/refactor; missing ones only degrade gracefully (warn, never fail).
- **Sandbox backend** — which command isolation is available (e.g. `sandbox-exec`
  on macOS, `bwrap` on Linux) and whether bash will run confined.
- **Config** — that any **council member `provider`** names a defined
  `[llm.profiles.*]`; an undefined one is a warn (it falls back to the default backend).

Exit code is `0` unless a **hard failure** (e.g. the LLM endpoint is unreachable),
which exits non-zero; warnings are advisory and do not change the exit code.

### Version / self-update
```sh
./magi --version
./magi --update            # update the binary AND managed plugins, then exit
./magi --update-core       # update only the binary (checksum-verified)
./magi --update-plugins    # update only managed (git) plugins
./magi --plugin-install <git-url> [--plugin-pin <ref>]   # clone a plugin into the user plugins dir
```

**Managed plugins** = plugins that are git checkouts (installed via `--plugin-install`
or cloned by hand). `--update-plugins` runs a fast-forward pull on each; a plugin with
local commits/changes (not fast-forwardable) or with no git remote is reported and
**skipped, never force-overwritten**. Hand-dropped, non-git plugins are left untouched.
Plugins hot-reload, so no restart is needed after `--update-plugins`.

**Interactive startup check.** When you launch the TUI on a terminal, magi checks for a
newer release at most once every 24h. A **patch** release just prints a one-line banner
(`… run magi --update`); a **minor or major** release is treated as required and installs
automatically after a short cancellable pause, then asks you to restart. This check fires
**only** on an interactive TTY — never under `-p`, a pipe, CI, or a benchmark — so
non-interactive runs make no network call and get no surprise install. Disable it with
`--no-update-check` (or `MAGI_NO_UPDATE_CHECK=1`).

## 3. Configuration

Flags / environment variables (precedence: flag > env > default):

| Flag | Env | Default | Description |
|---|---|---|---|
| `--model` | `MAGI_MODEL` | `gpt-oss:120b-cloud` | model id (Ollama free cloud; `ollama signin`) |
| `--base-url` | `MAGI_BASE_URL` | `http://localhost:11434/v1` | OpenAI-compatible base URL |
| `--permission` | `MAGI_PERMISSION` | TUI=`ask` / headless=`allow` | `ask`\|`auto`\|`allow`\|`deny` |
| `--profile` | `MAGI_PROFILE` | (none) | guardrail preset `safe`\|`standard`\|`yolo` — sets permission + sandbox together (below) |
| `--theme` | `MAGI_THEME` | `auto` | `auto`\|`dark`\|`light` |
| `--plugins` | `MAGI_PLUGINS` | (none) | additional plugin directory |
| `--no-harness` | — | (off = harness on) | disable the built-in harness (format/diagnostics/Stop hooks) |
| `--output` | — | `text` | `text`\|`json` (headless) |
| `--time-budget` | `MAGI_TIME_BUDGET` | `0` (off) | soft wall-clock budget shown to the agent as guidance (e.g. `20m`); **advisory, never a hard stop**. See §Time & step budget. Kept **off** for leaderboard/comparison runs |
| `--workflow` | `MAGI_WORKFLOW` | (off) | drive the task through the deterministic localize→implement→verify→review pipeline |
| `--verify-cmd` | `MAGI_VERIFY_CMD` | (auto) | workflow verification command; auto-detected (go/cargo/npm/pytest markers) when empty |
| `--http-timeout` | `MAGI_HTTP_TIMEOUT` | `0` (unbounded) | max wait for LLM response headers (e.g. `120s`) |
| `--no-cache` | `MAGI_NO_CACHE` | (off) | disable prompt `cache_control` (on by default; auto-falls back if the backend rejects it) |
| `--list-models` | — | — | list the backend's available models and exit |
| `--doctor` | — | — | environment diagnostics and exit (see §Environment check) |
| `--version` | — | — | print version and exit |
| `--update` / `--update-core` / `--update-plugins` | — | — | update binary+plugins / binary only / managed plugins only, then exit |
| `--plugin-install` / `--plugin-pin` | — | — | git URL of a plugin to clone into the user plugins dir / optional tag/branch/commit for it |
| `--no-update-check` | `MAGI_NO_UPDATE_CHECK` | (off) | disable the interactive startup update check |
| — | `MAGI_API_KEY` | (none) | key for remote backends (not needed for Ollama) |
| — | `MAGI_REASONING_EFFORT` | (backend default) | passed to the backend as `reasoning_effort` for reasoning models — e.g. `none` to disable thinking, or `low`\|`medium`\|`high`; empty = omit the field |
| — | `MAGI_EMOJI_WIDTH` | (auto-probe) | force emoji cell width: `narrow`\|`1` (one cell) or `wide`\|`2` (two cells). If unset, a startup probe measures it |
| — | `MAGI_WIDTH_PROBE` | (on) | `0` skips the startup terminal-width probes (ambiguous · decor · emoji) = no correction (library default widths) |
| — | `MAGI_AMBIGUOUS_WIDTH` | `auto` | `wide`\|`narrow`\|`auto` — force East-Asian ambiguous-char cell width (see below) |

Permission modes: `ask` = confirm every time · `auto` = **edits auto-approved, only commands (bash)/network confirmed** · `allow` = everything auto · `deny` = blocked. Cycle in the TUI with `Shift+Tab` (or `/permission`).

Guardrail posture (`--profile`/`MAGI_PROFILE`) is a preset that sets both axes (**approval** × **OS sandbox**) at once: `safe` = `ask` + `read-only`, `standard` (recommended) = `auto` + `workspace-write` (auto-approve edits, confirm commands/network, confine writes to the workspace), `yolo` = `allow` + `full`. An explicit `--permission`/`sandbox` overrides the preset. With no profile set, the sandbox stays opt-in (unconfined) and only the permission default applies — so an existing user's network / out-of-tree writes aren't silently cut. The sandbox axis (`sandbox = "read-only"|"workspace-write"|"full"`) can also be set directly in `config.toml`.

**Fine-grained rules (`config.toml`).** Beyond the mode, three list keys narrow the policy: `allow` / `deny` are glob rules over tool invocations (e.g. `Bash(git push:*)` auto-approves that command, `Read(**/.env)` blocks reading secrets) — this is what the `p` permission choice (§4) persists — and `deny` wins over `allow`. `allow_domains` restricts **WebFetch/bash network egress to a host allowlist** (e.g. `["api.github.com"]`); empty = no host restriction. All three **append** across the global and project configs rather than overriding, and are on the fixed deny-list a plugin's `set_config_key` can never touch (EXTENDING §Plugins).

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
planner = true             # ordered procedure → a strategy per step (solo|parallel|scout|delegate|refine); scout
                           # obtains a list then runs each item in parallel; delegate hands off an independent write
                           # sub-task; refine recurses on a dependent sub-goal in-context. For multi-step plans, before execution
                           # the council audits the plan: only a CRITICAL flaw blocks (re-plan); warn/info notes are
                           # accepted as non-blocking advice (injected for the executor). It also derives completion
                           # criteria (deliverables · test guidance) as that turn's termination contract.
                           # plan_absorb = true → fold the advice into the plan via one extra planner pass (default off)
subagent_timeout = "5m"    # base per-attempt subagent hard cap; the effective cap flexes with observed
                           # model speed (base/2–base×3). Env MAGI_SUBAGENT_TIMEOUT overrides; /subagent adjusts live.

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
preset     = "full"        # "light" = 1 verification member · 1 round (interactive latency; explicit member/max_rounds override)
# [[council.member]]       # if omitted, the default 3 MAGI members are used
# name = "Melchior"; lens = "correctness"   # lens: correctness|verification|completeness

[theme.dark]               # color theme overrides (per mode). Unspecified roles keep NERV/MAGI defaults
primary = "#FF7A1A"        # role: primary·accent·muted·outline·error·success·
accent  = "#5CD8E6"        #       surface·primaryContainer·outlineVariant·warn
[theme.light]
primary = "#B45309"
```
> The `[routing]` / `[llm.profiles.*]` / `model` above can **also be edited via the `/route` editor** and are saved to this file.
> **Consensus council (D14, the signature feature · on by default)**: when the model is about to end a turn, instead of terminating immediately a **3-member council (Melchior · Balthasar · Casper)** looks at the task (goal) · agent report · tool results · **the agent's own edits this turn** and votes done/continue. (The edits are reconstructed from the agent's write/edit tools — a per-file before→after diff, git-independent and correctly attributed, so a human/external/bash change is never credited to the agent; large changes collapse to a one-line summary. The detail modal renders them as `◆ path` headers with colored +/- lines.) A member votes continue only when its lens identifies a **concrete defect**, done when satisfied, and **abstains** when there's no evidence to judge by (it does not reflexively continue just because it's uncertain). A **pure conversational turn that used no tools (greetings · questions) does not convene the council**. On continue, the merged feedback is injected and the loop carries on = "the termination decision is taken away from any single model." Every rejection inject ends with a **keep-work directive** — what is missing is *evidence*, not a rebuild: do not delete or recreate existing work; take the smallest action that produces the requested evidence against the current state (this blocks the start-over reflex that destroys a finished deliverable after a rejection). `rule` sets the consensus method, `max_rounds` the round cap (with no-progress/cancel safeguards); a rejection on the FINAL allowed round lands the turn immediately as a forced **UNVERIFIED** finish rather than injecting one more feedback phase — files and running processes are left standing for verification instead of burning the remaining wall clock. With `criteria=true`, **completion criteria are derived once per turn** from the task (one extra LLM call) and used as the council's contract, making the verdict sharper. It is **on by default** and adds an LLM round at each termination point, so to turn it off set `[council] enabled=false`. It is inactive in workflow mode (the pipeline uses its own verify gate). Give each `[[council.member]]` a `provider` (= an `[llm.profiles.*]` backend) and `model` so **each member can deliberate on a different model (a mix of cheap + strong)** (defaults to the session model / default backend if unspecified). In the TUI, deliberation is shown live as a **header chip** (`⚖ council rN: <member>`) and **transcript lines** (convene / per-member **compact one-liner** (member-colored `●` + name + verdict icon — termination: ✓done · ✗reject · ∅abstain; plan audit tiers by severity: ✓approve · ✎advise(warn) · ·note(info) · ↻revise(critical,blocking) · ∅abstain) / verdict · tally). **Clicking a member line opens a detail modal** (lens · rationale · feedback · confidence) so you can see why each member voted as it did (close with esc/click). Per-member colors can be changed via the theme (`[theme.dark] melchior/balthasar/casper`). Giving `verify = "<command>"` (shorthand) or `[[council.signal]]` (name/command, e.g. test · lint · typecheck — **multiple allowed**) makes magi (opt-in) run those commands each round and feed **the results as council evidence** (judging on evidence, not vibes = blocks false success).
> Color themes can be defined externally per role in `[theme.dark]` / `[theme.light]` (default = NERV/MAGI). Pick the mode (auto/dark/light) with `--theme`.
> On first run a commented default `config.toml` is generated automatically (left untouched if it already exists).
> **Malformed config is never silently ignored**: if the global `config.toml` fails to parse (e.g. a duplicate top-level key), magi prints the parse error and refuses to start rather than falling back to an empty config that would drop your model, plugins, and every other setting. A malformed **project** `.magi/config.toml` warns and is skipped (the valid global config still applies). An unknown *key* (a typo) is only a warning — it never blocks startup.

> **Loop & execution safety guards (deterministic, always on)**: beyond the council, a few cheap deterministic guards catch failure modes a weak model walks into on its own. **Self-regression check** — magi tracks each file's content states within a turn; if an edit returns a file to a state it already had this turn (the silent "fix it, then quietly undo the fix" trap), a neutral note is appended to that tool result so the agent re-checks (advisory, never blocks; once per file). **Non-interactive command execution** — every `bash` command runs with no controlling terminal and stdin closed, so a command that tries to prompt (git credentials, `ssh` host-key confirmation, `apt`, a pager) **fails fast instead of hanging** until the timeout. **Loop guard** — an identical no-progress tool call repeated past a small limit is refused (the earlier result is echoed back); when the agent keeps thrashing it first gets **one corrective re-grounding** (re-read the original task, change approach) — and the nudge carries a **completion escape hatch**: if the work is genuinely complete, end the turn by replying with **no tool call at all** (that is what triggers verification; another confirmation command is not progress), and **never delete or rebuild finished work just to produce visible activity**. Only if it persists is the run stopped gracefully rather than burning the whole step budget. **Stall force-stop** — when the agent makes no progress across several steps even after the corrective nudge, the run is stopped gracefully (`stall_guard`) instead of grinding to the ceiling; a `bash` write counts as progress so a legitimately slow build isn't misread as a stall. **Empty-finish nudge** — a turn that ends with **no answer text** delivered nothing the user can read. This happens when a harmony-format weak model emits only its analysis channel and stops (a "reasoning-only stop"), or when it runs tools and then goes silent on the final step — in which case the council gate, seeing the tool work, could otherwise vote it "done" and finish with no deliverable and no UNVERIFIED flag. The turn is nudged **once** to actually write the result (the same nudge subagents get to call `report`), regardless of whether tools were used; a still-empty retry then finishes normally, so it can't loop.

> **Concern ledger & checkpoint-first planning (multi-step work).** A structural concern the council raises early — "the auth change has no test", "this migration is irreversible" — must not be forgotten three turns later when the model is deep in unrelated edits. magi keeps a **durable ledger** of such concerns that persists across turns and is re-surfaced to the planner; a **checkpoint-first gate** makes the plan address open concerns before it declares completion, rather than letting a turn end "done" with a known unresolved risk on the table. Because only the orchestrator holds the full context (original request · whole plan · every subagent's result), **only the orchestrator can retire a concern**, via the `resolveconcern` tool, and only after it is genuinely resolved — the executor can't quietly wave its own risk away.

> **Anti-fabrication under pressure.** The failure mode these guards target is a weak model, cornered by a shrinking step/time budget or a dead end, **inventing** a plausible result (a file's contents, a test outcome, a computed total) rather than admitting it's stuck. The system prompt explicitly forbids fabricating results or evidence under stuck/budget pressure and tells the model to report the honest partial state instead; the deterministic **data tools** (§5) give it a real way to compute tallies so it never has to guess; and the council judges on **fresh execution evidence and signal-command results** (below), not on the model's assertion that it finished — so a confident but unbacked "done" is caught, while an honest "I could not do X because Y" is accepted. An honest failure is a correct outcome; a fabricated success is the bug.

### Time & step budget

Every step, magi appends an ephemeral **budget line** to the agent's context (never to the cached system prompt, so the prefix stays cache-stable) so the model paces itself:

- **Step budget** — `You are on step N of at most M`. The ceiling `M` (default **240**, `MaxSteps`) is framed as a **hard ceiling, not a quota**: finishing early is better, and spending a step on a command that only *narrates* (an echo of "done", a status summary) is explicitly forbidden — say it in reply text instead. Tiny phase budgets (e.g. an internal summarize=3) skip the line entirely.
- **Soft planner estimate** — when the planner ran, it emits an `estimated_steps` count shown as *"estimated at roughly K steps — a pacing reference, not a limit"*. It is deliberately **advisory only**: a weak model's estimate is routinely wrong, and a wrong hard cap would cut off real work, so the 240 ceiling stays the only stop. If the agent runs far past the estimate, the line tells it to stop and reassess.
- **Self-measured elapsed** — once a turn crosses a minute, the line adds *"You have been working for 11m of wall-clock time so far"*. This is magi's **own stopwatch** (measured from the turn start), not external scorer information, so it is fair to use everywhere and lets the model notice a slow grind (ten compile retries) and change approach.
- **`--time-budget` (off by default)** — a user-stated soft deadline (e.g. `--time-budget 20m`). When set, the line states the budget and the remaining time, flipping to *"budget EXCEEDED — land the smallest honest result immediately"* once elapsed passes it. It is **guidance, never a hard stop**, and is kept **off for leaderboard/comparison runs** so results stay apples-to-apples.

**Council cost cap** — the council also watches its own wall clock. Once deliberation has consumed a disproportionate share of the turn (at least 60s **and** at least a quarter of the turn's elapsed time), further rounds cost more than they return, so round 2+ is skipped and the turn finishes marked **UNVERIFIED** rather than paying for another 3-member round. The first round always runs.

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
| `/route` (=`/model`=`/agents`) | **model & routing editor** (one screen): **(session)** default model, per-agent model/backend, **add/edit backends (profiles)**. ↑/↓ select · Enter edit/open · empty value = reset to default · Esc close. Editing the **session model** opens a **suggest box** — configured profile models plus the gateway's live catalog (prefetched on open), de-duplicated and filtered as you type: **↑/↓ cycle · Tab fills · Enter applies** the highlight or the typed value. An unreachable gateway falls back to free text. While editing an agent, **pick a profile with ←/→** (or type a model name). Use `+ add profile` to define a profile (endpoint/key/model/headers); in the form, Enter edits a field · **Tab saves**. **All edits are persisted to `config.toml`** (comments preserved) |
| `/tools` | available tools |
| `/sessions` | session list for this directory |
| `/resume [n]` | resume a session (no arg = list, `/resume 2` to switch) |
| `/rewind [n]` | roll back the last n user turns (default 1) |
| `/image <path>` | show an image inline |
| `/diff` | working-tree git diff |
| `/loop` | **Loop map** — projects turns · steps · tool activity · council rounds as structure (visualizes the *shape* of the loop) |
| `/context` | **context window** visualization — usage / window size · message count · compaction history (tokens before→after) · **every model in use and its window**. Edit a window: `/context <tokens>` (session model) or `/context <model> <tokens>` (e.g. `/context qwen3-coder:30b 128k`; `unlimited`/`0` clears it) |
| `/subagent` | **subagent timeout** — bare: show the base cap and the current elastic effective cap (from observed model speed); `/subagent <duration>` sets the base at runtime (e.g. `/subagent 10m`) |
| `/fork` | **branch** the current session to explore an alternative attempt (original preserved). Switches to the branch |
| `/replay` | **re-run the previous turn on the branch** (reproduce the same input). Compare with `/loopdiff` |
| `/loopdiff` | **structurally compare the current branch against the fork origin** (turns · steps · tools · council · tokens) |
| `/init` | analyze the project then write AGENTS.md |
| `/ultra <task>` | **ultra work mode** — orchestrate specialist subagents to carry out the task |
| `/permission` | cycle permission mode (ask→auto→allow→deny) |
| `/compact` | summarize/shrink the context (re-hydratable — see below) |
| `/clear` | clear the screen |
| `/quit` (=`/exit`) | exit |

> **Re-hydratable compaction**: when context is auto-compacted (or via `/compact`), the older turns are summarized for the live window as usual — but the originals are never lost. The compacted region is indexed **fully deterministically (no extra model call)** into topic shards **by the file each turn touched**, each carrying a one-line brief = its **tool-action trail** (e.g. `read · edit×2 · bash`); the summary then carries a notice listing the recoverable topics with those briefs. The agent calls **`recall_context("<topic>")`** (a file path works well) to pull a topic's original messages back **verbatim** on demand, instead of being stuck with the lossy summary. Recalls are bounded (each topic once, a per-turn budget, size-capped output) so re-hydration can't reopen the window; topics are aggregated across multiple compactions so nothing becomes undiscoverable. Unlike mainstream agents (opencode/Codex/Claude Code, which summarize-and-forget), the shed detail stays addressable. The pull is also **pushed**: each step, up to 3 compacted-away topics that lexically match the current task — the recent user prompt joined with the latest assistant message — are surfaced as one-line hints ("possibly relevant earlier context — call recall_context"), so a model that never thinks to recall still gets pointed at what it lost. Matching is ranked by **BM25-lite inverse-document-frequency**: a rare token that pins one region (`dehydration`, `heap.go`) outranks a generic one shared by many shards (`handler`, `the`), so the hints point at the region the current step actually needs rather than the most common word. It stays purely lexical/deterministic — no embedding dependency, and the hint only *points*; the model still pulls the verbatim originals via `recall_context`.

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
| Ctrl+F | search the transcript (type to narrow · enter/↓ next · ↑ prev · esc close) |
| Ctrl+L | clear the screen |
| Shift+Tab | switch permission mode |
| Ctrl+Q | exit (or `/quit`) |
| Ctrl+C | *nothing* — deliberately left free for the terminal's own drag-select + copy |
| mouse wheel | scroll · click a panel → focus (click again → zoom) |
| permission modal: y/a/p/n | allow (once) / always (this session) / project (persist to `.magi/config.toml`) / deny |

Typing keys go only to the input box and scrolling happens only via the dedicated keys above — so typing body text (including spaces) doesn't scroll the screen. While you're scrolled up reading, streaming doesn't yank the view down (auto-follow only when at the bottom). When the transcript overflows, a **header chip** shows the scroll position (`⇅ 42% (120/300)`), plus `↓ new` when fresh output is streaming in below while you're scrolled up (End jumps back). There is no drawn scrollbar — the chip replaced it, which also removed the Windows ambiguous-width misalignment class entirely.

Each working turn ends with a one-line receipt — `▣ turn: 14 steps · 3 file(s) · council r2 · 3m49s` — so a turn's cost is visible without scrolling back. When the agent needs YOU to decide between real alternatives, it can open a **selection modal** (the `ask_user` tool): ↑/↓ or 1-9 pick, enter answers, esc dismisses (the model is told you declined and proceeds on its own judgment). Multiple questions appear one modal at a time.

**Persisting a permission (`p` = project):** choosing `p` writes an allow rule to `.magi/config.toml` so the choice survives restarts. The rule is **scoped as narrowly as the tool allows**: most tools persist as `tool(**)`, but `bash` persists only the **program name** you approved — approving `curl https://x` records `bash(curl:*)`, not a blanket `bash(**)` — so one approval never silently pre-authorizes every future command. A command that opens with a shell metacharacter (a pipe/redirect) has no stable program to pin to, so it stays session-only rather than over-granting. The destructive/egress scanners still re-prompt on dangerous invocations even of an allowed program.

**Ambiguous-width characters (mostly a Windows note):** at startup magi probes the terminal once and matches its cell-width measure automatically (Console-API cursor delta on Windows, a cursor-position query elsewhere). If the probe can't run (e.g. redirected stdio) or guesses wrong, force it with `MAGI_AMBIGUOUS_WIDTH=wide` (or `narrow`); `MAGI_WIDTH_PROBE=0` disables the probe, and the standard `RUNEWIDTH_EASTASIAN=1` is also honored.

### Mouse / text copy (no modes)
Wheel scroll · drag select · click focus all work **without any mode switch** — because the app handles selection/copy itself. **Dragging highlights that range (character/cell granularity, partial-line selection allowed), and releasing copies it to the clipboard** (tries both the OS clipboard `pbcopy`/`wl-copy`/`xclip` and OSC52). Wheel scrolling during a drag works too (the selection is pinned to content position, so it persists across scrolling).

### Transcript rendering
- **edit/write appear as colored diffs** — syntax highlighting (language detected by file extension) + a line-number gutter, additions/deletions as `+`/`-`.
- **bash · read · grep · glob · list · webfetch · websearch results appear as collapsed blocks** inline (read shows line numbers + syntax highlighting). Long ones get a `… +N more` footer and **clicking the block expands it**.
- **Council rejection reasons** are shown wrapped below the one-line verdict (the full reason is in the detail modal via member click).
- **Council waiting line**: while a round is open and deliberation is under way, the footer names what it's waiting on in one fixed line alongside the spinner — `⚖ 플랜 감사 판정 대기 중…` for a plan audit, `⚖ 카운슬 심의 판정 대기 중…` for review/consensus — so the pause doesn't read as a stall.
- **Pre-review report folding**: a "review this" request flows report → council review → revised report; when a review round votes continue, the original (pre-review) report is folded to a one-line stub (`≡ (검수 전 보고서 — 접힘, 아래 최종본 참고)`) **the moment the revision actually lands**, leaving only the final result. The fold is deferred to the revision's arrival, so an interrupted or errored review leaves the original intact; an identical revision leaves the original untouched (no blink-out-and-reappear).

### Status panel (post-it)
When there are plans (todos) · subagents · context, a **rounded-outline box (post-it) appears at the top-right** (hidden if none). The transcript uses the full width and the box is drawn overlaid on top of it (bottom-aligned, so it usually floats over empty space); **dragging the box's left edge adjusts its width**. Click a subagent line to zoom into that agent. The box border is assembled from the terminal's real cell widths, so a todo/subagent line carrying an emoji like 🚀 keeps the right `│` aligned whether the terminal draws it one cell or two — the emoji width is probed once at startup, and can be forced with `MAGI_EMOJI_WIDTH` or disabled with `MAGI_WIDTH_PROBE=0`.

### Theme
At startup it detects dark/light from the terminal background color, and **if you change the OS theme while running it follows within a few seconds** (it re-queries the background color periodically). Force `auto`/`dark`/`light` with `--theme` (or `MAGI_THEME`).

### Steering (interrupting mid-work)
Input stays alive even while work (a turn) is running — you can keep typing (including inline Korean IME composition), and pressing Enter **injects the message immediately into the in-progress turn**. The main agent sees and reflects that message **at the next step** — it isn't queued to surface only after the turn fully ends; you're steering the running agent in place. The message appears in the transcript right away.
The main agent **runs subagents delegated via `task` in the background (as sidecars) and returns immediately**, so it isn't blocked — it reacts to steer messages right away.
Even when the main agent has *nothing to do but wait* for its background subagents to report, a message you send is handled right there — it doesn't sit until they finish. Small talk gets a brief reply; a steer actually changes the running work: "only look under `docs/`" cancels the now-irrelevant readers and switches course, "after that, also write a README" folds the extra step in, and an ambiguous steer prompts a quick clarifying question before it acts. (The main only *signals* the change while waiting; it carries it out — re-planning and re-dispatching — on its next step.) When a steer actually lands, the transcript records a durable "Steer applied …" audit line so you can see the redirect/cancel fired — the agent doesn't just verbally agree and move on. On a redirect/append the re-plan re-decomposes the *adopted* task (for "append", the original goal folded with the steer's constraint), not the bare steer text.
A message that can only be answered after the current work finishes is queued and **stays visible where you typed it**; when its answer is ready the query is **pulled down to just above the answer** so the two render as a pair (and on reopening the session there's no duplicate — the pairing is preserved).
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
- While a multi-step plan is executing, the header also surfaces the **active plan step** (the current item of the procedure planner's checklist), so what the agent is working on right now is visible without opening the status panel.

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

The tool set a given agent sees is gated by its **role** (read-only agents get no write/bash) and by **depth** (some orchestration tools are top-level only) — the tables below are the full catalog. The `Permission` column is the approval axis the tool trips (`ask` = confirmed under `ask`/`auto`; `—` = read-only, never prompts).

### File & search
| Tool | Description | Permission |
|---|---|---|
| `read` | read a file (line numbers, offset/limit) | — |
| `write` | create/overwrite a file | ask |
| `edit` | exact string replacement (unique match) | ask |
| `multiedit` | apply multiple hunks to one file atomically (all-or-nothing) | ask |
| `grep` | regex content search | — |
| `glob` | filename glob (`**` supported) | — |
| `list` | directory listing | — |
| `findcontext` | rank the files most relevant to a natural-language query, **prioritizing files that DEFINE the named symbols** (funcs/classes/types) — cheaper and more precise than reading broadly | — |
| `astgrep` | structural (AST) code search (`ast-grep`) — matches code shape, not text, so a rename/reformat doesn't fool it | — |

### Data & aggregation (deterministic — don't hand-count)
Weak models routinely miscount rows or fabricate a total when asked to "count/sum" over a large file. These tools do the arithmetic **in Go, deterministically**, so the answer is real evidence rather than a guess, and a huge file never has to enter the context to be tallied.
| Tool | Description | Permission |
|---|---|---|
| `countlines` | line/word/byte counts (pure-Go `wc`) over a file or glob — LOC & size tallies without reading whole files | — |
| `countmatches` | count regex/fixed-string occurrences across a file or glob → total + how many files matched | — |
| `groupby` | group delimited rows by a column value (or a regex capture) → per-group count or `sum(value_column)`, sorted | — |
| `tabulate` | aggregate one numeric column of a delimited file (`sum`\|`count`\|`avg`\|`min`\|`max`) over rows passing an optional numeric filter | — |

### Execution
| Tool | Description | Permission |
|---|---|---|
| `bash` | shell execution (timeout · exit code; `background=true` for long-running commands). Runs with **no controlling terminal and stdin closed** so a command that tries to prompt fails fast instead of hanging | ask |
| `bash_output` | fetch new output from a background command since the last read | — |
| `bash_input` | send a line to the **stdin of a background command** — drives a REPL/line-debugger (`python3`, `psql`, `gdb`); `eof=true` closes stdin. A pipe, not a TTY (curses/password prompts won't work) | — |
| `bash_kill` | terminate a background command | — |

### Code navigation (LSP)
| Tool | Description | Permission |
|---|---|---|
| `lsp_diagnostics` | `gopls` diagnostics (types/unused etc.) — Go | — |
| `lsp_definition` | symbol definition location (Go via `gopls`; ~30 langs via LSP) | — |
| `lsp_references` | all references to a symbol (semantic, multi-language) | — |
| `lsp_symbols` | file symbol outline (multi-language) | — |

### Web
| Tool | Description | Permission |
|---|---|---|
| `webfetch` | URL → text | ask |
| `websearch` | web search (DuckDuckGo, or Brave/Tavily key) | ask |

### Planning & self-control
| Tool | Description | Permission |
|---|---|---|
| `todowrite` | record/update a plan (checklist). The status panel is driven by deterministic signals too, so progress updates even without this call | — |
| `replan` | declare the **current plan is unworkable** given what execution has actually shown (a premise broke) and get a fresh decomposition — instead of grinding the dead plan to the step ceiling. Plan-eligible agents only | — |
| `recall_context` | re-hydrate detail an earlier compaction shed, **verbatim**, by topic (a file path works well) | — |

### Memory
| Tool | Description | Permission |
|---|---|---|
| `recall_memory` | pull saved team memories/skills from the shared experience store by keyword | — |
| `remember` | contribute a lesson to shared memory (lands in `pending/` for review) | — |
| `skill` | load a named skill's body to follow it | — |

### Subagents & orchestration
| Tool | Description | Permission |
|---|---|---|
| `task` | delegate to subagents (single/parallel); backgrounded as sidecars at top level | — |
| `report` | (subagent) end the turn and hand the result back to the orchestrator with a status (done/blocked/failed) | — |
| `ask` | (subagent) ask the orchestrator — which holds the full context — to unblock you; blocks then resumes on the answer | — |
| `cancel_dispatch` | (orchestrator) cancel still-running background subagents once an intermediate result made their work moot — reclaim the budget instead of waiting | — |
| `resolveconcern` | (orchestrator) retire a structural concern from the durable ledger **after** it is genuinely resolved (see §3, concern ledger) | — |
| `route_interjection` | (top level) decide how to handle a **new user request that arrived mid-task** — `redirect` (switch now) · `append` (satisfy both) · `queue` (defer). The safe default is to not call it and let the request run as its own turn | — |

### User interaction
| Tool | Description | Permission |
|---|---|---|
| `ask_user` | multiple-choice question **to the user** (selection modal; top-level interactive only — declined gracefully in headless) | — |

- All file tools **deny access outside the working directory** (a jail) — a `../../etc/hosts` read is refused, not served.
- Read-only tools run **in parallel** within a step; writes are serialized.
- After a file modification, **diagnostic feedback** (Go: gofmt/`go vet`, Python: `py_compile`) is fed back so the agent self-corrects (the harness, §3).

## 6. Multi-agent

Delegate to subagents with the `task` tool. Default agents:
- **explore** — read-only code exploration
- **reviewer** — code review (read-only)
- **coder** — implementation (read/write/edit/multiedit/grep/glob/list/bash)

Limits (D7): depth 3 · concurrency 8 · cumulative 50. Assign per-agent models with `[routing]`.

### Sidecar execution model (main = the UI thread)
When the top-level orchestrator delegates with `task`, the subagent runs as a **background sidecar** and `task` returns immediately. The main agent isn't blocked (= idle like a UI thread) and **reacts to user input right away**, while each subagent result is injected into the main session **as it finishes** and processed **incrementally**. Injected results come with **the number of subagents still running**, so the orchestrator decides for itself whether to wait for the rest (rather than re-delegating) · to **delegate a new follow-up** based on results · or to synthesize. It delegates heavy work and handles light work inline.

Each sidecar has a **supervisor** doing health checks. The orchestrator can't know in advance how long a delegated task will take, so the two watchdogs are deliberately asymmetric — a tight liveness check plus a *generous* wall-clock backstop:
- **Stall detection** (`SubagentStall`, default 4 min of no activity) — the primary liveness guard, catching a truly wedged child (no events at all). It stays **activity-based**: any event, including streaming reasoning/text deltas, re-arms it, so a slow single generation is never false-killed. It is **suppressed while a tool is in flight** — a silent long-running tool (e.g. a multi-minute `bash` build) emits nothing until it returns, and is bounded by its own timeout rather than mistaken for a hang.
- **Hard timeout** (`[orchestration] subagent_timeout`, default 5 min/attempt as the **base**) — the only bound on a subagent that stays event-active while making no progress (hallucinated targets, Q&A ping-pong with the parent): such churn never trips the stall watchdog, and at a generous cap one bad explorer can outlive the parent's whole wall-clock budget. The effective cap is **elastic**: magi tracks each model's recent LLM round-trip time and budgets the attempt for ~6 round trips, clamped to base/2–base×3 — so a slow model isn't cut mid-legitimate-generation and a fast one doesn't get slack to churn in. Set the base in config (`subagent_timeout = "5m"`), override per run with `MAGI_SUBAGENT_TIMEOUT`, or adjust live with **`/subagent <duration>`** (bare `/subagent` shows base + current effective cap). Expiry is a **judgment lease**, not an unconditional kill: the orchestrator's model reviews a deterministic digest of the child's recent tool calls (consecutive duplicates collapsed with repeat counts) and votes EXTEND (real progress → +base/2 more) or KILL (churn → restart policy applies). The judgment is bounded on every axis — it fires only at expiry (zero cost for the common under-cap attempt), the verdict call is time-capped, any error or ambiguous reply fails safe to KILL, and an absolute **backstop of base×3** ends the attempt regardless of verdicts, so a fooled judge costs at most the backstop. `MAGI_SUBAGENT_JUDGE=off` disables judging (expiry kills immediately, the pure elastic-cap behavior). Legitimate focused subagent work fits well under 5 minutes; a genuinely long delegated task should be split, not stretched. The planner's background explorers get an even tighter **spawn-wide** bound (3 min, restarts included): expiry is terminal rather than retried, so a churning explorer's failure result reaches the parked parent within minutes instead of holding it for the full cap × restart budget.
- **Auto-restart** (`SubagentMaxRestarts`, default 2) — retry on stall/timeout/transient error (reusing the same role panel), inject an ERROR result when exhausted. The rest · the main continue even if one dies.

(Nested subagents work via synchronous delegation — background is top-level only.)

### Escalation (subagent → orchestrator `ask`)
If a subagent gets stuck during execution, it **asks the orchestrator via the `ask` tool and gets an answer on the spot** (blocks then resumes). The orchestrator holds the **full context** (the user's original request · the whole plan · other subagents' results), so it can: clarify/decide intent, provide a path/constraint, **relay a follow-up question to the user**, and **coordinate with other subagents** (peer questions also go through the orchestrator — so context stays in one place). The `ask` tool's description spells out "what the orchestrator can do for you" so the subagent knows what to request. Only one ask is handled at a time (serialized), with a 2-minute timeout.

## 7. Memory & Context

- **AGENTS.md**: the contents of the working directory's (+ `.magi/AGENTS.md`, global `<config>/AGENTS.md`)
  are injected into the system prompt and **preserved even through compaction**. Auto-generate with `/init`.
- **Auto-compaction**: when token count (the larger of the backend's real count and the live estimate) exceeds 80% of the model window, older turns are summarized (recent ones preserved). The window is **per model** (different agents can run different models, each with its own window), resolved from the model registry; for an unseeded model magi **probes the backend** for its real context length (vLLM `max_model_len`, LiteLLM `/model/info`, Ollama `/api/show`) — at startup for the initial model, and **lazily the first time any other model is used** (e.g. after a runtime `/route` switch). Claude/Gemini expose no such endpoint, so they rely on the seeded table. A model with no usable window is treated as **unlimited** (no % gauge, no ratio compaction) rather than mis-sized to a tiny fallback; override any model's window with `/context <model> <tokens>`.
- **Tool-result cap**: a single tool result is capped (~64KB) before it enters the context, so one huge output (e.g. reading a 500KB file) can't blow the window past what compaction can recover — the agent is told to narrow its read/command.
- **Shared brain (D13)**: the `memories/` · `skills/` in `<config>/experience` (or `experience_dir`) are
  recalled and injected at session start. The `remember` tool contributes to `pending/` (moved to `memories/` after review).
  Make the directory a git repo and commit/pull to share with the team.
  Step-by-step bootstrap / file format / review procedure: [`EXTENDING.md`](EXTENDING.md) §2.

## 8. Skills

`<config>/skills/*.md` or `<workdir>/.magi/skills/*.md` (first line = description, rest = body), plus the **skill-creator layout** `<workdir>/.claude/skills/<slug>/SKILL.md` (frontmatter `description` = trigger) — the format Claude Code/OpenCode tooling and the bundled engram plugin produce, readable without conversion.
The list is exposed in the system prompt, and the model loads a body with the `skill` tool to follow it. The skill list refreshes when the source dirs change (mtime), so a skill engram saves mid-session is available on the next turn without a restart.

## 9. Plugins (Lua)

`plugin.toml` + `init.lua` in `<config>/plugins/<name>/` or `<workdir>/.magi/plugins/<name>/`.
Capabilities: `tool`, `command` (slash commands like `/login`), `context-provider`, `mcp`, `llm-headers`, `analyze`, `experience` (magi.propose_experience — route plugin-learned lessons/skills into the D13 shared store's review queue), `notify` (magi.notify — append a system ⟳ note to a session's transcript, the active-notification channel; the model sees it next turn). `magi.remove_file` deletes a workdir file/dir under the same fs:write grant — the undo half of artifact-writing plugins. **Hot-reload** on file change.
Sandboxed (dangerous stdlib blocked) + manifest permissions (`fs:read`, `net`, `exec`) enforced.
Example: `plugins/examples/wordcount`.

**Observer plugins.** Beyond the lifecycle events (`startup`/`shutdown`/`session_start`), `magi.on` accepts two **observation events** carrying a payload table: `user_message` (`{session, text}` — a genuine user prompt was submitted) and `turn_finished` (`{session, text, outcome, reason, skills}` — a top-level turn ended with that final assistant answer; `skills` = comma-joined skill names the agent loaded this turn, for usage metering). `outcome` is the turn's **structural verdict**, so an observer never has to guess success from phrasing: `verified` (the council itself voted done), `unverified` (landed without council approval — deadlock/cost/round cap), `guard` (loop/stall guard force-stop), `error`, or `done` (plain finish, no verdict either way); `reason` carries the cause when there is one. They fire **asynchronously off the turn path** (a bounded queue + one worker), so a slow handler never delays the conversation; overflow drops events (observation is best-effort). Paired with **`magi.analyze{system=, text=, model=?}`** — a one-shot, **tool-free sidecar LLM call** (capability `analyze`, since it spends tokens; time-capped; model defaults to the session model) — and **`magi.json_decode(s)`**, a plugin can watch the conversation, extract structured knowledge (lessons, summaries), and persist it with `magi.write_file`, then feed it back via `magi.register_context_provider`. The bundled **`plugins/engram`** self-improvement plugin (auto lesson/skill extraction gated on the structural outcome) is the reference user of this trio — see its README.

```toml
# plugin.toml
name = "wordcount"
capabilities = ["tool"]
permissions = ["fs:read:."]
```

**Install / update.** A plugin published as a git repo (its repo root holds `plugin.toml`)
installs with `magi --plugin-install <git-url> [--plugin-pin <tag/branch>]`, which clones it
into `<config>/plugins/`. `magi --update-plugins` (or `--update`, which also updates the
binary) fast-forwards every git-checkout plugin; local changes or a missing remote are
reported and skipped rather than overwritten. See §Version / self-update.

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

**Provider resilience (why a flaky backend rarely loses your turn).** magi treats the OpenAI-compatible layer as best-effort and recovers rather than aborting where it safely can:
- **Tool-call parsing** — it parses all tool-call variants of local/cloud models (JSON/XML/native), and if a backend rejects `cache_control` (400/422) it transparently retries once without caching and remembers that for the session.
- **Transient failures** — connection errors and 429/5xx are retried with bounded backoff (honoring `Retry-After`); an exhausted retry surfaces the **status + body** so the failure is diagnosable, never a bare "request failed".
- **Harmony tool-call misparse** — Ollama's gpt-oss harmony parser sometimes **500s when the model emits its final answer as prose** but the server tries to read it as a tool call (`error parsing tool call: raw=…`). Because the request is unchanged across retries the 500 is deterministic, so magi retries **once with the tools array stripped**: with no tools advertised the server skips tool-call parsing and returns the same prose as normal content — the answer the model actually produced is recovered instead of the turn hard-aborting. Scoped to that exact signature, so a genuine outage still surfaces as an error.

## 12. Status & scope

The **loop-engineering track is shipped**, not planned — it is the signature of the tool and is described throughout this manual: the **consensus council** that takes the termination decision away from any single model (Melchior · Balthasar · Casper, §3), the **Loop map** (`/loop`), the live deliberation panel, **rewind/fork/session-diff** (`/rewind` · `/fork` · `/loopdiff`, §4), the **concern ledger** (§3), and **re-hydratable compaction** (§4). Likewise already implemented: the **OS sandbox** (`--profile`/`sandbox`, §3), **LSP navigation** (`lsp_*`, §5), **web search** (`websearch`), and **prompt caching** (`cache_control`, on by default with automatic fallback). The feature/milestone spec with test examples lives in [`SPEC.md`](SPEC.md); the internals in [`ARCHITECTURE.md`](ARCHITECTURE.md).

**Genuinely out of scope (today).** No web UI or hosted remote sharing — magi is a terminal client (sharing is via a git-backed experience store, §7, not a server). Automatic context *ranking* is deliberately lexical/deterministic (BM25-lite, §4), not embedding-based, so there is no vector-DB dependency. These are scope choices, not gaps to be silently filled.

[Ollama]: https://ollama.com
</content>
</invoke>
