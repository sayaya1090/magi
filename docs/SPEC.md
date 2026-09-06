# magi — Feature specification (historical)

[English](SPEC.md) · [한국어](SPEC.ko.md) · [↑ Docs](README.md)

> ⚠️ **This is a history document.** Much of what it describes — the procedural planner, delegation
> to subagents and curated workers, authored acceptance checks and step gates, the council that
> decided termination by vote, the subagent lease — has been torn out. **The current as-built
> reference is [`ARCHITECTURE.md`](ARCHITECTURE.md)** and the user-facing one is
> [`MANUAL.md`](MANUAL.md); where they disagree, those win. This is kept as the record of what was
> decided and on what grounds.

> The daemon, the fleet view and the web console (`clients/web/server`) are not in here at all — they
> were built after this document stopped being maintained. See [`ARCHITECTURE.md`](ARCHITECTURE.md)
> §11 and [`MANUAL.md`](MANUAL.md) §12.

> Each feature = **rules (R)** + **example cases**. The examples are `given → when ⇒ then` (in code
> blocks), one-to-one with a row of a Go table test. The case id (`read-1` and so on) is how you
> find the test that holds a rule: **search the id in Go source.**
>
> That search is guaranteed for **Part A only**, and by a test — `internal/spec/probes_test.go`
> fails if a Part A id appears in no `.go` file, and the handful it cannot find are listed in it
> with the reason for each. Two things it does not promise: the id is usually in the comment above
> a test rather than in its name, and **Part B ids are mostly not in the code at all** (measured
> 2026-08-29: of 88 ids, 44 appear nowhere). Outside Part A, finding nothing means nothing.
>
> Code blocks rather than tables, because a backtick, brace or newline inside a cell breaks markdown
> table rendering. Notation: `\n` = newline, `ok` = IsError:false, `ERR("...")` = IsError:true with
> the message contained.
>
> **Part A = M1 (in depth)** / **Part B = later milestones (outline)**.

---

# Part A — M1 features

## F-TOOL — the built-in tools (Go, no POSIX dependency)

Shared rules:
- C1 Paths are relative to the session `workdir`, normalized internally with `filepath`.
- C2 **Access outside the workdir tree is refused by default** (absolute paths included) →
  `ERR("outside workdir")`.
- C3 Errors come back as a result, never a panic: `ToolResult{IsError:true, Content:"<reason>"}`.

### F-TOOL-READ — reading a file
Rules:
- R1 An existing file → its contents.
- R2 `offset`/`limit` (1-based line numbers) → only that line range.
- R3 A missing file → `ERR("file not found")`.
- R4 A directory → `ERR("is a directory")`.
- R5 Binary (contains a NUL byte) → `ERR("binary file")`, contents not read.

```
read-1: file a.txt="hello\nworld\n"      → read{path:"a.txt"}                 ⇒ "hello\nworld\n", ok
read-2: file a.txt="hello\nworld\n"      → read{path:"a.txt",offset:2,limit:1} ⇒ "world\n", ok
read-3: (no file)                        → read{path:"nope.txt"}              ⇒ ERR("file not found")
read-4: dir "sub/"                       → read{path:"sub"}                   ⇒ ERR("is a directory")
read-5: file img.png has NUL byte        → read{path:"img.png"}               ⇒ ERR("binary file")
read-6: file outside="/etc/passwd"       → read{path:"/etc/passwd"}           ⇒ ERR("outside workdir")
```

### F-TOOL-WRITE — writing a file (create / overwrite)
Rules:
- R1 Creates a new file. A missing parent directory is **created automatically**.
- R2 An existing file is **overwritten whole**.
- R3 Outside the workdir → ERR.
- R4 On success, reports the byte count and the path.

```
write-1: (empty workdir)        → write{path:"new.txt",content:"hi"}      ⇒ ok, file new.txt=="hi"
write-2: (no dir x/y)           → write{path:"x/y/z.txt",content:"a"}     ⇒ ok, dirs created, z.txt=="a"
write-3: file old.txt="old"     → write{path:"old.txt",content:"new"}     ⇒ ok, old.txt=="new"
write-4: (any)                  → write{path:"../escape.txt",content:"x"} ⇒ ERR("outside workdir")
```

### F-TOOL-EDIT — exact string replacement
Rules:
- R1 `old` present **exactly once** → replaced with `new`.
- R2 Zero occurrences → `ERR("not found")`.
- R3 Two or more → `ERR("not unique")` (unless `replaceAll:true`, which replaces all).
- R4 `old==new` → `ERR("no change")`.
- R5 The file's **existing EOL (CRLF/LF) is preserved**.

```
edit-1: "foo bar baz"     → edit{old:"bar",new:"BAR"}                ⇒ "foo BAR baz", ok
edit-2: "x x x"           → edit{old:"x",new:"y"}                    ⇒ ERR("not unique")
edit-3: "x x x"           → edit{old:"x",new:"y",replaceAll:true}    ⇒ "y y y", ok
edit-4: "abc"             → edit{old:"zzz",new:"y"}                  ⇒ ERR("not found")
edit-5: "abc"             → edit{old:"abc",new:"abc"}                ⇒ ERR("no change")
edit-6: "a\r\nb" (CRLF)   → edit{old:"a",new:"A"}                    ⇒ "A\r\nb", ok (CRLF kept)
```

### F-TOOL-GREP — regex search
Rules:
- R1 Searches contents by regex. The result is a list of `path:line:content`.
- R2 `glob`/`path` narrow the scope.
- R3 No match → an empty result (ok, not ERR).
- R4 An invalid regex → `ERR("invalid regex")`.
- R5 Binary files are skipped.

