/**
 * Type-checked webview DOM and action adapter.
 *
 * Runs in the browser context of the chat webview.
 * Enforces discriminated union types for outbound messages to the host
 * and safely validates/dispatches inbound messages from the host.
 */

import type {
  WebviewToHostMessage,
  HostToWebviewMessage,
  PaintedRow,
  PanelNoteInfo,
} from './webview_protocol';
import type { Ask } from './touched';
import type { Activity } from './activity';
import type { AnswerStateManager, AskEvent } from './answer_state';

export interface WebviewBridge {
  postMessage(message: WebviewToHostMessage): void;
}

export interface WebviewActionAdapter {
  openFile(session: string, callId: string, seq?: number): boolean;
  openDiff(session: string, callId: string): boolean;
  answer(callId: string, decision: string): boolean;
  reply(callId: string, text: string, attemptId: number): boolean;
  say(text: string): boolean;
  act(command: string): boolean;
  suggest(text: string, reqId: number, target: string): boolean;
  mention(text: string, reqId: number, target: string): boolean;
  drop(): void;
  start(): void;
  ready(): void;
}

export function createWebviewActionAdapter(vs: WebviewBridge): WebviewActionAdapter {
  return {
    openFile(session: string, callId: string, seq?: number): boolean {
      if (!session || !callId) return false;
      if (typeof seq === 'number') {
        vs.postMessage({ kind: 'open', session, callId, seq });
      } else {
        vs.postMessage({ kind: 'open', session, callId });
      }
      return true;
    },

    openDiff(session: string, callId: string): boolean {
      if (!session || !callId) return false;
      vs.postMessage({ kind: 'diff', session, callId });
      return true;
    },

    answer(callId: string, decision: string): boolean {
      if (!callId || !decision) return false;
      vs.postMessage({ kind: 'answer', callId, decision });
      return true;
    },

    reply(callId: string, text: string, attemptId: number): boolean {
      if (!callId || typeof attemptId !== 'number') return false;
      vs.postMessage({ kind: 'reply', callId, text, attemptId });
      return true;
    },

    say(text: string): boolean {
      const trimmed = text.trim();
      if (!trimmed) return false;
      vs.postMessage({ kind: 'say', text: trimmed });
      return true;
    },

    act(command: string): boolean {
      const trimmed = command.trim();
      if (!trimmed) return false;
      vs.postMessage({ kind: 'run', command: trimmed });
      return true;
    },

    suggest(text: string, reqId: number, target: string): boolean {
      vs.postMessage({ kind: 'suggest', text, reqId, target });
      return true;
    },

    mention(text: string, reqId: number, target: string): boolean {
      vs.postMessage({ kind: 'mention', text, reqId, target });
      return true;
    },

    drop(): void {
      vs.postMessage({ kind: 'drop' });
    },

    start(): void {
      vs.postMessage({ kind: 'start' });
    },

    ready(): void {
      vs.postMessage({ kind: 'ready' });
    },
  };
}

export interface HostMessageHandlers {
  onRows?(payload: { session: string; rows: PaintedRow[]; ask: Ask | null; refs: string[] }): void;
  onState?(payload: { state: Activity; note: PanelNoteInfo }): void;
  onInfo?(payload: {
    kind: 'info';
    state: string;
    label: string;
    version: string;
    model?: string;
    backend?: string;
    permission?: string;
    council?: string;
    socket?: string;
  }): void;
  onCompose?(payload: { text: string }): void;
  onNote?(payload: { text: string }): void;
  onReplyResult?(payload: {
    callId: string;
    attemptId: number;
    ok: boolean;
    error?: string;
    text?: string;
  }): void;
  onMentions?(payload: { files: string[]; reqId: number; target: string }): void;
  onSuggestion?(payload: { text: string; reqId: number; target: string }): void;
}

/**
 * Validates raw payload from host boundary into typed HostToWebviewMessage.
 * Rejects malformed payloads without silent fallback.
 */
