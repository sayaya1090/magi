# Headless Chromium backend

This backend runs the same bounded browser tools in isolated headless Chromium. Tests should select this mode rather than the user's Chrome profile. No account cookies, extensions, or persistent user profile are imported. Node and Playwright from `clients/web/e2e` plus its Chromium installation are required.

`New(context, Config{AllowedOrigins: []string{"http://127.0.0.1:PORT"}, InitialURL: "...", AllowActions: true})` starts one tab (`tabId: 1`). Origins are explicit HTTP(S) origins without a path. Localhost is not implicitly allowed. All HTTP requests including redirects and subresources are intercepted; WebSockets and service workers are disabled. Extra windows are closed. `AllowActions` defaults false; enabling it is startup authorization for fixture navigation, clicks, and fills without per-call dialogs. Production extension confirmation remains separate.

`Call(ctx, target, name, jsonArgs)` implements `list_tabs`, `read_page`, `navigate`, `click`, `fill`. Each nonempty `target` identifies an isolated browser lease with a separate temporary profile. `Release(target)` closes it; the caller must enforce session ownership. Snapshots and element IDs use the existing extension page implementation, excluding passwords and bounded to 32,000 text characters and 100 elements. Mutations invalidate snapshots. No evaluation tool is exposed. Calls are serialized, response size is capped at 256 KiB, timeout terminates the runner without automatic retry. The runner removes its temporary profile during normal shutdown or termination; call `Close` to reap the subprocess. Forced kill or machine crash can leave temporary profile directories.

Real Chromium verification:

```sh
MAGI_HEADLESS_LIVE=1 go test ./clients/browser/headless -count=1 -v
```

This tests an isolated local HTTP fixture: origin denial, password exclusion, filling, stale IDs, click outcome, and startup action denial. It does not verify logged-in websites or a user Chrome profile.
