# Lifecycle follow-up implementation review

[English](CLIENT_LIFECYCLE_REVIEW_2026-09-11.md) · [한국어](CLIENT_LIFECYCLE_REVIEW_2026-09-11.ko.md) · [↑ Docs](README.md) · [Lifecycle design](CLIENT_LIFECYCLE.md)

Updated 2026-09-12, reviewed through `6abe8fbc`. Scope: lifecycle, updates and transcript delivery after `2f71c779`. Resolved findings are condensed below; their implementation history remains in Git. Closure means the reviewed code addresses the finding. Platform acceptance is recorded separately.

## Open finding

**P1 — bridge `about` can still wait indefinitely.** In [bridge.go](../internal/adapter/idebridge/bridge.go), `about` uses `b.dial()` and then `Hello()`. The cached connection is created with `daemon.Dial`, without a connect or exchange timeout. A peer that accepts but never answers prevents the bridge from processing later requests. `6abe8fbc` fixes this for `rows` only.

Use a separate bounded connection for `about`, or support per-request deadlines. Do not impose a short timeout on the cached connection shared with `forward`: a forwarded model request may legitimately take minutes. Acceptance: a silent handshake produces a bounded failure and the next bridge request completes; a long-running forwarded request retains its intended timeout policy.

## Resolved findings

| Area | Current result and fixing commits |
|---|---|
| Earlier R5–R11 | Compatibility policy, feature-probe timeout, log descriptor cleanup and owner-channel failure reporting: `e0b36923`, `e0e15473`, `59230ae6`, `ca79c0f9`. Watcher-based failure detection and readiness-based confirmation: `b568d3e3`. Transaction locking: `55ec9458`; Windows lock contention coverage: `acd66b05`. Policy remains in design §§4 and 9.3. |
| Update admission | The automatic loop takes the hold (`ee92a23b`). Both `MeetingSayIn` and `MeetingWriteUp` admit work under the same lock (`3dfafa43`); the earlier claim that the meeting fix was complete in `ee92a23b` was premature. |
| JetBrains startup and retry | Readiness queries respect the remaining startup time (`ee92a23b`). State transitions control launch (`2ed05cc2`); reopening replaces a closed state and manual retry leaves Backoff/Blocked (`3dfafa43`). Launch exceptions now send `LaunchFailed` (`33f0ce4b`). |
| Generation identity | Clients reject a generation ID present on only one side, and JetBrains checks `hello.ok` (`ee92a23b`). The successor readiness check follows the same rule (`5953ce99`). |
| Successor failure | Failure to start now enters rollback recovery, retries the previous build once while its owner remains, and returns failure when recovery cannot proceed (`37e21536`, #190). This applies to Unix and Windows. |
| Windows rollback | Process liveness uses the process handle's signalled state. Restoring `.prev` uses `Apply` to move a running image aside (`8d84ad27`). Windows live tests cover rollback, owned pipes and detached processes (`8d84ad27`, `5953ce99`, `035112df`). |
| Transcript completion | The bridge exposes shared `rows`, and the daemon names the end of replay (`dd06f762`). An up-to-date cursor receives the marker; a head-read error is reported; History bounds silence to 15s and checks deadline-setting errors (`a09cb01e`, `d91508ec`). `rows` bounds connection to 2s and handshake to 5s (`6abe8fbc`). |
| Client display and web lifetime | JetBrains row vocabulary matches the core (`ee9176ff`). Replay completion reaches JetBrains and VS Code (`036800cb`, `1be5496b`). Web lifetime tests query process liveness instead of relying only on records (`2ed05cc2`). |

## Verification and limits

Reviewer runs on macOS, 2026-09-12:

- Targeted Go tests for transcript/history, update recovery, relaunch and locks passed through `0a4ca485`. The full `internal/adapter/idebridge` package was rerun at `6abe8fbc`.
- VS Code: 284 passed, 0 failed, 5 skipped through `0a4ca485`.
- JetBrains core: `PhasesTest`, `SourceTextTest`, `TranscriptTest` and `WireConformanceTest` passed at `036800cb`. No later Kotlin changes were included in this review.

Windows live tests were reviewed as code; the implementers report successful Windows runs. The reviewer did not repeat those runs or real IDE/browser acceptance. Earlier whole-repository, race and mutation-test reports are not fresh verification of this revision. `0a4ca485` records an intermittent test failure whose cause was not established; do not attribute it to a specific subsystem without a reproducer. Lifecycle design §§8–9 remain the platform acceptance checklist.
