# magi — Detailed design (historical)

[English](DESIGN.md) · [한국어](DESIGN.ko.md) · [↑ Docs](README.md)

> ⚠️ **This document is the design intent *as of* the M1 start.** The implementation grew a great
> deal after it, and much of that growth was later torn back out — the procedural planner, delegation
> to subagents, authored acceptance checks, the council that decided termination by vote.
> **For the current *as-built* reference read [`ARCHITECTURE.md`](ARCHITECTURE.md)** — where the two
> disagree, that file wins. This one is kept for the rationale (decisions D1–D13 made concrete).
>
> Korean edition: [`DESIGN.ko.md`](DESIGN.ko.md). The design intent is left as it was written, but
> **every note that claimed to describe the as-built system has been checked against the code and
> corrected** — a wrong statement about the present is not preserved history, it is just wrong.
> Each correction says what it is correcting.

> Makes PLAN's decisions (D1–D13) concrete down to the level just above code: event/command schemas,
> port signatures, package layout.
> The central pattern is **CQRS-lite** — *Commands* going in, *Events* coming out. That is what makes
> in-process and remote look the same (D5).

---

## 1. Package layout

> The tree below is the *as-built* one (as of 2026-06). Changes from the original proposal:
> `core/capability` removed (unused) · `core/{model,plugin}` added · `port` consolidated into a single
> `port.go` · `app` is `app.go` rather than `service.go`, with guardrail and workflow files added ·
> the built-in tool set grew well past the original six. Details in
> [`ARCHITECTURE.md`](ARCHITECTURE.md) §package map.

```
github.com/sayaya1090/magi

cmd/magi/                 # entrypoint: flag parsing (-p headless), DI wiring, system prompt

internal/
  core/                     # domain — zero outward (adapter) dependencies
    session/                #   Session, Message, Part, SessionMeta, Todo
    event/                  #   Event (the persisted log + the bus unit)   ★schema §3
    command/                #   Command (actor-tagged input)               ★schema §4
    artifact/               #   Artifact (D11)
    tool/                   #   Tool, ToolResult, Registry contracts
    model/                  #   ModelRef and other model-identity types
    plugin/                 #   plugin / capability metadata types
    agent/                  #   agent config + pure rules (stop conditions, context assembly)
    bus/                    #   EventBus (in-memory pub/sub, multi-subscriber fan-out)
  port/                     # ports (interfaces) — defined by the core        ★signatures §5
    port.go                 #   LLMProvider/Store/Tool/ToolEnv/Platform/PluginHost/ExperienceStore …
  app/                      # application services (use cases)                ★ §4
    app.go                  #   Application implementation + Config (Profile/Sandbox/Workflow…)
    loop.go                 #   the agent loop (port orchestration) + loop guard + language directive
    loop_gates.go           #   the finish path: Stop hooks · empty result · unrun output · declaration
    workflow.go             #   the deterministic workflow engine (phase gates)
    policy.go               #   guardrail policy engine (allow/deny/egress/secret-deny)
    context.go·compact.go   #   context assembly + compaction
    memory.go·skills.go·hooks.go·diagnose.go  #   AGENTS.md memory / skills / hooks / diagnostics
  adapter/                  # adapters (port implementations)
    llm/openai/             #   OpenAI-compatible (Ollama/vLLM/LiteLLM): caching, fallback, error mapping
    store/jsonl/            #   append-only JSONL
    tool/builtin/           #   read/write/edit/multiedit/grep/glob/list/bash/bash_output/
                            #   bash_kill/bash_input/wait_for/port_owner/todowrite/council/
                            #   webfetch/websearch/remember/skill/recall_* + sandbox_*
    platform/               #   per-OS exec / paths / terminal capabilities
    experience/git/         #   the shared brain (git repo)          (M5+)
    plugin/lua/             #   gopher-lua host                      (M3)
    mcp/                    #   MCP client                           (M4)
    tui/                    #   bubbletea UI                         (M2)
  config/                   # TOML config loader

plugins/examples/           # example Lua plugins
```

