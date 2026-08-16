# magi — Architecture

[English](ARCHITECTURE.md) · [한국어](ARCHITECTURE.ko.md) · [↑ Docs](README.md)

> **Current reference.** The as-built system, and the one that wins when the design documents disagree with it.

This is the **as-built** reference for developing on magi. `DESIGN.md` and `SPEC.md` are the
original design intent (kept for rationale, decisions D1–D13); where they disagree with this file,
**this file wins**.
Visual companion: [DIAGRAMS.md](DIAGRAMS.md) — one axis from the top-level container down to
**class diagrams**: L0 process boundary, L1 turn lifecycle, L2 component map, L3–L4 the
nudge/gate and model-I/O guard flows, L5 core domain types, L6 ports → adapters, L7 the
`internal/app` structs, L8 the tool layer, L9 one tool call as a sequence. All mermaid.

magi is an extensible terminal AI coding agent: a Go core, a Bubble Tea TUI, Lua plugins,
OpenAI-compatible LLM access (Ollama/LiteLLM/etc.), an event-sourced store, guardrails, an
advisory council the agent calls as a tool, and an opt-in deterministic workflow engine. Single
static binary (`CGO_ENABLED=0`), cross-platform.

**One agent by default.** magi used to spawn subagents and hand them slices of the work through a
curated brief. Nothing in the recorded runs shows that making a result better, and the defect log is
largely made of it — a brief paraphrased until the graded identifier was gone, a worker that never
received the checklist assembled for it, a killed explorer whose session id was dropped so its work
could not be salvaged. It is gone, along with the planner that decided when to use it.

What is NOT gone, and was added back deliberately, is the seam: a plugin can declare a subagent and
a user can switch it on (`/subagents`, EXTENDING §3.9). The distinction is the whole point. Every
defect in that list came from magi deciding **on the model's behalf** what to split off and what to
pass along; the seam decides neither. A plugin author writes the prompt, the tool's own arguments
are the brief, and magi passes them through without rewriting a byte. magi still ships no agent —
`plugins/seele` is one example, and it ships switched off.

---

## 1. Layering (hexagonal / ports & adapters)

Dependency rule, enforced at compile time: **`adapter → app → core`**, and
`app`/`adapter` depend on `port`. `core` imports nothing outside std + core.

```
cmd/magi/                 entrypoint: flag parsing, DI wiring, -p headless, TUI launch,
                          -daemon (engine with no UI) / -attach (a UI onto one) / -agents
cmd/magi-web/             the console: a read-mostly web view of every daemon on the machine,
                          and of other consoles' (peer.go). page.go is the whole front end
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
  app/                      application service + the agent loop + guardrails + workflow
    app.go                  App (the Application): commands in → events out; session/turn state
    routing.go query.go     model/profile routing and permission config (routing.go); the
                            read-only query surface the TUI reads — transcript, plan, observation,
                            git-diff/shell (query.go)
    config.go               Config/AgentSpec/profile types, withDefaults, applyProfile
    todos.go                the plan the AGENT keeps (todowrite) and its finalize-at-turn-end
    loop.go                 runLoop: the agent loop; buildStepSystem (the cacheable prompt); the
                            per-step stream/persist/finish flow
    loop_gates.go           the finish path: Stop hooks, the empty-result nudge, the
                            authored-but-never-run nudge, and the finish declaration
    interject.go interject_queue.go
                            mid-turn steer machinery: routing (applyInterjectRoute), the
                            finish-boundary triage mini-turn, and the queue that survives
                            a reload
    guard.go shellcmd.go shellparse.go
                            runGuard — what magi NOTICES about a run (repeats, stalls,
                            self-reverts, no-change writes, exercise churn) and the stateless
                            shell classifier it reads commands with. It reports; it does not
                            decide (see §4)
    observed.go observed_view.go world_snapshot.go
                            magi's own record: which calls it granted, how they really ended,
                            what they wrote (observed.go); the panel's view of it
                            (observed_view.go); and the fresh read of the workspace and the live
                            background jobs taken when a finish is declared (world_snapshot.go)
    council_advice.go council_events.go council_evidence.go
                            the council as a TOOL — one deliberation, three lenses over the same
                            record, rendered back to the agent; the events the TUI folds; and the
                            evidence assembly they read
    execute.go permission.go prompt.go
                            tool execution (including the unknown-argument check), permission
                            prompting, and prompt/system assembly
    hooks.go                lifecycle hooks (PreToolUse/PostToolUse/Stop) + the built-in harness
    workflow.go             the opt-in deterministic phase pipeline (-workflow, §6)
    policy.go               guardrail policy engine (rules, secret-deny, bash scan, egress)
    background.go           the background-command registry as an observer sees it (§7)
    compact.go recall.go reconstruct.go scratch.go …
  adapter/
    llm/openai/             OpenAI-compatible client (native + prompt-fallback tool calls,
                            prompt caching, error mapping, custom headers, retries)
    store/jsonl/            append-only JSONL event store
    tool/builtin/           the built-in tools (see §7) + OS sandbox wrappers
    platform/               Exec / ConfigDir / DataDir / TerminalCaps
    experience/git/         shared memory/skills store (git repo, D13)
    plugin/lua/             gopher-lua plugin host (capability bundles)
    mcp/                    MCP client: stdio + Streamable HTTP transports
    daemon/                 the engine over a unix socket: Listen/Serve, the flock claim that
                            makes a workspace's daemon unique, Publish (the record a console
                            reads), Client, and the optional interfaces an engine may satisfy —
                            Controller (rewind, compact, set-model, set-permission), JobRunner
                            (what is running beside the turn, and what is queued behind it),
                            ToolLister and ModelLister (the roster and the catalogue, which only
                            the process holding the run can say), UserNamer. A daemon that does
                            not implement one answers empty rather than erroring, and every
                            caller has to read that as "not known from here" rather than as "none".
    fleet/                  what every magi on this machine is doing, derived from the logs and
                            a short parallel probe of each socket — ONE derivation, because the
                            console and `--agents` both ask it
    tui/                    Bubble Tea UI, split by concern: model.go (Model + Update),
                            model_input.go (mouse/key/slash), model_event.go (event folding),
                            model_route.go (route/profile forms), model_layout.go (resize/panes),
                            model_view.go (render). Transcript, background-job panes, /route editor
                            (session-model suggest box = profiles ∪ `App.ListModels` gateway catalog).
  atomicfile/               one write-temp-then-rename, shared by everything that writes a file
                            a concurrent reader may be reading (the experience store, config)
  httpx/                    shared static+dynamic HTTP header set (MCP + LLM client)
  jsonx/                    the one reader for model-produced JSON: balanced-span extraction,
                            the repair ladder, tolerant field types, and parse-failure diagnosis
  config/                   TOML config loader + comment-preserving editor (SetKey)
  eval/                     quantitative task-suite harness (success/steps/tokens)
  update/                   GitHub-release self-update (`-update`)
  version/                  build version stamping
```

