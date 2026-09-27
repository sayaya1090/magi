import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import type { Companion } from '../ide/workspace';
import type { Ide } from '../core/hand';

// These tests run the real host lifecycle; no editor API is needed before registration.
const Module = require('module');
const originalLoad = Module._load;
Module._load = function (name: string, ...args: unknown[]) {
  return name === 'vscode' ? {} : originalLoad.call(this, name, ...args);
};
const { EditorHand } = require('../ide/hand') as typeof import('../ide/hand');
Module._load = originalLoad;

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function setup() {
  const capsEntered = deferred<void>();
  const startEntered = deferred<void>();
  const caps = deferred<Set<string> | null>();
  const calls = { caps: 0, start: 0, attach: 0, detach: 0, close: 0 };
  const server = {
    url: 'http://127.0.0.1:12345/mcp',
    headers: { 'X-Magi-Hand': 'test-token' },
    close() { calls.close++; },
  };
  const started = deferred<typeof server>();
  const messages: string[] = [];
  const companion = {
    async caps() { calls.caps++; capsEntered.resolve(); return caps.promise; },
    async ask(method: string, args: unknown) {
      if (method === 'mcp-attach') {
        calls.attach++;
        assert.deepEqual(args, { name: 'vscode', url: server.url, headers: server.headers });
        return { ok: true, tools: ['show'] };
      }
      assert.equal(method, 'mcp-detach-if');
      calls.detach++;
      return { ok: true };
    },
  } as unknown as Companion;
  const hand: InstanceType<typeof EditorHand> = new EditorHand(companion, '/workspace', async (ide: Ide) => {
    assert.equal(ide, hand);
    calls.start++;
    startEntered.resolve();
    return started.promise;
  });
  hand.told = message => messages.push(message);
  return { hand, calls, caps, started, capsEntered, startEntered, server, messages };
}

test('disposed hand never starts preparation or registration', async () => {
  const f = setup();
  f.hand.dispose();
  await f.hand.offer();
  f.hand.dispose();
  assert.deepEqual(f.calls, { caps: 0, start: 0, attach: 0, detach: 0, close: 0 });
});

test('dispose before the queued offer starts skips capabilities', async () => {
  const f = setup();
  const offering = f.hand.offer();
  f.hand.dispose();
  await offering;
  assert.deepEqual(f.calls, { caps: 0, start: 0, attach: 0, detach: 0, close: 0 });
});

test('dispose while capabilities are pending prevents server creation', async () => {
  const f = setup();
  const offering = f.hand.offer();
  await f.capsEntered.promise;
  f.hand.dispose();
  f.caps.resolve(new Set(['tool-servers']));
  // A faulty implementation must fail an assertion, not hang waiting for the fake server.
  f.started.resolve(f.server);
  await offering;
  assert.deepEqual(f.calls, { caps: 1, start: 0, attach: 0, detach: 0, close: 0 });
  assert.equal(f.hand.handWhy(), '');
  assert.deepEqual(f.messages, []);
});

test('dispose while server starts closes the late server exactly once without registering', async () => {
  const f = setup();
  f.caps.resolve(new Set(['tool-servers']));
  const offering = f.hand.offer();
  await f.startEntered.promise;
  f.hand.dispose();
  f.started.resolve(f.server);
  await offering;
  f.hand.dispose();
  await f.hand.offer();
  assert.deepEqual(f.calls, { caps: 1, start: 1, attach: 0, detach: 0, close: 1 });
  assert.equal(f.hand.handWhy(), '');
  assert.deepEqual(f.messages, []);
});

test('overlapping offers share preparation and successful registration; dispose is idempotent', async () => {
  const f = setup();
  try {
    const first = f.hand.offer();
    await f.capsEntered.promise;
    const second = f.hand.offer();
    assert.equal(second, first);
    f.caps.resolve(new Set(['tool-servers']));
    await f.startEntered.promise;
    const third = f.hand.offer();
    assert.equal(third, first);
    f.started.resolve(f.server);
    await Promise.all([first, second, third]);
    await f.hand.offer();
    assert.deepEqual(f.calls, { caps: 1, start: 1, attach: 1, detach: 0, close: 0 });
    assert.equal(f.hand.handWhy(), 'attached — show');
    assert.deepEqual(f.messages, ['attached — show']);
  } finally {
    f.hand.dispose();
  }
  f.hand.dispose();
  await f.hand.offer();
  assert.deepEqual(f.calls, { caps: 1, start: 1, attach: 1, detach: 1, close: 1 });
});

