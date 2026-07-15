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
    app.go                  App (the Application): commands in → events out; session/turn state
    routing.go query.go     app.go's split siblings: agent/model/profile routing + permission
                            config (routing.go), and the read-only session/workspace query
                            surface — transcript, plan, child sessions, git-diff/shell (query.go)
    config.go               Config/AgentSpec/route+profile types, withDefaults, applyProfile
    todos.go                plan/TODO state machine (SetTodos, advanceTo, finalizeTodos)
    loop.go                 runLoop: the agent loop; buildStepSystem (cacheable prompt); the
                            per-step stream/persist/finish flow
    interject.go            loop.go's split sibling: mid-turn steer/interjection machinery —
                            routing (applyRoute), the idle-park/finish triage mini-turns
                            (handleAside/triageQueued/interjectTurn), agent-initiated replan
                            (honorReplan), and the interjection/turnControl state accessors
    guard.go shellcmd.go council_gate.go criteria.go execute.go permission.go prompt.go
                            loop.go's split siblings: runGuard (stall/loop/regression) and its
                            stateless shell-command classifier (shellcmd.go), the consensus gate,
                            acceptance criteria, tool execution, permission prompting, and
                            prompt/system assembly
    orchestrate.go          subagent dispatch/spawn/supervisor; escalate (ask); bgGroup
    planner.go plan_flags.go plan_prompts.go
                            recursive pre-flight planner: solo/parallel/scout/delegate/refine;
                            planEnvelope budget/depth hint, guardExpansion depth-cap guard,
                            MAGI_ADAPT-gated reactive retry + escalate, MAGI_REFINE_SHARED
                            shared-session refine phases, MAGI_SPEC_FIDELITY literal-preservation
                            (planner rule + plan-time note + verbatim delegate SPEC anchor); the
                            MAGI_* A/B env knobs (plan_flags.go) and prompt/contract builders
                            (plan_prompts.go) are split into siblings
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
    tui/                    Bubble Tea UI, split by concern: model.go (Model + Update),
                            model_input.go (mouse/key/slash), model_event.go (event folding),
                            model_route.go (route/profile forms), model_layout.go (resize/panes),
                            model_view.go (render). Transcript, subagent panes, /route editor
                            (session-model suggest box = profiles ∪ `App.ListModels` gateway catalog).
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
   A per-step **volatileContext** (ephemeral trailing message, never in the cached
   system prompt) carries the step budget, a self-measured **elapsed** line once a
   turn passes 1m (`time.Since(runStart)` — magi's own stopwatch, not scorer info),
   an optional `--time-budget` remaining/EXCEEDED block (`Config.TimeBudget`, default
   off, kept off for leaderboard runs), the live todo list, and **push-side shard
   hints** (`shardHints` in `recall.go`) — the compacted-away topics that lexically
   match the current task, **BM25-lite IDF-ranked** so a rare token pinning one region
   ("heap.go") outweighs a generic one across all shards, requiring ≥2 distinct token
   matches; a weak model that never calls `recall_context` is still pointed at what it
   lost, and still pulls the verbatim originals through that tool.
2. `StreamChat`; stream text (`part.delta`) / reasoning / tool calls; on finish
   persist the assistant `part.appended`.
3. No tool calls → finish branch (see §5/§6 gates), else execute tools (read-only
   tools run concurrently; writes/permissioned/task run sequentially) and loop.

Guards that make weak models safe (in `guard.go`/`orchestrate.go`; the loop body
in `loop.go` calls into them each step):
- **runGuard**: an identical `(tool,args)` call is hard-blocked past `repeatLimit` **only
  when its outcome provably cannot change**: `read`s (whose fingerprint drops the `limit`
  arg, so re-reading the same head with limit jitter collapses onto one counter while
  genuine paging via `offset` doesn't), inspect-only bash (the closed `echo`/`cat`/`ls`
  verb set), and identical write replays. **Exec bash — build, test, any script — is
  exempt**: its outcome can change through state the guard can't see, and build/test can
  be invoked too many ways to enumerate, so exec is defined by exclusion from the closed
  inspect set; a genuine exec spin still climbs the no-progress window and is terminated
  by the stall layer. `blockedBudget` blocked repeats → the `repeat` stuck kind (recovered
  below, else `loop_guard` stop). A successful file write/edit with *changed* content bumps
  a mutation epoch that resets the counts — and so does a bash command that authors or
  mutates files, whether by redirect/heredoc/`tee` (`noteBashWrite`) or by a redirect-less
  mutating verb (`mutatesFiles`: `sed -i`, `patch`, `cp`/`mv`/`rm`, `git apply/checkout/…`,
  `go mod`, `pip`/`npm install`, `tar x`, …) — so a bash-driven fix cycle re-keys its
  build/test fingerprints the same way an edit-tool fix does. A blocked repeat echoes the
  earlier result.
- **decomposing stuck recovery** (`redecomposeStuck` → `driveStuckTodos`,
  `MAGI_STUCK_DECOMPOSE`, default on): when a plan-eligible turn is force-stopped stuck
  (`stall`, or `repeat` when the flag is on), recovery re-plans the stuck task into an
  explicit TODO list and drives the units ONE AT A TIME — each unit in a child seeded with
  the **full parent conversation** (`CloneContext`, so it continues from what was already
  read instead of re-deriving context and re-fixating) but scoped by prompt to just that
  unit, on a quarter of the whole-task step budget (floor 8) so a re-fixating unit fails
  fast. A landed unit's session is **reused** for the next unit (the refine shared-session
  pattern); a failed unit reverts its todo to pending, resets the chain to a fresh parent
  clone, and the driver moves on — units already landed survive. Recovery units are
  *appended* below any existing plan todos. When decomposition wasn't possible (<2 units)
  the old whole-task re-spawn runs as fallback (now also `CloneContext`); when it ran and
  every unit failed, the fallback is skipped. On a landed `repeat` recovery the blocked
  counter is cleared too (`resetRepeat` — `resetStall` alone would re-halt immediately).
