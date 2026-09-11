import { ChildProcess, spawn } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { Daemon } from './daemon';
import { features } from './binary';
import { socketPath, tooLong } from './workspace';
import { Launches } from './launches';

/**
 * How long after asking for a replacement an ending still counts as that replacement.
 *
 * The shared contract's `shutdownMs` (`clients/contract/lifecycle-policy.json`) — what a daemon
 * asked to end is given to end. It has to be bounded: `update` answers "already up to date" without
 * restarting anything, and an arm that never expired would hand the next real crash a free pardon,
 * on a flag nobody could see. `launches.test.ts` holds this number against the contract file.
 */
export const REPLACE_BY_MS = 5_000;

const pause = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));
const alive = (p: ChildProcess) => p.pid !== undefined && p.exitCode === null && p.signalCode === null;

/** Own only the process launched by this window. A pre-existing companion belongs to its caller. */
export class OwnedCompanion {
  private child?: ChildProcess;
  private pending?: Promise<void>;
  private closing?: Promise<void>;
  private closed = false;
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
  readonly socket: string;

  /**
   * How this window asks a binary what it can do — the real probe, or a test's.
   *
   * Injected rather than imported straight, because the defect this seam exists for lives in the
   * WAIT: `close()` can finish while the probe is still in flight. Reaching that window from a test
   * means controlling when the probe answers, and a direct import cannot be made to wait.
   */
  constructor(readonly workdir: string, private readonly ask: typeof features = features) {
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
    if (this.ownsSuccessor) return false;
    return !this.child || !alive(this.child);
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
    const log = this.socket + '.log';
    fs.mkdirSync(path.dirname(log), { recursive: true });
    const fd = fs.openSync(log, 'a', 0o600);
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
    // Measured 2026-09-11 (docs/CLIENT_LIFECYCLE_REVIEW R1): start → probe waits → `close()`
    // resolves → probe answers, and a child appeared with nothing left to stop it.
    //
    // Checked twice on purpose. Here, so the ordinary case costs nothing; and again after the
    // spawn, because `close()` can also land in the gap between this line and the process actually
    // existing — and that one cannot be prevented, only cleaned up.
    const owned = (await this.ask(binary)).has('owned-daemon-v1');
    if (this.closed) return;
    let child: ChildProcess;
    try {
      child = spawn(binary, owned ? ['--daemon', '--client-owned'] : ['--daemon'], {
        cwd: this.workdir, env: process.env, windowsHide: true,
        // The owner's pipe, and the only thing that carries lifetime authority: this window holds
        // the write end and hands it to nobody, so the daemon goes when this window does — even if
        // the extension host is killed and no `deactivate` ever runs. Without the mode, stdin stays
        // ignored and the lifetime is exactly what it was.
        stdio: [owned ? 'pipe' : 'ignore', fd, fd],
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
      if (this.publishedPID() === child.pid && await this.reachable()) {
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

  private publishedPID(): number | undefined {
    try { return JSON.parse(fs.readFileSync(this.socket + '.session', 'utf8')).pid; }
    catch { return undefined; }
  }

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
    this.closing = this.child ? this.stop(this.child) : Promise.resolve();
    return this.closing;
  }
  dispose(): void { void this.close(); }
}
