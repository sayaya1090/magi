import * as vscode from 'vscode';
import { Daemon } from '../core/daemon';
import { Event } from '../core/protocol';
import { Row, rows, seat } from '../core/transcript';
import { touched, pendingAsk } from '../core/touched';
import { Ref, refText } from '../core/refs';
import { Edits } from './edits';
import { Companion } from './workspace';

/**
 * The conversation, in the panel.
 *
 * A webview because there is no native surface that draws a conversation, and the guidelines are
 * explicit that this is the only reason to reach for one: "webviews should only be used if you
 * absolutely need them." The plan panel is the other, and there is no third.
 *
 * Everything it draws is themeable — no colour is written here that VS Code did not give us. That
 * is a stated rule ("Ensure all elements in the view are themeable") and it is also the only way
 * this looks right in somebody's high-contrast theme.
 */
export class Chat implements vscode.WebviewViewProvider, vscode.Disposable {
  static readonly viewId = 'magi.chat';

  private view: vscode.WebviewView | null = null;
  private stream: Daemon | null = null;
  private events: Event[] = [];
  private refs: Ref[] = [];
  private sid = '';
  private readonly subs: vscode.Disposable[] = [];
  private readonly edits = new Edits();

  constructor(private readonly companion: Companion, private readonly extUri: vscode.Uri) {
    this.subs.push(companion.onChanged(() => this.post({ kind: 'state', state: companion.state })));
  }

  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view;
    view.webview.options = { enableScripts: true, localResourceRoots: [this.extUri] };
    view.webview.html = this.html(view.webview);
    this.subs.push(view.webview.onDidReceiveMessage((m) => this.fromView(m)));
    view.onDidDispose(() => { this.view = null; this.stream?.close(); this.stream = null; });
    void this.openStream();
  }

  /** Open the transcript stream and keep it open. */
  private async openStream(): Promise<void> {
    if (this.stream) return;
    const d = await this.companion.reach();
    if (!d) { this.post({ kind: 'state', state: this.companion.state }); return; }
    // Which conversation. Measured against a live daemon: `status` answers permission and
    // backend and NOT a session id, so asking it for one gets an empty string and the transcript
    // streams nothing — with no error, which is how this would have shipped unnoticed. `sessions`
    // is the door that knows, newest first.
    let sid = this.sid;
    if (!sid) {
      const list = await this.companion.ask('sessions');
      const first = (list?.sessions ?? [])[0] as { id?: string } | undefined;
      sid = first?.id ?? '';
      this.sid = sid;
    }
    if (!sid) {
      // Not an error. Type and the daemon opens one — so say that, rather than leaving an empty
      // panel that reads as broken.
      this.post({ kind: 'note', text: 'No conversation here yet. Type below and one starts.' });
      return;
    }
    // A dedicated connection: this one is turned into a stream and answers nothing else, so
    // sharing it with the status poll would make every poll wait behind a conversation.
    const s = await Daemon.connect(this.companion.socket).catch(() => null);
    if (!s) return;
    this.stream = s;
    s.whenClosed(() => { if (this.stream === s) this.stream = null; });
    this.events = [];
    s.stream({ method: 'transcript', session: sid }, (r) => {
      if (r.event) { this.events.push(r.event); this.draw(); }
      else if (r.error) this.post({ kind: 'note', text: r.error });
      else if (r.why) this.post({ kind: 'note', text: r.why });
    });
  }

  private draw(): void {
    this.post({
      kind: 'rows',
      rows: rows(this.events).map(paint),
      ask: pendingAsk(this.events),
      refs: this.refs.map(refText),
    });
    // What the companion changed on disk, so the editor is not showing yesterday's file next to a
    // row that says it was rewritten. Never over a dirty buffer — see Edits.
    void this.edits.refresh(touched(this.events));
  }

  /** Read another conversation. The daemon is the source, so this only changes which one we ask for. */
  showSession(sid: string): void {
    this.stream?.close();
    this.stream = null;
    this.events = [];
    this.sid = sid;
    void this.openStream();
  }

  /** Put references above the composer. The person still writes the message. */
  attach(refs: Ref[]): void {
    for (const r of refs) {
      if (!this.refs.some((x) => refText(x) === refText(r))) this.refs.push(r);
    }
    this.draw();
  }

  /** Seed the composer and leave the cursor after it — a lead, not a question. */
  compose(text: string): void { this.post({ kind: 'compose', text }); }

  /**
   * Which turn wrote this line, as far as this window knows.
   *
   * Only the transcript it has streamed. Saying "I do not know" is the honest answer for a line
   * written before this window opened, and it is better than a confident wrong turn.
   */
  whoWrote(path: string, line: number): string {
    const t = touched(this.events);
    if (!t.named.includes(path)) {
      return t.unnamed
        ? `magi: this window has not seen an edit naming ${path}. A command it ran may have written it.`
        : `magi: this window has not seen magi write ${path}.`;
    }
    const said = rows(this.events).filter((r) => r.who === 'user').slice(-1)[0];
    return said
      ? `magi wrote ${path} in this conversation. The request that turn was answering: "${said.text.split('\n')[0]}" (line ${line})`
      : `magi wrote ${path} in this conversation (line ${line}).`;
  }

  private async fromView(m: { kind: string; text?: string; callId?: string; decision?: string }): Promise<void> {
    switch (m.kind) {
      case 'ready':
        this.post({ kind: 'state', state: this.companion.state });
        this.draw();
        break;
      case 'say': {
        const body = (m.text ?? '').trim();
        if (!body) break;
        // The references ride with the message as text, because that is what the daemon takes: a
        // prompt. They are cleared once sent — a chip that outlived its message would attach the
        // same file to every later one.
        const lead = this.refs.length ? this.refs.map(refText).join('\n') + '\n\n' : '';
        this.refs = [];
        await this.companion.ask('submit', { text: lead + body });
        this.draw();
        break;
      }
      case 'start':
        await vscode.commands.executeCommand('magi.start');
        break;
      case 'drop':
        this.refs = [];
        this.draw();
        break;
      case 'answer':
        // The decision travels as the core spells it. Two vocabularies for one verdict is a place
        // for the two to drift.
        await this.companion.ask('permission', { callId: m.callId, decision: m.decision });
        break;
      case 'reply':
        // A QUESTION, not a permission. Its own door, because what it takes is a sentence and not
        // a verdict — sending "allow" to a question would answer something nobody asked.
        await this.companion.ask('answer', { callId: m.callId, answer: m.text ?? '' });
        break;
      case 'mention': {
        // The file list behind `@`. It comes from the companion's own glob rather than from this
        // window's idea of the workspace: the companion is what will read the file, and what it
        // can reach is the answer that matters.
        const r = await this.companion.ask('tool', { name: 'glob',
          args: { pattern: `**/*${(m.text ?? '').trim()}*` } });
        let files: string[] = [];
        try { files = JSON.parse(r?.out ?? '[]') as string[]; } catch { files = []; }
        this.post({ kind: 'mentions', files: files.slice(0, 20) });
        break;
      }
      case 'suggest': {
        // The composer's ghost text — the same door the console uses for its own.
        const r = await this.companion.ask('suggest', { text: m.text ?? '' });
        this.post({ kind: 'suggestion', text: r?.ok ? (r.out ?? '') : '' });
        break;
      }
      default:
        break;
    }
  }

  private post(msg: unknown): void { void this.view?.webview.postMessage(msg); }

  reveal(): void { void vscode.commands.executeCommand(`${Chat.viewId}.focus`); }

  dispose(): void {
    this.stream?.close();
    this.edits.dispose();
    for (const s of this.subs) s.dispose();
  }

  private html(w: vscode.Webview): string {
    const nonce = Math.random().toString(36).slice(2) + Date.now().toString(36);
    // A strict policy, and the nonce is why the script may run at all. A webview that allowed
    // inline script would be a place for whatever the model wrote to become code.
    const csp = `default-src 'none'; style-src ${w.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';`;
    return `<!DOCTYPE html><html><head>
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
  /* Every colour is the editor's. Nothing here picks one. */
  body { margin:0; font-family:var(--vscode-font-family); font-size:var(--vscode-font-size);
         color:var(--vscode-foreground); background:var(--vscode-panel-background);
         display:flex; flex-direction:column; height:100vh; }
  #rows { flex:1; overflow-y:auto; padding:8px 10px; }
  .row { margin:0 0 8px; white-space:pre-wrap; word-break:break-word; }
  .who { font-size:.85em; opacity:.7; margin-bottom:2px; }
  .user { border-left:2px solid var(--vscode-focusBorder); padding-left:8px; }
  .pending { opacity:.75; }
  .thinking, .tool { opacity:.75; font-family:var(--vscode-editor-font-family); font-size:.9em; }
  .error { color:var(--vscode-errorForeground); }
  .council { border-left:2px solid var(--vscode-textLink-foreground); padding-left:8px; }
  #note { padding:6px 10px; opacity:.8; font-size:.9em; }
  #ask { padding:8px 10px; border-top:1px solid var(--vscode-panel-border); }
  #ask .what { margin-bottom:6px; }
  #ask button { margin-right:6px; }
  #hint { padding:0 10px 4px; font-size:.85em; opacity:.7; font-family:var(--vscode-editor-font-family); }
  #refs { display:flex; flex-wrap:wrap; gap:4px; padding:0 10px 6px; }
  .chip { font-size:.85em; padding:1px 6px; border-radius:9px;
          color:var(--vscode-badge-foreground); background:var(--vscode-badge-background); }
  #bar { display:flex; gap:6px; align-items:center; padding:8px 10px;
         border-top:1px solid var(--vscode-panel-border); }
  #say { flex:1; resize:none; min-height:2.2em; max-height:8em;
         font-family:inherit; font-size:inherit;
         color:var(--vscode-input-foreground); background:var(--vscode-input-background);
         border:1px solid var(--vscode-input-border, transparent); border-radius:2px; padding:4px 6px; }
  button { color:var(--vscode-button-foreground); background:var(--vscode-button-background);
           border:none; border-radius:2px; padding:4px 10px; cursor:pointer; }
  button:hover { background:var(--vscode-button-hoverBackground); }
</style></head><body>
<div id="rows"></div><div id="ask" hidden></div><div id="note"></div><div id="refs"></div>
<div id="hint"></div>
<div id="bar"><textarea id="say" rows="1" aria-label="Message the companion"></textarea><button id="send">Send</button></div>
<script nonce="${nonce}">
const vs = acquireVsCodeApi();
const rowsEl = document.getElementById('rows');
const noteEl = document.getElementById('note');
const say = document.getElementById('say');
const askEl = document.getElementById('ask');
const refsEl = document.getElementById('refs');
const hint = document.getElementById('hint');
let suggestion = '';
let typing = null;
function drawAsk(a) {
  askEl.textContent = '';
  askEl.hidden = !a;
  if (!a) return;
  const w = document.createElement('div');
  w.className = 'what';
  askEl.append(w);
  if (a.kind === 'permission') {
    w.textContent = 'magi wants to run: ' + a.what;
    /* The three words the core spells. One vocabulary, so the two cannot drift. */
    for (const d of ['allow', 'deny', 'always']) {
      const b = document.createElement('button');
      b.textContent = d;
      b.addEventListener('click', () => vs.postMessage({ kind: 'answer', callId: a.callId, decision: d }));
      askEl.append(b);
    }
    return;
  }
  /* A question wants a sentence, not a verdict. Options are shortcuts to one. */
  w.textContent = a.what;
  for (const opt of a.options || []) {
    const b = document.createElement('button');
    b.textContent = opt;
    b.addEventListener('click', () => vs.postMessage({ kind: 'reply', callId: a.callId, text: opt }));
    askEl.append(b);
  }
  const free = document.createElement('button');
  free.textContent = 'answer in the box';
  free.addEventListener('click', () => { pendingQuestion = a.callId; say.focus(); });
  askEl.append(free);
}
let pendingQuestion = null;
let mentions = [];
function drawState(st) {
  noteEl.textContent = '';
  if (!st) return;
  if (st.state === 'not-running') {
    /* Not just the fact — the way out. A line saying nothing is listening, with nothing to press,
       leaves somebody to find the command palette to learn what to do next. */
    noteEl.append('No companion is running for this workspace. ');
    const b = document.createElement('button');
    b.textContent = 'Start one';
    b.addEventListener('click', () => vs.postMessage({ kind: 'start' }));
    noteEl.append(b);
  } else if (st.state === 'unknown') {
    noteEl.textContent = 'Could not reach the companion. ' + (st.asking || '');
  }
  /* idle / working / waiting say nothing here: the status bar already says them, and repeating a
     line above the composer is a line in the way. */
}
function drawRefs(rs) {
  refsEl.textContent = '';
  for (const r of rs || []) {
    const c = document.createElement('span');
    c.className = 'chip';
    c.textContent = r;
    refsEl.append(c);
  }
  if ((rs || []).length) {
    const b = document.createElement('button');
    b.textContent = 'clear';
    b.addEventListener('click', () => vs.postMessage({ kind: 'drop' }));
    refsEl.append(b);
  }
}
function draw(rs) {
  /* Only scroll if they were already at the bottom. Yanking somebody back down while they read
     an older row is the single most annoying thing a live transcript does. */
  const wasAtBottom = rowsEl.scrollHeight - rowsEl.scrollTop - rowsEl.clientHeight < 40;
  rowsEl.textContent = '';
  for (const r of rs) {
    const d = document.createElement('div');
    d.className = 'row ' + r.who + (r.pending ? ' pending' : '');
    const w = document.createElement('div');
    w.className = 'who';
    w.textContent = r.label;
    const b = document.createElement('div');
    b.textContent = r.text;           /* textContent, never innerHTML: the model wrote this */
    d.append(w, b);
    rowsEl.append(d);
  }
  if (wasAtBottom) rowsEl.scrollTop = rowsEl.scrollHeight;
}
window.addEventListener('message', (e) => {
  const m = e.data;
  if (m.kind === 'rows') {
    draw(m.rows); drawAsk(m.ask); drawRefs(m.refs);
    if (noteEl.textContent === 'sending…') noteEl.textContent = '';
  }
  else if (m.kind === 'compose') { say.value = m.text || ''; say.focus();
    say.setSelectionRange(say.value.length, say.value.length); }
  else if (m.kind === 'mentions') {
    mentions = m.files || [];
    hint.textContent = mentions.length ? 'files: ' + mentions.slice(0, 6).join('  ') : '';
  }
  else if (m.kind === 'suggestion') {
    /* Ghost text for the composer. Tab takes it — the same key the terminal uses. */
    suggestion = m.text || '';
    hint.textContent = suggestion ? 'Tab: ' + suggestion.split('\n')[0].slice(0, 60) : '';
  }
  else if (m.kind === 'state') drawState(m.state);
  else if (m.kind === 'note') noteEl.textContent = m.text || '';
});
function send() {
  const t = say.value.trim();
  if (!t) return;
  /* If a question is open and they chose to type, the box answers THAT rather than starting a new
     turn — otherwise their sentence goes somewhere nobody was waiting for it. */
  if (pendingQuestion) {
    vs.postMessage({ kind: 'reply', callId: pendingQuestion, text: t });
    pendingQuestion = null;
  } else {
    vs.postMessage({ kind: 'say', text: t });
  }
  say.value = '';
  hint.textContent = '';
  /* The row for this arrives on the stream a moment later. Until then the box being empty is the
     only sign anything happened, and on a slow first turn that reads as a lost message. */
  noteEl.textContent = 'sending…';
  setTimeout(() => { if (noteEl.textContent === 'sending…') noteEl.textContent = ''; }, 4000);
}
document.getElementById('send').addEventListener('click', send);
/* Enter sends, Shift+Enter is a newline — the terminal and the web console both do this. */
say.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); return; }
  if (e.key === 'Tab' && suggestion) {
    e.preventDefault();
    say.value += suggestion;
    suggestion = '';
    hint.textContent = '';
  }
});
say.addEventListener('input', () => {
  suggestion = '';
  if (typing) clearTimeout(typing);
  const v = say.value;
  /* An @name at the start of a word asks the companion which files match. Two characters at
     least, because one matches everything and the list would be the whole workspace.
     (No backticks in here: this script lives in a template literal and one would close it.) */
  const at = /(^|\s)@([^\s@]{2,})$/.exec(v);
  typing = setTimeout(() => {
    if (at) vs.postMessage({ kind: 'mention', text: at[2] });
    else if (v.trim().length > 3) vs.postMessage({ kind: 'suggest', text: v });
    else hint.textContent = '';
  }, 450);
});
vs.postMessage({ kind: 'ready' });
</script></body></html>`;
  }
}

/** The one place a row gets its visible label, so two screens cannot spell it differently. */
function paint(r: Row): Row & { label: string } {
  const who = r.who === 'council' && seat(r.member) ? r.member!.toLowerCase() : r.who;
  const mark = r.who === 'tool' && r.ok !== undefined ? (r.ok ? ' ✓' : ' ✗') : '';
  return { ...r, label: who + mark };
}