- **corrective re-grounding nudge**: before a force-stop, a re-grounding message (re-read
  the task, change approach) is injected. It fires on either stall the guard can see —
  `blocked` past `nudgeThreshold` (the *same* action repeated, once per run), OR
  `sinceProgress` past `noProgressNudge` (many *varied* tool calls with no real mutation),
  which re-arms per window up to `maxStallNudges`. A successful **bash write** counts as a
  mutation too (`noteBashWrite` → epoch bump), so bash-heavy work isn't misread as a stall.
  **Regressive edits don't count as progress**: `noteEdit` hashes each touched file's
  content across the turn, and an edit that returns a file to a state already seen this
  turn is churn, not progress — `retractProgress` restores the pre-write stall epoch so an
  implement↔revert oscillation keeps climbing toward a stall stop instead of resetting it
  every swing (without this, a model that writes a stub, reverts it, and re-tries forever
  never trips any guard and burns the whole budget).
  **Tabu list** (`failedStates`/`checkTabu`): the higher-precision complement to the
  self-revert check. When a command that *exercises* the deliverable fails (`noteExerciseFail`,
  gated by the same `isInspectOnly` verb set as the execution-evidence signal), the whole
  authored file set's content signature (`deliverableSigLocked` over `changeSet` after-states)
  is recorded with a snippet of the failure. A later edit whose signature matches a recorded
  failing state gets a one-shot advisory citing that failure — so an agent circling back to a
  proven-bad state is told so, where the self-revert check only knows a state *repeated*, not
  that it was known to fail. Advisory, once per signature, never a block.
  After the stall nudges are exhausted, one further ignored window force-stops the run as
  `stall_guard` — the backstop that keeps an unresponsive agent from wandering to the
  (240-step default) ceiling. **Stalled-nudge convergence** (`stallConverge`,
  `MAGI_STALL_CONVERGE`, default on): a re-arm whose window produced *no forward motion since
  the last nudge* — neither a real mutation nor a first-seen non-inspect exercising command
  (`progressSinceNudge` stays false) — means the redirect was ignored, so the remaining nudge
  budget collapses and the `stall_guard` lands *this* window instead of firing up to
  `maxStallNudges` more. It only accelerates the identical terminal landing (epoch>0 → clean
  finish, else `stall_guard`); a mutation sets `progressSinceNudge` and restarts the window, so
  an agent that edits in response to a nudge always re-arms normally — convergence never cuts a
  productive redirect. The planner also emits an advisory `estimated_steps` that the
  per-step budget line cites as a pacing reference (never a limit).
