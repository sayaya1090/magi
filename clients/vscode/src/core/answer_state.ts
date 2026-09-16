/**
 * Answer and draft state manager for chat webview.
 *
 * Invariant: Question drafts, general drafts, and attempt IDs are isolated.
 * Pure logic without DOM or VS Code dependencies.
 */

export interface InFlightReply {
  attemptId: number;
  text: string;
  version: number;
  companionKey?: string;
  session?: string;
  generation?: number;
  webviewId?: string;
}

export interface ContextSwitchOptions {
  currentInputText?: string;
  activeAsk?: AskEvent | null;
  generation?: number;
  webviewId?: string;
  creationTaskId?: string;
}

export interface ContextSwitchResult {
  companionKey: string;
  sessionId: string;
  nextInputText: string;
  enterAnswerMode?: boolean;
  exitAnswerMode?: boolean;
  callId?: string;
  label?: string;
  clearAutoCompletion: boolean;
}

export interface AnswerStateSnapshot {
  pendingQuestion: string | null;
  generalDraft: string;
  questionDrafts: Record<string, string>;
  draftVersions: Record<string, number>;
  failedDrafts: Record<string, string[]>;
  inFlightReplies: Record<string, InFlightReply>;
  replyAttemptSeq: number;
  companionKey?: string;
  sessionId?: string;
  generation?: number;
  webviewId?: string;
}

export interface AskEvent {
  kind: 'question' | 'permission';
  callId: string;
  what?: string;
  options?: string[];
  [key: string]: unknown;
}

export interface ReplyResultEvent {
  callId: string;
  attemptId?: unknown;
  ok: boolean;
  text?: string;
  error?: string;
  companionKey?: string;
  session?: string;
  generation?: number;
  webviewId?: string;
}

export interface SubmitResult {
  ok: boolean;
  action?: 'reply' | 'say';
  callId?: string;
  text?: string;
  attemptId?: number;
  companionKey?: string;
  session?: string;
  generation?: number;
  webviewId?: string;
  error?: string;
  message?: string;
  exitAnswerMode?: boolean;
  nextInputText?: string;
  clearAutoCompletion?: boolean;
}

export interface ModeChangeResult {
  enterAnswerMode?: boolean;
  exitAnswerMode?: boolean;
  callId?: string;
  label?: string;
  nextInputText?: string;
  clearAutoCompletion: boolean;
}

export interface ReplyResultOutcome {
  handled: boolean;
  ok?: boolean;
  callId?: string;
  reason?: string;
  reenterAnswerMode?: boolean;
  targetLabel?: string;
  nextInputText?: string;
  clearAutoCompletion?: boolean;
  restoredInStoreOnly?: boolean;
  companionKey?: string;
  session?: string;
}

export interface CreationTaskInfo {
  companionKey: string;
  creationTaskId: string;
  webviewId?: string;
  draft: string;
  status: 'pending' | 'completed' | 'failed';
}

export interface AnswerStateManager {
  getState(companionKey?: string, sessionId?: string): AnswerStateSnapshot;
  getPendingQuestion(companionKey?: string, sessionId?: string): string | null;
  getGeneralDraft(companionKey?: string, sessionId?: string): string;
  getQuestionDraft(callId: string, companionKey?: string, sessionId?: string): string;
  isInFlight(callId: string, companionKey?: string, sessionId?: string): boolean;
  getInFlight(callId: string, companionKey?: string, sessionId?: string): InFlightReply | undefined;
  getDraftVersion(callId: string, companionKey?: string, sessionId?: string): number;
  getFailedDrafts(callId: string, companionKey?: string, sessionId?: string): string[];
  getCurrentContext(): { companionKey: string; sessionId: string; generation?: number; webviewId?: string };
  switchContext(
    companionKey: string,
    sessionId: string,
    options?: ContextSwitchOptions
  ): ContextSwitchResult;
  registerCreationTask(
    companionKey: string,
    creationTaskId: string,
    webviewId?: string,
    draft?: string
  ): boolean;
  updateCreationTaskDraft(
    companionKey: string,
    creationTaskId: string,
    draft: string
  ): boolean;
  getCreationTask(
    companionKey: string,
    creationTaskId: string
  ): CreationTaskInfo | undefined;
  failCreationTask(
    companionKey: string,
    creationTaskId: string,
    error?: string
  ): boolean;
  bindUnconfirmedSession(
    companionKey: string,
    newSessionId: string,
    creationTaskId?: string,
    webviewId?: string
  ): boolean;
  enterAnswerMode(callId: string, label?: string, currentInputText?: string): ModeChangeResult;
  exitAnswerMode(currentInputText?: string): ModeChangeResult;
  onAskChange(a: AskEvent | null | undefined, currentInputText?: string): ModeChangeResult;
  onInputChange(text: string): { target: string };
  onTabAccept(suggestion: string, currentInputText: string): { nextInputText: string; target: string };
  onCompose(lead: string, currentInputText: string): { nextInputText: string; leadLength: number; target: string };
  submitReply(callId: string, text: string, isChoice?: boolean): SubmitResult;
  submitSay(text: string): SubmitResult;
  onReplyResult(m: ReplyResultEvent, currentActiveAsk?: AskEvent | null): ReplyResultOutcome;
}

