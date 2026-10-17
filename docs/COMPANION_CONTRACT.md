# Companion purpose, access scope, and published operations

Status: implementation design. A live `about` query to the gwt daemon on 2026-10-17 returned two skill descriptions. It supplied no argument contract or direct invocation route. This design separates procedures an agent knows from operations the user permits others to invoke.

## 1. Declaration and authority

Users declare a purpose, operating instructions, and operations. Skills do not grant execution rights. A configured external tool server does not publish all of its tools to other companions.

The authoritative file is `companion-policies/<workspace SHA-256>/policy.toml` under the user configuration directory. Run `magi --companion-policy-path` in the workspace to locate it. The workspace identity is its cleaned absolute path after resolving symlinks. Project `.magi` files cannot replace this policy. The policy file itself must not be a symlink. Missing policy means no directly callable operations. Invalid policy fails closed.

```toml
version = 1
purpose = "Develop and validate the GWT build plugin"
instructions = ["Inspect consumer projects without modifying them.", "Deploy only when the user requests it."]

[[operations]]
id = "read-plugin-source"
description = "Read published plugin source files."
kind = "read_file"
root = "/Users/me/projects/gwt/src"
```

The first executor supports `read_file`. Callers supply a relative `path`, with optional `offset` and `limit`, and cannot change the configured root. Absolute paths, traversal, escaping symlinks, directories, and special files are refused. Go `os.Root` prevents symlink replacement between a path check and open from escaping the root. Publish directories containing files suitable for sharing: this executor does not identify secrets inside an allowed root.

Purpose and instructions appear in the companion's own system context and its public introduction. Instructions are guidance, not an OS sandbox. Initial enforcement covers the published read executor; it does not restrict every existing bash call or `hand_off` operation. Companions run as the same user, so this policy is not an OS security boundary against malicious local code.

To create a policy, make the parent directory printed by the command and save the TOML file there. This change does not automatically publish the current gwt workspace. Discovery and invocation reload policy. Own instructions take effect at the next context assembly. The daemon refreshes purpose and advertised work every three seconds. Peer roster caches follow their own refresh interval.

Authorization follows the current companion connection boundary: the local user socket and admitted SSH/TLS fleet connections. There is no caller-specific ACL initially. Every caller admitted through that boundary can invoke a published read operation. Leave an operation unpublished if it needs per-peer restrictions.

## 2. Modules and runtime path

```text
User policy → purpose and instructions → own system context
            → operation summaries → peer description → scoped executor → original result
```

Core defines contracts. An adapter loads policy and accesses files. Cmd wires the daemon and agent tools. The request path is caller model → `companion_call` → target daemon → policy loader → restricted executor. No receiving model interprets the arguments. Results and errors remain structured.

1. `companion_can` separates purpose, instructions, and published operations from learned procedures.
2. `companion_call` with `list` returns summaries. `describe` fetches one operation's complete input/output schemas and access scope.
3. `call` reloads current policy and executes only an explicitly published operation. Invalid arguments, removed operations, and scope violations fail before execution.
4. Read calls can run beside the target model's turn. Each owns a root handle. Files are bounded to 1 MiB; responses default to 200 lines and allow at most 1000. Binary files are refused. A response over 64 KiB fails with a request to select a smaller range; content is never silently clipped.
5. Daemon methods `capabilities`, `capability`, and `capability-call` are explicitly allowed through the SSH fleet door. Old daemons report an unsupported method; calls never fall back to model delegation.

Full schemas stay out of the ordinary roster. Each query or invocation uses one policy snapshot. Invocation applies current policy if it changed after description. Future mutating executors must also check the revision of the described contract. Calls do not retry automatically. A lost response is reported as a failure rather than replaced by another agent's account.

## 3. Decisions and quality attributes

| Decision | Reason | Cost and limitation |
|---|---|---|
| Reuse daemon as broker | Existing companion addressing and SSH boundary | Target must be running |
| Store authority in user directory | Repository edits cannot expand published operations | A policy editor remains to be built |
| Fetch schemas on demand | Avoid rebilling every operation schema each turn | Extra round trip on first use |
| Begin with scoped file reads | Enforce a filesystem boundary without exposing a shell | Builds and browser actions need further executors |
| Separate procedures and operations | Learned knowledge must not become a permission grant | Published operations use a `call:` prefix; counts are a startup snapshot |

## 4. Implementation sequence

Next, add a bounded command executor with user-fixed executable, argument templates, and allowed cwd. Never interpolate a caller string into a shell. Implement job identity, status, cancellation, timeout, result retention, write serialization, and audit records before publishing builds. Define approval, network, child-process, and write boundaries first. Validate the gwt Gradle operation in a disposable consumer workspace at that stage.

Then add selective proxies for browser and Office tools. MCP readOnlyHint alone is insufficient. Each provider must enforce the selected session, document, or tab. A supplied server URL or arbitrary tool name must not bypass publication policy.

Settings UI and TUI editors share the same policy model. Extending access policy to all tools of the companion itself requires integration with file guards and process sandboxing. Natural-language restrictions alone do not complete that work.

## 5. Acceptance criteria

