# Client lifecycle follow-up review — 2026-09-11

Scope: lifecycle and updater changes in `2f71c779..525afe53`. R5 policy clarification, R6 probe timeout and R7 log-fd cleanup are implemented (`e0b36923`, `e0e15473`, `59230ae6`). Superseded findings are omitted. R8 has landed too (`ca79c0f9`): per-step reasons, cleanup of partial creation and the degradation notice are in; "block the unsafe update path" was **judged against** — see [design §4](CLIENT_LIFECYCLE.md#4-launch-compatibility-and-ownership-contract).

## Remaining findings

- **R9 / P1 — Healthy concurrent starts trigger rollback:** `internal/update/journal.go:Resume` treats install-wide `Starts >= 1` as failure. Workspace B starting while A runs the candidate normally triggers rollback. Distinguish generations using liveness/readiness evidence and test two concurrent workspaces.
- **R10 / P1 — Incomplete transaction coordination:** The OS lock ends when `Commit` returns. `Salvage/Resume/Confirm/Retry/LeftCleanly` do not acquire it. Another daemon can restore an in-progress backup as an interrupted update, or a later replacement can overwrite a backup still under trial. Coordinate the install through confirmation/rollback and test actual process races.
- **R11 / P1 — Confirmation timer starts before readiness:** `cmd/magi/main.go` starts its 60-second timer before the listener starts. `Confirm` deletes the current journal's backup without checking candidate identity, generation or readiness. Delayed readiness or a later transaction can confirm an unverified candidate. Start the stable interval after readiness and require transaction identity when confirming.

The remaining R9-R11 do **not** need Windows: they are platform-neutral Go in `internal/update`, and measuring them means two workspaces contending as two processes. R10's install lock does have a per-platform implementation (`lock_windows.go`) whose own comment calls Windows "the platform where the file being replaced may also be locked by a running image", so running that contention test once on Windows is worth something.

Section 9 is partially implemented. Recovery from an early startup failure depends on another execution; safe-point handling, readiness and multi-daemon coordination still require acceptance. Windows execution is not the only remaining work.

## Verification

Targeted Go tests matching `Journal|Recover|Rollback|Commit|Lock` passed in updater/core packages. VS Code: 255 tests, 251 passed, three skipped, one failed. The failure checks whether TESTING lists every test file. Actual Windows/JetBrains IDE acceptance was not repeated. R9–R11 are code-path findings; the passing existing tests do not resolve them.
