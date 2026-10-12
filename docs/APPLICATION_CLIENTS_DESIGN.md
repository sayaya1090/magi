# Application client design and implementation contracts

[한국어](APPLICATION_CLIENTS_DESIGN.ko.md) · [Client overview](CLIENTS.md)

Figma, standard IMAP/SMTP mail, Slack and GitHub become new Magi clients. This document defines their shared implementation contracts and acceptance requirements. Each application manual explains installation, account configuration and everyday use. Mock tests and real account acceptance are reported separately.

## 1. Roles and surfaces

| Client | User surface | Agent work | Excluded defaults |
|---|---|---|---|
| Figma | Development plugin panel | Inspect selection, edit text, create frames | Shell, source edits, whole-file scans, publishing |
| Mail | Local browser mail UI | Read selected correspondence, identify requests, draft replies | Model-initiated sending, OAuth, attachments |
| Slack | App Home, DMs, mention threads | Summarize conversations, coordinate work, draft messages | Shell, source edits, unsolicited messages in other channels |
| GitHub | Chrome/Chromium side panel on github.com | Repository, issue and pull-request coding work | Unrequested comments, pushes and merges |
| Existing Office | PowerPoint, Word and Excel task panes | Presentation, document and spreadsheet work | Default coding identity, external shell and source editing |

`--client-role` and `MAGI_CLIENT_ROLE` select the role. An empty value preserves existing coding behavior; `github` also uses the coding role. Other roles receive application-specific system instructions from `internal/app/client_role.go`. The same role restriction applies when advertising tools and executing calls. Only the application's MCP namespace and common question, council, planning and recall tools are permitted. A model cannot execute a hidden `bash` call or another application's tools.

Office passes its role when starting a new companion. Installing a new executable does not change the role of an already running older companion. Migration acceptance must distinguish fresh role-aware starts from restarts of older processes.


## 1.1. Role-specific prompts

Role source text lives in `internal/app/client_role.go`; the base coding prompt is `systemPrompt` in `cmd/magi/main.go`. This table summarizes those prompts and is not another runtime configuration.

| Role | Specific guidance |
|---|---|
| Base coding (terminal, VS Code, JetBrains) | Inspect the project, follow existing conventions, plan, implement and verify |
| GitHub coding | Inspect the bound repository, issue or PR; verify repository and branch; test; require explicit instructions for comments, push and merge |
| Figma design | Preserve design intent, typography, spacing, components and accessibility; inspect selection, file and page before editing |
| Mail correspondence | Identify requests and draft in the user's tone; invent no facts, commitments or recipients; distinguish drafts from sending |
| Slack collaboration | Separate decisions from proposals, identify owners and actions, remain in the requesting user's channel and thread |
| PowerPoint presentations | Organize arguments, design and verify; use Korean outline phrasing; preserve editability; avoid unnecessary full reads |
| Word documents | Preserve facts, structure and author intent; use appropriate prose and consistent headings; read relevant sections |
| Excel analysis | Preserve formulas, types and structure; verify calculations; distinguish observations from assumptions; read relevant ranges |

The six application roles share user-language responses, truthful reporting, external content treated as data, and a prohibition on shell/source edits. GitHub retains the base coding system prompt and adds MCP guidance from `clients/github/github.go`. Figma, Slack and mail MCP guidance applies only to available tools in the bound session. `looking` is a read-only session of the same role, not another application identity; council is a separate completion-evidence gate.

MCP initialize `instructions` enters the model request's trailing context. It cannot override user instructions, application role or permission policy, and is removed when its tools detach. `internal/app/mcp_instructions_test.go` verifies actual model-request delivery and session/role isolation.

### Type declaration and console integration status

The role vocabulary is the eight `ClientKind` constants in `internal/app/client_kind.go`. Validation and prompt selection use this declaration; unknown roles cannot execute tools. Session Agent (default/looking), user-written Role text and console screen type are separate values.

`CompanionType.java` still registers only coding type `1`. The local chain `daemon.Identity` → `daemon.Info` → roster/fleet response now carries `type`. Coding and GitHub publish `1`; application roles publish their role key. Console mapping through `FleetAgent.type` and `CompanionType` remains incomplete. Remote cluster sightings also carry type as advisory UI metadata, like Version. Type is excluded from Signable so old and new peers retain the same signature bytes. It cannot authorize execution: the destination daemon enforces its client role. Missing type on older records remains possible. Applications must not use the coding child module, which exposes filesystem/shell UI; first provide an application child using shared conversation controls.

Acceptance requires matching actual daemon records and `/fleet` types for each role, preserving coding type `1`, excluding filesystem/shell controls from application screens, deterministic unknown-type handling, and desktop/narrow-screen Playwright checks. This console integration is currently incomplete.

## 2. Shared connection layer

`clients/appbridge` connects local HTTP clients to the daemon control socket. Figma calls pass through this layer into the plugin's main sandbox. Mail, Slack and GitHub APIs run in their respective helpers. Model API credentials never enter application UI code.

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
| `127.0.0.1:8737` | GitHub side panel helper |

