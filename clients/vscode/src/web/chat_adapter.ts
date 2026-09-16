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
} from '../core/webview_protocol';
import type { Ask } from '../core/touched';
import type { Activity } from '../core/activity';
import type { AnswerStateManager, AskEvent } from '../core/answer_state';
import type { RecoveryItem } from '../core/recovery_state';

export interface WebviewBridge {
  postMessage(message: WebviewToHostMessage): void;
}

export interface WebviewActionAdapter {
  openFile(session: string, callId: string, seq?: number): boolean;
  openDiff(session: string, callId: string): boolean;
  openOutput(session: string, outputId: string): boolean;
  answer(callId: string, decision: string): boolean;
  reply(
    callId: string,
    text: string,
    attemptId: number,
    context: { companionKey: string; session: string; generation: number; webviewId: string }
  ): boolean;
  say(text: string, creationTaskId?: string): boolean;
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

    openOutput(session: string, outputId: string): boolean {
      if (!session || !outputId) return false;
      vs.postMessage({ kind: 'output', session, outputId });
      return true;
    },

    answer(callId: string, decision: string): boolean {
      if (!callId || !decision) return false;
      vs.postMessage({ kind: 'answer', callId, decision });
      return true;
    },

    reply(
      callId: string,
      text: string,
      attemptId: number,
      context: { companionKey: string; session: string; generation: number; webviewId: string }
    ): boolean {
      if (
        !callId ||
        callId.trim().length === 0 ||
        typeof attemptId !== 'number' ||
        !Number.isInteger(attemptId) ||
        attemptId <= 0 ||
        !context
      ) {
        return false;
      }
      if (
        !context.companionKey ||
        context.companionKey.trim().length === 0 ||
        !context.session ||
        context.session.trim().length === 0 ||
        typeof context.generation !== 'number' ||
        !Number.isInteger(context.generation) ||
        context.generation < 0 ||
        !context.webviewId ||
        context.webviewId.trim().length === 0
      ) {
        return false;
      }
      vs.postMessage({
        kind: 'reply',
        callId,
        text,
        attemptId,
        companionKey: context.companionKey,
        session: context.session,
        generation: context.generation,
        webviewId: context.webviewId,
      });
      return true;
    },