```
grep-1: a.txt="foo\nbar\nfoobar"          → grep{pattern:"foo"}             ⇒ ["a.txt:1:foo","a.txt:3:foobar"], ok
grep-2: a.txt="foo", b.go="foo"           → grep{pattern:"foo",glob:"*.txt"}⇒ ["a.txt:1:foo"], ok
grep-3: a.txt="foo"                       → grep{pattern:"zzz"}             ⇒ [], ok
grep-4: (any)                             → grep{pattern:"[("}             ⇒ ERR("invalid regex")
```

### F-TOOL-GLOB — path pattern matching
Rules:
- R1 A glob pattern → a list of paths, **sorted** (deterministic).
- R2 `**` matches recursively.
- R3 No match → an empty list.
- R4 Hidden files excluded by default; `.gitignore` honoured (optional).

```
glob-1: a.go, b.go, c.txt                 → glob{pattern:"*.go"}        ⇒ ["a.go","b.go"]
glob-2: src/x.go, src/sub/y.go            → glob{pattern:"src/**/*.go"} ⇒ ["src/sub/y.go","src/x.go"]
glob-3: a.txt                             → glob{pattern:"*.md"}        ⇒ []
```

### F-TOOL-LIST — directory listing
Rules:
- R1 Entries are `{name,isDir}`, sorted (directories first, then by name).
- R2 A missing path → ERR.
- R3 Listing a file → `ERR("not a directory")`.

```
list-1: dir/{b.txt, a/(dir), c.txt}       → list{path:"dir"}   ⇒ [a/(dir), b.txt, c.txt]
list-2: (no path)                         → list{path:"nope"}  ⇒ ERR("not found")
```

---

## F-STORE — event-sourced persistence (the jsonl adapter)

### F-STORE-APPEND — append and seq assignment
Rules:
- R1 Assigns and returns a **monotonic per-session seq** (from 1).
- R2 Concurrent appends never collide or duplicate a seq (serialized).
- R3 **One line = one event** in the JSONL file.
- R4 Transient events are not appended.

```
append-1: empty session s1   → Append(session.created)                ⇒ seq=[1], file has 1 line
append-2: s1 (seq=1)         → Append(prompt.submitted, part.appended)⇒ seq=[2,3], file has 3 lines
append-3: s1                 → 100x Append concurrently (goroutines)  ⇒ all seq unique, no gap/dup
```

### F-STORE-READ-REPLAY — reading and replay
Rules:
- R1 `Read(s,fromSeq)` returns events in ascending seq.
- R2 `fromSeq=0` → everything; `fromSeq=N` → seq>N (reconnect / late joiner).
- R3 Replay reconstructs Session/Message/Part (F-EVENT-RECON).
- R4 Identical after a process restart (persisted).

```
read-replay-1: s1 has seq 1..4           → Read(s1, 0)  ⇒ 4 events, seq 1,2,3,4
read-replay-2: s1 has seq 1..4           → Read(s1, 2)  ⇒ 2 events, seq 3,4
read-replay-3: write s1, reopen Store    → Read(s1, 0)  ⇒ same 4 events (persisted)
```

### F-STORE-COMPACT — log compaction
Rules:
- R1 `Compact(s, upToSeq, snapshot)` writes a **new file** with everything up to upToSeq replaced by
  one snapshot.
- R2 The original is archived (`.archive`) or discarded (optional).
- R3 After compaction, Read returns the snapshot plus later events.

```
compact-1: s1 has seq 1..10  → Compact(s1, 7, snap)  ⇒ Read(s1,0)==[snap, seq8, seq9, seq10]
```

### F-STORE-LIST — session listing
Rule: `ListSessions(workdir)` returns that workdir's session metadata (id, created, lastActivity,
title), newest first.

```
list-sessions-1: /proj has s1,s2; /other has s3  → ListSessions("/proj")  ⇒ [s2, s1] (s3 excluded, newest first)
```

---

## F-EVENT — the event model

### F-EVENT-FACT-TRANSIENT — facts vs transients
Both sets below are the WHOLE vocabulary, not a sample. A client reads the log and the bus by
these names, so a name missing here is a line it will meet and not know, and a name here that the
code does not have is a line it will wait for for ever. `vocab-1` holds them to `event.go`.

What `vocab-1` holds is R1 and R2 — WHICH TYPES MAY SIT IN THE LOG. It says nothing about which
frames arrive carrying a seq, and a reader who takes the sets for a cursor rule will be wrong on
the type R4 names. The two questions look like one and are not.

Rules:
- R1 The persisted types (`session.created` / `prompt.submitted` / `part.appended` /
  `permission.decided` / `compaction` / `result.elided` / `turn.finished` / `todos.changed` /
  `labels.changed` / `error` / `council.convened` / `council.verdict` / `council.decided` /
  `interjection.deferred` / `interjection.answered` / `prompt.abandoned` / `session.moved` /
  `model.changed`) are written to the Store.
- R2 The transient types (`part.delta` / `tool.progress` / `permission.requested` /
  `question.requested` / `context.usage` / `workflow.phase` / `council.deliberating` /
  `question.answered` / `user.label.changed`) go to the bus only and are never recorded.
- R3 Every envelope (seq/sessionId/type/actor/ts/data) round-trips through JSON losslessly.
- R4 The two sets say which types the Store MAY hold — not which frames arrive carrying a seq. A
  client reads `seq == 0` off the envelope it has and never infers it from the type: one type can
  arrive both ways. `model.changed` is appended (and so seq-stamped) when the App has a Store and
  merely announced (seq 0) when it does not, from the same call.

