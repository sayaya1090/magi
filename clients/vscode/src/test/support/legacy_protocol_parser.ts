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

/**
 * Test Support: Hand-written parseHostToWebviewMessage from commit c6e042f4.
 * Preserved in test support to strictly verify 100% parity against Valibot schemas.
 *
 * Invariant: Never included in production VSIX package (excluded by .vscodeignore out/test/**).
 */
import type { HostToWebviewMessage, PaintedRow, PanelNoteInfo } from '../../core/webview_protocol';
import type { Ask } from '../../core/touched';
import type { Activity } from '../../core/activity';

export function legacyParseHostToWebviewMessage(raw: unknown): HostToWebviewMessage | undefined {
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
