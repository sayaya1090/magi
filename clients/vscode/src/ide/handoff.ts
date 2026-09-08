import * as vscode from 'vscode';
import { Daemon } from '../core/daemon';
import { Companion } from './workspace';
import { Handed, dispatchLabel, handState, peerLabel, peers } from '../core/handoff';

/**
 * Asking another companion on this machine to do something, and watching it.
 *
 * The fleet list has been on the panel for a while; this is the verb that makes it worth listing.
 * It is the only place this extension dials a socket that is not this workspace's — the request goes
 * to the other companion's own daemon, because that is whose work it becomes.
 */
export class HandOff implements vscode.Disposable {
  private readonly work: Handed[] = [];
  private timer: NodeJS.Timeout | null = null;
  private gone = false;
  private readonly changed = new vscode.EventEmitter<Handed[]>();
  readonly onChanged = this.changed.event;

  constructor(private readonly companion: Companion, private readonly me: string) {}

  /** What has been handed over, newest first, for a panel to draw. */
  get handed(): Handed[] { return this.work.slice().reverse(); }

  commands(): vscode.Disposable[] {
    return [
      vscode.commands.registerCommand('magi.handOff', () => void this.ask()),
    ];
  }

  private async ask(): Promise<void> {
    const caps = await this.companion.caps();
    if (!caps.has('roster')) {
      void vscode.window.showWarningMessage('magi: this companion cannot list the others on this machine.');
      return;
    }
    const list = peers(await this.companion.ask('roster')).filter((p) => p.socket !== this.companion.socket);
    if (!list.length) {
      void vscode.window.showInformationMessage('magi: no other companion is running on this machine.');
      return;
    }
    const pick = await vscode.window.showQuickPick(
      list.map((p) => ({
        label: peerLabel(p),
        description: [p.state, p.workdir].filter(Boolean).join(' · '),
        peer: p,
      })),
      { title: 'magi — ask which companion' },
    );
    if (!pick) return;
    const text = await vscode.window.showInputBox({
      title: `magi — ask ${pick.label}`,
      prompt: 'What should they do? They answer in their own transcript — there is no reply channel.',
      ignoreFocusOut: true,
    });
    if (!text) return;
    // Question or request, because the far side treats them differently — a question is answered
    // read-only, a request may change their workspace.
    const kind = await vscode.window.showQuickPick(
      [{ label: 'a request', looking: false, description: 'they may change their own workspace' },
       { label: 'a question', looking: true, description: 'they only look and answer' }],
      { title: 'magi — which is it' },
    );
    if (!kind) return;

    // Their socket, not ours: it becomes their work, on their daemon.
    const far = await Daemon.connect(pick.peer.socket).catch(() => null);
    if (!far) {
      void vscode.window.showWarningMessage(`magi: could not reach ${pick.label}.`);
      return;
    }
    try {
      const r = await far.exchange({ method: 'hand', name: dispatchLabel(this.me), text, looking: kind.looking });
      if (!r.ok || !r.out) {
        // A refusal is an answer — mid-turn, chaining refused. Said plainly rather than swallowed.
        void vscode.window.showWarningMessage(`magi: ${pick.label} did not take it — ${r.error ?? 'no reason given'}`);
        return;
      }
      this.work.push({ socket: pick.peer.socket, who: pick.label, receipt: r.out, asked: text });
      this.changed.fire(this.handed);
      void vscode.window.showInformationMessage(`magi: ${pick.label} took it (${r.out.slice(-6)}).`);
      this.watch();
    } finally { far.close(); }
  }

  /**
   * Poll the receipts.
   *
   * One connection per receipt, and only while something is unfinished — a finished one is drawn
   * from what was last learned rather than asked about again. Started on the first hand-off rather
   * than at activation: a window that never hands anything over should not hold a timer.
   */
  private watch(): void {
    if (this.timer || this.gone) return;
    const tick = async (): Promise<void> => {
      if (this.gone) return;
      const live = this.work.filter((w) => !w.over);
      if (!live.length) { this.timer = null; return; }
      for (const w of live) {
        const far = await Daemon.connect(w.socket).catch(() => null);
        const r = far ? await far.exchange({ method: 'hand-state', name: w.receipt }).catch(() => null) : null;
        far?.close();
        const { line, over } = handState(r);
        w.line = line;
        w.over = over;
      }
      this.changed.fire(this.handed);
      if (!this.gone) this.timer = setTimeout(() => void tick(), 3000);
    };
    this.timer = setTimeout(() => void tick(), 0);
  }

  dispose(): void {
    this.gone = true;
    if (this.timer) clearTimeout(this.timer);
    this.changed.dispose();
  }
}
