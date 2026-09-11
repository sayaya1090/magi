# Client lifecycle follow-up review — 2026-09-11

[English](CLIENT_LIFECYCLE_REVIEW_2026-09-11.md) · [한국어](CLIENT_LIFECYCLE_REVIEW_2026-09-11.ko.md) · [↑ Docs](README.md) · [Lifecycle design](CLIENT_LIFECYCLE.md)

Scope: lifecycle and updater changes in `2f71c779..525afe53`. R5 policy clarification, R6 probe timeout and R7 log-fd cleanup are implemented (`e0b36923`, `e0e15473`, `59230ae6`). Superseded findings are omitted. R8 has landed too (`ca79c0f9`): per-step reasons, cleanup of partial creation and the degradation notice are in; "block the unsafe update path" was **judged against** — see [design §4](CLIENT_LIFECYCLE.md#4-launch-compatibility-and-ownership-contract). R10 has landed too (`55ec9458`): the lock was taken by `Commit` alone and all six steps now take it. `Commit` still does **not** wait, per §9.3; the other five are finishing a transaction that already exists and so they **do** wait, capped at thirty seconds. Reproduced and confirmed with two processes on Windows: an unlocked `Salvage` overwrote a live `Commit` and deleted the `.prev` that Commit's own rollback depends on.

## Remaining findings

R9 and R11 are addressed as well (`b568d3e3`).

- **R9** — what counts as evidence moved from "a start happened" to **"the generation that was watching is gone without having confirmed"**. That generation's pid is recorded; while it is alive, a start belongs to another workspace and the transaction is left alone. An older record with no pid is read as gone (otherwise a build that really did fall over is never undone). An **unknown** liveness answer is not evidence, so nothing is rolled back on it. The liveness check moved out of `adapter/daemon` into `internal/procalive` so there is one spelling.
- **R11** — the stable window now starts **after the listener is bound and the record published**, and `Confirm` checks both the candidate version and the watcher. A minute is long enough for the record on disk to be a newer transaction, and confirming that one drops the backup for a build this process never ran and never watched.

Five mutations hold it (ignore a live watcher; never record one; read "no pid" as still-watched; confirm without checking the candidate; confirm without checking the watcher). ⚠ Three tests were faking a crash by calling `Resume` twice in one process, which is no longer a crash — they now use a **really dead pid** (a process started and reaped), because "a number nothing is likely to use" is a guess and not being a guess is the point of the check.

§9 is still partial. What remains is the **atomic safe point** (today it polls, so a turn can arrive between the check and the restart), successor readiness (Windows), and real acceptance.

## Verification

Targeted Go tests matching `Journal|Recover|Rollback|Commit|Lock` passed in updater/core packages. VS Code: 255 tests, 251 passed, three skipped, one failed. The failure checks whether TESTING lists every test file. Actual Windows/JetBrains IDE acceptance was not repeated. R9–R11 are code-path findings; the passing existing tests do not resolve them.
