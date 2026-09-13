# magi project review

[한국어](PROJECT_REVIEW_2026-09-11.ko.md) · [↑ Docs](README.md)

Updated 2026-09-13; implementation reviewed through `a1ef1b03` (including `f30b7149`). Resolved defects and corrections remain in Git history. This document retains current judgments and outstanding work. It does not establish a full repository audit or actual IDE acceptance.

## Current assessment and priorities

The council's review of completion against execution evidence, and access to one workspace through IDE, terminal and web clients, are useful directions. The next priorities are reducing the cost of shared contracts and verifying complete usage flows.

| Priority | Current state and next work |
|---|---|
| 1. Primary client stability | The lifecycle code findings reviewed so far are resolved. Continue actual product acceptance from installation through connection, work, reconnection and shutdown under [lifecycle design](CLIENT_LIFECYCLE.md) §§8–9. |
| 2. Shared data transformation | User messages, answers, Think and tool arguments/results retain full bodies (`f30b7149`); `Row.Summary` supplies list summaries. Establish the live-delivery contract and proceed with client migration. |
| 3. Council effectiveness | Under comparable task conditions, measure incorrect completion detected, unnecessary rework, latency and token cost together. Intended behavior alone does not establish effectiveness. |

## Implementation findings and resolution status

| Priority | Category | Finding | Resolution and commit |
|---|---|---|---|
| P2 | Tool argument preservation | [`AskedFor`](../internal/adapter/idebridge/fold.go) returned only the first string found among `path`, `command` and similar keys, losing edit contents in multi-field arguments (`{path, old_string, new_string}`). | `f30b7149`: Preserved full argument JSON in `Row.Args` and moved the single-line representation to `Row.Summary`. |
| P2 | Tool result preservation | Tool results populated `Out` only for non-advisory failures, preventing successful results and advisory bodies from being read from shared rows. | `f30b7149`: Preserved all result bodies in `Row.Out` regardless of outcome or advisory status. |

Separate body preservation from default visibility. Lossless preservation of long bodies, multiple lines, emoji and tool inputs/outputs is verified, and replay and live processing arrive at identical final rows (`TestALiveStreamEndsWhereAReplayDoes`). Remaining work is establishing the live-streaming delivery contract and migrating clients in stages while retaining their existing shapers during transition.

## Verification scope

Recent macOS review runs passed the relevant Go row/fold/source-announcement tests and JetBrains path/symlink tests. Windows live update tests (`5d4b5a6f`, `388f3ac4`, `aacafe1a`, `a1ef1b03`) were verified against code and execution results. Record source checks, unit tests, headless UI and actual product acceptance separately; automated test success does not close unexecuted platform acceptance.