```
fact-1:      bus.Publish(part.delta)        ⇒ Store unchanged (not persisted)
fact-2:      app completes a part           ⇒ exactly 1 part.appended line in Store
roundtrip-1: Event → JSON → Event           ⇒ deep-equal to original
vocab-1:     R1 and R2 above                ⇒ exactly event.go's fact consts / transientTypes
seq-1:       SetModel, no Store             ⇒ model.changed on the bus with seq 0 (R1 type, no seq)
seq-2:       SetModel, with a Store         ⇒ the same call, the same type, on the bus WITH a seq
```

### F-EVENT-RECON — log → conversation
Rule: group `part.appended` by messageId and rebuild Message[]/Part[] in seq order. Only what
follows the compaction marker is context.

```
recon-1: log = [session.created, prompt.submitted(user "add a test"),
                part.appended(assistant tool-call read),
                part.appended(tool result)]
         ⇒ Session{1 msg user + 1 msg assistant(tool-call) + 1 msg tool(result)}
```

---

## F-LLM — the OpenAI-compatible adapter (Ollama/vLLM/LiteLLM)

### F-LLM-SSE — stream parsing
Rules:
- R1 OpenAI SSE (`data: {...}\n\n`) maps to `ProviderEvent`.
- R2 `choices[].delta.content` → `text-delta`.
- R3 `data: [DONE]` → `finish`.
- R4 A `usage` chunk → `usage`.
- R5 A broken JSON line is skipped; the stream continues.
- R6 **The end is finish_reason**, not `[DONE]`: some backends (the Ollama cloud gateway) delay or
  omit `[DONE]` while holding the connection open, which left the reader hanging until the wall
  clock. Finish on finish_reason (plus trailing usage), with an epilogue grace
  (`streamEpilogueGrace`) as the backstop.
- R7 **The stall watchdog** (`consumeStream`; two bounds, split on purpose): a backend that accepts
  the request, returns 200 and then **sends no event at all** is detected by idle time (since the
  last event) and aborted — sealing a main generate whose read hung until the turn's wall clock
  (observed: a silent hang on cobol-modernization). It **resets on every event**, so a slow
  generation streaming tokens or reasoning never trips it. The two silences mean different things
  and get different bounds: **before the first token** the wait is dominated by PREFILL — minutes
  on a strong local model against magi's ~20k-token prompt — so it is bounded by
  `firstTokenTimeout` (default 300s, `MAGI_FIRST_TOKEN`; 0 = no separate bound, the inter-token
  bound applies from the start), sets `streamStep.stalled`, and the main loop re-issues the same
  request (`maxStreamStallRetries`=2, safe because nothing was committed), erroring when exhausted.
  A **freeze mid-generation** (after output began) is bounded by `streamStallTimeout` (default
  120s, `MAGI_STREAM_STALL`, 0=off), aborts, keeps the partial output and does not retry. The
  council's per-member deadline adds the first-token value on top of its 3 minutes for the same
  prefill reason.
- R7b **One guard for all model I/O** (`guardedProvider`, `provider_guard.go`): **every request to a
  model** — the main generate and every side call — goes through the single `StreamChat` chokepoint
  of a provider wrapped at construction by `GuardProvider` (everything `providerFor` returns is
  guarded; this replaced the whack-a-mole of per-consumer watchdogs). R7's `consumeStream` (the
  behavioural guard: stall-retry, reasoning spin) fires **first on the main generate**, and
  guardedProvider is the **safety net above it** (thresholds doubled) backstopping paths with no
  handling of their own. It cancels three failure modes: a **silent backend** (idle ≥ 2× the larger of
  `streamStall` and the first-token bound — 600s default, so a legitimate prefill is never killed
  by the net beneath the handler), **byte-spin** (no completion past 2× `spinCap`), and **degenerate repetition** (a
  short unit repeated back-to-back in the tail, ≥128B and ≥3 times — the same sentence or word
  looping; `MAGI_REPEAT_CAP` on by default, checking a 4KB tail every 256B, so it stops in hundreds
  of bytes rather than waiting for the ~800KB byte cap). A pure-whitespace unit (a blank line) does
  not count as repetition, and a non-repeating tail mismatches on the first comparison, so the scan
  is cheap.

### F-LLM-FALLBACK — tool calls without native support
Rules:
- R1 For a model with no native support, the system prompt instructs it to emit tool calls in an
  agreed JSON shape.
- R2 That shape is parsed out of the assistant text into a `tool-call`.
- R3 A violated or partial shape gets one repair re-request; if it still fails, it is treated as text.
- R4 The mode (native/fallback) is forced per model by config, and auto-detected otherwise.

```
fallback-1: assistant outputs fenced block:
              tool_call { "name":"read", "args":{"path":"x"} }
            ⇒ {tool-call, read, {path:"x"}}
fallback-2: assistant outputs an ordinary sentence      ⇒ text part, no tool-call
fallback-3: assistant outputs broken JSON               ⇒ 1 repair retry; if still bad → text part
```

> ⚠️ This area needs **both** mock-SSE fixture unit tests **and** a live integration test against a
> real Ollama model. Fixtures alone miss real-model tool-calling bugs.

### F-LLM-ERROR — error handling
```
llm-err-1: HTTP 500 from server        ⇒ {error} event, propagated to loop
llm-err-2: connection drops mid-stream ⇒ {error} event, partial parts preserved
llm-err-3: invalid base URL            ⇒ StreamChat returns error immediately
```

---

## F-LOOP — the agent loop (with a fake LLMProvider injected)

### F-LOOP-STOP — ending conditions
Rules:
- R1 No tool call → the turn ends with `turn.finished`.
- R2 A tool call → execute, then the next step.
- R3 **There is no pacing ceiling — only a runaway backstop.** The graceful stop that fired on
  magi's own arithmetic came out on measurement (ARCHITECTURE §4). What remains is `MaxSteps`,
  default **240**, sized far above any productive turn: a turn that spends it lands UNVERIFIED with
  the backstop named as the reason, and the work stands as it was left. Only a workflow phase
  declares a budget of its own.
