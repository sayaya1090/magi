<div align="center">

# magi

**A terminal AI coding agent that doesn't decide it's done on its own.**

Most agents let a single model call its own work finished — so they stop early, or churn forever.
**magi puts termination to a vote.** Three specialists, each with a different lens, deliberate on
every turn and only let the loop end when they agree.

[![CI](https://github.com/sayaya1090/magi/actions/workflows/ci.yml/badge.svg)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)
[![coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/sayaya1090/magi/badges/coverage.json)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)

[English](README.md) · [한국어](README.ko.md) · [Manual](docs/MANUAL.md)

![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-Apache--2.0-blue)
[![CI](https://github.com/sayaya1090/magi/actions/workflows/ci.yml/badge.svg)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)
![Single binary](https://img.shields.io/badge/build-CGO__free%20single%20binary-success)

</div>

---

## Why magi?

An agent loop has one hard question: **when is the turn actually finished?**

Leave it implicit — the turn ends when the model stops calling tools — and a turn that trailed off
mid-thought is indistinguishable from one that is actually done. magi makes ending an **act**: the
agent declares it, and a council reads what actually happened before the turn is allowed to close.

```text
you ▸ add a --dry-run flag to the deploy command

  … agent reads cmd/deploy.go, edits it, runs go build …

  ⚙ council {complete: true}          the agent says it is finished

  ⚖ the council reads the record
     ── WHAT MAGI OBSERVED
        changed: cmd/deploy.go
        ran clean: go build ./...
     ── THE WORKSPACE RIGHT NOW (read just now, not from the record)
        cmd/deploy.go — 4,102 bytes, modified 12s ago

     ● Balthasar [verification]  nothing runs the new flag — `go test ./cmd` was never run
     → not accepted yet; the agent keeps working

  … agent adds a test, runs go test …

  ⚙ council {complete: true}   →   accepted   ✓ turn over
```

The decision to stop is taken away from any single model and given to a **consensus council**. That
one change is the project's whole reason to exist; everything else is built to make that loop
observable, steerable, and reproducible.

---

## The Council

At the moment the loop would naturally end, a council of members votes **done**, **reject**, or
**abstain**. A pure, unit-tested consensus rule tallies the votes into one decision. A `reject`
doesn't just stop the agent — it feeds the members' aggregated feedback back into the loop as the
next instruction.

The three default members — the **MAGI** — each judge through a different lens:

| Member | Lens | Asks |
|---|---|---|
| **Melchior** | `correctness` | Is the work correct? Edge cases, regressions? |
| **Balthasar** | `verification` | Is there *evidence* it works — do build/tests pass? No claims on faith. |
| **Casper** | `completeness` | Did it do everything the task asked? Nothing left unfinished? |

**Consensus, not a single judge.** The tally rule is configurable:

| Rule | Finishes when… |
|---|---|
| `majority` *(default)* | a strict majority of voting members say done (a tie continues) |
| `unanimous` | every member says done |
| `quorum:k` | at least *k* members say done |
| `weighted:θ` | the done-weight share meets threshold θ |
| `veto:Name` | a named member can veto any finish |

**Built to never trap or rubber-stamp the loop:**

- A **tie, an unmet quorum, no voters, or an error** all resolve to *continue* — the council never
  finishes on ambiguity, only on affirmative agreement.
- **No-progress detection** stops the gate when feedback goes empty or repeats, so it can't churn.
- Rounds are **capped** (`max_rounds`, default 3); hitting the cap finishes with a noted "unresolved".
- A member that errors or returns garbage **abstains** (dropped from the denominator) rather than
  blocking the gate.

**Evidence, not vibes.** Members judge the agent's *report* against the *task* and *plan*, and can
weigh **deterministic signals** — real `build` / `test` / `lint` outcomes you opt into. A read-only
or investigation turn that changed nothing is recognized as such (`NoChanges`), so the council judges
the *answer* on its merits instead of demanding a diff that was never going to exist.

```toml
[council]
# enabled  = true         # on by default; set false for a plain single-model loop
rule       = "majority"   # unanimous | majority | quorum:k | weighted:θ | veto:Name
max_rounds = 3
verify     = "go test ./..."   # a deterministic signal the council weighs each round
# criteria = true              # elicit explicit acceptance criteria as the contract

# Customize the bench — or keep the MAGI default
[[council.member]]
name = "Melchior"
lens = "correctness"
# model / provider can route a member to a cheaper or stronger backend
```

> The consensus logic lives in `internal/core/council` as **pure domain code** — no I/O, no LLM. That
> separation is what lets *"the council decides, not one model"* be a tested invariant instead of a
> hopeful prompt.

---

## How a turn ends

A turn does not end by going quiet. When the agent believes the work is finished it says so —
`council{complete: true}` — and three members read the record before the turn is allowed to close:
what commands actually ran and how they ended, what the workspace holds *right now*, what the agent
itself said. They either accept, and the turn is over, or they name what is still undone and the
agent keeps working.

That record is the point. magi grants every tool call, so it knows which commands ran, what their
real exit was (including which stage of a pipeline failed, which the reported exit hides), and
which paths they wrote. On a declaration it also takes a fresh read of the workspace — files
modified since the task began, background commands still alive, and any path the record claims was
written that is not on disk. A check written in advance can be wrong about the work; a record of
what was granted cannot be wrong about what was granted, and where it is incomplete it says so.

The same record rides every step, re-rendered, so the agent is never working from memory alone.

Asking is separate from declaring: `council{question}` gets their reading on something you are
unsure of, and it ends nothing.

---

## One agent

magi used to spawn subagents, plan the work into steps, author executable checks for each step,
and hold the turn open until a council voted the checks satisfied. All of it is gone.

The reason is in the logs. Every one of those stages decided something *before* the work existed,
and the defects that cost the most were of exactly one kind: magi believing a judgement it had made
in advance over the record of what actually happened — a probe that passed only when the server was
down, a grep demanding a hyphen where the generator writes an underscore, a brief paraphrased until
the graded identifier was gone. What is left is the loop, the tools, the record, and a council the
agent calls when it wants one.

## Loop Engineering Toolkit

The loop is a first-class, inspectable object — not a black box between you and the model.

| Command | What it gives you |
|---|---|
| `/loop` | the loop map — turns · steps · council rounds at a glance |
| `/context` | exactly what's filling the context window (usage · compactions) |
| `/rewind` | roll back the last user turn(s) |
| `/fork` | branch the session to try an alternative, original kept |
| `/replay` | re-run the last turn on a branch |
| `/loopdiff` | compare a branch against its fork origin |

Every turn is **event-sourced** to an append-only JSONL log, which is what makes rewind, fork, and
replay possible — the loop is observable and reproducible, not ephemeral.

---

## Quick Start

### Requirements

- **Go 1.26+** (to build)
- **An OpenAI-compatible LLM backend.** [Ollama](https://ollama.com) is recommended. The default model
  is **`gpt-oss:120b-cloud`**, a strong model on Ollama's **free cloud tier** — no GPU needed, just sign
  in once:
  ```sh
  ollama signin                   # free tier; the default gpt-oss:120b-cloud runs in Ollama's cloud
  ```
  Prefer to run **fully local**? Pull a model and point magi at it:
  ```sh
  ollama pull qwen3-coder:30b
  ./magi --model qwen3-coder:30b  # strongest local coder (24 GB GPU); or MAGI_MODEL=…
  ```
  > Any OpenAI-compatible endpoint works (vLLM, LiteLLM, hosted APIs) — see Configuration. Very small
  > models (e.g. `llama3.1:8b`) tend to emit tool-call JSON even when greeting you, so they're a poor fit.

### Install

```sh
# Pre-built binary
curl -fsSL https://raw.githubusercontent.com/sayaya1090/magi/main/scripts/install.sh | bash

# Homebrew
brew install sayaya1090/tap/magi
```

### Build from source

```sh
make build        # CGO_ENABLED=0, version injected → ./magi
# or
CGO_ENABLED=0 go build -o magi ./cmd/magi
```

Pure Go — a single static binary, no CGo. Copy it anywhere and run.

### Run

```sh
./magi                         # interactive TUI
./magi --version               # print version
./magi --update                # update the binary AND managed plugins (checksum-verified)
./magi --update-core           # update only the binary
./magi --update-plugins        # update only managed (git) plugins
```

On an interactive terminal, magi checks for a newer release at most once a day: a patch
release just shows a banner, a minor/major release installs automatically (cancellable),
then asks you to restart. Non-interactive runs (`-p`, pipes, CI) never check. Opt out with
`--no-update-check`.

**In the TUI:** **Enter** sends · **Esc** interrupts the running turn · **Ctrl+Q** / `/quit` exits (Ctrl+C is left free for drag-copy).
Dangerous tools (`write`/`edit`/`bash`) ask before they run (`y` allow · `a` always · `n` deny).
Markdown and syntax highlighting adapt to dark/light terminals automatically.

### Headless (scripts & CI)

```sh
./magi -p "list the Go files and summarize the architecture"
./magi -p "create hello.txt containing: hi" --output json   # JSONL event stream
echo "explain main.go" | ./magi -p -                        # stdin
```

---

## Configuration

A commented `config.toml` is generated on first run (and never clobbered after). Precedence is
**flags > env > config > defaults**.

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `--model` | `MAGI_MODEL` | `gpt-oss:120b-cloud` | model id (Ollama free cloud tier; `ollama signin`) |
| `--base-url` | `MAGI_BASE_URL` | `http://localhost:11434/v1` | OpenAI-compatible base URL |
| `--permission` | `MAGI_PERMISSION` | TUI `ask` / headless `allow` | `ask` \| `auto` \| `allow` \| `deny` |
| `--output` | — | `text` | `text` \| `json` (headless) |
| — | `MAGI_API_KEY` | *(none)* | key for remote backends (Ollama needs none) |

**Per-agent model & backend routing** — run cheap models for grunt work, strong ones where it counts:

```toml
[routing]
explore = "fast"             # → [llm.profiles.fast] (its own endpoint/key)
coder   = "qwen3-coder:30b"  # just a different model on the default backend

[llm.profiles.fast]          # named backends; ${ENV} is expanded
base_url = "https://fast.gateway/v1"
api_key  = "${FAST_KEY}"
model    = "gpt-oss:20b"
```

---

## Tools

**Built-in tools:**

`read` · `write` · `edit` · `multiedit` (atomic multi-hunk) · `grep` · `glob` · `list` ·
`bash` (timeout · exit code · `background` for long-running commands) · `bash_output` ·
`bash_input` · `bash_kill` · `wait_for` · `port_owner` ·
`recall_context` · `recall_memory` ·
`webfetch` · `websearch` (DuckDuckGo, or Brave/Tavily with an API key) ·
`todowrite` · `council` (ask for a reading, or declare the task finished) · `remember` (shared
memory) · `skill` · `replan` · `ask_user` and `route_interjection` (interactive runs only)

After an edit, **diagnostic feedback** (gofmt / go vet / py_compile / LSP) flows back so the agent
self-corrects. Read-only tools run in parallel within a turn.

**Slash commands** — type `/` for an autocompleting palette (aliases search by prefix):

`/help` `/route` (`/model`) `/tools` `/sessions` `/resume` `/rewind` `/image` `/diff`
`/loop` `/context` `/fork` `/replay` `/loopdiff` `/init` `/ultra` `/permission` `/compact` `/clear`
`/quit`

---

## Context, Memory & Extensions

- **Project memory** — `AGENTS.md` (plus `.magi/AGENTS.md` and a global one) is injected into the
  system prompt as durable context that *survives compaction* (the CLAUDE.md equivalent).
- **Context-aware auto-compaction** — when real token usage passes ~80% of the model window, older
  turns are summarized while recent ones are preserved. A `ctx 42%` meter sits in the header.
- **Shared experience** — a git-backed memory/skill store (`<config>/experience`) the team can share;
  the `remember` tool contributes to a review queue.
- **Lua plugins** — drop a `plugin.toml` + `init.lua` into `.magi/plugins/`; auto-loaded, hot-reloaded,
  sandboxed. See [plugins/examples/wordcount](plugins/examples/wordcount).
- **MCP servers** — declare them in `config.toml` and their tools register at startup:
  ```toml
  [mcp.filesystem]
  command = "npx"
  args = ["-y", "@modelcontextprotocol/server-filesystem", "."]
  ```

---

## Architecture

magi is **ports & adapters (hexagonal)**: the core domain knows nothing about the UI, the LLM, or
plugins — adapters plug into it. Dependency direction is always inward.

| Choice | Why |
|---|---|
| **Go** | one static binary, trivial cross-compile, easy self-update, goroutine concurrency |
| **Bubble Tea (Charm)** | the standard for polished TUIs; markdown/code rendering turnkey |
| **Lua (gopher-lua)** | pure-Go embed (keeps the CGo-free build), natural hot-reload + sandbox |
| **Event-sourced JSONL** | an observable, replayable, fork-able loop |
| **OpenAI-compatible LLM** | one protocol adapter → local (Ollama/vLLM) or any hosted endpoint, incl. Claude/Gemini compatibility APIs |

```
cmd/magi            entrypoint (wiring)
internal/core       domain — depends on no adapter (incl. the pure council)
internal/port       ports (interfaces) — LLM, Store, Council, PluginHost …
internal/adapter    adapters — llm/openai · tui/bubbletea · plugin/lua · mcp · council/llm
plugins/examples    example Lua plugins
docs                ARCHITECTURE · DESIGN · SPEC · MANUAL · PLAN · FEATURES
```

Deeper reading: [ARCHITECTURE](docs/ARCHITECTURE.md) · [DESIGN](docs/DESIGN.md) ·
[SPEC](docs/SPEC.md) · [PLAN](docs/PLAN.md).

---

## License

**Apache-2.0** — see [LICENSE](LICENSE). When reusing third-party code, keep the `NOTICE` and
`THIRD_PARTY_LICENSES` files intact.