- **pre-finish landing**: a top-level turn that changed files finishes as soon as the model
  stops calling tools and the consensus council (when configured) votes done — there is no
  separate delegated review gate or self-verify self-prompt. The `tester`/`reviewer` agents
  remain in the registry as manual `task`-tool delegation targets; they are no longer
  auto-dispatched. The advisory `guard.unverifiedDeliverable()` signal (below) still feeds
  the council so a changed-but-never-run deliverable is a hard fact a text vote can't wave
  through.
- **churn graceful-finish**: when the loop guard force-stops a spinning turn (`guard.stuck()`
  repeat/stall), the outcome depends on whether the run produced a deliverable. Epoch > 0
  (real output already written, now only re-confirming) → finish cleanly (`turn.finished`,
  exit 0) with the work as-is, so a completed task is not misreported as an agent-level
  failure. Epoch 0 (pure thrash, nothing produced) → keep the `loop_guard`/`stall_guard`
  error abort so the failure stays visible.
- **fabrication defense — behavioral, structural, no phrase matching**: the signal is
  **structural, not lexical**: `guard.unverifiedDeliverable()`
  is true when the turn produced/changed a deliverable this epoch (`mutationEpoch() > 0`) but
  ran **no command that exercises the current version** (`execSinceMut == 0`). Execution is
  counted by leading verb: a small closed set of POSIX inspection verbs (`ls`/`cat`/`echo`/
  `exit`/`true`/… — see `inspectOnlyCmds`) does NOT count; anything else (a program, its
  tests, `./run`, `make`) does. A bash file-write is deliverable *production*, not exercise,
  so it bumps the epoch and resets the counter — writing then declaring done without running
  trips the flag. This replaced the former English-only confession-phrase scan
  (`core/selfcheck`, now deleted): it is language-agnostic (it reads behavior, not wording),
  catches confident-but-false "verified" claims (there is no execution to point to), and needs
  no ever-growing phrase list. It is deliberately biased toward NOT flagging (unknown verbs
  read as execution) to keep false positives low. The flag feeds two advisory paths, neither a
  hard block: the council as a deterministic `self-check: unverified` signal; and the subagent
  take-report branch, which refuses a "done" report once when the subagent *could* have run its
  work (`agent.allows("bash")`) but didn't — a read/write-only agent that cannot execute is
  never refused.
- **no retry storm**: a terminal provider error ends the turn; `startRun` does NOT
  re-run a failed turn (only re-runs to pick up a user *steer*).
- **mid-turn interjection routing**: `turnTask` (what the nudge and council anchor on) is
  snapshotted once at step 0, so a 2nd user request that lands *mid-turn* used to be ignored
  by both — the agent thrashed, re-running the already-done first task. Now the loop detects a
  new genuine (`ActorUser`) prompt at step>0 and, by default, **queues** it: it stays anchored
  on the current task, an **ephemeral notice** tells the agent the request is deferred (so it
  stops oscillating), and the queued text goes on the wait queue (`pendingInterject`, drained in
  `startRun`). The notice rides the per-step volatile context — never persisted — keyed by the
  interjection's MessageID and pruned the moment it resolves (routed/drained/resurfaced), so a
  handled interjection can't echo a stale "queued" note into later turns or reload views; the
  dispatch-case "answer briefly now" nudge is one-shot. Once a queued interjection re-runs as
  its own turn, `liveEvents` also drops its stranded ORIGINAL prompt from the model context
  (`dropResurfacedOrigins`, previously display-only) so the instruction never appears twice. The orchestrator may override with the **`route_interjection`** tool — `redirect`
  (re-anchor `turnTask` on the interjection now) or `append` (fold it in) — which re-snapshots the
  task and `reground`s the stall/council accounting. A tool's Execute callback can't touch
  loop-local state, so it records a per-session `turnControl` signal the loop drains at the top of
  each step. Depth-0/orchestrator only — subagents aren't user-steered.
