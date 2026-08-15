# Design: cross-version compatibility and fleet self-update

Status: proposed (decisions locked, not yet implemented).

magi companions each run as an independent process — a per-workspace `magi --daemon`
on a unix socket, with web/TUI attach clients relaying to it, and a fleet of named
companions that gossip, hand work to each other, and hold meetings over ssh. This
document covers two related additions:

- **A. Compatibility** — a newer and an older instance keep interoperating (relay,
  handoff, meetings, gossip) instead of silently misbehaving when versions diverge.
  This stays **fleet-wide** (mixed versions across machines must still interoperate).
- **B. Update** — an instance updates itself, scoped to **same-machine** companions:
  each shows its version in the agent list, updates itself automatically when a new
  release drops (toggle), and can be pushed an update by hand from the list when it lags.
  Cross-machine (remote) push is out of scope for now.

## Terrain (verified seams)

- **version** — `internal/version` (`Version`/`Commit`/`Date`, ldflags). Surfaced
  locally only; **never exchanged between instances**.
- **relay** — `internal/adapter/daemon/daemon.go`: newline-delimited JSON,
  `Request{Method, …flat bag}` / `Response{OK, Err, …union}`. **No protocol-version
  field, no handshake.** An unknown *method* returns a typed `Refused` (graceful); an
  unknown *field* is silently dropped by `encoding/json` (no `DisallowUnknownFields`).
  The `about` method (`answers["about"]`, client `exchange(Request{Method:"about"})`)
  is the only self-description, and it carries capabilities but **no version**.
- **fleet** — two ssh transports: gossip (`cluster.Member`, signed via `Signable`,
  **no version field**) and work relay (`--relay` byte pipe; `--fleet-door` narrowed
  to `doorMethods = "about, hand, hand-state"`).
- **update** — `internal/update` is **self-only** (`Source` iface, `Run`, `Apply`;
  SHA256 integrity, **no authorship signature**; **no restart** — it prints "Restart
  magi"). No peer/remote update, no graceful reload. `shutdown` is the only remote
  lifecycle verb and is **not** on the door allowlist. The startup auto-check
  (`autoupdate.go`) is **TTY-only, never headless/CI** — a bench-safety invariant.
- **config** — `LoadWithUnknown` loads unknown keys (warns) and defaults missing ones.
  Robust; **the model the wire should follow.**

### Where it breaks if versions diverge
1. No version handshake on any inter-instance path.
2. Silent field-drop (no `DisallowUnknownFields`) → a new field to an old peer is
   ignored, wrong behavior, no error. The only *graceful* mismatch is an unknown method.
3. A change to the signed gossip set (`Signable`) hard-drops old peers until re-gossip.
4. Version is stamped locally but never travels, so the fleet can't even see who is on what.
5. No peer-update primitive and no graceful reload.

## Principles

- **Additive-only wire** — never remove, rename, or repurpose a `Request`/`Response`/
  `Member` field; new fields are optional with safe zero-values.
- **Negotiate, then gate** — exchange version + capabilities, and only send a new
  method/field to a peer that advertises it. This defeats the silent field-drop trap
  by never sending what the peer can't parse.
- **Down-convert at the boundary** when the peer is older.
- **Inherit config's lenient-decode philosophy.**

## Part A — compatibility

- **A1. Version + capabilities on `about`.** Extend the existing `about` response with
  `Version string`, `Proto int`, `Caps []string` (additive). A client caches the peer's
  version/capset per connection on first `about`. No new seam invented.
- **A2. Capability gating.** A helper "send X only if the peer advertised X." New
  methods/fields live behind it.
- **A3. Version in gossip (unsigned).** Add `Version` to `cluster.Member` but **not** to
  `Signable` — a change to the signed set hard-drops old peers during a rolling upgrade.
  Display/diagnostic only, so the fleet can see who is on what.
- **A4. Down-convert shim.** When a negotiated peer version < ours, translate new→old
  payloads (hand/meet) at the boundary; per-version adapters.
- **A5. Wire-invariant golden tests.** Snapshot the `Request`/`Response`/`Member` field
  sets to lock against deletion/rename, plus a test of the unknown-method `Refused` path.

## Part B — update (same-machine scope)

Scope is **companions on this machine** — each its own `magi --daemon` on a local unix
socket. That removes the scary parts: no ssh, no fleet-door `update` verb, no remote
authentication. The "update" reaches a companion over its **own local socket**, whose
owner-only file permission *is* the authentication (the same trust boundary `--relay`
already relies on). No binary is ever sent over any wire: the signal names a target
version and the companion self-pulls.

- **B0. Version in the agent list.** Surface each companion's version (from A3's
  `cluster.Member.Version`, and its own `about` for local ones) in the web/TUI companion
  list, with an "update available / behind" marker when it trails the newest local one
  or the latest release.