export function parseHostToWebviewMessage(raw: unknown): HostToWebviewMessage | undefined {
  if (!raw || typeof raw !== 'object') return undefined;
  const m = raw as Record<string, unknown>;
  const rawKind = m.kind;
  const kind = typeof rawKind === 'string' ? rawKind : '';

  if (kind === 'rows') {
    if (!Array.isArray(m.rows)) return undefined;
    if (typeof m.session !== 'string') return undefined;
    if (!Array.isArray(m.refs) || !m.refs.every((r) => typeof r === 'string')) return undefined;

    const rows: PaintedRow[] = [];
    for (const r of m.rows) {
      if (!r || typeof r !== 'object') return undefined;
      const rowObj = r as Record<string, unknown>;
      if (
        typeof rowObj.who !== 'string' ||
        typeof rowObj.label !== 'string' ||
        typeof rowObj.text !== 'string'
      ) {
        return undefined;
      }
      rows.push(r as PaintedRow);
    }

    let ask: Ask | null = null;
    if (m.ask !== undefined && m.ask !== null) {
      if (typeof m.ask !== 'object' || Array.isArray(m.ask)) return undefined;
      const askObj = m.ask as Record<string, unknown>;
      if (typeof askObj.callId !== 'string' || !askObj.callId) return undefined;
      if (typeof askObj.what !== 'string') return undefined;
      if (askObj.kind !== 'permission' && askObj.kind !== 'question') return undefined;
      if (
        askObj.options !== undefined &&
        (!Array.isArray(askObj.options) || !askObj.options.every((o) => typeof o === 'string'))
      ) {
        return undefined;
      }
      if (
        askObj.report !== undefined &&
        (!Array.isArray(askObj.report) ||
          !askObj.report.every(
            (item) =>
              item &&
              typeof item === 'object' &&
              typeof (item as any).key === 'string' &&
              typeof (item as any).text === 'string'
          ))
      ) {
        return undefined;
      }
      if (askObj.args !== undefined && typeof askObj.args !== 'string') return undefined;
      if (askObj.reason !== undefined && typeof askObj.reason !== 'string') return undefined;
      if (askObj.diff !== undefined && typeof askObj.diff !== 'string') return undefined;
      if (
        askObj.diffKind !== undefined &&
        askObj.diffKind !== 'sides' &&
        askObj.diffKind !== 'patch' &&
        askObj.diffKind !== 'none'
      ) {
        return undefined;
      }
      if (askObj.filePath !== undefined && typeof askObj.filePath !== 'string') return undefined;
      if (askObj.index !== undefined && typeof askObj.index !== 'number') return undefined;
      if (askObj.total !== undefined && typeof askObj.total !== 'number') return undefined;
      if (askObj.since !== undefined && typeof askObj.since !== 'string') return undefined;
      ask = m.ask as Ask;
    }

    return {
      kind: 'rows',
      session: m.session,
      rows,
      ask,
      refs: m.refs as string[],
    };
  } else if (kind === 'compose') {
    if (typeof m.text !== 'string') return undefined;
    return { kind: 'compose', text: m.text };
  } else if (kind === 'mentions') {
    if (!Array.isArray(m.files) || typeof m.reqId !== 'number' || typeof m.target !== 'string') {
      return undefined;
    }
    const files = m.files.filter((f): f is string => typeof f === 'string');
    return { kind: 'mentions', files, reqId: m.reqId, target: m.target };
  } else if (kind === 'suggestion') {
    if (typeof m.text !== 'string' || typeof m.reqId !== 'number' || typeof m.target !== 'string') {
      return undefined;
    }
    return { kind: 'suggestion', text: m.text, reqId: m.reqId, target: m.target };
  } else if (kind === 'replyResult') {
    if (
      typeof m.callId !== 'string' ||
      !m.callId ||
      typeof m.attemptId !== 'number' ||
      typeof m.ok !== 'boolean'
    ) {
      return undefined;
    }
    return {
      kind: 'replyResult',
      callId: m.callId,
      attemptId: m.attemptId,
      ok: m.ok,
      error: typeof m.error === 'string' ? m.error : undefined,
      text: typeof m.text === 'string' ? m.text : undefined,
    };
  } else if (kind === 'state') {
    if (!m.state || typeof m.state !== 'object' || !m.note || typeof m.note !== 'object') {
      return undefined;
    }
    const sObj = m.state as Record<string, unknown>;
    if (typeof sObj.state !== 'string') return undefined;
    const act: Activity = {
      state: sObj.state as any,
      asking: typeof sObj.asking === 'string' ? sObj.asking : undefined,
      doing: typeof sObj.doing === 'string' ? sObj.doing : undefined,
    };

    const noteObj = m.note as Record<string, unknown>;
    if (typeof noteObj.text !== 'string') return undefined;
    const note: PanelNoteInfo = {
      text: noteObj.text,
      offerStart: Boolean(noteObj.offerStart),
    };
    return { kind: 'state', state: act, note };
  } else if (kind === 'info') {
    if (
      typeof m.state !== 'string' ||
      typeof m.label !== 'string' ||
      typeof m.version !== 'string'
    ) {
      return undefined;
    }
    return {
      kind: 'info',
      state: m.state,
      label: m.label,
      version: m.version,
      model: typeof m.model === 'string' ? m.model : undefined,
      backend: typeof m.backend === 'string' ? m.backend : undefined,
      permission: typeof m.permission === 'string' ? m.permission : undefined,
      council: typeof m.council === 'string' ? m.council : undefined,
      socket: typeof m.socket === 'string' ? m.socket : undefined,
    };
  } else if (kind === 'note') {
    if (typeof m.text !== 'string') return undefined;
    return { kind: 'note', text: m.text };
  }

  return undefined;
}

