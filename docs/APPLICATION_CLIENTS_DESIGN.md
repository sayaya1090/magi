# Application client design and implementation contracts

[한국어](APPLICATION_CLIENTS_DESIGN.ko.md) · [Client overview](CLIENTS.md)

Figma, standard IMAP/SMTP mail, Slack and Chrome browser become new Magi clients. This document defines their shared implementation contracts and acceptance requirements. Each application manual explains installation, account configuration and everyday use. Mock tests and real account acceptance are reported separately.

## 1. Roles and surfaces

| Client | User surface | Agent work | Excluded defaults |
|---|---|---|---|
| Figma | Development plugin panel | Inspect selection, edit text, create frames | Shell, source edits, whole-file scans, publishing |
| Mail | Local browser mail UI | Read selected correspondence, identify requests, draft replies | Model-initiated sending, OAuth, attachments |
| Slack | App Home, DMs, mention threads | Summarize conversations, coordinate work, draft messages | Shell, source edits, unsolicited messages in other channels |
| Chrome browser | Chrome side panel | Inspect and operate granted tabs | Unrelated tabs, passwords, arbitrary scripts |
| Existing Office | PowerPoint, Word and Excel task panes | Presentation, document and spreadsheet work | Default coding identity, external shell and source editing |

`--client-role` and `MAGI_CLIENT_ROLE` select the role. Coding has the explicit kind `coding`. Empty input normalizes to `coding`; the removed `github` role is rejected. Chrome uses the `browser` kind. Other roles receive application-specific system instructions from `internal/app/client_role.go`. The same role restriction applies when advertising tools and executing calls. Only the application's MCP namespace and common question, council, planning and recall tools are permitted. A model cannot execute a hidden `bash` call or another application's tools.

Office passes its role when starting a new companion. Installing a new executable does not change the role of an already running older companion. Migration acceptance must distinguish fresh role-aware starts from restarts of older processes.


## 1.1. Role-specific prompts

Role source text lives in `internal/app/client_role.go`; the base coding prompt is `systemPrompt` in `cmd/magi/main.go`. This table summarizes those prompts and is not another runtime configuration.

| Role | Specific guidance |
|---|---|
| Base coding (terminal, VS Code, JetBrains) | Inspect the project, follow existing conventions, plan, implement and verify |
| Figma design | Preserve design intent, typography, spacing, components and accessibility; inspect selection, file and page before editing |
| Mail correspondence | Identify requests and draft in the user's tone; invent no facts, commitments or recipients; distinguish drafts from sending |
| Slack collaboration | Separate decisions from proposals, identify owners and actions, remain in the requesting user's channel and thread |
| PowerPoint presentations | Organize arguments, design and verify; use Korean outline phrasing; preserve editability; avoid unnecessary full reads |
| Word documents | Preserve facts, structure and author intent; use appropriate prose and consistent headings; read relevant sections |
| Excel analysis | Preserve formulas, types and structure; verify calculations; distinguish observations from assumptions; read relevant ranges |

The six application roles share user-language responses, truthful reporting, external content treated as data, and a prohibition on shell/source edits. Figma, Slack and mail MCP guidance applies only to available tools in the bound session. `looking` is a read-only session of the same role, not another application identity; council is a separate completion-evidence gate.

MCP initialize `instructions` enters the model request's trailing context. It cannot override user instructions, application role or permission policy, and is removed when its tools detach. `internal/app/mcp_instructions_test.go` verifies actual model-request delivery and session/role isolation.

### Type declaration and console integration status

The role vocabulary is the seven `ClientKind` constants in `internal/app/client_kind.go`. Validation and prompt selection use this declaration; unknown roles cannot execute tools. Session Agent (default/looking), user-written Role text and console screen type are separate values.

`CompanionType.java` registers coding type `1` and six application types. Base coding use the coding child; other business kinds use `application-ui`. Mail and Slack share communication type. The `coding` spelling resolves to type `1`, and the existing missing/unknown-type coding fallback remains. Local daemon/roster/fleet and remote cluster carry type. Remote type is advisory metadata excluded from Signable, like Version; execution rights come from the destination daemon's role gate.

The application child shares conversation/composer code without mounting the filesystem/Git workspace. The parent clears slots on type changes; PaneSharing caches module-specific renders for revisits. Late coding loads cannot replace application slots. WorkspaceStore rejects application contexts to prevent background file requests too.

`clients/web/ui/application-ui/test/compiled-playwright.mjs` runs actual GWT output at 1440px and 390px, checking application/coding transitions, toggle restoration, late module loading and zero application filesystem API requests. It does not verify accounts or real Figma/Slack execution. See the [application conversation manual](../clients/web/ui/application-ui/README.md) for reproduction.

### Business kind, codebase role and client

The business vocabulary is the eight AgentKind values in `internal/app/agent_kind.go`: coding, design, communication, document, presentation, spreadsheet, operations and browser. ClientKind selects application-specific instructions and tool boundaries; it is not a business kind. Mail and Slack map to communication but cannot use each other's tools. IDEs and terminal are coding clients.