- R4 R1's quiet stop is not the end by itself — the **finish path** (`loop_gates.go`, `finishTurn`)
  runs six gates in this order: Stop hooks → the empty-result nudge → **the declaration** → the
  dropped-calls-after-the-declaration notice → outstanding hand-offs → what the answers that came back were
  worth. Any one of them sends the turn back to work. Then, when it truly ends: the optional distil
  pass (off by default), the late-interjection sweep, `finalizeTodos` (every still-open step becomes
  completed on a genuine finish and cancelled otherwise), and `turn.finished` carrying the
  UNVERIFIED reason if there is one. The council is not on that list: it is a tool the agent calls
  (Part B, F-COUNCIL), and the declaration gate only checks that it did.

```
loop-stop-1: fake replies ["hello"]                       ⇒ 1 step, turn.finished, 1 text part
loop-stop-2: fake replies [tool-call read]→["done"]       ⇒ 2 steps, tool-result part + text part
loop-stop-3: fake replies [tool-call]×N, no declaration   ⇒ finish path asks for the declaration (bounded)
```

### F-LOOP-INTERRUPT — interruption
Rule: on ctx cancellation (Interrupt) the running step stops, partial results are kept, and an
interrupted event is emitted.

```
loop-int-1: Interrupt during streaming  ⇒ stop immediately, received text persisted as part.appended
```

### F-LOOP-PERMISSION — permission gating
Rules:
- R1 A dangerous tool (write/edit/bash…) emits `permission.requested` before executing and waits for
  `RespondPermission`.
- R2 Policy `allow` → auto-approve; `deny` → auto-refuse; `ask` → the user.
- R3 An `always` answer auto-approves that (tool, session) from then on.
- R4 A refusal comes back to the model as a tool result `ERR("denied")`.

```
perm-1: policy=ask,   tool=write          ⇒ permission.requested emitted, blocks until response
perm-2: policy=allow, tool=write          ⇒ executes without request
perm-3: policy=ask, user denies           ⇒ tool-result ERR("denied"), loop continues
perm-4: policy=ask, user answers "always" ⇒ 1st write asks, 2nd write auto-allowed
```

### F-LOOP-STEER — routing a mid-run user interjection
Rule: `turnTask` (the anchor for nudges and the council) is frozen once at step 0. A second user
request arriving *mid-run* therefore never reached the anchor, and the agent oscillated, re-running
the first request it had already finished.
- R1 **The default is queueing**: a new `ActorUser` prompt seen at step>0 (and different from the
  current turnTask) goes into the `pendingInterject` FIFO, with one deterministic instruction
  injected — "your request is queued for after the current task; stay on the current task". At the
  end of the turn `startRun` drains the queue and it resurfaces as its own turn. Depth 0 and
  non-workflow only.
- R2 **`route_interjection`** (orchestrator only): `redirect` re-anchors `turnTask` to the
  interjection and regrounds; `append` joins it to the current task (A ∪ interjection) and
  regrounds; `queue` keeps it explicitly. An absorbed interjection (redirect/append) is removed from
  the queue (`consumeInterject`) so it cannot resurface.
- R4 A tool's Execute callback cannot touch loop-local state (`turnTask`, `guard`), so it records a
  per-session `turnControl` signal that the loop drains at the top of every step.
- R5 **The queue is never lost**: a queued interjection resurfaces as its own turn when the turn ends
  normally, and it is not stranded in an in-memory map when a backend error or cancellation ends the
  run goroutine — the remainder is persisted to the log as an unanswered user prompt (picked up on
  the next run) without immediately re-issuing it to a failing backend (no retry storm). The run
  goroutine's post-loop block holds `a.mu`, so it inspects and deletes the queue **inline** (never
  through a self-locking helper — re-locking would deadlock the goroutine).

---

