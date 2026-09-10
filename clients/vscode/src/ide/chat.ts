import * as vscode from 'vscode';
import { Daemon, retryAfter } from '../core/daemon';
import { Event } from '../core/protocol';
import { Row, rows, seat, todos, turnOpen, verdictWord } from '../core/transcript';
import { touched, pendingAsk } from '../core/touched';
import { panelNote, label as activityLabel } from '../core/activity';
import { usage } from '../core/panel';
import { Ref, refText, wireRef } from '../core/refs';
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
    this.subs.push(companion.onChanged(() => this.post({ kind: 'state', state: companion.state, note: panelNote(companion.state) })));
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
    // The number is taken BEFORE the first wait, not after: an attempt that cannot tell it was
    // overtaken during its first await is not guarded, it is guarded later. A guard added to some
    // of a function's waits is the shape that reads as covered and is not.
    const mine = ++this.opening;
    const d = await this.companion.reach();
    if (mine !== this.opening) return;   // a newer attempt is already under way — see `opening`
    if (!d) { this.post({ kind: 'state', state: this.companion.state, note: panelNote(this.companion.state) }); return; }
    // Which conversation. Measured against a live daemon: `status` answers permission and
    // backend and NOT a session id, so asking it for one gets an empty string and the transcript
    // streams nothing — with no error, which is how this would have shipped unnoticed. `sessions`
    // is the door that knows, newest first.
    let sid = this.sid;
    if (!sid) {
      const list = await this.companion.ask('sessions');
      if (mine !== this.opening) return;   // somebody asked for another conversation while we waited
      const first = (list?.sessions ?? [])[0];
      sid = first?.id ?? '';
      this.sid = sid;
      // The status poll needs it too: the model is only in a reply that names a conversation.
      this.companion.session = sid;
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
    // A newer attempt started while this one was connecting. Hand this socket back rather than
    // letting two streams feed one transcript — see `opening`.
    if (mine !== this.opening) { s.close(); return; }
    this.stream = s;
    s.whenClosed(() => {
      if (this.stream !== s) return;   // we moved on, or the panel closed — not an ending to report
      this.stream = null;
      // ⚠ **A stream that ends is not a conversation that ended.** The daemon restarts often — a
      // self-update, a crash, somebody stopping it — and until now this window just stopped
      // receiving: no new rows, no word, and a panel that looks like a companion with nothing to
      // say. The JetBrains client tells the three endings apart and reattaches on two of them.
      this.post({ kind: 'note', text: 'lost the conversation — reconnecting…' });
      void this.reattach();
    });
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
      rows: rows(this.events).map((r) => paint(r, this.companion.you)),
      ask: pendingAsk(this.events),
      refs: this.refs.map(refText),
    });
    // What the companion changed on disk, so the editor is not showing yesterday's file next to a
    // row that says it was rewritten. Never over a dirty buffer — see Edits.
    void this.edits.refresh(touched(this.events));
    // The plan rides this same stream. A second connection for it would be a second reader of one
    // fact, and the panel would disagree with the conversation for as long as they were out of step.
    this.onPlan?.(todos(this.events));
    // The context meter rides the same stream. The door for it is a capability a daemon may not
    // have, and this number arrives every turn regardless.
    this.onUsage?.(usage(this.events));
    this.tellInfo();
  }

  /**
   * The facts about the companion itself, for the info card.
   *
   * Gathered here rather than asked for again: every one of these already arrives on the status
   * poll (`Setup`) or on the handshake (`about`). A card that re-asked would be a second reader of
   * one fact and would disagree with the status bar for as long as they were out of step.
   *
   * `state` travels as the WORD, so the card can colour by it the way the console does — the state
   * is the class name there, and the colour is the stylesheet's business, not this file's.
   */
  private tellInfo(): void {
    const s = this.companion.facts;
    this.post({
      kind: 'info',
      state: this.companion.state.state,
      label: activityLabel(this.companion.state),
      version: this.companion.version,
      model: s.model,
      backend: s.backend,
      permission: s.permission,
      council: s.council,
      socket: this.companion.socket,
    });
  }

  /**
   * Which attempt to open a stream is the current one.
   *
   * ⚠ **`openStream` waits twice** — for the session list, then for the connection — and until now
   * it came back and used `this.sid` as if nothing could have happened meanwhile. Two things can:
   * a person picks another conversation, and the reattach timer calls in on its own. Then the
   * slower attempt lands last and the panel streams a conversation nobody asked for, while
   * `this.sid` names a different one — and BOTH streams push into `this.events`, so two
   * conversations interleave in one transcript.
   *
   * The reattach loop is what made this ordinary: before it, opening twice needed a person doing
   * two things quickly. Same guard as the status poll's, one level up.
   */
  private opening = 0;

  /** Read another conversation. The daemon is the source, so this only changes which one we ask for. */
  showSession(sid: string): void {
    this.stream?.close();
    this.stream = null;
    this.events = [];
    this.sid = sid;
    this.companion.session = sid;
    void this.openStream();
  }

  /** Told the plan whenever the stream moves. Set by the extension, which owns both views. */
  onPlan: ((list: { content: string; status: string }[]) => void) | null = null;
  /** Told how full the window is, from the stream rather than the door. */
  onUsage: ((line: string) => void) | null = null;

  /** Which conversation this panel is on, for the doors that act on one. */
  get session(): string { return this.sid; }

  /**
   * The person's own turns.
   *
   * Handed out as ROWS rather than as the raw log, because "which of my prompts" is a question
   * about the conversation as it is drawn — and the rule that turns events into rows lives in one
   * place (invariant 0-1). A caller that filtered the log itself would be the second copy.
   */
  userRows(): Row[] { return rows(this.events).filter((r) => r.who === 'user'); }

  /**
   * Read the conversation again from the daemon.
   *
   * After something rewrote it. The events held here are a copy of a stream, and a rewind leaves
   * that copy describing a conversation that no longer exists — redrawing from it would show the
   * dropped turns until the next restart.
   */
  reload(): void { this.showSession(this.sid); }

  /**
   * Get the stream back after the far side went away.
   *
   * Backs off rather than spinning: a daemon that is down stays down for a while, and a retry loop
   * with no wait turns one restart into a busy panel. Says it is trying, because the backoff grows
   * to half a minute and without a word the last failure would stand as "the current state" for
   * that whole time — the sibling's reason, in its own comment.
   *
   * Stops when the panel closes or when somebody else has already attached (`this.stream`), so a
   * person who picks another conversation is not dragged back to this one.
   */
  private async reattach(): Promise<void> {
    for (let attempt = 0; this.view && !this.stream; attempt++) {
      // The schedule is a rule and lives in core, where a test can run it — see `retryAfter`.
      await new Promise((r) => setTimeout(r, retryAfter(attempt)));
      if (!this.view || this.stream) return;
      this.post({ kind: 'note', text: 'reconnecting…' });
      this.showSession(this.sid);
      // showSession is async inside; give it a moment to land before deciding to wait again.
      await new Promise((r) => setTimeout(r, 200));
      if (this.stream) { this.post({ kind: 'note', text: '' }); return; }
    }
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
   * A send that did not land: say why, and give the person their words back.
   *
   * The composer empties itself the moment Enter is pressed, on purpose — the row for the message
   * arrives on the stream a moment later, and until then the empty box is the only sign anything
   * happened. That default is right, and it is exactly why a refusal cannot be dropped here: the
   * sentence is already off the screen, so saying nothing reads as "sent". A daemon that went away
   * mid-typing, a conversation that ended, a companion that moved — all of them answer, and this
   * window threw the answer away.
   *
   * The JetBrains client clears its box only after `ok`, and its comment names the same trap for
   * the chips: the core promises that no attachment vanishes, and the client was the place that
   * promise broke. So the chips come back too — they were cleared so one could not outlive its
   * message, and a message that never went has nothing to outlive.
   */
  private giveBack(text: string, refs: Ref[], why: string): void {
    for (const r of refs) {
      if (!this.refs.some((x) => refText(x) === refText(r))) this.refs.push(r);
    }
    this.post({ kind: 'note', text: `not sent — ${why}` });
    this.compose(text);
  }

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

  private async fromView(m: { kind: string; text?: string; callId?: string; decision?: string; command?: string }): Promise<void> {
    switch (m.kind) {
      case 'ready':
        this.post({ kind: 'state', state: this.companion.state, note: panelNote(this.companion.state) });
        this.draw();
        break;
      case 'say': {
        const body = (m.text ?? '').trim();
        if (!body) break;
        // The references ride as `refs`, the field the door reads — NOT spliced into the person's
        // words. The core renders each excerpt inside the workspace jail, caps it, and persists it
        // with the prompt, so the transcript shows what the agent was actually shown. Cleared once
        // sent — a chip that outlived its message would attach the same file to every later one.
        const sent = this.refs;
        const refs = sent.map(wireRef);
        this.refs = [];
        // Which door: `steer` while a turn is running, `submit` otherwise. Not one door with two
        // names — `submit` is a new top-level request and the core wipes the plan for it, so a
        // clarification typed mid-turn would delete the plan of the turn it was clarifying.
        // The fact comes off the transcript this window streams, not from `status` (see turnOpen).
        const door = turnOpen(this.events) ? 'steer' : 'submit';
        const r = await this.companion.ask(door, refs.length ? { text: body, refs } : { text: body });
        if (!r?.ok) this.giveBack(body, sent, r?.error ?? 'no companion is listening on this workspace.');
        this.draw();
        break;
      }
      case 'start':
        await vscode.commands.executeCommand('magi.start');
        break;
      /**
       * One of the editor's own commands, asked for by the info card.
       *
       * ⚠ **The name is checked here, not trusted from the page.** A webview is a page; letting it
       * name any command would let anything that got script into it run whatever the extension host
       * can. So the card may ask for these six and nothing else, and each of them is a thing the
       * card actually offers.
       */
      case 'run': {
        const allowed = new Set(['magi.chooseModel', 'magi.chooseBackend', 'magi.choosePermission',
          'magi.compact', 'magi.restartDaemon', 'magi.updateCore']);
        const name = String(m.command ?? '');
        if (!allowed.has(name)) break;
        await vscode.commands.executeCommand(name);
        break;
      }
      case 'drop':
        this.refs = [];
        this.draw();
        break;
      case 'answer': {
        // The decision travels as the core spells it. Two vocabularies for one verdict is a place
        // for the two to drift.
        const v = await this.companion.ask('permission', { callId: m.callId, decision: m.decision });
        // A pressed button whose answer is thrown away is a window where nothing happens when you
        // press it — the JetBrains client's own words for the same defect, which it fixed in the
        // one place all four of its buttons go through. The prompt is redrawn from the stream, so
        // without this the only thing a refusal changes on screen is nothing.
        if (!v?.ok) this.post({ kind: 'note', text: `not sent — ${v?.error ?? 'no companion is listening on this workspace.'}` });
        break;
      }
      case 'reply': {
        // A QUESTION, not a permission. Its own door, because what it takes is a sentence and not
        // a verdict — sending "allow" to a question would answer something nobody asked.
        const said = m.text ?? '';
        const a = await this.companion.ask('answer', { callId: m.callId, answer: said });
        // Same box, same rule: it emptied itself before the round trip, so a refusal has to put
        // the words back or the answer they typed is gone with nothing said.
        if (!a?.ok) this.giveBack(said, [], a?.error ?? 'no companion is listening on this workspace.');
        break;
      }
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
        // ⚠ This asks the model every time typing pauses, so it is switchable like the other two
        // typing-time doors (`magi.complete`, `magi.lookWhileTyping`). The JetBrains plugin and the
        // web console have carried that switch all along; this client fired the door unconditionally.
        // The default is on, which is what this client did before and what the other two default to.
        if (!vscode.workspace.getConfiguration('magi').get<boolean>('suggest', true)) {
          this.post({ kind: 'suggestion', text: '' });
          break;
        }
        const r = await this.companion.ask('suggest', { text: m.text ?? '' });
        this.post({ kind: 'suggestion', text: r?.ok ? (r.out ?? '') : '' });
        break;
      }
      default:
        break;
    }
  }

  private post(msg: unknown): void { void this.view?.webview.postMessage(msg); }

  /**
   * Open the conversation.
   *
   * `preserveFocus` opens it WITHOUT taking the keyboard, which is the difference between a person
   * pressing a command and this happening on its own at startup. The generated `<view>.focus`
   * command takes it as an option and passes `!preserveFocus` to openView, so the same command
   * serves both — a hand that pressed something wants to type in it, and a window that just opened
   * does not want the cursor pulled out of the editor.
   */
  reveal(preserveFocus = false): void {
    void vscode.commands.executeCommand(`${Chat.viewId}.focus`, { preserveFocus });
  }

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
  /* Parked, not being worked on. Its own mark because "asked and waiting" and "shelved until
     this turn ends" draw the same bar otherwise, and a person cannot tell which they typed. */
  .queued .who::after { content:' ⏸'; }
  .abandoned { opacity:.6; text-decoration:line-through; }
  .thinking, .tool { opacity:.75; font-family:var(--vscode-editor-font-family); font-size:.9em; }
  .error { color:var(--vscode-errorForeground); }
  /* A note about the conversation, not something the companion said — a fold, a recovered error.
     It had no rule at all, so it read as the agent's own words with a small label beside it. The
     JetBrains client draws the same rows small, italic and faint; this is that, in this editor's
     tokens. */
  .system { color:var(--vscode-descriptionForeground); font-style:italic; font-size:.9em; }
  /* What a tool was asked to do, beside its name. Dimmer than the name and clipped to one line:
     it is the answer to "which one", not the argument's full text. */
  .args { color:var(--vscode-descriptionForeground); opacity:.85; }
  /* The companion itself, folded away. A gear rather than "…": the card is what this companion is
     RUNNING ON and what you can change about it, and a gear is the word every editor already uses
     for that — "…" says "more of the same", which this is not. */
  #topbar { display:flex; justify-content:flex-end; padding:2px 6px 0; }
  #more { background:none; border:none; cursor:pointer; font-size:1.05em; line-height:1;
    color:var(--vscode-descriptionForeground); padding:2px 4px; }
  #more:hover, #more[aria-expanded="true"] { color:var(--vscode-foreground); }
  #info { border:1px solid var(--vscode-panel-border); border-radius:4px; margin:4px 6px;
    padding:6px 8px; font-size:.9em; }
  #info .line { display:flex; align-items:center; gap:6px; margin:3px 0; }
  #info .k { color:var(--vscode-descriptionForeground); min-width:5.5em; }
  #info .v { flex:1; }
  /* The traffic light. The state IS the class, the way the console does it — the colour is this
     sheet's business and the word travels alone. */
  #info .dot { width:.7em; height:.7em; border-radius:50%; flex:none;
    background:var(--vscode-descriptionForeground); }
  #info .working .dot { background:var(--vscode-testing-iconPassed); }
  #info .waiting .dot { background:var(--vscode-editorWarning-foreground); }
  #info .attached .dot { background:var(--vscode-textLink-foreground); }
  #info .not-running .dot, #info .unknown .dot { background:var(--vscode-editorError-foreground); }
  #info button { font-size:.95em; }
  #info .acts { display:flex; flex-wrap:wrap; gap:4px; margin-top:6px; }
  /* The subject of a permission. Monospace and scrollable: it is a command or a patch, and a
     wrapped one is a different command to read. */
  #ask pre { font-family:var(--vscode-editor-font-family); font-size:.9em; margin:4px 0;
    max-height:12em; overflow:auto; white-space:pre-wrap; }
  #ask pre.diff { border-left:2px solid var(--vscode-textLink-foreground); padding-left:6px; }
  #ask .unstated { color:var(--vscode-editorWarning-foreground); font-size:.9em; margin:4px 0; }
  /* Which of how many. Dimmer than the question — it places it, it is not it. */
  #ask .at { color:var(--vscode-descriptionForeground); }
  /* What the question was asked on. Denser than the question and above the buttons —
     it is what the decision is made FROM, so it must be read before they are pressed. */
  #ask .ground { font-size:.9em; margin:2px 0; }
  #ask .ground b { color:var(--vscode-descriptionForeground); font-weight:600; }
  /* A failure's own words. Its colour is the editor's error colour — the same meaning the glyph
     carries, so the two cannot say different things. */
  .out { color:var(--vscode-errorForeground); font-size:.9em; white-space:pre-wrap; margin-top:2px; }
  /* What a verdict stands on, and what it says to keep — two different kinds of text.
     ⚠ No backticks in this block: it is inside a template literal and one closes it.
     The cite is a FRAGMENT OF THE RECORD: measured against a live run, nine of twelve were diffs,
     leading minus/plus/space and all. This file states the rule for that a few lines up —
     "Monospace and scrollable: it is a command or a patch, and a wrapped one is a different
     command to read" — and it applies here for the same reason: the core keeps this checkable
     (magi looks the fragment up in what the member was shown), and a reader can only check what
     is drawn as it is. Capped, because one member's evidence must not push the round off screen.
     The keep is the member's own prose, so it stays in the reading font. */
  .cite { font-family:var(--vscode-editor-font-family); font-size:.9em; opacity:.75; margin-top:2px;
    max-height:9em; overflow:auto; }
  .keep { font-size:.9em; opacity:.75; margin-top:2px; }
  /* An image row carries a path, not the picture — the same font as a tool row, because that is
     what it is: something a tool produced, with a place to find it. */
  .image { opacity:.75; font-family:var(--vscode-editor-font-family); font-size:.9em; }
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
<div id="topbar"><button id="more" title="This companion" aria-label="This companion" aria-expanded="false">⚙</button></div>
<div id="info" hidden></div>
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
function askedAt(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const clock = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const now = new Date();
  const sameDay = d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth()
    && d.getDate() === now.getDate();
  return sameDay ? clock : d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + clock;
}
function drawAsk(a) {
  askEl.textContent = '';
  askEl.hidden = !a;
  if (!a) return;
  const w = document.createElement('div');
  w.className = 'what';
  askEl.append(w);
  /* Where this sits in the run the call is asking: (3/5). The core says why it travels — a viewer
     "has no other way to know that answering this one leads to another" — and without it somebody
     who answers the first question of five believes they are done. Only when there IS more than
     one: "(1/1)" beside a lone question is noise pretending to be information. */
  if (a.total > 1) {
    const n = document.createElement('span');
    n.className = 'at';
    n.textContent = ' (' + a.index + '/' + a.total + ')';
    w.append(n);
  }
  /* WHEN it was asked. A prompt that went up forty minutes ago while nobody was looking is drawn
     exactly like one you just caused, and those are different situations — the first means a turn
     has been stopped dead since before you stepped away. Drawn as a CLOCK, not as "40m ago":
     nothing redraws this panel while a prompt stands (no events arrive), so an elapsed figure would
     freeze at whatever it said when it was first painted and then quietly lie. The date comes along
     when it is not today, or "14:32" on a prompt from yesterday reads as an hour ago. */
  const when = askedAt(a.since);
  if (when) {
    const t = document.createElement('span');
    t.className = 'at';
    t.textContent = ' asked ' + when;
    w.append(t);
  }
  if (a.kind === 'permission') {
    w.prepend('magi wants to run: ' + a.what);
    /* WHAT is being allowed, not a description of it. Without this a person presses allow knowing
       only the tool's name — the place where the most is riding on the answer was the one drawn
       with the least. The args are the thing itself; the reason is prose about why the policy
       stopped here; the diff is what approving would change. */
    for (const [cls, text] of [['args', a.args], ['reason', a.reason], ['diff', a.diff]]) {
      if (!text) continue;
      const p = document.createElement('pre');
      p.className = cls;
      p.textContent = text;      /* textContent, never innerHTML: this is workspace input */
      askEl.append(p);
    }
    /* Nothing came. Say so — three buttons over a blank space read as "there is nothing to it",
       and that is the reading this must not allow. */
    if (!a.args && !a.reason && !a.diff) {
      const u = document.createElement('div');
      u.className = 'unstated';
      u.textContent = 'the companion did not say what this would do';
      askEl.append(u);
    }
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
  w.prepend(a.what);
  /* The grounds it was asked on. The decision-report skill gathers these and asks in their order,
     and a prompt whose grounds stayed behind is exactly what carrying them exists to stop — the
     person would be deciding from a bare sentence while the reasons sat in another process. */
  for (const g of a.report || []) {
    const row = document.createElement('div');
    row.className = 'ground';
    const k = document.createElement('b');
    k.textContent = g.key + ': ';
    row.append(k, g.text);   /* text as a node, never innerHTML: the model wrote it */
    askEl.append(row);
  }
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
const moreEl = document.getElementById('more');
const infoEl = document.getElementById('info');
let info = null;
moreEl.addEventListener('click', () => {
  const open = infoEl.hidden;
  infoEl.hidden = !open;
  moreEl.setAttribute('aria-expanded', open ? 'true' : 'false');
  if (open) drawInfo();
});
/* Run one of the editor's own commands. The NAME is chosen here and checked on the other side —
   a webview is a page and a page must not be able to name any command it likes. */
function act(command) { vs.postMessage({ kind: 'run', command: command }); }
function line(k, v, cls) {
  const d = document.createElement('div');
  d.className = 'line' + (cls ? ' ' + cls : '');
  if (cls) { const dot = document.createElement('span'); dot.className = 'dot'; d.append(dot); }
  const kk = document.createElement('span'); kk.className = 'k'; kk.textContent = k;
  const vv = document.createElement('span'); vv.className = 'v'; vv.textContent = v || 'not said';
  d.append(kk, vv);
  return d;
}
function drawInfo() {
  if (infoEl.hidden) return;
  infoEl.textContent = '';
  if (!info) { infoEl.append(line('state', 'asking…')); return; }
  /* The state word IS the class — the stylesheet paints the light, this only says which. */
  infoEl.append(line('now', info.label, info.state));
  infoEl.append(line('build', info.version));
  for (const [k, v, cmd] of [['model', info.model, 'magi.chooseModel'],
                             ['provider', info.backend, 'magi.chooseBackend'],
                             ['approval', info.permission, 'magi.choosePermission']]) {
    const row = line(k, v);
    const b = document.createElement('button');
    b.textContent = 'change';
    b.addEventListener('click', () => act(cmd));
    row.append(b);
    infoEl.append(row);
  }
  if (info.council) infoEl.append(line('council', info.council));
  const acts = document.createElement('div');
  acts.className = 'acts';
  for (const [text, cmd] of [['fold context', 'magi.compact'],
                             ['restart', 'magi.restartDaemon'],
                             ['update', 'magi.updateCore']]) {
    const b = document.createElement('button');
    b.textContent = text;
    b.addEventListener('click', () => act(cmd));
    acts.append(b);
  }
  infoEl.append(acts);
}
let pendingQuestion = null;
let mentions = [];
function drawState(note) {
  noteEl.textContent = '';
  if (!note || !note.text) return;
  /* The words and whether to offer a way out are decided in core (panelNote), so this draws and
     decides nothing. It used to decide: not-running got a button and unknown got a bare sentence,
     which left somebody whose companion could not be reached with nothing to press.

     The parameter is the NOTE, not the state. It used to be handed the state and reach for
     st.note - and the note is a SIBLING of state in the message, not a child of it, so that
     reach was always undefined. Nothing failed: the panel simply never drew the sentence and
     never drew the button, which is the same screen as "everything is fine" and is exactly the
     screen somebody with no companion running was left looking at.
     (No backticks in here: this script lives in a template literal and one would close it.) */
  noteEl.append(note.text + ' ');
  if (note.offerStart) {
    const b = document.createElement('button');
    b.textContent = 'Start one';
    b.addEventListener('click', () => vs.postMessage({ kind: 'start' }));
    noteEl.append(b);
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
    d.className = 'row ' + r.who + (r.pending ? ' pending' : '')
      + (r.queued ? ' queued' : '') + (r.abandoned ? ' abandoned' : '');
    const w = document.createElement('div');
    w.className = 'who';
    w.textContent = r.label;
    const b = document.createElement('div');
    b.textContent = r.text;           /* textContent, never innerHTML: the model wrote this */
    /* A tool row names the call AND what it was asked to do. Without the second half a turn that
       runs thirty commands is thirty rows reading the same word, and the transcript cannot answer
       the one question it exists for. Its own element so it can be dimmed and clipped without
       touching the name. */
    /* Why it failed, under the row. The glyph says the shape of the trouble; this says what it
       was, which is what a person opened the transcript for. */
    if (r.who === 'tool' && r.out) {
      const o = document.createElement('div');
      o.className = 'out';
      o.textContent = r.out;
      d.append(o);
    }
    /* What the verdict rests on, and what it says a revision must keep.
       ⚠ No backticks in here: this whole script is a template literal, and one closes it.
       The cite is the fragment magi can look up in the material the member was shown, and the core
       says the case that matters — an empty one on an approval is itself worth seeing, so a done
       standing on nothing must not draw the same as one standing on the record. The keep arrives on
       approvals too, and that is exactly when it is worth reading: it is what a rewrite forced by
       somebody else's objection would otherwise drop. */
    if (r.who === 'council') {
      for (const [cls, label, text] of [['cite', 'on', r.cite], ['keep', 'keep', r.keep]]) {
        if (!text) continue;
        const el = document.createElement('div');
        el.className = String(cls);
        el.textContent = label + ': ' + text;
        d.append(el);
      }
    }
    if (r.who === 'tool' && r.args) {
      const a = document.createElement('span');
      a.className = 'args';
      a.textContent = r.args;
      b.append(' ', a);
    }
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
  else if (m.kind === 'compose') {
    /* PREPENDED, never assigned. This carries two things: a lead-in for a question the person is
       about to type, and their own words handed back after a send that did not land. Assigning
       destroyed whatever was in the box — so somebody mid-sentence who reached for "ask about this
       code" lost the sentence, which is the very thing the caller's own comment says they are meant
       to write ("The person types the question"). The box is theirs.
       After a send the box is already empty, so prepending is what assigning was for that caller.
       The caret goes to the end of what arrived: the lead reads first and typing continues after
       it, and on an empty box that is the end of everything. */
    const lead = m.text || '';
    say.value = lead + say.value;
    say.focus();
    say.setSelectionRange(lead.length, lead.length);
  }
  else if (m.kind === 'mentions') {
    mentions = m.files || [];
    hint.textContent = mentions.length ? 'files: ' + mentions.slice(0, 6).join('  ') : '';
  }
  else if (m.kind === 'suggestion') {
    /* Ghost text for the composer. Tab takes it — the same key the terminal uses. */
    suggestion = m.text || '';
    hint.textContent = suggestion ? 'Tab: ' + suggestion.split('\n')[0].slice(0, 60) : '';
  }
  else if (m.kind === 'state') drawState(m.note);
  else if (m.kind === 'info') { info = m; drawInfo(); }
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
function paint(r: Row, you?: string): Row & { label: string } {
  // The person's own name when something has told us one. Every screen said "user" at whoever
  // had logged in, because the field the daemon fills for exactly this was never read.
  const who = r.who === 'council' && seat(r.member) ? r.member!.toLowerCase()
    : r.who === 'user' && you ? you
    : r.who;
  // A council row's label carries the vote and the round. Without them nine rows over three rounds
  // read as one undifferentiated block, and the one thing a verdict IS — how they voted — is absent.
  const v = r.who === 'council' ? verdictWord(r.decision, r.silent) : { icon: '', word: '' };
  // The lens goes with the name because it IS the seat: three verdicts without it are three
  // interchangeable names, and "two said done" then says nothing about what was examined.
  // The row that OPENS a round carries the threshold instead of a vote — nobody has voted yet, and
  // the rule is what the votes about to arrive will be counted against. Two `continue` and one `done`
  // mean different things under "majority" and under "unanimous", and until now this client drew
  // neither the opening nor the rule.
  const vote = r.who !== 'council' ? ''
    : r.opened ? ` opened${r.rule ? ` · ${r.rule}` : ''}${r.round ? ` r${r.round}` : ''}`
    : (r.lens ? ` [${r.lens}]` : '') + (v.word ? ` ${v.icon} ${v.word}` : '') + (r.round ? ` r${r.round}` : '');
  // Three outcomes, not two: done, done-with-something-to-read, failed. Folding the middle one
  // into ✗ is the defect the core measured on a live run — a file that was written and then
  // linted drew as a write that failed.
  const mark = r.who !== 'tool' || r.ok === undefined ? '' : r.note ? ' ⚑' : r.ok ? ' ✓' : ' ✗';
  return { ...r, label: who + vote + mark };
}
