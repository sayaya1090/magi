# Design: cross-version compatibility and fleet self-update

Status: **implemented** (the compat commit series `270e94b1…88fd5614` plus three audit
waves of hardening after it), except **A4 (down-convert shim)** — deferred until a
second protocol capability exists to convert between — and the supervisor-restart
option in decision 3. This document describes the system **as built**; where the
built thing differs from the original proposal, the difference is called out inline.

magi companions each run as an independent process — a per-workspace `magi --daemon`
on a unix socket, with web/TUI attach clients relaying to it, and a fleet of named
companions that gossip, hand work to each other, and hold meetings over ssh. This
document covers two related additions:

- **A. Compatibility** — a newer and an older instance keep interoperating (relay,
  handoff, meetings, gossip) instead of silently misbehaving when versions diverge.
  Fleet-wide: mixed versions across machines must still interoperate.
- **B. Update** — an instance updates itself, scoped to **same-machine** companions:
  the console shows each local companion's build, a daemon picks up new releases on
  its own (toggle), and an operator can push an update by hand to one that lags.
  Cross-machine (remote) push is out of scope.

## Terrain — before, and what changed

- **version** — `internal/version` (`Version`/`Commit`/`Date`, ldflags). WAS surfaced
  locally only; NOW travels two ways: the `about` reply's `Version` (A1) and the
  unsigned `Member.Version` in gossip (A3).
- **relay** — `internal/adapter/daemon/daemon.go`: newline-delimited JSON,
  `Request{Method, …flat bag}` / `Response{OK, Err, …union}`. WAS un-versioned with no
  handshake; NOW `about` returns `Version`/`Proto`/`Caps`, a client caches them via
  `Hello()` and gates newer sends with `PeerSupports()`. An unknown *method* still
  returns a typed `Refused` (graceful); an unknown *field* is still silently dropped by
  `encoding/json` — which is exactly why new fields must stay behind the capability
  gate.
- **fleet** — two ssh transports: gossip (`cluster.Member`, signed via `Signable` —
  `Version` now exists and is deliberately NOT in the signed set) and work relay
  (`--relay` byte pipe; `--fleet-door` narrowed to `about, hand, hand-state` — the
  lifecycle verbs are asserted off it by test).
- **update** — `internal/update` WAS self-only with no restart. NOW: `RunCommit`
  (download + SHA256 + `Commit` with rollback), `internal/graceful.Reexec` (the
  restart), and two daemon methods — `restart` and `update` — joining `shutdown` as
  local lifecycle verbs, none of them on the fleet door. The "Restart magi to use the
  new version" print survives only on the interactive startup install. Still **no
  authorship signature** on releases (decision 2).
- **config** — `LoadWithUnknown` loads unknown keys (warns) and defaults missing ones.
  The model the wire's negotiation now follows.

### Where it broke, and what closed it
1. ~~No version handshake on any inter-instance path~~ — closed by A1 (`270e94b1`).
2. Silent field-drop (no `DisallowUnknownFields`) — still true of the wire itself;
   defeated operationally by A2's gate: a newer sender never ships what the peer did
   not advertise.
3. A change to the signed gossip set hard-drops old peers until re-gossip — still
   true, which is why `Member.Version` is unsigned (pinned by test).
4. ~~Version never travels~~ — closed by A1 + A3 (`576ffc20`).
5. ~~No update primitive, no graceful reload~~ — closed for the same-machine scope by
   B1 + B2 (`5e3d8887`, `9df2b734`).

## Principles

- **Additive-only wire** — never remove, rename, or repurpose a `Request`/`Response`/
  `Member`/`Info` field; new fields are optional with safe zero-values. Held by golden
  tests over the json field sets (`wire_invariant_test.go`) — `Info` is in the set too,
  because the published record is a cross-BUILD contract on one machine.
- **Negotiate, then gate** — `about` exchanges version + capabilities; a new
  method/field is sent only to a peer that advertised it.
- **Down-convert at the boundary** when the peer is older (A4 — deferred; the only
  capability so far is `handshake` itself, so there is nothing to convert yet).
- **Inherit config's lenient-decode philosophy.**

## Part A — compatibility, as built

