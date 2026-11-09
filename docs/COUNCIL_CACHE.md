# Council evidence preparation and final reports

The council needs fresh evidence without repeatedly rebuilding its entire input. The main model also should not repeat a final report after the council accepts it. These changes address both paths. They do not guarantee cache hits or remove the panel's second, closing judgment.

## Final report contract

Write the user-facing report before calling `council{complete:true}`. `report_ready` defaults to true: on acceptance, the loop runs its normal finish checks and records `turn.finished` without another main-model generation. Report length is irrelevant. If no assistant report exists in this turn, the loop produces a closing answer once after acceptance. Set `report_ready:false` explicitly when the existing text is not the final answer. Rejection still allows further work. Stop hooks, arriving handoffs and the existing finish checks may reopen work even after acceptance.

## Shared evidence prefix

`councilPrefix` builds a chronological journal from this turn's persisted events: user requests, tool results with their arguments, assistant claims, plan updates and previous council judgments. A new event is appended after the old entries. Previous judgments are labelled as judgments, not execution evidence. The current review snapshot follows the journal and continues to include fresh workspace observations, unresolved objections and the latest report. It supersedes historical state.

The journal is limited to 64 KiB and each entry to 4,000 bytes. Overflow removes the oldest entries, which changes the cache prefix. A new turn starts a new journal. All lenses use the same minimal system instruction; detailed judging instructions follow the current snapshot. The panel's closing call retains the original exchange. Different models cannot share KV state, and backend retention policies determine whether a prefix survives until the next call.

## Background preparation

For gateways that support explicit cache boundaries, judging requests mark the end of the evidence prefix with `cache_control`. The fresh review snapshot stays outside that boundary. Disabling caching preserves the original string message.

One worker per top-level, non-workflow turn receives notifications after events are persisted. It coalesces notifications for 1500 ms and prepares the latest prefix. Each preparation request has a 30-second timeout. A build or test tool gives it an opportunity to prepare earlier evidence while the main model is idle. The build's own result can only be prepared after that result exists. Preparation does not generate an answer, record a judgment, or gate completion. Unsupported providers return `ErrCapabilityAbsent`; there is no fallback generation request.

`MAGI_COUNCIL_PREFILL` defaults to `auto`. Automatic detection uses a read-only `/props` request and requires native server properties (`total_slots`, `chat_template`, `default_generation_settings`); an unsupported gateway receives no preparation completion request. Detection is cached per endpoint. `off` or `0` disables preparation API requests, and `llama.cpp` selects the native API without detection. `-no-cache` also disables native preparation. Explicit native mode requires every affected provider to be a compatible llama-server using the same chat template as its chat endpoint. Ollama and hosted OpenAI-compatible gateways without these native APIs are skipped.

The native path uses `/apply-template` and `/completion`. The server renders its own chat template, and preparation stops inside the evidence message before its closing role markers. The completion request sets `n_predict:0`, `cache_prompt:true` and `stream:false`. A response containing generated content is an error.

`MAGI_COUNCIL_PREFILL_PREEMPT` defaults to `0`: in-flight preparation may finish, while new preparation waits for foreground requests. Set it to `1` when shared resources make that remaining work delay the main model; foreground requests through the same client then cancel preparation. Already processed work is not refunded, and retaining partial KV state depends on the server. Neither policy controls other clients, other processes, server scheduling or cache eviction. Repeated preparation of the same unchanged prefix is skipped; a busy provider is retried after 250 ms. Turn cancellation stops the worker.

## Verification and measurement

HTTP contract tests check zero-generation requests, template suffix removal and foreground cancellation. Council adapter tests compare the prepared prefix with the actual judging request and its closing call. Core tests cover chronological judgments, turn isolation and short final reports ending without another model call. These tests do not establish cache hits or speed on a real model.

Council stream logs report first-token wait, total duration and cached tokens when the provider supplies them. First-token wait includes network and queueing, so it is not pure prefill time. Compare those measurements on the actual backend before claiming a latency improvement. Run the full Playwright transcript harness alongside the Go checks when changing this path.

Each `magi.analyze` call writes one line to the daemon log, whether or not `MAGI_DEBUG` is set: `magi: analyze usage plugin=engram label=reflection input=... output=... cached_tokens=... elapsed=... status=ok`. Engram labels its calls `facts`, `reflection`, `dream` and `reconcile`. `unreported` means the backend said nothing, which is different from a reported 0. The line reads the same usage event as `magi.analyze_measured`, and no real-backend run of it has been checked yet.

### Measured on the `claudecode` backend (2026-11-09)

These figures come from two days of Claude CLI session files and the magi daemon log. The session folder is shared between workspaces, so other projects' calls may be included. No model was called to collect them.

| Call | Calls | Cache written | Cache read | Read share |
|---|---:|---:|---:|---:|
| Main conversation | 344 | 11,002,908 | 74,330,476 | 87.1% |
| Council closing call | 21 | 119,771 | 989,620 | 89.2% |
| Council first call | 64 | 2,688,418 | 530,166 | 16.5% |
| Engram reflection | 10 | 383,609 | 2,855 | 0.7% |
| Engram facts (classified by input form) | 6 | 290,606 | 5,416 | 1.8% |
| Engram dream | 1 | 6,481 | 0 | 0% |

The closing call's hits are not reuse between rounds. It continues the panel call's exchange (`panel.go`), and in the four rounds after the 2026-11-09 05:22 restart its reads (38,340, 40,700, 58,112, 28,921) equal what that panel call had just written. Those four panel calls read 0.

Preparation does not reach the judging call on this backend. Counted separately, 32 preparation calls wrote 418,891 tokens and read 494,139, and that read total was not split by session. Of the 31 CLI sessions that received a preparation, 2 went on to judge. The `claudecode` plugin is excluded from this repository (`.git/info/exclude`) and ignores `cache_control`. Two differences are visible in the session files:

- A sampled preparation input starts with `[system]` and the judging input with `[user]`, so they share one leading character.
- The history grows inside a single user message. The plugin matches sessions per message (`findSession`), so a longer prefix appears to start a new child. This part is inferred from the code and the one-message sessions, not traced at run time.

Setting `MAGI_COUNCIL_PREFILL` to any value other than `auto` or `llama.cpp`, such as `off`, turns preparation off. Whether that lowers total token use has not been measured.
