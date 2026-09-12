import { ChildProcess, spawn } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { Daemon } from './daemon';
import { features } from './binary';
import { socketPath, tooLong } from './workspace';
import { Launches } from './launches';
import { OwnerChannel, ownerChannel } from './owner';

/**
 * How long after asking for a replacement an ending still counts as that replacement.
 *
 * The shared contract's `shutdownMs` (`clients/contract/lifecycle-policy.json`) — what a daemon
 * asked to end is given to end. It has to be bounded: `update` answers "already up to date" without
 * restarting anything, and an arm that never expired would hand the next real crash a free pardon,
 * on a flag nobody could see. `launches.test.ts` holds this number against the contract file.
 */
export const REPLACE_BY_MS = 5_000;

/**
 * The same, for a replacement the daemon will make when it is next idle (`update` with `idle`).
 *
 * Not a guess at how long a turn runs — it is a ceiling on how long a latch may pardon an ending. The
 * ending itself clears it (see `ended`), so this only matters when the replacement never happens at
 * all, and then the question is how long a stale pardon may stand rather than how long work takes.
 */
export const DEFERRED_REPLACE_BY_MS = 60 * 60 * 1_000;

/** The record beside the socket, as the core writes it (`internal/adapter/daemon/publish.go`). */
export type Published = { pid?: number; instance?: string; owner?: string };

/** What `about` says about the process answering. */
export type Hello = { instance?: string; owner?: string };

/**
 * Is the daemon answering the socket the child this window started?
 *
 * Three facts, not one. The pid says the record describes our child; the instance says the process
 * ANSWERING is the one the record describes. Reachability alone says neither — it says somebody is
 * there, and the case this exists for is somebody else being there.
 *
 * ⚠ **A core that publishes no instance falls back to the pid**, which is exactly what this client
 * did before. docs/CLIENT_LIFECYCLE §4: an old core keeps the lifetime it always had rather than
 * being failed for lacking a field it never wrote. The stricter check applies as soon as both sides
 * name a generation.
 */
export function sameGeneration(record: Published | null, hello: Hello | null, childPid?: number): boolean {
  if (!record || !hello) return false;
  if (childPid === undefined || record.pid !== childPid) return false;
  // ⚠ **One side naming a generation and the other not is NOT an old core.** The record's writer and
  // the process answering are the same daemon, so a record that carries an instance came from one
  // that answers with it too. Only one of them present means something ELSE answered — treating
  // that as backward compatibility waves through the very case this check exists for. The pid
  // fallback is for when NEITHER names one.
  if (record.instance || hello.instance) return record.instance === hello.instance;
  return true;
}

const pause = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));
const alive = (p: ChildProcess) => p.pid !== undefined && p.exitCode === null && p.signalCode === null;

/** Own only the process launched by this window. A pre-existing companion belongs to its caller. */
export class OwnedCompanion {
  private child?: ChildProcess;
  private pending?: Promise<void>;
  private closing?: Promise<void>;
  private closed = false;
  /**
   * The owning lineage this window confirmed at readiness, when the core publishes one.
   *
   * Inherited across the daemon's own updates, unlike the instance — which is the whole point: it
   * answers "is the thing on this socket still mine" for a successor this window never spawned and
   * holds no handle for.
   */
  private lineage?: string;
  /** What went wrong while stopping, if anything. Kept rather than thrown — see `close`. */
  private trouble?: Error;