**Reading model JSON (`internal/jsonx`).** Council verdicts and tool-call arguments are JSON a
MODEL wrote, and they fail in the same handful of ways. One package owns that, so a defect fixed once
is fixed for all of them:
- **Extraction** — `BalancedObjects`/`BalancedArrays` pull candidate spans out of a reply that
  wrapped its JSON in prose or a code fence.
- **Repair ladder** (`RepairCandidates` → `Unmarshal`) — the ORIGINAL is always candidate 0, so a
  clean reply is never rewritten. Then light repairs (a trailing comma, a raw control character in
  a string — the common defects in fields carrying multi-line prose or shell commands), then
  structural ones (an unescaped inner quote, single-quoted strings, bare identifier values). Each
  structural repair acts only on text that is ALREADY invalid JSON, so it cannot corrupt a
  well-formed document.
- **Tolerant field types** (`Text`, `Texts`, `Number`) — Go's decoder aborts the WHOLE document on
  the first type mismatch, so one field answered in an unexpected shape (a list where the schema
  says string, a quoted `"0.9"`, a number where prose was asked for) discarded every sibling field
  and every element beside it. Model-facing structs read their free-text fields through these
  instead, and validate by VALUE afterwards.
- **Diagnosis** (`Diagnose`, `Report`) — what every parse-failure log renders: the bounded excerpt
  plus the named reason (no JSON at all / a syntax defect with its byte offset and a window around
  it / it parses and the mismatch is the schema). An excerpt alone keeps only the head and tail,
  which is where the defect usually is not.

---

## 2. Core data model (`core/session`, `core/event`)

A conversation is a `Session` of `Message`s; each message is a list of `Part`s
(tagged union by `Kind`: text | reasoning | tool-call | tool-result | image | error).
`ToolCall{CallID,Name,Args(json.RawMessage)}`, `ToolResult{CallID,Content(json.RawMessage),IsError}`.

Everything is an **`Event`** (CQRS-lite: commands in, events out):

```go
type Event struct {
	Seq       int64             // per-session, assigned by the Store on append; 0 = transient
	SessionID session.SessionID
	Type      Type
	Actor     Actor
	TS        time.Time
	Stage     string          // plan|execute|council|finalize (D15); empty on older logs
	Data      json.RawMessage // payload struct per Type, in event.go
}
type Actor struct { Kind ActorKind; ID string } // user | agent | system
```

The `Actor.Kind` is load-bearing, not decoration: several scans use `ActorUser` as the
**turn boundary**, and the system actors (`loop`, `orchestrator`, `hook`, `plugin`,
permission) are deliberately not it — a nudge magi injected must not read as the user
starting a new turn.

