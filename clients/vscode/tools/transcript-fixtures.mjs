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
    rows: [],
    ask: null,
    refs: [],
    ...overrides,
  };
  return msg;
}