    say(text: string, creationTaskId?: string): boolean {
      const trimmed = text.trim();
      if (!trimmed) return false;
      const msg: WebviewToHostMessage = { kind: 'say', text: trimmed };
      if (creationTaskId && typeof creationTaskId === 'string' && creationTaskId.trim().length > 0) {
        msg.creationTaskId = creationTaskId;
      }
      vs.postMessage(msg);
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
  onRows?(payload: {
    session: string;
    rows: PaintedRow[];
    ask: Ask | null;
    refs: string[];
    companionKey?: string;
    generation?: number;
    webviewId?: string;
  }): void;
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
  onSessionCreated?(payload: {
    companionKey: string;
    session: string;
    creationTaskId: string;
    webviewId: string;
  }): void;
  onSessionCreationFailed?(payload: {
    companionKey: string;
    creationTaskId: string;
    webviewId: string;
    error?: string;
  }): void;
  onReplyResult?(payload: {
    callId: string;
    attemptId: number;
    ok: boolean;
    companionKey: string;
    session: string;
    generation: number;
    webviewId: string;
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
        typeof rowObj.text !== 'string' ||
        (rowObj.outputId !== undefined && typeof rowObj.outputId !== 'string')
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

    const rowsMsg: HostToWebviewMessage = {
      kind: 'rows',
      session: m.session,
      rows,
      ask,
      refs: m.refs as string[],
    };
    if (typeof m.companionKey === 'string') (rowsMsg as any).companionKey = m.companionKey;
    if (typeof m.generation === 'number') (rowsMsg as any).generation = m.generation;
    if (typeof m.webviewId === 'string') (rowsMsg as any).webviewId = m.webviewId;
    return rowsMsg;
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
  } else if (kind === 'sessionCreated') {
    if (
      typeof m.companionKey !== 'string' ||
      m.companionKey.trim().length === 0 ||
      typeof m.session !== 'string' ||
      m.session.trim().length === 0 ||
      typeof m.creationTaskId !== 'string' ||
      m.creationTaskId.trim().length === 0 ||
      typeof m.webviewId !== 'string' ||
      m.webviewId.trim().length === 0
    ) {
      return undefined;
    }
    return {
      kind: 'sessionCreated',
      companionKey: m.companionKey,
      session: m.session,
      creationTaskId: m.creationTaskId,
      webviewId: m.webviewId,
    };
  } else if (kind === 'sessionCreationFailed') {
    if (
      typeof m.companionKey !== 'string' ||
      m.companionKey.trim().length === 0 ||
      typeof m.creationTaskId !== 'string' ||
      m.creationTaskId.trim().length === 0 ||
      typeof m.webviewId !== 'string' ||
      m.webviewId.trim().length === 0
    ) {
      return undefined;
    }
    return {
      kind: 'sessionCreationFailed',
      companionKey: m.companionKey,
      creationTaskId: m.creationTaskId,
      webviewId: m.webviewId,
      error: typeof m.error === 'string' ? m.error : undefined,
    };
  } else if (kind === 'replyResult') {
    if (
      typeof m.callId !== 'string' ||
      m.callId.trim().length === 0 ||
      typeof m.attemptId !== 'number' ||
      !Number.isInteger(m.attemptId) ||
      m.attemptId <= 0 ||
      typeof m.ok !== 'boolean' ||
      typeof m.companionKey !== 'string' ||
      m.companionKey.trim().length === 0 ||
      typeof m.session !== 'string' ||
      m.session.trim().length === 0 ||
      typeof m.generation !== 'number' ||
      !Number.isInteger(m.generation) ||
      m.generation < 0 ||
      typeof m.webviewId !== 'string' ||
      m.webviewId.trim().length === 0
    ) {
      return undefined;
    }
    return {
      kind: 'replyResult',
      callId: m.callId,
      attemptId: m.attemptId,
      ok: m.ok,
      companionKey: m.companionKey,
      session: m.session,
      generation: m.generation,
      webviewId: m.webviewId,
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
  } else if (m.kind === 'sessionCreated') {
    handlers.onSessionCreated?.(m);
    return true;
  } else if (m.kind === 'sessionCreationFailed') {
    handlers.onSessionCreationFailed?.(m);
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

export interface SuggestController {
  invalidate(): void;
  onSessionChange(session: string): void;
  scheduleInput(options: {
    text: string;
    target: string;
    actions: WebviewActionAdapter;
    delayMs?: number;
  }): number;
  acceptMentions(files: string[], reqId?: number, target?: string): boolean;
  acceptSuggestion(text: string, reqId?: number, target?: string): boolean;
  clearSuggestion(): void;
  getSuggestion(): string;
  getMentions(): string[];
  getReqId(): number;
  getCurrentTarget(): string;
  getCurrentSession(): string;
  dispose(): void;
}

export function createSuggestController(): SuggestController {
  let reqId = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let currentSession = '';
  let activeTarget = 'general';
  let activeSuggestion = '';
  let activeMentions: string[] = [];

  function invalidate(): void {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    reqId++;
    activeSuggestion = '';
    activeMentions = [];
  }

  function onSessionChange(session: string): void {
    if (currentSession !== session) {
      currentSession = session;
      invalidate();
      activeTarget = 'general';
    }
  }

  function scheduleInput(options: {
    text: string;
    target: string;
    actions: WebviewActionAdapter;
    delayMs?: number;
  }): number {
    invalidate();
    activeTarget = options.target;
    const thisReqId = reqId;
    const target = options.target;
    const text = options.text;
    const delay = options.delayMs ?? 450;

    const at = /(^|\s)@([^\s@]{2,})$/.exec(text);
    timer = setTimeout(() => {
      if (at) {
        options.actions.mention(at[2], thisReqId, target);
      } else if (text.trim().length > 3) {
        options.actions.suggest(text, thisReqId, target);
      }
    }, delay);

    return thisReqId;
  }

  function acceptMentions(files: string[], rId?: number, target?: string): boolean {
    if (rId !== undefined && rId !== reqId) return false;
    if (target !== undefined && target !== activeTarget) return false;
    activeMentions = files || [];
    return true;
  }

  function acceptSuggestion(text: string, rId?: number, target?: string): boolean {
    if (rId !== undefined && rId !== reqId) return false;
    if (target !== undefined && target !== activeTarget) return false;
    activeSuggestion = text || '';
    return true;
  }

  return {
    invalidate,
    onSessionChange,
    scheduleInput,
    acceptMentions,
    acceptSuggestion,
    clearSuggestion(): void {
      activeSuggestion = '';
    },
    getSuggestion: () => activeSuggestion,
    getMentions: () => activeMentions,
    getReqId: () => reqId,
    getCurrentTarget: () => activeTarget,
    getCurrentSession: () => currentSession,
    dispose(): void {
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
    },
  };
}

export interface SessionCreatedResult {
  accepted: boolean;
  transitioned: boolean;
  conflict: boolean;
}

export interface WebviewInputAdapter {
  enterAnswerMode(callId: string, label?: string): void;
  exitAnswerMode(): void;
  applyAnswerModeUI(label?: string, text?: string): void;
  applyGeneralModeUI(text?: string): void;
  clearAutoCompletion(): void;
  onSessionChange(session: string): void;
  onContextChange(
    companionKey: string,
    session: string,
    activeAsk?: Ask | null,
    generation?: number,
    webviewId?: string
  ): void;
  onSessionCreated?(payload: {
    companionKey: string;
    session: string;
    creationTaskId: string;
    webviewId: string;
  }): SessionCreatedResult;
  onSessionCreationFailed?(payload: {
    companionKey: string;
    creationTaskId: string;
    webviewId: string;
    error?: string;
  }): void;
  send(): void;
  submitChoice(callId: string, option: string): boolean;
  handleCompose(text: string): void;
  handleMentions(files: string[], reqId?: number, target?: string): void;
  handleSuggestion(text: string, reqId?: number, target?: string): void;
  handleReplyResult(
    m: {
      callId: string;
      attemptId: number;
      ok: boolean;
      companionKey: string;
      session: string;
      generation: number;
      webviewId: string;
      error?: string;
      text?: string;
    },
    currentAsk: Ask | null
  ): void;
  getSuggestReqId(): number;
  getSuggestController(): SuggestController;
  getActiveCreationTaskId?(): string | null;
  getCurrentSession?(): string;
  getCurrentCompanionKey?(): string;
  dispose(): void;
}

export function createWebviewInputAdapter(
  elements: WebviewInputElements,
  actions: WebviewActionAdapter,
  answerState: AnswerStateManager,
  suggestController?: SuggestController
): WebviewInputAdapter {
  const { say, sendBtn, replyModeEl, replyTargetEl, replyCancelEl, noteEl, hintEl } = elements;
  const suggestCtrl = suggestController ?? createSuggestController();
  let currentCompanionKey = '';
  let currentSession = '';
  let currentGeneration: number | undefined = undefined;
  let currentWebviewId = '';
  let creationSeq = 0;
  let activeCreationTaskId: string | null = null;

  function clearAutoCompletion(): void {
    suggestCtrl.invalidate();
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
    const doc = say ? say.ownerDocument : (typeof document !== 'undefined' ? document : null);
    const inRecovery = !!(
      doc &&
      doc.activeElement &&
      typeof doc.activeElement.closest === 'function' &&
      doc.activeElement.closest('#recovery-panel')
    );
    if (!inRecovery) {
      say.focus();
    }
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
    if (activeCreationTaskId && !currentSession) {
      answerState.updateCreationTaskDraft(currentCompanionKey, activeCreationTaskId, say.value);
    }
    const res = answerState.enterAnswerMode(callId, label, say.value);
    applyAnswerModeUI(res.label, res.nextInputText);
  }

  function exitAnswerMode(): void {
    const res = answerState.exitAnswerMode(say.value);
    applyGeneralModeUI(res.nextInputText);
    if (activeCreationTaskId && !currentSession) {
      answerState.updateCreationTaskDraft(currentCompanionKey, activeCreationTaskId, res.nextInputText || '');
    }
  }

  function onContextChange(
    companionKey: string,
    session: string,
    activeAsk?: Ask | null,
    generation?: number,
    webviewId?: string
  ): void {
    const prevComp = currentCompanionKey;
    const prevSess = currentSession;
    const prevWebviewId = currentWebviewId;
    const isDifferent =
      prevComp !== companionKey ||
      prevSess !== session ||
      (Boolean(webviewId) && prevWebviewId !== webviewId);

    currentCompanionKey = companionKey || '';
    currentSession = session || '';
    currentGeneration = generation;
    if (webviewId) currentWebviewId = webviewId;

    suggestCtrl.onSessionChange(currentSession);
    clearAutoCompletion();

    if (isDifferent) {
      const res = answerState.switchContext(currentCompanionKey, currentSession, {
        currentInputText: say.value,
        activeAsk: activeAsk as unknown as AskEvent | null,
        generation,
        webviewId: currentWebviewId,
        creationTaskId: (prevSess === '' && activeCreationTaskId) ? activeCreationTaskId : undefined,
      });
      if (res.enterAnswerMode) {
        applyAnswerModeUI(res.label, res.nextInputText);
      } else {
        applyGeneralModeUI(res.nextInputText);
      }
    }
  }

  function onSessionCreated(payload: {
    companionKey: string;
    session: string;
    creationTaskId: string;
    webviewId: string;
  }): SessionCreatedResult {
    // 1. Context validation: companionKey and webviewId must match if current values are known
    if (
      (currentWebviewId && payload.webviewId !== currentWebviewId) ||
      (currentCompanionKey && payload.companionKey !== currentCompanionKey)
    ) {
      return { accepted: false, transitioned: false, conflict: false };
    }

    // Ensure active creation task draft is synced with current say.value if still pending in empty session in general mode
    const pendingQBefore = answerState.getPendingQuestion();
    const isAnswerModeBefore = Boolean(pendingQBefore) || Boolean(replyModeEl && !replyModeEl.hidden);
    if (!isAnswerModeBefore && activeCreationTaskId && activeCreationTaskId === payload.creationTaskId && currentSession === '') {
      answerState.updateCreationTaskDraft(currentCompanionKey, activeCreationTaskId, say.value);
    }

    // 2. Process completion via answerState
    const res = answerState.bindUnconfirmedSession(
      payload.companionKey,
      payload.session,
      payload.creationTaskId,
      payload.webviewId
    );

    if (!res.ok) {
      // Rejection: keep activeCreationTaskId, currentSession, AnswerState, DOM input, mode, and autocompletion completely untouched!
      return { accepted: false, transitioned: false, conflict: false };
    }

    // Valid completion:
    if (activeCreationTaskId === payload.creationTaskId) {
      activeCreationTaskId = null;
    }

    let transitioned = false;
    const isCurrentEmptySession =
      currentCompanionKey === payload.companionKey &&
      currentSession === '';

    if (isCurrentEmptySession) {
      currentSession = payload.session;
      transitioned = true;

      // 문맥 전환 때문에 입력 대상이 달라진 경우 자동완성·대기 타이머를 무효화 (§4.5 Item 2)
      suggestCtrl.onSessionChange(currentSession);
      clearAutoCompletion();

      const pendingQ = answerState.getPendingQuestion();
      const isAnswerMode = Boolean(pendingQ) || Boolean(replyModeEl && !replyModeEl.hidden);
      const activeAskEvent = pendingQ ? { kind: 'question' as const, callId: pendingQ } : null;

      const switchRes = answerState.switchContext(currentCompanionKey, currentSession, {
        currentInputText: isAnswerMode ? say.value : undefined,
        activeAsk: activeAskEvent,
        webviewId: currentWebviewId,
      });

      if (isAnswerMode || switchRes.enterAnswerMode) {
        // 답변 모드에서는 일반 초안을 삽입하지 않으며 질문 초안과 모드가 유지돼야 함 (§4.5 Item 2)
        if (pendingQ) {
          answerState.onInputChange(say.value);
        }
      } else {
        // 일반 모드: 빈 문맥에서 생성 결과 세션으로 전환할 때 현재 입력이 비어 있는지와 무관하게 대상 문맥의 입력을 렌더링 (§4.5 Item 1)
        // 일반 모드 충돌이면 EXISTING을 표시하고 NEW DRAFT는 완료된 생성 작업 자료로 보존 (§4.5 Item 1)
        const displayText = res.conflict
          ? (res.existingDraft ?? switchRes.nextInputText ?? '')
          : (res.draft ?? switchRes.nextInputText ?? '');
        applyGeneralModeUI(displayText);
      }
    }

    return { accepted: true, transitioned, conflict: res.conflict };
  }

  function onSessionCreationFailed(payload: {
    companionKey: string;
    creationTaskId: string;
    webviewId: string;
    error?: string;
  }): void {
    if (
      (currentWebviewId && payload.webviewId !== currentWebviewId) ||
      (currentCompanionKey && payload.companionKey !== currentCompanionKey)
    ) {
      return;
    }
    answerState.failCreationTask(payload.companionKey, payload.creationTaskId, payload.error);
    if (activeCreationTaskId === payload.creationTaskId) {
      activeCreationTaskId = null;
    }
  }

  function onSessionChange(session: string): void {
    onContextChange(currentCompanionKey, session);
  }

  function send(): void {
    const t = say.value.trim();
    if (!t) return;
    const pending = answerState.getPendingQuestion();
    if (pending) {
      const replyTitle = (replyTargetEl?.textContent || '').trim() || undefined;
      const res = answerState.submitReply(pending, t, false, replyTitle);
      if (!res.ok) {
        if (res.error === 'in_flight' && noteEl) {
          noteEl.textContent = 'reply already in flight…';
        }
        return;
      }
      clearAutoCompletion();
      if (typeof res.attemptId === 'number') {
        const comp = res.companionKey || currentCompanionKey;
        const sess = res.session || currentSession;
        const gen = res.generation !== undefined ? res.generation : (currentGeneration ?? 0);
        const wid = res.webviewId || currentWebviewId;
        actions.reply(res.callId ?? pending, res.text ?? t, res.attemptId, {
          companionKey: comp,
          session: sess,
          generation: gen,
          webviewId: wid,
        });
      }
      if (res.exitAnswerMode) {
        applyGeneralModeUI(res.nextInputText);
      }
    } else {
      let creationTaskId: string | undefined = undefined;
      if (!currentSession) {
        if (!activeCreationTaskId) {
          creationTaskId = 'create-' + (++creationSeq);
          activeCreationTaskId = creationTaskId;
          answerState.registerCreationTask(
            currentCompanionKey,
            creationTaskId,
            currentWebviewId,
            ''
          );
        } else {
          creationTaskId = activeCreationTaskId;
          answerState.updateCreationTaskDraft(currentCompanionKey, activeCreationTaskId, '');
        }
      }
      const res = answerState.submitSay(t);
      if (!res.ok) return;
      clearAutoCompletion();
      actions.say(res.text ?? t, creationTaskId);
      say.value = res.nextInputText ?? '';
      if (activeCreationTaskId && !currentSession) {
        answerState.updateCreationTaskDraft(currentCompanionKey, activeCreationTaskId, '');
      }
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
    const replyTitle = (replyTargetEl?.textContent || '').trim() || undefined;
    const res = answerState.submitReply(callId, option, true, replyTitle);
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
      const comp = res.companionKey || currentCompanionKey;
      const sess = res.session || currentSession;
      const gen = res.generation !== undefined ? res.generation : (currentGeneration ?? 0);
      const wid = res.webviewId || currentWebviewId;
      actions.reply(callId, option, res.attemptId, {
        companionKey: comp,
        session: sess,
        generation: gen,
        webviewId: wid,
      });
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
    clearAutoCompletion();
    const lead = text || '';
    say.value = lead + say.value;
    answerState.onInputChange(say.value);
    if (activeCreationTaskId && !currentSession) {
      answerState.updateCreationTaskDraft(currentCompanionKey, activeCreationTaskId, say.value);
    }
    say.focus();
    say.setSelectionRange(lead.length, lead.length);
  }

  function handleMentions(files: string[], reqId?: number, target?: string): void {
    const currentTarget = answerState.getPendingQuestion() || 'general';
    if (!suggestCtrl.acceptMentions(files, reqId, target ?? currentTarget)) return;
    const mentions = suggestCtrl.getMentions();
    if (hintEl) {
      hintEl.textContent = mentions.length ? 'files: ' + mentions.slice(0, 6).join('  ') : '';
    }
  }

  function handleSuggestion(text: string, reqId?: number, target?: string): void {
    const currentTarget = answerState.getPendingQuestion() || 'general';
    if (!suggestCtrl.acceptSuggestion(text, reqId, target ?? currentTarget)) return;
    const suggestion = suggestCtrl.getSuggestion();
    if (hintEl) {
      hintEl.textContent = suggestion ? 'Tab: ' + suggestion.split('\n')[0].slice(0, 60) : '';
    }
  }

  function handleReplyResult(
    m: {
      callId: string;
      attemptId: number;
      ok: boolean;
      companionKey: string;
      session: string;
      generation: number;
      webviewId: string;
      error?: string;
      text?: string;
    },
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
    const suggestion = suggestCtrl.getSuggestion();
    if (e.key === 'Tab' && suggestion) {
      e.preventDefault();
      say.value += suggestion;
      answerState.onInputChange(say.value);
      if (activeCreationTaskId && !currentSession) {
        answerState.updateCreationTaskDraft(currentCompanionKey, activeCreationTaskId, say.value);
      }
      clearAutoCompletion();
    }
  };

  const onInput = (): void => {
    if (hintEl) hintEl.textContent = '';
    const v = say.value;
    const target = answerState.onInputChange(v).target;
    if (activeCreationTaskId && !currentSession) {
      answerState.updateCreationTaskDraft(currentCompanionKey, activeCreationTaskId, v);
    }
    suggestCtrl.scheduleInput({
      text: v,
      target,
      actions,
    });
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
    onSessionChange,
    onContextChange,
    onSessionCreated,
    onSessionCreationFailed,
    send,
    submitChoice,
    handleCompose,
    handleMentions,
    handleSuggestion,
    handleReplyResult,
    getSuggestReqId: () => suggestCtrl.getReqId(),
    getSuggestController: () => suggestCtrl,
    getActiveCreationTaskId: () => activeCreationTaskId,
    getCurrentSession: () => currentSession,
    getCurrentCompanionKey: () => currentCompanionKey,
    dispose(): void {
      suggestCtrl.dispose();
      say.removeEventListener('keydown', onKeyDown);
      say.removeEventListener('input', onInput);
      sendBtn.removeEventListener('click', onSendClick);
      replyCancelEl?.removeEventListener('click', onCancelClick);
    },
  };
}

export interface RecoveryElements {
  recoveryBtn: HTMLElement;
  recoveryPanel: HTMLElement;
  recoveryItemsEl: HTMLElement;
  recoveryScopeAll?: HTMLInputElement | null;
  recoveryStatus?: HTMLElement | null;
  say: HTMLTextAreaElement;
}

export interface RecoveryControllerOptions {
  elements: RecoveryElements;
  answerState: AnswerStateManager;
  inputAdapter: WebviewInputAdapter;
  getCurrentCompanionKey: () => string;
  getCurrentSession: () => string;
  document?: { createElement(tag: string): any };
}

export interface RecoveryController {
  refresh(): void;
  open(): void;
  close(): void;
  toggle(): void;
  isOpen(): boolean;
  setScopeAll(all: boolean): void;
  getScopeAll(): boolean;
  toggleFullText(recoveryId: string): void;
  isFullTextOpen(recoveryId: string): boolean;
  copyDraft(recoveryId: string): boolean;
  confirmAppend(recoveryId: string): boolean;
  cancelConfirm(recoveryId: string): void;
  deleteItem(recoveryId: string): boolean;
  setComposing(composing: boolean): void;
  isComposing(): boolean;
  getPendingConfirmId(): string | null;
  onContextChange(companionKey: string, sessionId: string): void;
  dispose(): void;
}

interface RenderedItemEntry {
  root: HTMLElement;
  metaEl: HTMLElement;
  titleEl: HTMLElement;
  reasonEl: HTMLElement;
  previewEl: HTMLElement;
  actsEl: HTMLElement;
  fullBtn: HTMLButtonElement;
  copyBtn: HTMLButtonElement;
  delBtn: HTMLButtonElement;
  fullPre: HTMLPreElement | null;
  confirmBox: HTMLElement | null;
  appendBtn: HTMLButtonElement | null;
  cancelBtn: HTMLButtonElement | null;
}

export function createWebviewRecoveryController(options: RecoveryControllerOptions): RecoveryController {
  const { elements, answerState, inputAdapter, getCurrentCompanionKey, getCurrentSession } = options;
  const { recoveryBtn, recoveryPanel, recoveryItemsEl, recoveryScopeAll, recoveryStatus, say } = elements;
  const doc = options.document || (recoveryItemsEl && (recoveryItemsEl as any).ownerDocument) || (typeof document !== 'undefined' ? document : undefined);

  const renderedItems = new Map<string, RenderedItemEntry>();
  let emptyEl: HTMLElement | null = null;
  let panelOpen = false;
  let scopeAll = false;
  let isComposing = false;
  const openFullTexts = new Set<string>();
  let pendingConfirmId: string | null = null;
  let confirmSession: string = '';
  let confirmCompanion: string = '';

  function setStatus(msg: string): void {
    if (recoveryStatus) {
      recoveryStatus.textContent = msg;
    }
  }

  function getVisibleItems(): RecoveryItem[] {
    const compKey = getCurrentCompanionKey();
    const sessId = getCurrentSession();
    if (!scopeAll) {
      return answerState.listRecoveryItems({
        companionKey: compKey,
        sessionId: sessId,
      });
    } else {
      return answerState.listRecoveryItems({
        companionKey: compKey,
        includeOtherSessions: true,
      });
    }
  }

  function updateButtonsDisabled(): void {
    for (const entry of renderedItems.values()) {
      entry.copyBtn.disabled = isComposing;
      if (entry.appendBtn) {
        entry.appendBtn.disabled = isComposing;
      }
    }
  }

  function refresh(): void {
    const currentComp = getCurrentCompanionKey();
    const currentSess = getCurrentSession();

    // Cancel pending confirm if context changed (§4.6.3)
    if (pendingConfirmId !== null && (confirmCompanion !== currentComp || confirmSession !== currentSess)) {
      pendingConfirmId = null;
    }

    const items = getVisibleItems();
    recoveryBtn.textContent = '복구 초안 ' + items.length;
    recoveryBtn.setAttribute('aria-label', '복구 초안 ' + items.length + '개');
    if (recoveryScopeAll) {
      recoveryScopeAll.checked = scopeAll;
    }

    if (!panelOpen) {
      recoveryPanel.hidden = true;
      recoveryBtn.setAttribute('aria-expanded', 'false');
      return;
    }

    recoveryPanel.hidden = false;
    recoveryBtn.setAttribute('aria-expanded', 'true');

    if (!doc) return;

    // Pre-refresh capture of active element and selection within recoveryItemsEl (§4.6)
    let focusedRecoveryId: string | null = null;
    let focusedAction: 'full' | 'copy' | 'del' | 'append' | 'cancel' | null = null;
    const activeEl = doc ? (doc.activeElement as HTMLElement | null) : null;

    if (activeEl && (activeEl === recoveryItemsEl || (typeof recoveryItemsEl.contains === 'function' && recoveryItemsEl.contains(activeEl)))) {
      for (const [id, entry] of renderedItems) {
        if (entry.root === activeEl || (typeof entry.root.contains === 'function' && entry.root.contains(activeEl))) {
          focusedRecoveryId = id;
          if (activeEl === entry.fullBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('fulltext-btn'))) {
            focusedAction = 'full';
          } else if (activeEl === entry.copyBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('copy-btn'))) {
            focusedAction = 'copy';
          } else if (activeEl === entry.delBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('delete-btn'))) {
            focusedAction = 'del';
          } else if (activeEl === entry.appendBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('confirm-append-btn'))) {
            focusedAction = 'append';
          } else if (activeEl === entry.cancelBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('confirm-cancel-btn'))) {
            focusedAction = 'cancel';
          }
          break;
        }
      }
    }

    interface CapturedSelection {
      recoveryId: string;
      startOffset: number;
      endOffset: number;
    }
    let capturedSelection: CapturedSelection | null = null;

    const win = doc ? ((doc as any).defaultView || (typeof window !== 'undefined' ? window : null)) : null;
    const sel = win && typeof win.getSelection === 'function' ? win.getSelection() : null;
    if (sel && sel.rangeCount > 0 && !sel.isCollapsed && sel.anchorNode && sel.focusNode) {
      if (typeof recoveryItemsEl.contains === 'function' && (recoveryItemsEl.contains(sel.anchorNode) || recoveryItemsEl.contains(sel.focusNode))) {
        for (const [id, entry] of renderedItems) {
          if (entry.fullPre && typeof entry.root.contains === 'function' && entry.root.contains(sel.anchorNode)) {
            try {
              const range = sel.getRangeAt(0);
              const pre = entry.fullPre;
              if (typeof doc.createRange === 'function') {
                const preRange = doc.createRange();
                preRange.selectNodeContents(pre);
                preRange.setEnd(range.startContainer, range.startOffset);
                const start = preRange.toString().length;
                const len = range.toString().length;
                capturedSelection = {
                  recoveryId: id,
                  startOffset: start,
                  endOffset: start + len,
                };
              }
            } catch {
              // Ignore if selection cannot be computed
            }
            break;
          }
        }
      }
    }

    if (items.length === 0) {
      for (const entry of renderedItems.values()) {
        entry.root.remove();
      }
      renderedItems.clear();

      if (!emptyEl) {
        const el = doc.createElement('div');
        el.className = 'recovery-empty';
        el.textContent = '보관 중인 복구 초안이 없습니다.';
        emptyEl = el;
      }
      if (emptyEl && emptyEl.parentNode !== recoveryItemsEl) {
        recoveryItemsEl.append(emptyEl);
      }
      return;
    }

    if (emptyEl && emptyEl.parentNode) {
      emptyEl.remove();
    }

    // 1. Update or create entries for visible items
    for (const item of items) {
      let entry = renderedItems.get(item.recoveryId);
      if (!entry) {
        const itemEl = doc.createElement('div');
        itemEl.className = 'recovery-item';
        itemEl.dataset.recoveryId = item.recoveryId;

        const metaEl = doc.createElement('div');
        metaEl.className = 'recovery-meta';
        itemEl.append(metaEl);

        const titleEl = doc.createElement('div');
        titleEl.className = 'recovery-title';
        itemEl.append(titleEl);

        const reasonEl = doc.createElement('div');
        reasonEl.className = 'recovery-reason';
        itemEl.append(reasonEl);

        const previewEl = doc.createElement('div');
        previewEl.className = 'recovery-preview';
        itemEl.append(previewEl);

        const actsEl = doc.createElement('div');
        actsEl.className = 'recovery-actions';

        const fullBtn = doc.createElement('button');
        fullBtn.type = 'button';
        fullBtn.className = 'fulltext-btn';
        fullBtn.addEventListener('click', () => {
          toggleFullText(item.recoveryId);
        });
        actsEl.append(fullBtn);

        const copyBtn = doc.createElement('button');
        copyBtn.type = 'button';
        copyBtn.className = 'copy-btn';
        copyBtn.textContent = '일반 초안으로 복사';
        copyBtn.addEventListener('click', () => {
          if (isComposing) return;
          copyDraft(item.recoveryId);
        });
        actsEl.append(copyBtn);

        const delBtn = doc.createElement('button');
        delBtn.type = 'button';
        delBtn.className = 'delete-btn';
        delBtn.textContent = '삭제';
        delBtn.addEventListener('click', () => {
          deleteItem(item.recoveryId);
        });
        actsEl.append(delBtn);

        itemEl.append(actsEl);

        entry = {
          root: itemEl,
          metaEl,
          titleEl,
          reasonEl,
          previewEl,
          actsEl,
          fullBtn,
          copyBtn,
          delBtn,
          fullPre: null,
          confirmBox: null,
          appendBtn: null,
          cancelBtn: null,
        };
        renderedItems.set(item.recoveryId, entry);
      }

      // Update text contents only if changed (to preserve selection and minimize churn)
      const originText = item.creationTaskId
        ? '생성 작업: ' + item.creationTaskId
        : '세션: ' + (item.sessionId || '미지정');
      const metaText = originText + ' · 발생: ' + item.attempts + '회';
      if (entry.metaEl.textContent !== metaText) {
        entry.metaEl.textContent = metaText;
      }

      if (entry.titleEl.textContent !== item.title) {
        entry.titleEl.textContent = item.title;
      }

      if (entry.reasonEl.textContent !== item.reason) {
        entry.reasonEl.textContent = item.reason;
      }

      const prevText = item.text.length > 80 ? item.text.slice(0, 80) + '…' : item.text;
      if (entry.previewEl.textContent !== prevText) {
        entry.previewEl.textContent = prevText;
      }

      entry.copyBtn.disabled = isComposing;

      // Full text display toggle
      const isFullOpen = openFullTexts.has(item.recoveryId);
      const fullBtnText = isFullOpen ? '전문 닫기' : '전문 보기';
      if (entry.fullBtn.textContent !== fullBtnText) {
        entry.fullBtn.textContent = fullBtnText;
      }
      entry.fullBtn.setAttribute('aria-expanded', isFullOpen ? 'true' : 'false');

      if (isFullOpen) {
        if (!entry.fullPre) {
          const fullPre = doc.createElement('pre');
          fullPre.className = 'recovery-full-text';
          fullPre.textContent = item.text;
          entry.root.insertBefore(fullPre, entry.actsEl);
          entry.fullPre = fullPre;
        } else if (entry.fullPre.textContent !== item.text) {
          entry.fullPre.textContent = item.text;
        }
      } else if (entry.fullPre) {
        entry.fullPre.remove();
        entry.fullPre = null;
      }

      // Confirm box toggle
      const isPendingConfirm = (pendingConfirmId === item.recoveryId);
      if (isPendingConfirm) {
        if (!entry.confirmBox) {
          const confirmBox = doc.createElement('div');
          confirmBox.className = 'recovery-confirm-box';

          const msgSpan = doc.createElement('div');
          msgSpan.className = 'recovery-confirm-msg';
          msgSpan.textContent = '작성 중인 일반 초안이 있습니다. 이어 붙이시겠습니까?';
          confirmBox.append(msgSpan);

          const appendBtn = doc.createElement('button');
          appendBtn.type = 'button';
          appendBtn.className = 'confirm-append-btn';
          appendBtn.textContent = '이어 붙이기';
          appendBtn.disabled = isComposing;
          appendBtn.addEventListener('click', () => {
            if (isComposing) return;
            confirmAppend(item.recoveryId);
          });
          confirmBox.append(appendBtn);

          const cancelBtn = doc.createElement('button');
          cancelBtn.type = 'button';
          cancelBtn.className = 'confirm-cancel-btn';
          cancelBtn.textContent = '취소';
          cancelBtn.addEventListener('click', () => {
            cancelConfirm(item.recoveryId);
          });
          confirmBox.append(cancelBtn);

          entry.root.append(confirmBox);
          entry.confirmBox = confirmBox;
          entry.appendBtn = appendBtn;
          entry.cancelBtn = cancelBtn;
        } else if (entry.appendBtn) {
          entry.appendBtn.disabled = isComposing;
        }
      } else if (entry.confirmBox) {
        entry.confirmBox.remove();
        entry.confirmBox = null;
        entry.appendBtn = null;
        entry.cancelBtn = null;
      }
    }

    // 2. Remove entries no longer in visible items
    const visibleIds = new Set(items.map((it) => it.recoveryId));
    for (const [id, entry] of renderedItems) {
      if (!visibleIds.has(id)) {
        entry.root.remove();
        renderedItems.delete(id);
      }
    }

    // 3. Ensure proper order in DOM (§4.6)
    // Use state-preserving DOM move (Element.moveBefore) if available, with safe fallback to insertBefore
    const canMoveBefore = typeof (recoveryItemsEl as any).moveBefore === 'function';
    for (let i = 0; i < items.length; i++) {
      const entry = renderedItems.get(items[i].recoveryId);
      if (!entry) continue;
      const elChildren = (recoveryItemsEl.children || (recoveryItemsEl as any).childNodes || []) as unknown as HTMLElement[];
      if (elChildren[i] !== entry.root) {
        const refNode = elChildren[i] || null;
        if (canMoveBefore) {
          try {
            (recoveryItemsEl as any).moveBefore(entry.root, refNode);
          } catch {
            if (typeof recoveryItemsEl.insertBefore === 'function') {
              recoveryItemsEl.insertBefore(entry.root, refNode);
            } else {
              recoveryItemsEl.append(entry.root);
            }
          }
        } else if (typeof recoveryItemsEl.insertBefore === 'function') {
          recoveryItemsEl.insertBefore(entry.root, refNode);
        } else {
          recoveryItemsEl.append(entry.root);
        }
      }
    }

    // 4. Restore focus if it was inside a recovery item before refresh (§4.6)
    if (focusedRecoveryId && focusedAction) {
      const focusedEntry = renderedItems.get(focusedRecoveryId);
      if (focusedEntry) {
        let targetEl: HTMLElement | null = null;
        if (focusedAction === 'full') targetEl = focusedEntry.fullBtn;
        else if (focusedAction === 'copy') targetEl = focusedEntry.copyBtn;
        else if (focusedAction === 'del') targetEl = focusedEntry.delBtn;
        else if (focusedAction === 'append') targetEl = focusedEntry.appendBtn;
        else if (focusedAction === 'cancel') targetEl = focusedEntry.cancelBtn;

        if (targetEl && doc.activeElement !== targetEl && typeof targetEl.focus === 'function') {
          try {
            targetEl.focus({ preventScroll: true });
          } catch {
            targetEl.focus();
          }
        }
      }
    }

    // 5. Restore text selection if it was inside a recovery item before refresh (§4.6)
    if (capturedSelection && win && sel && doc) {
      const selEntry = renderedItems.get(capturedSelection.recoveryId);
      if (selEntry && selEntry.fullPre && typeof doc.createRange === 'function') {
        const pre = selEntry.fullPre;
        const textNode = pre.firstChild || pre;
        const textLen = textNode.textContent ? textNode.textContent.length : 0;
        const start = Math.max(0, Math.min(capturedSelection.startOffset, textLen));
        const end = Math.max(0, Math.min(capturedSelection.endOffset, textLen));
        if (start <= end && textLen > 0) {
          try {
            const newRange = doc.createRange();
            newRange.setStart(textNode, start);
            newRange.setEnd(textNode, end);
            sel.removeAllRanges();
            sel.addRange(newRange);
          } catch {
            // Ignore if range could not be applied
          }
        }
      }
    }
  }

  function toggleFullText(recoveryId: string): void {
    if (openFullTexts.has(recoveryId)) {
      openFullTexts.delete(recoveryId);
    } else {
      openFullTexts.add(recoveryId);
    }
    refresh();
  }

  function copyDraft(recoveryId: string): boolean {
    if (isComposing) return false;
    const item = answerState.getRecoveryItem(recoveryId);
    if (!item) return false;

    const compKey = getCurrentCompanionKey();
    const sessId = getCurrentSession();
    const res = answerState.applyRecoveryDraft({
      recoveryId,
      companionKey: compKey,
      sessionId: sessId,
      currentInputText: say.value,
      append: false,
    });

    if (!res.ok) {
      if (res.reason === 'requires_confirm') {
        pendingConfirmId = recoveryId;
        confirmSession = sessId;
        confirmCompanion = compKey;
        setStatus('이어 붙이기 확인이 필요합니다.');
        refresh();
        return true;
      }
      return false;
    }

    inputAdapter.clearAutoCompletion();
    inputAdapter.applyGeneralModeUI(res.nextInputText);
    say.focus();
    setStatus('일반 초안으로 복사되었습니다.');
    refresh();
    return true;
  }

  function confirmAppend(recoveryId: string): boolean {
    if (isComposing) return false;
    const compKey = getCurrentCompanionKey();
    const sessId = getCurrentSession();

    if (pendingConfirmId !== recoveryId || sessId !== confirmSession || compKey !== confirmCompanion) {
      pendingConfirmId = null;
      setStatus('문맥이 변경되어 복사가 취소되었습니다.');
      refresh();
      return false;
    }

    const item = answerState.getRecoveryItem(recoveryId);
    if (!item) {
      pendingConfirmId = null;
      setStatus('항목이 삭제되어 복사가 취소되었습니다.');
      refresh();
      return false;
    }

    const res = answerState.applyRecoveryDraft({
      recoveryId,
      companionKey: compKey,
      sessionId: sessId,
      currentInputText: say.value,
      append: true,
    });

    pendingConfirmId = null;
    if (!res.ok) {
      setStatus('복사 적용에 실패했습니다.');
      refresh();
      return false;
    }

    inputAdapter.clearAutoCompletion();
    inputAdapter.applyGeneralModeUI(res.nextInputText);
    say.focus();
    setStatus('일반 초안에 이어 붙였습니다.');
    refresh();
    return true;
  }

  function cancelConfirm(recoveryId: string): void {
    if (pendingConfirmId === recoveryId) {
      pendingConfirmId = null;
      setStatus('복사가 취소되었습니다.');
      refresh();
    }
  }

  function deleteItem(recoveryId: string): boolean {
    if (pendingConfirmId === recoveryId) {
      pendingConfirmId = null;
    }
    openFullTexts.delete(recoveryId);

    // §4.6.3: Focus preservation on deletion
    const activeEl = doc ? (doc.activeElement as HTMLElement | null) : null;
    const entryToDelete = renderedItems.get(recoveryId);
    let targetSiblingId: string | null = null;
    let focusRole: 'del' | 'copy' | 'full' | 'default' = 'default';
    let shouldShiftFocus = false;

    if (entryToDelete && activeEl && (activeEl === entryToDelete.root || entryToDelete.root.contains(activeEl))) {
      shouldShiftFocus = true;
      if (activeEl === entryToDelete.delBtn || activeEl.classList.contains('delete-btn')) {
        focusRole = 'del';
      } else if (activeEl === entryToDelete.copyBtn || activeEl.classList.contains('copy-btn')) {
        focusRole = 'copy';
      } else if (activeEl === entryToDelete.fullBtn || activeEl.classList.contains('fulltext-btn')) {
        focusRole = 'full';
      }

      const visible = getVisibleItems();
      const idx = visible.findIndex((it) => it.recoveryId === recoveryId);
      if (idx !== -1) {
        const sibling = (idx + 1 < visible.length)
          ? visible[idx + 1]
          : (idx - 1 >= 0 ? visible[idx - 1] : null);
        if (sibling) {
          targetSiblingId = sibling.recoveryId;
        }
      }
    }

    const deleted = answerState.deleteRecoveryItem(recoveryId);
    if (deleted) {
      setStatus('복구 초안이 삭제되었습니다.');
      refresh();

      if (shouldShiftFocus) {
        if (targetSiblingId) {
          const siblingEntry = renderedItems.get(targetSiblingId);
          if (siblingEntry) {
            if (focusRole === 'del') siblingEntry.delBtn.focus();
            else if (focusRole === 'copy') siblingEntry.copyBtn.focus();
            else if (focusRole === 'full') siblingEntry.fullBtn.focus();
            else siblingEntry.copyBtn.focus();
          } else {
            recoveryBtn.focus();
          }
        } else {
          recoveryBtn.focus();
        }
      }
    }
    return deleted;
  }

  function onBtnClick(): void {
    panelOpen = !panelOpen;
    refresh();
  }

  function onScopeChange(): void {
    if (recoveryScopeAll) {
      scopeAll = recoveryScopeAll.checked;
      refresh();
    }
  }

  function onCompositionStart(): void {
    isComposing = true;
    updateButtonsDisabled();
  }

  function onCompositionEnd(): void {
    isComposing = false;
    updateButtonsDisabled();
  }

  recoveryBtn.addEventListener('click', onBtnClick);
  if (recoveryScopeAll) {
    recoveryScopeAll.addEventListener('change', onScopeChange);
  }
  say.addEventListener('compositionstart', onCompositionStart);
  say.addEventListener('compositionend', onCompositionEnd);

  // Initial render
  refresh();

  return {
    refresh,
    open(): void {
      panelOpen = true;
      refresh();
    },
    close(): void {
      panelOpen = false;
      refresh();
    },
    toggle(): void {
      panelOpen = !panelOpen;
      refresh();
    },
    isOpen: () => panelOpen,
    setScopeAll(val: boolean): void {
      scopeAll = val;
      refresh();
    },
    getScopeAll: () => scopeAll,
    toggleFullText,
    isFullTextOpen: (id: string) => openFullTexts.has(id),
    copyDraft,
    confirmAppend,
    cancelConfirm,
    deleteItem,
    setComposing(val: boolean): void {
      isComposing = val;
      updateButtonsDisabled();
    },
    isComposing: () => isComposing,
    getPendingConfirmId: () => pendingConfirmId,
    onContextChange(_companionKey: string, _sessionId: string): void {
      if (pendingConfirmId !== null) {
        pendingConfirmId = null;
        setStatus('문맥이 변경되어 복사가 취소되었습니다.');
      }
      refresh();
    },
    dispose(): void {
      recoveryBtn.removeEventListener('click', onBtnClick);
      if (recoveryScopeAll) {
        recoveryScopeAll.removeEventListener('change', onScopeChange);
      }
      say.removeEventListener('compositionstart', onCompositionStart);
      say.removeEventListener('compositionend', onCompositionEnd);
      for (const entry of renderedItems.values()) {
        entry.root.remove();
      }
      renderedItems.clear();
      if (emptyEl && emptyEl.parentNode) {
        emptyEl.remove();
      }
    },
  };
}

export interface WebviewReceiveAdapterOptions {
  inputAdapter: WebviewInputAdapter;
  answerState: AnswerStateManager;
  recoveryController?: RecoveryController;
  getCurrentAsk: () => Ask | null;
  getCurrentSession: () => string;
  setCurrentSession: (s: string) => void;
  getCurrentCompanionKey?: () => string;
  setCurrentCompanionKey?: (k: string) => void;
  getCurrentGeneration?: () => number | undefined;
  setCurrentGeneration?: (g: number | undefined) => void;
  getCurrentWebviewId?: () => string;
  setCurrentWebviewId?: (w: string) => void;
  clearExpandedCallIds: () => void;
  resetCurrentAsk?: () => void;
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
      const boundCompanion = payload.companionKey || '';
      const boundWebviewId = payload.webviewId || '';
      const currentSession = options.getCurrentSession();
      const currentCompanion = options.getCurrentCompanionKey ? options.getCurrentCompanionKey() : '';
      const currentWebview = options.getCurrentWebviewId ? options.getCurrentWebviewId() : '';
      const contextChanged =
        currentSession !== boundSession ||
        (Boolean(options.getCurrentCompanionKey) && currentCompanion !== boundCompanion) ||
        (Boolean(boundWebviewId) && currentWebview !== boundWebviewId);

