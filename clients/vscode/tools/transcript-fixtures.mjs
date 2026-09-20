/**
 * Standard test fixtures and helper factories for transcript browser tests.
 * Enforces strict message contracts without global monkey-patching.
 */

/**
 * Creates a valid host-to-webview 'rows' message satisfying WebviewProtocol contracts.
 * Default values are provided ONLY when omitted, never overwriting explicitly passed undefined/null.
 */
export function createRowsMessage(overrides = {}) {
  const msg = {
    kind: 'rows',
    session: 'test-session',
    companionKey: '/workspace',
    generation: 0,
    webviewId: 'test-webview',
    rows: [],
    ask: null,
    refs: [],
    ...overrides,
  };
  return msg;
}

/**
 * Creates a valid host-to-webview 'replyResult' message satisfying WebviewProtocol contracts.
 */
export function createReplyResultMessage(overrides = {}, sourceAttempt = {}) {
  const msg = {
    kind: 'replyResult',
    callId: overrides.callId || sourceAttempt.callId || 'test-call',
    attemptId: overrides.attemptId !== undefined ? overrides.attemptId : (sourceAttempt.attemptId ?? 1),
    ok: overrides.ok !== undefined ? overrides.ok : true,
    companionKey: overrides.companionKey || sourceAttempt.companionKey || '/workspace',
    session: overrides.session || sourceAttempt.session || 'test-session',
    generation: overrides.generation !== undefined ? overrides.generation : (sourceAttempt.generation ?? 0),
    webviewId: overrides.webviewId || sourceAttempt.webviewId || 'test-webview',
    ...overrides,
  };
  return msg;
}

/**
 * Creates a valid host-to-webview 'state' message.
 *
 * `note` is a SIBLING of `state` in this message, not a child of it — a reader that reaches for
 * `state.note` gets undefined and the panel silently draws nothing, which is the same screen as
 * "everything is fine". That shape is why this helper exists.
 */
export function createStateMessage(state, note, overrides = {}) {
  return {
    kind: 'state',
    state: { state, ...(overrides.activity || {}) },
    note: { text: '', offerStart: false, ...(note || {}) },
  };
}
