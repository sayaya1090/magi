import * as vscode from 'vscode';
import * as activity from '../core/activity';
import { Companion } from './workspace';

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
      jobs: jobs === null ? null : (jobs.out ?? ''),
      context: ctx === null ? null : (ctx.out ?? ''),
      fleet: fleet === null ? null : (fleet.out ?? ''),
      cron: cron === null ? null : (cron.out ?? ''),
    });
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
});
vs.postMessage({ kind: 'ready' });
</script></body></html>`;
  }
}
