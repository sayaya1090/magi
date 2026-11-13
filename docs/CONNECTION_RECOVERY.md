# Connection recovery

A temporary disconnection should not require the developer to send the task again. Recovery preserves saved progress. It does not guarantee completion during a permanent outage.

| Interruption | Behavior |
|---|---|
| Retryable API failure before output | The core retries the request with jittered backoff and honors `Retry-After`. The default limit is six retries and three minutes of cumulative waiting per step. A server delay beyond the remaining budget ends the attempt without asking early. |
| Connection cut after partial output | The core saves the prefix, adds a continuation note, and requests the next response. Transient network, timeout and overloaded errors after output use this path too. |
| Repeated partial responses without a completed tool call | Continuations use jittered backoff and honor `Retry-After`, with at most six retries and three minutes of cumulative waiting. Exhaustion records a resumable error with retry counts; a follow-up can continue from saved progress. |
| A complete tool call arrives before disconnection | The normal execution path handles the call and saves its result. Recovery does not replay the original request. An incomplete tool call must not be treated as a completed call. |
| IDE transcript connection closes | The daemon's run has its own lifetime. JetBrains reconnects with backoff and its last durable event sequence; the daemon replays subsequent events. Reconnection does not resubmit the task. |
| Shutdown or update hold | New `Submit` and `Steer` requests are rejected before saving a prompt. Shutdown waits, within its deadline, for prompt writes already admitted. An update cannot take its idle hold during those writes. |

Authentication failures, permission failures, exhausted quota and user cancellation do not become automatic retries. If an external operation may have executed but its result is unknown, inspect its effects before repeating it. This mechanism does not provide exactly-once execution across arbitrary external services or process crashes.

## Verification

`internal/adapter/daemon/connection_recovery_test.go` uses the real HTTP adapter and daemon socket. It truncates an HTTP response, checks that the next request includes the saved prefix, disconnects the IDE socket while that request is running, and reconnects from a durable cursor. The test requires a final answer, one user prompt, two model requests and no replayed durable events.

`internal/app/cut_by_lost_stream_test.go` covers recovery after partial output, bounded repeated disconnections and a completed write call executed once. `prompt_admission_test.go` covers rejection during shutdown/update and shutdown waiting on an admitted store write. JetBrains `TranscriptTest` covers cursor serialization and distinguishes transport failure from explicit close; `TranscriptReconnectTest` checks that reconnect requests share one worker, failures during attachment are retained, and closing cancels waiting. `stream_recovery_test.go` checks server delay budgets and cancellation.

These are controlled fault-injection checks. They do not establish recovery for every proxy, remote model service or IDE installation.

## Desktop verification (2026-11-14)

Commit `e088aa4f` was built and opened with `:intellij:runIde` on macOS in an isolated workspace. Requests were sent through the visible Chat input. A local HTTP server injected failures; a Unix socket proxy interrupted the IDE connection. This used the real plugin, daemon and desktop, with controlled responses instead of a remote model.

| Check | Observed result |
|---|---|
| Truncated HTTP response | `SAVED_PREFIX` remained visible; the continuation request included it and the final answer arrived. |
| Two 429 responses | `Retry-After: 2` produced waits of 2.005s and 2.004s before success. |
| Stop during a delayed response | The title-bar Stop action recorded `prompt.abandoned` and `turn.finished`. |
| 401 response | The error appeared in Chat and the turn ended without a recovery retry. Auxiliary calls were counted separately. |
| IDE socket cut during a response | The proxy dropped the live connection. Reconnection completed without resubmission: durable sequences 27–29 contained one prompt, one answer and one turn end. |
| Daemon killed during a response | Restart recorded `interrupted`, `resumable=true` and one recovery turn end. A follow-up completed. |
| SIGTERM during a shell tool | The daemon, shell and sleep child exited. The marker file contained one write; the saved result had `mayHaveRun=true` and the tool was not replayed. |
| IDE exit and reopen | The IDE exited normally. Reopening the Chat panel restored the saved conversation and interrupted tool result. The separately owned daemon remained running. |

The standard IntelliJ suite passed 221 tests. All three native browser tests passed after correcting an outdated wheel expectation from 60px to 90px (three notches × three units × 10px). Process shutdown tests also passed. Desktop captures and local logs were retained at `/tmp/magi-ide-reliability` on the verification host; they are temporary evidence, not repository fixtures.

This does not establish behavior for Windows, a real remote-provider outage, or every external tool. A process crash still requires a follow-up to resume. The initially empty restored Chat panel was investigated with a held indexing task in the desktop IDE. Before the fix, `DumbUnawareHider` made the content container invisible. `MagiToolWindow` now implements `DumbAware`: transcript replay and the composer do not require IDE indices. With indexing still active, the fixed container and input remained visible without toggling the panel. A regression test checks the factory contract; the full IntelliJ suite and plugin build passed. This verifies the indexing path, not every possible startup timing.

## Recovery when a conversation opens

Daemon startup checks the selected conversation. Other conversations are checked once on first transcript subscription, session selection or input; failed recovery is checked again on explicit retry. Running sessions are left alone. Only children created during the selected conversation's unfinished turn are checked. Tools are not automatically rerun.

