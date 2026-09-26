import type * as Adapter from './chat_adapter';
import type { createAnswerState as CreateAnswerState } from '../core/answer_state';
import type { createRecoveryState as CreateRecoveryState } from '../core/recovery_state';
import type { Ask } from '../core/touched';
import type { HostToWebviewMessage } from '../core/webview_protocol';

type InfoMessage = Extract<HostToWebviewMessage, { kind: 'info' }>;
type ReceiveOptions = Adapter.WebviewReceiveAdapterOptions;

declare function acquireVsCodeApi(): Adapter.WebviewBridge & { getState(): unknown; setState(s: unknown): void };
declare const createAnswerState: typeof CreateAnswerState;
declare const createRecoveryState: typeof CreateRecoveryState;
declare const createWebviewActionAdapter: typeof Adapter.createWebviewActionAdapter;
declare const createWebviewInputAdapter: typeof Adapter.createWebviewInputAdapter;
declare const createSuggestController: typeof Adapter.createSuggestController;
declare const createWebviewReceiveHandlers: typeof Adapter.createWebviewReceiveHandlers;
declare const parseHostToWebviewMessage: typeof Adapter.parseHostToWebviewMessage;
declare const dispatchHostMessage: typeof Adapter.dispatchHostMessage;
declare const classifyDiffLines: typeof Adapter.classifyDiffLines;
declare const renderMarkdown: typeof Adapter.renderMarkdown;
declare const formatChoiceOptions: typeof Adapter.formatChoiceOptions;
declare const updateInFlightUI: typeof Adapter.updateInFlightUI;
declare const createWebviewRecoveryController: typeof Adapter.createWebviewRecoveryController;
declare const createRecoveryView: typeof Adapter.createRecoveryView;
declare const captureSelection: typeof Adapter.captureSelection;
declare const restoreSelection: typeof Adapter.restoreSelection;
declare const restoreFocus: typeof Adapter.restoreFocus;
declare const moveDomChild: typeof Adapter.moveDomChild;


