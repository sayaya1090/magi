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
import { emptyTranscriptNote } from '../core/activity';
import type { AnswerStateManager, AskEvent } from '../core/answer_state';
import type { RecoveryController } from './recovery_controller';
import {
  Subject,
  timer,
  EMPTY,
  asyncScheduler,
  SchedulerLike,
  switchMap,
  tap,
  catchError,
  takeUntil,
} from 'rxjs';

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

import { parseHostToWebviewMessage } from '../core/webview_protocol';
export { parseHostToWebviewMessage };

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
  askControlsEl?: HTMLElement | null;
  getCurrentAsk?: () => Ask | null;
  onInFlightChange?: (state: { inFlight: boolean; answeringThisAsk: boolean }) => void;
}

export interface SuggestControllerOptions {
  scheduler?: SchedulerLike;
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

export function createSuggestController(options?: SuggestControllerOptions): SuggestController {
  let reqId = 0;
  let currentSession = '';
  let activeTarget = 'general';
  let activeSuggestion = '';
  let activeMentions: string[] = [];
  let disposed = false;

  const destroy$ = new Subject<void>();
  type SuggestCommand =
    | {
        type: 'schedule';
        reqId: number;
        text: string;
        target: string;
        actions: WebviewActionAdapter;
        delayMs: number;
      }
    | {
        type: 'invalidate';
      };

  const command$ = new Subject<SuggestCommand>();
  const scheduler: SchedulerLike = options?.scheduler ?? asyncScheduler;

  const subscription = command$
    .pipe(
      takeUntil(destroy$),
      switchMap((cmd) => {
        if (cmd.type === 'invalidate') {
          return EMPTY;
        }
        const text = cmd.text;
        const at = /(^|\s)@([^\s@]{2,})$/.exec(text);
        const meetsSuggest = !at && text.trim().length > 3;
        if (!at && !meetsSuggest) {
          return EMPTY;
        }

        return timer(cmd.delayMs, scheduler).pipe(
          tap(() => {
            if (disposed) return;
            try {
              if (at) {
                cmd.actions.mention(at[2], cmd.reqId, cmd.target);
              } else {
                cmd.actions.suggest(text, cmd.reqId, cmd.target);
              }
            } catch {
              // 오류는 해당 요청 범위에서 처리하고 다음 입력 스트림까지 종료시키지 않습니다.
            }
          }),
          catchError(() => EMPTY)
        );
      })
    )
    .subscribe();

  function invalidate(): void {
    if (disposed) return;
    reqId++;
    activeSuggestion = '';
    activeMentions = [];
    command$.next({ type: 'invalidate' });
  }

  function onSessionChange(session: string): void {
    if (disposed) return;
    if (currentSession !== session) {
      currentSession = session;
      invalidate();
      activeTarget = 'general';
    }
  }

  function scheduleInput(opts: {
    text: string;
    target: string;
    actions: WebviewActionAdapter;
    delayMs?: number;
  }): number {
    if (disposed) return reqId;
    reqId++;
    activeSuggestion = '';
    activeMentions = [];
    activeTarget = opts.target;
    const thisReqId = reqId;
    const delay = opts.delayMs ?? 450;

    command$.next({
      type: 'schedule',
      reqId: thisReqId,
      text: opts.text,
      target: opts.target,
      actions: opts.actions,
      delayMs: delay,
    });

    return thisReqId;
  }

  function acceptMentions(files: string[], rId?: number, target?: string): boolean {
    if (disposed) return false;
    if (rId !== undefined && rId !== reqId) return false;
    if (target !== undefined && target !== activeTarget) return false;
    activeMentions = files || [];
    return true;
  }

  function acceptSuggestion(text: string, rId?: number, target?: string): boolean {
    if (disposed) return false;
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
      if (disposed) return;
      disposed = true;
      destroy$.next();
      destroy$.complete();
      command$.complete();
      subscription.unsubscribe();
      activeSuggestion = '';
      activeMentions = [];
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
  updateInFlightStatus?(): void;
  dispose(): void;
}

export interface InFlightUIOptions {
  askControlsEl?: HTMLElement | null;
  sendBtn?: HTMLElement | null;
  getAsk: () => Ask | null;
  answerState: AnswerStateManager;
  getCurrentCompanionKey: () => string;
  getCurrentSession: () => string;
}

export interface InFlightUIResult {
  inFlight: boolean;
  answeringThisAsk: boolean;
}

export function updateInFlightUI(options: InFlightUIOptions): InFlightUIResult {
  const { askControlsEl, sendBtn, getAsk, answerState, getCurrentCompanionKey, getCurrentSession } = options;
  const currentAsk = getAsk ? getAsk() : null;
  const companionKey = getCurrentCompanionKey ? getCurrentCompanionKey() : '';
  const session = getCurrentSession ? getCurrentSession() : '';

  const inFlight = (currentAsk && currentAsk.kind === 'question' && currentAsk.callId)
    ? answerState.isInFlight(currentAsk.callId, companionKey, session)
    : false;

  const pendingQ = answerState.getPendingQuestion(companionKey, session);
  const answeringThisAsk = Boolean(pendingQ && currentAsk && pendingQ === currentAsk.callId);

  if (askControlsEl) {
    if (inFlight) {
      if (typeof askControlsEl.setAttribute === 'function' && askControlsEl.getAttribute('aria-busy') !== 'true') {
        askControlsEl.setAttribute('aria-busy', 'true');
      }
    } else {
      if (typeof askControlsEl.removeAttribute === 'function' && askControlsEl.hasAttribute?.('aria-busy')) {
        askControlsEl.removeAttribute('aria-busy');
      }
    }
    const statusEl = typeof askControlsEl.querySelector === 'function'
      ? (askControlsEl.querySelector('.ask-status') as HTMLElement | null)
      : null;
    if (statusEl) {
      const text = inFlight ? '답변 전송 중…' : '';
      if (statusEl.textContent !== text) {
        statusEl.textContent = text;
      }
    }
    const choiceBtns = typeof askControlsEl.querySelectorAll === 'function'
      ? askControlsEl.querySelectorAll<HTMLButtonElement>('button.choice-btn')
      : [];
    for (let i = 0; i < choiceBtns.length; i++) {
      if (choiceBtns[i].disabled !== inFlight) {
        choiceBtns[i].disabled = inFlight;
      }
    }
  }

  if (sendBtn) {
    const shouldDisableSend = inFlight && answeringThisAsk;
    const btn = sendBtn as HTMLButtonElement;
    if (btn.disabled !== shouldDisableSend) {
      btn.disabled = shouldDisableSend;
    }
  }

  return { inFlight, answeringThisAsk };
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

  function updateInFlightStatus(): void {
    const uiRes = updateInFlightUI({
      askControlsEl: elements.askControlsEl,
      sendBtn: elements.sendBtn,
      getAsk: elements.getCurrentAsk || (() => null),
      answerState,
      getCurrentCompanionKey: () => currentCompanionKey,
      getCurrentSession: () => currentSession,
    });
    if (elements.onInFlightChange) {
      elements.onInFlightChange(uiRes);
    }
  }

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
    updateInFlightStatus();
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
    updateInFlightStatus();
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
    updateInFlightStatus();
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
      updateInFlightStatus();
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
      updateInFlightStatus();
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
    updateInFlightStatus();
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
    if (res.reenterAnswerMode) {
      applyAnswerModeUI(res.targetLabel, res.nextInputText);
    }
    updateInFlightStatus();
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
    updateInFlightStatus,
    dispose(): void {
      suggestCtrl.dispose();
      say.removeEventListener('keydown', onKeyDown);
      say.removeEventListener('input', onInput);
      sendBtn.removeEventListener('click', onSendClick);
      replyCancelEl?.removeEventListener('click', onCancelClick);
    },
  };
}

export {
  RecoveryInputTarget,
  RecoveryElements,
  RecoveryControllerOptions,
  RecoveryController,
  createWebviewRecoveryController,
} from './recovery_controller';

export {
  RenderedItemEntry,
  RecoveryViewCallbacks,
  RecoveryViewState,
  RecoveryViewOptions,
  RecoveryView,
  createRecoveryView,
} from './recovery_view';

export {
  CapturedSelection,
  docHasNode,
  captureSelection,
  restoreSelection,
  restoreFocus,
  moveDomChild,
} from './dom_interaction';


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
  drawEmptyNote: (text: string) => void;
  drawInfo: (info: HostToWebviewMessage & { kind: 'info' }) => void;
  setNoteText: (text: string) => void;
  getNoteText: () => string;
  scrollContainer?: HTMLElement | null;
}