- **A1. Version + capabilities on `about`.** The `about` response carries
  `Version` (the binary's, via the optional `Versioner` engine capability),
  `Proto` (`ProtoVersion`) and `Caps` (`Caps()`, currently `["handshake"]`). All
  additive and omitempty; a pre-handshake daemon sets none, which a caller reads as
  proto 0 / no caps — "hold to old behaviour".
- **A2. Capability gating.** `Client.Hello()` runs the handshake and caches
  `PeerInfo`; `PeerSupports(cap)` gates a newer send. Foundation-only today: no caller
  gates on it yet, and the first real consumer arrives with the second capability.
- **A3. Version in gossip, unsigned.** `Member.Version` is populated in `Mine()` from
  the daemon's own published record and deliberately excluded from `Signable` — a
  signed-set change would drop every older peer from the roster mid-upgrade, exactly
  when it must show both versions (pinned by test). Advisory: a spoofed version
  misleads a display, nothing that routes work. One subtlety shipped with it: a live
  socket whose record was never read (a daemon mid-startup — the restart window) is
  NOT gossiped, because signing an identity-less member overwrote peers' good rows for
  a whole cycle.
- **A4. Down-convert shim.** Deferred (see status).
- **A5. Wire-invariant golden tests.** `Request`/`Response`/`Member`/`Info` field sets
  are locked against deletion/rename (additions are free); the unknown-method
  `Refused` path and the pre-handshake zero-value are pinned.

## Part B — update, as built (same-machine scope)

Scope is **companions on this machine** — each its own `magi --daemon` on a local unix
socket. No ssh, no fleet-door `update` verb, no remote authentication: the signal
reaches a companion over its **own local socket**, whose owner-only permission is the
authentication. No binary is ever sent over any wire — the signal means "update
yourself to the **latest** release" (it names no version; the original sketch's
"version X" was never needed) and the companion self-pulls.

- **B0. Version in the console.** Each companion's build renders as a **Build** field
  on its facts panel in the web console (from the daemon's record locally, from gossip
  for remote rows). As built this is narrower than the sketch: no TUI surface, no
  computed "behind" marker — the update button is offered whenever it could do
  something, and the daemon's own "already up to date" answers the question the marker
  would have guessed at.
- **B1. Local `update` signal.** The daemon's `update` method (optional `Updater`
  engine capability) runs `update.RunCommit` — download the latest release, verify the
  SHA256, **Commit with rollback** — and, only when it actually updated, restarts onto
  the result. The console's "Update to latest" button relays it; the reply is the
  daemon's own one-line account. Same-machine three ways: not on the fleet door
  (asserted), the web handler refuses a peer-scoped request, and it refuses on a
  shared console.
- **B2. Graceful restart.** The `restart` method reuses the shutdown drain — Serve
  drains its **connections**, releases the socket and the lock — then the process
  re-execs instead of exiting. Note the honest wording: the drain is connection-level;
  waiting for an idle **turn** belongs to the auto path (below), and the manual push
  restarts immediately on purpose (an operator pressing a button and watching should
  not have their reply held hostage to an idle moment). A restarted daemon **reopens
  the conversation it was on** — the predecessor leaves the session id in the
  environment (`MAGI_RESTART_SESSION`, consumed once) so a Resume the operator made
  survives the restart.
- **B5. Rollback — a pre-flight, not a post-restart health check.** What shipped:
  `Verify` runs the new binary as `--version` (bounded, hang-proof) and `Commit`
  restores the kept-aside `.prev` if it fails — so the on-disk binary is only ever one
  that passed the pre-flight, and a bad build never becomes the one the daemon would
  restart into. The original sketch's post-restart health-check-and-restart-back was
  descoped: it needs a watchdog that survives the re-exec, and the pre-flight catches
  the cheap-to-check failures (corrupt download, wrong platform, crash-on-start) that
  a SHA256 match does not. `Commit` is serialized and idempotent (two updaters racing
  cannot corrupt the rollback source), and restore does not route through `Apply`
  (whose Windows branch would need to replace the running image — the reason rollback
  used to be impossible there).
