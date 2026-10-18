# Delegated approvals

A tool requested by another companion still needs the local operator's permission. A standing `allow` setting does not authorize that request. The operator must be able to review it without selecting the delegated chat.

`App.PendingPrompts` collects pending permissions and questions from delegated sessions. The local daemon exposes them in `jobs.approvals`, with the source `session`, `requester`, and full `prompt`. Existing clients can ignore this optional field. Fleet permissions are unchanged.

VS Code and JetBrains poll independently of their chat and status panels. A new prompt produces one notification. Pending requests remain accessible in the status panel; VS Code also provides a status-bar entry and the `magi.approvals` command. Review shows the requester, source session, arguments, reason, diff, and question report. JetBrains uses the same `ReviewSurface` body formatting, spacing, and background as local review above the main composer, preserving the originating companion’s ordered report sections and source-session response routing; notification and status actions open that area. Changes open in the existing IDE editor. Delegated free-text answers use a separate field within review to preserve the main chat draft. VS Code currently uses native review dialogs. Closing or hiding review leaves the request pending.

Before submitting, each client checks that the source session and prompt ID are still pending. The daemon accepts only the first response. Permission actions offer Allow once, Reject, and Auto-approve this task. `task_allow` authorizes routine tool calls only within the originating delegated task explicitly approved by the user. Scanner-forced approvals and user questions still require a response. Deny mode and tool access restrictions remain in force. The grant expires on task completion, cancellation, the next task, or process restart; it is never saved to project settings. Questions use the existing `answer` method.

Interactive delegated requests have no approval timer. Explicit response, cancellation, or process shutdown ends the wait. This does not promise recovery of an in-flight call after daemon restart. Scheduled ordinary work and headless runs retain their existing bounds and permission rules.

| `permission.decided.origin` | Meaning |
|---|---|
| `user` | An explicit approval or rejection arrived |
| `timeout` | Nobody responded before an applicable deadline |
| `policy` | Permission settings or a guardrail decided |
| `cancelled` | The request context was cancelled |
| `headless` | No interactive approval channel is available |

A rejected tool result uses the recorded reason. Successful approvals have no denial reason. Old events without these optional fields remain readable; their provenance cannot be reconstructed reliably from the old `deny` value alone.

Regression tests cover waiting past the ordinary timer, global discovery, source-session routing, review dismissal, notification deduplication, stale prompts, and distinct refusal reasons. Already expired handoffs are not retried automatically.
