# The boundary between machines

Status: **design, nothing built.** The people half — who may open the console and what they may do
once in — is [`auth-rbac-2026-08-12.md`](auth-rbac-2026-08-12.md). This is the other half: what one
machine may do to another, and what a person reaches across one.

Written by walking the code rather than by imagining attackers, and re-walked after each change to
the design. What that turned up is in [§7](#7-what-the-simulation-says), including the hole this
design had in its first version.

## 1. The premise, changed

The architecture has said this since the daemon landed:

> magi opens no port of its own between machines and holds no credential of its own: the security
> boundary is ssh.

It buys simplicity and it is honest about who is trusted. What it cannot express is the ordinary
failure: **one ssh key ends up on five machines**. A deploy key, a shared CI account, a laptop
restored from a backup. Under that premise all five are the operator, everywhere.

So:

> **ssh carries the bytes, narrowed by a forced command. The fingerprint says who. Admission says
> what. The OS account is still the final boundary.**

Every clause earns its place below, and the last one is not a hedge — see [§8](#8-what-this-does-not-solve).

## 2. Two doors

The protocol has one socket today and everything is on it. Reading the code for what actually
crosses a machine boundary: **four methods**, all from `internal/adapter/tool/companion/across.go`
and `cmd/magi/hand.go` — `about`, `hand`, `hand-state`, `watch`. Everything else (`submit`,
`steer`, `interrupt`, `answer`, `resume`, `rewind`, `compact`, `set-model`, `set-permission`,
`shell`, `shutdown`, `status`, `tools`, `jobs`, `models`, `reload-cron`) is the local console
surface and has never had a reason to leave the machine.

```
studio's daemon
├─ daemon-docs.sock          the console and the terminal on THIS machine.
│                            Everything. Guarded by the OS: 0600, owner only.
└─ daemon-docs.fleet.sock    other companions.
                             about · hand · hand-state · watch — and nothing else EXISTS here.
```

The split is enforced in **dispatch**, not in the pipe. Somebody with a shell can reach a socket
with `socat`; what stops them is that the method is not there to call.

### The forced command

A shell is a shell. `ssh host magi --relay <sock>` and `ssh host socat - UNIX:<sock>` are the same
capability, so a fleet key that can run commands defeats the split by itself. A fleet key is
therefore issued restricted:

```
command="magi --fleet-door",no-pty,no-agent-forwarding,no-port-forwarding,no-X11-forwarding ssh-ed25519 AAAA…
```

`magi --fleet-door` reads `SSH_ORIGINAL_COMMAND`, accepts only a path ending in `.fleet.sock`
inside its own config directory, and pipes. No shell, no chosen path.

**This is load-bearing.** Without it the design degrades to a recommendation, which is what the
first version of it was.

## 3. Who — a handshake, not a claim

A daemon makes an ed25519 keypair on first start (`id_ed25519`, 0600) and publishes the public half
and its fingerprint in its record. On the fleet door:

```
caller ──connect──▶ fleet.sock
       ◀── nonce ──                       fresh per connection
       ── sig(nonce) + pubkey ──▶         verify → this connection IS SHA256:9f2a…
                                          look the fingerprint up in the admission list
```

- **Connection-scoped, so replay is dead.** A fresh nonce per connection beats signing every
  request: simpler, and there is nothing to capture and re-send.
- **The caller's NAME stops being self-asserted.** `Hand` takes a label today
  (`"— asked by master on mini"`) and writes it into the conversation verbatim. After this, who is
  asking comes from the admitted record; the caller supplies only what the work is.
- The key never leaves its machine, and a key whose recorded host no longer matches is regenerated
  — a shared config directory would otherwise be a shared identity.

## 4. What — admission, in four states

Not RBAC. A companion does two things to me: hands me work, speaks into my ear. A role system over
two verbs is ceremony, and two of the four things admission looks like it governs are not grants to
them at all but choices of mine:

| | what it is |
|---|---|
| on my roster, the model is told it exists | mine — routing and display |
| I may delegate to it | mine |
| **it may hand me work** | **security** |
| **it may speak into my ear** | **security** |

```
heard → pending    the operator sees it. Nothing else does — not the roster the model reads,
                   not a delegation target, not an ear.
      → watched    on the roster, delegable by me. May NOT hand me work or speak into my ear.
      → admitted   all four methods.
      → refused    remembered, so the same fingerprint does not queue again every gossip round.
```

`watched` has a concrete case: a CI box admitted in order to be watched, which must not queue work
into a laptop. Admission is three buttons — `[admit] [watch] [refuse]` — and no second config file.

An unverified or unadmitted caller may call `about` and nothing else, and its arrival is one line in
the queue, rate-limited per peer uid and capped.

## 5. The master account

Approving the same companion on five machines is the friction that makes people automate approval
badly. The answer is **not** a master companion: its private key would sit inside a running agent,
and "approve me" is a sentence a model can be made to read. The answer is a key a person holds.

```
magi admin-key new
  → ~/.config/magi/admin_ed25519          private. No daemon ever sees it.
  → fingerprint SHA256:c41b…

each machine, once, at setup:
  [cluster]
  admin_keys = ["ssh-ed25519 AAAA…"]      ← the anchor
```

An admission becomes a signed statement that gossip carries:

```json
{"fp":"SHA256:9f2a…","state":"admitted","at":"2026-08-12T…","by":"SHA256:c41b…"} + signature
```

Five rules, and the first one must never bend:

1. **The anchor arrives as configuration and is never learned from the network.** Gossip that could
   carry an anchor is gossip that can install one.
2. **The newest statement wins, by the time inside it.** A replayed older `admitted` after a
   `refused` changes nothing.
3. **`admin_keys` is a list**, so rotation is add-then-remove.
4. **No anchor configured is the default**, and then it is per-machine approval — which is the right
   shape for three machines and does not need a key at all.
5. **The local veto stays.** A machine may refuse what the anchor admitted. What runs here is this
   machine's responsibility.

This reverses one earlier decision: `pending` IS propagated, because gathering the queue is the
point. What is not propagated is a *decision* — that travels as a signature.

### Where the private key lives

A choice with real consequences, so it is a flag rather than an assumption:

```
magi-web -exposed …                       default: no key. The console shows the queue; the
                                          person signs on their own machine (`magi admit <fp>`,
                                          ssh-agent, hardware keys) and the console distributes it.
magi-web -exposed -admin-key key.pem      convenience: the console is the cluster's CA.
```

Default is the first. The console is the host being opened to several people; putting a key that
admits anybody into the fleet on it makes "one console admin is compromised" and "the fleet is
compromised" the same event.

## 6. Reading, and where a person crosses

The rule this settles, which was implicit and is now written down:

> **Writing is the daemon's. Reading on the same machine is the file's. Reading across a machine is
> the console's HTTP — because the console is the only layer that knows PEOPLE. Companions get the
> fleet door's four methods.**

Why the log is not simply served by the daemon, which is the obvious alternative:

- The shared append-only file is what lets a console **start late, be killed, and start again**
  while the daemon never notices, and what lets `/history`, `/search` and the board answer for
  companions that are **no longer running**. A daemon-served log loses both: no daemon, no history.
- The daemon **does not know people**. It knows a uid and (after §3) a companion's fingerprint. A
  transcript is the most sensitive read in this system — it holds the code and the whole
  conversation — so "may this person read it" needs an identity the daemon does not have. Serving
  the log from the daemon would move the bytes and leave the authorization unanswered.

The file is already safe for a second reader: `jsonl` writes compactions to a temporary file and
renames, and the store has tests for exactly this shape (`second_reader_test.go`,
`rewrite_window_test.go`).

### Remote attach

`magi --attach` dials a LOCAL socket and reads the transcript from the LOCAL store, so pointing the
socket at another machine draws an empty screen. Three ways out, in the order they should happen:

1. **The person crosses** — and this is NOT a feature to build. Somebody with a shell can already
   type it, so a flag wrapping it adds no capability; what it would add is a second way to attach
   remotely, and worse, the impression that giving somebody a shell account is how you enable
   remote attach. That is A1, invited. It belongs in the manual as a sentence:

   ```sh
   ssh -t mini magi --attach -workdir /w/docs
   ```

   Zero lines of code, and an operator still has a way in on a machine with no console.
2. **The TUI becomes a client of the console.** `magi --attach https://console…` over `/events`,
   `/submit`, `/answer`. This inherits OIDC, TLS, roles and the audit record without inventing any
   of them, and it is the only arrangement that gives somebody a terminal WITHOUT giving them a
   shell on the machine — which is precisely the person the console's roles exist for.

   Two pieces are missing and both are small.

   **A raw log endpoint.** `/log?since=` returning `[]event.Event`. The TUI renders
   `session.Message` with its own renderer; feeding it the web's already-rendered rows would make
   the two windows disagree about the same conversation, which this tree has been caught by more
   than once (the council read differently in each).

   **A token for a client that is not a browser.** The standard answer is the OAuth **device
   authorization grant** (RFC 8628), which exists for exactly this shape:

   ```
   $ magi --attach https://console.example
     open https://console.example/device and enter:  WDJB-MJHT
     (waiting…)
   ```

   The person authorises in a browser they already trust, the terminal polls for the token, and
   magi still never sees a password — the same property the console's own login has. The token is
   the person's identity on disk, so it carries an expiry and appears in the people screen with a
   way to revoke it; a personal access token issued by the console is the same thing with a worse
   enrolment and the same revocation requirement.

   Everything else it needs is already there: `-exposed` requires TLS, the route table already
   answers 403 to a viewer's `/submit`, and every call is a line in the audit record under the name
   the gateway gave.

   **And it forces one more question, because it is the only remote path.** A console reads the
   local store and dials local sockets, so attaching to mini's companion means mini runs a console.
   Either every machine runs one — a TLS and OIDC surface per box — or one console relays to
   another, which is `-peer`, which this design refuses because a shared console would act with the
   operator's authority on the far machine.

   The way out is to make `-peer` carry the PERSON rather than the operator: forward the caller's
   own token, have the far console validate it against the same issuer, and let its audit record
   name them rather than "the console over there". Then the refusal in the RBAC proposal becomes
   conditional — federation is allowed exactly when identity crosses with the request. That is a
   design of its own and is what step 10 has to answer before ② is finished.
3. ~~Add the log to the daemon protocol.~~ **No.** It reopens the attach door across machines,
   duplicates what the console does, and leaves authorization where it cannot be answered.

## 7. What the simulation says

Attacker positions, against the design above.

| Position | What they can do |
|---|---|
| Network only, no ssh | Nothing. There is no port. |
| A fleet key (forced command) | The four methods, and only `about` unless admitted. Work they hand still passes the permission policy and never runs in `allow`. |
| An admitted companion, compromised | Hand work — which prompts, with the origin named. Exfiltration needs a human to approve it. ⚠ Residual: a human who approves out of habit. |
| A console role, no shell | Exactly their role. With `answer` split from `approve`, a responder cannot approve a tool call. |
| A shell on the box, same account | Everything, including reading the daemon's private key. `--locked` removes set-permission/shell/rewind from the protocol so the *shared* state cannot be changed, and every socket request is recorded with its peer uid and pid. The rest is the OS boundary. |
| A cloned workspace | Cannot grant a role: `auth.toml` is read from the global directory only. ⚠ But see C1 below. |
| Prompt injection through handed work | Admission decides who may put text in front of the model; it cannot decide what the text says. The side-session floor and the named origin are the whole defence. |

Two findings this raised that are not otherwise covered:

- **C1 — a project's MCP servers still run.** `-exposed` refuses MCP *writes*, but a
  `.magi/config.toml` already in a checkout names commands the daemon spawns at startup. Clone,
  start a daemon, arbitrary execution — a path admission never touches. A stdio MCP server declared
  by a PROJECT should be refused until approved for that workspace; the global config's are the
  operator's own and stay.
- **C2 — admitting is itself a privilege**, and an `admin` can admit their own fingerprint. That is
  intended; what makes it safe is that every decision is a line in the audit record and `admin` is
  a role given to few.

## 8. What this does not solve

- **A shell on the daemon's account.** Same uid is the same trust domain: the key is readable, the
  binary is replaceable, `auth.toml` is writable. Every check magi adds is a check that account can
  remove. What is left is `--locked` (the dangerous methods do not exist) and attribution (peer uid
  and pid in the record).
- **A stolen admin private key.** The cluster is that key, the same way a Kubernetes cluster is its
  CA. Hardware-backed keys and rotation are the mitigation, not cleverness here.
- **What a model does with text it was given.** Every layer above decides who may speak; none of
  them reads.

## 9. What lands, in order

Each step is useful alone, and the order is not negotiable in one place: the checks come before the
screens, because a screen in front of a check that does not exist is decoration.

1. **The side-session floor.** `Origin: "handoff:<fp>"`, never `allow`, no `always`/`persist`, the
   origin named in the prompt and the audit. Independent of everything else here, and the largest
   single reduction in blast radius.
2. **`answer` split into `answer` and `approve`** in the console's capability table.
3. **`--locked`**, and the peer uid/pid on socket requests.
4. **The fleet door**: a second listener, the four methods, `--fleet-door`, and the record naming it.
5. **The daemon key and the handshake**, with per-record signatures and a time inside gossip.
6. **pending / watched / admitted / refused**, enforced at the door.
7. **The admission screens**, web and TUI: three buttons, the whole fingerprint, `magi --whoami` for
   the out-of-band comparison.
8. **The anchor**, `magi admin-key`, signed statements, `-admin-key` on the console.
9. **C1**, project MCP servers behind a per-workspace approval.
10. **Remote attach ②** — `/log?since=`, the device flow, token expiry and revocation on the
    people screen, and the federation question above: a peer console that forwards the person
    rather than acting as the operator. ① is a line in the manual, not code.
11. **Docs** — `MANUAL`, `ARCHITECTURE`, `UI`, and both Korean mirrors, including the premise in §1,
    which contradicts what `ARCHITECTURE` says today.
