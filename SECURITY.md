# Security

[한국어](SECURITY.ko.md) · [↑ Project README](README.md)

magi runs a language model that reads your files, writes to them, and executes commands. That is
what it is for, and it is also the whole of the risk. This page is the one place that says what
stands between the model and your machine, who each boundary is meant to stop, and — the part
worth reading twice — what is deliberately **not** defended.

Reporting something: for anything already public, open a GitHub issue. For anything that is not, use
GitHub's private vulnerability reporting on the repository — and if that is unavailable, open an
issue saying only that you have something to report, and it will be moved somewhere private. There
is no bounty and no SLA; this is one person's project.

## The short version

| | |
|---|---|
| Every tool call is gated | approval mode × OS sandbox, plus deny rules that win over everything |
| Secrets are denied by default | `.env`, `*.pem`, `.ssh/**`, `.aws/credentials` and friends, for file tools and in bash commands |
| A cloned repo may tighten, never loosen | `.magi/config.toml` can ask for **more** care than your machine's default, and is refused out loud when it asks for less |
| Plugins from a clone do not run | until you say `magi --trust` here |
| The console has authorization, not authentication | capabilities per person; **who** they are comes from a gateway you put in front |
| Nothing listens on a port | the daemon is a unix socket; machines reach each other over ssh |
| Tool output is data, never instruction | including web pages the agent fetched |

## 1. The tool gate

Every call the model makes goes through one path, and each gate can only refuse. The order matters:

```
deny rules → allow rules → approval mode → guardrail scan → run
```

- **Deny rules are a hard floor.** They are checked first and win over `allow`, over an allow rule
  you wrote, and over `--permission allow`. Rules are globs over tool invocations —
  `Read(**/.env)`, `Bash(git push:*)`, `WebFetch(domain:example.com)`.
- **Approval mode** is `ask` (confirm everything) · `auto` (edits pass, commands and network ask) ·
  `allow` (nothing prompts on its own) · `deny` (guarded tools refused).
- **The OS sandbox** is a second, independent axis: `read-only`, `workspace-write` (seatbelt on
  macOS, bwrap on Linux), or `full`. `--profile safe|standard|yolo` sets both at once.
- **The guardrail scan runs even under `allow`.** A destructive command (`rm -rf`), a pipe-to-shell,
  network egress, or a path that looks like a credential forces a prompt on top of whatever the mode
  said. It exists because of the policy, not because somebody asked to be asked.

**Secret-looking paths ship denied.** `**/.env`, `**/.env.*`, `**/*.pem`, `**/*.key`, `**/id_rsa`,
`**/id_ed25519`, `**/.ssh/**`, `**/.aws/credentials`, `**/.aws/config`, `**/.netrc`, `**/.npmrc`,
`**/.pypirc`, `**/secrets/**`, `**/*.secret`, `**/credentials.json`. Denied for the file tools and
flagged in bash commands, so a prompt-injected agent cannot quietly read or exfiltrate them.

**The files that decide what the agent may do are denied for writing and allowed for reading.**
Knowing your own posture is useful and harmless; rewriting it is the entire problem. The project
config lives inside the workspace, which is inside the tool jail, so `write` could reach it — and
in `auto` mode an edit is approved without anybody seeing it. An agent that could edit that file
could grant itself hooks, tool servers and an allow list.

**Persisting an approval narrows it.** Choosing `p` at a prompt writes the narrowest rule the tool
allows: approving `curl https://x` records `bash(curl:*)`, never `bash(**)`. A command that opens
with a pipe or a redirect has no stable program to pin a rule to, so it stays session-only rather
than over-granting.

**Egress can be pinned to a host allowlist** (`allow_domains`). Be precise about what that buys: a
`webfetch` to an off-list host and a bash command carrying a **literal** URL naming one are hard
denied. Any other egress shape — a bare hostname, a variable, a host read from a file — cannot be
resolved by a string scan, so it **forces a confirmation prompt instead**. Under headless
`--permission allow` that prompt resolves to allow, because that posture means full trust.