/**
 * Validates and dispatches host messages to appropriate UI handlers.
 */
export function dispatchHostMessage(raw: unknown, handlers: HostMessageHandlers): boolean {
  const m = parseHostToWebviewMessage(raw);
  if (!m) return false;

  if (m.kind === 'rows') {
    handlers.onRows?.(m);
    return true;
  } else if (m.kind === 'compose') {
    handlers.onCompose?.(m);
    return true;
  } else if (m.kind === 'mentions') {
    handlers.onMentions?.(m);
    return true;
  } else if (m.kind === 'suggestion') {
    handlers.onSuggestion?.(m);
    return true;
  } else if (m.kind === 'replyResult') {
    handlers.onReplyResult?.(m);
    return true;
  } else if (m.kind === 'state') {
    handlers.onState?.(m);
    return true;
  } else if (m.kind === 'info') {
    handlers.onInfo?.(m);
    return true;
  } else if (m.kind === 'note') {
    handlers.onNote?.(m);
    return true;
  }
  return false;
}

export interface WebviewInputElements {
  say: HTMLTextAreaElement;
  sendBtn: HTMLElement;
  replyModeEl?: HTMLElement | null;
  replyTargetEl?: HTMLElement | null;
  replyCancelEl?: HTMLElement | null;
  noteEl?: HTMLElement | null;
  hintEl?: HTMLElement | null;
}

export interface WebviewInputAdapter {
  enterAnswerMode(callId: string, label?: string): void;
  exitAnswerMode(): void;
  applyAnswerModeUI(label?: string, text?: string): void;
  applyGeneralModeUI(text?: string): void;
  clearAutoCompletion(): void;
  send(): void;
  submitChoice(callId: string, option: string): boolean;
  handleCompose(text: string): void;
  handleMentions(files: string[], reqId?: number, target?: string): void;
  handleSuggestion(text: string, reqId?: number, target?: string): void;
  handleReplyResult(
    m: { callId: string; attemptId: number; ok: boolean; error?: string; text?: string },
    currentAsk: Ask | null
  ): void;
  getSuggestReqId(): number;
  dispose(): void;
}

