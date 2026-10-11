# Magi for GitHub

This Chrome side panel connects the current github.com repository, issue or pull request to Magi. The local helper checks that its configured checkout's `origin` matches the selected repository. The coding agent works in that checkout; the extension does not edit GitHub's page or submit comments.

## Install and configure

Build Magi and the helper from the repository root:

```sh
go build -o /tmp/magi ./cmd/magi
go build -o /tmp/magi-github ./clients/github/cmd
export MAGI_BINARY=/tmp/magi
/tmp/magi-github -repo /absolute/path/to/checkout -repository owner/repository
```

Configure a model in Magi before connecting. `MAGI_CONFIG_DIR` selects its configuration directory. The helper listens on `127.0.0.1:8737`; only one helper can use that port. `MAGI_GITHUB_TOKEN` optionally supplies its local connection token. Otherwise copy the token printed by the helper. Keep that token private.

In Chrome, open `chrome://extensions`, enable Developer mode, press **Load unpacked**, and choose `clients/github/extension`. Open a repository, issue or PR at `https://github.com`, then press the extension action to open its side panel. Paste the local token and press **현재 GitHub 페이지 연결**. The extension requests `sidePanel`, session `storage`, github.com access and access to the loopback helper. It has no content script or remote script. Token storage lasts for the Chrome session.

`GITHUB_TOKEN` belongs to the helper process, not the extension. Public repository reads may work without it. For private repositories use a narrowly scoped token with metadata, issues and pull-request read access. No GitHub write tool is registered. Credentials never enter tool results. Rebuild the helper after source changes and reload the unpacked extension after UI changes.

## Use and sessions

Repository pages bind `owner/repository`; issue and PR pages bind their numbered target. Other paths in the same repository bind the repository. Navigation to another tab or a new URL in the connected tab clears the active binding; reconnect before sending. Background tabs navigating do not clear the current conversation. Pending request or new-session responses cannot reactivate a panel cleared by navigation.

Send a coding request, interrupt work with **작업 중단**, choose a model name and press **모델 변경**, or start **새 세션**. A new session uses the same target and local checkout. The request input clears only if its original request succeeds in the same binding and the user has not edited it meanwhile. Work may continue in Magi after closing the panel; reconnect to read persisted events.

Approval cards show the requested tool and arguments; Allow/Deny responses go to their original binding. Questions offer choices or a text answer. The coding agent can use Magi's coding tools, so inspect approvals before permitting changes. GitHub text is untrusted input. Remote comments, pushes and merges require an explicit user request; the added GitHub tools only read.

## Scope and recovery

`github_repository`, `github_issue` and `github_pull` read only the bound target and require an empty argument object. They return selected metadata, issue/PR bodies and PR branch refs/SHAs. HTTP responses over 1 MiB are rejected; strings over 12,000 characters are clipped with `truncated:true`. Comments, files, check runs, pagination and attachments are not fetched. github.com is supported; GitHub Enterprise is not.

The panel displays plain text, not Markdown. Event streams reconnect after one second and skip already seen numbered events. Transient unnumbered questions may be repeated, with duplicate controls filtered by call ID. Failed requests preserve the draft and show the helper's error. If authentication fails, check the local token. If the repository check fails, verify `git remote get-url origin` matches the configured owner/repository. HTTPS and `git@github.com:owner/repository.git` origins are recognized. If Magi does not start, check `MAGI_BINARY`, model credentials and configuration directory.

Do not claim a PR checkout is automatic: selecting a PR does not switch branches or fetch its head. Ask the agent to verify the local branch before editing. For a different repository, stop the helper and restart with its checkout and target.

## Tests and acceptance

Run `go test -race ./clients/github/...`. API tests cover malformed targets, target-specific tools, empty arguments, selected response fields, clipping, large bodies and safe status errors. Install browser dependencies with `npm ci --prefix clients/web/e2e`, install Playwright Chromium, then run `node clients/github/test/ui-playwright.mjs`. The test mocks Chrome APIs and local HTTP endpoints and checks late request/new-session isolation, text-only rendering, SSE reconnect deduplication, background-tab navigation and manifest permissions.

Real Chrome extension installation, a live GitHub account, private repository permission, production API limits and actual coding-agent execution have not been verified. Before deployment, load the extension in Chrome and verify repo/issue/PR navigation, model failure recovery, approval/denial, a new session, side-panel close/reopen and a disconnected helper. Use a disposable checkout to verify an approved file edit and a refused remote write. Check that the displayed target, checkout origin and branch all match before testing.

## Official interface review (2026-10-12)

The helper now pins REST version `2026-03-10`. The earlier `2022-11-28` version remains supported until 2028-03-10, so its use was not an expired-API bug. The [version policy](https://docs.github.com/en/rest/about-the-rest-api/api-versions) and [breaking changes](https://docs.github.com/en/rest/about-the-rest-api/breaking-changes) were read before updating. Our projected fields do not include the removed repository settings or PR `merge_commit_sha`; head/base SHAs remain separate.

The [issue read contract](https://docs.github.com/en/rest/issues/issues#get-an-issue) requires Issues read permission for restricted repositories. The [PR read contract](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request) accepts Pull requests read or Contents read. The [authentication guide](https://docs.github.com/en/rest/authentication/authenticating-to-the-rest-api) explains why 404 can also mean hidden or inaccessible private data. Local helper authentication and GitHub authentication remain separate.

The [Side Panel API](https://developer.chrome.com/docs/extensions/reference/api/sidePanel) is available in Chrome 114+ for MV3; the manifest now states that minimum. `side_panel.default_path`, `action` and `setPanelBehavior({openPanelOnActionClick:true})` match the toolbar opening contract. We do not call `open()` without a user gesture. This is a global panel; github.com-only connection checks are enforced by our client rather than tab-specific panel enablement.

The [Tabs API](https://developer.chrome.com/docs/extensions/reference/api/tabs) permits reading matching github.com URLs through host permission, so a broad `tabs` permission is unnecessary. Navigation to an unpermitted site can hide its URL; `status:loading` now also clears the bound tab, preventing a stale GitHub target. Background-tab loading remains ignored. [Session storage](https://developer.chrome.com/docs/extensions/reference/api/storage) is restricted to trusted extension contexts and clears on extension disable/reload/update as well as browser restart. Token restore errors and background initialization rejections now have explicit guidance. These document checks and mock tests do not replace real Chrome or GitHub acceptance.