## 2. The workspace is a trust boundary

A cloned repository brings files that were written by somebody else. Two of them would otherwise
decide what magi may do on your machine.

**`.magi/config.toml` may tighten and may not loosen.** The file is meant to be committed — that is
the point of it, the workflow travels with the repo. So the posture keys are clamped in one
direction: a repo asking for `read-only` and `ask` gets it, because that is a request. A repo
asking for `permission = "allow"`, `sandbox = "full"`, `profile = "yolo"` or extra `allow` entries
is **refused out loud**, because silently ignoring it would leave somebody believing their
committed posture was in force.

Keys with no tighter direction — hooks (which run `/bin/sh`), MCP servers (a command the daemon
spawns), `base_url` (where your prompts go) — are not merged from an untrusted workspace at all.
They are named on the way past instead.

**`.magi/plugins/` does not load until you trust the workspace.** A plugin declares its own
permissions in its own manifest and they are granted, which is a fair arrangement for a directory
you installed into and none at all for one that arrived with a clone. Untrusted, magi names what it
found and skipped. `magi --trust` here, or `-plugins .magi/plugins` for one run.

**`auth.toml` is never merged from a workspace.** A permission model a cloned repository can edit is
not a permission model.

## 3. Plugins

Lua, in a sandbox with the dangerous stdlib removed, and permissions declared in `plugin.toml`:
`fs:read`, `fs:write` (workdir-confined), `net`, `exec`. Gated capabilities — `exec`, `open_url`,
`http`, `spawn`, `analyze` — are refused without their grant.

Two limits worth knowing. A plugin's `set_config_key` has a **fixed deny-list it cannot touch even
with a grant**: `mcp`, `hooks`, `allow`, `deny` — the four that decide what runs. And a subagent's
declared shape is **checked, not trusted**: a tool that says `readonly_children` has every spawn
inspected at the moment the child's tools are decided, and a spec asking for anything outside
`read`/`grep`/`glob`/`list` is refused by name. An absent or empty tool list is refused too, because
it does not mean "nothing", it means everything this companion has.

## 4. The console

`magi-web` binds loopback and has **no login of its own**. Reach it the way your organisation
already allows: an `ssh -L` tunnel, or your own proxy with your own SSO in front of it. Building
accounts into it would be a second door beside the company's, and the second door is always the
weaker one.

What it *does* have is **authorization**. `auth.toml` names roles and the capabilities each buys:

| Capability | What it is |
|---|---|
| `read` | everything that only looks |
| `answer` | unblocking a companion and stopping a turn — deliberately apart from `prompt` |
| `prompt` | giving the agent work: submitting, steering, dispatching |
| `curate` | what the agent learns from — the experience store |
| `configure` | how it runs: model, approval mode, schedule, tool servers |
| `admin` | people, roles, and admitting companions — the one that grants the others |
| `shell` | running a command outside the permission policy. Never granted on `-exposed` |

Three properties of that gate, each there for a reason:

- **A route missing from the table is refused**, with a sentence saying the table is what is
  missing — and a test walks the handler list and fails on any path in neither set. The
  alternative, a check inside each handler, is a list somebody maintains and eventually forgets.
- **Hiding is not the check.** The page leaves out controls you may not use, because a composer for
  somebody who may not prompt is a box that swallows what they typed. The server refuses regardless.
- **Every route is same-site only**, so a page on another origin cannot POST to it.

**Identity comes from a gateway and is not verified.** `-user-header X-Forwarded-User` names the
header your proxy sets. magi reads it, records it, and does not check it — anything that can reach
the console can set that header. Put something in front that strips it from client requests.

**`-exposed`, and a console that has an `auth.toml`, both turn off two routes**: `/shell`, and
writing an MCP server's command line. Those are the two that make the machine run something the
*caller* chose, outside the permission policy tool calls go through. Everything else stays, because
it reaches the machine through the agent — which is what the console is for.

**A policy with no admin cannot be saved.** A console whose file locks its own author out would
refuse to start, so `magi --access`, `--grant` and `--revoke` are the way back in.

