# Chrome browser client

Magi Browser exposes explicitly granted Chrome tabs to a dedicated browser agent. It replaces the retired GitHub-specific extension. The extension uses Chrome Manifest V3, sidePanel and scripting APIs. Isolated headless tests use Playwright Chromium or installed Chrome. Neither mode requires a GitHub account.

## Install and connect

Build an updated core with `go build -o /tmp/magi ./cmd/magi` and the helper with `go build -o /tmp/magi-browser ./clients/browser/cmd`. Configure Magi's model first. Start `MAGI_BINARY=/tmp/magi /tmp/magi-browser -mode extension`; port 8737 must be free. The helper prints a local bearer token. `MAGI_BROWSER_TOKEN` optionally supplies a fixed token. Keep the helper and core running.

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

## Optional browser MCP registration

The core does not register a built-in `browser` tool. Browser tools are available only after the user installs and starts the helper and registers its MCP server. Connect the Chrome panel first; start headless mode with explicit allowed origins. Modes never fall back automatically.

```toml
[mcp.servers.browser_runtime]
url = "http://127.0.0.1:8737/mcp"
[mcp.servers.browser_runtime.headers]
Authorization = "Bearer ${MAGI_BROWSER_TOKEN}"
```

Headless mode uses port 8739 by default. The token must match the helper. `/mcp` initializes only when exactly one browser connection exists and pins that window and chat. It refuses ambiguous windows; use an explicit `/mcp/<binding>/<session>` address in that case. Reconnect MCP after closing the original window or changing its chat.

Each MCP connection has a separate execution identity, keeping headless profiles separate. Chrome calls use the original panel's granted tabs and confirmation flow. Disconnect cleans up profiles and pending calls. Sending, purchasing and deletion still require the user's authorization; inspect results before retrying a timeout.

Independent testing continues to use `companions`, `companion_can` and `hand_off`. The recipient must have a registered browser MCP server too. Missing installations are not treated as available browser functionality.

## Headless test startup

Install the existing Playwright dependencies and browser with `npm ci --prefix clients/web/e2e` and `cd clients/web/e2e && npx playwright install chromium`. Start a scoped helper:

```sh
MAGI_BINARY=/tmp/magi /tmp/magi-browser -mode headless \
  -allow-origins http://127.0.0.1:3000 -url http://127.0.0.1:3000
```

Headless mode listens on 8739 and creates an independent browser companion for test handoffs. `-allow-actions` defaults true for this CLI: starting with explicit origins authorizes test navigation/click/fill in that scope. Set `-allow-actions=false` for read-only testing. The backend library defaults to read-only unless enabled explicitly. This companion starts with core permission=allow while the browser role and startup origins still restrict execution; existing agents retain their own core approval policy.

Each lease uses a separate temporary profile. No user Chrome cookies or accounts are imported. All HTTP requests, redirects and subresources must match the supplied origins; include fixture asset origins explicitly. WebSockets and service workers are disabled. Use `-channel chrome` for installed Google Chrome or omit it for Playwright Chromium. `-runner` overrides the runner path. Source-built installation still needs the runner, `extension/page.js` and Playwright dependencies; copying only the Go executable is insufficient for headless mode.

Runtime credentials are published under Magi's `runtime/browser-headless.json` or `browser-extension.json` with owner-only permissions. They are never included in model-visible discovery results. Other application agents gain the browser entry point and existing companion collaboration tools; they do not gain another application's MCP tools or shell access. Detailed contracts are in [browser collaboration design](../../docs/BROWSER_COLLABORATION_DESIGN.md).

The dedicated browser companion’s primary session has `browser_runtime` MCP tools attached because browsing is its main task. Existing agents and delegated side sessions use the small lazy entry point; their catalogs do not receive the full browser schemas.

## Window-scoped panel and management

Use the side panel per Chrome window. Switching tabs retains the chat, and one chat manages multiple explicitly granted tabs. Grant current tab adds the active tab of the panel's own window. Moving a granted tab to another site or window requires a new grant. Site permission alone does not authorize every tab.

The target ID is stored in `chrome.storage.session` under the window ID. Panels in different windows use separate connections and chats. Reopening a panel during the same Chrome run recovers its window target, but tab grants must be added again. Fully closing Chrome removes these IDs. Automatic restoration of a previous window's chats after a browser restart is not provided.

Management follows the Office layout and includes a local Material Web bundle. Settings select saved providers and manage their names, endpoints, keys and models. An empty key preserves the saved value; a separate checkbox deletes it. Editing the active profile applies to the next request, with save and application failures reported separately. Session listing and restoration accept only chats associated with the current window target.

Standing rules and skills are stored in the target workspace and reread for the next request. Council toggles save configuration and apply on the next turn without restarting. Confirmation explains that part of the context may need processing again; cancellation preserves the setting. New chat, session selection, model changes, interrupt and context compaction are also available. Management does not extend tab access permissions.

`node clients/browser/test/panel-playwright.mjs` loads the actual extension HTML, CSS, Material bundle and panel script. Chrome APIs and helper responses are mocked. It checks separate window connections, session switching, settings saves and 320px horizontal bounds. Existing `ui-playwright.mjs` and `browser.test.mjs` check grants, revocation, origin boundaries, password exclusion and stale snapshot rejection. Real extension installation and model-driven multi-tab operations still require separate acceptance.
