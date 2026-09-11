# Lifecycle implementation follow-up review — 2026-09-11

[한국어](CLIENT_LIFECYCLE_REVIEW_2026-09-11.ko.md) · [Target design](CLIENT_LIFECYCLE.md)

Reviewed changes after design commit `2e0179c6`, at local `43f45f1c` and remote `dcc43ed4`. Their difference adds a TUI meter test, with no difference in the lifecycle code below. This review focuses on lifecycle and transcript follow-up commits, not a full audit of concurrent meeting/documentation work. No implementation code was changed.

## Findings

The implementer feedback was also reviewed. R2 adds evidence for Windows transfer already marked as deferred. Publishing `ownerId` together with owned mode was appropriate. The objection to a state file conflicting with existing principles is accepted: withdraw the `<socket>.lifecycle` requirement and report unknown exit reasons when evidence is unavailable. Use handshake and `about` for current state. An update recovery journal must not determine current daemon liveness or ownership.

| ID | Priority | Location/condition | Impact and acceptance requirement |
|---|---|---|---|
| R1 | P1 | VS Code `src/core/lifecycle.ts`, between `await features(binary)` and spawn | Closing while feature discovery waits can still create a child afterward. Recheck generation/closed after discovery and honor cancellation through spawn. Clean up children racing close after creation too. |
| R2 | P1 | VS Code piped stdin ownership plus Windows successor launch in `internal/graceful/graceful_windows.go` | Node closes the original child's stdin when that child exits. An inheriting successor can therefore receive owner EOF during an update even while the owner remains alive. Use an independently managed channel and test predecessor exit separately from IDE exit on Windows. |
| R3 | P1 | JetBrains `StartDaemon.start`: `ProcessBuilder(..., "--daemon")`, followed by `child.outputStream.close()` | The new owned mode is unused. Existing normal-disposal termination does not apply the EOF contract to forced IDE termination. Connect feature discovery, owned launch and channel retention; execute L05–L07. |
| R4 | P1 | JetBrains `StartDaemon` calls into `Launches` | The policy exists, but readiness success and launch failure are not reported through `ready`/`failed`. A daemon failing from startup can continue retrying after the rolling window expires without accumulating consecutive failures. Inject three actual launch failures and verify blocking persists as time advances. |
| R5 | P2 | VS Code `OwnedCompanion.launch` unsupported-feature branch | Without `owned-daemon-v1`, it launches plain `--daemon`, contrary to target §4's update-and-block requirement for a new owned launch. Separate compatibility with an existing daemon from permission to launch a new owned one. |

P1 findings must be resolved before shutdown/recovery acceptance; P2 identifies a compatibility/design mismatch. R3–R5 are based on implementation-path inspection, not execution in a Windows IDE.

## Reproduction and verification

R1 used the current TypeScript transpiled in memory, delaying only the feature-discovery Promise. Sequence: request start, pause at feature discovery, complete `close()`, release the feature result. Observed `spawnsAfterClose=1`, `stops=0`. A child was created without cleanup after close returned. The earlier closed check cannot prevent this race.

R2 used three processes on macOS with Node v24.4.1. An IDE-like Node parent created a child with piped stdin; that child passed fd 0 to a successor and exited. Without closing the IDE-like parent or explicitly closing stdin, the observations were `ownerStillRunning=true`, `stdinDestroyed=true`, `successorSawEOF=true`. The local Node runtime's `internal/child_process` also contains `this.stdin.destroy()` in exit handling. Actual Windows update validation remains necessary.

VS Code tests reported 231 passes and one skip out of 232. `go test ./internal/adapter/idebridge ./internal/adapter/daemon ./cmd/magi -run 'Owned|Owner|Instance|Features|Relay' -count=1` also passed. These results do not establish that R1/R2 are absent. Extend normal EOF and policy-function tests to execute asynchronous shutdown and generation replacement boundaries.

## Progress and handoff

Feature discovery, core owned mode/process identity, shared IDE policy fixtures and backoff integration, and preview replacement by final facts have progressed. Preserve that work. Distinguish declarations, policy functions and individual call sites from end-to-end acceptance in installed products.

Core/client implementers should address R1–R5 with reproducing regression tests. Automatic-update safe points, inter-process replacement locking and rollback after actual readiness failure are additional acceptance scope in [design §9](CLIENT_LIFECYCLE.md#9-automatic-updates--included-in-this-phase). Existing rollback based on `--version` preflight alone does not complete U05.