**Dependency rule**: `adapter → app → core`, and `app/adapter → port`. `core` imports nothing
(stdlib and core-internal only). Enforced at compile time.

---

## 2. Core data types (`core/session`, `core/artifact`)

```go
type SessionID string
type Role string // "user" | "assistant" | "tool" | "system"

type Session struct {
    ID       SessionID
    Workdir  string
    Agent    string        // name of the agent in use
    Model    ModelRef      // provider + model
    Created  time.Time
    Meta     map[string]string
}

type Message struct {
    ID    string
    Role  Role
    Parts []Part
}

// Part = the smallest unit of streaming and storage. Discriminated by kind (tagged union).
type Part struct {
    ID   string   `json:"id"`
    Kind PartKind `json:"kind"`
    // per-kind fields (exactly one is filled)
    Text     string          `json:"text,omitempty"`      // text|reasoning
    ToolCall *ToolCall        `json:"toolCall,omitempty"`  // tool-call
    ToolResult *ToolResult    `json:"toolResult,omitempty"`// tool-result
    Image    *ImageRef        `json:"image,omitempty"`     // image
    Err      string           `json:"error,omitempty"`     // error
}

type PartKind string // text | reasoning | tool-call | tool-result | image | error

type ToolCall struct {
    CallID string          `json:"callId"`
    Name   string          `json:"name"`
    Args   json.RawMessage `json:"args"`
}
type ToolResult struct {
    CallID  string          `json:"callId"`
    Content json.RawMessage `json:"content"` // text / json / an image reference
    IsError bool            `json:"isError,omitempty"`
}
type ImageRef struct { // the original lives in a separate file/blob; the log carries only a reference
    Path string `json:"path"` // or a blob hash
    MIME string `json:"mime"`
}

// Artifact (D11) — a reviewable output the agent emits
type Artifact struct {
    ID          string    `json:"id"`
    Kind        string    `json:"kind"`   // plan|walkthrough|screenshot|test-report|diff|...
    Title       string    `json:"title"`
    Content     json.RawMessage `json:"content"`
    SourceAgent string    `json:"sourceAgent"`
    Status      string    `json:"status"` // draft|proposed|approved|rejected
    Created     time.Time `json:"created"`
}
```

---

## 3. Event schema (`core/event`) — the persisted log + the bus

**The common envelope** — every event:

```go
type Event struct {
    Seq       int64           `json:"seq"`       // per-session, monotonic (assigned by the Store); 0 = bus-only
    SessionID SessionID       `json:"sessionId"`
    Type      Type            `json:"type"`
    Actor     Actor           `json:"actor"`     // who caused it (D5)
    TS        time.Time       `json:"ts"`
    Data      json.RawMessage `json:"data"`      // per-type payload
}
type Actor struct {
    Kind ActorKind `json:"kind"` // user|agent|system
    ID   string    `json:"id"`   // user id / agent name
}
```

**A. Persisted** (appended to the log, one JSONL line) — replaying them restores the conversation:

| Type | Data |
|---|---|
| `session.created` | `{workdir, agent, model}` |
| `prompt.submitted` | `{messageId, parts[]}` (role=user) |
| `part.appended` | `{messageId, role, part}` (one completed part) |
| `permission.decided` | `{callId, decision}` (for audit) |
| `artifact.emitted` | `{artifact}` |
| `council.convened` | `{round, members[], rule}` (D14) |
| `council.verdict` | `{round, member, decision(done\|continue\|abstain), confidence, rationale, feedback}` |
| `council.decided` | `{round, decision, tally, feedback}` |
| `compaction` | `{summary, replacesUpToSeq, tokens:{before,after}}` |
| `turn.finished` | `{usage:{in,out,cost}}` |
| `todos.changed` | `{todos[]}` (once per plan change — seed → step check → done/cancel; log, replay, panel re-render) |
| `error` | `{message, code}` |

**B. Transient** (bus only, never stored) — for live UX:

