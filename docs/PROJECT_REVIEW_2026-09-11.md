# magi project review — 2026-09-11

[한국어](PROJECT_REVIEW_2026-09-11.ko.md)

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
