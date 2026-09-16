export type RecoveryItemKind =
  | 'reply_failed'
  | 'session_creation_failed'
  | 'session_creation_conflict';

export interface RecoveryItem {
  recoveryId: string;
  companionKey: string;
  sessionId?: string;
  creationTaskId?: string;
  callId?: string;
  kind: RecoveryItemKind;
  text: string;
  title: string;
  error?: string;
  reason: string;
  attempts: number;
  seq: number;
  createdAt: number;
}

export interface RegisterRecoveryOptions {
  companionKey: string;
  sessionId?: string;
  creationTaskId?: string;
  callId?: string;
  kind: RecoveryItemKind;
  text: string;
  title?: string;
  error?: string;
  reason?: string;
  eventKey?: string;
}

export interface RecoveryFilter {
  companionKey?: string;
  sessionId?: string;
  includeOtherSessions?: boolean;
}

export interface RecoveryStateManager {
  register(options: RegisterRecoveryOptions): RecoveryItem | undefined;
  listItems(filter?: RecoveryFilter): RecoveryItem[];
  getItem(recoveryId: string): RecoveryItem | undefined;
  deleteItem(recoveryId: string): boolean;
  getFailedDrafts(callId: string, companionKey?: string, sessionId?: string): string[];
  isEventConsumed(eventKey: string): boolean;
  clear(): void;
}

export function createRecoveryState(): RecoveryStateManager {
  const items: RecoveryItem[] = [];
  const consumedEvents = new Set<string>();
  let recoverySeq = 0;
  let globalSeq = 0;

  function register(options: RegisterRecoveryOptions): RecoveryItem | undefined {
    // 1. Empty string is not registered. Whitespace-only string with length > 0 is preserved verbatim! (§4.6.1)
    if (options.text === undefined || options.text === null || options.text.length === 0) {
      return undefined;
    }

    const compKey = options.companionKey || '';
    const sessId = options.sessionId || '';
    const callId = options.callId || '';
    const taskId = options.creationTaskId || '';
    const text = options.text;
    const kind = options.kind;
    const eventKey = options.eventKey;

    // 2. Check if event was already consumed (e.g. duplicate event replay / stale duplicate) (§4.6.2)
    if (eventKey) {
      if (consumedEvents.has(eventKey)) {
        return undefined;
      }
      consumedEvents.add(eventKey);
    }

    // 3. Find if there is an existing matching item to group with
    // "같은 컴패니언·세션·질문(또는 생성 작업)·종류·동일 원문에 한해 반복 실패를 하나로 합치고 횟수와 최신 오류를 갱신합니다." (§4.6.2)
    const existing = items.find((it) =>
      it.companionKey === compKey &&
      (it.sessionId || '') === sessId &&
      (it.callId || '') === callId &&
      (it.creationTaskId || '') === taskId &&
      it.kind === kind &&
      it.text === text
    );

    if (existing) {
      existing.attempts++;
      if (options.error !== undefined) {
        existing.error = options.error;
      }
      if (options.title) {
        existing.title = options.title;
      }
      existing.seq = ++globalSeq;
      return { ...existing };
    }

    // 4. Create new recovery item
    const recoveryId = 'rec-' + (++recoverySeq);
    const title = options.title || (callId ? callId : '새 대화 초안');
    let reason = options.reason;
    if (!reason) {
      if (kind === 'reply_failed') {
        reason = options.error
          ? `답변 전송을 확인하지 못함: ${options.error}`
          : '답변 전송을 확인하지 못함';
      } else if (kind === 'session_creation_failed') {
        reason = options.error
          ? `대화 생성 실패: ${options.error}`
          : '대화 생성 실패';
      } else if (kind === 'session_creation_conflict') {
        reason = '기존 초안과 충돌하여 별도 보관';
      } else {
        reason = '임시 보관';
      }
    }

    const newItem: RecoveryItem = {
      recoveryId,
      companionKey: compKey,
      sessionId: sessId || undefined,
      creationTaskId: taskId || undefined,
      callId: callId || undefined,
      kind,
      text,
      title,
      error: options.error,
      reason,
      attempts: 1,
      seq: ++globalSeq,
      createdAt: Date.now(),
    };

    items.push(newItem);
    return { ...newItem };
  }

  function listItems(filter?: RecoveryFilter): RecoveryItem[] {
    let result = items.slice();

    if (filter) {
      if (filter.companionKey !== undefined) {
        result = result.filter((it) => it.companionKey === filter.companionKey);
      }
      if (!filter.includeOtherSessions) {
        const targetSess = filter.sessionId || '';
        result = result.filter((it) => {
          // Current session items or items without session (unassigned creation tasks)
          return (it.sessionId || '') === targetSess || !it.sessionId;
        });
      }
    }

    // Sort by seq DESC (newest first, §4.6.2)
    result.sort((a, b) => b.seq - a.seq);
    return result.map((it) => ({ ...it }));
  }

  function getItem(recoveryId: string): RecoveryItem | undefined {
    const item = items.find((it) => it.recoveryId === recoveryId);
    return item ? { ...item } : undefined;
  }

  function deleteItem(recoveryId: string): boolean {
    const idx = items.findIndex((it) => it.recoveryId === recoveryId);
    if (idx === -1) return false;
    items.splice(idx, 1);
    return true;
  }

  function getFailedDrafts(callId: string, companionKey?: string, sessionId?: string): string[] {
    const result: string[] = [];
    for (const it of items) {
      if (it.kind !== 'reply_failed') continue;
      if (it.callId !== callId) continue;
      if (companionKey !== undefined && it.companionKey !== companionKey) continue;
      if (sessionId !== undefined && (it.sessionId || '') !== (sessionId || '')) continue;
      result.push(it.text);
    }
    return result;
  }

  function isEventConsumed(eventKey: string): boolean {
    return consumedEvents.has(eventKey);
  }

  function clear(): void {
    items.length = 0;
    consumedEvents.clear();
    recoverySeq = 0;
    globalSeq = 0;
  }

  return {
    register,
    listItems,
    getItem,
    deleteItem,
    getFailedDrafts,
    isEventConsumed,
    clear,
  };
}
