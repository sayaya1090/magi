/**
 * Raw output resolution, formatting, and virtual document identity.
 *
 * For finalized assistant answers and tool results, provides read-only inspection
 * in native VS Code editor tabs without clipping, truncation, or side effects.
 *
 * Invariant: Does NOT depend on vscode or DOM APIs.
 */

import { Event } from './protocol';

export type OutputKind = 'assistant' | 'tool-result';
export type OutputLanguage = 'markdown' | 'plaintext' | 'json';

export interface OutputItem {
  outputId: string;
  kind: OutputKind;
  content: string;
  title: string;
  language: OutputLanguage;
}

/**
 * Creates an opaque output ID for a finalized assistant message.
 */
export function makeAssistantOutputId(seq: number): string {
  return `assistant:${seq}`;
}

/**
 * Creates an opaque output ID for a tool result, binding both callId and result event seq.
 */
export function makeToolResultOutputId(callId: string, resultSeq?: number): string {
  return resultSeq !== undefined ? `tool:${callId}:${resultSeq}` : `tool:${callId}`;
}

/**
 * Parses an opaque output ID into its constituent parts.
 */
export function parseOutputId(
  outputId: string
):
  | { kind: 'assistant'; seq: number }
  | { kind: 'tool-result'; callId: string; resultSeq?: number }
  | null {
  if (typeof outputId !== 'string' || !outputId) return null;
  if (outputId.startsWith('assistant:')) {
    const seq = Number(outputId.slice('assistant:'.length));
    if (!Number.isInteger(seq) || seq <= 0) return null;
    return { kind: 'assistant', seq };
  }
  if (outputId.startsWith('tool:')) {
    const rest = outputId.slice('tool:'.length);
    if (!rest) return null;
    const parts = rest.split(':');
    const callId = parts[0];
    if (!callId) return null;
    const resultSeq = parts[1] !== undefined && parts[1] !== '' ? Number(parts[1]) : undefined;
    if (resultSeq !== undefined && (!Number.isInteger(resultSeq) || resultSeq <= 0)) {
      return null;
    }
    return { kind: 'tool-result', callId, resultSeq };
  }
  return null;
}

/**
 * Resolves raw content and metadata for an assistant answer or tool result from confirmed facts.
 *
 * Never clips or summarizes content. Preserves exact string representation without trimming or
 * newline modification. Structures non-string content as formatted JSON.
 * Returns null if the item cannot be resolved, is still in draft, or contains null/undefined content.
 */
export function resolveOutputItem(events: Event[], outputId: string): OutputItem | null {
  const parsed = parseOutputId(outputId);
  if (!parsed) return null;

  if (parsed.kind === 'assistant') {
    const ev = events.find((e) => e.seq === parsed.seq);
    if (!ev || ev.type !== 'part.appended') return null;
    const d = (ev.data ?? {}) as Record<string, unknown>;
    const role = String(d.role ?? '');
    const p = (d.part ?? {}) as { kind?: string; text?: unknown };
    if (role !== 'assistant' || p.kind !== 'text' || typeof p.text !== 'string') {
      return null;
    }
    return {
      outputId,
      kind: 'assistant',
      content: p.text,
      title: `모델 답변 (seq ${parsed.seq})`,
      language: 'markdown',
    };
  }

  if (parsed.kind === 'tool-result') {
    let resultEv: Event | undefined;
    if (parsed.resultSeq !== undefined) {
      resultEv = events.find(
        (e) =>
          e.seq === parsed.resultSeq &&
          e.type === 'part.appended' &&
          (e.data as any)?.part?.kind === 'tool-result' &&
          (e.data as any)?.part?.toolResult?.callId === parsed.callId
      );
      if (!resultEv) return null;
    } else {
      resultEv = [...events].reverse().find(
        (e) =>
          e.type === 'part.appended' &&
          (e.data as any)?.part?.kind === 'tool-result' &&
          (e.data as any)?.part?.toolResult?.callId === parsed.callId
      );
    }
    if (!resultEv) return null;

    const toolResult = (resultEv.data as any)?.part?.toolResult as
      | { callId?: string; content?: unknown; isError?: boolean; advisory?: boolean }
      | undefined;
    if (!toolResult) return null;

    const rawContent = toolResult.content;
    if (rawContent === undefined || rawContent === null) {
      return null; // undefined/null은 자료 없음
    }

    const callEv = events.find(
      (e) =>
        e.type === 'part.appended' &&
        (e.data as any)?.part?.kind === 'tool-call' &&
        (e.data as any)?.part?.toolCall?.callId === parsed.callId
    );
    const toolName = (callEv?.data as any)?.part?.toolCall?.name || '도구';

    let content: string;
    let language: OutputLanguage;
    let title: string;

    if (typeof rawContent === 'string') {
      content = rawContent; // Verbatim string (empty string is valid)
      language = 'plaintext';
      title = `${toolName} 결과`;
    } else {
      try {
        content = JSON.stringify(rawContent, null, 2);
      } catch {
        content = String(rawContent);
      }
      language = 'json';
      title = `${toolName} 결과 (JSON)`;
    }

    return {
      outputId,
      kind: 'tool-result',
      content,
      title,
      language,
    };
  }

  return null;
}

