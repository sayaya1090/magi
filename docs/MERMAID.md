# Communicating with developers through Mermaid

[English](MERMAID.md) · [한국어](MERMAID.ko.md) · [Documentation map](README.md)

Status: implementation reference and remaining scope. 2026-11-14.

## Purpose and agent guidance

Use a Mermaid diagram by default, with a short explanation, whenever an answer explains relationships, ordering or causation with two or more steps. This covers architecture, component relationships, request and data flows, state transitions, root-cause chains, the order of an operation, changes that span several files and comparisons of alternatives. Use actual code names and distinguish the implementation from proposals.

The `skill` tool can load `visual-explanation`. This built-in guide covers flowcharts, sequences, states and classes, the OOAD analysis/design distinction, and explaining GoF responsibilities and costs. Private course materials are not distributed. A workspace skill with the same name takes precedence. Load additional OOAD or architecture skills only when available and relevant.

## Tools and surfaces

| Surface | Presentation | Limit |
|---|---|---|
| JetBrains | Inject the local Mermaid script into the existing IDE Markdown panel. | Falls back to text when a JCEF Markdown panel is unavailable. |
| Web console | Convert Mermaid Markdown code blocks using the shared renderer. | Requires browser JavaScript. |
| VS Code | Load the renderer packaged with the extension into its webview. | Incomplete fences are not rendered. |
| Standalone local TUI | `mermaid_preview` saves HTML and returns a link. | Register the tool only after checking the environment. |
| SSH, CI, TUI attached to a remote daemon | Do not provide the preview tool. | Explain in text. Do not infer the user's browser from the daemon host. |

`mermaid` accepts `title` and `source`, plus optional `descriptions` and `animate`. Source is plain Mermaid without a code fence. The tool checks input size and policy; it does not claim that Mermaid syntax was validated. Clients display rendering errors and preserve the source. Graphical clients receive a Mermaid fenced block in the final answer.

TUI answers contain a short explanation and the HTML link only. Do not expand the source. Source copying is available inside the preview. Files are saved under `magi/diagrams` in the user cache; no browser is launched automatically. Users can remove these files through their cache cleanup policy.

## Browser availability

Check before registering the TUI tool and again when executing it. Exclude SSH and CI. On macOS, require a console session owned by the current user and Safari discoverable through Launch Services. On Linux, require DISPLAY or WAYLAND_DISPLAY, xdg-open, a default browser setting and a known browser executable. Windows TUI preview remains hidden.

These checks establish prerequisites. They cannot guarantee a window will open. The tool therefore reports that a page was saved, never that a browser opened. The three graphical clients use their own web renderers and do not depend on the server's browser probe.

## Interaction

Fit preserves a small diagram’s intrinsic size and shrinks larger diagrams to the available width and a height of at most 420 pixels or 55% of the viewport. Zoom remains available for reading dense charts. Icon controls show labels on hover and keyboard focus.

Offer zoom, fit, source copying, node selection and question copying. Select a node, copy the prepared question, then paste it into the composer. Nothing is submitted automatically and no code is executed. Enter and Space select nodes too. When clipboard access is denied, expand the source and offer manual copying.

Supply `descriptions` as an object keyed by each unique visible node label, for example `{"Agent":"Reads the request and prepares a reply."}`. Selecting a node opens its explanation to the right, or below on narrow screens. Missing descriptions are marked as missing. Right-click or Shift+F10 opens the editable follow-up question; Copy question includes the diagram context. Clipboard denial exposes a selectable copy field. Questions are never sent automatically.

Set `animate: true` only for flowcharts whose arrows show data movement. The renderer adds moving dashes, a checkbox to stop them, and respects the system’s reduced-motion preference. Class and state diagrams are unaffected. The tool preserves both options as `%% magi-descriptions` and `%% magi-animate` comments in portable Mermaid source; keep these comments when placing the result in an answer. Descriptions are plain text, with at most 100 entries and 2000 bytes per description, within the total 24,000-byte source limit.

File/symbol navigation, path highlighting, live execution state and direct insertion into the composer remain future work. Do not describe them as implemented.

## Assets and boundaries

The shared implementation is `internal/diagram/web/view.js` and the Mermaid 11.17.2 bundle. Go embeds them; VS Code and JetBrains builds package the same files. No CDN is called. Keep the license with the bundle.

Limit tool input to 24,000 bytes. Reject HTML, URLs, click directives and configuration overrides. Render with Mermaid strict mode, then remove scripts, external images, links and event attributes from SVG. Model-provided JavaScript never executes. Serialize rendering and cache the most recent 32 sources.

## Validation and remaining work

`node internal/diagram/render-test.mjs` checks rendering, zoom, selection, repeated SVG IDs, syntax failures, input restrictions, streaming, narrow layouts and absence of external requests in Chromium. Go tests cover tool input, SSH refusal, attached-TUI visibility and loading the built-in skill.

Installed JetBrains and VS Code acceptance, all Linux desktop combinations and Windows TUI support need separate verification. An installed browser is not proof that opening a link will succeed.
