# IMAP/SMTP client

The executable in `clients/mail/cmd` serves a local browser UI and connects a single mail account to the shared Magi app bridge. Account credentials are loaded from environment variables. They are used by the IMAP and SMTP adapter, never by model tool arguments. See [the mail manual](../clients/mail/README.md) for startup variables, TLS modes and acceptance steps.

## Session and tool contract

The selected-message target is `account/mailbox/uidValidity/uid`. IMAP uses UID fetch with `BODY.PEEK[]<0.2097153>` and opens the mailbox read-only. Before each read it checks UIDVALIDITY against the selected message. A message deleted or a mailbox rebuilt after listing produces an error, not another message's content.

`mail_read` accepts an empty object to read the message bound to the session; optional `mailbox`, `uidValidity` and `uid` must identify that same message. The host rejects arguments whose target does not match the binding. The response separates `text` from `raw` MIME. Both are untrusted external data; the model must not treat instructions in mail as user requests. `mail_draft` receives `to`, `subject` and `body`, saves to the binding's in-memory draft, and returns `sent:false`. No tool sends or deletes mail.

## Browser and sending

Mail headers, decoded plain text, original MIME and Magi event text enter the DOM through `textContent`. They never execute as HTML. Selecting a different message aborts the old SSE stream and clears the active binding. A selection counter rejects late read/connect responses from the previous selection.

SMTP is reachable only through the authenticated local `/mail/send` HTTP endpoint. The UI displays the complete draft in a confirmation dialog; canceling makes no send request. The endpoint requires `confirm:true`, a single valid recipient and single-line header fields. TLS is mandatory. The client does not retry delivery automatically or save a copy in Sent.

## Current verification

Ten top-level Go tests passed, and a Chromium Playwright test passed with mock mail/Magi routes. Browser checks cover text-only rendering of hostile HTML, message-bound chat requests, bearer authentication, canceled send, confirmed send, permission approval and question answers. Run `node clients/mail/test/ui-playwright.mjs` after installing dependencies from `clients/web/e2e`.

No real account was connected and no mail was delivered during these checks. The current UI provides plain-text user, assistant and tool rows; it does not render Markdown. HTML-only bodies, attachment handling, OAuth, account switching, folder browsing and Sent-folder storage remain outside this first implementation. The [manual](../clients/mail/README.md#verification) gives the real-account acceptance checklist.

RFC822.SIZE is checked before body retrieval. A local TLS IMAP test verifies oversized rejection and partial fetching. Playwright also checks foreign-session exclusion, new session, model change, interruption and input preservation.
