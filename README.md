<div align="center">

# magi

**A terminal AI coding agent that doesn't decide it's done on its own.**

Most agents let a single model call its own work finished — so they stop early, or churn forever.
**magi puts termination to a vote.** Three specialists, each with a different lens, deliberate on
every turn and only let the loop end when they agree.

[English](README.md) · [한국어](README.ko.md) · [Manual](docs/MANUAL.md)

![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-Apache--2.0-blue)
[![CI](https://github.com/sayaya1090/magi/actions/workflows/ci.yml/badge.svg)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)
![Single binary](https://img.shields.io/badge/build-CGO__free%20single%20binary-success)

</div>

---

## Why magi?

An agent loop has one hard question: **when is the turn actually finished?**

Leave that to the same model that did the work and you get the two failure modes everyone has seen —
it declares victory with a bug still in the diff, or it loops "just to be safe" long after the task
was done. magi treats *the loop itself* as the thing to engineer, not just the prompt.

```text
you ▸ add a --dry-run flag to the deploy command

  ◈ planner   3 steps — locate the flag parser · add the flag · wire the guard
  ✓ explore   deploy command & flag parsing → cmd/deploy.go uses pflag
  … agent edits cmd/deploy.go, runs go build …

  ⚖ council · round 1
     ● Melchior   [correctness]   done    · 88%
     ● Balthasar  [verification]  reject  · 91%   → no test covers --dry-run
     ● Casper     [completeness]  done    · 80%
     → reject  (1 done / 2 continue)   feedback injected, loop continues

  … agent adds a test, reruns go test …

  ⚖ council · round 2  →  done  (3 / 0)   ✓ turn complete
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

## The Procedure Planner

Before the main agent runs, a tool-free planner decomposes your request into an **ordered procedure**
and picks a **strategy per step** — then, for multi-step plans, the *council audits the plan itself*
before a single file is touched.

| Strategy | What it does |
|---|---|
| `solo` | the main agent handles it directly (writes, edits, anything needing full context) |
| `parallel` | independent read-only investigations you already know are relevant, run concurrently |
| `scout` | **adaptive** discover → fan-out: one explorer lists what exists, then each item becomes its own parallel investigation |

`scout` is the interesting one: *"read every design doc"* becomes one explorer that lists the docs,
then one parallel reader per doc — fan-out targets discovered at runtime, not guessed up front.

Steps register as **todos** you can watch tick off. The plan-audit council approves (`approve`) or
sends it back for revision (`revise`), and the criteria the members derive become the **completion
contract** the termination gate later judges the finished work against. Findings are synthesized into
the main agent's context — it continues the plan rather than re-reading everything.

```toml
[orchestration]
planner = true            # default on; set false for a plain single-agent loop

[routing]
planner = "fast"          # run the planner on a cheaper/faster backend
```

---

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
- **An OpenAI-compatible LLM backend.** Local is great — [Ollama](https://ollama.com) recommended:
  ```sh
  ollama pull qwen3-coder:30b     # default — strongest at multi-step tool-calling
  ollama pull gpt-oss:20b         # lighter, well-balanced alternative
  ```
  > Small models (e.g. `llama3.1:8b`) tend to emit tool-call JSON even when greeting you once tools
  > are enabled, so they're off the default list.

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
./magi --update                # self-update to the latest release (checksum-verified)
```

**In the TUI:** **Enter** sends · **Esc** interrupts the running turn · **Ctrl+C** / `/quit` exits.
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
| `--model` | `MAGI_MODEL` | `qwen3-coder:30b` | model id |
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

## Agents & Tools

**Bundled subagents** — seven specialists delegated via the `task` tool, with bounded recursion
(depth/concurrency/total caps) so fan-out can't run away:

`explore` · `locator` · `analyst` · `architect` · `coder` · `reviewer` · `tester`

(plus `planner` — the pre-flight procedure planner above, run automatically each turn rather than
delegated via `task`.)

**Built-in tools:**

`read` · `write` · `edit` · `multiedit` (atomic multi-hunk) · `grep` · `glob` · `list` ·
`bash` (timeout · exit code) · `astgrep` · `findcontext` · `lsp_diagnostics` · `webfetch` ·
`todowrite` · `remember` (shared memory) · `skill`

After an edit, **diagnostic feedback** (gofmt / go vet / py_compile / LSP) flows back so the agent
self-corrects. Read-only tools run in parallel within a turn.

**Slash commands** — type `/` for an autocompleting palette (aliases search by prefix):

`/help` `/route` (`/model`, `/agents`) `/tools` `/sessions` `/resume` `/rewind` `/image` `/diff`
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
