# Opening the console to other people

Status: **the three "either way" items are built** (see below); the decision the rest turns on is
still open. Written while the operator was away, so it states the decision first, builds only what
is right under either answer, and stops there.

## What is being asked

Reach the console from outside this machine, with more than one person using it — which is why
authentication came up, and why the icon kit gained `user-lock`, `key`, `right-to-bracket`,
`user-shield`, `github` and `google` before any of this was designed.

## What is true today

- `magi-web` **refuses to bind to anything but loopback**, and exits rather than warns. That
  refusal is the only thing standing between the console and the network.
- It has **no authentication of its own**, by design (`docs/UI.md` §4.6). The intended path outward
  is `ssh -L`, which brings its own keys, encryption and audit trail.
- The console is a **complete control surface for the workspace**: submit a prompt, run a shell
  command (`/shell`), change the approval mode, change the model, write a skill file, write a cron
  job, rewind a session. A login page does not make this a web app — it makes it a web app that
  executes arbitrary commands as whoever runs the daemon.
- Cross-site protection today is `sameSiteOnly`: Sec-Fetch-Site plus Origin, applied by wrapping
  the mux so a route added later is covered. It defends the browser of somebody who is already the
  operator. It is not an authorization mechanism and does not become one.
- The session logs hold **the code and the whole conversation**. What a leak costs here is not an
  account; it is the work.

## The decision this turns on

**Who authenticates: something in front, or magi itself?**

### A. An authenticating proxy in front (recommended)

`oauth2-proxy`, Cloudflare Access, Tailscale, or the organisation's existing SSO. magi stays exactly
as it is — loopback, no auth — and the proxy terminates TLS, authenticates, and forwards.

- Nothing new to get wrong in this repository. The parts that are easy to get subtly wrong (session
  cookies, CSRF tokens, token refresh, replay, rate limiting) are somebody else's tested code.
- Works today with no release.
- What it cannot do: **per-person authorization inside magi** — "this person may see these
  companions", "this person may answer but not run a shell". A proxy knows who; it does not know
  what a companion is.

### B. Authentication in magi

Only worth it for what A cannot do. If it is chosen, it is not one feature — it is all of these,
and any one missing makes the rest theatre:

1. **TLS**, or a documented requirement that a TLS terminator is in front. A token over plaintext is
   a token on the wire.
2. **Authorization, not just authentication.** An allow-list of accounts; an OAuth provider is a
   directory of every person on earth. Wiring "log in with GitHub" without an allow-list is the
   single most common way this goes wrong.
3. **Sessions**: httpOnly + Secure + SameSite cookies, an expiry, a way to revoke, and a CSRF token
   on state-changing requests — `sameSiteOnly` is not enough once the attacker can be a
   *legitimate* user of another origin.
4. **An audit trail**: who submitted what, to which companion, from where. There is one person
   today, so nothing records this.
5. **Rate limiting and lockout** on the login path.
6. **A decision about `/shell` and `/dispatch`.** In an externally reachable mode I would refuse
   `/shell` outright: it is a remote shell with a friendly face.

## Needed either way — built

None of these depends on the choice and none weakens the current posture.

- **An audit record of state-changing requests** (`cmd/magi-web/audit.go`). One JSON line per
  non-GET request appended to `console-audit.jsonl` beside the session store: time, method, path,
  the companion it named, origin, and status. Wrapped OUTSIDE the cross-site guard, so a refusal —
  the line somebody would actually want — is recorded even though no handler runs.

  Two things changed from the sketch above. It is a **file, not an event in the session log**: the
  record has to hold requests the daemon never saw (refused at the guard, or a socket that could
  not be dialled), it must be one place regardless of which companion was named, and magi-web is a
  reader — the daemon owns the log. And it records **that** a prompt was submitted, never what it
  said: the log already holds every word, and a second copy would be the whole conversation again
  in a file with different permissions and no compaction.

  **Who** needed a source. This process has no users and cannot tell a proxied request from any
  other loopback connection, so identity comes from the gateway through a header the operator names
  with `-user-header` — unverified, and documented as unverified. Unset, the record says where and
  not who.
- **An `exposed` mode flag.** `-exposed` refuses `/shell`, refuses `-peer` at startup, and — added
  on the same reasoning — refuses **MCP writes**: a server entry is a command line a daemon spawns
  at startup, which is the shell route wearing a settings form, and this repository's own
  same-site guard exists because a page on another origin once wrote one. Reading the MCP list
  stays. `/dispatch` and `/cron` stay: they reach the machine through the agent and its permission
  policy, and refusing them leaves a console that cannot be used for the thing it is for.
- **A test that the bind guard cannot be bypassed** (`cmd/magi-web/bind_test.go`). Binding moved
  into `listenLoopback` so one place opens a port and it is the place that checks; the test pins
  the refusal, that the socket is released rather than held, and that the message still tells the
  operator how to reach the console from elsewhere. A second test counts the port-openers in the
  binary, because the first is worth exactly that count.

## What is left, in order

1. **The operator picks A or B.** If B, they say what A could not do for them — that answer decides
   the scope of the rest.
2. If **A**: nothing more in this repository. Put the proxy in front, start the console with
   `-exposed -user-header <what your gateway sets>`, and the record names people from day one.
3. If **B**: design the session and authorization model as its own proposal, with the allow-list
   first and the login page last. The audit record and `-exposed` are already there to build on.

## What I will not do without being told twice

Ship a login page in front of a control surface that runs shell commands, without TLS, an
allow-list, CSRF tokens and an audit trail. Half of that is worse than the ssh tunnel it replaces,
because it *looks* like security.