export function createWebviewInputAdapter(
  elements: WebviewInputElements,
  actions: WebviewActionAdapter,
  answerState: AnswerStateManager
): WebviewInputAdapter {
  const { say, sendBtn, replyModeEl, replyTargetEl, replyCancelEl, noteEl, hintEl } = elements;
  let suggestion = '';
  let mentions: string[] = [];
  let typing: ReturnType<typeof setTimeout> | null = null;
  let suggestReqId = 0;

  function clearAutoCompletion(): void {
    if (typing) {
      clearTimeout(typing);
      typing = null;
    }
    suggestReqId++;
    suggestion = '';
    mentions = [];
    if (hintEl) hintEl.textContent = '';
  }

  function applyAnswerModeUI(label?: string, text?: string): void {
    clearAutoCompletion();
    if (replyModeEl) {
      replyModeEl.hidden = false;
      if (replyTargetEl) replyTargetEl.textContent = label || '';
    }
    say.placeholder = '답변을 입력하세요 (Esc로 취소)…';
    sendBtn.textContent = '답변';
    if (text !== undefined) say.value = text;
    say.focus();
  }

  function applyGeneralModeUI(text?: string): void {
    clearAutoCompletion();
    if (replyModeEl) {
      replyModeEl.hidden = true;
      if (replyTargetEl) replyTargetEl.textContent = '';
    }
    if (text !== undefined) say.value = text;
    say.placeholder = '';
    sendBtn.textContent = 'Send';
  }

  function enterAnswerMode(callId: string, label?: string): void {
    const res = answerState.enterAnswerMode(callId, label, say.value);
    applyAnswerModeUI(res.label, res.nextInputText);
  }

  function exitAnswerMode(): void {
    const res = answerState.exitAnswerMode(say.value);
    applyGeneralModeUI(res.nextInputText);
  }

  function send(): void {
    const t = say.value.trim();
    if (!t) return;
    const pending = answerState.getPendingQuestion();
    if (pending) {
      const res = answerState.submitReply(pending, t, false);
      if (!res.ok) {
        if (res.error === 'in_flight' && noteEl) {
          noteEl.textContent = 'reply already in flight…';
        }
        return;
      }
      clearAutoCompletion();
      if (typeof res.attemptId === 'number') {
        actions.reply(res.callId ?? pending, res.text ?? t, res.attemptId);
      }
      if (res.exitAnswerMode) {
        applyGeneralModeUI(res.nextInputText);
      }
    } else {
      const res = answerState.submitSay(t);
      if (!res.ok) return;
      clearAutoCompletion();
      actions.say(res.text ?? t);
      say.value = res.nextInputText ?? '';
    }
    if (hintEl) hintEl.textContent = '';
    if (noteEl) {
      noteEl.textContent = 'sending…';
      setTimeout(() => {
        if (noteEl.textContent === 'sending…') noteEl.textContent = '';
      }, 4000);
    }
  }

  function submitChoice(callId: string, option: string): boolean {
    const res = answerState.submitReply(callId, option, true);
    if (!res.ok) {
      if (res.error === 'in_flight' && noteEl) {
        noteEl.textContent = 'reply already in flight…';
      }
      return false;
    }
    clearAutoCompletion();
    if (res.exitAnswerMode) {
      applyGeneralModeUI(res.nextInputText);
    }
    if (typeof res.attemptId === 'number') {
      actions.reply(callId, option, res.attemptId);
    }
    if (noteEl) {
      noteEl.textContent = 'sending…';
      setTimeout(() => {
        if (noteEl.textContent === 'sending…') noteEl.textContent = '';
      }, 4000);
    }
    return true;
  }

  function handleCompose(text: string): void {
    const lead = text || '';
    say.value = lead + say.value;
    answerState.onInputChange(say.value);
    say.focus();
    say.setSelectionRange(lead.length, lead.length);
  }

  function handleMentions(files: string[], reqId?: number, target?: string): void {
    const currentTarget = answerState.getPendingQuestion() || 'general';
    if (reqId !== undefined && reqId !== suggestReqId) return;
    if (target !== undefined && target !== currentTarget) return;
    mentions = files || [];
    if (hintEl) {
      hintEl.textContent = mentions.length ? 'files: ' + mentions.slice(0, 6).join('  ') : '';
    }
  }

  function handleSuggestion(text: string, reqId?: number, target?: string): void {
    const currentTarget = answerState.getPendingQuestion() || 'general';
    if (reqId !== undefined && reqId !== suggestReqId) return;
    if (target !== undefined && target !== currentTarget) return;
    suggestion = text || '';
    if (hintEl) {
      hintEl.textContent = suggestion ? 'Tab: ' + suggestion.split('\n')[0].slice(0, 60) : '';
    }
  }

  function handleReplyResult(
    m: { callId: string; attemptId: number; ok: boolean; error?: string; text?: string },
    currentAsk: Ask | null
  ): void {
    const res = answerState.onReplyResult(m, currentAsk as unknown as AskEvent | null);
    if (!res.handled) return;
    if (res.reenterAnswerMode) {
      applyAnswerModeUI(res.targetLabel, res.nextInputText);
    }
  }

  const onKeyDown = (e: KeyboardEvent): void => {
    if (e.isComposing || e.keyCode === 229) return;
    if (e.key === 'Escape' && answerState.getPendingQuestion()) {
      e.preventDefault();
      exitAnswerMode();
      return;
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send();
      return;
    }
    if (e.key === 'Tab' && suggestion) {
      e.preventDefault();
      say.value += suggestion;
      answerState.onInputChange(say.value);
      suggestion = '';
      if (hintEl) hintEl.textContent = '';
    }
  };

  const onInput = (): void => {
    suggestion = '';
    if (hintEl) hintEl.textContent = '';
    if (typing) clearTimeout(typing);
    const v = say.value;
    const currentTarget = answerState.onInputChange(v).target;
    const reqId = ++suggestReqId;
    const at = /(^|\s)@([^\s@]{2,})$/.exec(v);
    typing = setTimeout(() => {
      if (at) actions.mention(at[2], reqId, currentTarget);
      else if (v.trim().length > 3) actions.suggest(v, reqId, currentTarget);
      else if (hintEl) hintEl.textContent = '';
    }, 450);
  };

  const onSendClick = (): void => {
    send();
  };

  const onCancelClick = (): void => {
    exitAnswerMode();
  };

  say.addEventListener('keydown', onKeyDown);
  say.addEventListener('input', onInput);
  sendBtn.addEventListener('click', onSendClick);
  replyCancelEl?.addEventListener('click', onCancelClick);

  return {
    enterAnswerMode,
    exitAnswerMode,
    applyAnswerModeUI,
    applyGeneralModeUI,
    clearAutoCompletion,
    send,
    submitChoice,
    handleCompose,
    handleMentions,
    handleSuggestion,
    handleReplyResult,
    getSuggestReqId: () => suggestReqId,
    dispose(): void {
      if (typing) clearTimeout(typing);
      say.removeEventListener('keydown', onKeyDown);
      say.removeEventListener('input', onInput);
      sendBtn.removeEventListener('click', onSendClick);
      replyCancelEl?.removeEventListener('click', onCancelClick);
    },
  };
}

