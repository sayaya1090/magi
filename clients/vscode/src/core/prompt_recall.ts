/**
 * The composer's ↑/↓ — bring back something typed before.
 *
 * The list comes from the daemon's `prompts` door: what a person typed to this workspace's companion
 * in EVERY conversation, newest first, each text once — so it survives a switch and a reload
 * (2026-10-17, asked for in every client; this panel had none). Same rules as the JetBrains
 * `PromptRecall`: what was being typed is kept as a draft on the first ↑ and handed back at the
 * bottom; a sent line goes to the front at once, before the daemon is asked again.
 */
export class PromptRecall {
  private history: string[] = []; // newest first
  private at = -1; // -1 = the draft
  private draft = '';

  /** Swap in the daemon's list — but not mid-browse, so a refresh never moves the reader. */
  load(newestFirst: readonly string[]): void {
    if (this.at >= 0) return;
    this.history = [...new Set(newestFirst.filter((t) => t.trim() !== ''))];
  }

  /** ↑: one older, or undefined when there is none (leave the input alone). */
  older(current: string): string | undefined {
    if (this.at + 1 >= this.history.length) return undefined;
    if (this.at === -1) this.draft = current;
    this.at++;
    return this.history[this.at];
  }

  /** ↓: one newer, the draft at the bottom; undefined when not browsing. */
  newer(): string | undefined {
    if (this.at < 0) return undefined;
    this.at--;
    return this.at === -1 ? this.draft : this.history[this.at];
  }

  get browsing(): boolean { return this.at >= 0; }

  /** Sent — to the front, and the browse ends. */
  remember(sent: string): void {
    const t = sent.trim();
    if (t) this.history = [t, ...this.history.filter((h) => h !== t)];
    this.at = -1;
    this.draft = '';
  }
}

/** Whether a caret at `pos` sits on the first (↑) or last (↓) line of `text` — recall happens only there. */
export function onEdgeLine(text: string, pos: number, up: boolean): boolean {
  return up ? !text.slice(0, pos).includes('\n') : !text.slice(pos).includes('\n');
}
