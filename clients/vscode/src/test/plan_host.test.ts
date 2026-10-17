import { test } from 'node:test';
import * as assert from 'node:assert/strict';
const Module = require('module');
const originalLoad = Module._load;
const calls: unknown[][] = [];
let confirmation: string | undefined;
let finish: (() => void) | undefined;
Module._load = function (id: string, ...args: unknown[]) {
  if (id === 'vscode') return {
    env: { language: 'ko' },
    commands: { executeCommand: (...args: unknown[]) => {
      calls.push(args);
      return new Promise<void>(resolve => { finish = resolve; });
    } },
    window: { showWarningMessage: async () => confirmation },
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

test('panel controls whitelist commands and reject buttons from another session', async () => {
  let receive: (m: unknown) => void = () => {};
  const companion = { session: 's1', state: { state: 'attached' },
    caps: async () => new Set(), onChanged: () => ({ dispose() {} }) };
  const plan = new Plan(companion);
  const before = calls.length;
  try {
    plan.resolveWebviewView({ webview: { cspSource: '', options: {}, html: '',
      postMessage() {}, onDidReceiveMessage: (fn: typeof receive) => { receive = fn; return { dispose() {} }; } },
      onDidDispose: () => ({ dispose() {} }) });
    receive({ kind: 'control', command: 'workbench.action.closeWindow', session: 's1' });
    receive({ kind: 'control', command: 'magi.chooseModel', session: 'old' });
    assert.equal(calls.length, before);
    receive({ kind: 'control', command: 'magi.chooseModel', session: 's1' });
    assert.deepEqual(calls.slice(before), [['magi.chooseModel']]);
    finish?.();
    await new Promise(resolve => setImmediate(resolve));
  } finally { plan.dispose(); }
});


test('council toggle confirms, accepts omitted false and targets the captured session', async () => {
  let receive: (m: unknown) => void = () => {};
  const requests: any[] = [];
  const companion = { session: 's1', state: { state: 'attached' },
    caps: async () => new Set(), onChanged: () => ({ dispose() {} }), refresh: async () => {},
    ask: async (method: string, payload: any) => { requests.push([method, payload]); return { ok: true }; } };
  const plan = new Plan(companion);
  try {
    plan.resolveWebviewView({ webview: { cspSource: '', options: {}, html: '', postMessage() {},
      onDidReceiveMessage: (fn: typeof receive) => { receive = fn; return { dispose() {} }; } },
      onDidDispose: () => ({ dispose() {} }) });
    confirmation = undefined;
    receive({ kind: 'council', session: 's1' });
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(requests.filter(r => r[0] === 'set-council').length, 0);
    confirmation = '변경';
    receive({ kind: 'council', session: 's1' });
    receive({ kind: 'council', session: 's1' });
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(requests.filter(r => r[0] === 'set-council'), [['set-council', { session: 's1', name: 'true' }]]);
    companion.session = 's2';
    receive({ kind: 'council', session: 's1' });
    assert.equal(requests.filter(r => r[0] === 'set-council').length, 1);
  } finally { confirmation = undefined; plan.dispose(); }
});

test('inline fields validate choices, bind sessions and route provider names to use-profile', async () => {
  let receive: (m: unknown) => void = () => {};
  const requests: any[] = [];
  const selected: string[] = [];
  const companion = { session: 's1', state: { state: 'attached' },
    caps: async () => new Set(), onChanged: () => ({ dispose() {} }), refresh: async () => {},
    ask: async (method: string, payload: any) => { requests.push([method, payload]); return { ok: true }; } };
  const plan = new Plan(companion, (id: string) => selected.push(id));
  (plan as any).choices = { models: ['model-1'], profiles: ['saved-provider'], sessions: [{ id: 's2', title: 'Second' }] };
  try {
    plan.resolveWebviewView({ webview: { cspSource: '', options: {}, html: '', postMessage() {},
      onDidReceiveMessage: (fn: typeof receive) => { receive = fn; return { dispose() {} }; } },
      onDidDispose: () => ({ dispose() {} }) });
    for (const message of [
      { session: 'old', field: 'model', value: 'model-1' },
      { session: 's1', field: 'model', value: 'injected' },
      { session: 's1', field: 'permission', value: 'injected' },
    ]) {
      receive({ kind: 'select', ...message });
      await new Promise(resolve => setImmediate(resolve));
    }
    assert.equal(requests.length, 0);
    receive({ kind: 'select', session: 's1', field: 'model', value: 'model-1' });
    receive({ kind: 'select', session: 's1', field: 'model', value: 'model-1' });
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(requests, [['set-model', { session: 's1', name: 'model-1' }]]);
    receive({ kind: 'select', session: 's1', field: 'session', value: 's2' });
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(selected, ['s2']);
    receive({ kind: 'select', session: 's1', field: 'provider', value: 'saved-provider' });
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(requests.find(r => r[0] === 'use-profile'), ['use-profile', { session: 's1', name: 'saved-provider' }]);
  } finally { plan.dispose(); }
});