export interface WebviewReceiveAdapterOptions {
  inputAdapter: WebviewInputAdapter;
  answerState: AnswerStateManager;
  getCurrentAsk: () => Ask | null;
  getCurrentSession: () => string;
  setCurrentSession: (s: string) => void;
  clearExpandedCallIds: () => void;
  drawRows: (rows: PaintedRow[]) => void;
  drawAsk: (ask: Ask | null) => void;
  drawRefs: (refs: string[]) => void;
  drawState: (note: PanelNoteInfo) => void;
  drawInfo: (info: HostToWebviewMessage & { kind: 'info' }) => void;
  setNoteText: (text: string) => void;
  getNoteText: () => string;
  scrollContainer?: HTMLElement | null;
}

export function createWebviewReceiveHandlers(
  options: WebviewReceiveAdapterOptions
): HostMessageHandlers {
  return {
    onRows(payload) {
      const boundSession = payload.session || '';
      if (options.getCurrentSession() !== boundSession) {
        options.clearExpandedCallIds();
      }
      options.setCurrentSession(boundSession);

      const scrollEl = options.scrollContainer;
      const wasAtBottom = scrollEl
        ? scrollEl.scrollHeight - scrollEl.scrollTop - scrollEl.clientHeight < 40
        : false;
      const initialScrollTop = scrollEl ? scrollEl.scrollTop : 0;

      options.drawRows(payload.rows);
      options.drawAsk(payload.ask);
      options.drawRefs(payload.refs);

      if (scrollEl) {
        if (wasAtBottom) {
          scrollEl.scrollTop = scrollEl.scrollHeight;
        } else {
          scrollEl.scrollTop = initialScrollTop;
          const maxScroll = Math.max(0, scrollEl.scrollHeight - scrollEl.clientHeight);
          if (scrollEl.scrollTop > maxScroll) scrollEl.scrollTop = maxScroll;
        }
      }
      if (options.getNoteText() === 'sending…') {
        options.setNoteText('');
      }
    },
    onCompose(payload) {
      options.inputAdapter.handleCompose(payload.text);
    },
    onMentions(payload) {
      options.inputAdapter.handleMentions(payload.files, payload.reqId, payload.target);
    },
    onSuggestion(payload) {
      options.inputAdapter.handleSuggestion(payload.text, payload.reqId, payload.target);
    },
    onReplyResult(payload) {
      options.inputAdapter.handleReplyResult(payload, options.getCurrentAsk());
    },
    onState(m) {
      options.drawState(m.note);
    },
    onInfo(payload) {
      options.drawInfo(payload);
    },
    onNote(payload) {
      options.setNoteText(payload.text || '');
    },
  };
}

export interface ClassifiedDiffLine {
  text: string;
  cls: string;
}

