import { ChildProcess, spawn } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { Daemon } from './daemon';
import { socketPath, tooLong } from './workspace';

const pause = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));
const alive = (p: ChildProcess) => p.pid !== undefined && p.exitCode === null && p.signalCode === null;

/** Own only the process launched by this window. A pre-existing companion belongs to its caller. */
export class OwnedCompanion {
  private child?: ChildProcess;
  private pending?: Promise<void>;
  private closing?: Promise<void>;
  private closed = false;
  private attempts: number[] = [];
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
      this.attempts = [];
      return;
    }
    if (this.closed || (this.child && alive(this.child))) return;
    this.attempts = this.attempts.filter((t) => Date.now() - t < 60_000);
    if (!manual && this.attempts.length >= 3) return;
    this.attempts.push(Date.now());
    this.external = false;
    const log = this.socket + '.log';
    fs.mkdirSync(path.dirname(log), { recursive: true });
    const fd = fs.openSync(log, 'a', 0o600);
    let child: ChildProcess;
    try {
      child = spawn(binary, ['--daemon'], {
        cwd: this.workdir, env: process.env, windowsHide: true, stdio: ['ignore', fd, fd],
      });
    } finally { fs.closeSync(fd); }
    this.child = child;
    let failure: Error | undefined;
    child.on('error', (e) => { failure = e; });
    const end = Date.now() + 30_000;
    while (!this.closed && !failure && alive(child) && Date.now() < end) {
      if (this.publishedPID() === child.pid && await this.reachable()) return;
      await pause(100);
    }
    if (this.closed) return;
    if (!failure && await this.reachable()) { this.external = true; return; }
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
