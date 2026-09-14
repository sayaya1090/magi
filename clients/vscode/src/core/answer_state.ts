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
}

export interface AnswerStateSnapshot {
  pendingQuestion: string | null;
  generalDraft: string;
  questionDrafts: Record<string, string>;
  draftVersions: Record<string, number>;
  failedDrafts: Record<string, string[]>;
  inFlightReplies: Record<string, InFlightReply>;
  replyAttemptSeq: number;
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
}

export interface SubmitResult {
  ok: boolean;
  action?: 'reply' | 'say';
  callId?: string;
  text?: string;
  attemptId?: number;
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
}

export interface AnswerStateManager {
  getState(): AnswerStateSnapshot;
  getPendingQuestion(): string | null;
  getGeneralDraft(): string;
  getQuestionDraft(callId: string): string;
  isInFlight(callId: string): boolean;
  getInFlight(callId: string): InFlightReply | undefined;
  getDraftVersion(callId: string): number;
  getFailedDrafts(callId: string): string[];
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

export function createAnswerState(): AnswerStateManager {
  let pendingQuestion: string | null = null;
  let generalDraft = '';
  const questionDrafts: Record<string, string> = {};
  const inFlightReplies: Record<string, InFlightReply> = {};
  const draftVersions: Record<string, number> = {};
  const failedDrafts: Record<string, string[]> = {};
  let replyAttemptSeq = 0;

  function getState(): AnswerStateSnapshot {
    return {
      pendingQuestion,
      generalDraft,
      questionDrafts: Object.assign({}, questionDrafts),
      draftVersions: Object.assign({}, draftVersions),
      failedDrafts: Object.assign({}, failedDrafts),
      inFlightReplies: Object.assign({}, inFlightReplies),
      replyAttemptSeq
    };
  }

  function getPendingQuestion(): string | null {
    return pendingQuestion;
  }

  function getGeneralDraft(): string {
    return generalDraft;
  }

  function getQuestionDraft(callId: string): string {
    return questionDrafts[callId] || '';
  }

  function isInFlight(callId: string): boolean {
    return !!inFlightReplies[callId];
  }

  function getInFlight(callId: string): InFlightReply | undefined {
    return inFlightReplies[callId];
  }

  function getDraftVersion(callId: string): number {
    return draftVersions[callId] || 0;
  }

  function getFailedDrafts(callId: string): string[] {
    return failedDrafts[callId] ? failedDrafts[callId].slice() : [];
  }

  function enterAnswerMode(callId: string, label?: string, currentInputText?: string): ModeChangeResult {
    if (currentInputText === undefined) currentInputText = '';
    if (pendingQuestion !== callId) {
      if (!pendingQuestion) {
        generalDraft = currentInputText;
      } else {
        questionDrafts[pendingQuestion] = currentInputText;
      }
      pendingQuestion = callId;
    }
    return {
      enterAnswerMode: true,
      callId,
      label: label || callId,
      nextInputText: questionDrafts[callId] || '',
      clearAutoCompletion: true
    };
  }

  function exitAnswerMode(currentInputText?: string): ModeChangeResult {
    if (currentInputText === undefined) currentInputText = '';
    if (pendingQuestion) {
      questionDrafts[pendingQuestion] = currentInputText;
      pendingQuestion = null;
    }
    return {
      exitAnswerMode: true,
      nextInputText: generalDraft,
      clearAutoCompletion: true
    };
  }

  function onAskChange(a: AskEvent | null | undefined, currentInputText?: string): ModeChangeResult {
    if (currentInputText === undefined) currentInputText = '';
    if (!a) {
      if (pendingQuestion) {
        return exitAnswerMode(currentInputText);
      }
      return { clearAutoCompletion: false };
    }
    if (a.kind === 'permission') {
      if (pendingQuestion) {
        return exitAnswerMode(currentInputText);
      }
      return { clearAutoCompletion: false };
    }
    if (pendingQuestion && pendingQuestion !== a.callId) {
      return exitAnswerMode(currentInputText);
    }
    return { clearAutoCompletion: false };
  }

  function onInputChange(text: string): { target: string } {
    if (pendingQuestion) {
      draftVersions[pendingQuestion] = (draftVersions[pendingQuestion] || 0) + 1;
      questionDrafts[pendingQuestion] = text;
      return { target: pendingQuestion };
    }
    generalDraft = text;
    return { target: 'general' };
  }

  function onTabAccept(suggestion: string, currentInputText: string): { nextInputText: string; target: string } {
    const v = currentInputText + suggestion;
    if (pendingQuestion) {
      draftVersions[pendingQuestion] = (draftVersions[pendingQuestion] || 0) + 1;
      questionDrafts[pendingQuestion] = v;
      return { nextInputText: v, target: pendingQuestion };
    }
    generalDraft = v;
    return { nextInputText: v, target: 'general' };
  }

  function onCompose(lead: string, currentInputText: string): { nextInputText: string; leadLength: number; target: string } {
    const v = lead + currentInputText;
    if (pendingQuestion) {
      draftVersions[pendingQuestion] = (draftVersions[pendingQuestion] || 0) + 1;
      questionDrafts[pendingQuestion] = v;
      return { nextInputText: v, leadLength: lead.length, target: pendingQuestion };
    }
    generalDraft = v;
    return { nextInputText: v, leadLength: lead.length, target: 'general' };
  }

  function submitReply(callId: string, text: string, isChoice?: boolean): SubmitResult {
    const t = (text || '').trim();
    if (!t) return { ok: false, error: 'empty' };
    if (inFlightReplies[callId]) {
      return { ok: false, error: 'in_flight', message: 'reply already in flight…' };
    }
    const attemptId = ++replyAttemptSeq;
    const ver = isChoice ? (draftVersions[callId] || 0) + 1 : (draftVersions[callId] || 0);
    if (isChoice) draftVersions[callId] = ver;
    const finalText = isChoice ? text : t;
    inFlightReplies[callId] = { attemptId, text: finalText, version: ver };
    questionDrafts[callId] = finalText;

    const wasAnswering = pendingQuestion === callId;
    if (wasAnswering) {
      pendingQuestion = null;
    }

    return {
      ok: true,
      action: 'reply',
      callId,
      text: finalText,
      attemptId,
      exitAnswerMode: wasAnswering,
      nextInputText: generalDraft,
      clearAutoCompletion: true
    };
  }

  function submitSay(text: string): SubmitResult {
    const t = (text || '').trim();
    if (!t) return { ok: false, error: 'empty' };
    generalDraft = '';
    return {
      ok: true,
      action: 'say',
      text: t,
      nextInputText: '',
      clearAutoCompletion: true
    };
  }

  function onReplyResult(m: ReplyResultEvent, currentActiveAsk?: AskEvent | null): ReplyResultOutcome {
    if (typeof m.attemptId !== 'number') {
      return { handled: false, reason: 'missing_or_invalid_attempt_id' };
    }
    const inFlight = inFlightReplies[m.callId];
    if (!inFlight || inFlight.attemptId !== m.attemptId) {
      return { handled: false, reason: 'mismatched_attempt_id' };
    }
    delete inFlightReplies[m.callId];

    const currentVer = draftVersions[m.callId] || 0;
    if (m.ok) {
      if (inFlight.version === currentVer) {
        delete questionDrafts[m.callId];
        delete failedDrafts[m.callId];
      }
      return { handled: true, ok: true, callId: m.callId };
    }

    if (!failedDrafts[m.callId]) failedDrafts[m.callId] = [];
    failedDrafts[m.callId].push(m.text || '');

    const modifiedSinceAttempt = currentVer > inFlight.version;
    if (!modifiedSinceAttempt) {
      questionDrafts[m.callId] = m.text || questionDrafts[m.callId] || '';
      if (currentActiveAsk && currentActiveAsk.callId === m.callId) {
        enterAnswerMode(m.callId, currentActiveAsk.what, generalDraft);
        return {
          handled: true,
          ok: false,
          callId: m.callId,
          reenterAnswerMode: true,
          targetLabel: currentActiveAsk.what || m.callId,
          nextInputText: questionDrafts[m.callId],
          clearAutoCompletion: true
        };
      }
    }
    return { handled: true, ok: false, callId: m.callId, restoredInStoreOnly: true };
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