export function classifyDiffLines(lines: string[]): ClassifiedDiffLine[] {
  const result: ClassifiedDiffLine[] = [];
  let inHunk = false;
  let oldRemaining = 0;
  let newRemaining = 0;

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    let cls = 'diff-plain';
    const hunkMatch = /^@@\s+-(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s+@@(?:$|\s)/.exec(rawLine);
    const isGitHeader = rawLine.startsWith('diff --git ') || rawLine.startsWith('Index: ');

    if (isGitHeader) {
      inHunk = false;
      oldRemaining = 0;
      newRemaining = 0;
      cls = 'diff-file-header';
    } else if (hunkMatch) {
      inHunk = true;
      oldRemaining = hunkMatch[2] !== undefined ? parseInt(hunkMatch[2], 10) : 1;
      newRemaining = hunkMatch[4] !== undefined ? parseInt(hunkMatch[4], 10) : 1;
      cls = 'diff-hunk-header';
      if (oldRemaining === 0 && newRemaining === 0) {
        inHunk = false;
      }
    } else if (inHunk) {
      if (rawLine.startsWith('+')) {
        cls = 'diff-added';
        if (newRemaining > 0) newRemaining--;
      } else if (rawLine.startsWith('-')) {
        cls = 'diff-deleted';
        if (oldRemaining > 0) oldRemaining--;
      } else if (rawLine.startsWith(' ') || rawLine === '') {
        cls = 'diff-context';
        if (oldRemaining > 0) oldRemaining--;
        if (newRemaining > 0) newRemaining--;
      } else if (rawLine.startsWith('\\')) {
        cls = 'diff-context';
      } else {
        cls = 'diff-plain';
      }
      if (oldRemaining <= 0 && newRemaining <= 0) {
        inHunk = false;
      }
    } else {
      const isFileMeta = (
        rawLine.startsWith('--- ') ||
        rawLine.startsWith('+++ ') ||
        rawLine.startsWith('index ') ||
        rawLine.startsWith('new file mode ') ||
        rawLine.startsWith('deleted file mode ') ||
        rawLine.startsWith('similarity index ') ||
        rawLine.startsWith('rename from ') ||
        rawLine.startsWith('rename to ') ||
        rawLine.startsWith('old mode ') ||
        rawLine.startsWith('new mode ') ||
        rawLine.startsWith('Binary files ')
      );
      if (isFileMeta) {
        cls = 'diff-file-header';
      } else if (rawLine.startsWith('\\')) {
        cls = 'diff-context';
      } else {
        cls = 'diff-plain';
      }
    }

    result.push({ text: rawLine, cls });
  }

  return result;
}

/**
 * Renders Markdown into a DOM container using purely safe DOM methods
 * (createElement, createTextNode, appendChild). Never touches innerHTML.
 */