  /** Why the last close had trouble, or undefined. For a caller that wants to say so. */
  get closeTrouble(): Error | undefined { return this.trouble; }
  /**
   * The launch budget, as the shared contract states it — `clients/contract/lifecycle-policy.json`.
   *
   * It used to be a bare array of timestamps here and a different rule again in the JetBrains
   * client, which is how two editors ended up green on two policies (measured 2026-09-11).
   */
  private readonly budget = new Launches();
  private external = false;
  /**
   * Set when this window started a daemon that it CANNOT own — a core without `owned-daemon-v1`.
   *
   * ⚠ **The point is not to hide it.** docs/CLIENT_LIFECYCLE §4 says to block a new launch that
   * REQUIRES the owned mode and, in the very next sentence, never to fall back silently. A plain
   * `--daemon` start is not a blocked case: the window still stops its own child on close, and that
   * is the lifetime this tree had before the mode existed and which §4 preserves. What it loses is
   * the one thing a handle cannot do — an extension host that is KILLED runs no `deactivate`, and
   * the daemon then outlives the window. That is a smaller guarantee, not a failure, and a person
   * is owed the difference rather than a silent downgrade.
   *
   * Read once by whoever draws it (see `unowned`), so the notice does not repeat on every poll.
   */
  private unownedStart = false;
  /**
   * Until when an ending is the replacement the person asked for, rather than a crash.
   *
   * ⚠ **On Windows the owned child EXITS for `magi.updateCore` and `magi.restartDaemon`.** Windows
   * has no execve, so the daemon's own restart spawns a successor and ends this process
   * (`internal/graceful/graceful_windows.go`); on Unix the image is replaced and the PID stays, so
   * nothing here ever hears about it. Without this, the handler below reads the person's own button
   * as a crash — and the contract's case "an update replacement the person asked for is not a
   * failure" had no caller anywhere in this client, so `Launches.replaced` was dead code.
   *
   * Measured on Windows 11, 2026-09-11, against the real binary: the owned child exited 27ms after
   * the `restart` door answered `{"ok":true}`. Three presses of Restart inside one stable window is
   * then three consecutive failures, and `failuresToBlock` is three — the companion goes to Blocked
   * and this window stops starting it automatically, for a button the person pressed on purpose.
   */
  private replaceBy = 0;
  /**
   * A replacement this window asked for has happened, and the daemon now listening is still ours.
   *
   * There is no handle for it — on Windows the successor is a different process this window never
   * spawned — but it is not somebody else's companion either: it inherited the owner pipe this
   * extension host holds. `theirs` is where that distinction is made.
   */
  private ownsSuccessor = false;
  /**
   * The write end of the owner pipe, when this window made it itself.
   *
   * Held here rather than left to `child.stdin`, which belongs to `child_process` and is destroyed
   * the moment the child exits — see `ownerChannel`. Closed by `close()`, so letting go of the
   * window reaches even a successor this window holds no handle for.
   */
  private channel?: OwnerChannel;
  /** Why the last start fell back to Node's pipe, until somebody has been told. See `unpiped`. */
  private unheldWhy = '';
  readonly socket: string;

  /**
   * Two seams, both there because the interesting moment cannot be produced any other way.
   *
   * `ask` is how this window asks a binary what it can do — the real probe, or a test's. Injected
   * rather than imported straight, because the defect it exists for lives in the WAIT: `close()` can
   * finish while the probe is still in flight, and a direct import cannot be made to wait.
   *
   * `pipe` is how this window takes an owner pipe. Injected for the same kind of reason: the
   * interesting answer is the one where taking it FAILED, and the real one cannot be made to fail on
   * demand — its name carries eight random hex digits precisely so that nobody can take it first.
   * What a test needs to reach here is not the failure itself (`owner.test.ts` produces that against
   * a squatted name) but what this class DOES with it, which had no reader at all (issue #189).
   */
  constructor(readonly workdir: string, private readonly ask: typeof features = features,
              private readonly pipe: typeof ownerChannel = ownerChannel,
              // Injected for the same reason the probe is: what the ready path DOES with the answer
              // — keeping the lineage — cannot be measured by a function that only returns a verdict,
              // and reaching it through a real daemon needs one that can be made to disagree with
              // its own record.
              private readonly hello: () => Promise<Hello | null> = () => Promise.resolve(null)) {
    this.socket = socketPath(workdir);
  }

  start(binary: string, manual = false): Promise<void> {
    if (this.closed) return Promise.resolve();
    if (this.pending) return this.pending;
    this.pending = this.launch(binary, manual).finally(() => { this.pending = undefined; });
    return this.pending;
  }

  /**
   * The person asked this companion to replace itself — `magi.updateCore`, `magi.restartDaemon`.
   *
   * Called BEFORE the door, not after: `restart` answers and then ends the process, and on Windows
   * the exit lands within tens of milliseconds. Arming afterwards would be arming after the event
   * it describes.
   */
  replacing(now = Date.now()): void { this.replaceBy = now + REPLACE_BY_MS; }

