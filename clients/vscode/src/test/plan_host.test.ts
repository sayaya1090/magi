import { test } from 'node:test';
import * as assert from 'node:assert/strict';
const Module = require('module');
const originalLoad = Module._load;
const calls: unknown[][] = [];
let finish: (() => void) | undefined;
Module._load = function (id: string, ...args: unknown[]) {
  if (id === 'vscode') return {
    commands: { executeCommand: (...args: unknown[]) => {
      calls.push(args);
      return new Promise<void>(resolve => { finish = resolve; });
    } },
    window: { showWarningMessage: async () => {} },
  };
  return originalLoad.call(this, id, ...args);
};
const { Plan } = require('../ide/plan');
Module._load = originalLoad;

test('context action binds displayed session, rejects stale messages and blocks duplicate requests', async () => {
  let receive: (m: unknown) => void = () => {};
  const messages: any[] = [];
  const companion = { session: 's1', state: { state: 'attached' },
    caps: async () => new Set(), onChanged: () => ({ dispose() {} }) };
  const plan = new Plan(companion);
  try {
    plan.resolveWebviewView({ webview: { cspSource: '', options: {}, html: '',
      postMessage: (m: any) => messages.push(m),
      onDidReceiveMessage: (fn: typeof receive) => { receive = fn; return { dispose() {} }; } },
      onDidDispose: () => ({ dispose() {} }) });
    receive({ kind: 'compact', session: 'old' });
    assert.equal(calls.length, 0);
    receive({ kind: 'compact', session: 's1' });
    receive({ kind: 'compact', session: 's1' });
    assert.deepEqual(calls, [['magi.compact', 's1']]);
    await new Promise(resolve => setImmediate(resolve));
    assert.ok(messages.some(m => m.compacting === true));
    finish?.();
    await new Promise(resolve => setImmediate(resolve));
    assert.ok(messages.some(m => m.compacting === false));
    companion.session = 's2';
    receive({ kind: 'compact', session: 's1' });
    assert.equal(calls.length, 1);
    companion.session = '';
    receive({ kind: 'compact', session: '' });
    assert.equal(calls.length, 1);
  } finally { plan.dispose(); }
});


test('a delayed context read cannot label the old graph with a new session', async () => {
  let receive: (m: unknown) => void = () => {};
  const messages: any[] = [];
  let complete: ((value: unknown) => void) | undefined;
  const companion = { session: 's1', state: { state: 'attached' },
    caps: async () => new Set(['context']), onChanged: () => ({ dispose() {} }),
    ask: async (_method: string, request: { session: string }) => {
      assert.equal(request.session, 's1');
      return new Promise(resolve => { complete = resolve; });
    } };
  const plan = new Plan(companion);
  try {
    plan.resolveWebviewView({ webview: { cspSource: '', options: {}, html: '',
      postMessage: (m: any) => messages.push(m),
      onDidReceiveMessage: (fn: typeof receive) => { receive = fn; return { dispose() {} }; } },
      onDidDispose: () => ({ dispose() {} }) });
    await new Promise(resolve => setImmediate(resolve));
    companion.session = 's2';
    complete?.({ ok: true, context: { used: 10, window: 100 } });
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(messages.length, 0, 'stale context must not be rendered or rebound');
  } finally { plan.dispose(); }
});