- **B7. Auto toggle + bench safety.** `[update] auto` (default on, per companion)
  gates ONLY the auto path — the manual push works with it off. The auto loop starts
  only on the `--daemon` path (a benchmark is a headless one-shot, never a daemon),
  honours `--no-update-check`, and **refuses any non-release build**
  (`SelfUpdatable`): both "dev" and the Makefile's git-describe stamp
  (`v0.22.2-13-g…-dirty`) — a developer's own binary is never replaced out from under
  them. `[update]` is deliberately not merged from a project config: a cloned repo
  must not re-enable auto-update the operator turned off.

### Two triggers, as built
- **Auto** — a daemon whose `[update] auto` is on checks the release source every 6h,
  staggered by a per-**socket** 64-bit-hash jitter across a quarter of that window
  (per-exe seeding was wrong — a machine's daemons share one exe — and both 32-bit
  jitter spellings collapsed the spread to seconds; the arithmetic is now pinned).
  Each daemon keeps its own check stamp (a shared one silenced every daemon but the
  first for the whole window). On a newer release: `RunCommit`, then wait for an idle
  moment — no turn in flight AND no meeting round being composed (`MeetingActive`;
  meeting turns deliberately never enter the run states, which made them invisible to
  the first idle gate) — then restart. If the daemon never idles, the binary is on
  disk and the next start runs it.
- **Manual** — the console's button, over the local socket, regardless of the toggle.

## Decisions (locked, with as-built notes)

1. **update scope** — same-machine only. The boundary, stated precisely: the
   owner-only local socket **plus whatever the operator deliberately pipes into it**
   (`--relay` over their own ssh) — the same boundary `shutdown` and `restart` have.
   The fleet door carries none of the three (asserted by test).
2. **release signing** — none; the signal is a signal, each companion self-pulls +
   SHA256-verifies from the official release over HTTPS. Defer minisign/cosign unless
   a compromised release host becomes a concern.
3. **restart mechanism** — drain, release, then `graceful.Reexec()`; build-tag split.
   Unix: `syscall.Exec` replaces the image (same PID — verified live), successor binds
   fresh; no fd hand-off, deliberately (the tableflip pattern is the riskiest code the
   plan could have contained, for a sub-second window a client rides out). Windows: no
   execve — spawn into its own process group and exit. The re-exec runs from `main()`
   AFTER `run()` returns so the deferred unpublish/release execute first. Clients:
   the web console redials and survives; the attached TUI holds a boxed client and
   redials once on failure (it used to die permanently and blame the daemon).
   Supervisor delegation (launchd/systemd) remains deferred.
4. **triggers** — automatic (toggle, bench-excluded, rollback) plus manual; the
   toggle gates only auto.

## Boundaries

- **The handoff-permission floor is unchanged** — an unattended daemon must not
  auto-approve mutating handed work, and the update path must not become a way around it.
- **No binary is ever shipped over a socket** — the signal names no bytes; the
  companion self-pulls and verifies. (This is what keeps unsigned releases acceptable.)
- **Bench-safety invariant preserved** — the auto loop never runs outside `--daemon`,
  and `--no-update-check` kills it too.
- **Same-machine only** — nothing here can update a companion on another machine.

## Verification (as built — each item is a real test)

- Wire: the field-set golden tests; unknown method → `Refused`; a pre-handshake peer
  supports nothing; `Signable` ignores `Version`.
- Restart: a `restart` request drains and flags the relaunch and frees the socket; a
  `shutdown` does not flag one, and a late `Restart()` cannot resurrect a stopping
  daemon; `reexec` on a missing binary returns without replacing the process.
- Rollback: a bad build rolls back byte-exactly and the good one still runs; a good
  build lands with nothing left behind; a hang in the pre-flight is caught by its
  timeout; `SelfUpdatable` rejects git-describe stamps by name.
- Auto loop: a new release commits and restarts; the restart holds while a turn runs
  (download-anchored window) and fires once idle; already-current does not bounce
  (with real release bytes, so the check is load-bearing); a source build refuses and
  its binary is untouched.
- Console: `/update` refuses on a shared console (both policy shapes) and for a
  peer-scoped request; a record-less starting daemon is not gossiped while the healthy
  one is.