export function createWebviewReceiveHandlers(
  options: WebviewReceiveAdapterOptions
): HostMessageHandlers {
  let lastActivity: Activity | null = null;
  let lastHasRows = false;
  let lastHasAsk = false;
  /* 판정은 코어가 한다(emptyTranscriptNote). 여기서는 최신 조합을 넘길 뿐이다. */
  const paintEmptyNote = () =>
    options.drawEmptyNote(emptyTranscriptNote(lastActivity, lastHasRows, lastHasAsk));

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
      /* ⚠ 이 둘은 **어느 쪽이 먼저 와도** 된다. 한쪽만 보고 그리면 늦게 온 쪽이 반영 안 된 화면이
         남는다 — 그래서 최신 조합을 들고 있다가 매번 다시 판정한다. 세션이 바뀌면 그 세션의
         행 수로 다시 정해지므로 옛 세션의 빈 안내가 남지 않는다. */
      lastHasRows = payload.rows.length > 0;
      lastHasAsk = !!payload.ask;
      paintEmptyNote();
      options.drawRefs(payload.refs);
      options.inputAdapter.updateInFlightStatus?.();

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
        options.inputAdapter.updateInFlightStatus?.();
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
      lastActivity = m.state ?? null;
      paintEmptyNote();
    },
    onInfo(payload) {
      options.drawInfo(payload);
    },
    onNote(payload) {
      options.setNoteText(payload.text || '');
    },
  };
}

