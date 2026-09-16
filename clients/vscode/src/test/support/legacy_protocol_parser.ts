/**
 * Test Support: Hand-written parseWebviewToHostMessage from commit 98a733aa.
 * Preserved in test support to strictly verify 100% parity against Valibot schemas.
 *
 * Invariant: Never included in production VSIX package (excluded by .vscodeignore out/test/**).
 */

import { WebviewToHostMessage } from '../../core/webview_protocol';

export function legacyParseWebviewToHostMessage(raw: unknown): WebviewToHostMessage | undefined {
  if (!raw || typeof raw !== 'object') return undefined;
  const m = raw as Record<string, unknown>;
  const kind = typeof m.kind === 'string' ? m.kind : '';

  switch (kind) {
    case 'ready':
    case 'start':
    case 'drop':
      return { kind };

    case 'say': {
      if (typeof m.text !== 'string') return undefined;
      let creationTaskId: string | undefined;
      if (m.creationTaskId !== undefined) {
        if (typeof m.creationTaskId !== 'string' || m.creationTaskId.trim().length === 0) {
          return undefined;
        }
        creationTaskId = m.creationTaskId;
      }
      return creationTaskId !== undefined
        ? { kind: 'say', text: m.text, creationTaskId }
        : { kind: 'say', text: m.text };
    }

    case 'run':
      if (typeof m.command !== 'string' || !m.command) return undefined;
      return { kind: 'run', command: m.command };

    case 'diff':
      if (
        typeof m.session !== 'string' ||
        m.session.trim().length === 0 ||
        typeof m.callId !== 'string' ||
        m.callId.trim().length === 0
      ) {
        return undefined;
      }
      return { kind: 'diff', session: m.session, callId: m.callId };

    case 'open':
      if (
        typeof m.session !== 'string' ||
        m.session.trim().length === 0 ||
        typeof m.callId !== 'string' ||
        m.callId.trim().length === 0
      ) {
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
        m.session.trim().length === 0 ||
        typeof m.outputId !== 'string' ||
        m.outputId.trim().length === 0
      ) {
        return undefined;
      }
      return { kind: 'output', session: m.session, outputId: m.outputId };

    case 'answer':
      if (
        typeof m.callId !== 'string' ||
        m.callId.trim().length === 0 ||
        typeof m.decision !== 'string' ||
        m.decision.trim().length === 0
      ) {
        return undefined;
      }
      return { kind: 'answer', callId: m.callId, decision: m.decision };

    case 'reply': {
      if (
        typeof m.callId !== 'string' ||
        m.callId.trim().length === 0 ||
        typeof m.text !== 'string' ||
        typeof m.attemptId !== 'number' ||
        !Number.isInteger(m.attemptId) ||
        m.attemptId <= 0 ||
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
        kind: 'reply',
        callId: m.callId,
        text: m.text,
        attemptId: m.attemptId,
        companionKey: m.companionKey,
        session: m.session,
        generation: m.generation,
        webviewId: m.webviewId,
      };
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