- **B1. Local `update` signal — not a binary.** A daemon method that tells a
  same-machine companion "update yourself to version X from the official release." The
  companion runs its own `update.Run` (GitHubSource, HTTPS + SHA256 against the
  release's `checksums.txt`) — identical trust to today's self-update. Reached over the
  target's local socket; no fleet-door, no Vouched keys needed (all local).
- **B2. Graceful restart** (see decision 3): drain in-flight turns, then hand the socket
  to the new binary.
- **B5. Rollback on unhealthy.** Keep the previous binary. After the restart, health-check
  the new daemon (rebinds the socket AND answers `about`) within a timeout; on failure,
  restore the previous binary and restart back. Auto without rollback is forbidden.
- **B7. Auto toggle + bench/CI exclusion.** A config toggle `[update] auto` (per
  companion) turns automatic updating on or off. It gates **only auto** — an explicit
  manual push from the list still updates a companion whose auto is off. The auto path
  **never** runs in a headless/bench/eval context, preserving the existing bench-safety
  invariant (`autoupdate.go` is TTY-only today; a daemon in a bench context must not
  auto-update).

### Two triggers
- **Auto** — a companion whose `[update] auto` is on checks the release source
  periodically (hours, jittered) and, on a newer release, self-pulls + verifies →
  graceful restart with rollback. Skipped entirely in a bench/CI context. To avoid every
  same-machine daemon restarting at once, stagger by a per-socket jitter.
- **Manual** — from the companion list, an operator pushes an update to a lagging
  same-machine companion (B1) over its local socket. Works regardless of that
  companion's auto toggle — it is an explicit, local, human action.

## Decisions (locked)

1. **update scope** — **same-machine companions only**. The signal reaches a companion
   over its own local unix socket, whose owner-only permission is the authentication —
   no ssh, no fleet-door `update` verb, no Vouched keys. (Remote/cross-machine push is
   deferred; it would add the fleet-door verb + Vouched auth back.)
2. **release signing** — none; B1 is a signal, each companion self-pulls + SHA256-verifies
   from the official release. (Defer minisign/cosign unless a compromised release host
   becomes a concern.)
3. **restart mechanism** — cross-platform, one `graceful.Reexec()` seam, build-tag split.
   **Drain, release, then re-exec** — deliberately NOT zero-gap fd-inheritance:
   - The daemon reuses its existing shutdown drain: a `restart` request signals Stop, so
     Serve drains its connections and its deferred cleanup releases the socket and the
     lock, exactly as a shutdown does. Then, instead of exiting, the process re-execs.
   - **Unix (darwin/linux)**: `syscall.Exec` replaces the image (same PID). The socket
     was already released, so the successor **binds fresh** — no fd hand-off. This drops
     the fragile part of the `tableflip`/nginx pattern (extracting the listener fd,
     clearing `FD_CLOEXEC`, re-listening on an inherited fd) for a sub-second window
     where the socket is unavailable, which a client rides out by retrying. For a
     personal agent daemon whose restart is a rare deliberate event, that trade is right;
     zero-gap fd-inheritance would be over-engineering and the riskiest code in the plan.
   - **Windows**: no `execve` — spawn the new binary detached and exit; the successor
     rebinds. Same sub-second window.
   - The re-exec runs from `main()` AFTER `run()` returns, so the daemon's deferred
     cleanup (unpublish the record, release socket + lock) executes first — `syscall.Exec`
     would skip any pending defer, which is why it happens out there and not inside `run()`.
   - **Optional (deferred)**: under launchd/systemd/Windows-Service, prefer "exit and let
     the supervisor restart." Not built yet.
4. **triggers** — **automatic** (per-companion `[update] auto` toggle, bench/CI-excluded,
   with rollback) **plus** a **manual** push from the companion list. The toggle gates
   only auto; a manual push works even when auto is off.

## Landing order

- **Phase 1 (foundation, no feature)**: A1 + A2 + A5 — negotiation, gating, invariant
  tests. Makes every later change safe; independent of the update work.
- **Phase 2**: A3 (version visibility, incl. gossip `Member.Version`) + A4 (down-convert
  shim; first real use is hand/meet across versions).
- **Phase 3 (update, same-machine)**: B2 (graceful restart, so self-update finishes the
  job) → B5 (rollback) → B0 (version in the list) + B1 (local update signal) + B7 (auto
  toggle + bench-exclusion) + the two triggers (auto/manual).

## Boundaries

- **The handoff-permission floor is unchanged** — an unattended daemon must not
  auto-approve mutating handed work, and the update path must not become a way around it.
- **No binary is ever shipped over a socket** — the signal names a version; the companion
  self-pulls and verifies. (This is also what keeps unsigned releases acceptable.)
- **Bench-safety invariant preserved** — auto-update never runs in a headless/bench/eval
  context.
- **Same-machine only** — the update signal crosses a local, owner-only socket, never
  ssh; nothing here can update a companion on another machine.

## Verification

- Cross-version run (two binaries at different versions): a new method to an old daemon
  returns `Refused`; a new field is *not sent* (gated), not silently dropped.
- Rolling gossip upgrade: an old `Member` still verifies on a new daemon (Version is
  unsigned).
- Update: dry-run (download + verify only); the restart drains in-flight turns; an
  unhealthy new binary rolls back to the previous one; a bench/headless context skips the
  auto-check entirely; the `[update] auto` toggle off stops the auto path but a manual
  push still updates; the list marks a companion that is behind.