- **Facts** (persisted, JSONL, replayable) — `event.go`'s first const block:
  `session.created`, `prompt.submitted`, `part.appended`, `permission.decided`,
  `artifact.emitted`, `compaction`, `turn.finished`, `todos.changed`, `error`,
  `diagnostic`, `council.convened`, `council.verdict`, `council.decided`,
  `interjection.deferred`, `prompt.abandoned`.
  Two of them are persisted but **never reconstructed into a message**, so they are
  auditable without entering the model's context: `diagnostic` (a raw input a side call
  recovered from) and `prompt.abandoned` (a cancelled turn's seed, read by `seedPromptIdx`).
- **Transient** (bus only, never persisted): `part.delta`, `tool.started`,
  `tool.progress`, `permission.requested`, `question.requested`, `context.usage`,
  `workflow.phase`, `council.deliberating`, `model.changed`, `user.label.changed`.
  The set is enumerated once (`transientTypes`) rather than re-listed per call site.

Store path: `<dataDir>/projects/<cwd>/<sessionId>.jsonl`. `Store.Read(fromSeq)`
returns events with `Seq > fromSeq`. `Subscribe` = live bus first, then store
replay, deduped by seq (race-safe late-joiner).

---

## 3. Ports (`internal/port/port.go`)

- **`LLMProvider`**: `StreamChat(ctx, ChatRequest) (<-chan ProviderEvent, error)`.
  `ProviderEventType` ∈ text-delta | reasoning-delta | tool-call | finish | usage | error.
- **`Store`**: `Append/Read/ListSessions/ChildSessions/Compact/Truncate`. `Compact`
  rewrites the log up to a seq behind one snapshot event; `Truncate` drops it.
- **`Tool`**: `Name/Description/Schema/Execute(ctx, args, ToolEnv)`. `ToolEnv` is the
  capability surface handed to a tool — note it is much larger than a plain fs env.
  A tool reaches the application **only** through these closures, which is why a nil
  field means "this capability is not available in this run" and every tool nil-checks
  before calling:

  ```go
  type ToolEnv struct {
    SessionID  session.SessionID
    Workdir    string          // the session's working directory
    ScratchDir string          // the turn's scratch dir (created at depth 0)
    ScratchTmp string          // TMPDIR handed to child processes
    Platform   Platform

    AskPermission func(callID, name string, args json.RawMessage) (bool, error)
    EmitArtifact  func(artifact.Artifact)              // reviewable output (D11)
    EmitProgress  func(text string)                    // live note while a tool blocks (wait_for)

    Council func(ctx, question string, complete bool) (string, error) // complete=declare finished
    AskUser func(question string, options []string) (string, error)   // interactive only; nil ⇒ tool says so
    RouteInterjection func(action, reason, requestID string) error     // top level only

    SetTodos     func([]session.Todo)                  // todowrite
    NoteForTurn  func(text string) error               // remember{scope:"turn"}; err = NOT kept
    Propose      func(Contribution) error              // shared experience (D13)
    LoadSkill    func(name string) (string, bool)      // skill
    Recall       func(query string) (string, error)    // recall_context — THIS session's compacted detail
    RecallMemory func(query string) (string, error)    // recall_memory — the cross-session D13 store

    Sandbox SandboxSpec // OS confinement for bash (read-only|workspace-write|full)
  }
  ```

  Two conventions in that list are worth stating, because both were once broken:
  `NoteForTurn` returns an **error** rather than nothing, so the tool cannot answer
  "noted" on a note the bounded queue discarded; and `Recall` / `RecallMemory` are
  different stores — one recovers what a compaction shed from *this* session, the other
  reaches durable team memory.
- **`ExperienceStore`** (Retrieve/Propose), **`PluginHost`** (Load/Unload/Reload/Capabilities),
  **`Platform`** (Exec/ConfigDir/DataDir/TerminalCaps/ProcessCPUTime), **`ContextProvider`**,
  **`Council`** (Deliberate), **`ToolRegistry`**, **`DoctorProbe`**, **`PluginCommand`**,
  **`Scheduler`**.

`ToolEnv` used to carry two more fields — `Ask` (a subagent escalating to its
orchestrator) and `Report` (a subagent's structured final result, `port.ReportInput`).
Nothing set them and no tool read them once the one-agent change landed; a port that
advertises a contract the application never fulfils is how a reader — or a model reading
the tool surface — learns something untrue about the system. They are gone.

It carries four fields for the spawn seam instead, and each is `nil` unless the host offers it:
`Spawn` runs a child to completion, `ChildSteps` returns what that child actually did, and
`RestoreChild` puts its file changes back. All three are scoped to the tool call in flight — a call
can only read or restore a child it started — and `Spawn` is `nil` **inside** a child, which is what
makes recursion impossible by construction rather than bounded by a counter someone has to check.

---

## 4. The agent loop (`app/loop.go`)

`Submit` appends `prompt.submitted` and starts one run goroutine (`startRun`); `run` drives
either the free-form loop (`runLoop`) or, under `Config.Workflow`, the phase engine (§6).

The loop is deliberately small. It used to be a pipeline of stages magi imposed — orient,
spec-mine, a contract council, a planner, a plan audit, check authoring, coverage fill,
delegation to subagents, a termination vote. Every one of those decided something *before* the
work existed, and every recorded defect of that period was of one kind: magi believing a
judgement it had made in advance over the record of what actually happened. They are gone.

`runLoop` still takes a `depth`, and every `depth > 0` branch in it — interjection detection,
`route_interjection`, `ask_user`, the top-level contract reset, the council finish gate — is written
for a child. Those branches are reached only when a plugin spawns one; nothing in the tree does.

What runs now, per step:

1. **Assemble** the request: history since the last compaction, project memory (AGENTS.md),
   skills, shared experience. Then an ephemeral **volatileContext** — never part of the cached
   system prompt — carrying the agent's own todo list, a self-measured elapsed line once a turn
   passes a minute, an optional `--time-budget` remainder, push-side recall hints for topics a
   compaction shed, and **the run state**: magi's own record, re-rendered every step (`runState`
   in `world_snapshot.go`) — which commands it granted, how they really ended, which paths they
   wrote, and which background commands are still alive. A screen-driven agent re-reads its
   terminal before every decision; this is the same refresh over the store magi actually keeps.
2. **Stream** one model response: text (`part.delta`), reasoning, tool calls; persist the
   assistant message. Two recoveries belong to getting it — a context too large to send, and a
   backend that goes silent (`generate_step.go`).
3. **Tool calls** → execute (read-only concurrently; writes and permissioned calls sequentially)
   and loop. **No tool calls** → the finish path (§5).

There is **no pacing ceiling — only a runaway backstop**. A turn ends when the agent declares it
finished and the council accepts, when the model stops and the finish path lets it, when the
context is cancelled, or when whoever launched magi stops waiting. The guards that used to stop a
run on magi's own arithmetic came out on measurement: across every recorded trial, runs that
reached the external deadline were still scored and 76 of 396 passed, while 28 runs magi stopped
itself produced no pass at all — and 8 were never scored, because a nonzero exit reads to the
caller as "the agent failed to run" rather than "the agent decided to stop". What remains is
`MaxSteps` (default 240), sized far above any productive turn, so a genuinely runaway loop cannot
hold a daemon forever; a top-level turn that spends it lands with a persisted `turn.finished`
marked UNVERIFIED, naming the backstop — the work stands as it was left, and nothing reads the
session as still working. A workflow PHASE is different in kind: it declares its own budget as
part of the pipeline's shape, and spending it is ordinary.

### What the guard does now (`guard.go`)

The guard **reports**; it does not decide. It used to force-stop a run on its own reading of a
repeat/stall/idle/spin counter, and that stop bought nothing (the measurement above). Every signal
it collected is still collected, and still SAID — to the agent, as a nudge it can act on or
ignore:

- **Repeat**: an identical `(tool,args)` call is counted — reads (fingerprint drops `limit`, so
  head re-reads collapse while genuine paging by `offset` does not), inspect-only bash, identical
  write replays. Exec bash is exempt: its outcome can change through state the guard cannot see.
- **Self-revert** (`noteEdit`): each touched file's content is hashed across the turn. A write that
  returns a file to a state it already held this turn is churn, not progress — progress is
  retracted, and **every swing is reported**, the later ones carrying how many there have been and
  how many versions the file is cycling among. A write that changes nothing at all says so: the
  tool answers "wrote N bytes", which reads as a change.
- **Exercise ledger**: an exercising command that NAMES an authored file marks it exercised — by
  filename, or by module stem for the languages that load a source file that way (`from run import
  …` is a real invocation of `run.py`). What it cannot match, it says it cannot match: the finish
  path's nudge states that no command *naming* the file ran, which is what the record holds, not a
  verdict on the work.
- **Exercise churn**: when the agent's OWN build or test keeps failing across repeated edits
  without converging, the turn lands UNVERIFIED with the work standing rather than churning to an
  external kill that tears a live deliverable down. It reads only magi's own signals — no external
  clock.

## 5. Finishing a turn (`app/loop_gates.go`, `app/council_advice.go`)

A turn ends because someone decided to end it. Going quiet is not a decision: a turn that trailed
off mid-thought and one that was actually finished used to end identically, and neither was ever
asked which it was.

The finish path, in order:

1. **Stop hooks** (`hooks.go`) — the workspace's own procedure. A failing hook pushes the agent
   back to work with the hook's output.
2. **Empty result** — an answer with no text delivered nothing a reader can use; nudged once.
3. **Authored but never run** — the turn wrote something runnable and magi's record holds no
   command naming it. Deterministic, no model call, once per turn.
4. **The declaration** — a working turn that stopped without declaring is told how to: call the
   `council` tool with `complete: true`. Bounded at three asks; past that the work lands and the
   turn is recorded as ending *undeclared*, which is a different claim from ending declared.

### The council is a tool

It used to convene by itself at the finish boundary, which decided two things it could not get
right: WHEN it was asked — at the one moment the agent had already made up its mind — and whether
its answer would be read at all. In a headless run it was not: the advice was injected and
`turn.finished` was written in the same tick.

- **`council{question}`** — three members read the SAME record through different lenses
  (correctness, verification, completeness) and answer in their own words. The tally is not
  rendered: counting votes is what the gate did, and a count invites reading a majority as an
  order. Advice; the agent may disagree.
- **`council{complete: true}`** — the agent DECLARES the task finished. The members read the
  record as a finish and either accept (the loop is signalled, the turn ends through the same
  finish path as any other) or hand back what is undone, and the agent keeps working.

What the members see on a declaration is a **fresh read, not a replay**. magi's record answers
"what happened" and cannot answer "what is there now" — a build's own output, a shell redirect it
could not parse, a file a later command removed. So the declaration carries the workspace as it
stands (files modified since the task began, directories that churned collapsed to a count), the
background commands still alive, and the one contradiction a record can never surface on its own:
paths the record says were written that are not on disk.

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
- **Persist-rule narrowing** (`persistRule`): choosing "always (project)" writes an
  allow rule scoped as tightly as the tool permits. Non-bash tools persist as
  `tool(**)`; `bash` persists only the approved **program name** — `curl https://x`
  yields `bash(curl:*)`, not `bash(**)` — via `safeCommandPrefix` (first argv word;
  empty, so no persist, when the command opens with a shell metachar and has no fixed
  program to pin). One approval can't silently pre-authorize every later command.

**Workflow engine (`app/workflow.go`, opt-in via `-workflow`)** drives a task through
a deterministic, code-enforced pipeline so the *flow* doesn't depend on the model:
`localize` (read-only) → `implement` (edit) → `verify` (bash/real command) →
`review` (read-only) → `summarize`. Each phase runs with a restricted toolset; gates:
implement must actually edit files (else re-prompt), and a verification command
(configured `-verify-cmd` or auto-detected per build system) must pass — looping
implement↔verify up to `WorkflowMaxLoops`. Emits `workflow.phase` events.

---

## 7. Tools (`adapter/tool/builtin`)

Built-ins (`builtin.Default()`): `read`, `write`, `edit`, `multiedit`, `grep`, `glob`, `list`,
`bash`, `bash_output`, `bash_kill`, `bash_input`, `wait_for`, `port_owner`, `todowrite`,
`council`, `webfetch`, `websearch`, `remember`, `skill`, `recall_context`, `recall_memory`.
Added by `builtin.RegisterOrchestration(r, headless)` for interactive runs only: `ask_user`,
`route_interjection`. It sits beside `Default` rather than at each call site because a hand-kept
second copy cannot fail a build when it falls behind — and one had, by two tools, before the
function existed.

A tool earns its place only when it gives magi something bash cannot, or gives the model something
bash cannot. Counted across every recorded bench run, the tools that failed both came out:

| removed | calls in the record | what the model reached for instead |
|---|---|---|
| `tabulate` `countmatches` `countlines` `groupby` | 0 | `wc -l`, `grep -c`, `sort \| uniq -c` |
| `findcontext` | 0 | `grep`, `glob` |
| `lsp`, `lsp_diagnostics` | 0 | `grep`, and the compiler |
| `astgrep` | 2 | `grep` |
| `replan` | 1 | nothing — see below |

59% of recorded bash calls contain a pipe, so a tool that merely competes with a pipe loses. What
stayed and why: `write`/`edit`/`multiedit` because the change tracking, the self-revert check and
the council's evidence hang off them; `read` for its line gutter, paging and non-text formats;
`bash_input` because nothing else can write to a running process's stdin; `wait_for` and
`port_owner` because they answer where `sleep`-polling and `ss`/`lsof` are absent or guard-tripping.

`replan` was the hard case, because it did do something bash cannot: it cleared the stall guard's
no-progress count so an agent that had deliberately changed course was not force-stopped for the
abandoned approach's spinning. It came out anyway. The guard already treats a **novel exercising
command or a mutation** as forward motion, so an agent that genuinely pivots re-arms it by acting —
deterministically, without having to know a tool exists. What the tool added on top was a name
promising something nobody does (a re-plan), an anti-abuse budget policing a tool called once, and
a whole sterile-replan landing path fed only by that budget.

The **LSP pool stays** even though both LSP tools went. It is what runs the automatic post-edit
diagnostics (`app/diagnose.go` → `builtin.AutoDiagnose`), which fire without the model asking.

Background commands: `bash` with `background=true` starts a detached process
(registry in `bgproc.go`) and returns an id; `bash_output` polls new output, `bash_kill`
stops it. **`port_owner`** (`portowner.go`) finds which process is bound to a TCP port by
scanning `/proc/net/tcp{,6}` + `/proc/<pid>/fd` and can kill it — a portable way to free a
port squatted by a stale/leftover server when `pkill`/`lsof`/`ss`/`fuser` are absent
(exit 127) in a stripped container (Linux only; a stub reports unsupported elsewhere).
Post-edit diagnostics use the gopls CLI for Go and a minimal stdio JSON-RPC client
(`lspclient.go`) for other languages (typescript-language-server, pyright,
rust-analyzer, clangd), degrading gracefully when a server is absent. `websearch`
uses DuckDuckGo by default, or Brave/Tavily when `BRAVE_API_KEY`/`TAVILY_API_KEY` is set.

Notes: file tools are jailed to the workdir (`pathutil.go:resolvePath`); `read`
recovers imprecise paths by basename and prefixes each line with `N⇥` — the 1-based
number and a tab, cat -n style — so the gutter reads as metadata rather than as file
content and a later edit can address a line by number. `edit` takes **either** a text
match (`old`/`new`: exact → line-ending-normalized → trailing-whitespace-tolerant,
leading indentation never guessed, plus a salvage tier that strips a pasted read
gutter before retrying) **or** an **anchor** (`at:"N"`, optional `to:` for a line
range). `write`/`edit`/`multiedit` additionally
append a **non-blocking advisory** when freshly added comments read like
change-narration ("// I've updated the loop …") or placeholders/elisions
("// rest of the code unchanged", "// …") — comments should capture non-obvious
intent, not narrate the diff; the edit still applies. After a file modification magi runs
**diagnostics itself** and feeds the result back: gofmt/`go vet` for Go, `py_compile` for Python,
and for every other language the file is opened in its language server and the pushed
`textDocument/publishDiagnostics` is read — errors and warnings only, degrading to a "build/run the
project" suggestion when no server is installed.

**bash is bash.** `/bin/sh` is dash on Debian/Ubuntu images, where the bash a model writes
everywhere else — `[[ ]]`, `source`, arrays — is a syntax error that belongs to the shell choice
rather than to the work. magi runs `/bin/bash` when the machine has it. Nothing else changes: no
`pipefail`, no `errexit`, because the agent reads the exit status to decide what to do next and a
shell that quietly redefines it would be lying. What magi does instead is read **PIPESTATUS out of
band** — written to a side file the command never touches — so `make … | tail` reporting 0 comes
back annotated with which stage actually failed, and the observation record files it as FAILED
rather than as a status it could not determine.

**Add a tool**: implement `port.Tool` and register it in `builtin.Default()` (or ship it from a
plugin/MCP).

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
`-update`, `-version`, `-doctor`, `-time-budget`, and the three that outlive one terminal:
`-daemon`, `-attach`, `-agents`, `-join` (§11). API key via `MAGI_API_KEY` (or `OPENAI_API_KEY`).

`cmd/magi-web` has its own small set — `-addr`, `-config-dir`, `-workdir`, `-peer name=url`
(repeatable), `-version`, and `-emit-demo <dir>` (write the page as a static site answered by a
mock, for the Pages workflow) — and no config file: a console's peers are an operator's decision, and
reading them from a file magi itself writes would make them reachable by anything that can write
one.

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

## 11. Beyond one terminal — daemon, fleet, console

Three pieces, each a thin layer over what already existed. None of them is a service: there is no
scheduler, no registry, no second copy of the engine, and no state of their own.

```
magi -daemon          the App, no UI, listening on <config>/daemon-<dir>-<hash>.sock
  ├── magi -attach    a TUI that joins the daemon's session; five calls go over the socket,
  │                   everything else it answers itself from the same store
  ├── magi --agents   one line per daemon (fleet.List)
  └── magi-web        the console (fleet.ListCached + the same socket calls)
        └── -peer     another magi-web, merged into the same list
```

- **The socket is named from the workspace**, symlinks resolved, so "the daemon here" is
  well-defined and `--attach` cannot reach a neighbour's. `Listen` claims it with a flock BEFORE
  publishing: the split exists because publishing first let two simultaneous starts overwrite each
  other's record and then delete the winner's on the way out.
- **Fleet state is derived, never recorded.** What an agent is doing, whether it is waiting on a
  permission, when it last moved, whether a turn is unfinished, what a person had to say mid-turn —
  all of it comes out of the event log plus one 700ms parallel probe per socket. Nothing writes a
  status file, so nothing can be stale, and the answer is the same for a session that ran last week.
  `fleet.Cache` keeps only the one expensive part (the last line an idle agent said) and only while
  its sequence number is unchanged.
- **The console reads; the daemons act.** `magi-web` builds its own `app.App` over the same store
  with **no LLM and no tools** — it cannot run a turn even by mistake. Everything that changes a run
  (submit, steer, interrupt, answer a permission, forget) goes to the daemon that owns it.
- **Federation is composition.** A console that watches several machines is a console that reads
  several consoles: `/fleet` and `/skills` are the wire format, and actions are
  forwarded with only the method, path, target socket and form body copied. Peer URLs come from the
  operator, never from a page or another peer's response — the same rule the `?d=` allowlist follows
  one layer down.
- **A team is addressing, not topology** (`internal/adapter/tool/companion`, MANUAL §13). Companions
  publish a name, a role and optionally a team into the same records the fleet already reads, so
  membership needs no registry: the directory is it. The tools on top of that list the others (with
  what each has learned, which is what makes a specialist), ask one of them what it can do, hand a
  piece of work over, and record what an answer was worth. **Nothing ranks candidates** — the model
  chooses, the tool refuses anything ambiguous, and the record is shown as a tally rather than an
  order. Registered by `cmd/magi`, not in builtin: `app` imports builtin and `daemon` imports `app`,
  so a built-in that read daemon records would close a cycle.
- **A team address resolves to the lightest member, hub on a tie.** The hub is elected from who is
  actually there rather than read off a config flag, so a team stops being addressable when everyone
  in it stops and never because nobody typed a word. It used to always resolve to the hub, which
  made a team of three behave as one queue — nothing can pass handed-over work on, so it all piled
  up behind whoever had been elected.
- **Depth is bounded by shape, not by a counter.** Work handed to a companion cannot be handed on.
  The rule is read off a label in the transcript, which survives restarts, attaches and resumes.
- **Conversations are isolated; the workspace is not — unless the work cannot write.** Each asker
  gets a side session, so nobody's request lands in the conversation a person is having — and one
  turn runs at a time per workspace, the person's included, because two turns in one tree are two
  writers with nothing between them. That rule is about WRITING, and applying it to everything cost
  an answer that could have been given at once: a companion asked "what does your README say" sat
  behind somebody's build. So `hand_off` takes `looking`, the asker's statement that this is a
  question; the receiver opens the side session in the **looking role**, whose tools are fixed at
  the four that only read (`app.LookingAgent`, enforced where every other allowlist is), and the
  drain gate asks `WritingRun` rather than `Running` — is anything in flight that could touch the
  workspace. The asker declares and the receiver enforces: an asker cannot bind anybody, and a flag
  that only travelled would be a promise rather than a mechanism. A question and a piece of work
  from one asker are two conversations, because what a session may do is fixed when it is opened.
  A busy companion therefore queues (bounded, four) rather than refusing, and a receipt is minted at
  ACCEPT while the log position is taken at START: taken at accept, a piece that waited would have
  had somebody else's finished turn returned as its answer, which is a plausible wrong answer rather
  than an error. Two numbers — queued, and one in hand — ride in the published record, because
  neither can be derived from a state that is read off the session a person attaches to.
- **Load is written down as well as advertised** (`<socket>.load`). The live number answers "how
  busy is it now"; whether one copy is enough is a pattern, and a pattern is invisible in an
  instant. It decays after a month and survives the daemon, since the week after a companion was
  killed is when somebody asks whether it was overloaded. Deliberately not with the delegation
  verdicts, which are a judgement about a companion's work and belong in the repository.
- **One door across a machine, and it is ssh** (`--fleet-door`, with `--relay` as the wide pipe for
  your own machines). A remote companion is reached by an ssh pipe into `magi --fleet-door`, which
  carries four methods of the daemon protocol — ask what a companion is, hand it work, ask what
  became of it, and watch for how it goes — rather than subcommands that each re-derive what the
  daemon already knows. `watch` is the one that streams: the far daemon pushes each change back
  down the pipe the asker opened, so a hand-off's answer arrives when it happens instead of on a
  poll's clock. magi opens no port of its own between machines and holds no credential of its own:
  the security boundary is ssh (or the fingerprint-pinned TLS door, which carries the same four
  methods and nothing else).
- **Here or elsewhere is decided by the record, not by the hostname.** A config directory can be
  shared, so comparing this machine's name against a sighting's would dial a path that answers —
  and open the wrong workspace, with the work arriving looking delivered.
- **Membership across machines is gossip that decays** (`cluster.json`, `internal/core/cluster`).
  One ssh exchange is the whole join; after that each daemon trades with two hosts a minute. A
  companion unseen for five minutes is shown and not offered work, one unseen for an hour is
  dropped. It is a runtime file rather than configuration because configuration does not go out of
  date on its own, and this machine's own companions are never written into it — they are read from
  the published records every time.
- **A call that runs the model gets its own connection.** The console keeps one client per daemon
  and the client holds a mutex across the whole round trip, so a call that takes seconds is seconds
  in which nothing else about that companion can be asked. Measured live: with a drafted commit
  message in flight, the file tree took 2.7s against 0.6ms idle — it was not slow, it was queued
  behind a model. The five that run one (`/look`, `/git-msg`, `/pr-msg`, `/git-pr`, `/compact`) dial
  a connection of their own and drop it, which is what a meeting turn has always done. The daemon
  serves each connection in its own goroutine, so the second one costs a socket.
- **One stream per window, and none from a window nobody is looking at.** A browser allows six
  connections to one host and a stream never ends. Two per window — the transcript and the roster —
  meant three windows consumed the whole budget and every ordinary request queued behind a stream
  that would never finish: measured, the third window's first fetch never returned, and at six the
  sixth window could not load the document. The roster rides the transcript's connection as a named
  frame, a meeting screen opens its own (it watches a room rather than a companion), and a hidden
  tab closes its stream and re-reads the screen when it comes back.
- **What the console serves**, all of it derived or forwarded: `/fleet` (the list), `/events` (a
  transcript, streamed), `/context` `/plan` `/handoffs` (one companion, read off its log),
  `/skills` `/forget` `/remember` (the supervision loop), `/history` and `/search`
  (what a companion has done before now, and finding it by word), `/cron` (its unattended
  schedule), `/mcp` (read and edit a companion's external tool servers), `/report-format` (the shape
  a report must take before that companion may ask anything), `/tools` and `/model` (asked of the
  daemon, because the tool registry and the model catalogue are assembled by the process holding the
  run), `/loop` (the map of the turns, and the diff against the session a fork came from — read off
  the log here), `/console` (which machine this is), `/push` and `/sw.js` (waking a phone when a
  companion blocks), and the seven that change a run — `/submit` `/interrupt` `/answer` `/dispatch`
  `/compact` `/shell` `/model` — each forwarded to the daemon that owns it. A test checks that every path the page references — including its ES imports,
  which is how a 404 on `/vendor/material.js` went unseen — is one this binary serves.

  ⚠ `/promote` is gone with the promotion pipeline, and `/dispatch` no longer has a caller on the
  page: addressing a companion by typing its name, from a list where it is already one click away,
  was a second and harder way to do what the list does.
- **The binary carries what the page loads.** The typeface, the vendored RxJS bundle
  (`cmd/magi-web/vendor`, built once from a pinned version with its hash written down) and the
  language packs (`cmd/magi-web/i18n`) are embedded and served from this process. Nothing is
  fetched from a CDN: a page that reached out would depend on somebody else's machine being up and
  would tell it when you look at your agents. A test walks every path the page references and
  requires the static demo to carry each one — added after a deploy went out blank because the
  page had become an ES module and the demo still copied only the fonts.
- **Which conversation a companion is in is the DAEMON's answer.** It publishes one session in its
  record and every reader believes it — the fleet row, the console resolving where a prompt goes, a
  terminal attaching. So moving is `daemon.SessionMover`: refuse mid-turn, refuse a session outside
  this workspace, mark the conversation being left with `session.moved`, then republish. The mark
  is how other screens find out, through the log they are already reading. The record is written
  temp-and-rename under one lock, because its readers poll it and two goroutines write it (the
  queue depth, and this).
- **What a person may do is one table and one wrapper** (`cmd/magi-web/gate.go`). Every GET needs
  `read`; a write needs the capability the table names; a route in neither that table nor the
  read/public sets is refused, and a test walks the handler list so the decision is made when a
  route is written rather than when somebody is surprised by it. The policy itself is
  `internal/core/auth` — pure, so "may a responder change the model" is three lines and no server —
  loaded by `config.LoadAuth` from the GLOBAL config directory only. Nobody configured means one
  operator, which is what a loopback console is.
- **No authentication of magi's own**, by decision: loopback, and reached through whatever the
  organisation already runs. See `proposals/companions-and-supervision-2026-08-07.md` for the
  supervision model this exists to serve, and
  `proposals/external-access-2026-08-12.md` for what opening it to other people would take.
- **Three things that hold whatever the answer to that turns out to be**, and none of them assumes
  one: `listenLoopback` is the single place a port is opened and it refuses anything routable (a
  test counts the openers, because one lock is worth what the door count is); `-exposed` drops the
  routes that make the machine run what the caller chose — `/shell` and MCP writes — while leaving
  everything that reaches the machine through the agent; and `audited` wraps every route OUTSIDE
  the cross-site guard so a refusal is recorded too, appending method, path, companion, origin and
  status to `console-audit.jsonl`. Who comes from the gateway in front through the header
  `-user-header` names, unverified and said to be.

---

## 12. Extension points

> Step-by-step guides (adding an MCP server, bootstrapping shared experience):
> [`EXTENDING.md`](EXTENDING.md). Korean: [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md).

- **Lua plugins** (`adapter/plugin/lua`, `-plugins <dir>`): capability bundles
  (tools/hooks), hot-reloadable. NOT for transport-level concerns (auth/TLS).
- **MCP** (`adapter/mcp`, `config.toml [mcp]`): external tool servers over stdio.
- **Hooks** (`config.toml [[hooks]]`): PreToolUse/PostToolUse/Stop shell commands
  (POSIX shell; not available on Windows).
- **The council**: `port.Council` is the seam. The bundled implementation polls three members
  over one OpenAI-compatible backend each; a different one only has to answer
  `Deliberate(DeliberationRequest) (Deliberation, error)`.
- **Auth** (planned): custom auth (OIDC/mTLS/rotating tokens) belongs at the Go
  `http.RoundTripper` seam (`openai.WithHTTPClient`), not in Lua.
