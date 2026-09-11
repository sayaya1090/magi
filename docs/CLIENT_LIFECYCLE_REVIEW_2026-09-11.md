# Client lifecycle follow-up review — 2026-09-11

Scope: lifecycle and updater changes in `2f71c779..525afe53`. R5 policy clarification, R6 probe timeout and R7 log-fd cleanup are implemented (`e0b36923`, `e0e15473`, `59230ae6`). Superseded findings are omitted. R8 has landed too (`ca79c0f9`): per-step reasons, cleanup of partial creation and the degradation notice are in; "block the unsafe update path" was **judged against** — see [design §4](CLIENT_LIFECYCLE.md#4-launch-compatibility-and-ownership-contract). R10 has landed too (`55ec9458`): the lock was taken by `Commit` alone and all six steps now take it. `Commit` still does **not** wait, per §9.3; the other five are finishing a transaction that already exists and so they **do** wait, capped at thirty seconds. Reproduced and confirmed with two processes on Windows: an unlocked `Salvage` overwrote a live `Commit` and deleted the `.prev` that Commit's own rollback depends on.

## Remaining findings

- **R9 / P1 — Healthy concurrent starts trigger rollback:** `internal/update/journal.go:Resume` treats install-wide `Starts >= 1` as failure. Workspace B starting while A runs the candidate normally triggers rollback. Distinguish generations using liveness/readiness evidence and test two concurrent workspaces.
- **R11 / P1 — Confirmation timer starts before readiness:** `cmd/magi/main.go` starts its 60-second timer before the listener starts. `Confirm` deletes the current journal's backup without checking candidate identity, generation or readiness. Delayed readiness or a later transaction can confirm an unverified candidate. Start the stable interval after readiness and require transaction identity when confirming.

The remaining R9 and R11 do **not** need Windows: they are platform-neutral Go in `internal/update`, and measuring them means two workspaces contending as two processes. R10 was measured on Windows, where the same race goes wrong in two ways — the restore lands and the update is silently undone, or it fails with `Access is denied` because the pre-flight is executing the target — and one lock removes both.

⚠ That also uncovered the fact that this package measured **none** of the transaction on Windows: `journal_test.go` and `rollback_test.go` are `!windows` because they stand shell scripts in for binaries. `internal/update/lockscope_test.go` fills that gap by standing the test binary itself in the install's place, so it runs everywhere.

Section 9 is partially implemented. Recovery from an early startup failure depends on another execution; safe-point handling, readiness and multi-daemon coordination still require acceptance. Windows execution is not the only remaining work.

## Verification

Targeted Go tests matching `Journal|Recover|Rollback|Commit|Lock` passed in updater/core packages. VS Code: 255 tests, 251 passed, three skipped, one failed. The failure checks whether TESTING lists every test file. Actual Windows/JetBrains IDE acceptance was not repeated. R9–R11 are code-path findings; the passing existing tests do not resolve them.