| Type | Data |
|---|---|
| `part.delta` | `{messageId, partId, kind, text}` (a streaming text fragment) |
| `tool.started` | `{callId, name}` |
| `tool.progress` | `{callId, ...}` |
| `permission.requested` | `{callId, name, args}` → UI prompt (the decision is stored as A) |
| `context.usage` | `{used, max, …}` (the context meter) |
| `workflow.phase` | `{phase, status, detail}` (workflow engine progress) |
| `council.deliberating` | `{round, member, state}` (the live deliberation panel, D14) |

> ★Correction: the original listed `agent.spawned` / `agent.status` here. Both are gone and stay
> gone. A plugin can spawn a child again, but its state is not reported through the bus: the child
> is a SESSION, so its own log already carries what it did, and the only thing a log cannot answer —
> which children are running right now and which just ended — is a live register the screen polls
> (`App.SubagentJobs`). A second, event-shaped copy of either would be one more pair to keep in step.

> Principle: **facts are persisted, progress (delta/progress) is transient.** A replay does not need
> deltas — the completed parts are enough. That keeps the log clean and preserves D6's
> "the bus is the store" spirit.

**A JSONL log sample** (`~/<datadir>/projects/<cwd>/<sessionId>.jsonl`):

```json
{"seq":1,"sessionId":"s_01","type":"session.created","actor":{"kind":"user","id":"local"},"ts":"...","data":{"workdir":"/x","agent":"default","model":{"provider":"openai","model":"qwen2.5-coder"}}}
{"seq":2,"sessionId":"s_01","type":"prompt.submitted","actor":{"kind":"user","id":"local"},"ts":"...","data":{"messageId":"m1","parts":[{"id":"p1","kind":"text","text":"add a test"}]}}
{"seq":3,"sessionId":"s_01","type":"part.appended","actor":{"kind":"agent","id":"default"},"ts":"...","data":{"messageId":"m2","role":"assistant","part":{"id":"p2","kind":"tool-call","toolCall":{"callId":"c1","name":"read","args":{"path":"x_test.go"}}}}}
{"seq":4,"sessionId":"s_01","type":"part.appended","actor":{"kind":"agent","id":"default"},"ts":"...","data":{"messageId":"m2","role":"tool","part":{"id":"p3","kind":"tool-result","toolResult":{"callId":"c1","content":"...","isError":false}}}}
```

---

## 4. Command schema + Application (`core/command`, `app`)

**A Command is input going in: actor-tagged and serializable.** The result flows back out as Events
(CQRS-lite).

```go
type CreateSession struct { Workdir, Agent string; Model ModelRef; Actor Actor }
type SubmitPrompt   struct { SessionID SessionID; Parts []Part; Actor Actor }
type Interrupt      struct { SessionID SessionID; Actor Actor }
type RespondPermission struct { SessionID SessionID; CallID string; Decision string; Actor Actor } // allow|deny|always
type Compact        struct { SessionID SessionID; Actor Actor }
type ReviewArtifact struct { SessionID SessionID; ArtifactID, Decision string; Actor Actor }      // approve|reject (→ a D13 contribution)
```

**The Application interface** — commands in, an event stream out:

```go
type Application interface {
    CreateSession(ctx context.Context, c CreateSession) (SessionID, error)
    Submit(ctx context.Context, c SubmitPrompt) error          // async: the loop is a goroutine, results arrive as events
    Interrupt(ctx context.Context, c Interrupt) error
    RespondPermission(ctx context.Context, c RespondPermission) error
    Compact(ctx context.Context, c Compact) error

    // Subscribe: replay from fromSeq, then live (supports late joiners and reconnects)
    Subscribe(ctx context.Context, s SessionID, fromSeq int64) (<-chan Event, func(), error)
    ListSessions(ctx context.Context, workdir string) ([]SessionMeta, error)
}
```

> Because of this shape the TUI (in-process) calls it directly, and a future server exposes the same
> methods over HTTP/SSE — D5's "only the transport is added".