## F-COMPACT — context compaction
Rules:
- R1 Auto-compaction when the context tokens exceed a threshold (a share of the model's window). The
  share is `[limits] compact_ratio` (default 0.8).
- R2 Older messages are summarized and appended as a `compaction` event (the originals are kept).
- R3 The context afterwards is the newest compaction summary plus everything after it. Detail the
  compaction shed is recovered with `recall_context`.
- R4 A manual `Compact` command does the same.
- R5 Folding is tiered, cheapest first (2026-09-07). Before any summary: bulky tool results the
  assistant already narrated are stubbed, newest first; then results of READ-ONLY tools (the
  builtin looks, and any MCP tool declaring `annotations.readOnlyHint`), oldest first, the newest
  three spared — re-derivable, so a stub says how to have them again. The fold runs only when
  that does not cover the overage.
- R6 The fold keeps a tail measured in tokens — `[limits] compact_keep` of the budget (default
  0.25), never fewer than six events — and its brief ACCUMULATES: the previous brief is kept
  verbatim and only the new turns are folded after it; the running brief is condensed once, as a
  whole, when it outgrows a tenth of the budget. The brief is structured (request, decisions,
  done, open, names), written in the person's language, and headed by the statement that it
  records what was done, not the state now. `[limits] compact_model` may name the model that
  writes it.

```
compact-ctx-1: history over threshold → next turn       ⇒ 1 compaction event, request message count drops
compact-ctx-2: after compaction       → Read(s,0)       ⇒ full history still retrievable (preserved)
compact-ctx-3: Compact command issued                   ⇒ immediate compaction event
```

---

## F-HEADLESS — `-p` headless mode
Rules:
- R1 `magi -p "<prompt>"` creates a session, runs one turn, prints the result to stdout.
- R2 `--output text|json` (text by default). json is a JSONL event stream.
- R3 **Non-TTY detection** disables the TUI, colour and spinners (CI-safe).
- R4 Exit code: 0 on success, non-zero on error.
- R5 A prompt can be piped in on stdin.
- R6 **A council objection's evidence is its body, not its tally**: the headless log printed only the
  tally (`council round N: continue — a/b`), and the demand that produced that continue flowed only
  through the prompt injected on the next turn, where it hit the `PromptSubmitted` note's **200-char
  truncation** — and the keep-list advice sits *above* the feedback and consumed those 200 chars
  first, so the demand that kept the turn open **appeared nowhere in the log** (observed: the subject
  of a three-round continue was recoverable only from the model's paraphrase). So `CouncilDecided`
  renders **the feedback body itself** as its own case — line by line (≤12 lines × ≤200 chars,
  with a "feedback continues" tail), the way the `PlanRevised` diff already does for the same reason.
  The log is the only record a post-mortem has; a tally is a summary, not evidence.

```
headless-1: magi -p "hi" --output json     ⇒ JSONL events to stdout, exit 0
headless-2: echo "hi" | magi -p -          ⇒ reads prompt from stdin
headless-3: run via pipe (non-TTY)         ⇒ no ANSI color codes in output
headless-4: LLM error                      ⇒ message to stderr, exit != 0
headless-5: council continue + feedback    ⇒ body rendered line by line under the tally
headless-6: very long feedback             ⇒ cut at 12 lines + "feedback continues" (no log flood)
```

---

# Part B — later milestones (outline, to be expanded to Part A's depth on entry)

> Do not over-specify now (the design still moves). Add rules and examples on entry.

## F-COUNCIL — the council the agent calls (D14)
The signature feature. Three members read the same record through different lenses and answer. **On
by default** — turn it off with `[council] enabled=false`.

> ⛔ **This was inverted once.** The council used to intercept the loop's natural stopping point as a
> **gate that convened itself**. That placement decided two things it could not get right: **when**
> it was asked (the one moment the agent had already made up its mind), and whether its answer would
> be read at all (in a headless run the advice was injected and `turn.finished` written in the same
> tick). Now **the agent calls the `council` tool**: `{question}` is advice, `{complete:true}` is the
> **finish declaration**, and the loop is signalled when the members accept. R5, R6 and R8 below are
> rewritten to match.

Rules:
- R1 `core/council.Tally(verdicts, rule)` is a **pure function** — same input, same output, no I/O.
- R2 Consensus rules: `unanimous` (all done) · `majority` (done>50%) · `quorum:k` (done≥k) ·
  `weighted:θ` (done weight / total weight ≥ θ) · `veto` (a named member's refusal overrides done).
- R3 **A tie or a missed quorum → continue** (never an early finish). `abstain` is excluded from the
  denominator.
- R4 A member is `{Name (label), Lens (attribute), Model, Weight}`. The default three: Melchior
  (correctness), Balthasar (verification), Casper (completeness).
- R5 `decision==continue` → the members' feedback (`AggregateFeedback`) comes back **as the tool
  result**, with a sentence saying the declaration was not accepted. An advisory call (`{question}`)
  returns the reading with no decision, and the tally is not rendered — counting votes is what the
  gate did, and a count invites reading a majority as an order.
- R6 Safety: a deliberation happens only when the agent asks for one, so **a round runaway is
  structurally impossible**. What is bounded instead is the finish path's demand for a declaration
  (`requireFinishDeclaration`): **three asks per stretch of no progress**, and a real file mutation
  since the last ask restarts the budget.
- R7 Events: `council.convened`, `council.verdict` (per member) and `council.decided` are persisted;
  `council.deliberating` is transient.
- R8 **Only a working turn is asked to declare**: a conversational turn that used no tools (a
  greeting, a question) skips the demand, so small talk cannot be trapped in a declaration loop.
- R9 **How members vote**: a member votes `continue` only when its lens finds a **concrete, real
  defect** (a failing signal, a contract the report shows unmet, a plain error), naming the next step
  in its feedback; `done` when the task is reasonably satisfied; `abstain` when its lens cannot
  judge. **The absence of evidence is never itself grounds for `continue`** — an investigation, a
  read, an analysis or an answer turn has no diff by nature, and demanding an artifact that was never
  going to exist is the main source of chronic churn. Use evidence when it is there; otherwise judge
  from the report and the task, or abstain. The diff in the council's evidence **includes the
  contents of new untracked files** (a temporary `GIT_INDEX_FILE` index, leaving the real index
  untouched), so a freshly created file is visible as evidence.
  - **R9a State the objective, delegate the method**: when a member invents a verification procedure
    of its own (a council-made demand, *not* a literal contract the task stated), the feedback
    carries **only what must be shown to be true** and leaves **how to check it to the agent** — no
    pinning a particular inspection command (`ps`/`netstat`/`lsof`/`curl`). If that tool is absent
    from the environment, the objective stays unmet forever even once it is satisfied (observed on
    kv-store-grpc run17: `ps: not found`). **An end-to-end functional success already satisfies "it
    must respond/run"** — demanding a process or port listing on top of that is ritual churn, and the
    functional success is the stronger evidence. (When the **task states a literal command, input or
    number**, demand exactly that — the brief-paraphrase false-done defence stays.)
  - **R9b The agent may CONTEST — removal only**: when a continue is injected the agent gets an
    affordance. If one of the council's demands is **already satisfied by evidence already shown**,
    or its **stated method is impossible in this environment** while its objective is already met,
    the agent does not comply pointlessly — it answers with one line,
    `CONTEST: <demand> — <concrete evidence that it is met or impossible>`. The council **weighs that
    evidence** in the next round: if it holds, that demand is **not reissued** (vote done, or name a
    *different* real defect); if it does not (a bare re-assertion with no concrete evidence), it is
    ignored. **A contest removes that one item; it is not itself evidence the task is done** — every
    other demand is judged on its own merits and done remains the council's independent decision.
    That isolation is what preserves the council's reason to exist (blocking a false done).
- R10 **The no-change turn signal (NoChanges)**: when the diff is (successfully) empty and there are
  no signals, the turn is a **read-only / investigation / answer turn with nothing changed**, and the
  council is told so through `DeliberationRequest.NoChanges` — the members then know there is no
  artifact to verify and approve a reasonable report (R9). **The consensus rule is unchanged** (no
  relaxation, no quorum:1): when the deliberation runs, it is always a real consensus. A **failed
  GitDiff** (a non-git workdir) is *not* read as "no changes", so a real write turn is not
  misjudged. If every member abstains, the no-progress guard ends the turn.
- R11 **After the independent vote** (each flag on by default): ① **the rebuttal round**
  (`MAGI_COUNCIL_DEBATE`) — when a would-be-done is SPLIT, members are polled once more (each seeing
  the others' verdicts and reasons, free to hold or change) and re-tallied. ② **keep**
  (`MAGI_COUNCIL_KEEP`) — members also name **what is already right**, carried as advice in the
  continue feedback (never affecting the decision or the tally). ⛔ The **devil** flag
  (`MAGI_COUNCIL_DEVIL`) that stood here does not exist in the code.

> ⛔ **R12 (typed deliverable checks, ①–⑦) and R13 (contract-first, three stages) were deleted.**
> Check authoring, the validation pass, the step gate, coverage filling, the churn landing,
> substitution, and the council round that authored a contract before the plan — none of it is in
> the code. Only the name `verifyStepChecks` survives on the finish path, and it does something else.
> Why it came out: [`ARCHITECTURE.md`](ARCHITECTURE.md) §4 — every one of those stages decided
> something before the work existed.

- R14 **Reading a member's reply — an abstention is not a neutral outcome** (`parseReply`,
  `jsonx.SalvagePrefix`, `councilRetryReminder`): a reply that cannot be read records that member as
  **abstaining**, and the tally **cannot tell that apart** from "my lens has nothing to say" — a cast
  vote quietly disappears and the remaining minority decides. So a read failure is defended three
  ways. ① **Tolerant parsing** (every balanced object × the `jsonx` repair candidates × per-field
  tolerant types) — Go abandons the whole document on the first type mismatch, so one field's shape
  swallows a vote. ② **Prefix salvage** (`jsonx.SalvagePrefix`): a model's structural mistake is not
  uniform across the document but **confined to one container** — measured, 11 of 11 the same shape:
  a `criteria` array closed with the next key instead of `]`, breaking at byte 563 of 567 and taking
  a `decision` completed at byte 12 down with it (once a `critical` continue). Keep everything
  **before** the syntax error (applying the repair candidates first, so a raw newline inside a
  multi-line string is not mistaken for the cut point, and rewinding to the last **complete**
  element so a half object is dropped) and close the open containers. If `decision` sits *after* the
  defect there is **no vote to save, so the member abstains** — a vote is never invented. This
  recovery is **lossy**, so it is deliberately **not** wired into `jsonx.Unmarshal`/`RepairCandidates`
  (the shared path): a plan truncated at its third step would otherwise succeed quietly as a
  "two-step plan" (the same line `CloseTruncated` draws by living only in the span extractor). The
  loss is stated on stderr **together with the defect diagnosis**. ③ **The single retry reminder is
  shaped to the failure** (`councilRetryReminder`): one reminder assuming every failure was "wrapped
  in prose" was itself the defect — it told a model that had sent a bare object with a mismatched
  array to strip prose it had never written, and in measurement the retry produced the same
  malformation and lost the vote. Now the `jsonx.Diagnose` magi **already computes for the log** (the
  offset plus a `⟪HERE⟫` window) is fed back to the only party that can do anything about it: a
  syntax error gets the position and "close the `[` before the next key"; a schema failure (it parses
  but has no `decision`) gets the required field named; anything else gets the plain JSON-only note.

- R15 **The requirements walk precedes the verdict** (`memberPrompt`, `verdictSchema`,
  `panelSchema`): a member writes `checks[]` — one line per requirement the task states, each
  `<requirement> - SATISFIED|UNSATISFIED - <verbatim fragment of a tool result, or NO-EVIDENCE>` —
  and the field sits **before** `decision` in the schema it fills in, so a reading cannot be
  assembled backwards from a conclusion already reached. What may settle a line is something a
  **tool returned**; the agent's account of its own work settles nothing, and `NO-EVIDENCE` is a
  recorded answer rather than a gap a member may quietly pass over. The walk is **unconditional** —
  not gated on the keep flag or on the presence of a diff, because the turn with no artifact (R10)
  is exactly the turn where the temptation to conclude first is strongest.
- R16 **Each lens has a route: an order of search, not a jurisdiction** (`core/council.Routes`,
  `RouteFor`). `correctness` walks the task's literal words, then the premises the work rests on,
  then **the values themselves** — whether a reported number is one the task's *subject* admits —
  and is told the two answers that will be offered for a suspect number, both traps: **consistency**
  (values from one input agree whether that input was read right or not; an equal-factor agreement
  is the *symptom* of one upstream cause, not evidence) and **the agent's own explanation** of why
  the number only looks wrong (part of the claim under examination, admissible only where a tool
  returned something showing it true). `verification` walks the behaviours — for each thing that
  must work, the moment it ran and the output that came back. `completeness` walks the parts,
  including the one named once in passing. **All three still judge the whole task**: partitioning
  jurisdiction would be worse than no route at all, because a defect inside one member's slice draws
  one continue against two uninformed dones and the rule waves it through. An unrecognized lens gets
  a neutral route. Why routes exist at all: measured over one arm, three members with one line of
  lens apiece and every other instruction identical voted done **21 of 21** with no dissent — three
  samples of one opinion.
- R17 **One panel call, then a closing call whose conclusion only tightens** (`samePanelBackend`,
  `pollPanel`, `panelCloseAsk`, `closeSaid`).
  - **One call** carries every member's walk and verdict when the members share provider *and*
    model; members pinned to different backends keep the per-member shape, because folding a
    deliberately mixed council into one request would answer with whichever backend the first member
    named. One call means **one deadline for all three**: a panel cut off is recorded as *did not
    answer* (distinct from *could not be understood*), never as a partial round.
  - **The closing call** is a different question over different material. It is the only reader that
    sees all three walks at once, and it is asked for what only that view shows: a **contradiction**
    between two readings of the same output, a **requirement no walk covered**, and a **value wrong
    on its face** (a backstop for R16's route, not a second owner of it). Two earlier shapes
    established this by failing — re-reading the same evidence with the same prompt produced 11
    convenings with no disagreement, and a mechanical re-read with the report removed produced 4
    more with none.
  - **The clamp**: `close==continue` over a `done` tally turns the round to continue; the reverse is
    never applied. This council's measured failure mode is over-approval, so a conclusion free to
    overrule a *blocking* tally would be a second road to done rather than a check on the first. The
    ask is therefore phrased neutrally — naming both answers, neither as the point of asking.
  - **It is recorded either way** (`Deliberation.Close`, rendered above the lead in
    `renderCouncilAdvice`, and one stderr line per round saying *agreed with* or *DISAGREED with*):
    an arm that never sees the line cannot tell a conclusion that agreed from one that never ran.

```
council-tally-unanimous-1: rule=unanimous, [done,done,continue]      ⇒ continue
council-tally-majority-1:  rule=majority,  [done,done,continue]      ⇒ done
council-tally-tie-1:       rule=majority,  [done,continue]           ⇒ continue (tie → continue)
council-tally-veto-1:      rule=veto(Balthasar), [done,done, Balthasar=continue] ⇒ continue
council-tally-abstain-1:   rule=majority,  [done, abstain, continue] ⇒ continue (abstain out of the denominator → 1/2)
council-gate-continue-1:   decision=continue ⇒ feedback returns as the tool result, the turn goes on
council-gate-skip-1:       a turn that used no tools ⇒ no declaration demanded          (R8)
council-abstain-noevid-1:  verification lens + no signals or diff ⇒ abstain (no reflexive continue) (R9)
council-evidence-newfile-1: a new untracked file ⇒ its contents are in the diff ⇒ converges to done (R9)
council-noevid-noContinue-1: absence of evidence alone ⇒ judge from report/task, or abstain (R9)
council-objective-not-method-1: the prompt demands the objective only, accepts end-to-end success (R9a)
council-contest-affordance-1: a continue carries the CONTEST affordance; valid evidence retires that demand (R9b)
council-nochanges-1:       diff succeeds and is empty + 0 signals ⇒ NoChanges=true, rule unchanged  (R10)
council-nochanges-noterror-1: GitDiff fails (non-git) ⇒ NoChanges=false (a write turn is not misjudged) (R10)
council-debate-split-1:    would-be-done + SPLIT ⇒ one rebuttal round, then a re-tally            (R11)
council-salvage-prefix-1:  damage only after the syntax error, decision intact ⇒ prefix salvage keeps the vote (R14)
council-salvage-nodecision-1: decision sits after the defect ⇒ salvage refused, abstain (no invented vote) (R14)
council-salvage-notshared-1: SalvagePrefix ∉ jsonx.Unmarshal/RepairCandidates (lossy; no silent plan truncation) (R14)
council-retry-shape-1:     the reminder branches three ways — syntax / schema / prose (Diagnose fed back) (R14)
council-walk-unconditional-1: the walk is asked for with keep on or off, diff or no diff          (R15)
council-walk-before-verdict-1: checks[] precedes decision in both the member and the panel schema (R15)
council-routes-differ-1:   the three routes differ, and none of them narrows jurisdiction to a slice (R16)
council-panel-once-1:      members on one backend ⇒ a single call returns every lens              (R17)
council-panel-split-backend-1: a member pinned elsewhere ⇒ per-member calls, no silent folding    (R17)
council-close-material-1:  the closing ask carries the walks and results, not the agent's report  (R17)
council-close-tightens-1:  close=continue over a done tally ⇒ continue; close=done over continue ⇒ continue (R17)
council-close-recorded-1:  what the close said travels with the round whether or not it changed it (R17)
```

## F-LOOP-STAGES — macro stages (D15; the stage tag withdrawn)
- The stages: `Plan (contract) → Execute → Verify (evidence) → Report (claim) → Council (audit) → Finalize`.
- Plan and Report are **soft** (reusing todos and the report surface); only Council is a hard point.
- The loop map still reads a turn back out of the log — `scanTurns` in `internal/app/loopmap.go`,
  behind `/loop`. It groups by what the events themselves say, not by a tag.
- **The envelope's `stage` tag is withdrawn** (`d77a064f`, 2026-08-05). It was stamped onto every
  event and persisted in every log line, and exactly two readers were ever written — both in
  `scanTurns`, both asking `e.Stage == stagePlan`. Nothing had set that stage since the stages came
  out in `8eacf04`: `setStage` was called with execute or finalize and nothing else, so the one value
  the readers compared against never appeared, `loopTurn.planned` could never become true, and the
  `◈ plan` line it gated could never print. Out with the field went `setStage`/`currentStage` and
  their four call sites, the `sessionState` field and the rewind that cleared it, the three constants
  and the render. Nothing outside Go read it either. Anything reviving this has to stamp the tag
  everywhere it claims to be read, or it is a field that costs every log line and answers nothing.

## F-SIGNAL — feedback signals as first-class (D16, withdrawn)
- Design target was `{source, kind, verdict, payload, atSeq}`, unifying the deterministic output of
  hooks, diagnostics and reports behind one model the council consumes.
- **Withdrawn**: the shipped half was config-declared commands (`[council] verify`, `[[council.signal]]`)
  run at each deliberation. A command written in a config file cannot know what the task will be, and
  what verifies a task is decided per task — which is what the acceptance criteria and deliverable
  checks already do, derived from the request. The producer was removed with the finish gate
  (`e4acdd2`) and the rest is now gone; anything reviving this must derive the check from the task,
  not from a fixed string.

## F-PLAN / F-PLAN-REC — procedural planner · plan audit · recursive decomposition — **removed**

> ⛔ **The two sections that stood here (D17 and D18, together some seventy lines of R items and test
> scenarios) were deleted.** Nothing they described is left in the code — the procedural planner and
> its per-step strategies, the pre-execution plan-audit council (`Phase="plan"`, `runPlanAuditGate`),
> the derived completion criteria, `delegate`/`refine` recursion over shared child sessions,
> `guardExpansion`/`planEnvelope`/`MaxPlanDepth`, `redecomposeStuck`.
>
> ⚠️ **Updated.** "There are no subagents at all" was true when this note was written and is not now.
> magi still ships no agent, and nothing in the tree spawns — but a **plugin** can declare a
> subagent, and a user switches it on in `/subagents` (EXTENDING §3.9, ARCHITECTURE §3). What did
> not come back is the part this section is actually about: magi deciding, on the model's behalf,
> how to split work and what to pass on. The seam passes the plugin's prompt and the tool's own
> arguments through unrewritten and decides nothing.
>
> **Why they came out**: every one of those stages decided something before the work existed, and
> every recorded defect of that period was of one kind — magi trusting its own advance judgement
> over the record of what actually happened ([`ARCHITECTURE.md`](ARCHITECTURE.md) §4). Leaving the spec
> would advertise handles that are not there. The plan is now the agent's own `todowrite`, and the
> council does not audit anything in advance.

## F-PLUGIN (M3) — Lua plugins
- Manifest (TOML) parsing: name/version/capabilities/permissions, and `exec_timeout` — one
  `magi.exec` bound for the plugin, clamped to [1s, 10m] (the 60s default was sized for probes,
  and a backend plugin's model turn is not a sixty-second command). The per-call form
  `magi.exec(cmd, args, {timeout=...})` can only shorten it.
- Capability registration (tool / command / skill / hook / mcp-server / agent / context-provider /
  ui-panel).
- Sandbox: `os.execute` and friends are blocked; only the `magi.*` bridge is exposed.
- Permission enforcement: calling an undeclared permission is refused.
- **Hot reload**: a changed file unloads and reloads just that plugin, with no loss of session state.
- Examples (later): a loaded plugin appears in the tool registry / an undeclared fs access is
  refused / a modified file reloads within N seconds.
- **Multi-instance isolation**: several magi processes on one machine share, by default, **one**
  config tree (`ConfigDir()/config.toml`) and one data tree
  (`DataDir()/plugin-data/<name>.json` — an SSO token cache, say). A plugin that persists a runtime
  choice (`set_model` → `config.SetKey`, or `store_set`) is the collision point, where one instance's
  write lands in another's file. Two defences: ① `config.SetKey`/`AppendListItem` add a
  **cross-process O_EXCL lock** (`withFileLock` — not flock, for Windows portability) on top of the
  in-process mutex, making read-modify-write atomic across processes too, so two simultaneous writes
  cannot corrupt config.toml with a torn write or a lost update (a corrupt config fails TOML parsing
  and refuses to start, i.e. never silently falls back to defaults). ② `MAGI_CONFIG_DIR` and
  `MAGI_DATA_DIR` separate the config and data directories **completely** per instance, so each has
  its own config.toml and its own plugin token slot — removing the sharing rather than guarding it.

## F-MCP (M4)
- Spawn the server (stdio) → discover via tools/list → register → bridge the calls. When the server
  dies its tools are removed.

## F-AGENT-MULTI (M5) — multi-agent — **removed**
> ⛔ Built, then torn out. The `task` tool, spawn, parallel children and the bundled orchestration
> plugin are all absent from the code, and `ToolEnv`'s `Spawn`/`Dispatch`/`Ask`/`Report` went with
> them. **There is one agent.** Reasons: [`ARCHITECTURE.md`](ARCHITECTURE.md), "One agent".

## F-ARTIFACT (M5)
- artifact emit → `artifact.emitted` → ui-panel render → ReviewArtifact (approve/reject).

## F-EXPERIENCE (M5+) — the shared brain (D13)
- Retrieve: session-start RAG. Propose: a learning or skill into the review queue, committed to git
  on approval, with secret redaction.

## F-TUI (M2)
- Conversation rendering (glamour), input, slash commands, the permission dialog, the model picker,
  the session list.

## F-IMAGE (M2+) — D8
- Terminal capability detection → kitty → iterm2 → sixel → half-block fallback. Image parts and the
  ui-panel image.

## F-SCHEDULER (M5+) — D12
- Tier 1 an in-process ticker (within a session), tier 2 an OS scheduler adapter.

## F-UPDATE / F-DIST (M7)
- goreleaser multi-target, CGO_ENABLED=0. Self-update (signed checksums, rename-swap on Windows).
