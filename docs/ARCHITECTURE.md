# magi — Architecture (current)

This is the **as-built** reference for developing on magi. `DESIGN.md`, `SPEC.md`,
and `PLAN.md` are the original design intent / roadmap (kept for rationale, decisions
D1–D13); where they disagree with this file, **this file wins**.

magi is an extensible terminal AI coding agent: a Go core, a Bubble Tea TUI,
Lua plugins, OpenAI-compatible LLM access (Ollama/LiteLLM/etc.), multi-agent
orchestration, an event-sourced store, guardrails, and a deterministic workflow
engine. Single static binary (`CGO_ENABLED=0`), cross-platform.

---

## 1. Layering (hexagonal / ports & adapters)

Dependency rule, enforced at compile time: **`adapter → app → core`**, and
`app`/`adapter` depend on `port`. `core` imports nothing outside std + core.

```
cmd/magi/                 entrypoint: flag parsing, DI wiring, -p headless, TUI launch
internal/
  core/                     domain — no outward deps
    session/                Session, Message, Part, ToolCall, ToolResult, Todo, SessionMeta
    event/                  Event envelope + types (facts vs transient) + payloads
    command/                Commands (CreateSession, SubmitPrompt, Interrupt, …)
    artifact/               Artifact (reviewable outputs, D11)
    bus/                    in-memory pub/sub fan-out (per session)
    model/                  model registry (context window / pricing / caps)
    agent/ plugin/ tool/    (placeholder dirs — types live in app/ and adapter/)
  port/                     interfaces the core depends on (port.go): LLMProvider, Store,
                            Tool/ToolEnv, ExperienceStore, PluginHost, Platform, Scheduler…
  app/                      application service + agent loop + orchestration + guardrails + workflow
    app.go                  App (the Application): commands in → events out; session state
    loop.go                 runLoop: the agent loop; runGuard; systemFor; toolSpecs
    orchestrate.go          subagent dispatch/spawn/supervisor; escalate (ask); bgGroup
    planner.go              pre-flight planner: judge solo-vs-parallel, fan out explorers
    workflow.go             deterministic phase pipeline (localize→implement→verify→review)
    policy.go               guardrail policy engine (rules, secret-deny, bash scan, egress)
    hooks.go                lifecycle hooks (PreToolUse/PostToolUse/Stop) + built-in harness
    context.go diagnose.go compact.go …
  adapter/
    llm/openai/             OpenAI-compatible client (native + prompt-fallback tool calls,
                            prompt caching, error mapping, custom headers, retries)
    store/jsonl/            append-only JSONL event store
    tool/builtin/           the built-in tools (see §7) + OS sandbox wrappers
    platform/               Exec / ConfigDir / DataDir / TerminalCaps
    experience/git/         shared memory/skills store (git repo, D13)
    plugin/lua/             gopher-lua plugin host (capability bundles)
    mcp/                    MCP client: stdio + Streamable HTTP transports
    tui/                    Bubble Tea UI (transcript, subagent panes, /route editor)
  httpx/                    shared static+dynamic HTTP header set (MCP + LLM client)
  config/                   TOML config loader + comment-preserving editor (SetKey)
  eval/                     quantitative task-suite harness (success/steps/tokens)
  update/                   GitHub-release self-update (`-update`)
  version/                  build version stamping
```

---

## 2. Core data model (`core/session`, `core/event`)

A conversation is a `Session` of `Message`s; each message is a list of `Part`s
(tagged union by `Kind`: text | reasoning | tool-call | tool-result | image | error).
`ToolCall{CallID,Name,Args(json.RawMessage)}`, `ToolResult{CallID,Content(json.RawMessage),IsError}`.

Everything is an **`Event`** (CQRS-lite: commands in, events out):

```go
type Event struct { Seq int64; SessionID; Type; Actor; TS; Data json.RawMessage }
type Actor struct { Kind ActorKind; ID string } // user | agent | system
```

- **Facts** (persisted, JSONL, replayable): `session.created`, `prompt.submitted`,
  `part.appended`, `permission.decided`, `artifact.emitted`, `compaction`,
  `turn.finished`, `error`.