---

## 5. Port signatures (`internal/port`)

```go
// LLM — the OpenAI-compatible adapter is the first implementation (D3)
type LLMProvider interface {
    StreamChat(ctx context.Context, r ChatRequest) (<-chan ProviderEvent, error)
}
type ChatRequest struct {
    Model    string
    System   string
    Messages []Message
    Tools    []ToolSpec     // name / description / jsonschema
    Params   map[string]any // temp, maxTokens...
}
type ProviderEvent struct { // one shape for every provider's stream
    Type string // text-delta|reasoning-delta|tool-call|finish|usage|error
    Text string
    ToolCall *ToolCall
    Usage *Usage
    Err   error
}

// Store — event-sourced persistence (D6). The first implementation is jsonl.
type Store interface {
    Append(ctx context.Context, s SessionID, evs ...Event) ([]int64, error) // returns the assigned seqs
    Read(ctx context.Context, s SessionID, fromSeq int64) ([]Event, error)
    ListSessions(ctx context.Context, workdir string) ([]SessionMeta, error)
    Compact(ctx context.Context, s SessionID, upToSeq int64, snapshot Event) error
}

// Tool — built-ins are Go implementations (no POSIX dependency). Plugin and MCP tools
// satisfy the same interface.
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage, env ToolEnv) (ToolResult, error)
}
// ToolEnv — the original had five fields; guardrails and the council widened it.
// Below is the as-built summary (the full set, with comments, is in internal/port/port.go).
//
// ★Correction: the four multi-agent fields the original listed (Spawn/Dispatch/Ask/Report) do
// NOT exist. After the one-agent change nothing set them and no tool read them, and a port that
// advertises a contract the application never fulfils teaches something untrue to a reader — and
// to a model reading the tool surface. They were removed.
type ToolEnv struct {
    SessionID SessionID
    Workdir   string
    ScratchDir, ScratchTmp string                                               // the turn's scratch dir / the child TMPDIR
    AskPermission func(callID, name string, args json.RawMessage) (bool, error) // the permission gate
    EmitArtifact  func(Artifact)                                                // D11 output
    EmitProgress  func(text string)                                             // a live note while a tool blocks
    // Council and user — a nil field means the capability is not available in this run,
    // and every tool checks before calling.
    Council func(ctx context.Context, question string, complete bool) (string, error) // complete = declare finished
    AskUser func(question string, options []string) (string, error)                   // interactive only
    RouteInterjection func(action, reason, requestID string) error                    // top level only
    // Plan / memory / skills
    SetTodos    func(todos []session.Todo)          // todowrite
    NoteForTurn func(text string) error             // remember{scope:"turn"}; an error means NOT kept
    Propose     func(c Contribution) error          // a shared-experience (D13) contribution
    LoadSkill   func(name string) (string, bool)    // load a named skill
    Recall       func(query string) (string, error) // detail this session's compaction shed
    RecallMemory func(query string) (string, error) // the cross-session D13 store
    Platform  Platform
    Sandbox   SandboxSpec                           // OS sandbox (read-only|workspace-write…); zero value = unconfined
}
type ToolRegistry interface { Register(Tool); Get(name string) (Tool, bool); List() []Tool }

// ExperienceStore — the shared brain (D13), backed by a git repo
type ExperienceStore interface {
    Retrieve(ctx context.Context, q string) ([]Memory, []Skill, error) // session-start RAG
    Propose(ctx context.Context, c Contribution) error                  // into the review queue (never auto-applied)
}

// PluginHost — hot reload (D10)
type PluginHost interface {
    Load(ctx context.Context, dir string) (PluginInfo, error)
    Unload(name string) error
    Reload(name string) error
    Capabilities() CapabilitySet
}

// Others
type ContextProvider interface { Provide(ctx context.Context, q ContextQuery) ([]ContextChunk, error) }
type Scheduler interface { // D12: tier 1 ticker (M5), tier 2 OS (later)
    Schedule(spec ScheduleSpec, target Trigger) (id string, err error)
    Cancel(id string) error
}
type Platform interface { // the cross-platform abstraction (§9.5)
    Exec(ctx context.Context, cmd Cmd) (ExecResult, error)
    ConfigDir() string
    DataDir() string
    TerminalCaps() TermCaps // truecolor / image-protocol detection
}

// Council — D14. Member fan-out is the adapter's job; the consensus rule is pure core.
// The bundled adapter parses each reply into a Verdict (reusing the JSON fallback). It calls
// StreamChat once per member in parallel ONLY when the members are pinned to different backends;
// when they share provider and model, one panel call carries every member's walk and verdict, and
// a second call closes the round (see CouncilMember below).
type Council interface {
    Deliberate(ctx context.Context, r DeliberationRequest) (Deliberation, error)
}
type DeliberationRequest struct {
    Round    int
    Phase    string         // a label distinguishing kinds of deliberation (today: the finish declaration)
    Task     string         // the original goal
    Plan     string         // the contract: acceptance criteria
    Report   string         // the claim: the agent's own report, if any
    Actions  string         // the evidence: this turn's tool results (bytes written, cat output …) — git-independent
    Signals  []Signal       // the evidence: test / lint / type outcomes
    Changes  string         // this turn's file edits, reconstructed from the agent's write/edit tools (optional)
    Members  []CouncilMember
    Rule     string         // unanimous|majority|quorum:k|weighted:θ|veto
    Debate   bool           // MAGI_COUNCIL_DEBATE: a SPLIT would-be-done triggers one rebuttal round
    Keep     bool           // MAGI_COUNCIL_KEEP: members also name what to keep; advisory, carried in continue feedback
    // Also: DefaultModel, NoChanges, Changes. See port.go for the full set.
    // ★Correction: there is no Devil (MAGI_COUNCIL_DEVIL). Neither is there a Phase="plan" plan
    // audit, nor the Criteria and deliverable Checks it used to produce — those decided things
    // before the work existed, and went out with the planner.
}
// Where the evidence comes from: a git diff is empty in a non-git workdir (a sandboxed task
// directory), which left the finish gate unable to judge what had been produced and churning
// forever. Passing this turn's tool RESULTS (Actions) and the edits reconstructed from the tools
// (Changes) as git-independent evidence fixed that. The model's own narration is deliberately
// excluded from the evidence (Report is the claim), which is what stops a "talked its way to done
// with nothing built" regression — an [ok] or an exit 0 is not evidence on its own; the output has
// to be shown.
type CouncilMember struct { // a themed label plus a lens
    Name     string  // "Melchior" | "Balthasar" | "Casper"
    Lens     string  // "correctness" | "verification" | "completeness"
    Model    string  // empty = the session model
    Provider string  // empty = the default backend; a different one keeps the per-member call shape
    Weight   float64
}
// A lens comes with a ROUTE (core/council.Routes): where that member walks FIRST through the same
// evidence — the literal words and the values themselves (correctness), the moment each behaviour
// actually ran (verification), every distinct part the task asked for (completeness). A route is an
// order of search, NOT a jurisdiction: all three still judge the whole task, because dividing it
// would let a defect inside one member's share draw one continue against two uninformed dones. The
// route exists because the lens alone did not differentiate them — one line of lens apiece and
// every other instruction identical produced 21 done votes out of 21 with no dissent.
//
// Before it may state a decision, a member writes the WALK: one line per requirement, SATISFIED or
// UNSATISFIED, settled by a verbatim fragment of a tool result or by NO-EVIDENCE. The field sits
// before `decision` in the schema, so a reading cannot be assembled backwards from a conclusion
// already reached; what the member said it was reading is kept on Verdict.Cite.
//
// After the tally, ONE closing call reads all three walks together — the only seat from which a
// contradiction between members, a requirement no walk covered, or a value wrong on its face is
// visible. Its conclusion is clamped one way (done → continue, never the reverse) and is carried on
// Deliberation.Close whether or not it changed anything. The clamp and the batching are ADAPTER
// concerns; core/council still only counts votes.
// Verdict/Deliberation/Tally and the consensus rules live in core/council (pure). Signal is D16.
```