Test missing policy, malformed TOML, unknown version, duplicate IDs, and unsupported kinds. Confirm purpose and instructions reach both the own prompt and peer introduction. Direct calls must execute without a model and preserve content/isError. Cover normal relative reads, paging, outside paths, escaping and replaced symlinks, binary/special/oversized files, cancelled requests, and policy revocation. Exercise real daemon sockets and the fleet-door allowlist.

## 6. Initial implementation scope

The initial implementation includes policy loading, own instructions, public introductions, `companion_call` discovery and reads, and daemon/SSH routing. A settings editor, command and mutation executors, per-peer ACLs, and enforcement across all ordinary agent tools remain unimplemented. The actual gwt workspace has not been granted publication rights. Tests use temporary workspaces.


## 7. User-declared advertising

Save a declaration with `magi --companion-policy-write < declaration.toml`; inspect it using `magi --companion-policy-show`. Writes validate before atomic replacement and preserve an existing declaration on invalid input. Only an explicit local CLI action exposes writing, never a model tool or remote fleet method.

Users declare purpose, responsibilities, instructions, offered tasks, and access scope. Tasks are model-mediated work offers. Operations invoke an executor without a receiving model.

```toml
version = 1
purpose = "Develop and validate the GWT plugin"
responsibilities = ["Maintain plugin source and tests."]
instructions = ["Do not publish without a request."]

[[tasks]]
id = "diagnose-build"
description = "Diagnose and fix GWT build failures."
constraints = ["Report verification before and after changes."]

[[access]]
resource = "/Users/me/projects/gwt"
mode = "write"
description = "Maintain this project's source and tests"
```

`can` and `does` count only declared tasks and operations, using `task:` and `call:` prefixes. Learning a skill or attaching an MCP server never adds an advertised offer. Detail queries show purpose, responsibilities, tasks and constraints, and access scope first. Skills appear separately as experience. Deleting policy clears offers; invalid policy clears stale offers and reports an error state. Existing `companion.role` remains a compatibility introduction, not a permission grant or task list.

Access modes are read, write, execute, deny, and approval. They communicate boundaries to the own model and peers but do not constrain the OS permissions of ordinary shell tools. Direct read authority comes only from operations.root. Enforcement across all ordinary tools remains follow-up work. Coding companions may fill missing descriptive metadata after understanding their workspace. Access is never granted automatically.

## Self-description after understanding a workspace

Interactive coding companions (VS Code, JetBrains and TUI) automatically extract missing purpose, responsibilities and offered tasks after a successful turn that contains a successful project-file `read`. The foreground model no longer has to remember to invoke the publication CLI. Prepared coding-task runners do not publish separate identities.

When both offered tasks and callable operations are empty, the first genuine user turn after daemon startup receives a one-time model nudge to ask permission for a project survey. The model uses `ask_user`, explains the read-only scope and model usage, and offers approval or deferral. Dismissal is not approval. This does not create a startup model request or interrupt urgent work. The nudge stays stable through the approval turn and is not repeated in another session of the same daemon. It is guidance to the model, not a new filesystem permission boundary. Existing user-requested work and passive metadata extraction remain separate from this optional survey.

Headless runs do not receive the nudge or perform automatic profile extraction. Cron jobs, child sessions, prepared coding tasks and peer-originated turns do not receive the introduction. IDE daemons serving interactive clients are interactive for this purpose, even though their process has no TUI.

`companion_auto_describe = "on"` is the default. Set it to `"off"` to stop extraction; changes apply without restarting the daemon. This switch is separate from memory and skill learning. An untrusted project configuration cannot change it. Turning it off preserves descriptions already stored.

The extractor waits for foreground turns to finish, makes one tool-free model request with at most 24 KiB of observed transcript data, and times out after 90 seconds. A new foreground request cancels it. There is one extractor per daemon, with a ten-minute cooldown between attempts; cancellation allows another attempt after the next successful turn. If the evidence is insufficient, the model may return `null`. Failures are logged and do not change the declaration or fail the user's turn. The extra request uses the session's provider route and model and incurs model usage.

Generated metadata goes through the existing validated profile writer. Only empty sections are filled; existing descriptions, access, instructions and direct operations remain intact. A successful save refreshes the companion advertisement. Learned skills do not become advertised tasks automatically.

In JetBrains settings, **Automatic workspace description** is a checkbox. **Workspace purpose and offered tasks** provides purpose and responsibility fields and a task table with add/remove controls. In VS Code, open **Magi: Change one of magi's own settings**, select the automatic-description switch or the workspace description, edit individual entries and choose **Save**. The metadata editor can replace or remove existing descriptions; it preserves access permissions and callable operations. Disabling automatic extraction is useful when deliberately leaving sections empty.

The daemon exposes these through `config-get` and `config-set`: `companion_auto_describe` accepts `on` or `off`; `companion.profile` accepts a JSON object containing only `purpose`, `responsibilities` and `tasks`. Profile edits use the same cross-process lock and atomic validation as the CLI. Invalid edits leave the stored policy unchanged.

For terminal inspection or manual submission, use `magi --companion-policy-show` and send profile JSON to `magi --companion-profile-write` on stdin from the original workspace. That CLI fills empty sections only. The full-policy CLI remains available for intentional authority changes. Automatic extraction persists a description; it does not guarantee that the model understood the workspace correctly, so review the offered tasks before relying on them.
