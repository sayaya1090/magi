import * as vscode from 'vscode';
import { Daemon, retryAfter } from '../core/daemon';
import { Event } from '../core/protocol';
import { Row, rows, seat, todos, turnOpen, verdictWord } from '../core/transcript';
import { touched, pendingAsk } from '../core/touched';
import { panelNote, label as activityLabel } from '../core/activity';
import { usage } from '../core/panel';
import { Ref, refText, wireRef, globQuote } from '../core/refs';
import { noteCompletion } from '../core/complete';
import { Edits } from './edits';
import { Companion } from './workspace';
import { DiffProvider, openApprovalDiff } from './diff';
import { OutputProvider, openOutputDocument } from './output';
import { determineApprovalDiffKind, AskStore } from '../core/diff';
import { resolveAndOpenFile, resolveAndOpenDiff, extractAskFilePath } from '../core/nav';
import { parseWebviewToHostMessage, HostToWebviewMessage } from '../core/webview_protocol';
import { renderChatHtml } from '../web/chat_html';

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
  private readonly diffProvider = new DiffProvider();
  private readonly outputProvider = new OutputProvider();
  private readonly asks = new AskStore(50);

  constructor(private readonly companion: Companion, private readonly extUri: vscode.Uri) {
    this.subs.push(
      companion.onChanged(() => this.post({ kind: 'state', state: companion.state, note: panelNote(companion.state) })),
      vscode.workspace.registerTextDocumentContentProvider(DiffProvider.scheme, this.diffProvider),
      this.diffProvider,
      vscode.workspace.registerTextDocumentContentProvider(OutputProvider.scheme, this.outputProvider),
      this.outputProvider,
    );
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
    // ⚠ **A stream that ends is not a conversation that ended.** The daemon restarts often — a
    // self-update, a crash, somebody stopping it — and until now this window just stopped
    // receiving: no new rows, no word, and a panel that looks like a companion with nothing to
    // say. The JetBrains client tells the three endings apart and reattaches on two of them.
    const ended = () => {
      if (this.stream !== s) return;   // we moved on, or the panel closed — not an ending to report
      this.stream = null;
      // ⚠ **Dropping the reference is not closing the socket**, and which of the two endings arrived
      // decides whether that matters. `whenClosed` fires on a socket that is already gone; the `over`
      // frame arrives on one that is still OPEN — and that is the path this client now prefers,
      // precisely because on Windows the close is the unreliable half. Letting go of it there leaks
      // the connection and, on Windows, the `ide-bridge --raw-socket` relay process behind it: one
      // per daemon restart, for as long as the window stays open (review, 2026-09-14).
      //
      // Closed AFTER the reference is dropped, so the `whenClosed` handler this triggers finds
      // `this.stream !== s` and stays a no-op — the same guard that already makes the two endings
      // idempotent.
      s.close();
      this.post({ kind: 'note', text: 'lost the conversation — reconnecting…' });
      void this.reattach();
    };
    // Two ways to learn it, and the socket is the less reliable one.
    //
    // ⚠ **On Windows the closing of a socket is not reliable news.** AF_UNIX there loses a close
    // that follows a write too closely — measured with no magi code involved, 12 of 600 rounds
    // (2026-09-13; the core's `Response.Over` carries the table). The frame arrives and the close
    // does not, so a panel waiting for `whenClosed` waits for ever: alive-looking, and nothing
    // coming. That is the shape the paragraph above says was fixed. A deadline is no defence
    // either — a quiet transcript stream is normal, so silence and a lost close look the same.
    //
    // So the daemon SAYS the stream is over (`over`), and this reads it. Whichever arrives first
    // wins; the guard above makes the second one a no-op.
    s.whenClosed(ended);
    /**
     * ⚠ **The panel is emptied here, not when the first frame lands.**
     *
     * Clearing `events` alone changes nothing a person can see: the webview keeps the rows it was
     * last handed, and `draw()` runs only inside the frame callback. A conversation with no events
     * sends no frames — and the core makes that explicit: the stream's opening note goes out only
     * when a tail was clipped (`answerable` returns "" for `since <= 0`), so a plain attach is
     * silent until something happens.
     *
     * So opening a brand-new conversation, or resuming one nothing has been said in, left the
     * PREVIOUS conversation's rows on screen under the new one's name — the ask, the plan and the
     * context meter with them.
     *
     * The JetBrains client says the same thing in its own words and puts the clear before the
     * worker thread starts: attachment "is already true on this line, and deferring it leaves the
     * order against the first frame up to luck".
     */
    this.events = [];
    this.draw();
    s.stream({ method: 'transcript', session: sid }, (r) => {
      if (r.event) {
        this.events.push(r.event);
        if (this.sid) {
          if (turnOpen(this.events)) {
            this.sessionActiveTurns.add(this.sid);
          } else {
            this.sessionActiveTurns.delete(this.sid);
          }
        }
        this.draw();
      }
      else if (r.error) this.post({ kind: 'note', text: r.error });
      else if (r.why) this.post({ kind: 'note', text: r.why });
      /* The replay is over. Drawn ONLY when there is nothing to show, because that is the only place
         it changes what a person sees: an empty panel is identical whether the conversation has not
         arrived yet or nothing was ever said in it. With rows on screen the rows are the evidence,
         and a note on every attach would be noise in the one line notes have.
         Nothing is claimed when the marker does not come: an older daemon never sends it, and a panel
         that waited for it would be worse than the ambiguity it was meant to fix. */
      else if (r.over) ended();
      else if (r.live && this.events.length === 0) {
        this.post({ kind: 'note', text: 'Caught up — nothing has been said in this conversation yet.' });
      }
    });
  }

  private draw(): void {
    const rawAsk = pendingAsk(this.events);
    const ask = rawAsk
      ? { ...rawAsk, diffKind: determineApprovalDiffKind(rawAsk), filePath: extractAskFilePath(rawAsk) }
      : null;
    if (ask) this.asks.record(ask, this.companion.workdir, this.session);
    this.post({
      kind: 'rows',
      session: this.session,
      rows: rows(this.events).map((r) => paint(r, this.companion.you)),
      ask,
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
  private sessionCreating: Promise<string> | null = null;
  private generation = 0;
  private sendQueue: Promise<void> = Promise.resolve();
  private readonly sessionActiveTurns = new Set<string>();

  private isSessionTurnOpen(sid: string): boolean {
    if (this.sessionActiveTurns.has(sid)) return true;
    if (this.sid === sid && turnOpen(this.events)) return true;
    return false;
  }

  /** Read another conversation. The daemon is the source, so this only changes which one we ask for. */
  showSession(sid: string): void {
    this.generation++;
    this.stream?.close();
    this.stream = null;
    this.events = [];
    this.sid = sid;
    this.companion.session = sid;
    this.sessionCreating = null;
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

  /* visible for testing */ async fromView(raw: unknown): Promise<void> {
    const m = parseWebviewToHostMessage(raw);
    if (!m) return;
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

        // Pin the target session or session creation synchronously at queue registration time.
        // If this chat view is empty (no sid yet), bind to the current or newly initiated session-new
        // creation Promise. A subsequent message queued before creation settles shares the exact same
        // creation Promise and will not switch to another session if the view navigates away.
        const targetSid = this.sid;
        const creationGen = this.generation;
        let pendingCreation: Promise<string> | null = null;
        if (!targetSid) {
          if (!this.sessionCreating) {
            const promise = (async () => {
              const created = await this.companion.ask('session-new');
              const sid = created?.session ?? '';
              if (!sid) {
                throw new Error(created?.error ?? 'the companion could not open a conversation.');
              }
              return sid;
            })();
            this.sessionCreating = promise;
            const cleanup = () => {
              if (this.sessionCreating === promise) {
                this.sessionCreating = null;
              }
            };
            promise.then(cleanup, cleanup);
          }
          pendingCreation = this.sessionCreating;
        }

        const prevQueue = this.sendQueue;
        let resolveQueue!: () => void;
        this.sendQueue = new Promise<void>((res) => { resolveQueue = res; });

        try {
          await prevQueue;

          let resolvedSid = targetSid;
          if (!resolvedSid && pendingCreation) {
            try {
              resolvedSid = await pendingCreation;
            } catch (e: any) {
              this.giveBack(body, sent, e?.message ?? 'the companion could not open a conversation.');
              break;
            }
            if (this.generation === creationGen && !this.sid) {
              this.sid = resolvedSid;
              this.companion.session = resolvedSid;
              void this.openStream();
            }
          }

          if (!resolvedSid) {
            this.giveBack(body, sent, 'the companion could not determine target session.');
            break;
          }

          // Which door: `steer` while a turn is running, `submit` otherwise. Not one door with two
          // names — `submit` is a new top-level request and the core wipes the plan for it, so a
          // clarification typed mid-turn would delete the plan of the turn it was clarifying.
          // The fact comes off the target session state, or the transcript this window streams (turnOpen).
          const isTurnRunning = this.isSessionTurnOpen(resolvedSid) || (this.sid === resolvedSid && turnOpen(this.events));
          const door = isTurnRunning ? 'steer' : 'submit';

          const r = await this.companion.ask(door, refs.length ? { session: resolvedSid, text: body, refs } : { session: resolvedSid, text: body });
          if (!r?.ok) {
            this.sessionActiveTurns.delete(resolvedSid);
            this.giveBack(body, sent, r?.error ?? 'no companion is listening on this workspace.');
          } else {
            // Once submit succeeds, mark turn active immediately even before first stream chunk lands
            this.sessionActiveTurns.add(resolvedSid);
          }
          if (this.sid === resolvedSid) {
            this.draw();
          }
        } finally {
          resolveQueue();
        }
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
      case 'diff': {
        await resolveAndOpenDiff({
          m,
          session: this.session,
          companionWorkdir: this.companion.workdir,
          companionState: this.companion.state.state,
          asks: this.asks,
          events: this.events,
          postNote: (text) => this.post({ kind: 'note', text }),
          opener: {
            openDiff: (workdir, sessionId, ask) =>
              openApprovalDiff(this.diffProvider, workdir, sessionId, ask),
          },
        });
        break;
      }
      case 'open': {
        await resolveAndOpenFile({
          m,
          session: this.session,
          companionWorkdir: this.companion.workdir,
          companionState: this.companion.state.state,
          asks: this.asks,
          events: this.events,
          postNote: (text) => this.post({ kind: 'note', text }),
          opener: {
            async openDocument(absPath: string, line?: number) {
              const uri = vscode.Uri.file(absPath);
              const doc = await vscode.workspace.openTextDocument(uri);
              let selection: vscode.Range | undefined;
              let actualLine: number | undefined;
              if (line !== undefined && Number.isInteger(line) && line > 0) {
                const lineCount = doc.lineCount;
                const lineIdx = Math.min(line - 1, Math.max(0, lineCount - 1));
                const pos = new vscode.Position(lineIdx, 0);
                selection = new vscode.Range(pos, pos);
                actualLine = lineIdx + 1;
              }
              await vscode.window.showTextDocument(doc, { selection, preserveFocus: false });
              return { opened: true, line: actualLine };
            },
          },
        });
        break;
      }
      case 'output': {
        if (!m.session || !m.outputId) break;
        if (this.session && m.session !== this.session) {
          this.post({ kind: 'note', text: '자료를 더 이상 열 수 없음 — 세션이 일치하지 않습니다.' });
          break;
        }
        const res = await openOutputDocument({
          provider: this.outputProvider,
          companionKey: this.companion.workdir,
          session: m.session,
          outputId: m.outputId,
          events: this.events,
          preserveFocus: false,
        });
        if (!res.opened) {
          this.post({ kind: 'note', text: res.error ?? '자료를 더 이상 열 수 없음' });
        }
        break;
      }
      case 'answer': {
        // The decision travels as the core spells it. Two vocabularies for one verdict is a place
        // for the two to drift.
        const v = await this.companion.ask('permission', {
          session: this.sid,
          callId: m.callId,
          decision: m.decision,
        });
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
        const attemptId = m.attemptId;
        const a = await this.companion.ask('answer', {
          session: this.sid,
          callId: m.callId,
          answer: said,
        });
        if (a?.ok) {
          this.post({ kind: 'replyResult', callId: m.callId, attemptId, ok: true });
        } else {
          const err = a?.error ?? 'no companion is listening on this workspace.';
          this.post({ kind: 'replyResult', callId: m.callId, attemptId, ok: false, error: err, text: said });
          this.post({ kind: 'note', text: `not sent — ${err}` });
        }
        break;
      }
      case 'mention': {
        // The file list behind `@`. It comes from the companion's own glob rather than from this
        // window's idea of the workspace: the companion is what will read the file, and what it
        // can reach is the answer that matters.
        const r = await this.companion.ask('tool', { name: 'glob',
          args: { pattern: `**/*${globQuote((m.text ?? '').trim())}*` } });
        let files: string[] = [];
        try { files = JSON.parse(r?.out ?? '[]') as string[]; } catch { files = []; }
        this.post({ kind: 'mentions', files: files.slice(0, 20), reqId: m.reqId, target: m.target });
        break;
      }
      case 'suggest': {
        // The composer's ghost text — the same door the console uses for its own.
        // ⚠ This asks the model every time typing pauses, so it is switchable like the other two
        // typing-time doors (`magi.complete`, `magi.lookWhileTyping`). The JetBrains plugin and the
        // web console have carried that switch all along; this client fired the door unconditionally.
        // The default is on, which is what this client did before and what the other two default to.
        if (!vscode.workspace.getConfiguration('magi').get<boolean>('suggest', true)) {
          this.post({ kind: 'suggestion', text: '', reqId: m.reqId, target: m.target });
          break;
        }
        const r = await this.companion.ask('suggest', { text: m.text ?? '' });
        // The same sink as the completer's: these two doors hang off one interface in the core, so
        // a refusal on either is the answer to "why is there never a hint". Ghost text cannot say
        // it here — a message per keystroke is noise — so `magi.setup` is where it surfaces.
        noteCompletion(r?.ok ? (r.out ?? '') : '', r?.reason, r?.ok ? undefined : (r?.error ?? undefined));
        this.post({ kind: 'suggestion', text: r?.ok ? (r.out ?? '') : '', reqId: m.reqId, target: m.target });
        break;
      }
      default:
        break;
    }
  }

  private post(msg: HostToWebviewMessage): void { void this.view?.webview.postMessage(msg); }

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
    this.asks.clear();
    this.outputProvider.dispose();
    for (const s of this.subs) s.dispose();
  }

  private html(w: vscode.Webview): string {
    const nonce = Math.random().toString(36).slice(2) + Date.now().toString(36);
    const cspSource = w.cspSource;
    const scriptUri = typeof this !== 'undefined' && this?.extUri && w.asWebviewUri
      ? w.asWebviewUri(vscode.Uri.joinPath(this.extUri, 'out', 'web', 'answer_state.js')).toString()
      : 'out/web/answer_state.js';
    const adapterUri = typeof this !== 'undefined' && this?.extUri && w.asWebviewUri
      ? w.asWebviewUri(vscode.Uri.joinPath(this.extUri, 'out', 'web', 'chat_adapter.bundle.js')).toString()
      : 'out/web/chat_adapter.bundle.js';
    return renderChatHtml({
      nonce,
      cspSource,
      scriptUri,
      adapterUri,
    });
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
    : r.opened ? ` opened${r.rule ? ` · ${r.rule}` : ''}${r.readOnly ? ' · read-only turn' : ''}${r.round ? ` r${r.round}` : ''}`
    : (r.lens ? ` [${r.lens}]` : '') + (v.word ? ` ${v.icon} ${v.word}` : '')
      // How sure, in the shape the terminal uses. The tally weighs by it, so the word alone
      // shows the vote and hides what the rule did with it.
      + (r.confidence ? ` ${Math.round(r.confidence * 100)}%` : '')
      + (r.round ? ` r${r.round}` : '');
  // Three outcomes, not two: done, done-with-something-to-read, failed. Folding the middle one
  // into ✗ is the defect the core measured on a live run — a file that was written and then
  // linted drew as a write that failed.
  const mark = r.who !== 'tool' || r.ok === undefined ? '' : r.note ? ' ⚑' : r.ok ? ' ✓' : ' ✗';
  return { ...r, label: who + vote + mark };
}