---

> **Extension note**: the real `app.Application` carries more than the skeleton above — guardrail
> policy, the deterministic workflow, AGENTS.md memory, hooks, mid-turn interjection, and the council
> as a tool.
> ★Correction: the multi-agent surface the original listed here (task/spawn/dispatch/ask/report)
> does not exist. The behavioural reference is [`ARCHITECTURE.md`](ARCHITECTURE.md).

## 6. The agent loop (`app/loop.go`) — pseudocode

```
Submit(cmd):
  store.Append(prompt.submitted); bus.Publish(...)
  go run(sessionID)           // async; Interrupt cancels the ctx

run(sessionID):
  for each step:
    msgs   = assemble(history, latest compaction, contextProviders, experience.Retrieve)
    stream = llm.StreamChat(req{msgs, tools})
    for ev in stream:
      text-delta   -> bus.Publish(part.delta)                 // transient
      tool-call    -> collect
      finish       -> store.Append(part.appended for text)    // persisted
    if no tool calls:
      // ★Correction: the council gate that convened by itself, as the original described, is gone.
      // That placement decided two things it could not get right — WHEN it was asked (the one
      // moment the agent had already made up its mind) and whether its answer would be read at all
      // (in a headless run the advice was injected and turn.finished written in the same tick).
      // What runs now is the finish path (loop_gates.go), in order:
      //   1) Stop hooks — a failing hook pushes the agent back to work with the hook's output
      //   2) the empty-result nudge (an answer with no text) — once
      //   3) authored but no command NAMED it — deterministic, no model call, once per turn
      //   4) the declaration — the agent is told to call the `council` tool with complete:true.
      //      Bounded at three asks per stretch of no progress; a real mutation since the last ask
      //      restarts the budget.
      // The council is now a TOOL the agent calls, and accepting a declaration signals the loop.
      store.Append(turn.finished); return
    for call in toolcalls:
      if needsPermission(call): bus.Publish(permission.requested); wait RespondPermission
      store.Append(permission.decided)
      bus.Publish(tool.started)
      res = registry.Get(call.name).Execute(...)
      store.Append(part.appended{tool-result})
```

