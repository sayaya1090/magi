# Magi mail client

This local browser client connects one IMAP/SMTP account to Magi. It lists the newest 50 INBOX messages and opens a read-only plain-text view with a separate original MIME view. Selecting a message connects a Magi session identified by account, mailbox, UIDVALIDITY and UID. UIDVALIDITY detects a mailbox rebuilt by the server; refresh the list before opening its messages again.

The model can read the selected message or save a draft. There is no sending tool. The user reviews the recipient, subject and full body in the browser and confirms the send dialog. A failed SMTP response after DATA may leave delivery uncertain; check Sent or the recipient before retrying.

## Run

Start the Magi daemon with the same configuration directory, then run:

```sh
export MAGI_MAIL_IMAP=imap.example.com:993
export MAGI_MAIL_SMTP=smtp.example.com:465
export MAGI_MAIL_USER=you@example.com
export MAGI_MAIL_PASSWORD='your-app-password'
export MAGI_MAIL_FROM=you@example.com
go run ./clients/mail/cmd
```

Open `http://127.0.0.1:8793`, paste the local bearer token printed by the process, and press **INBOX 새로고침**. Use an app password where your provider requires one. Keep credentials outside source control and shell history. Credentials never enter Magi tool results. `MAGI_CONFIG_DIR` selects the daemon configuration directory; `MAGI_MAIL_ADDR` changes the loopback listener and `MAGI_MAIL_TOKEN` sets the local UI token.

IMAP uses implicit TLS. SMTP defaults to implicit TLS, typically port 465. For port 587, set `MAGI_MAIL_SMTP_TLS=starttls`; sending refuses to authenticate if the server cannot establish STARTTLS. TLS certificates are verified and TLS 1.2 is the minimum. Server requests have connection and operation deadlines.

## Scope

This first version supports one account, one recipient per draft and INBOX only in the UI. The read API supports another mailbox when explicitly requested. It does not implement OAuth, attachments, search, folders, reply threading, delete/move, delivery retries or Sent-folder append. Multipart plain text, base64 and quoted-printable are decoded; legacy non-UTF-8 charset conversion is not implemented. HTML never renders as HTML. HTML-only mail is available as original MIME, with a notice in the body view. Messages above 2 MiB and drafts above 1 MiB are rejected.

Magi events render as user messages, assistant replies, tool calls/results, errors and completion notices. The first version uses plain-text conversation rows; it does not render Markdown. Permission requests offer Allow/Deny controls and questions offer choices or a text answer. Switching messages clears recipient, subject, body and chat input, and cancels the old event stream; tools remain bound to their original selected message. Drafts exist only in this process and are lost on restart.

## Verification

Run `go test ./clients/mail/...` and `go test -race ./clients/mail/...`. Tests cover selected-message ownership, a missing send tool, explicit send consent, header injection, MIME decoding, draft limits and text-only DOM rendering. Fake accounts validate contracts without sending mail.

Real IMAP/SMTP accounts have not been tested. Before relying on this client, use a test mailbox to verify list/read against a certificate-valid server, UIDVALIDITY changes, expired credentials, SMTP STARTTLS, one confirmed delivery, cancellation, reconnect and session isolation between two messages. Check a real draft against the envelope recipient and the delivered MIME. Browser automation should mock mail and Magi endpoints; it must not send real mail.

## Build and recovery

Build an executable with `go build -o /tmp/magi-mail ./clients/mail/cmd`, then run `/tmp/magi-mail` with the same environment variables. Go embeds the browser page, so the executable does not require a separate web directory. Install the executable on PATH only after the local tests pass. The local bearer token is a client connection secret, separate from the IMAP password. Avoid sharing console output containing it.

Select a message before sending a Magi request. Use **Magi 초안 가져오기** to copy that message's saved draft into the editor; edit it before confirming delivery. Changing messages clears the visible draft deliberately so another message's recipient cannot survive unnoticed. A draft response arriving after a selection change is ignored. Ask/approval buttons retain their original binding and cannot answer another message's request.

If the bearer token is wrong, replace it and refresh INBOX. If IMAP authentication fails, stop the client, update the app password and restart. If Magi connection fails, confirm `magi` is on PATH and that its model settings are valid; select the message again. The bridge can start its own isolated companion. A broken event connection displays an error; reselect the message to reconnect and replay persisted conversation. Unsaved edits and in-memory drafts do not survive a restart. The UI preserves typed input after a failed request, so retry after resolving the connection error.

For local browser tests, run `npm ci --prefix clients/web/e2e`, install Chromium with that package's Playwright tool, then run `node clients/mail/test/ui-playwright.mjs`. Tests also cover old draft clearing, late draft response rejection and readable text/plain bridge errors. No account credentials are needed for these tests.
# Official protocol review

Reviewed on 2026-10-12 against [IMAP4rev2, RFC 9051](https://www.rfc-editor.org/rfc/rfc9051.html), [mail TLS, RFC 8314](https://www.rfc-editor.org/rfc/rfc8314.html), and [SMTP AUTH, RFC 4954](https://www.rfc-editor.org/rfc/rfc4954.html). The current `go-imap` v1 dependency implements IMAP4rev1; this client does not claim full IMAP4rev2 support. A server must accept the rev1 commands used here. Moving to rev2 needs a separate capability and interoperability review.

`mail.go` uses implicit TLS for IMAP, validates server certificates, and requires TLS 1.2 or later. SMTP supports implicit TLS (normally 465) and mandatory STARTTLS (normally 587); it does not fall back to cleartext authentication. SMTP AUTH currently uses PLAIN over TLS. Servers requiring OAuth or another authentication mechanism are outside the implemented scope. Selected messages are bound to mailbox UIDVALIDITY and UID, and body reads use PEEK to avoid marking mail as read.

This review checks implementation contracts. It does not establish interoperability or delivery with a real mail provider.


## Session and execution controls

New session starts another conversation while keeping the selected mail target. Enter a model name and use Change model to select it; Stop interrupts the current run. Failed requests preserve typed input, and events from an old session are excluded after switching.

Before reading a body, RFC822.SIZE rejects messages above 2 MiB. Allowed messages use a partial BODY.PEEK request for at most 2 MiB plus one byte. `network_test.go` verifies oversized rejection before body fetch and bounded partial fetching against a local TLS IMAP server. This is separate from testing a real provider.
