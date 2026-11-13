# JetBrains and VS Code feature and test comparison

This review compares repository implementations as of 2026-11-14. Command counts are not the comparison unit. JetBrains exposes work through tool windows and settings; VS Code uses commands and webviews. Paths below are relative to each client directory.

| Feature | JetBrains implementation / tests | VS Code implementation / tests | Result |
|---|---|---|---|
| Install, start, update, shutdown | `Launches`, `Workspace` / `DaemonLifecycleTest`, `LaunchesTest` | `ide/start.ts`, `getcore.ts`, `workspace.ts` / `lifecycle`, `launches`, `owner` | Available in both |
| Reconnection and session replay | `MagiToolWindow`, `TranscriptReconnect` / `TranscriptReconnectTest`, `TranscriptTest` | `ide/chat.ts` / `chat_host`, `stream`, `lockstep` | Added an incremental cursor, deduplication and one cancelable retry worker to VS Code |
| Send, steer, cancel, attachments, input history | `Companion`, `MagiToolWindow` / `CompanionTest`, `PromptRecallTest`, `SendDraftTest` | `ide/chat.ts`, `core/refs.ts` / `chat_host`, `prompt_recall`, `wire` | Available in both |
| Questions, approvals, diff review, failed draft recovery | `AnswerMode`, `ReviewSurface` / `AnswerModeTest`, `AnswerTransitionCoordinatorTest`, `AnswerRecoveryDialogTest` | `ide/approvals.ts`, `web/answer_state.ts` / `approvals`, `answer_state`, `recovery_state`, browser asks and diff | Available in both; test methods differ |
| Create, select, rewind and compact sessions | `Companion`, `PlanToolWindow` / `CompanionTest`, `PlanPanelLayoutTest` | `ide/doors.ts`, `plan.ts` / `chat_host`, `plan_host` | Available in both |
| Model, profile, permissions, council | `Companion`, info popup, `CouncilControl` / `CompanionTest`, `CouncilControlTest` | `ide/choose.ts`, `plan.ts` / `chat_host`, `plan_host`, `setup` | Added JetBrains council control; saved profiles now use `use-profile` |
| Plans, context, council results | `PlanToolWindow`, `Rows` / `PlanStripViewTest`, `ContextGaugeTest`, `RowsTest` | `ide/plan.ts`, `core/transcript.ts` / `plan_host`, `planstrip`, `stream` | Available in both |
| Child jobs, termination, schedules, delegated approvals | `PlanToolWindow`, `ScheduleActions` / `SchedulesTest`, `CompanionTest`, `PlanPanelLayoutTest` | `ide/doors.ts`, `handoff.ts` / `background`, `handoff`, `approvals` | Available in both |
| Knowledge and profile management | `KnowledgeDialog`, `ProfileMaintenanceDialog`, `CompanionProfileEditor` / `CompanionProfileEditorTest`, `KnowledgeDialogTest`, `ProfileMaintenancePanelTest` | `ide/knowledge.ts`, `profileMaintenance.ts`, `companionProfile.ts` / corresponding tests | Available in both; added JetBrains runtime tests for inventory reloads, conflicts, connection failures, closing and profile refresh |
| Code review, completion, authorship, commit messages | `Assist`, `Authorship`, IDE actions / `AssistTest`, `AuthorshipTest`, `LookNotesTest` | `ide/look.ts`, `complete.ts`, `edits.ts`, `doors.ts` / `look`, `touched`, `hand` | Available in both; editor-specific behavior needs separate tests |
| Editor MCP, ownership, output documents | `IdeHand`, `OutputEditor` / `HandRegistrationTest`, `HandEdtCallTest`, `OutputEditorTest` | `ide/hand.ts`, `output.ts` / `hand_lifecycle`, `hand_provider`, `output_provider` | Available in both |
| Streaming, thinking folds, Markdown, Mermaid | `MagiToolWindow`, `NativeMarkdownSurface` / `TailScrollTest`, `StreamingHeightTest`, `NativeMarkdownSurfaceTest` | `web/chat_view.ts`, shared diagram bundle / browser layout, markdown, virtualization | Thinking starts collapsed in both. Added VS Code Mermaid rendering, zoom, copy and preservation during subsequent streaming |

## Changed behavior

VS Code preserves the transcript and UI generation during automatic reconnection, sending the last durable event number as `since`. It ignores late callbacks from old connections and duplicate sequence numbers. Session changes and explicit reloads reset the cursor; a daemon cursor rejection also starts a full replay. Retries await the connection attempt. Closing the view cancels the waiting timer.

Thinking starts collapsed in both IDEs. A thought opened by the user stays open during streaming. JetBrains reuses the closed component and updates only the appended text in an open draft. VS Code retains the draft DOM too.

The JetBrains plan window now includes a council control. Canceling confirmation or receiving a daemon rejection keeps the existing value. Saved profiles selected in the info popup now call `use-profile`, and status includes the current profile name.

## Validation scope and limits

VS Code Node tests passed 653 of 660 tests, with seven platform-dependent skips. All nine Playwright groups passed. They include 56 registered scenarios and two separate long-transcript virtualization tests. Mermaid checks use the real bundle to generate SVG, zoom, copy source and retain the SVG during later streaming. Missing bundles now fail preflight.

The full JetBrains core run passed 451 of 458 tests, with five skips and two failures caused by the existing file described below. All 225 IntelliJ tests passed. The existing untracked `ZzReplayProbeTest.kt` belongs to other work and was left untouched. It constrains full core source-tracking and documentation-inventory checks, as well as ordinary test compilation. The changed core tests pass separately, and plugin packaging is also checked.

This does not imply equal test counts. VS Code has property-based draft recovery tests and browser accessibility checks; JetBrains tests actual Swing and JCEF components. Some paths still have only source-text assertions. This work did not manually exercise every management dialog or installed Windows and Linux IDEs. VS Code `test:live` checks extension activation and command registration; it is not an outage recovery test.

## Management screen follow-up

JetBrains knowledge management now reloads the full inventory after a plugin action, so an acknowledgement without inventory does not erase existing entries. Connection failures preserve the last inventory and disable mutations until a fresh read succeeds. Slot notifications arriving during a request are coalesced into one later read. Revision conflicts reload the current revision; closing the dialog cancels pending requests.

`KnowledgeDialogTest` and `ProfileMaintenancePanelTest` click actual Swing controls to verify responses, failures, retries and closing. Transport responses are injected, so these are not end-to-end tests against a live server. JetBrains reports missing profile status bodies as errors; VS Code also rejects malformed JSON structures. The follow-up VS Code run passed 661 of 668 tests, with seven skips. All 231 tests passed in the JetBrains IntelliJ follow-up run.