Internal coding roles follow ownership of a codebase or module. Role describes the user-assigned ownership; concrete repository/path boundaries determine access. Investigation, design, implementation, verification and review are work phases performed by each owner, not separate agent kinds or ownership roles.

AgentKind mapping and client tool-isolation tests are implemented. Published type and the console catalog use business kinds, interpreting legacy application keys as their corresponding business kind. Client identity remains in the ClientKind execution configuration. Coding type 1 is retained for compatibility. Kind grouping must never widen tool permissions, session targets or codebase ownership.

## 2. Shared connection layer

`clients/appbridge` connects local HTTP clients to the daemon control socket. Figma calls pass through this layer into the plugin's main sandbox. Mail and Slack APIs run in their respective helpers. Saved model API credentials are never returned to application UI code. Provider management sends a newly entered key to the authenticated local helper and then clears the input.

```text
Application UI / Slack event
    ↓ local bearer authentication or Slack Socket Mode authentication
Application helper → appbridge → target-bound Magi session
    ↑                                  ↓ MCP call
Application operation ← binding-specific MCP URL
```

| Default address | Purpose |
|---|---|
| `127.0.0.1:8734` | Figma plugin helper |
| `127.0.0.1:8793` | Mail UI and IMAP/SMTP helper |
| `127.0.0.1:8736` | Slack helper MCP callback |

Listeners require an explicit loopback IP. Local APIs and MCP require `Authorization: Bearer <token>`. An unspecified token is generated with 256 bits of cryptographic randomness. Cross-origin browser extension and sandbox iframe requests require the bearer credential; CORS permission is not authentication. Browser text uses `textContent` rendering.

## 3. Target and session ownership

| Application | Target identity | Additional check |
|---|---|---|
| Figma | File key or user-supplied permanent document ID | Capture page and selection IDs; recheck before mutation |
| Mail | Account / mailbox / UIDVALIDITY / UID | Refuse old UIDs after UIDVALIDITY changes |
| Slack | Team / channel / thread timestamp / requesting user | Reject another user's approval actions |

`POST /api/connect` accepts `{target}` and returns `{binding, session, target}`. The helper generates the opaque binding ID. Application UI cannot provide arbitrary socket paths, work directories or session IDs. Sessions use `session-new` with `keep` and a `for` tag for creation and recovery. Connecting another document does not switch the companion's foreground conversation.

Figma, mail and Slack derive a hashed workspace directory for each target. Document bodies and titles never become work directories. Bindings live in process memory and must be reconnected after restart; persisted sessions are recovered by their `for` tag.

## 4. Chat and tool protocol

`POST /api/request` accepts a binding and a bounded command set. `say` or `prompt` maps to `submit`, `model` to `set-model`, `reply` to question `answer`, and `permission` to a permission response. `new`, `interrupt`, `status`, `models`, `context` and `compact` are supported. On a new session, UI applies the returned session and cancels the old event stream. Administrative commands such as `shell`, `config-set` and `shutdown` are unavailable through this API.

`GET /api/events?binding=...` forwards the daemon transcript as SSE, including persisted events and transient permission/question requests. UI separates original `part.appended` output from approval and question cards. It checks event session IDs and rejects late replies from another target. Slack uses a separate replay boundary to avoid reposting historical output into a channel.

`POST /mcp/<binding>/<session>` accepts MCP `2025-06-18` initialize, ping, tools/list and tools/call. Only registered application tool names are dispatched. The binding and session in the URL fix the target. An old session URL cannot be used in a new session. Handlers for tools with explicit document or recipient arguments must compare them with the original bound target. MCP annotations identify read-only tools.

Figma receives calls from `/api/figma/poll` and returns results through `/api/figma/result`. Random call IDs are bound to one binding. Foreign, duplicate and expired results are rejected. Calls time out after 30 seconds; a poll waits up to 20 seconds. A timeout does not prove that a mutation did not occur. Check the actual design before retrying.

## 5. Accounts and external operations

IMAP/SMTP credentials, Slack app/bot tokens stay in helper environments. `CompanionEnv` removes application tokens and mail passwords when starting a companion, retaining model-provider credentials. Existing processes need restarting because their environments cannot be changed. They are excluded from tool results and server error output. Figma invokes design APIs in its main sandbox and does not require a Figma API token.

The mail model receives read and draft tools. SMTP delivery is a separate UI action after the user reviews recipient, subject and body. Slack tools read the current thread and prepare drafts. Responses to an initiated Magi conversation stay in that thread; no tool sends to unrelated channels. Remote writes are not automatically authorized merely because implementation or testing would benefit from them.

## 6. Coding tasks and worktrees

Worktrees belong in a common coding task layer. Starting a repository task creates a separate branch/worktree and connects a companion that owns that workspace. Creating a directory and beginning the model's first turn are separate actions. General coding agents, VS Code, JetBrains, terminal clients should share this contract.

Figma, mail, Slack and Office do not expose worktree operations by default. They may delegate coding work to a separate coding companion. [Coding worktree design](CODING_WORKTREE_DESIGN.md) defines creation, recovery and cleanup. A worktree service is outside this initial client implementation's completion scope.

## 7. Implementation and verification order

