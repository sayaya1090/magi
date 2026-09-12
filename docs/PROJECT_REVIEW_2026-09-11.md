# magi project review — 2026-09-11

[한국어](PROJECT_REVIEW_2026-09-11.ko.md) · [↑ Docs](README.md)

This is Codex's assessment, recorded at the user's request after work on the JetBrains and VS Code clients and the council Think delivery path. It draws on the code inspected and the fixes verified during that work. It is not a full repository audit or a completed test on a Windows IDE. The priorities below are recommendations, not an agreed development plan.

magi has a clear direction and deserves further development. Its most pressing need is to make the connections between components reliable before expanding the feature set further.

The distinction between an agent claiming completion and evidence that the work is complete is particularly useful. The council is a consensus procedure among reviewing models. Execution records, approvals and work history support that judgment. Accessing a workspace's companion through an IDE, terminal or web interface is also a useful product direction. The council's actual effect on incorrect completion needs separate measurement, though. Its intended role does not establish its effectiveness.

My main concern is the growing cost of maintaining shared contracts across clients. In the Think issue, values disappeared between provider reasoning collection, event publication, persistence, client conversion and display. Fields and rendering code existed in remote commits, but two event construction sites omitted the value, leaving the user's screen empty. Reasoning from a panel that returned valid verdicts and its display in the three Office clients were also missing. The fixes are recorded in [731b404a](https://github.com/sayaya1090/magi/commit/731b404a).

The project invests heavily in tests and documentation. The balance of verification methods needs attention. Tests that look for a field or phrase in source can catch missing declarations. Execution needs checking too. An escaping error in JavaScript generated from the VS Code webview template could prevent that script from running; browser execution exposed it. The [browser regression test](../clients/vscode/tools/transcript-test.mjs) and [live and persisted event test](../internal/app/council_live_verdict_test.go) exercise the boundaries where these defects occurred.

I recommend these priorities:

- **Stabilize the main clients' complete usage flow.** Check installation, connection, work, reconnection and shutdown in the primary IDE and web client. Record automated test success separately from success in the user's actual installation environment.
- **Concentrate shared data conversion.** Reduce independent event interpretation in each client and develop the common bridge to supply consistent display data. Check compatibility with existing clients during that transition.
- **Measure the council's effectiveness and cost together.** Track incorrect completion caught, unnecessary rework, completion delay and token cost. Comparable task conditions are needed to judge whether a change improves the result.

The project's maturity will depend on how rarely users have to guess whether it connected, why it stopped or whether the work actually finished. As that trust grows, the value of the features already implemented should become easier to see.

## Follow-up implementation review — 2026-09-13

Reviewed through `fd68d489`. The [lifecycle design](CLIENT_LIFECYCLE.md) retains the first recommendation's implementation contracts and outstanding platform acceptance. `56927914` fixes the missing custom release-source announcement for console updates. The findings below concern the second recommendation, shared data transformation.

**Correction:** the concern that the shared fold clipped user messages, answers and Think did not match the implementation. Those bodies were already preserved. `fd68d489` removes line/length clipping from tool arguments and failed results and adds `Row.Summary` for lists. Two losses remain.

| Priority | Remaining problem | Fix and acceptance |
|---|---|---|
| P2 | [AskedFor](../internal/adapter/idebridge/fold.go) returns only the first string found among `path`, `command` and similar keys. `{path, old_string, new_string}` retains only the path. | Preserve all arguments as structured data or complete JSON. Select representative fields only for the summary; verify that multi-field edit and execution arguments can be recovered. |
| P2 | Tool results populate `Out` only for non-advisory failures. Successful results and advisory bodies cannot be read from shared rows. | Preserve result bodies regardless of success. Keep default folding or hiding separate, and verify complete bodies when expanding successful, failed and advisory results. |

These are existing losses not yet addressed by the full-body contract, rather than new regressions. A previous decision to hide content does not justify discarding it from the canonical rows: migrated clients must be able to expand it. First verify lossless long bodies, multiple lines, emoji, Think and tool inputs/outputs. Then establish a contract in which **replay and live processing produce the same final rows**, and migrate clients in stages. Retain existing shapers during that transition.

The reviewer ran the relevant Go row/fold/source-announcement tests and JetBrains path/symlink tests on macOS; they passed. The Windows live update test in `5d4b5a6f` was reviewed as code but not executed. These results do not establish actual IDE or Windows acceptance.