- **Transient** (bus only, never persisted): `part.delta`, `tool.started`,
  `tool.progress`, `permission.requested`, `agent.spawned`, `agent.status`,
  `context.usage`, `workflow.phase`.

Store path: `<dataDir>/projects/<cwd>/<sessionId>.jsonl`. `Store.Read(fromSeq)`
returns events with `Seq > fromSeq`. `Subscribe` = live bus first, then store
replay, deduped by seq (race-safe late-joiner).

---

## 3. Ports (`internal/port/port.go`)

- **`LLMProvider`**: `StreamChat(ctx, ChatRequest) (<-chan ProviderEvent, error)`.
  `ProviderEventType` ∈ text-delta | reasoning-delta | tool-call | finish | usage | error.
- **`Store`**: `Append/Read/ListSessions/Compact` (+ `ChildSessions` via the jsonl adapter).
- **`Tool`**: `Name/Description/Schema/Execute(ctx, args, ToolEnv)`. `ToolEnv` is the
  capability surface handed to a tool — note it is much larger than a plain fs env:

  ```go
  type ToolEnv struct {
    SessionID; Workdir; Platform
    AskPermission func(callID, name, args) (bool, error)
    EmitArtifact  func(Artifact)
    Spawn    func(ctx, SpawnRequest) SpawnResult     // task tool: run a subagent (blocking)
    Dispatch func(SpawnRequest) string               // task tool: background subagent; "" ok, else a note
    Ask      func(question) (string, error)          // subagent → orchestrator escalation (blocks; bg-dispatched only, else fast-fails)
    Report   func(summary, status, details) error    // subagent → final result; ends the turn
    SetTodos func([]Todo)                             // todowrite
    Propose  func(Contribution) error                // shared experience (D13)
    LoadSkill func(name) (string, bool)              // skill tool
    Sandbox  SandboxSpec                             // OS confinement for bash (read-only|workspace-write|full)
  }
  ```
- **`ExperienceStore`** (Retrieve/Propose), **`PluginHost`** (Load/Unload/Reload),
  **`Platform`** (Exec/ConfigDir/DataDir/TerminalCaps), **`ContextProvider`**, **`Scheduler`**.

---

## 4. The agent loop (`app/loop.go`)

`Submit` appends `prompt.submitted` and starts a single run goroutine (`startRun`);
`run` either drives the free-form loop (`runLoop`) or, when `Config.Workflow`, the
workflow engine (§6). `runLoop(ctx, session, agent, depth, maxSteps)`:

1. Assemble context (history since last compaction + project memory/AGENTS.md +
   skills + shared experience), publish `context.usage`, maybe auto-compact.
2. `StreamChat`; stream text (`part.delta`) / reasoning / tool calls; on finish
   persist the assistant `part.appended`.
3. No tool calls → finish branch (see §5/§6 gates), else execute tools (read-only
   tools run concurrently; writes/permissioned/task run sequentially) and loop.

Guards that make weak models safe (all in `loop.go`/`orchestrate.go`):
- **runGuard**: identical `(tool,args)` call blocked past `repeatLimit`; `blockedBudget`
  blocked repeats → `loop_guard` stop (long before MaxSteps).
- **no retry storm**: a terminal provider error ends the turn; `startRun` does NOT
  re-run a failed turn (only re-runs to pick up a user *steer*).
- **language lock** (`langDirective`): the user's script (Hangul/Kana/…) is detected
  and a "reply in X" directive is prepended to the system prompt (top-level only).

**Consensus council gate (D14, the signature — `runCouncilGate`).** When `Config.Council`
is set (ON by default; disable with `[council] enabled=false`), the finish branch (no tool calls, `depth==0`, not workflow
mode) does NOT finish immediately: it convenes a **council** that votes done-vs-continue.
- 3 default members (the MAGI): Melchior/correctness, Balthasar/verification,
  Casper/completeness — theme-name labels, lens attributes, configurable via `[council]`.
