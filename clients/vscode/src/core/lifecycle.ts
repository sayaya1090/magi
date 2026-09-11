import { ChildProcess, spawn } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { Daemon } from './daemon';
import { features } from './binary';
import { socketPath, tooLong } from './workspace';
import { Launches } from './launches';

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
  readonly socket: string;

  constructor(readonly workdir: string) { this.socket = socketPath(workdir); }

  start(binary: string, manual = false): Promise<void> {
    if (this.closed) return Promise.resolve();
    if (this.pending) return this.pending;
    this.pending = this.launch(binary, manual).finally(() => { this.pending = undefined; });
    return this.pending;
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
      this.external = !this.child || !alive(this.child);
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
    const log = this.socket + '.log';
    fs.mkdirSync(path.dirname(log), { recursive: true });
    const fd = fs.openSync(log, 'a', 0o600);
    // ⚠ **Ask before using it.** A build without the owned mode refuses the flag and exits 2, and
    // the window would report "Companion failed to start" for a binary that is perfectly fine —
    // docs/CLIENT_LIFECYCLE §4: an old core keeps the old lifetime rather than being handed a mode
    // it does not understand.
    const owned = (await features(binary)).has('owned-daemon-v1');
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
    let failure: Error | undefined;
    child.on('error', (e) => { failure = e; });
    // The moment of loss, recorded where it happens rather than where the next start is decided.
    // Putting it beside `may` would be recording a loss and then asking whether the grace has
    // passed in the same breath — the answer is always no, and nothing ever starts again.
    child.on('exit', () => { if (!this.closed) this.budget.lost(Date.now()); });
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