export {
  ClassifiedDiffLine,
  classifyDiffLines,
  renderMarkdown,
  isSafeUrl,
} from './markdown_render';

export interface FormattedChoiceOption {
  raw: string;
  buttonLabel: string;
}

export interface FormattedChoices {
  hideListMarker: boolean;
  items: FormattedChoiceOption[];
}

type NumberedFormat = 'dot' | 'paren' | 'bracket';

interface ParsedChoicePrefix {
  format: NumberedFormat;
  prefixLen: number;
}

function parseChoiceNumberPrefix(text: string, expectedNum: number): ParsedChoicePrefix | null {
  // Format 1: `1. 내용`
  const dotPrefix = `${expectedNum}.`;
  if (text.startsWith(dotPrefix)) {
    const rest = text.slice(dotPrefix.length);
    const m = rest.match(/^[ \t]+/);
    if (m) {
      const prefixLen = dotPrefix.length + m[0].length;
      if (text.slice(prefixLen).trim().length > 0) {
        return { format: 'dot', prefixLen };
      }
    }
  }

  // Format 2: `1) 내용`
  const parenPrefix = `${expectedNum})`;
  if (text.startsWith(parenPrefix)) {
    const rest = text.slice(parenPrefix.length);
    const m = rest.match(/^[ \t]+/);
    if (m) {
      const prefixLen = parenPrefix.length + m[0].length;
      if (text.slice(prefixLen).trim().length > 0) {
        return { format: 'paren', prefixLen };
      }
    }
  }

  // Format 3: `(1) 내용`
  const bracketPrefix = `(${expectedNum})`;
  if (text.startsWith(bracketPrefix)) {
    const rest = text.slice(bracketPrefix.length);
    const m = rest.match(/^[ \t]+/);
    if (m) {
      const prefixLen = bracketPrefix.length + m[0].length;
      if (text.slice(prefixLen).trim().length > 0) {
        return { format: 'bracket', prefixLen };
      }
    }
  }

  return null;
}

export function formatChoiceOptions(options?: readonly string[] | null): FormattedChoices {
  if (!options || !Array.isArray(options) || options.length === 0) {
    return { hideListMarker: false, items: [] };
  }

  let alreadyNumbered = true;
  let detectedFormat: NumberedFormat | null = null;
  const prefixLens: number[] = [];

  for (let i = 0; i < options.length; i++) {
    const opt = options[i];
    if (typeof opt !== 'string') {
      alreadyNumbered = false;
      break;
    }
    const parsed = parseChoiceNumberPrefix(opt, i + 1);
    if (!parsed) {
      alreadyNumbered = false;
      break;
    }
    if (i === 0) {
      detectedFormat = parsed.format;
    } else if (parsed.format !== detectedFormat) {
      alreadyNumbered = false;
      break;
    }
    prefixLens.push(parsed.prefixLen);
  }

  const items: FormattedChoiceOption[] = [];
  for (let i = 0; i < options.length; i++) {
    const raw = options[i];
    const clean = alreadyNumbered ? raw.slice(prefixLens[i]) : raw;
    const firstLine = clean.split('\n')[0].trim();
    const shortLabel = firstLine.length > 20 ? firstLine.slice(0, 19) + '…' : firstLine;
    const buttonLabel = `${i + 1}. ${shortLabel || (alreadyNumbered ? clean.trim().slice(0, 20) : raw.slice(0, 20))}`;
    items.push({ raw, buttonLabel });
  }

  return {
    hideListMarker: alreadyNumbered,
    items,
  };
}