  /**
   * The same request, deferred: the daemon will replace itself **when nothing is running**, which may
   * be minutes from now.
   *
   * ⚠ **A deadline is the wrong shape for this one.** `replacing` gives the ending five seconds
   * because that is what an immediate restart takes; a deferred one waits for a quiet moment nobody
   * can predict, and the window would then count the person's own update as a crash — the defect
   * `c5373a08` fixed for the other path, arriving through the new door. So this is a LATCH, cleared
   * by the first ending, which is the ending it describes.
   *
   * Bounded anyway. A latch that never cleared would pardon a real crash on a flag nobody can see,
   * so it expires after an hour: long enough that a companion working through a long turn still gets
   * its restart counted as one, short enough that a forgotten latch is not permanent.
   */
  replacingWhenIdle(now = Date.now()): void { this.replaceBy = now + DEFERRED_REPLACE_BY_MS; }

  /**
   * What the owned process ending means to the budget.
   *
   * Named rather than written inline in the handler so it can be measured: reaching it through a
   * live daemon would need one, and on the platform where it matters the process it needs is the
   * one that just exited.
   */
  private ended(now: number, child?: ChildProcess): void {
    if (this.closed) return;
    if (now > this.replaceBy) { this.budget.lost(now); return; }
    this.replaceBy = 0;
    this.ownsSuccessor = true;
    if (child && this.child === child) this.child = undefined;
    this.budget.replaced(now);
  }

  /**
   * Whether the companion answering on this socket belongs to somebody else.
   *
   * ⚠ **"No live child of mine" is not the same question.** After a replacement this window has no
   * handle at all, and reading that as "a pre-existing companion" disowned the very daemon this
   * window had just asked for: `launch` returns at its first line for anything not manual, so the
   * window would never start it again — and `close()` would leave it to be stopped by the pipe
   * alone.
   */
  private theirs(): boolean {
    // The wire answers this better than a local flag can: a daemon publishing the lineage this
    // window confirmed is this window's, however many times it has replaced itself and whether or
    // not a handle survived. `ownsSuccessor` stays for cores that publish no owner at all.
    const owner = this.published()?.owner;
    if (owner && this.lineage) return owner !== this.lineage;
    if (this.ownsSuccessor) return false;
    return !this.child || !alive(this.child);
  }

  /**
   * Who is answering this socket, from the daemon's own mouth.
   *
   * ⚠ **"Something answered" is not "the child I started answered."** The record beside the socket
   * and the process on it can disagree — a previous daemon's record outliving it, a replacement
   * mid-flight, somebody else's companion on a socket path this window also resolved. The two ids
   * were on the wire and in this client's own types the whole time, and nothing read either
   * (docs/CLIENT_LIFECYCLE §4: readiness requires the child pid, the workspace AND a matching
   * instance in the record and in `about`).
   */
  private async identify(): Promise<Hello | null> {
    const injected = await this.hello();
    if (injected) return injected;
    try {
      const d = await Daemon.connect(this.socket, 1000);
      try {
        const r = await d.exchange({ method: 'about' }, 1000);
        return r.ok ? { instance: r.instance, owner: r.owner } : null;
      } finally { d.close(); }
    } catch { return null; }
  }

  private async reachable(): Promise<boolean> {
    try { const d = await Daemon.connect(this.socket, 1000); d.close(); return true; }
    catch { return false; }
  }

