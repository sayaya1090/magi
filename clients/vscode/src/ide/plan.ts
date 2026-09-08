import * as vscode from 'vscode';
import * as activity from '../core/activity';
import { Companion } from './workspace';
import { context as contextOf, fleet as fleetOf, jobs as jobsOf, schedules } from '../core/panel';

/**
 * The plan and the dials, in the sidebar.
 *
 * The JetBrains client folded these into its settings screen, by a decision its manual records.
 * That option is closed here: VS Code says ❌ "Create your own settings page/webview", so the
 * facts go back to a panel of their own — which is where they were before that fold.
 *
 * ⚠ This panel is often collapsed. Anything a person must not miss (a permission waiting on them)
 * is NOT only here; the status bar says it too.
 */
export class Plan implements vscode.WebviewViewProvider, vscode.Disposable {
  static readonly viewId = 'magi.plan';

  private view: vscode.WebviewView | null = null;
  private timer: NodeJS.Timeout | null = null;
  private readonly subs: vscode.Disposable[] = [];

  constructor(private readonly companion: Companion) {
    this.subs.push(companion.onChanged(() => void this.refresh()));
  }

  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view;
    view.webview.options = { enableScripts: true };
    view.webview.html = this.html(view.webview);
    this.subs.push(view.webview.onDidReceiveMessage((m: { kind: string }) => {
      if (m.kind === 'ready') void this.refresh();
    }));
    view.onDidDispose(() => { this.view = null; if (this.timer) clearTimeout(this.timer); });
    const tick = () => { void this.refresh(); this.timer = setTimeout(tick, 5000); };
    tick();
  }

  private async refresh(): Promise<void> {
    if (!this.view) return;
    // Each of these is a door the daemon may or may not have. `about` says which, and a door that
    // is not advertised is not called — reading a refusal to find out is how a client learns to
    // draw an empty panel for an old build.
    const caps = await this.companion.caps();
    const [jobs, ctx, fleet, cron] = await Promise.all([
      caps.has('job-kill') ? this.companion.ask('jobs') : null,
      caps.has('context') ? this.companion.ask('context') : null,
      caps.has('roster') ? this.companion.ask('roster') : null,
      caps.has('cron') ? this.companion.ask('cron') : null,
    ]);
    this.view.webview.postMessage({
      kind: 'plan',
      state: activity.label(this.companion.state),
      // "not asked" and "asked and empty" are different facts, and the panel says which.
      // ⚠ From the STRUCTURED fields. These four never fill `out` — measured against a live daemon —
      // and reading `out` drew three empty sections on every build, without failing: an absent field
      // is an empty string, and an empty string is what "nothing to report" looks like.
      jobs: jobs === null ? null : jobLines(jobs),
      context: ctx === null ? null : contextOf(ctx),
      fleet: fleet === null ? null : fleetOf(fleet).join('\n'),
      cron: cron === null ? null : schedules(cron).map((r) => r.line).join('\n'),
      handed: this.handed,
    });
  }

  private handed = '';

  /**
   * What has been handed to other companions.
   *
   * Held here rather than asked for: the receipts belong to this window (the far side knows them by
   * number, not by who asked), so if this panel had to fetch them there would be nowhere to fetch
   * them from.
   */
  showHanded(work: { who: string; asked: string; line?: string }[]): void {
    this.handed = work
      .map((w) => `${w.who} · ${w.asked.split('\n')[0].slice(0, 50)} → ${w.line ?? 'asked'}`)
      .join('\n');
    void this.refresh();
  }

  dispose(): void {
    if (this.timer) clearTimeout(this.timer);
    for (const s of this.subs) s.dispose();
  }

  private html(w: vscode.Webview): string {
    const nonce = Math.random().toString(36).slice(2) + Date.now().toString(36);
    const csp = `default-src 'none'; style-src ${w.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';`;
    return `<!DOCTYPE html><html><head>
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
  body { margin:0; padding:8px 10px; font-family:var(--vscode-font-family);
         font-size:var(--vscode-font-size); color:var(--vscode-foreground); }
  h3 { font-size:.85em; text-transform:uppercase; letter-spacing:.04em; opacity:.7;
       margin:14px 0 4px; font-weight:600; }
  h3:first-child { margin-top:0; }
  pre { margin:0; white-space:pre-wrap; word-break:break-word;
        font-family:var(--vscode-editor-font-family); font-size:.9em; }
  .none { opacity:.6; font-style:italic; }
</style></head><body><div id="body"></div>
<script nonce="${nonce}">
const vs = acquireVsCodeApi();
const body = document.getElementById('body');
function section(title, text) {
  const h = document.createElement('h3'); h.textContent = title;
  const p = document.createElement('pre');
  if (text === null) { p.className = 'none'; p.textContent = 'this companion does not answer that'; }
  else if (!String(text).trim()) { p.className = 'none'; p.textContent = 'nothing'; }
  else p.textContent = text;
  body.append(h, p);
}
window.addEventListener('message', (e) => {
  const m = e.data;
  if (m.kind !== 'plan') return;
  body.textContent = '';
  section('now', m.state);
  section('context', m.context);
  section('jobs', m.jobs);
  section('scheduled', m.cron);
  section('fleet', m.fleet);
  section('handed over', m.handed);
});
vs.postMessage({ kind: 'ready' });
</script></body></html>`;
  }
}

/**
 * The jobs section: what is running beside the turn, and what runs next.
 *
 * Queued work is drawn with it rather than in a section of its own — a person asking "what else is
 * it doing" means both, and two sections that are usually empty read as a broken panel.
 */
function jobLines(resp: Parameters<typeof jobsOf>[0]): string {
  const { queued, jobs: running } = jobsOf(resp);
  return [
    ...running.map((j) => `${j.id} · ${j.what}`),
    ...queued.map((q) => `next — ${q}`),
  ].join('\n');
}
