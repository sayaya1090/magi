/**
 * Discriminated union message protocol between VS Code host and chat webview.
 *
 * Invariant: Every message kind specifies its required fields.
 * Runtime parsing at the boundary rejects malformed payloads without silent fallback.
 */

import { Row } from './transcript';
import { Ask } from './touched';
import { Activity } from './activity';

export type PaintedRow = Row & { label: string };

export interface PanelNoteInfo {
  text: string;
  offerStart: boolean;
}

// ── Webview to Host Messages ──

export type WebviewToHostMessage =
  | { kind: 'ready' }
  | { kind: 'say'; text: string }
  | { kind: 'start' }
  | { kind: 'run'; command: string }
  | { kind: 'drop' }
  | { kind: 'diff'; session: string; callId: string }
  | { kind: 'open'; session: string; callId: string; seq?: number }
  | { kind: 'output'; session: string; outputId: string }
  | { kind: 'answer'; callId: string; decision: string }
  | {
      kind: 'reply';
      callId: string;
      text: string;
      attemptId: number;
      companionKey?: string;
      session?: string;
      generation?: number;
    }
  | { kind: 'mention'; text: string; reqId: number; target: string }
  | { kind: 'suggest'; text: string; reqId: number; target: string };

/**
 * Runtime validation of messages received from the webview boundary.
 * Returns parsed message if valid according to schema, otherwise undefined.
 */
export function parseWebviewToHostMessage(raw: unknown): WebviewToHostMessage | undefined {
  if (!raw || typeof raw !== 'object') return undefined;
  const m = raw as Record<string, unknown>;
  const kind = typeof m.kind === 'string' ? m.kind : '';

  switch (kind) {
    case 'ready':
    case 'start':
    case 'drop':
      return { kind };

    case 'say':
      if (typeof m.text !== 'string') return undefined;
      return { kind: 'say', text: m.text };

    case 'run':
      if (typeof m.command !== 'string' || !m.command) return undefined;
      return { kind: 'run', command: m.command };

    case 'diff':
      if (typeof m.session !== 'string' || !m.session || typeof m.callId !== 'string' || !m.callId) {
        return undefined;
      }
      return { kind: 'diff', session: m.session, callId: m.callId };

    case 'open':
      if (typeof m.session !== 'string' || !m.session || typeof m.callId !== 'string' || !m.callId) {
        return undefined;
      }
      return {
        kind: 'open',
        session: m.session,
        callId: m.callId,
        seq: typeof m.seq === 'number' ? m.seq : undefined,
      };

    case 'output':
      if (
        typeof m.session !== 'string' ||
        !m.session ||
        typeof m.outputId !== 'string' ||
        !m.outputId
      ) {
        return undefined;
      }
      return { kind: 'output', session: m.session, outputId: m.outputId };

    case 'answer':
      if (typeof m.callId !== 'string' || !m.callId || typeof m.decision !== 'string' || !m.decision) {
        return undefined;
      }
      return { kind: 'answer', callId: m.callId, decision: m.decision };

    case 'reply': {
      if (
        typeof m.callId !== 'string' ||
        !m.callId ||
        typeof m.text !== 'string' ||
        typeof m.attemptId !== 'number'
      ) {
        return undefined;
      }
      const parsed: WebviewToHostMessage = {
        kind: 'reply',
        callId: m.callId,
        text: m.text,
        attemptId: m.attemptId,
      };
      if (typeof m.companionKey === 'string') (parsed as any).companionKey = m.companionKey;
      if (typeof m.session === 'string') (parsed as any).session = m.session;
      if (typeof m.generation === 'number') (parsed as any).generation = m.generation;
      return parsed;
    }

    case 'mention':
      if (typeof m.text !== 'string' || typeof m.reqId !== 'number' || typeof m.target !== 'string') {
        return undefined;
      }
      return { kind: 'mention', text: m.text, reqId: m.reqId, target: m.target };

    case 'suggest':
      if (typeof m.text !== 'string' || typeof m.reqId !== 'number' || typeof m.target !== 'string') {
        return undefined;
      }
      return { kind: 'suggest', text: m.text, reqId: m.reqId, target: m.target };

    default:
      return undefined;
  }
}

// ── Host to Webview Messages ──

export type HostToWebviewMessage =
  | { kind: 'state'; state: Activity; note: PanelNoteInfo }
  | {
      kind: 'info';
      state: string;
      label: string;
      version: string;
      model?: string;
      backend?: string;
      permission?: string;
      council?: string;
      socket?: string;
    }
  | {
      kind: 'rows';
      session: string;
      rows: PaintedRow[];
      ask: Ask | null;
      refs: string[];
      companionKey?: string;
      generation?: number;
    }
  | { kind: 'compose'; text: string }
  | { kind: 'note'; text: string }
  | {
      kind: 'replyResult';
      callId: string;
      attemptId: number;
      ok: boolean;
      error?: string;
      text?: string;
      companionKey?: string;
      session?: string;
      generation?: number;
    }
  | { kind: 'mentions'; files: string[]; reqId: number; target: string }
  | { kind: 'suggestion'; text: string; reqId: number; target: string };