const vs = acquireVsCodeApi();
const actions = createWebviewActionAdapter(vs);
const scrollEl = document.getElementById('scroll') as HTMLElement;
const rowsEl = document.getElementById('rows') as HTMLElement;
const askBodyEl = document.getElementById('ask-body') as HTMLElement;
const askControlsEl = document.getElementById('ask-controls') as HTMLElement;
const replyModeEl = document.getElementById('reply-mode') as HTMLElement;
const replyTargetEl = document.getElementById('reply-target') as HTMLElement;
const replyCancelEl = document.getElementById('reply-cancel') as HTMLElement;
const noteEl = document.getElementById('note') as HTMLElement;
const stateNoteEl = document.getElementById('state-note') as HTMLElement;
const emptyNoteEl = document.getElementById('empty-note') as HTMLElement;
const say = document.getElementById('say') as HTMLTextAreaElement;
const refsEl = document.getElementById('refs') as HTMLElement;
const hint = document.getElementById('hint') as HTMLElement;
const recoveryBtn = document.getElementById('recovery-btn') as HTMLElement;
const recoveryPanel = document.getElementById('recovery-panel') as HTMLElement;
const recoveryItemsEl = document.getElementById('recovery-items') as HTMLElement;
const recoveryScopeAll = document.getElementById('recovery-scope-all') as HTMLInputElement;
const recoveryStatus = document.getElementById('recovery-status') as HTMLElement;
let currentAsk: Ask | null = null;
let currentAskCallId: string | null = null;
function askedAt(iso: string | undefined) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const clock = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const now = new Date();
  const sameDay = d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth()
    && d.getDate() === now.getDate();
  return sameDay ? clock : d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + clock;
}
function renderDiff(container: HTMLElement, text: string) {
  if (!text || typeof classifyDiffLines !== 'function') return;
  const lines = text.split('\n');
  if (lines.length > 0 && lines[lines.length - 1] === '' && text.endsWith('\n')) lines.pop();
  const classified = classifyDiffLines(lines);
  for (let i = 0; i < classified.length; i++) {
    const row = document.createElement('div');
    row.className = 'diff-line ' + classified[i].cls;
    row.textContent = classified[i].text + (i < classified.length - 1 || text.endsWith('\n') ? '\n' : '');
    container.append(row);
  }
}
function baseName(p: string) {
  if (!p) return '';
  const parts = p.split('/');
  const last = parts[parts.length - 1];
  const winParts = last.split('\\');
  return winParts[winParts.length - 1] || p;
}
function drawAsk(a: Ask | null) {
  const boundSession = currentSession;
  if (!a) {
    if (answerState.getPendingQuestion()) inputAdapter.exitAnswerMode();
    currentAsk = null;
    currentAskCallId = null;
    askBodyEl.hidden = true;
    askBodyEl.textContent = '';
    askControlsEl.hidden = true;
    askControlsEl.textContent = '';
    askControlsEl.removeAttribute('aria-busy');
    inputAdapter.updateInFlightStatus?.();
    return;
  }
  if (currentAskCallId === a.callId) return;
  if (answerState.getPendingQuestion() && answerState.getPendingQuestion() !== a.callId) inputAdapter.exitAnswerMode();
  currentAsk = a;
  currentAskCallId = a.callId;

  askBodyEl.textContent = '';
  askControlsEl.textContent = '';
  askBodyEl.hidden = false;
  askControlsEl.hidden = false;

  const w = document.createElement('div');
  w.className = 'what';
  askBodyEl.append(w);
  /* Where this sits in the run the call is asking: (3/5). The core says why it travels — a viewer
     "has no other way to know that answering this one leads to another" — and without it somebody
     who answers the first question of five believes they are done. Only when there IS more than
     one: "(1/1)" beside a lone question is noise pretending to be information. */
  if (a.total !== undefined && a.total > 1) {
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

  const sumRow = document.createElement('div');
  sumRow.className = 'summary-row';
  const sumText = document.createElement('span');
  sumText.className = 'summary-text';
  const countTag = a.total !== undefined && a.total > 1 ? ' (' + a.index + '/' + a.total + ')' : '';
  const labelPrefix = a.kind === 'permission' ? '승인 대기: ' : '답변 대기: ';
  const targetPath = (a.filePath || '').trim();
  const fileTag = targetPath ? ' · ' + baseName(targetPath) : '';
  sumText.textContent = labelPrefix + a.what + fileTag + countTag;
  const askStatusEl = document.createElement('span');
  askStatusEl.className = 'ask-status';
  askStatusEl.setAttribute('role', 'status');
  askStatusEl.setAttribute('aria-live', 'polite');
  const jumpBtn = document.createElement('button');
  jumpBtn.className = 'jump-btn';
  jumpBtn.textContent = '질문으로 이동';
  jumpBtn.title = '질문 본문으로 스크롤 이동';
  jumpBtn.addEventListener('click', () => {
    askBodyEl.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  });
  sumRow.append(sumText, askStatusEl, jumpBtn);
  askControlsEl.append(sumRow);

  const acts = document.createElement('div');
  acts.className = 'acts';

  if (a.kind === 'permission') {
    if (answerState.getPendingQuestion()) inputAdapter.exitAnswerMode();
    w.prepend('magi wants to run: ' + a.what);
    if (targetPath) {
      const fileEl = document.createElement('div');
      fileEl.className = 'file-target';
      fileEl.textContent = '파일: ';
      const openBtn = document.createElement('button');
      openBtn.type = 'button';
      openBtn.className = 'file-nav-btn inspect-btn';
      openBtn.textContent = targetPath;
      openBtn.title = '파일 열기 (현재 파일)';
      openBtn.setAttribute('aria-label', '파일 열기 (현재 파일): ' + targetPath);
      if (!boundSession || !a.callId) {
        openBtn.disabled = true;
      } else {
        openBtn.addEventListener('click', () => {
          actions.openFile(boundSession, a.callId);
        });
      }
      fileEl.append(openBtn);
      askBodyEl.append(fileEl);
    }
    /* WHAT is being allowed, not a description of it. Without this a person presses allow knowing
       only the tool's name — the place where the most is riding on the answer was the one drawn
       with the least. The args are the thing itself; the reason is prose about why the policy
       stopped here; the diff is what approving would change. */
    for (const [cls, text] of [['args', a.args], ['reason', a.reason], ['diff', a.diff]] as [string, string | undefined][]) {
      if (!text) continue;
      const p = document.createElement('pre');
      p.className = cls;
      if (cls === 'diff') {
        renderDiff(p, text);
      } else {
        p.textContent = text;      /* textContent, never innerHTML: this is workspace input */
      }
      askBodyEl.append(p);
    }
    /* Nothing came. Say so — three buttons over a blank space read as "there is nothing to it",
       and that is the reading this must not allow. */
    if (!a.args && !a.reason && !a.diff) {
      const u = document.createElement('div');
      u.className = 'unstated';
      u.textContent = 'the companion did not say what this would do';
      askBodyEl.append(u);
    }
    const canDiff = a.diffKind === 'sides' || a.diffKind === 'patch';
    if (canDiff) {
      const diffBtn = document.createElement('button');
      diffBtn.type = 'button';
      diffBtn.className = 'diff-btn inspect-btn';
      diffBtn.textContent = '변경 보기';
      diffBtn.title = '변경 보기 (승인 당시 비교 자료)';
      diffBtn.setAttribute('aria-label', '변경 보기 (승인 당시 비교 자료)');
      if (!boundSession || !a.callId) {
        diffBtn.disabled = true;
      } else {
        diffBtn.addEventListener('click', () => actions.openDiff(boundSession, a.callId));
      }
      acts.append(diffBtn);
    }
    /* The three words the core spells are the TOKENS — they go on the wire unchanged, so the two
       vocabularies cannot drift. What a person reads is a different thing, and this screen used to
       conflate them: the buttons read allow/deny/always, lowercase English, inside an
       otherwise Korean prompt, with no accessible name of their own — so a screen reader announced
       the wire token. Display and token are separated here; the label is also the accessible name,
       because a control that is read differently from how it looks is its own defect.

       The "always" label says only what the core does (permission.go records the tool for THIS
       session; persisting to project rules is a further step), so it does not promise forever. */
    /* ⚠ This block is INSIDE a template literal — it ships as webview JS text, so TypeScript never
       looks at it and never strips anything. TS syntax written here survives into the bundle and
       kills the whole script at parse time. An "as const" here did exactly that: the conversation
       stopped sending AND receiving, with nothing on screen to say why. Plain JS only. */
    const LABEL = { allow: '허용', deny: '거절', always: '항상 허용' };
    for (const d of ['allow', 'deny', 'always'] as const) {
      const label = LABEL[d];
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'approval-btn decision-' + d;
      b.textContent = label;
      b.setAttribute('aria-label', label);
      b.addEventListener('click', () => actions.answer(a.callId, d));
      acts.append(b);
    }
    askControlsEl.append(acts);
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
    askBodyEl.append(row);
  }
  const formattedChoices = typeof formatChoiceOptions === 'function'
    ? formatChoiceOptions(a.options)
    : { hideListMarker: false, items: (a.options || []).map((opt: string, i: number) => ({ raw: opt, buttonLabel: (i + 1) + '. ' + opt })) };
  if (formattedChoices.items.length) {
    const ol = document.createElement('ol');
    ol.className = formattedChoices.hideListMarker ? 'choices hide-marker' : 'choices';
    for (const item of formattedChoices.items) {
      const li = document.createElement('li');
      li.textContent = item.raw;
      ol.append(li);
    }
    askBodyEl.append(ol);
  }
  for (let i = 0; i < formattedChoices.items.length; i++) {
    const item = formattedChoices.items[i];
    const b = document.createElement('button');
    b.className = 'choice-btn';
    b.textContent = item.buttonLabel;
    b.title = item.raw;
    b.addEventListener('click', () => {
      inputAdapter.submitChoice(a.callId, item.raw);
    });
    acts.append(b);
  }
  const free = document.createElement('button');
  free.className = 'direct-btn';
  free.textContent = '직접 입력';
  free.title = '입력창에서 직접 답변 작성';
  free.addEventListener('click', () => { inputAdapter.enterAnswerMode(a.callId, a.what); });
  acts.append(free);
  askControlsEl.append(acts);
  if (formattedChoices.items.length === 0) {
    inputAdapter.enterAnswerMode(a.callId, a.what);
  }
  inputAdapter.updateInFlightStatus?.();
}
const moreEl = document.getElementById('more') as HTMLElement;
const infoEl = document.getElementById('info') as HTMLElement;
let info: InfoMessage | null = null;
moreEl.addEventListener('click', () => {
  const open = infoEl.hidden;
  infoEl.hidden = !open;
  moreEl.setAttribute('aria-expanded', open ? 'true' : 'false');
  if (open) drawInfo();
});
/* Run one of the editor's own commands. The NAME is chosen here and checked on the other side —
   a webview is a page and a page must not be able to name any command it likes. */
function act(command: string) { actions.act(command); }
function line(k: string, v?: string, cls?: string) {
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
                             ['approval', info.permission, 'magi.choosePermission']] as [string, string | undefined, string][]) {
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
const answerState = createAnswerState();
let currentCompanionKey = '';
let currentSession = '';
let currentGeneration: number | undefined = undefined;
let currentWebviewId = '';
const expandedCallIds = new Set();
const inputAdapter = createWebviewInputAdapter({
  say,
  sendBtn: document.getElementById('send') as HTMLElement,
  replyModeEl,
  replyTargetEl,
  replyCancelEl,
  noteEl,
  hintEl: hint,
  askControlsEl,
  getCurrentAsk: () => currentAsk,
}, actions, answerState);
const recoveryController = createWebviewRecoveryController({
  elements: {
    recoveryBtn,
    recoveryPanel,
    recoveryItemsEl,
    recoveryScopeAll,
    recoveryStatus,
    say,
  },
  answerState,
  inputAdapter,
  getCurrentCompanionKey: () => currentCompanionKey,
  getCurrentSession: () => currentSession,
});
/* 빈 전사가 말하는 것은 **전사에 대한 사실**뿐이다. 단추는 여기 안 붙는다 — 나가는 길은
   state-note 가 혼자 맡는다(시작 단추가 두 곳에 생기면 같은 일을 두 번 그리는 것이다).
   무엇을 말할지는 코어가 정한다(emptyTranscriptNote). 이 함수는 그리기만 한다.
   (이 스크립트는 템플릿 문자열 안이다 — 주석에 백틱을 쓰면 안 된다.) */
function drawEmptyNote(text: string) {
  emptyNoteEl.textContent = text || '';
  emptyNoteEl.hidden = !text;
}

function drawState(note: ReceiveOptions['drawState'] extends (n: infer N) => void ? N : never) {
  /* ⚠ **지속 상태는 제 자리에 산다.** 이 함수는 오래 note 칸에 썼고, 같은 칸에 "sending…" 같은
     일시 알림도 살았다. 둘이 한 싱크라 서로를 지웠다: 컴패니언이 죽어 「없습니다」와 시작 단추가
     떠 있을 때 무언가 보내면 그것이 덮이고, 4초 뒤 타이머가 지우고, **다음 state 사건이 올 때까지
     안 돌아왔다.** 보고(사건)와 상태(수준)가 한 자리면 뒤가 앞을 지운다. 이제 갈라 둔다 —
     여기는 상태가 바뀔 때만 바뀌고, 일시 알림의 만료는 이 자리를 건드리지 않는다.
     (이 스크립트는 템플릿 문자열 안에 산다 — 주석에도 백틱을 쓰면 안 된다.) */
  stateNoteEl.textContent = '';
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
  stateNoteEl.append(note.text + ' ');
  if (note.offerStart) {
    const b = document.createElement('button');
    b.textContent = 'Start one';
    b.addEventListener('click', () => actions.start());
    stateNoteEl.append(b);
  }
  /* idle / working / waiting say nothing here: the status bar already says them, and repeating a
     line above the composer is a line in the way. */
}
function drawRefs(rs: string[]) {
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
    b.addEventListener('click', () => actions.drop());
    refsEl.append(b);
  }
}
function draw(rs: Parameters<ReceiveOptions['drawRows']>[0]) {
  const boundSession = currentSession;
  const activeEl = document.activeElement as HTMLElement | null;
  const focusedCallId = (activeEl && activeEl.classList && activeEl.classList.contains('args-toggle-btn')) ? activeEl.dataset.callId : null;
  rowsEl.textContent = '';
  for (const r of rs) {
    const d = document.createElement('div');
    d.className = 'row ' + r.who + (r.pending ? ' pending' : '')
      + (r.queued ? ' queued' : '') + (r.abandoned ? ' abandoned' : '');
    const w = document.createElement('div');
    w.className = 'who';
    w.textContent = r.label;
    if (r.outputId) {
      const boundOutputId = r.outputId;
      const openOutputBtn = document.createElement('button');
      openOutputBtn.type = 'button';
      openOutputBtn.className = 'output-open-btn';
      openOutputBtn.textContent = '편집창에서 열기';
      openOutputBtn.title = '편집창에서 열기 (읽기 전용)';
      openOutputBtn.setAttribute('aria-label', '편집창에서 열기 (읽기 전용)');
      openOutputBtn.dataset.outputId = boundOutputId;
      if (!boundSession) {
        openOutputBtn.disabled = true;
      } else {
        openOutputBtn.addEventListener('click', (ev) => {
          ev.stopPropagation();
          actions.openOutput(boundSession, boundOutputId);
        });
      }
      w.append(' ', openOutputBtn);
    }
    const b = document.createElement('div');
    b.className = 'body';
    if (r.who === 'agent' && typeof renderMarkdown === 'function') {
      renderMarkdown(b, r.text);
    } else {
      b.textContent = r.text;           /* textContent, never innerHTML: the model wrote this */
    }
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
      /* The thought is last and dimmest, because it is the one line here that is NOT a vote: it
         never went through the parser, and reading it with the same weight as the grounds above
         would be reading a model's musing as a finding. It is drawn at all for the silent
         members — a reply that came as reasoning alone left "no answer came back" and nothing
         else, over thousands of characters of work. */
      for (const [cls, label, text] of [['cite', 'on', r.cite], ['keep', 'keep', r.keep],
        ['thought', 'thought (not a vote)', r.thought]]) {
        if (!text) continue;
        const el = document.createElement('div');
        el.className = String(cls);
        el.textContent = label + ': ' + text;
        d.append(el);
      }
    }
    if (r.who === 'tool' && r.fileNav) {
      const a = document.createElement('button');
      a.type = 'button';
      a.className = 'file-nav-btn';
      const loc = r.fileNav.line ? r.fileNav.path + ':' + r.fileNav.line : r.fileNav.path;
      a.textContent = loc;
      a.title = r.fileNav.line ? '파일 열기: ' + loc : '파일 열기: ' + r.fileNav.path;
      a.setAttribute('aria-label', a.title);
      if (!boundSession || !r.callId) {
        a.disabled = true;
      } else {
        const callId = r.callId; // narrowed by the check above; a closure would lose that
        a.addEventListener('click', (ev) => {
          ev.stopPropagation();
          actions.openFile(boundSession, callId, r.seq);
        });
      }
      b.append(' ', a);
    }
    if (r.who === 'tool' && r.args) {
      const loc = r.fileNav ? (r.fileNav.line ? r.fileNav.path + ':' + r.fileNav.line : r.fileNav.path) : '';
      if (!r.fileNav || (r.args !== r.fileNav.path && r.args !== loc)) {
        const a = document.createElement('span');
        a.className = 'args';
        a.textContent = r.args;
        b.append(' ', a);
      }
    }
    if (r.who === 'tool' && r.rawArgs && r.rawArgs !== r.args) {
      const toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.className = 'args-toggle-btn';
      if (r.callId) toggle.dataset.callId = r.callId;
      const isExpanded = !!(boundSession && r.callId && expandedCallIds.has(r.callId));
      toggle.textContent = isExpanded ? '접기' : '…';
      toggle.title = isExpanded ? '인자 접기' : '인자 전체 펼치기';
      toggle.setAttribute('aria-label', toggle.title);
      toggle.setAttribute('aria-expanded', isExpanded ? 'true' : 'false');
      const rawBox = document.createElement('pre');
      rawBox.className = 'raw-args';
      rawBox.textContent = r.rawArgs;
      rawBox.hidden = !isExpanded;
      toggle.addEventListener('click', (ev) => {
        ev.stopPropagation();
        const open = rawBox.hidden;
        rawBox.hidden = !open;
        toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
        toggle.textContent = open ? '접기' : '…';
        toggle.title = open ? '인자 접기' : '인자 전체 펼치기';
        toggle.setAttribute('aria-label', toggle.title);
        if (boundSession && r.callId) {
          if (open) expandedCallIds.add(r.callId);
          else expandedCallIds.delete(r.callId);
        }
      });
      b.append(' ', toggle);
      d.append(rawBox);
    }
    d.append(w, b);
    rowsEl.append(d);
  }
  if (focusedCallId) {
    const btns = rowsEl.querySelectorAll<HTMLElement>('.args-toggle-btn');
    for (let i = 0; i < btns.length; i++) {
      if (btns[i].dataset.callId === focusedCallId) {
        btns[i].focus();
        break;
      }
    }
  }
}
const receiveHandlers = createWebviewReceiveHandlers({
  inputAdapter,
  answerState,
  recoveryController,
  getCurrentAsk: () => currentAsk,
  getCurrentSession: () => currentSession,
  setCurrentSession: (s) => { currentSession = s; },
  getCurrentCompanionKey: () => currentCompanionKey,
  setCurrentCompanionKey: (k) => { currentCompanionKey = k; },
  getCurrentGeneration: () => currentGeneration,
  setCurrentGeneration: (g) => { currentGeneration = g; },
  getCurrentWebviewId: () => currentWebviewId,
  setCurrentWebviewId: (w) => { currentWebviewId = w; },
  clearExpandedCallIds: () => expandedCallIds.clear(),
  resetCurrentAsk: () => { currentAsk = null; currentAskCallId = null; },
  drawRows: draw,
  drawAsk,
  drawRefs,
  drawState,
  drawInfo: (m) => { info = m; drawInfo(); },
  drawEmptyNote,
  setNoteText: (t) => { noteEl.textContent = t; },
  getNoteText: () => noteEl.textContent,
  scrollContainer: scrollEl,
});
window.addEventListener('message', (e) => {
  dispatchHostMessage(e.data, receiveHandlers);
});
actions.ready();

export {};