      if (contextChanged) {
        options.clearExpandedCallIds();
        options.resetCurrentAsk?.();
        options.recoveryController?.onContextChange?.(boundCompanion, boundSession);
        options.inputAdapter.onContextChange(
          boundCompanion,
          boundSession,
          payload.ask,
          payload.generation,
          boundWebviewId
        );
      }
      options.setCurrentSession(boundSession);
      options.setCurrentCompanionKey?.(boundCompanion);
      options.setCurrentGeneration?.(payload.generation);
      if (boundWebviewId) {
        options.setCurrentWebviewId?.(boundWebviewId);
      }

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
      options.recoveryController?.refresh();
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
    onSessionCreated(payload) {
      const res = options.inputAdapter.onSessionCreated?.(payload);
      if (res?.transitioned) {
        options.setCurrentSession(payload.session);
        if (payload.companionKey && options.setCurrentCompanionKey) {
          options.setCurrentCompanionKey(payload.companionKey);
        }
        if (payload.webviewId && options.setCurrentWebviewId) {
          options.setCurrentWebviewId(payload.webviewId);
        }
        options.recoveryController?.onContextChange?.(payload.companionKey, payload.session);
      }
      options.recoveryController?.refresh();
    },
    onSessionCreationFailed(payload) {
      options.inputAdapter.onSessionCreationFailed?.(payload);
      options.recoveryController?.refresh();
    },
    onReplyResult(payload) {
      options.inputAdapter.handleReplyResult(payload, options.getCurrentAsk());
      options.recoveryController?.refresh();
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

