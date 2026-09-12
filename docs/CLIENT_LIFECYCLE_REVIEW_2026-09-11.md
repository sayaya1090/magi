# Lifecycle follow-up implementation review — 2026-09-11

[English](CLIENT_LIFECYCLE_REVIEW_2026-09-11.md) · [한국어](CLIENT_LIFECYCLE_REVIEW_2026-09-11.ko.md) · [↑ Docs](README.md) · [Lifecycle design](CLIENT_LIFECYCLE.md)

Scope: lifecycle and update changes after `2f71c779`. Each finding below is **struck through once closed, naming the commit that closed it.** Only what is still open carries no line.

## First pass (R5–R11)

- ~~**R5** — state in §4 the policy that warns and then starts anyway.~~ `e0b36923` — what "requiring" means, as a table. The split is **the shutdown guarantee**: the relay cannot stand up at all, so it is blocked; an ordinary launch is the lifetime §4 says to preserve, so it is said once per launch.
- ~~**R6** — the feature probe does not bound reading the output.~~ `e0e15473` — `readLine()` sat BEFORE `waitFor`, so those five seconds were never reached. Order reversed, cleanup added, and moved into the SDK-free `core` so it can be measured without an editor.
- ~~**R7** — the log fd survives a close race.~~ `59230ae6` — the open moved past the last `await`. Counted against `/dev/fd` rather than inspected.
- ~~**R8** — owner-channel failures are swallowed and it falls back to `'pipe'` silently.~~ `ca79c0f9` (#189) — per-step reasons, partial-setup cleanup, degradation reported. "Block the unsafe update path" was **decided against** ([design §4](CLIENT_LIFECYCLE.md#4-launch-compatibility-and-ownership-contract)).
- ~~**R9** — healthy concurrent starts are read as a failure.~~ `b568d3e3` — evidence moved from "a start happened" to **"the generation that was watching is gone without confirming"**. The watcher's pid is recorded; while it lives, a start belongs to another workspace. The liveness check moved into `internal/procalive` so there is one spelling.
- ~~**R10** — transaction arbitration was on `Commit` alone.~~ `55ec9458` — all six take it. `Commit` does not wait (§9.3); the other five are finishing a transaction that already exists, so they do (30s cap). Reproduced with two processes on Windows: an unlocked `Salvage` overwrote a live `Commit` and deleted its `.prev`.
- ~~**R11** — the confirmation clock starts before readiness.~~ `b568d3e3` — the stable window starts after the listener is bound and the record published, and `Confirm` checks both the candidate and the watcher.

## Second pass (2026-09-12, six commits)

- ~~**① the automatic update has no atomic wait wired.**~~ `ee92a23b` — `3f693903` fixed only the pressed button; the six-hourly loop still polled and then restarted. **The other half of that very gap.** The loop takes and releases the hold, and the test pins that `running` is never consulted when a hold exists.
- ~~**② starting a meeting bypasses the update hold.**~~ `ee92a23b` — `HoldForUpdate` looked at the meeting counter, but nothing looked at the hold, and the counter was raised outside the lock. `beginMeetingRound` reads both under one lock.
- ~~**③ JetBrains readiness can be delayed by 120s.**~~ `ee92a23b` — bounded by the remaining ready time (250ms–5s). The same shape as R6: **the deadline has to come first to be a deadline**.
- ~~**④ a one-sided generation id still passes readiness.**~~ `ee92a23b` — the record's writer and the process answering are the same daemon, so only one side naming a generation means **something else answered**. The pid fallback applies only when neither does, and JetBrains now also checks `hello.ok`.
- ~~**⑤ the state machine's call sites are incompletely wired.**~~ this commit — `Lost`, `Retry`, `Exhausted` and `Settled` are each attached to their moment, and **the transition verdict is used** (a window that is closing does not launch). ⚠ One `Progress` per workspace is **kept deliberately** — a workspace has one state, and one per path would let the two drift.
- ~~**L12's test proves nothing about process liveness from file comparison alone.**~~ this commit — `kill(pid, 0)` asks the kernel. Verified by reverting: leaving the record untouched and `SIGKILL`ing the daemon fails with "that pid no longer runs — only the record is left".

## Verification (as of 2026-09-12)

`gofmt` silent · `go vet ./...` (and `GOOS=windows`) silent · `go test ./... -count=1` with no failures · the packages touched today also pass under `-race` · VS Code 283 passed / 0 failed / 3 skipped · JetBrains `:core:test` rc=0 · the web e2e `lifetime.spec.mjs` 2 passed.

⚠ **All of that says is that tests pass.** Real Windows IDE, JetBrains IDE and browser acceptance were not re-run — §8's L01–L13, §9's U01–U08, the native `deactivate` and L12's server restart are where that lives. The findings above came from reading code paths, and a struck-through line means **the fix landed and the test measuring it was verified by mutation**, not that it was seen on the real thing.