- Consensus is pure `core/council.Tally` (unanimous|majority|quorum:k|weighted:θ|veto);
  a tie/unmet-quorum/abstain-all/degenerate-param all resolve to **continue** (never
  finish unless affirmatively satisfied). Member fan-out is `adapter/council/llm` over an
  `LLMProvider`; a member that errors or returns unparseable output abstains.
- On **continue**, the aggregated feedback is injected as a `prompt.submitted`
  (reusing the Stop-hook injection path) and the loop runs again. On **done** (or the
  safety stops below) the turn finishes.
- Safety so the gate can't trap the loop: `CouncilMaxRounds` cap (default 3), a
  no-progress guard (empty/repeated feedback finishes), and a ctx-cancel early-out.
  Forced finishes are recorded as a `council.decided` with a `note` (not an error).
- Evidence judged = Task (the original goal, `firstUserText`) + Report (the agent's
  final message) + working diff (size-capped). Plan/Signals (D15/D16) are not yet wired.
- Events: `council.convened`/`council.verdict`/`council.decided` (fact) +
  `council.deliberating` (transient). See PLAN §4.2, DESIGN §5/§6, SPEC F-COUNCIL.

---

## 5. Multi-agent orchestration (`app/orchestrate.go`)

The orchestrator (top-level session, `Parent==""`) delegates via the **`task`** tool:
- **Background dispatch (sidecar)**: `Dispatch` spawns a subagent goroutine and
  returns immediately; its result is injected into the parent session when ready.
  The orchestrator stays responsive (can steer / interleave its own work).
- **`needsOrchestratorTurn`**: the orchestrator is re-invoked ONLY when there is
  something to act on — all subagents done, a user steer, or a subagent `ask` —
  NOT per individual subagent result. (This killed weak-model fabrication / re-dispatch.)
- **Re-dispatch dedup**: an identical `(agent+prompt)` already in flight is refused.
- **I/O contract**: input = the task prompt; **`ask`** = mid-task escalation routed
  through the orchestrator (blocks the subagent); **`report(summary,status,details)`**
  = the subagent's final result + status (done|blocked|failed), which ends its turn.
  A subagent that trails off without reporting gets one nudge to report.