- **finish-boundary triage of the wait queue**: at turn end the `startRun` drain pops each queued
  interjection one at a time and runs the shared triage mini-turn (`triageQueued` → `interjectTurn`
  in `modeQueued`, §idle-park below): the model either **answers** a question/chitchat inline from
  the session's own recent transcript (no fresh-slate reset, so a follow-up like "how many files
  did you change?" keeps the task context) and the item is consumed, or it **routes** (calls
  `route_interjection` — any action, since there is no running turn to re-anchor) to signal *real
  work*, which **escalates** the item to its own fresh top-level turn. The safe default is escalate:
  a triage that produces no usable reply runs the steer as its own turn rather than risk dropping
  it. Each pop is atomic under `a.mu` but the triage model call runs unlocked; the teardown re-checks
  both the trailing-message safety net (`hasUnansweredUserPrompt`) and the queue under the *same*
  lock as the `cancels` delete, so a steer landing during triage (`Steer` also takes `a.mu`) is
  never stranded — it is either caught and re-run or seen by `Steer` as a retired goroutine it
  restarts. On a terminal error/cancel the drain skips triage and persists any still-queued
  interjection back to the log as an unanswered prompt rather than stranding it in memory, but does
  not re-run it (preserving the no-retry-storm guarantee). An escalated (or persisted) prompt carries a
  `ResurfacedFrom` link to the original prompt's MessageID so the display layer pairs the query
  with its answer: `SessionState`→`dropResurfacedOrigins` drops the stranded original on replay,
  and the TUI's `applyEvent` pulls the still-visible live bubble down to just above the incoming
  answer (`moveUserBlockToEnd`) — display-only, turn logic is unchanged (it uses `reconstruct` directly).
