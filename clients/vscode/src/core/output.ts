/**
 * Raw output resolution, formatting, and virtual document identity.
 *
 * For finalized assistant answers and tool results, provides read-only inspection
 * in native VS Code editor tabs without clipping, truncation, or side effects.
 *
 * Invariant: Does NOT depend on vscode or DOM APIs.
 */

import { Event } from './protocol';
import { partOf } from './part';
import { ImmutableSnapshotStore } from './snapshot';

export { ImmutableSnapshotStore } from './snapshot';

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
  if (!Number.isInteger(seq) || seq <= 0) {
    throw new Error('seq must be a positive integer');
  }
  return `assistant:${seq}`;
}

/**
 * Creates an opaque output ID for a tool result, binding both encoded callId and result event seq.
 *
 * Encodes callId so colons within callId do not collide with delimiters.
 * Requires a positive integer resultSeq for exact confirmed item resolution (§3.3).
 */
export function makeToolResultOutputId(callId: string, resultSeq: number): string {
  if (!callId || typeof callId !== 'string') {
    throw new Error('callId must be a non-empty string');
  }
  if (!Number.isInteger(resultSeq) || resultSeq <= 0) {
    throw new Error('resultSeq must be a positive integer');
  }
  return `tool:${encodeURIComponent(callId)}:${resultSeq}`;
}

/**
 * Parses an opaque output ID into its constituent parts.
 *
 * Strictly rejects extra tokens, empty seq, malformed numbers, and unencoded delimiters.
 */
export function parseOutputId(
  outputId: string
):
  | { kind: 'assistant'; seq: number }
  | { kind: 'tool-result'; callId: string; resultSeq: number }
  | null {
  if (typeof outputId !== 'string' || !outputId) return null;
  const parts = outputId.split(':');
  if (parts.length === 2 && parts[0] === 'assistant') {
    const rawSeq = parts[1];
    if (!rawSeq || !/^[1-9]\d*$/.test(rawSeq)) return null;
    const seq = Number(rawSeq);
    if (!Number.isInteger(seq) || seq <= 0 || String(seq) !== rawSeq) return null;
    return { kind: 'assistant', seq };
  }
  if (parts.length === 3 && parts[0] === 'tool') {
    const rawCallId = parts[1];
    const rawSeq = parts[2];
    if (!rawCallId || !rawSeq || !/^[1-9]\d*$/.test(rawSeq)) return null;
    const resultSeq = Number(rawSeq);
    if (!Number.isInteger(resultSeq) || resultSeq <= 0 || String(resultSeq) !== rawSeq) return null;
    try {
      const callId = decodeURIComponent(rawCallId);
      if (!callId) return null;
      return { kind: 'tool-result', callId, resultSeq };
    } catch {
      return null;
    }
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
    const resultEv = events.find(
      (e) =>
        e.seq === parsed.resultSeq &&
        partOf(e).kind === 'tool-result' &&
        partOf(e).toolResult?.callId === parsed.callId
    );
    if (!resultEv) return null;

    const toolResult = partOf(resultEv).toolResult;
    if (!toolResult) return null;

    const rawContent = toolResult.content;
    if (rawContent === undefined || rawContent === null) {
      return null; // undefined/null은 자료 없음
    }

    const callEv = events.find(
      (e) => partOf(e).kind === 'tool-call' && partOf(e).toolCall?.callId === parsed.callId
    );
    const toolName = partOf(callEv).toolCall?.name || '도구';

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
 *    are protected from eviction (insertion-order FIFO).
 *  - Temporary Overflow: If all entries are pinned, allows capacity to be exceeded until unpinned.
 */
export class OutputSnapshots extends ImmutableSnapshotStore<string> {}

