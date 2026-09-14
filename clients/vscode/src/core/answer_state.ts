import * as fs from 'fs';
import * as path from 'path';

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

/**
 * Loads the createAnswerState factory directly from chat.ts to ensure identical execution
 * between the real webview and node test suites without duplicate code or extra bundlers.
 */
export const createAnswerState: () => AnswerStateManager = (() => {
  const candidates = [
    path.join(__dirname, '..', 'ide', 'chat.ts'),
    path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'),
    path.join(__dirname, '..', '..', 'clients', 'vscode', 'src', 'ide', 'chat.ts'),
  ];
  let chatSrc = '';
  for (const c of candidates) {
    if (fs.existsSync(c)) {
      chatSrc = fs.readFileSync(c, 'utf8');
      break;
    }
  }
  if (!chatSrc) {
    throw new Error('chat.ts not found for answer state loader');
  }
  const startTag = '/* START createAnswerState */';
  const endTag = '/* END createAnswerState */';
  const start = chatSrc.indexOf(startTag);
  const end = chatSrc.indexOf(endTag, start);
  if (start < 0 || end < 0) {
    throw new Error('createAnswerState boundaries not found in chat.ts');
  }
  const fnCode = chatSrc.slice(start + startTag.length, end).trim();
  return new Function(fnCode + '\nreturn createAnswerState;')() as () => AnswerStateManager;
})();