**There is an audit log.** Method, path, companion, origin and status for every route, refusals
included, in `console-audit.jsonl`.

## 5. Across machines

**Nothing listens on a port.** A daemon is a unix socket with a record beside it, and that directory
is the membership list. Machines reach each other over **ssh**, carrying the daemon's own protocol
as bytes.

Two doors, and the difference is whose key it is:

- **`--relay`** carries the whole protocol and any socket path. Right for your own machines.
- **`--fleet-door`** is the narrow crossing for a key that is somebody else's: four methods, and
  only companions this account published. Meant as an ssh forced command.

Machines are admitted explicitly — `magi --whoami` prints a fingerprint, `--admit` accepts one —
and a fleet identity (`fleet-key.pem`, `fleet-cert.pem`) lives beside the config. A companion not
seen for an hour is forgotten, so a stale record ages out instead of becoming a registry entry
somebody has to clean up.

**What replicates between machines is bounded.** Only the team experience tier, only two file shapes
(a wiki revision and a memory), and every incoming file is checked against everything its name
claims — the content hash must match the name — before it is written. Per-reply payload and
want-list caps bound the rest.

## 6. Prompt injection

Tool output is **data, never instruction**. Web content the agent fetched is fenced before it enters
the context. The deny rules above are the teeth: an agent talked into reading `.env` is refused by
the same floor that refuses it any other time, because the floor does not consult the model.

The council reads **magi's own record** of what ran, not the agent's account of it — so text that
persuades the agent has nothing to persuade at the gate. What it can still do is make the agent
spend a turn doing something useless, and that is not a defence magi claims.

## 7. What is deliberately not defended

Stated plainly, because a security page that only lists strengths is worse than none.

- **magi does not authenticate anybody.** Authorization exists; identity is your gateway's job. A
  console reached without one is a console where everyone is the operator.
- **A trusted workspace's plugins run with the permissions they declare.** Trust is the whole check.
  Read a plugin before trusting the repo it came in.
- **`--permission allow` means it.** It is the daemon's default, because work that runs while nobody
  is watching must not stop for a person who is not there. The guardrail scan still fires, but a
  posture you chose is a posture you get.
- **The model's judgment is not a security control.** Everything above is deterministic and sits
  outside the model on purpose. Nothing here assumes the model is aligned, careful, or not being
  manipulated.
- **A companion's work cannot be rolled back from another machine.** Every mutation has exactly one
  owner and one log; there is no fleet-wide undo.
- **No supply-chain verification of the models you point it at.** `base_url` goes where you say.
- **What a command PRINTS is never scanned for secrets.** The secret defences are path-based: the
  deny rules attach to file tools and match the path being opened, so `.env` and `id_rsa` cannot be
  read by name. Nothing looks at output. `env`, `printenv`, a build log that echoes a token, a
  `docker inspect` — whatever those print goes into the model's context and into the session log,
  which is kept. Redacting output was considered and refused for now: a regex that removes
  secret-shaped strings both invites trust it cannot keep and silently corrupts what it misfires on
  (a truncated hash, a gutted payload) with no way for the model to tell. Keep secrets out of the
  environment the daemon runs in.
- **Third-party MCP servers are processes you asked magi to spawn.** They run with your privileges,
  outside the Lua sandbox. Their tools go through the same permission gate as any other tool, which
  bounds what they can do *through the agent*, not what the process itself can do.

## 8. If you are deploying this for more than one person

1. Put an authenticating proxy in front of `magi-web` and set `-user-header` to the header it sets.
   Strip that header from client requests at the proxy.
2. Write an `auth.toml`. Start people at `read` + `answer`; `configure` and `admin` are separate for
   a reason.
3. Run the daemons under `--profile standard` (or `safe` where the work allows it) rather than
   inheriting whatever the last person typed.
4. Set `allow_domains` if the work does not need the open internet.
5. Trust workspaces one at a time, and read a repo's `.magi/` before you do.
6. Keep `console-audit.jsonl` somewhere it is read.
