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