interface SessionDraftState {
  generalDraft: string;
  pendingQuestion: string | null;
  questionDrafts: Record<string, string>;
  draftVersions: Record<string, number>;
  failedDrafts: Record<string, string[]>;
  inFlightReplies: Record<string, InFlightReply>;
}

function makeContextKey(companionKey: string, sessionId: string): string {
  return JSON.stringify([companionKey || '', sessionId || '']);
}

export function createAnswerState(): AnswerStateManager {
  const contexts = new Map<string, SessionDraftState>();
  const creationTasks = new Map<string, CreationTaskInfo>();
  const attemptToContext = new Map<number, {
    companionKey: string;
    sessionId: string;
    callId: string;
    generation: number;
    webviewId: string;
  }>();
  let currentCompanionKey = '';
  let currentSessionId = '';
  let currentGeneration: number | undefined = undefined;
  let currentWebviewId = '';
  let replyAttemptSeq = 0;

  function makeTaskKey(companionKey: string, creationTaskId: string): string {
    return JSON.stringify([companionKey || '', creationTaskId || '']);
  }

  function registerCreationTask(
    companionKey: string,
    creationTaskId: string,
    webviewId?: string,
    draft?: string
  ): boolean {
    if (!creationTaskId || typeof creationTaskId !== 'string' || creationTaskId.trim().length === 0) {
      return false;
    }
    const key = makeTaskKey(companionKey, creationTaskId);
    if (creationTasks.has(key)) {
      return false;
    }
    creationTasks.set(key, {
      companionKey: companionKey || '',
      creationTaskId,
      webviewId,
      draft: draft ?? '',
      status: 'pending',
    });
    return true;
  }

  function updateCreationTaskDraft(
    companionKey: string,
    creationTaskId: string,
    draft: string
  ): boolean {
    if (!creationTaskId || typeof creationTaskId !== 'string' || creationTaskId.trim().length === 0) {
      return false;
    }
    const key = makeTaskKey(companionKey, creationTaskId);
    const task = creationTasks.get(key);
    if (!task || task.status !== 'pending') {
      return false;
    }
    task.draft = draft;
    return true;
  }

  function getCreationTask(
    companionKey: string,
    creationTaskId: string
  ): CreationTaskInfo | undefined {
    const key = makeTaskKey(companionKey, creationTaskId);
    const task = creationTasks.get(key);
    if (!task) return undefined;
    return { ...task };
  }

  function failCreationTask(
    companionKey: string,
    creationTaskId: string,
    _error?: string
  ): boolean {
    if (!creationTaskId || typeof creationTaskId !== 'string' || creationTaskId.trim().length === 0) {
      return false;
    }
    const key = makeTaskKey(companionKey, creationTaskId);
    const task = creationTasks.get(key);
    if (!task || task.status !== 'pending') {
      return false;
    }
    task.status = 'failed';
    return true;
  }

  function getSessionState(companionKey: string, sessionId: string): SessionDraftState {
    const key = makeContextKey(companionKey, sessionId);
    let s = contexts.get(key);
    if (!s) {
      s = {
        generalDraft: '',
        pendingQuestion: null,
        questionDrafts: {},
        draftVersions: {},
        failedDrafts: {},
        inFlightReplies: {}
      };
      contexts.set(key, s);
    }
    return s;
  }

  function currentSessionState(): SessionDraftState {
    return getSessionState(currentCompanionKey, currentSessionId);
  }

  function getCurrentContext(): { companionKey: string; sessionId: string; generation?: number; webviewId?: string } {
    return {
      companionKey: currentCompanionKey,
      sessionId: currentSessionId,
      generation: currentGeneration,
      webviewId: currentWebviewId,
    };
  }

  function getState(companionKey?: string, sessionId?: string): AnswerStateSnapshot {
    const s = (companionKey !== undefined && sessionId !== undefined)
      ? getSessionState(companionKey, sessionId)
      : currentSessionState();
    const compKey = companionKey !== undefined ? companionKey : currentCompanionKey;
    const sessId = sessionId !== undefined ? sessionId : currentSessionId;

    const qDrafts: Record<string, string> = {};
    for (const k of Object.keys(s.questionDrafts)) {
      qDrafts[k] = s.questionDrafts[k];
    }
    const dVersions: Record<string, number> = {};
    for (const k of Object.keys(s.draftVersions)) {
      dVersions[k] = s.draftVersions[k];
    }
    const fDrafts: Record<string, string[]> = {};
    for (const k of Object.keys(s.failedDrafts)) {
      fDrafts[k] = s.failedDrafts[k].slice();
    }
    const infReplies: Record<string, InFlightReply> = {};
    for (const k of Object.keys(s.inFlightReplies)) {
      infReplies[k] = { ...s.inFlightReplies[k] };
    }

    return {
      pendingQuestion: s.pendingQuestion,
      generalDraft: s.generalDraft,
      questionDrafts: qDrafts,
      draftVersions: dVersions,
      failedDrafts: fDrafts,
      inFlightReplies: infReplies,
      replyAttemptSeq,
      companionKey: compKey,
      sessionId: sessId,
      generation: currentGeneration,
      webviewId: currentWebviewId,
    };
  }

  function getPendingQuestion(companionKey?: string, sessionId?: string): string | null {
    const s = (companionKey !== undefined && sessionId !== undefined)
      ? getSessionState(companionKey, sessionId)
      : currentSessionState();
    return s.pendingQuestion;
  }

  function getGeneralDraft(companionKey?: string, sessionId?: string): string {
    const s = (companionKey !== undefined && sessionId !== undefined)
      ? getSessionState(companionKey, sessionId)
      : currentSessionState();
    return s.generalDraft;
  }

  function getQuestionDraft(callId: string, companionKey?: string, sessionId?: string): string {
    const s = (companionKey !== undefined && sessionId !== undefined)
      ? getSessionState(companionKey, sessionId)
      : currentSessionState();
    return s.questionDrafts[callId] || '';
  }

  function isInFlight(callId: string, companionKey?: string, sessionId?: string): boolean {
    const s = (companionKey !== undefined && sessionId !== undefined)
      ? getSessionState(companionKey, sessionId)
      : currentSessionState();
    return !!s.inFlightReplies[callId];
  }

  function getInFlight(callId: string, companionKey?: string, sessionId?: string): InFlightReply | undefined {
    const s = (companionKey !== undefined && sessionId !== undefined)
      ? getSessionState(companionKey, sessionId)
      : currentSessionState();
    const inFlight = s.inFlightReplies[callId];
    return inFlight ? { ...inFlight } : undefined;
  }

  function getDraftVersion(callId: string, companionKey?: string, sessionId?: string): number {
    const s = (companionKey !== undefined && sessionId !== undefined)
      ? getSessionState(companionKey, sessionId)
      : currentSessionState();
    return s.draftVersions[callId] || 0;
  }

  function getFailedDrafts(callId: string, companionKey?: string, sessionId?: string): string[] {
    const s = (companionKey !== undefined && sessionId !== undefined)
      ? getSessionState(companionKey, sessionId)
      : currentSessionState();
    return s.failedDrafts[callId] ? s.failedDrafts[callId].slice() : [];
  }

  function switchContext(
    companionKey: string,
    sessionId: string,
    options?: ContextSwitchOptions
  ): ContextSwitchResult {
    const prevSess = currentSessionId;
    const targetComp = companionKey || '';
    const targetSess = sessionId || '';

    // 1. 기존 입력 저장
    const currentText = options?.currentInputText;
    const oldState = currentSessionState();
    if (currentText !== undefined) {
      if (oldState.pendingQuestion) {
        oldState.draftVersions[oldState.pendingQuestion] =
          (oldState.draftVersions[oldState.pendingQuestion] || 0) + 1;
        oldState.questionDrafts[oldState.pendingQuestion] = currentText;
      } else {
        oldState.generalDraft = currentText;
        if (prevSess === '' && options?.creationTaskId) {
          const key = makeTaskKey(currentCompanionKey, options.creationTaskId);
          const task = creationTasks.get(key);
          if (task && task.status === 'pending') {
            task.draft = currentText;
          } else if (!task) {
            creationTasks.set(key, {
              companionKey: currentCompanionKey,
              creationTaskId: options.creationTaskId,
              webviewId: options.webviewId || currentWebviewId,
              draft: currentText,
              status: 'pending',
            });
          }
        }
      }
    }

    // 2. 문맥 전환 (일반 switchContext에서는 미확정 초안을 자동 이전하지 않음 - §4.5 Item 3)
    currentCompanionKey = targetComp;
    currentSessionId = targetSess;
    if (options?.generation !== undefined) {
      currentGeneration = options.generation;
    }
    if (options?.webviewId !== undefined) {
      currentWebviewId = options.webviewId;
    }

    // 3. 새 문맥 상태 복원 및 실제 대기 질문으로 답변 모드 결정
    const newState = currentSessionState();
    const activeAsk = options?.activeAsk;

    if (activeAsk && activeAsk.kind === 'question') {
      const isFreeText = !activeAsk.options || activeAsk.options.length === 0;
      if (newState.pendingQuestion === activeAsk.callId || isFreeText) {
        newState.pendingQuestion = activeAsk.callId;
        return {
          companionKey: currentCompanionKey,
          sessionId: currentSessionId,
          enterAnswerMode: true,
          callId: activeAsk.callId,
          label: activeAsk.what || activeAsk.callId,
          nextInputText: newState.questionDrafts[activeAsk.callId] || '',
          clearAutoCompletion: true
        };
      } else {
        newState.pendingQuestion = null;
        return {
          companionKey: currentCompanionKey,
          sessionId: currentSessionId,
          exitAnswerMode: true,
          nextInputText: newState.generalDraft,
          clearAutoCompletion: true
        };
      }
    } else {
      newState.pendingQuestion = null;
      return {
        companionKey: currentCompanionKey,
        sessionId: currentSessionId,
        exitAnswerMode: true,
        nextInputText: newState.generalDraft,
        clearAutoCompletion: true
      };
    }
  }

  function enterAnswerMode(callId: string, label?: string, currentInputText?: string): ModeChangeResult {
    if (currentInputText === undefined) currentInputText = '';
    const s = currentSessionState();
    if (s.pendingQuestion !== callId) {
      if (!s.pendingQuestion) {
        s.generalDraft = currentInputText;
      } else {
        s.questionDrafts[s.pendingQuestion] = currentInputText;
      }
      s.pendingQuestion = callId;
    }
    return {
      enterAnswerMode: true,
      callId,
      label: label || callId,
      nextInputText: s.questionDrafts[callId] || '',
      clearAutoCompletion: true
    };
  }

  function exitAnswerMode(currentInputText?: string): ModeChangeResult {
    if (currentInputText === undefined) currentInputText = '';
    const s = currentSessionState();
    if (s.pendingQuestion) {
      s.questionDrafts[s.pendingQuestion] = currentInputText;
      s.pendingQuestion = null;
    }
    return {
      exitAnswerMode: true,
      nextInputText: s.generalDraft,
      clearAutoCompletion: true
    };
  }

  function onAskChange(a: AskEvent | null | undefined, currentInputText?: string): ModeChangeResult {
    if (currentInputText === undefined) currentInputText = '';
    const s = currentSessionState();
    if (!a) {
      if (s.pendingQuestion) {
        return exitAnswerMode(currentInputText);
      }
      return { clearAutoCompletion: false };
    }
    if (a.kind === 'permission') {
      if (s.pendingQuestion) {
        return exitAnswerMode(currentInputText);
      }
      return { clearAutoCompletion: false };
    }
    if (s.pendingQuestion && s.pendingQuestion !== a.callId) {
      return exitAnswerMode(currentInputText);
    }
    return { clearAutoCompletion: false };
  }

  function onInputChange(text: string): { target: string } {
    const s = currentSessionState();
    if (s.pendingQuestion) {
      s.draftVersions[s.pendingQuestion] = (s.draftVersions[s.pendingQuestion] || 0) + 1;
      s.questionDrafts[s.pendingQuestion] = text;
      return { target: s.pendingQuestion };
    }
    s.generalDraft = text;
    return { target: 'general' };
  }

  function onTabAccept(suggestion: string, currentInputText: string): { nextInputText: string; target: string } {
    const v = currentInputText + suggestion;
    const s = currentSessionState();
    if (s.pendingQuestion) {
      s.draftVersions[s.pendingQuestion] = (s.draftVersions[s.pendingQuestion] || 0) + 1;
      s.questionDrafts[s.pendingQuestion] = v;
      return { nextInputText: v, target: s.pendingQuestion };
    }
    s.generalDraft = v;
    return { nextInputText: v, target: 'general' };
  }

  function onCompose(lead: string, currentInputText: string): { nextInputText: string; leadLength: number; target: string } {
    const v = lead + currentInputText;
    const s = currentSessionState();
    if (s.pendingQuestion) {
      s.draftVersions[s.pendingQuestion] = (s.draftVersions[s.pendingQuestion] || 0) + 1;
      s.questionDrafts[s.pendingQuestion] = v;
      return { nextInputText: v, leadLength: lead.length, target: s.pendingQuestion };
    }
    s.generalDraft = v;
    return { nextInputText: v, leadLength: lead.length, target: 'general' };
  }

  function bindUnconfirmedSession(
    companionKey: string,
    newSessionId: string,
    creationTaskId?: string,
    webviewId?: string
  ): boolean {
    if (!creationTaskId || typeof creationTaskId !== 'string' || creationTaskId.trim().length === 0) {
      return false;
    }
    const compKey = companionKey || '';
    const newSess = newSessionId || '';
    if (!newSess.trim()) return false;

    const taskKey = makeTaskKey(compKey, creationTaskId);
    const task = creationTasks.get(taskKey);
    if (!task) {
      return false;
    }
    if (task.status !== 'pending') {
      return false;
    }
    if (webviewId && task.webviewId && task.webviewId !== webviewId) {
      return false;
    }

    const targetState = getSessionState(compKey, newSess);
    if (targetState.generalDraft && targetState.generalDraft.length > 0) {
      return false;
    }

    targetState.generalDraft = task.draft;
    task.status = 'completed';

    const emptyState = contexts.get(makeContextKey(compKey, ''));
    if (emptyState && emptyState.generalDraft === task.draft) {
      emptyState.generalDraft = '';
    }
    return true;
  }

  function submitReply(callId: string, text: string, isChoice?: boolean): SubmitResult {
    const t = (text || '').trim();
    if (!t) return { ok: false, error: 'empty' };
    const s = currentSessionState();
    if (s.inFlightReplies[callId]) {
      return { ok: false, error: 'in_flight', message: 'reply already in flight…' };
    }
    const attemptId = ++replyAttemptSeq;
    const ver = isChoice ? (s.draftVersions[callId] || 0) + 1 : (s.draftVersions[callId] || 0);
    if (isChoice) s.draftVersions[callId] = ver;
    const finalText = isChoice ? text : t;
    const inFlightRecord: InFlightReply = {
      attemptId,
      text: finalText,
      version: ver,
      companionKey: currentCompanionKey,
      session: currentSessionId,
      generation: currentGeneration ?? 0,
      webviewId: currentWebviewId,
    };
    s.inFlightReplies[callId] = inFlightRecord;
    s.questionDrafts[callId] = finalText;
    attemptToContext.set(attemptId, {
      companionKey: currentCompanionKey,
      sessionId: currentSessionId,
      callId,
      generation: currentGeneration ?? 0,
      webviewId: currentWebviewId,
    });

    const wasAnswering = s.pendingQuestion === callId;
    if (wasAnswering) {
      s.pendingQuestion = null;
    }

    return {
      ok: true,
      action: 'reply',
      callId,
      text: finalText,
      attemptId,
      companionKey: currentCompanionKey,
      session: currentSessionId,
      generation: currentGeneration ?? 0,
      webviewId: currentWebviewId,
      exitAnswerMode: wasAnswering,
      nextInputText: s.generalDraft,
      clearAutoCompletion: true
    };
  }

  function submitSay(text: string): SubmitResult {
    const t = (text || '').trim();
    if (!t) return { ok: false, error: 'empty' };
    const s = currentSessionState();
    s.generalDraft = '';
    return {
      ok: true,
      action: 'say',
      text: t,
      companionKey: currentCompanionKey,
      session: currentSessionId,
      generation: currentGeneration,
      webviewId: currentWebviewId,
      nextInputText: '',
      clearAutoCompletion: true
    };
  }

  function onReplyResult(m: ReplyResultEvent, currentActiveAsk?: AskEvent | null): ReplyResultOutcome {
    if (typeof m.attemptId !== 'number') {
      return { handled: false, reason: 'missing_or_invalid_attempt_id' };
    }
    const attemptMeta = attemptToContext.get(m.attemptId);
    if (!attemptMeta) {
      return { handled: false, reason: 'mismatched_attempt_id' };
    }
    if (attemptMeta.callId !== m.callId) {
      return { handled: false, reason: 'mismatched_attempt_id' };
    }
    if (m.companionKey !== undefined && m.companionKey !== attemptMeta.companionKey) {
      return { handled: false, reason: 'context_mismatch' };
    }
    if (m.session !== undefined && m.session !== attemptMeta.sessionId) {
      return { handled: false, reason: 'context_mismatch' };
    }
    if (m.generation !== undefined && m.generation !== attemptMeta.generation) {
      return { handled: false, reason: 'generation_mismatch' };
    }
    if (m.webviewId !== undefined && m.webviewId !== attemptMeta.webviewId) {
      return { handled: false, reason: 'webview_mismatch' };
    }

    const targetState = getSessionState(attemptMeta.companionKey, attemptMeta.sessionId);
    const inFlight = targetState.inFlightReplies[m.callId];
    if (!inFlight || inFlight.attemptId !== m.attemptId) {
      return { handled: false, reason: 'mismatched_attempt_id' };
    }
    if (m.generation !== undefined && inFlight.generation !== m.generation) {
      return { handled: false, reason: 'generation_mismatch' };
    }
    if (m.webviewId !== undefined && inFlight.webviewId !== m.webviewId) {
      return { handled: false, reason: 'webview_mismatch' };
    }

    delete targetState.inFlightReplies[m.callId];
    attemptToContext.delete(m.attemptId);

    const isCurrentContext =
      attemptMeta.companionKey === currentCompanionKey &&
      attemptMeta.sessionId === currentSessionId &&
      attemptMeta.webviewId === currentWebviewId &&
      (currentGeneration === undefined || attemptMeta.generation === currentGeneration);

    const currentVer = targetState.draftVersions[m.callId] || 0;
    if (m.ok) {
      if (inFlight.version === currentVer) {
        delete targetState.questionDrafts[m.callId];
        delete targetState.failedDrafts[m.callId];
      }
      return {
        handled: true,
        ok: true,
        callId: m.callId,
        companionKey: attemptMeta.companionKey,
        session: attemptMeta.sessionId
      };
    }

    if (!targetState.failedDrafts[m.callId]) targetState.failedDrafts[m.callId] = [];
    targetState.failedDrafts[m.callId].push(m.text || '');

    const modifiedSinceAttempt = currentVer > inFlight.version;
    if (!modifiedSinceAttempt) {
      targetState.questionDrafts[m.callId] = m.text || targetState.questionDrafts[m.callId] || '';
      if (isCurrentContext && currentActiveAsk && currentActiveAsk.callId === m.callId) {
        targetState.pendingQuestion = m.callId;
        return {
          handled: true,
          ok: false,
          callId: m.callId,
          reenterAnswerMode: true,
          targetLabel: currentActiveAsk.what || m.callId,
          nextInputText: targetState.questionDrafts[m.callId],
          clearAutoCompletion: true,
          companionKey: attemptMeta.companionKey,
          session: attemptMeta.sessionId
        };
      }
    }
    return {
      handled: true,
      ok: false,
      callId: m.callId,
      restoredInStoreOnly: true,
      companionKey: attemptMeta.companionKey,
      session: attemptMeta.sessionId
    };
  }

  return {
    getState,
    getPendingQuestion,
    getGeneralDraft,
    getQuestionDraft,
    isInFlight,
    getInFlight,
    getDraftVersion,
    getFailedDrafts,
    getCurrentContext,
    switchContext,
    registerCreationTask,
    updateCreationTaskDraft,
    getCreationTask,
    failCreationTask,
    bindUnconfirmedSession,
    enterAnswerMode,
    exitAnswerMode,
    onAskChange,
    onInputChange,
    onTabAccept,
    onCompose,
    submitReply,
    submitSay,
    onReplyResult
  };
}