test('server failure rejects all waiting callers and allows a fresh retry', async () => {
  let attempts = 0;
  let attaches = 0;
  let closes = 0;
  const failure = new Error('no port');
  const hand = new EditorHand({
    caps: async () => new Set(['tool-servers']),
    ask: async (method: string) => { if (method === 'mcp-attach') attaches++; return { ok: true }; },
  } as unknown as Companion, '/workspace', async () => {
    if (++attempts === 1) throw failure;
    return { url: 'http://localhost/mcp', headers: {}, close() { closes++; } };
  });
  try {
    const first = hand.offer();
    const second = hand.offer();
    await Promise.all([assert.rejects(first, e => e === failure), assert.rejects(second, e => e === failure)]);
    assert.match(hand.handWhy(), /no port/);
    await hand.offer();
    assert.equal(attempts, 2);
    assert.equal(attaches, 1);
  } finally {
    hand.dispose();
  }
  assert.equal(closes, 1);
});

test('capability rejection propagates and permits retry', async () => {
  const f = setup();
  const failure = new Error('caps disconnected');
  const offering = f.hand.offer();
  const rejected = assert.rejects(offering, e => e === failure);
  await f.capsEntered.promise;
  f.caps.reject(failure);
  await rejected;
  await assert.rejects(f.hand.offer(), e => e === failure);
  assert.equal(f.calls.caps, 2);
  assert.equal(f.calls.start, 0);
  f.hand.dispose();
});

test('missing and unsupported capabilities preserve guidance without opening a server', async () => {
  for (const [caps, reason] of [[null, /could not read/], [new Set<string>(), /no tool-servers/]] as const) {
    const f = setup();
    f.caps.resolve(caps);
    await f.hand.offer();
    assert.match(f.hand.handWhy(), reason);
    assert.deepEqual(f.calls, { caps: 1, start: 0, attach: 0, detach: 0, close: 0 });
    f.hand.dispose();
  }
});

test('refused registration keeps its reason and reuses the server on retry', async () => {
  let starts = 0;
  let attaches = 0;
  let detaches = 0;
  let closes = 0;
  const messages: string[] = [];
  const hand = new EditorHand({
    caps: async () => new Set(['tool-servers']),
    ask: async (method: string) => {
      if (method === 'mcp-detach-if') { detaches++; return { ok: true }; }
      return ++attaches === 1 ? { ok: false, error: 'already owned' } : { ok: true, tools: ['problems'] };
    },
  } as unknown as Companion, '/workspace', async () => {
    starts++;
    return { url: 'http://localhost/mcp', headers: {}, close() { closes++; } };
  });
  hand.told = message => messages.push(message);
  try {
    await hand.offer();
    assert.equal(hand.handWhy(), 'refused — already owned');
    await hand.offer();
    assert.equal(starts, 1);
    assert.equal(attaches, 2);
    assert.deepEqual(messages, ['refused — already owned', 'attached — problems']);
  } finally {
    hand.dispose();
  }
  assert.equal(detaches, 1);
  assert.equal(closes, 1);
});

test('dispose during attach waits for reply then conditionally releases captured identity once', async () => {
  const entered = deferred<void>();
  const answer = deferred<{ ok: boolean }>();
  const calls: string[] = [];
  const server = { url: 'http://127.0.0.1:12345/mcp', headers: { 'X-Magi-Hand': 'old-token' }, close() { calls.push('close'); } };
  const companion = {
    async caps() { return new Set(['tool-servers']); },
    async ask(method: string, args: unknown) {
      calls.push(method);
      assert.deepEqual(args, { name: 'vscode', url: server.url, headers: server.headers });
      if (method === 'mcp-attach') { entered.resolve(); return answer.promise; }
      assert.equal(method, 'mcp-detach-if');
      return { ok: true, removed: false };
    },
  } as unknown as Companion;
  const hand = new EditorHand(companion, '/workspace', async () => server);
  const offering = hand.offer();
  await entered.promise;
  hand.dispose();
  hand.dispose();
  assert.deepEqual(calls, ['mcp-attach', 'close']);
  answer.resolve({ ok: true });
  await offering;
  await Promise.resolve();
  assert.deepEqual(calls, ['mcp-attach', 'close', 'mcp-detach-if']);
});