> ★Correction (as-built): **there is no pacing ceiling — only a 240-step runaway backstop** — and
> **the guard reports rather than stops**. The force-stops came out on measurement — runs that
> reached the external deadline were still scored and 76 of 396 passed, while 28 runs magi stopped
> itself produced no pass at all, 8 of them never scored because a nonzero exit reads as "the agent
> failed to run". A top-level turn that spends the backstop lands with a persisted UNVERIFIED
> turn.finished naming it. Every signal the guard collected (repeats, stalls, self-reverts,
> no-change writes, exercise churn) is still collected and still **said** to the agent as a nudge.
> The language directive (`langDirective`) and the workflow branch (`runWorkflow`) are unchanged.
> A workflow PHASE declares its own budget.

---

## 7. M1 implementation order (against this design)

1. `core/session`, `core/event`, `core/command`, `core/artifact` types.
2. `core/bus` in-memory pub/sub.
3. Declare every `port` interface (empty).
4. `adapter/store/jsonl` — Append/Read/Subscribe replay.
5. `adapter/llm/openai` — Ollama `/v1` streaming + tool_calls + **the prompt fallback**.
6. `adapter/tool/builtin` — read/write/edit/grep/glob/list (Go).
7. `adapter/platform` — exec / paths / terminal capabilities (darwin/linux/windows).
8. `app/app.go` + `loop.go` — the loop above. (The original called this `service.go`.)
9. `cmd/magi` — `-p` headless (a prompt on stdin, events on stdout).
10. **A live Ollama real-model tool-calling test** (native + fallback) plus core unit tests.