  private async launch(binary: string, manual: boolean): Promise<void> {
    if (this.external && !manual) return;
    const long = tooLong(this.socket);
    if (long) throw new Error(long);
    if (await this.reachable()) {
      this.external = this.theirs();
      // ⚠ **Being reachable forgives nothing on its own.** This used to clear the whole budget, so
      // a daemon that came up and died every few seconds was pardoned on every poll and retried
      // forever — it never missed the ready deadline, so nothing ever counted it as a failure.
      // `connected` counts how long it has actually held, and only a minute of it clears anything.
      this.budget.connected(Date.now());
      return;
    }
    if (this.closed || (this.child && alive(this.child))) return;
    const now = Date.now();
    if (this.budget.may(now, manual) !== 'allow') return;
    this.budget.spawned(now);
    this.external = false;
    // A child of this window's own again, so the successor rule below goes back to asking about it.
    this.ownsSuccessor = false;
    // ⚠ **Ask before using it.** A build without the owned mode refuses the flag and exits 2, and
    // the window would report "Companion failed to start" for a binary that is perfectly fine —
    // docs/CLIENT_LIFECYCLE §4: an old core keeps the old lifetime rather than being handed a mode
    // it does not understand.
    // ⚠ **`close()` can finish while this await is in flight, and then nobody owns what comes
    // next.** The check at the top of this function ran before the probe; `close()` stops
    // `this.child`, and during the wait there is no child to stop — so it returns having stopped
    // nothing, its promise resolves, the window is gone, and the spawn below still happens. The
    // daemon it starts belongs to nobody: no `deactivate` will run again, and the owner pipe's
    // write end is held by an extension host that has finished with this companion.
    //
    // Measured 2026-09-11 (fixed in commit 179169f8; docs/CLIENT_LIFECYCLE.md §4): start → probe waits → `close()`
    // resolves → probe answers, and a child appeared with nothing left to stop it.
    //
    // Checked twice on purpose. Here, so the ordinary case costs nothing; and again after the
    // spawn, because `close()` can also land in the gap between this line and the process actually
    // existing — and that one cannot be prevented, only cleaned up.
    const owned = (await this.ask(binary)).has('owned-daemon-v1');
    if (this.closed) return;
    // The owner's pipe, and the only thing that carries lifetime authority: this window holds the
    // write end and hands it to nobody, so the daemon goes when this window does — even if the
    // extension host is killed and no `deactivate` ever runs. Without the mode, stdin stays ignored
    // and the lifetime is exactly what it was.
    //
    // ⚠ **`'pipe'` does not keep that promise, and on Windows it turned an update into a kill** —
    // `child_process` owns what it makes and destroys `child.stdin` when the child exits, so the
    // successor of a restart read EOF and stopped itself (commit 07358567; see `ownerChannel`). The same closed
    // check as above: this await is a second chance for `close()` to finish first.
    const channel = owned ? await this.pipe(path.basename(this.socket)) : undefined;
    if (this.closed) { channel?.close(); return; }
    this.channel?.close();
    this.channel = channel;
    // A channel that could not be taken leaves this companion on the unheld fallback lifetime (commit 07358567), and
    // §4 forbids arriving there in silence. Recorded here — where whether it was held is actually
    // known — and read once by whoever draws it.
    this.unheldWhy = channel && !channel.held ? channel.why : "";
    // ⚠ **The log fd is opened AFTER the last await, and that placement is the fix.** It used to be
    // opened at the top of this function, before the feature probe — and the two `closed` checks
    // (commit 179169f8) both return between there and the `finally` that closes it, so every launch that
    // lost the close race leaked one file handle (fixed in commit 59230ae6). An
    // extension host is long-lived and a window can race a launch as often as a user reloads it.
    // Nothing between here and the spawn awaits, so no return can slip in front of the finally.
    const log = this.socket + '.log';
    fs.mkdirSync(path.dirname(log), { recursive: true });
    const fd = fs.openSync(log, 'a', 0o600);
    let child: ChildProcess;
    try {
      child = spawn(binary, owned ? ['--daemon', '--client-owned'] : ['--daemon'], {
        cwd: this.workdir, env: process.env, windowsHide: true,
        stdio: [channel ? channel.stdin : 'ignore', fd, fd],
      });
    } finally { fs.closeSync(fd); }
    this.child = child;
    // Recorded where `owned` is actually known, and only for a start this window made: connecting
    // to somebody else's daemon says nothing about what this binary can do for us.
    this.unownedStart = !owned;
    // ⚠ **Nothing awaits between the check above and this line, and that is the only reason this
    // is not a second race.** `spawn` returns synchronously and the fd work around it is sync too,
    // so `closed` cannot flip in between — one `await` introduced there and the window reopens,
    // with no test able to see it. Kept as the cleanup that would be needed then: whoever loses
    // stops the process and forgets it, because a window that has closed has no child.
    //
    // Deliberately not covered by a test. The order cannot be produced today, and a test that
    // cannot fail is worse than none — it reads as proof.
    if (this.closed) {
      await this.stop(child);
      if (this.child === child) this.child = undefined;
      return;
    }
    let failure: Error | undefined;
    child.on('error', (e) => { failure = e; });
    // The moment of loss, recorded where it happens rather than where the next start is decided.
    // Putting it beside `may` would be recording a loss and then asking whether the grace has
    // passed in the same breath — the answer is always no, and nothing ever starts again.
    child.on('exit', () => this.ended(Date.now(), child));
    const end = Date.now() + 30_000;
    while (!this.closed && !failure && alive(child) && Date.now() < end) {
      const hello = await this.identify();
      if (sameGeneration(this.published(), hello, child.pid)) {
        // The lineage this window confirmed. A later generation answering with it is this window's
        // companion having replaced itself; one answering with a different owner is somebody
        // else's, whatever handle this process happens to hold.
        if (hello?.owner) this.lineage = hello.owner;
        this.budget.ready(Date.now());
        return;
      }
      await pause(100);
    }
    if (this.closed) return;
    if (!failure && await this.reachable()) { this.external = true; return; }
    // Missed the ready deadline, or died on the way up. Counted apart from the rolling window: a
    // window slides and forgives, a run of failures must not — otherwise a crashloop is pardoned
    // once a minute, forever.
    this.budget.failed(Date.now());
    const reason = failure?.message ?? (alive(child) ? 'timed out after 30s' : `exit ${child.exitCode ?? child.signalCode}`);
    await this.stop(child);
    throw new Error(`Companion failed to start: ${reason}. Log: ${log}`);
  }