/**
 * Deterministic URI for a virtual output document.
 *
 * Encodes companion, session, kind, outputId, and filename to prevent cross-session collisions
 * while allowing tab reuse on repeated clicks of the same item.
 */
export function outputUri(
  companionKey: string,
  sessionId: string,
  kind: OutputKind,
  outputId: string,
  filename?: string
): string {
  const encCompanion = encodeURIComponent(companionKey || 'default');
  const encSession = encodeURIComponent(sessionId || 'default');
  const encKind = encodeURIComponent(kind);
  const encId = encodeURIComponent(outputId);
  const filePart = filename ? `/${encodeURIComponent(filename)}` : '';
  return `magi-output:/${encCompanion}/${encSession}/${encKind}/${encId}${filePart}`;
}

/**
 * Derives a clean filename for the output document tab.
 */
export function defaultOutputFilename(item: OutputItem): string {
  const ext = item.language === 'markdown' ? '.md' : item.language === 'json' ? '.json' : '.txt';
  const safeTitle = item.title.replace(/[\/\\:*?"<>|]/g, '_').trim() || 'output';
  return `${safeTitle}${ext}`;
}

/**
 * Immutable snapshot cache for virtual output documents.
 *
 *  - Immutability: Once stored for a key, subsequent calls to put() preserve initial content.
 *  - Open Tab Protection: When capacity is exceeded, entries that are pinned (open tabs or in-flight)
 *    are protected from LRU eviction.
 *  - Temporary Overflow: If all entries are pinned, allows capacity to be exceeded until unpinned.
 */
export class OutputSnapshots {
  private readonly store = new Map<string, string>();
  private readonly order: string[] = [];
  private readonly tempPinned = new Map<string, number>();

  constructor(
    private readonly maxEntries: number = 100,
    private readonly isPinned?: (key: string) => boolean
  ) {}

  /**
   * Temporarily protects keys from eviction during in-flight open operations.
   * Reference-counted to support concurrent operations independently.
   */
  protectTemp(keys: string[]): () => void {
    for (const k of keys) {
      const current = this.tempPinned.get(k) ?? 0;
      this.tempPinned.set(k, current + 1);
    }
    let released = false;
    return () => {
      if (released) return;
      released = true;
      for (const k of keys) {
        const count = this.tempPinned.get(k) ?? 0;
        if (count <= 1) {
          this.tempPinned.delete(k);
        } else {
          this.tempPinned.set(k, count - 1);
        }
      }
      this.evictExcess();
    };
  }

  put(key: string, content: string): boolean {
    if (this.store.has(key)) {
      return false; // Immutable: keep initial content
    }
    this.order.push(key);
    this.store.set(key, content);

    while (this.order.length > this.maxEntries) {
      const evictIndex = this.order.slice(0, -1).findIndex((k) => !this.isProtected(k));
      if (evictIndex < 0) {
        break; // All older entries protected; allow temporary limit overflow
      }
      const [evicted] = this.order.splice(evictIndex, 1);
      this.store.delete(evicted);
    }
    return true;
  }

  /**
   * Evicts unpinned entries if size exceeds maxEntries.
   */
  evictExcess(): number {
    let count = 0;
    while (this.order.length > this.maxEntries) {
      const evictIndex = this.order.findIndex((k) => !this.isProtected(k));
      if (evictIndex < 0) {
        break;
      }
      const [evicted] = this.order.splice(evictIndex, 1);
      this.store.delete(evicted);
      count++;
    }
    return count;
  }

  private isProtected(key: string): boolean {
    if (this.tempPinned.has(key)) return true;
    return this.isPinned ? this.isPinned(key) : false;
  }

  get(key: string): string | undefined {
    return this.store.get(key);
  }

  has(key: string): boolean {
    return this.store.has(key);
  }

  delete(key: string): boolean {
    const idx = this.order.indexOf(key);
    if (idx >= 0) this.order.splice(idx, 1);
    this.tempPinned.delete(key);
    return this.store.delete(key);
  }

  get size(): number {
    return this.store.size;
  }

  get tempPinnedSize(): number {
    return this.tempPinned.size;
  }

  clear(): void {
    this.store.clear();
    this.order.length = 0;
    this.tempPinned.clear();
  }
}