export function renderMarkdown(
  container: HTMLElement,
  markdown: string,
  options?: { document?: Document }
): void {
  const doc = options?.document || container.ownerDocument || (typeof document !== 'undefined' ? document : null);
  if (!doc) return;

  container.textContent = '';
  if (!markdown) return;

  const lines = markdown.split(/\r?\n/);
  let i = 0;

  function renderInline(target: Node, text: string): void {
    // 1. Code: `...`
    // 2. Bold italic: ***...***
    // 3. Bold: **...**
    // 4. Italic: *...*
    // 5. Strikethrough: ~~...~~
    // 6. Link: [...](...)
    const inlineRegex = /(`[^`\n]+`)|(\*\*\*[^*]+\*\*\*)|(\*\*[^*]+\*\*)|(\*[^*\s][^*]*\*)|(~~[^~]+~~)|(\[([^[\]]*)\]\(([^)]*)\))/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;

    while ((match = inlineRegex.exec(text)) !== null) {
      if (match.index > lastIndex) {
        target.appendChild(doc.createTextNode(text.slice(lastIndex, match.index)));
      }
      const fullMatch = match[0];

      if (match[1]) {
        const code = doc.createElement('code');
        code.textContent = fullMatch.slice(1, -1);
        target.appendChild(code);
      } else if (match[2]) {
        const strong = doc.createElement('strong');
        const em = doc.createElement('em');
        renderInline(em, fullMatch.slice(3, -3));
        strong.appendChild(em);
        target.appendChild(strong);
      } else if (match[3]) {
        const strong = doc.createElement('strong');
        renderInline(strong, fullMatch.slice(2, -2));
        target.appendChild(strong);
      } else if (match[4]) {
        const em = doc.createElement('em');
        renderInline(em, fullMatch.slice(1, -1));
        target.appendChild(em);
      } else if (match[5]) {
        const del = doc.createElement('del');
        renderInline(del, fullMatch.slice(2, -2));
        target.appendChild(del);
      } else if (match[6]) {
        const linkText = match[7] || '';
        const linkHref = match[8] || '';
        const isSafeScheme = /^(https?:|mailto:|command:|#|\/|\.)/i.test(linkHref) && !/^\s*javascript:/i.test(linkHref);
        if (isSafeScheme) {
          const a = doc.createElement('a');
          a.href = linkHref;
          a.target = '_blank';
          a.rel = 'noreferrer noopener';
          renderInline(a, linkText || linkHref);
          target.appendChild(a);
        } else {
          target.appendChild(doc.createTextNode(fullMatch));
        }
      }
      lastIndex = match.index + fullMatch.length;
    }

    if (lastIndex < text.length) {
      target.appendChild(doc.createTextNode(text.slice(lastIndex)));
    }
  }

  function isTableDivider(line: string): boolean {
    const trimmed = line.trim();
    if (!trimmed.startsWith('|') || !trimmed.endsWith('|')) return false;
    const parts = trimmed.slice(1, -1).split('|');
    return parts.length > 0 && parts.every(p => /^[\s:-]+$/.test(p) && p.includes('-'));
  }

  while (i < lines.length) {
    const line = lines[i];

    // Fenced code block (``` or ~~~ with length matching)
    const fenceMatch = /^(\s*)(`{3,}|~{3,})(.*)$/.exec(line);
    if (fenceMatch) {
      const fenceChar = fenceMatch[2][0];
      const fenceLen = fenceMatch[2].length;
      const lang = fenceMatch[3].trim().split(/\s+/)[0];
      const codeLines: string[] = [];
      i++;
      while (i < lines.length) {
        const closeRegex = new RegExp('^\\s*\\' + fenceChar + '{' + fenceLen + ',}\\s*$');
        if (closeRegex.test(lines[i])) {
          i++;
          break;
        }
        codeLines.push(lines[i]);
        i++;
      }
      const pre = doc.createElement('pre');
      if (lang) {
        pre.dataset.lang = lang;
      }
      const code = doc.createElement('code');
      const isDiff = lang === 'diff' || lang === 'patch';
      if (isDiff) {
        const classified = classifyDiffLines(codeLines);
        for (let j = 0; j < classified.length; j++) {
          const item = classified[j];
          const span = doc.createElement('span');
          span.className = 'diff-line ' + item.cls;
          span.textContent = item.text + (j < classified.length - 1 ? '\n' : '');
          code.appendChild(span);
        }
      } else {
        code.textContent = codeLines.join('\n');
      }
      pre.appendChild(code);
      container.appendChild(pre);
      continue;
    }

    // Horizontal rule: ---, ***, ___
    if (/^(\s*[-*_]\s*){3,}$/.test(line)) {
      container.appendChild(doc.createElement('hr'));
      i++;
      continue;
    }

    // Heading: # H1 ~ ###### H6
    const headingMatch = /^(#{1,6})\s+(.*)$/.exec(line);
    if (headingMatch) {
      const level = headingMatch[1].length;
      const tag = 'h' + level;
      const h = doc.createElement(tag);
      renderInline(h, headingMatch[2]);
      container.appendChild(h);
      i++;
      continue;
    }

    // Blockquote: > ...
    if (line.trimStart().startsWith('>')) {
      const quoteLines: string[] = [];
      while (i < lines.length && lines[i].trimStart().startsWith('>')) {
        const qLine = lines[i].trimStart().slice(1);
        quoteLines.push(qLine.startsWith(' ') ? qLine.slice(1) : qLine);
        i++;
      }
      const bq = doc.createElement('blockquote');
      for (let qIdx = 0; qIdx < quoteLines.length; qIdx++) {
        if (qIdx > 0) bq.appendChild(doc.createElement('br'));
        renderInline(bq, quoteLines[qIdx]);
      }
      container.appendChild(bq);
      continue;
    }

    // Table: | col1 | col2 |
    if (line.trim().startsWith('|') && line.trim().endsWith('|') && i + 1 < lines.length && isTableDivider(lines[i + 1])) {
      const headerCells = line.trim().slice(1, -1).split('|').map(s => s.trim());
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].trim().startsWith('|') && lines[i].trim().endsWith('|')) {
        rows.push(lines[i].trim().slice(1, -1).split('|').map(s => s.trim()));
        i++;
      }
      const table = doc.createElement('table');
      const thead = doc.createElement('thead');
      const headerTr = doc.createElement('tr');
      for (const hc of headerCells) {
        const th = doc.createElement('th');
        renderInline(th, hc);
        headerTr.appendChild(th);
      }
      thead.appendChild(headerTr);
      table.appendChild(thead);

      if (rows.length > 0) {
        const tbody = doc.createElement('tbody');
        for (const row of rows) {
          const tr = doc.createElement('tr');
          for (let c = 0; c < headerCells.length; c++) {
            const td = doc.createElement('td');
            renderInline(td, row[c] || '');
            tr.appendChild(td);
          }
          tbody.appendChild(tr);
        }
        table.appendChild(tbody);
      }
      container.appendChild(table);
      continue;
    }

    // Lists: unordered or ordered (supports nesting and start numbers)
    const isUnordered = /^(\s*)([-*+])\s+(.*)$/.exec(line);
    const isOrdered = /^(\s*)(\d+)\.\s+(.*)$/.exec(line);
    if (isUnordered || isOrdered) {
      interface RawItem {
        indent: number;
        isOrdered: boolean;
        start?: number;
        text: string;
      }
      const items: RawItem[] = [];
      while (i < lines.length) {
        const u = /^(\s*)([-*+])\s+(.*)$/.exec(lines[i]);
        const o = /^(\s*)(\d+)\.\s+(.*)$/.exec(lines[i]);
        if (u) {
          items.push({ indent: u[1].length, isOrdered: false, text: u[3] });
          i++;
        } else if (o) {
          items.push({
            indent: o[1].length,
            isOrdered: true,
            start: parseInt(o[2], 10),
            text: o[3],
          });
          i++;
        } else {
          break;
        }
      }

      interface StackItem {
        indent: number;
        listEl: HTMLElement;
        lastLi: HTMLElement;
      }

      const rootList = doc.createElement(items[0].isOrdered ? 'ol' : 'ul');
      if (items[0].isOrdered && items[0].start !== undefined && items[0].start !== 1) {
        rootList.setAttribute('start', String(items[0].start));
        (rootList as HTMLOListElement).start = items[0].start;
      }
      const firstLi = doc.createElement('li');
      renderInline(firstLi, items[0].text);
      rootList.appendChild(firstLi);
      container.appendChild(rootList);

      const stack: StackItem[] = [
        { indent: items[0].indent, listEl: rootList, lastLi: firstLi },
      ];

      for (let k = 1; k < items.length; k++) {
        const item = items[k];
        if (item.indent > stack[stack.length - 1].indent) {
          const childList = doc.createElement(item.isOrdered ? 'ol' : 'ul');
          if (item.isOrdered && item.start !== undefined && item.start !== 1) {
            childList.setAttribute('start', String(item.start));
            (childList as HTMLOListElement).start = item.start;
          }
          const li = doc.createElement('li');
          renderInline(li, item.text);
          childList.appendChild(li);
          stack[stack.length - 1].lastLi.appendChild(childList);
          stack.push({ indent: item.indent, listEl: childList, lastLi: li });
        } else {
          while (stack.length > 1 && stack[stack.length - 1].indent > item.indent) {
            stack.pop();
          }
          const current = stack[stack.length - 1];
          const expectedTag = item.isOrdered ? 'OL' : 'UL';
          if (current.listEl.tagName === expectedTag) {
            const li = doc.createElement('li');
            renderInline(li, item.text);
            current.listEl.appendChild(li);
            current.lastLi = li;
          } else {
            const siblingList = doc.createElement(expectedTag);
            if (item.isOrdered && item.start !== undefined && item.start !== 1) {
              siblingList.setAttribute('start', String(item.start));
              (siblingList as HTMLOListElement).start = item.start;
            }
            const li = doc.createElement('li');
            renderInline(li, item.text);
            siblingList.appendChild(li);
            if (stack.length > 1) {
              stack[stack.length - 2].lastLi.appendChild(siblingList);
            } else {
              container.appendChild(siblingList);
            }
            stack[stack.length - 1] = { indent: item.indent, listEl: siblingList, lastLi: li };
          }
        }
      }
      continue;
    }

    // Blank line
    if (!line.trim()) {
      i++;
      continue;
    }

    // Paragraph: collect consecutive non-blank lines that are not special block starts
    const p = doc.createElement('p');
    let pLineCount = 0;
    while (
      i < lines.length &&
      lines[i].trim() &&
      !/^(\s*)(`{3,}|~{3,})/.test(lines[i]) &&
      !lines[i].trimStart().startsWith('>') &&
      !/^(#{1,6})\s+/.test(lines[i]) &&
      !/^(\s*[-*_]\s*){3,}$/.test(lines[i]) &&
      !(lines[i].trim().startsWith('|') && lines[i].trim().endsWith('|') && i + 1 < lines.length && isTableDivider(lines[i + 1])) &&
      !/^\s*[-*+]\s+/.test(lines[i]) &&
      !/^\s*\d+\.\s+/.test(lines[i])
    ) {
      if (pLineCount > 0) {
        p.appendChild(doc.createElement('br'));
      }
      renderInline(p, lines[i]);
      pLineCount++;
      i++;
    }
    container.appendChild(p);
  }
}

