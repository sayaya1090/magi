# Lifecycle implementation follow-up review — 2026-09-11

## Follow-up: 8f68a5aa through c927279e

R1 launch cancellation, R3 owned-mode integration and R4 readiness/failure reporting are implemented. R2 has an independent owner channel; the implementer records Windows core verification in §2.5. This reviewer did not repeat Windows execution. VS Code tests passed 240 of 243 cases, with three skipped. JetBrains and web changes were checked against code and documentation; actual IDE/browser acceptance was not repeated.

- **R6 / P1:** `CoreBinary.features` calls `readLine()` before `waitFor(5 seconds)`. A probe keeping stdout open without sending a line blocks startup without a deadline. Include output reading in the deadline and clean up processes and streams on timeout or exceptions.
- **R7 / P2:** VS Code `OwnedCompanion.launch` opens its log fd before probing, but both new `closed` returns precede the try/finally that closes it. Cancellation leaks the file handle. Open it after the last await or cover all returns with finally; extend the race test to check fd cleanup.
- **R8 / P2:** Windows `ownerChannel` swallows all setup/connect failures and falls back to `'pipe'`. R2 can recur, while capability-based `unownedStart` does not report degraded ownership. The catch also leaves partially created servers/sockets unclosed. Inject failures at each stage, verify cleanup and notification, and block the affected update path when successor lifetime cannot be guaranteed.

R5 now permits ordinary launch with a warning. That is a defensible legacy compatibility choice, but marking it complete while §4 still requires blocking launches that need owned mode leaves the contract ambiguous. Specify the allowed conditions and weaker shutdown guarantee in §4.

Web backoff, draft persistence and disconnection indication were added. The document correctly distinguishes generation verification and successor readiness as unfinished. The §9 update transaction and readiness-failure rollback were not implemented in these changes.