On 2026-11-14 the startup timeout was reproduced in the actual magi workspace. The diagnostic process stack showed `recoveryTree → ChildSessions → scanSessions → readFile`: each parent reparsed every project log. The JSONL store now reads parent relationships once from `session.created` headers, while recovery reads bodies only for the selected session tree. Initial store indexing now discovers paths without reading bodies. Ordinary session listing still reads each uncached or changed log for its title, labels and current model.

The patched daemon started in the actual workspace, preserved its selected session and answered `about` successfully. `recovery_selected_test.go` checks that unopened conversations are neither read nor changed, that opening a transcript recovers it, and that reconnecting does not repeat recovery. `recovery_inventory_test.go` checks header-based topology and cloned workspace ownership. Startup plugin model selection no longer attempts a durable write before a session exists. The workspace trust warning is separate; trust settings were not changed.

## Installed binary verification (2026-11-14)

Three process tests passed against `/Users/sayaya/.local/bin/magi`, version `v0.42.1-119-g94b2017a`. They used isolated config, data and workspace directories, a local HTTP server and real daemon sockets. The user's running companion was not stopped.

`TestRecoveryLifecycleThroughRealDaemon` truncated an HTTP response after `SAVED_PREFIX`, waited for a continuation carrying that prefix, and closed an attached transcript socket. Reattachment from durable sequence 5 received the final answer: two model requests and one user prompt. API capability probes and system continuation prompts are counted separately.

The test then killed the daemon during another model request. Restart checked only the selected session; a second interrupted log stayed unchanged until `resume` selected it. Opening it again added no recovery events. A follow-up completed, and normal shutdown removed both the socket and daemon registration. The selected session was `s_653d120f2c05b1904d3574c6`; the unopened session was `s_e6acc640e9290d1db1f94ab6`. These were temporary test sessions, removed after verification. Initial readiness took 53ms and restart readiness 61ms in this small fixture, not a large-history performance measurement.

`TestADaemonStoppedBySigtermHandsTheCutCallToThePersonAndRestartsClean` checked tool process cleanup and uncertain-operation handling; SIGTERM exited with code 0 in 16ms. `TestAnOwnedDaemonDiesWithItsOwnersPipe` checked that closing the owner's pipe stops its daemon and removes its registration within five seconds.

On macOS or Linux, rerun against an installed binary with:

```sh
MAGI_VERIFY_BINARY=/absolute/path/to/magi go test ./cmd/magi -run 'TestRecoveryLifecycleThroughRealDaemon|TestADaemonStoppedBySigtermHands|TestAnOwnedDaemonDiesWithItsOwnersPipe' -count=1 -v
```

Without `MAGI_VERIFY_BINARY`, these tests build the current source. This run exercised daemon processes and a controlled provider, not a live remote-provider outage or desktop IDE exit/reopen. Focused JetBrains transcript and recovery tests passed 24 core tests and four IntelliJ tests; an unrelated untracked probe was excluded from core test compilation. The subsequent full-suite and listing-cache checks are recorded below.


## Resume control and log lookup

JetBrains and VS Code show a **Resume task** button for a resumable interruption. Clicking it rechecks the daemon’s recovery state and submits the continuation without changing the composer draft. Duplicate clicks are blocked while the request is pending. Sessions with uncertain tool calls still require those calls to be checked first. A failed recovery keeps its separate retry action.

The JSONL store discovers file paths at startup and loads a session’s last sequence only before its first append. Listing caches up to 4096 metadata entries separately from transcript bodies. File identity, modification time and size invalidate an entry, including another process’s append or an atomic replacement. On macOS and Linux, disposable metadata files in `listing-cache` survive process restarts. File identity, size, mtime and ctime must match before reuse. Missing, corrupt or stale entries fall back to the source log; cache write failures do not hide sessions. Other platforms retain ordinary log reads. Logs without a valid saved entry still require one full read.

`lazy_index_test.go` checks lazy sequence initialization, external updates, same-size atomic replacement, deletion and isolation of returned labels. On an Apple M4 Pro, `BenchmarkLazyHistory` with 64 logs totaling 16 MiB measured about 0.084ms to open the store and 0.152ms for a warm listing (20 iterations). These are synthetic local measurements, not IDE startup timings.


## Full core-suite and persistent listing checks (2026-11-14)

A detached checkout of `bbc501ed`, with the resume button’s local variable renamed from `state` to `recoveryReply`, passed 456 of 462 JetBrains core tests, with six environment-dependent skips and no failures. The rename satisfies the existing source guard against bringing back the old state label. No test was excluded from this checkout, and the original untracked replay probe was left untouched. No plugin was installed or restarted.

The JSONL race suite and recovery process tests passed with the persistent metadata cache. Tests cover corrupt and unwritable caches, external append, atomic replacement and in-place edits that restore the original size and modification time. Windows and Linux test binaries were cross-compiled; this is not runtime validation on those operating systems. With 64 logs totaling 16 MiB, a new store followed by a cached disk listing measured 1.66ms; a warm in-memory listing measured 0.16ms (Apple M4 Pro, 20 iterations).

Remote-provider verification could not proceed with the available configuration: the Gemini profile endpoint was unreachable, and the configured cloud model was absent from the responding server’s model list. Only local models were listed. This is a connection prerequisite check, not a successful remote outage/recovery test.
