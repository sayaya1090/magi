# Chrome browser client

Magi Browser exposes explicitly granted Chrome tabs to a dedicated browser agent. It replaces the retired GitHub-specific extension. It uses Chrome Manifest V3, sidePanel and scripting APIs; it does not require remote debugging or a GitHub account.

## Install and connect

Build an updated core with `go build -o /tmp/magi ./cmd/magi` and the helper with `go build -o /tmp/magi-browser ./clients/browser/cmd`. Configure Magi's model first. Start `MAGI_BINARY=/tmp/magi /tmp/magi-browser`; port 8737 must be free. The helper prints a local bearer token. `MAGI_BROWSER_TOKEN` optionally supplies a fixed token. Keep the helper and core running.

In `chrome://extensions`, enable Developer mode and load `clients/browser/extension` as an unpacked extension. Open its side panel, paste the helper token and connect. Open an ordinary HTTP(S) page, press the grant-tab button and accept the origin permission. Grant each desired tab separately. The revoke button removes all local tab grants. Browser grants live only while the panel is open; reopening requires new grants. The connection target is a random ID persisted in extension-local storage; the token is stored only in Chrome session storage.

Ask Magi to inspect or operate on a granted tab. The panel supports chat, interruption, new sessions, model changes, tool approval and question replies. After changing the origin of a granted tab, grant it again. Navigation tools currently stay within the granted origin.

## Tools and boundaries

| Tool | Behavior |
|---|---|
| `list_tabs` | List granted tabs, without exposing unrelated tabs |
| `read_page` | Read bounded main-frame text and up to 100 actionable elements |
| `navigate` | Navigate a granted tab within its allowed HTTP(S) origin |
| `click` | Click an element identified by a fresh page snapshot |
| `fill` | Replace a non-password text field, at most 10,000 characters; does not submit |

Navigation, clicking and filling require a separate browser confirmation for each operation. Core permission settings cannot skip that confirmation. A changed page invalidates element IDs; read it again. Password fields, incognito tabs, browser-internal pages, cookies, arbitrary script execution, file access and download tools are excluded. Main-frame DOM inspection does not cover cross-origin frames, canvas or closed shadow roots. Click/fill results report the API action, not application-level success; read the page again to verify the outcome.

The authenticated loopback helper uses appbridge bindings and session-bound MCP URLs. The browser agent permits only browser MCP and common planning/question/recall tools, without shell or source editing. Page content is untrusted input. Actual sending, publishing, purchasing or deleting still requires the user's intended authorization. A rejected or timed-out operation must not be retried blindly because its effects may already exist.

## Verification and next acceptance

Run `go test -race ./clients/browser/... ./clients/appbridge`, `go vet ./clients/browser/... ./clients/appbridge`, `node --test clients/browser/test/*.test.mjs` and `node clients/browser/test/ui-playwright.mjs`. Browser tests mock Chrome and the helper; page tests exercise real DOM in Playwright. They do not establish acceptance of an installed Chrome extension or a live model session.

For live acceptance, load the unpacked extension and use a disposable local HTTP form. Grant only that tab, connect the helper and ask the model to read it. Confirm one fill/click, verify the DOM result, deny another action, and check the field stays unchanged. Navigate or alter the DOM before using an old element ID and confirm rejection. Check that ungranted tabs and password fields stay unavailable, then switch sessions during a pending action and confirm it cannot execute. Record the core commit, Chrome version and observed results. Do not use a real payment or messaging form for these checks.
