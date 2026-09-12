# magi project review

[한국어](PROJECT_REVIEW_2026-09-11.ko.md) · [↑ Docs](README.md)

Updated 2026-09-13; implementation reviewed through `fd68d489`. Resolved defects and corrections remain in Git history. This document retains current judgments and outstanding work. It does not establish a full repository audit or actual IDE acceptance.

## Current assessment and priorities

The council's review of completion against execution evidence, and access to one workspace through IDE, terminal and web clients, are useful directions. The next priorities are reducing the cost of shared contracts and verifying complete usage flows.

| Priority | Current state and next work |
|---|---|
| 1. Primary client stability | The lifecycle code findings reviewed so far are resolved. Continue actual product acceptance from installation through connection, work, reconnection and shutdown under [lifecycle design](CLIENT_LIFECYCLE.md) §§8–9. |
| 2. Shared data transformation | User messages, answers and Think retain full bodies; `Row.Summary` supplies list summaries. Resolve the tool input/output losses below before the live-delivery contract and client migration. |
| 3. Council effectiveness | Under comparable task conditions, measure incorrect completion detected, unnecessary rework, latency and token cost together. Intended behavior alone does not establish effectiveness. |

## Open implementation findings

| Priority | Remaining problem | Fix and acceptance |
|---|---|---|
| P2 | [AskedFor](../internal/adapter/idebridge/fold.go) returns only the first string found among `path`, `command` and similar keys. `{path, old_string, new_string}` retains only the path. | Preserve all arguments as structured data or complete JSON. Select representative fields only for the summary; verify that multi-field edit and execution arguments can be recovered. |
| P2 | Tool results populate `Out` only for non-advisory failures. Successful results and advisory bodies cannot be read from shared rows. | Preserve result bodies regardless of success. Keep default folding or hiding separate, and verify complete bodies when expanding successful, failed and advisory results. |

Separate body preservation from default visibility. Verify lossless long bodies, multiple lines, emoji and tool inputs/outputs, then matching final rows from replay and live processing. Migrate clients in stages and retain their existing shapers during the transition.

## Verification scope

Recent macOS review runs passed the relevant Go row/fold/source-announcement tests and JetBrains path/symlink tests. The Windows live update test (`5d4b5a6f`) was reviewed as code only. Record source checks, unit tests, headless UI and actual product acceptance separately; automated test success does not close unexecuted platform acceptance.