1. Implement shared connection, session, authentication and role restrictions. Test with actual local sockets.
2. Connect chat, permission responses, questions and events in each application. Check target/input preservation on new sessions, model changes and connection failure.
3. Test tool success, failure, foreign targets, malformed arguments and large responses. Mail/Slack delivery tests use mock servers or transports.
4. Run Playwright for Figma, mail and Chrome browser surfaces. Also run the existing full VS Code transcript harness.
5. Follow owner self-review with another owner's code/document/test review. Fix findings and rerun affected verification.
6. Record real account acceptance separately. Without an account, do not claim authentication, app installation or actual message delivery passed.

Each manual must include real run commands, credential locations, session identity, supported tools, size limits, recovery steps, test commands and unverified scope. Update this design and both language editions when contracts change.

### Actual daemon integration check

`clients/appbridge/real_daemon_test.go` connects the actual App, JSONL store, Unix-socket daemon, MCP Manager and HTTP server. It checks session creation, initialize guidance, target-bound tool calls, foreign-session rejection, tool reattachment on new session, and removal on disconnect. A test engine wrapper exposes the App session API to the daemon. This does not test CLI child-process startup, application accounts or model API execution. Run `go test -race ./clients/appbridge -count=1`.

Shared business prompts live in `AgentKind.System()`. Application prompts combine common language/safety guidance, business identity and client constraints in that order. Mail and Slack receive the same communication guidance plus sending-confirmation or channel/thread rules. Coding retains the existing base system prompt.

## 8. Verification record (2026-10-12)

The implementation was cross-reviewed between client owners and by the integrating agent. Findings corrected include stale session calls, duplicate Figma ownership, helper credentials inherited by companions, mail body limits applied too late, MIME transport encoding, malformed GitHub responses, and application/coding slot switching.

| Requirement | Verified evidence |
|---|---|
| Four application clients and bridge | `go test -race ./clients/appbridge ./clients/figma/... ./clients/mail/... ./clients/slack/... ./clients/github/... -count=1` passed after final prompt changes |
| Role and actual execution gates | Application role tests, AgentKind tests and actual model-request MCP guidance tests passed |
| Existing core/Office integration | Full tests for `internal/app`, `cmd/magi`, Office helper, daemon, fleet and architecture passed |
| Browser clients | Figma, mail and GitHub Playwright passed; mocks are described in their manuals |
| Existing VS Code browser | Full transcript harness: 7 test groups passed; axe checks reported zero violations |
| Console app/coding separation | GWT compile and shell tests passed; compiled parent/children Playwright passed at 1440px and 390px |
| Build and manuals | New client command builds and `go vet` passed; 14 design/manual files had no broken local links |
| Coding worktrees | EN/KO design specifies creation, ownership, recovery and removal; no worktree service was implemented |

Real Figma/Slack installation, real IMAP/SMTP delivery remain unverified. Account-based acceptance steps are in each client manual. No real mail or unrelated remote messages were sent during development. Updating an existing installation requires rebuilding the core/helpers and restarting old companions; committed source alone does not upgrade running processes.

## Chrome browser replacement (2026-10-12)

The GitHub-specific helper and extension were removed at the user’s request. Earlier verification rows above describe the implementation at that time; they do not describe currently available clients. Port 8737 now belongs to the Chrome browser helper. Its scope, permissions and acceptance procedure are in [the browser manual](../clients/browser/README.md). Browser tasks use a dedicated business kind and cannot execute coding tools.

Browser cooperation reuses `hand_off` and introduces a small on-demand `browser` entry point without changing the session-frozen tool catalog. Tests default to isolated headless mode. See [the cooperation contract](BROWSER_COLLABORATION_DESIGN.md).

## Figma management and window placement (2026-10-13)

Figma follows the Office conversation layout and embeds Material Web controls in installation HTML. Session controls and settings sit above the transcript, with the composer below it. Standing rules and skills use the document's hashed workspace and reload on the next request. Providers use shared `config.toml`; selecting a name applies its endpoint, key and model together. Editing the active profile reapplies it and distinguishes a successful save from a failed application. Council configuration is saved for the companion and applied through explicit restart and reconnect.

Session listing and selection accept only conversations where `For == App + ":" + Target`. Empty document sessions are included before their first request, so event subscriptions do not reject their IDs. Switching suspends the Figma sandbox session before transferring MCP ownership and replaying the selected transcript. Restart uses the same lifecycle lock as creation, selection and disconnection.

Left alignment computes a plugin window from viewport bounds and zoom. It does not dock into the native sidebar or reduce canvas width. The approach follows the bottom-window placement in [Parrot's public distribution](https://github.com/opral/parrot/blob/main/versions/dev/code.js), but checks every 500ms and repositions only after a change. Floating mode stops automatic placement, and closing clears the timer. Procedures and limits are in [the Figma manual](../clients/figma/README.md).

The actual helper and daemon on this Mac accepted new empty-session event polling with HTTP 200 and served settings reads. Figma Desktop with a signed-in account ran the plugin and delivered UI messages. Real interaction with the Material management UI and actual model/document-tool delivery remain acceptance work.
