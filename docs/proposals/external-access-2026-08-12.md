# Opening the console to other people

Status: **proposal, nothing built.** Written while the operator was away, so the decision it turns
on is stated first and the work stops there.

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

## Needed either way, and buildable now

These do not depend on the choice, and none of them weakens the current posture:

- **An audit record of state-changing requests** — the seven that change a run, plus the writes
  (`/skills`, `/forget`, `/remember`, `/report-format`, `/cron`, `/mcp`). Derived like everything
  else: an event in the log rather than a new file. Useful with one person too, because "who
  interrupted that turn" is already a question a second console cannot answer.
- **An `exposed` mode flag** that turns off `/shell` and refuses `-peer`, so the shape of a
  reachable console is decided before it is reachable rather than after.
- **A test that the bind guard cannot be bypassed** — there is one refusal today and no test that
  it stays a refusal.

## What I would do next, in order

1. The operator picks A or B. If B, they say what A could not do for them — that answer decides
   the scope of 2.
2. Build the three "either way" items above, with tests. They are small and they are not wasted
   under either choice.
3. Only then, if B: design the session and authorization model as its own proposal, with the
   allow-list first and the login page last.

## What I will not do without being told twice

Ship a login page in front of a control surface that runs shell commands, without TLS, an
allow-list, CSRF tokens and an audit trail. Half of that is worse than the ssh tunnel it replaces,
because it *looks* like security.