Listeners require an explicit loopback IP. Local APIs and MCP require `Authorization: Bearer <token>`. An unspecified token is generated with 256 bits of cryptographic randomness. Cross-origin browser extension and sandbox iframe requests require the bearer credential; CORS permission is not authentication. Browser text uses `textContent` rendering.

## 3. Target and session ownership

| Application | Target identity | Additional check |
|---|---|---|
| Figma | File key or user-supplied permanent document ID | Capture page and selection IDs; recheck before mutation |
| Mail | Account / mailbox / UIDVALIDITY / UID | Refuse old UIDs after UIDVALIDITY changes |
| Slack | Team / channel / thread timestamp / requesting user | Reject another user's approval actions |
| GitHub | Owner / repository and issue or pull number | Match the local origin to the connected repository |

`POST /api/connect` accepts `{target}` and returns `{binding, session, target}`. The helper generates the opaque binding ID. Application UI cannot provide arbitrary socket paths, work directories or session IDs. Sessions use `session-new` with `keep` and a `for` tag for creation and recovery. Connecting another document does not switch the companion's foreground conversation.

Figma, mail and Slack derive a hashed workspace directory for each target. GitHub connects an explicitly configured local checkout. Document bodies and titles never become work directories. Bindings live in process memory and must be reconnected after restart; persisted sessions are recovered by their `for` tag.

## 4. Chat and tool protocol

`POST /api/request` accepts a binding and a bounded command set. `say` or `prompt` maps to `submit`, `model` to `set-model`, `reply` to question `answer`, and `permission` to a permission response. `new`, `interrupt`, `status`, `models`, `context` and `compact` are supported. On a new session, UI applies the returned session and cancels the old event stream. Administrative commands such as `shell`, `config-set` and `shutdown` are unavailable through this API.

`GET /api/events?binding=...` forwards the daemon transcript as SSE, including persisted events and transient permission/question requests. UI separates original `part.appended` output from approval and question cards. It checks event session IDs and rejects late replies from another target. Slack uses a separate replay boundary to avoid reposting historical output into a channel.

`POST /mcp/<binding>/<session>` accepts MCP `2025-06-18` initialize, ping, tools/list and tools/call. Only registered application tool names are dispatched. The binding and session in the URL fix the target. An old session URL cannot be used in a new session. Handlers for tools with explicit document or recipient arguments must compare them with the original bound target. MCP annotations identify read-only tools.

Figma receives calls from `/api/figma/poll` and returns results through `/api/figma/result`. Random call IDs are bound to one binding. Foreign, duplicate and expired results are rejected. Calls time out after 30 seconds; a poll waits up to 20 seconds. A timeout does not prove that a mutation did not occur. Check the actual design before retrying.

## 5. Accounts and external operations

IMAP/SMTP credentials, Slack app/bot tokens and GitHub API tokens stay in helper environments. `CompanionEnv` removes application tokens and mail passwords when starting a companion, retaining model-provider credentials. Existing processes need restarting because their environments cannot be changed. They are excluded from tool results and server error output. Figma invokes design APIs in its main sandbox and does not require a Figma API token.

The mail model receives read and draft tools. SMTP delivery is a separate UI action after the user reviews recipient, subject and body. Slack tools read the current thread and prepare drafts. Responses to an initiated Magi conversation stay in that thread; no tool sends to unrelated channels. Initial GitHub API tools are read-only. Remote writes are not automatically authorized merely because implementation or testing would benefit from them.

## 6. Coding tasks and worktrees

Worktrees belong in a common coding task layer. Starting a repository task creates a separate branch/worktree and connects a companion that owns that workspace. Creating a directory and beginning the model's first turn are separate actions. General coding agents, VS Code, JetBrains, terminal and GitHub clients should share this contract.

Figma, mail, Slack and Office do not expose worktree operations by default. They may delegate coding work to a separate coding companion. [Coding worktree design](CODING_WORKTREE_DESIGN.md) defines creation, recovery and cleanup. A worktree service is outside this initial client implementation's completion scope.

## 7. Implementation and verification order

1. Implement shared connection, session, authentication and role restrictions. Test with actual local sockets.
2. Connect chat, permission responses, questions and events in each application. Check target/input preservation on new sessions, model changes and connection failure.
3. Test tool success, failure, foreign targets, malformed arguments and large responses. Mail/Slack delivery tests use mock servers or transports.
4. Run Playwright for Figma, mail and GitHub browser surfaces. Also run the existing full VS Code transcript harness.
5. Follow owner self-review with another owner's code/document/test review. Fix findings and rerun affected verification.
6. Record real account acceptance separately. Without an account, do not claim authentication, app installation or actual message delivery passed.

Each manual must include real run commands, credential locations, session identity, supported tools, size limits, recovery steps, test commands and unverified scope. Update this design and both language editions when contracts change.
