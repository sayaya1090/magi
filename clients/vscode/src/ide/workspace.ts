import * as vscode from 'vscode';
import * as fs from 'fs';
import { Daemon } from '../core/daemon';
import { Response } from '../core/protocol';
import { socketPath, tooLong } from '../core/workspace';
import * as activity from '../core/activity';

/**
 * The one companion this window talks to, and the connection to it.
 *
 * One per window rather than one per folder: VS Code opens a window per workspace, and the
 * companion is the workspace's. A multi-root workspace picks the first folder — a second
 * companion for the same window would be two engines on one screen with nothing saying which
 * answered.
 */
export class Companion implements vscode.Disposable {
  private conn: Daemon | null = null;
  private readonly changed = new vscode.EventEmitter<activity.Activity>();
  readonly onChanged = this.changed.event;
  private last: activity.Activity = activity.cannotSay();
  private timer: NodeJS.Timeout | null = null;
  private gone = false;

  constructor(readonly workdir: string) {}

  get socket(): string { return socketPath(this.workdir); }
  get state(): activity.Activity { return this.last; }

  /**
   * Reach the companion, or say why not.
   *
   * A missing socket file is "not running" — a fact. Anything else is "cannot say", which is a
   * different fact and must not be drawn as the first one (invariant 0-3).
   */
  async reach(): Promise<Daemon | null> {
    if (this.conn) return this.conn;
    const p = this.socket;
    const long = tooLong(p);
    if (long) { this.set({ state: activity.State.Unknown, asking: long }); return null; }
    if (!fs.existsSync(p)) { this.set(activity.notRunning()); return null; }
    try {
      const d = await Daemon.connect(p);
      d.whenClosed(() => { if (this.conn === d) this.conn = null; });
      this.conn = d;
      return d;
    } catch {
      // The socket file is there and nothing answered. That is a corpse, not an absence — but for
      // the person the useful word is still "not running", because there is nothing to talk to.
      this.set(activity.notRunning());
      return null;
    }
  }

  /** Ask one question. Null when we could not ask at all — never a fabricated answer. */
  async ask(method: string, extra: Record<string, unknown> = {}): Promise<Response | null> {
    const d = await this.reach();
    if (!d) return null;
    try {
      return await d.exchange({ method, ...extra });
    } catch {
      this.conn = null;
      return null;
    }
  }

  /** Poll `status` and tell the screens. One place decides the word (core/activity). */
  watch(everyMs = 2000): void {
    const tick = async () => {
      if (this.gone) return;
      const p = this.socket;
      if (!fs.existsSync(p)) this.set(activity.notRunning());
      else this.set(activity.of(await this.ask('status')));
      if (!this.gone) this.timer = setTimeout(tick, everyMs);
    };
    void tick();
  }

  private set(a: activity.Activity): void {
    if (a.state === this.last.state && a.doing === this.last.doing && a.asking === this.last.asking) return;
    this.last = a;
    this.changed.fire(a);
  }

  dispose(): void {
    this.gone = true;
    if (this.timer) clearTimeout(this.timer);
    this.conn?.close();
    this.changed.dispose();
  }
}
