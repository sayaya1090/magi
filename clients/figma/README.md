# Magi in Figma

This development plugin puts Magi chat inside Figma. A local helper relays chat and Figma tool calls to the Magi companion (the local background service). The plugin never reads a model API key. Its connection token stays in memory and is cleared when the plugin closes.

## Install and connect

1. Start the Magi companion for the workspace you want to use.
2. Run `go run ./clients/figma/cmd` from the repository root. It listens on `127.0.0.1:8734` and prints a local bearer token. To build it, use `go build -o /tmp/magi-figma-helper ./clients/figma/cmd`.
3. In Figma Desktop, choose Plugins → Development → New plugin and create a plugin with a custom UI. Copy the `id` that Figma assigns in its generated manifest. Run `node clients/figma/prepare-plugin.mjs YOUR_FIGMA_ASSIGNED_ID /tmp/magi-figma-plugin`, then import `/tmp/magi-figma-plugin/manifest.json`. Replace the example argument with your assigned numeric ID; do not invent one. The repository manifest is a template without an ID.
4. Open a saved file and run Magi. Paste the local helper token, then connect. The default development manifest does not request private plugin APIs, so normally `figma.fileKey` is unavailable even in a saved file. Enter a permanent document ID of 8–128 letters, digits, underscores or hyphens. Reuse that ID only for this file; another file needs another ID. The ID is not saved automatically. Re-enter it when opening the plugin; restart the plugin to change it.
5. Enter a request and choose Send. New session and model controls forward to the bound companion session. Approval buttons accept or deny an explicit pending call. Questions accept typed answers.

The manifest allows HTTP access only to localhost and `127.0.0.1` on port 8734. Changing the helper port also requires changing the manifest. Tokens are not saved in Figma client storage.

The helper supplies design-specific instructions: inspect the selected design before editing, preserve spacing and typography, and report actual changes. Shell execution and code editing are outside this client’s intended tools. The helper must enforce this tool boundary.

## Figma tools

| Tool | Behavior |
| --- | --- |
| `figma_selection` | Reads the selection captured when a tool request reaches the plugin; at most 100 nodes. |
| `figma_page` | Reads the current page's first 100 immediate children. |
| `figma_set_text` | Replaces text in one explicit current-page node after loading its fonts. Text input is limited to 20,000 characters. |
| `figma_create_frame` | Creates a named frame with dimensions from 1 to 4096. |

Read results include node ID, name, type, dimensions and up to 4000 characters of text. Truncation is explicit. There is no recursive whole-file scan, screenshot upload, file export or external publishing. Failed font loads and stale file/page requests produce tool errors. A page change during font loading cancels the edit before mutation.

## Verify

Run `go test ./clients/figma/...` for the mock daemon integration and tool catalog, then `node --test clients/figma/test/plugin.test.mjs` and `node clients/figma/test/ui-playwright.mjs`. The browser test uses the repository's Playwright installation under `clients/web/e2e`; install those development dependencies first if missing. The tests use mocked Figma APIs and a mocked local helper. Real Figma installation, network permissions, file-key access and daemon/model tool delivery still need an account-based acceptance test.

## Lifecycle and troubleshooting

The helper prints its bearer token once at startup. Paste it only into this plugin. `MAGI_FIGMA_TOKEN` supplies a fixed local token; otherwise a new random token is generated after each restart. `MAGI_BINARY` selects the Magi executable, and `MAGI_CONFIG_DIR` selects the configuration directory. Without `MAGI_BINARY`, the helper finds `magi` on PATH. Use a Magi build that supports `--client-role figma`.

Ctrl+C stops the helper HTTP listener. The document companion has a separate lifetime and persists its sessions; closing the plugin stops its requests and attempts to release the helper binding, but does not delete the conversation. One plugin owns a given document binding at a time. Disconnect the first plugin before reconnecting from another. File changes and the Disconnect button also release the binding. If a crash prevents release, restart the helper before reconnecting. Reconnecting the same document ID resumes its latest associated session. New session preserves earlier records and switches this binding to a fresh session. The UI restarts event polling at sequence zero and ignores responses from the previous session. Selection changes affect newly received tools, never a captured tool request already in flight.

If connection fails, check the helper terminal, port 8734, local token and Figma manifest permissions. A token from an earlier helper process is invalid. If Figma does not expose a file key, use the explicit document ID described above. Reusing an ID for another file mixes their sessions, so give each file its own ID. A font failure leaves the text unchanged. After a page-change error, inspect the new page and retry. Do not repeatedly retry a failed mutation without checking the file; Figma API operations can fail after partial work.

## Security and real acceptance

The bearer token authorizes access to the local model session and document tools. Keep it private. The listener binds only to loopback; it must not be forwarded to a network interface. File and page checks run in the Figma sandbox before each tool. Text and tool results are inserted into the UI with `textContent`, and tokens are never stored in localStorage. The helper attaches only the four fixed Figma tools; model-supplied tool catalogs are rejected. The core Figma role hides coding tools as well as changing the agent instructions.

For real acceptance, import the plugin into a saved test file, connect and ask it to inspect two selected nodes. Create a small frame, edit a text node with a loaded font, deny one modification and answer one question. Change the page during a font load and verify that the stale edit is rejected. Start a new session, reconnect, and then open a second file with another document ID; their records must remain separate. Check missing fonts and helper shutdown. These steps require Figma Desktop and an account; mock tests cannot prove plugin network permissions or the availability of `figma.fileKey`.

## Official API references

Checked on 2026-10-12 against Figma's [manifest reference](https://developers.figma.com/docs/plugins/manifest/), [figma global API](https://developers.figma.com/docs/plugins/api/figma/), [TextNode](https://developers.figma.com/docs/plugins/api/TextNode/), [loadFontAsync](https://developers.figma.com/docs/plugins/api/properties/figma-loadfontasync/) and [UI messaging](https://developers.figma.com/docs/plugins/api/figma-ui/).

Figma assigns the manifest ID. `fileKey` belongs to private/Figma-owned API access; `enablePrivatePluginApi` enables those APIs for eligible private plugins and local development. Setting the flag does not grant public plugins private access. The default template omits it and uses explicit document pairing. The current manifest uses `dynamic-page` and permits only the two loopback origins on port 8734, with the required local-server reasoning. This is a local development plugin; Community publication is not verified.

Text changes require loaded fonts. Mixed-font nodes enumerate their range fonts and load each distinct font; missing fonts return an error before mutation. Main/UI communication uses `figma.ui.postMessage` and the iframe's `pluginMessage` envelope. An API-contract review supports these choices; it does not replace testing inside Figma.