  /**
   * Did this window start a daemon it cannot own, and has nobody been told yet?
   *
   * Answers true ONCE per such start. A window polls every fifteen seconds; a notice that repeated
   * on every poll would be noise, and noise is how a real warning stops being read.
   */
  unowned(): boolean {
    if (!this.unownedStart) return false;
    this.unownedStart = false;
    return true;
  }

  /**
   * Why this window could not take an owner pipe of its own — once, then empty.
   *
   * ⚠ **The fallback lands on exactly the lifetime R2 fixed.** A core WITH the owned mode, started
   * on Node's own `'pipe'`, is a companion an update alone kills on Windows. `ownerChannel` falls
   * back on purpose — refusing to start over a taken pipe name would hand an outage to whoever took
   * it — but §4's next sentence is that a weaker lifetime is never arrived at quietly, and there was
   * no reader for `held` at all (issue #189, review R8).
   *
   * Same one-shot as `unowned` and for the same reason: fifteen-second polls turn a repeated notice
   * into noise.
   */
  unpiped(): string {
    const why = this.unheldWhy;
    this.unheldWhy = '';
    return why;
  }

  private published(): Published | null {
    try { return JSON.parse(fs.readFileSync(this.socket + '.session', 'utf8')) as Published; }
    catch { return null; }
  }

  private publishedPID(): number | undefined { return this.published()?.pid; }

  private async stop(child: ChildProcess): Promise<void> {
    if (!alive(child)) return;
    if (this.publishedPID() === child.pid) {
      try {
        const d = await Daemon.connect(this.socket, 500);
        try { await d.exchange({ method: 'shutdown' }, 500); } finally { d.close(); }
      } catch { /* fall back to the owned process handle */ }
      const end = Date.now() + 500;
      while (alive(child) && Date.now() < end) await pause(25);
    }
    if (alive(child)) child.kill();
    const end = Date.now() + 500;
    while (alive(child) && Date.now() < end) await pause(25);
    if (alive(child)) child.kill('SIGKILL');
  }

  close(): Promise<void> {
    if (this.closing) return this.closing;
    this.closed = true;
    // ⚠ **Let go of the pipe AFTER the handle, and unconditionally.** `stop` talks to the daemon on
    // the socket and then kills the process this window spawned; after a replacement that process is
    // already gone and the daemon answering is a successor with no handle here. Dropping the owner
    // pipe is what reaches that one — it is the whole point of the mode — and it must happen whether
    // or not there was still a child to stop.
    //
    // ⚠ **And closing SETTLES, whatever stopping did.** `stop` talks to a socket and kills a
    // process, and both of those fail for ordinary reasons — the daemon already gone, a kill
    // refused. `.finally` ran the cleanup but let the failure through, so `close()` rejected: at
    // window close that is an error VS Code reports about a shutdown that went fine, and the
    // rejected promise is cached in `this.closing`, so every later `close()` — `dispose` is called
    // more than once — rejects again with nobody awaiting it. §5 asks for confirmation of shutdown,
    // not for its success.
    this.closing = (this.child ? this.stop(this.child) : Promise.resolve())
      .catch((e) => { this.trouble = e instanceof Error ? e : new Error(String(e)); })
      .finally(() => { this.channel?.close(); this.channel = undefined; });
    return this.closing;
  }
  dispose(): void { void this.close(); }
}