- **idle-park aside handler** (`handleAside`): the routing above assumes the orchestrator is
  running its own steps, which the soft directive rides on. But when the planner's only work
  this turn is background explorers, the loop *idle-parks* (§5, `awaitingExplorers`) and runs
  no step — so a soft "you MAY answer" directive starves: an interjection there got a verbal
  ack at best and never fired the wired steer tools. The idle-park path instead runs a
  **bounded, tool-capable mini-loop** in isolated context (just the aside + a clip of the task
  for reference — never the whole transcript, which would let synthesis pressure bury the
  reply). This is the same `interjectTurn` primitive the finish-boundary drain triage uses; the
  two share the stream/persist/effect-trace machinery and differ only by `triageMode` — `modeAside`
  (here) wires `route_interjection` to signal the running turn's `turnControl`, while `modeQueued`
  (drain) marks `escalate` since the turn has already ended. It offers EXACTLY three
  *signal/interaction* tools — `route_interjection`,
  `cancel_dispatch`, `ask_user` — and NO execution tools (read/bash/write/`task`): those would
  re-create the starvation/duplication bug by doing the delegated work in this isolated turn.
  So the aside turn only SIGNALS (reply to chitchat, or route ± cancel ± clarify); the real
  re-plan/re-dispatch happens in the next normal step with the full toolset restored. The
  aside is enqueued *before* the mini-loop (so `route_interjection`'s pending-interjection
  requirement is met); a routed redirect/append is left queued for the `turnControl` drain to
  apply, while a resolved chitchat reply or a bare cancel is consumed there so it doesn't also
  re-run as its own turn. A "switch now" redirect is expected to pair with `cancel_dispatch`
  (the prompt says so) — a bare redirect is no-loss but only re-anchors what gets synthesized
  once the still-running explorers report. Asides that piled up *before* the park (e.g. during
  planning) are flushed through the same handler on park entry (`pendingInterjectTexts`
  snapshot) instead of starving until turn end. Because the mini-loop's raw tool call/result
  stay isolated (to keep the delegated task's log clean), a routed/cancelled steer would leave
  no trace in the transcript — so its *effect* (`asideEffect`: which `route_interjection` action
  fired, how many subagents `cancel_dispatch` stopped, the reason) is persisted as a durable
  **system-actor `steer`** prompt. That actor is deliberately not `ActorUser`, so every
  interjection/turn-detection path (all `ActorUser`-filtered) ignores it — it audits, it never
  becomes a new "last user prompt" or `turnTask`. The prompt itself is also stiffened: a text
  ack ("got it, I'll focus on X") changes nothing, so anything that touches the work (narrows/
  widens scope, changes files/targets, adds/drops a constraint, reorders, switches goal) MUST
  route rather than merely reply.
- **deferral ledger survives a hard kill** (`interjection.deferred`): the interjection mask —
  which queued follow-ups the running turn must *not* fold into its model context — lives only in
  the in-memory queue (`pendingInterject`/`interjectSeen`). A graceful teardown resurfaces every
  still-queued item (above), but a hard kill drops the queue while the original `PromptSubmitted`
  facts stay on disk, so a reload would re-see them as fresh pending prompts and mix an abandoned
  interjection into the next request. An append-only ledger of `TypeInterjectionDeferred{MessageID,
  Resolved}` facts records each queue transition: enqueue writes `Resolved:false`, an absorbed-inline/
  by-route removal writes `Resolved:true` (`recordDeferral`), and a drain-to-own-turn needs no mark
  (already recorded by the resurfaced prompt's `ResurfacedFrom`). On load `abandonedDeferrals` =
  deferred(false) − Resolved:true − `ResurfacedFrom` origins; `ensureDeferredHydrated` seeds it once
  per session into `SessionState.deferredAbandoned` (read outside `a.mu`, double-checked flag, never
  cleared by `resetForNewTopLevel`). Both mask accessors (`deferredInterjectIDs` for live context,
  `interjectSeenIDs` for turnTask/council) union that set, so an abandoned orphan stays masked from
  the model while remaining grey in the transcript (raw `reconstruct` is untouched — history, not
  turn logic). The ledger writes are inert extra facts no control-flow predicate reads, so the
  graceful path is byte-identical.
- **re-plan anchors on the adopted task**: when a route (`redirect`/`append`) `reground`s with a
  fresh decomposition, the re-plan must decompose `turnTask` (the *adopted* task — for `append`,
  the original goal folded with the steer's constraint), not the bare last user prompt (which is
  only the steer). `maybePlanPreflight` takes the adopted task as `taskOverride`; since the
  planner decomposes a *window* of conversation (`plannerWindow`'s byte budget), a long turn's
  explorer results can push the original goal out of that window, so `runPlanner` appends the
  adopted task as a final anchor message that survives the trim. Normal pre-flight and plan-audit
  re-plans pass no override, so their input is byte-identical.
- **agent-initiated replan** (`replan` tool, plan-eligible agents): when the work itself proves
  the current plan unworkable (a premise broke), the agent requests a fresh decomposition +
  reset no-progress window instead of thrashing into the stall guard. It is advertised only to a
  plan-eligible agent (`toolSpecs` hides it via `planEligible(agent, depth)`, mirroring the
  `env.Replan` nil-gating) so a read-only or max-depth subagent never sees a dead tool. Anti-abuse:
  `honorReplan` caps it at `maxReplansPerTurn` (2) and refuses a back-to-back replan with no tool
  work in between (`guard.callCount()` unchanged) — so replan can't indefinitely reset the stall
  guard; a refused replan leaves the guard intact and injects guidance.
- **language lock** (`langDirective`): the user's script (Hangul/Kana/…) is detected
  and a "reply in X" directive is prepended to the system prompt (top-level only).

**Consensus council gate (D14, the signature — `runCouncilGate`).** When `Config.Council`
is set (ON by default; disable with `[council] enabled=false`), the finish branch (no tool calls, `depth==0`, not workflow
mode) does NOT finish immediately: it convenes a **council** that votes done-vs-continue.
- 3 default members (the MAGI): Melchior/spec-fidelity, Balthasar/verification,
  Casper/completeness — theme-name labels, lens attributes, configurable via `[council]`.
- Consensus is pure `core/council.Tally` (unanimous|majority|quorum:k|weighted:θ|veto);
  a tie/unmet-quorum/abstain-all/degenerate-param all resolve to **continue** (never
  finish unless affirmatively satisfied). Member fan-out is `adapter/council/llm` over an
  `LLMProvider`; a member that errors or returns unparseable output abstains.
- On **continue**, the aggregated feedback is injected as a `prompt.submitted`
  (reusing the Stop-hook injection path) and the loop runs again. The injection
  (`continuationText`) re-anchors the **verbatim objective** — so a long turn can't
  lose the exact spec to a paraphrase — followed by a short **completion-audit rubric**
  (`councilCompletionAudit`: treat "done" as UNPROVEN until the current state shows it,
  uncertain ⇒ not done), which re-grounds a weak model resuming after a continue vote
  on the letter of the task. On **done** (or the
  safety stops below) the turn finishes.
- Safety so the gate can't trap the loop: `CouncilMaxRounds` cap (default 3), a
  no-progress guard (empty/repeated feedback finishes), a **cost-efficiency cap**
  (deliberation self-times via `councilSpent`; once it has run ≥1 round AND consumed
  ≥60s AND ≥¼ of the turn's wall-clock, further rounds cost more than they're worth,
  so it finishes UNVERIFIED rather than convening another 3-member round), and a
  ctx-cancel early-out. Forced finishes are recorded as a `council.decided` whose
  `note` states the council never approved (treat as UNVERIFIED) and carries the last
  outstanding feedback.
- Member prompts additionally refuse a "done" that RATIONALIZES incompletion ("impossible,
  so this is full completion") and require, for checkable deliverables, that the turn
  actually RAN the check with real output visible — existence is not correctness.
  `[council] preset="light"` trades the 3-member gate for one verification member and a
  1-round cap (interactive latency).
- Members also check the deliverable against the **letter of the task**: when the task
  dictates exact content/value/format/name/location, a deliverable that exists but whose
  content doesn't match (a placeholder, a filename where the content was asked for, the
  right shape with the wrong value) is a concrete defect → continue. The agent's own
  paraphrase of what it did is a claim, never proof the content is right.
- Evidence judged = Task (the original goal) + Report (the agent's final message) + tool
  results + **the agent's own edits this turn** (size-capped). Those edits are reconstructed
  from the agent's write/edit/multiedit tool calls — the run guard captures each touched
  file's before→after content and `buildCouncilChanges`/`core/change.LineDiff` renders a
  per-file diff (line-capped to a summary past 1000 lines so it can't OOM). This is
  git-independent and correctly attributed — a human/external/bash change is never credited
  to the agent. (`GitDiff` remains, but only for the `/diff` command, not the council.)
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
- **Supervisor**: activity-based stall watchdog (primary liveness; suppressed while a
  tool is in flight so a silent long tool isn't mistaken for a hang), a generous
  per-attempt hard timeout as a pathological backstop, and bounded auto-restart.
- **Auto-orchestration** (`Config.AutoOrchestrate`): when context usage exceeds the
  threshold (default 60%, -1 to disable), the system injects a directive forcing the
  top-level agent into orchestration mode — decompose work and delegate to subagents.
  Fires once per session to prevent context overflow on complex tasks (SWE-bench style).
- **Pre-flight planner** (`Config.Planner`, `app/planner.go`, default on,
  `[orchestration] planner=false` to disable): before a turn, a tool-free call to the
  `planner` agent judges whether the task splits into independent steps and assigns each a
  strategy — `solo` (the main agent does it), `parallel`/`scout` (read-only explorers fanned
  out via `spawn`), **`delegate`** (a self-contained, *independent* write sub-task handed
  context-free to a producing agent), or **`refine`** (a large *non-independent* sub-goal worked
  out in-context — see the hierarchical-refine bullet below).
  Explorers are proactive parallel *investigation* (the complement to reactive
  auto-orchestration); delegate steps are dispatched **inline/sequentially** so their writes
  can't race the council's change capture. Combined findings are injected before the main loop
  runs; the main agent is told to **verify and integrate** delegated work (marked
  `(delegated to …)`), not redo it. Degrades to solo on any failure. The decision is
  observable: it emits a `plan` phase (`workflow.phase` event) carrying the mode + reason,
  which the TUI shows as a header chip and a transcript line.
- **Recursive planning** (`planEligible` in `app/planner.go`): the preflight lives inside
  `runLoop`, so a `delegate`d child re-plans at its own level — one tree can mix solo,
  parallel, scout, and further delegate branches (*heterogeneous* decomposition). It is
  double-bounded: a dedicated `Config.MaxPlanDepth` (default 2, tighter than `MaxDepth`) caps
  planner recursion, and it fires only for producing agents (`producesFiles` = allows
  `write`/`edit`, **not** bash — a run-only tester/verifier never re-plans), off the
  interactive path, and not in workflow mode. The consensus council runs at **depth 0 only**:
  a parent that merely delegated verifies the *merged* working tree — leaves don't each re-run
  the full council; the parent verifies the aggregate.
- **Recursion policy** (planned decomposition first; `guardExpansion` + `planEnvelope` in
  `app/planner.go`): the default is *up-front* hierarchical decomposition with just-in-time
  sub-planning, not ADaPT's *as-needed* (reactive) re-decomposition. Two deterministic guardrails
  (always on, they only downgrade `refine`→`solo`) run in `maybePlanPreflight` after sanitize and
  after the audit gate: **(1) depth cap** — at `depth+1 >= MaxPlanDepth` a `refine` step could never
  be expanded (a child re-plans only while `depth < MaxPlanDepth`), so it is downgraded to inline
  work; **(2) no pure re-deferral** — a `depth >= 1` plan (itself a refine expansion) that is all
  `refine` with no concrete work step (`solo`/`delegate`) is downgraded, so every expansion makes
  real progress. The planner is *told* these constraints up front via `planEnvelope`, which injects
  its step budget (`maxSteps`) and depth/cap into the planner system prompt so it right-sizes the
  plan (and omits `refine` at the cap).
- **Reactive failure re-decomposition** (ADaPT, `executeSteps`/`runRefineStep`, gated by the
  `MAGI_ADAPT` env knob — default *on*): when a delegate returns an error or empty result and we're
  still below the plan-depth cap (with budget left), it is retried **once** with a
  decomposition-framed prompt telling the same executor to break the sub-task into smaller
  independent steps (the child re-plans smaller — the natural decomposition point); refine gets
  `refineLocalRetries` informed retries the same way. With `MAGI_ADAPT=0` both collapse to a single
  shot — a failed node backtracks instead of re-decomposing, leaving only planned decomposition and
  the stall net (`redecomposeStuck`). Either way, if a delegate ultimately fails the step's todo
  stays `pending` and is recorded as `(delegate FAILED — do this yourself)` so the redo-prevention
  directive can't suppress it.
- **Hierarchical refine** (`runRefineStep`, ADaPT/HTN backtracking): where `delegate` partitions
  *independent* chunks to context-free children, `refine` recurses on a large sub-goal whose
  pieces **depend on each other**. By default (`sharedRefineEnabled`) a plan's sequential refine
  phases run in **one shared child session**: the first phase seeds it by **cloning** the parent
  (`SpawnRequest.CloneContext` → `cloneConversation`); later phases and local retries **reuse** it
  (`SpawnRequest.ReuseSession`, threaded via `SpawnResult.SessionID` and the `refineShare` state),
  so each re-plans at depth+1 on top of its predecessors' **actual conversation** (tool calls,
  outputs, code) rather than a spawn-time snapshot. `MAGI_REFINE_SHARED=0` restores the legacy
  per-phase clone-at-spawn. Session sharing is accounting-neutral (each phase is still one
  `spawn`/`runAttempt`: depth+1, budget, supervisor) and council-safe (change capture is per
  `runLoop` invocation via `newRunGuard`, not per session). It drives a local re-plan → escalate
  loop: **success** completes the todo and flags `delegated` (the depth-0 council verifies
  the merged tree); **failure** records the reason into the *parent* context (`recordRefineFailure`)
  and retries locally up to `refineLocalRetries` — the failure reason is prefixed onto the retry
  prompt so the attempt is *informed* ("a previous attempt failed because X"), and under the shared
  session the retry also runs on top of the failed attempt's conversation; **exhaustion** (or an
  explicit child `STATUS: FAILED`, which backtracks early) leaves the todo `pending` and returns a
  FAILED finding, so the parent — itself possibly a refine node — re-approaches with the accumulated
  failures in view ("no more to try → backtrack up"). A refine step's `agent` is *optional* (it
  states a goal, not who runs it): `resolveWriteExecutor` falls back to any delegatable agent
  (pinned on the first phase for a consistent shared session), and refine degrades to solo only when
  none exists. **Sibling visibility**: sequential phases see each other's real work *structurally*
  through the shared session; each success additionally seeds a compact result note into the parent
  (`recordRefineSuccess`, the mirror of the failure record) as the summary the parent reads (and the
  visibility fallback when `MAGI_REFINE_SHARED=0`). The plan council must not reject a `refine` plan merely for being abstract
  (that is the point of hierarchical decomposition), but still rejects genuinely unsound plans by
  member-lens judgment.

- **Spec fidelity** (`specFidelityEnabled`, `MAGI_SPEC_FIDELITY`, default on): deep planning
  paraphrases the instruction, and the executor then normalizes a *literal* the grader checks
  verbatim — the request's `value` field became `val`, failing kv-store-grpc, where a shallow/solo
  run that reads the raw instruction directly keeps `value`. Three defenses fire together: a planner
  **literal-preservation rule** (`literalRule`, appended to the planner system prompt — copy exact
  identifiers/formats/thresholds into the step title/task verbatim); a **plan-time note**
  (`specFidelityNote`, injected into the main session right after `registerPlanTodos` and *before*
  `executeSteps`, so refine clones and the findings-synthesis path inherit it via the parent context,
  and an all-solo plan is covered too); and a **verbatim SPEC anchor** for the context-free delegate
  child (`delegateBrief` carries the goal as an authoritative SPEC, generously clipped, since the
  child never sees the raw request). `MAGI_SPEC_FIDELITY=0` restores the paraphrase-only baseline.

- **Plan-tree hierarchy (normalized B-variant)**: when a delegate/refine step's child forms its own
  sub-plan at depth+1, the TUI plan panel renders the child's sub-todos **indented under the parent
  step**. Structure is a one-time immutable fact — the child's `SessionCreated` event carries
  `ParentStep *int` (the parent plan-step index it was spawned from; nil = not a plan-step child, e.g.
  council/scout-list/stuck-redecompose), threaded `PlanStepIndex` through `SpawnRequest` →
  `runAttempt` → `Session.ParentStep`. `SpawnRequest.MaxSteps` (>0) caps a child's per-attempt
  loop steps below the configured default — used by the stuck-recovery units, whose task is
  deliberately a small slice of the whole. State stays **single-source**: each session owns its own todos
  (no mirroring into the parent), so failure backtracking needs no cross-tree resync. `App.PlanChildren(parent, step)`
  joins children by `Parent==parent && *ParentStep==step` in creation order; `renderPlan(sid, depth)`
  recurses, depth naturally bounded by `MaxPlanDepth`. Purely additive: with no child sub-todos the
  panel renders exactly as before, so no A/B flag. (Shared-session refine keeps the first phase's
  step — a reused child is not re-attributed.)

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
recovers imprecise paths by basename and renders each line as `N#hh|content` — the
1-based line number, a 2-char content hash of that line, then the text — so a later
edit can address a line by hash. `edit` takes **either** a text match (`old`/`new`:
exact → line-ending-normalized → trailing-whitespace-tolerant, leading indentation
never guessed, plus a salvage tier that strips pasted `N#hh|` read prefixes before
retrying) **or** an **anchor** (`at:"N#hh"`, optional `to:` for a line range): the
anchor recomputes the hash from the *current* file content and rejects a stale or
mismatched reference — a deterministic guard against editing a line that has since
moved or changed underneath a stale read. `write`/`edit`/`multiedit` additionally
append a **non-blocking advisory** when freshly added comments read like
change-narration ("// I've updated the loop …") or placeholders/elisions
("// rest of the code unchanged", "// …") — comments should capture non-obvious
intent, not narrate the diff; the edit still applies. `findcontext` ranks by symbol
definition + path + content coverage; `astgrep` is structural (AST) search via the
external `ast-grep` CLI (shells out, no CGO) and degrades to a "use grep/findcontext"
message when the binary is absent; `lsp_diagnostics` reports LSP diagnostics (type
errors, unused/undefined symbols, …) for a file in **any supported language** — Go
through the gopls CLI, every other language (Python, Rust, TypeScript/JS, C/C++, and
the long tail `serverFor` knows) by opening the file in its language server and
reading the pushed `textDocument/publishDiagnostics` — errors and warnings only,
degrading to a "build/run the project" suggestion when no server is installed.

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