- **`ask` requires an answerable parent**: escalation only works for **background-
  dispatched** subagents — the orchestrator stays in its loop and answers via
  `needsOrchestratorTurn`. A **synchronously-spawned** child (planner scout/parallel
  explorers, or a nested subagent's own `task` delegation) has a parent *blocked
  awaiting it*, so no one can answer. Such children are marked `Escalatable=false`
  (`SpawnRequest.Background` flows to `Session.Escalatable`); their `ask` **fails
  fast** with guidance ("proceed with your best assumption and note any ambiguity")
  instead of blocking until the 2-minute escalation timeout.
- **Supervisor**: per-attempt timeout, stall watchdog, bounded auto-restart.
- **Auto-orchestration** (`Config.AutoOrchestrate`): when context usage exceeds the
  threshold (default 60%, -1 to disable), the system injects a directive forcing the
  top-level agent into orchestration mode — decompose work and delegate to subagents.
  Fires once per session to prevent context overflow on complex tasks (SWE-bench style).
- **Pre-flight planner** (`Config.Planner`, `app/planner.go`, default on,
  `[orchestration] planner=false` to disable): before a top-level turn, a tool-free
  call to the `planner` agent judges whether the task splits into independent areas.
  If so the app fans out read-only explorers in parallel (`spawn`), then injects their
  combined findings before the main loop runs — proactive parallel *investigation*
  (the complement to reactive auto-orchestration). Defers implementation planning until
  after exploration; degrades to solo on any failure. Route it to a cheap backend with
  `[routing] planner = "fast"`. The decision is observable: it emits a `plan` phase
  (`workflow.phase` event) carrying the mode + reason, which the TUI shows as a header
  chip and a transcript line.

Default subagents (`cmd/magi/main.go:defaultAgents`): explore, locator, analyst,
architect, coder, tester, reviewer, planner — each with a restricted toolset (+ ask/report).

**Per-agent backend routing (M6+)**: `[routing] <agent>` selects a model on the
default backend, or names an `[llm.profiles.<name>]` (endpoint/key/model/headers) to
run that agent on a different backend. `App.providerFor(spec)` resolves the agent's
provider (the two `StreamChat` sites — the loop and compaction — use it); a runtime
override map (the `/route` editor) applies over config via `resolveAgentSpec`, used by
both `agentFor` (top-level) and `spawn` (subagents). Edits persist to `config.toml`
via `config.SetKey` (comment-preserving) through a `RoutePersister`.

---

## 6. Guardrails & workflow

**Guardrail policy (`app/policy.go`)** sits above interactive permission prompting:
- `Tool(spec)` allow/deny pattern rules (e.g. `Bash(git push:*)`, `Read(**/.env)`);
  secret paths are denied by default (hard floor).
- bash command scan: destructive / pipe-to-shell / network-egress / secret-path →
  forces a prompt (or deny). Optional egress host allowlist.
- **Profiles** = 2 axes: Permission (ask|auto|allow|deny) × Sandbox
  (read-only|workspace-write|full), presets `safe`/`standard`/`yolo`.
- **OS sandbox** for bash (`adapter/tool/builtin/sandbox_{darwin,linux,windows,other}.go`):
  macOS seatbelt, Linux bwrap, Windows restricted-token (stage 1), with graceful
  fallback when the backend is unavailable. Opt-in via profile.
- **Prompt-injection rule**: tool output is treated as untrusted data; webfetch
  output is fenced.

**Workflow engine (`app/workflow.go`, opt-in via `-workflow`)** drives a task through
a deterministic, code-enforced pipeline so the *flow* doesn't depend on the model:
`localize` (read-only) → `implement` (edit) → `verify` (bash/real command) →
`review` (read-only) → `summarize`. Each phase runs with a restricted toolset; gates:
implement must actually edit files (else re-prompt), and a verification command
(configured `-verify-cmd` or auto-detected per build system) must pass — looping
implement↔verify up to `WorkflowMaxLoops`. Emits `workflow.phase` events.

---

## 7. Tools (`adapter/tool/builtin`)

Built-ins (registered in `builtin.Default()`): `read`, `write`, `edit`, `multiedit`,
`grep`, `glob`, `list`, `bash`, `bash_output`, `bash_kill`, `todowrite`, `webfetch`,
`websearch`, `remember`, `skill`, `findcontext`, `astgrep`, `lsp_diagnostics`,
`lsp_definition`, `lsp_references`, `lsp_symbols`. Orchestration tools (registered in
`main.go`): `task`, `ask`, `report`.

Background commands: `bash` with `background=true` starts a detached process
(registry in `bgproc.go`) and returns an id; `bash_output` polls new output, `bash_kill`
stops it. LSP navigation uses the gopls CLI for Go and a minimal stdio JSON-RPC client
(`lspclient.go`) for other languages (typescript-language-server, pyright,
rust-analyzer, clangd), degrading gracefully when a server is absent. `websearch`
uses DuckDuckGo by default, or Brave/Tavily when `BRAVE_API_KEY`/`TAVILY_API_KEY` is set.

Notes: file tools are jailed to the workdir (`pathutil.go:resolvePath`); `read`
recovers imprecise paths by basename; `edit` matches exact → line-ending-normalized
→ trailing-whitespace-tolerant (leading indentation never guessed); `findcontext`
ranks by symbol definition + path + content coverage; `astgrep` is structural
(AST) search via the external `ast-grep` CLI (shells out, no CGO) and degrades to
a "use grep/findcontext" message when the binary is absent; `lsp_diagnostics` runs
gopls check for LSP diagnostics (type errors, unused vars) and degrades to a "use
go build/test" suggestion when gopls is not installed.

**Add a tool**: implement `port.Tool`, register it in `builtin.Default()` (or via a
plugin/MCP). For role-scoped tools, `toolSpecs` filters `ask`/`report` to subagents
and `task` to the orchestrator.

---

## 8. LLM adapter (`adapter/llm/openai`)

One OpenAI-compatible client covers Ollama / LiteLLM / vLLM / OpenAI by base URL.
- **Tool calls**: native `tool_calls` accumulation (args reduced to the first JSON
  value to survive duplicate-arg backends) + a prompt-based fallback for models
  without native support.
- **Prompt caching** (on by default, `-no-cache` to disable): `cache_control:
  ephemeral` on the system prompt + tool list; auto-falls back to plain on a 400/422
  and sticks to plain for the session (safe for non-Anthropic backends).
- **Errors**: status mapped to a cause (`describeStatus`: 401 auth, 404 model/endpoint,
  429 rate-limit, 502/503 gateway, 504 upstream timeout).
- **Resilience**: bounded retries on 429/5xx with Retry-After; `-http-timeout` bounds
  time-to-first-header without cutting the token stream.
- `ListModels` (`-list-models`) fetches the backend `/v1/models` catalog.

---

## 9. CLI & config

Flags (`cmd/magi/main.go`), each with a `MAGI_*` env equivalent:
`-p` (headless), `-output text|json`, `-model`, `-base-url`, `-permission`
(ask|auto|allow|deny), `-profile` (safe|standard|yolo), `-workflow`, `-verify-cmd`,
`-no-cache`, `-http-timeout`, `-plugins`, `-list-models`, `-theme`, `-no-harness`,
`-update`, `-version`. API key via `MAGI_API_KEY` (or `OPENAI_API_KEY`).

Config: global `<configDir>/config.toml` + project `.magi/config.toml` (committable;
project scalars override, hooks/rules append). Keys: model, base_url, permission,
profile, sandbox, allow/deny (rules), allow_domains, hooks, mcp, routing,
experience_dir.

---

## 10. Build, test, run

```
make build           # go build ./...
make test            # go test ./...           (E2E + eval auto-skip if backends unreachable)
make test-race       # go test ./... -race
make vet / make fmt
make snapshot        # goreleaser --snapshot (local cross-compile)
```

- **Unit/deterministic tests** use a fake `LLMProvider` (no model needed) — the
  bulk of `internal/app` and `internal/adapter/...` tests.
- **Real-model E2E** (`Test*E2E*`) hit a live backend, gated by env and auto-skipped
  when unreachable:
  `MAGI_E2E_OLLAMA_BASE`, `MAGI_E2E_OLLAMA_MODEL`, `MAGI_E2E_API_KEY`.
- **Eval harness** (`internal/eval`): `MAGI_EVAL_BASE/_MODEL/_KEY` → `go test -run
  TestEvalSuite ./internal/eval -v` prints a scored table (cross-model comparison).
- CI (`.github/workflows/ci.yml`) runs build/vet/test on ubuntu+macos+windows
  (fail-fast off); release (`release.yml`) runs goreleaser on `v*` tags.

Weak local models are the central reliability constraint: prefer the deterministic
fake-LLM tests for regression coverage; use real-model E2E for gated confirmation.

---

## 11. Extension points

> 실전 단계별 가이드(MCP 서버 추가, 공유 경험 부트스트랩): [`EXTENDING.md`](EXTENDING.md).

- **Lua plugins** (`adapter/plugin/lua`, `-plugins <dir>`): capability bundles
  (tools/hooks), hot-reloadable. NOT for transport-level concerns (auth/TLS).
- **MCP** (`adapter/mcp`, `config.toml [mcp]`): external tool servers over stdio.
- **Hooks** (`config.toml [[hooks]]`): PreToolUse/PostToolUse/Stop shell commands
  (POSIX shell; not available on Windows).
- **Orchestration policy**: the primitives (task/ask/report/supervisor) are in core;
  a multi-role orchestration choreography is intended to be a swappable policy (the
  bundled default agents are the current policy).
- **Auth** (planned): custom auth (OIDC/mTLS/rotating tokens) belongs at the Go
  `http.RoundTripper` seam (`openai.WithHTTPClient`), not in Lua.
