import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
const Module = require('module');

const commandRegistry = new Map<string, Function>();
const warnings: string[] = [];
const infos: string[] = [];

// What `workspace.ts`, `doors.ts`, `handoff.ts`, and `hand.ts` reach for.
const origLoad = (Module as any)._load;
(Module as any)._load = function (request: string, parent: any, isMain: boolean) {
  if (request === 'vscode') {
    return {
      commands: {
        registerCommand(id: string, fn: Function) {
          commandRegistry.set(id, fn);
          return { dispose() { commandRegistry.delete(id); } };
        },
        async executeCommand(id: string, ...args: any[]) {
          const fn = commandRegistry.get(id);
          if (fn) return await fn(...args);
        },
      },
      window: {
        showWarningMessage(msg: string) {
          warnings.push(msg);
          return Promise.resolve(undefined);
        },
        showInformationMessage(msg: string) {
          infos.push(msg);
          return Promise.resolve(undefined);
        },
        async showQuickPick() { return undefined; },
        async showInputBox() { return undefined; },
      },
      EventEmitter: class {
        event = () => ({ dispose() {} });
        fire() {}
        dispose() {}
      },
      Disposable: class {
        static from(...disposables: { dispose(): any }[]) {
          return { dispose() { for (const d of disposables) d.dispose(); } };
        }
      },
    };
  }
  return origLoad.call(this, request, parent, isMain);
};

const { Companion } = require('../ide/workspace');
const { doorCommands } = require('../ide/doors');
const { HandOff } = require('../ide/handoff');
const { EditorHand } = require('../ide/hand');

/**
 * ★ **"We could not ask" and "it does not offer that" are different facts, and only one of them
 * was being reported.**
 *
 * `caps()` used to answer `new Set()` when `about` came back null, and every caller read that as an
 * answer FROM the daemon. Measured 2026-09-19 with the daemon stopped and a fresh window:
 * `magi: Start a new conversation` put this on screen —
 *
 *     magi: this companion does not offer opening a new conversation.
 *
 * — about a companion that was not running at all. The same empty set told the handoff picker this
 * build "cannot list the others on this machine" and made the editor hand log a missing
 * `tool-servers` door. Three sentences inventing a fact about a build nobody spoke to.
 *
 * So the unreachable answer has its own shape, and a caller cannot spend it as knowledge by accident.
 */
test('caps says it could not verify capabilities, rather than answering for a daemon it never reached or that refused', async () => {
  const c = new Companion('/nowhere');
  (c as any).ask = async () => null;                       // nothing is listening
  assert.equal(await c.caps(), null, 'an unreachable companion must not come back as an empty set');

  // And it is not remembered: the daemon may start a second later, and a cached "nothing" would
  // outlive it for the life of the window.
  (c as any).ask = async () => ({ ok: true, caps: ['session-new'], version: 'v9' });
  const caps = await c.caps();
  assert.ok(caps instanceof Set && caps.has('session-new'), 'a companion that answered must come back as its doors');
});

/** A refusal from a running daemon is still an empty-handed answer — and it IS an answer. */
test('caps distinguishes a daemon that refused from a daemon that is not there, without asserting absence', async () => {
  const c = new Companion('/nowhere');
  (c as any).ask = async () => ({ ok: false, error: 'access denied' });
  assert.equal(await c.caps(), null, 'a refused about cannot verify capabilities, returning null without asserting absence');
});

/**
 * ★ The sentence itself, held where it is spoken.
 *
 * The fix above only helps if the caller keeps the two apart, and the caller is the one with the
 * words. Read as source because that is where the wording lives; a mutation that puts the old
 * sentence back on the null branch fails here.
 */
test('the command gate names capability verification failure, not a missing door', () => {
  const src = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'doors.ts'), 'utf8');
  const gate = src.slice(src.indexOf('const has = async (cap: string'));
  const body = gate.slice(0, gate.indexOf('\n  };'));
  assert.match(body, /caps === null/, 'the gate must test for "could not ask" at all');
  const nullBranch = body.slice(body.indexOf('caps === null'), body.indexOf('caps.has(cap)'));
  assert.match(nullBranch, /could not read companion capabilities/,
    'an unverified companion must be reported as verification failure, not as a build without the door');
  assert.doesNotMatch(nullBranch, /does not offer/,
    'the null branch must not claim to know what the companion offers');
});

const flush = () => new Promise((r) => setTimeout(r, 10));

test('§5.9 P2: magi.newConversation registration and invocation across 4 capability states', async () => {
  // Case 1: about = null (daemon unreachable)
  {
    warnings.length = 0;
    const c = new Companion('/nowhere');
    let sessionNewCalled = 0;
    (c as any).ask = async (method: string) => {
      if (method === 'about') return null;
      if (method === 'session-new') { sessionNewCalled++; return { ok: true, session: 's1' }; }
      return null;
    };
    const shown: string[] = [];
    const chat: any = { session: 's0', showSession: (s: string) => shown.push(s) };
    const disposables = doorCommands(c, chat, {} as any);
    try {
      const cmd = commandRegistry.get('magi.newConversation');
      assert.ok(cmd, 'magi.newConversation must be registered');
      await cmd();
      await flush();
      assert.equal(sessionNewCalled, 0, 'session-new must not be called when capabilities cannot be verified');
      assert.equal(shown.length, 0, 'showSession must not be called');
      assert.deepEqual(warnings, ['magi: could not read companion capabilities; check the connection and retry.']);
    } finally {
      disposables.forEach((d: any) => d.dispose());
    }
  }

  // Case 2: about = { ok: false, error: 'access denied' } (daemon refused)
  {
    warnings.length = 0;
    const c = new Companion('/nowhere');
    let sessionNewCalled = 0;
    (c as any).ask = async (method: string) => {
      if (method === 'about') return { ok: false, error: 'access denied' };
      if (method === 'session-new') { sessionNewCalled++; return { ok: true, session: 's2' }; }
      return null;
    };
    const shown: string[] = [];
    const chat: any = { session: 's0', showSession: (s: string) => shown.push(s) };
    const disposables = doorCommands(c, chat, {} as any);
    try {
      const cmd = commandRegistry.get('magi.newConversation');
      assert.ok(cmd);
      await cmd();
      await flush();
      assert.equal(sessionNewCalled, 0, 'session-new must not be called when about was refused');
      assert.equal(shown.length, 0, 'showSession must not be called');
      assert.deepEqual(warnings, ['magi: could not read companion capabilities; check the connection and retry.']);
    } finally {
      disposables.forEach((d: any) => d.dispose());
    }
  }

  // Case 3: about = { ok: true, caps: [], version: 'v1' } (connected, empty capabilities)
  {
    warnings.length = 0;
    const c = new Companion('/nowhere');
    let sessionNewCalled = 0;
    (c as any).ask = async (method: string) => {
      if (method === 'about') return { ok: true, caps: [], version: 'v1' };
      if (method === 'session-new') { sessionNewCalled++; return { ok: true, session: 's3' }; }
      return null;
    };
    const shown: string[] = [];
    const chat: any = { session: 's0', showSession: (s: string) => shown.push(s) };
    const disposables = doorCommands(c, chat, {} as any);
    try {
      const cmd = commandRegistry.get('magi.newConversation');
      assert.ok(cmd);
      await cmd();
      await flush();
      assert.equal(sessionNewCalled, 0, 'session-new must not be called when capability is absent');
      assert.equal(shown.length, 0, 'showSession must not be called');
      assert.deepEqual(warnings, ['magi: this companion does not offer opening a new conversation.']);
    } finally {
      disposables.forEach((d: any) => d.dispose());
    }
  }

  // Case 4: about = { ok: true, caps: ['session-new'], version: 'v1' } (connected, offers session-new)
  {
    warnings.length = 0;
    const c = new Companion('/nowhere');
    let sessionNewCalled = 0;
    (c as any).ask = async (method: string) => {
      if (method === 'about') return { ok: true, caps: ['session-new'], version: 'v1' };
      if (method === 'session-new') { sessionNewCalled++; return { ok: true, session: 'sess-success' }; }
      return null;
    };
    const shown: string[] = [];
    const chat: any = { session: 's0', showSession: (s: string) => shown.push(s) };
    const disposables = doorCommands(c, chat, {} as any);
    try {
      const cmd = commandRegistry.get('magi.newConversation');
      assert.ok(cmd);
      await cmd();
      await flush();
      assert.equal(sessionNewCalled, 1, 'session-new must be called exactly once');
      assert.deepEqual(shown, ['sess-success']);
      assert.equal(warnings.length, 0, 'no warning should be displayed on success');
    } finally {
      disposables.forEach((d: any) => d.dispose());
    }
  }
});

test('§5.9 P2: HandOff and EditorHand preserve capability verification failure semantics', async () => {
  // HandOff with unreachable daemon
  {
    warnings.length = 0;
    const c = new Companion('/nowhere');
    (c as any).ask = async () => null;
    const handoff = new HandOff(c, 'me');
    const disposables = handoff.commands();
    try {
      const cmd = commandRegistry.get('magi.handOff');
      assert.ok(cmd);
      await cmd();
      await flush();
      assert.deepEqual(warnings, ['magi: could not read companion capabilities; check the connection and retry.']);
    } finally {
      disposables.forEach((d: any) => d.dispose());
    }
  }

  // HandOff with empty capabilities
  {
    warnings.length = 0;
    const c = new Companion('/nowhere');
    (c as any).ask = async (method: string) => method === 'about' ? { ok: true, caps: [] } : null;
    const handoff = new HandOff(c, 'me');
    const disposables = handoff.commands();
    try {
      const cmd = commandRegistry.get('magi.handOff');
      assert.ok(cmd);
      await cmd();
      await flush();
      assert.deepEqual(warnings, ['magi: this companion cannot list the others on this machine.']);
    } finally {
      disposables.forEach((d: any) => d.dispose());
    }
  }

  // EditorHand with refused capabilities
  {
    const c = new Companion('/nowhere');
    (c as any).ask = async () => ({ ok: false, error: 'connection refused' });
    const hand = new EditorHand(c, '/workspace');
    await hand.offer();
    assert.equal(hand.why, 'could not read companion capabilities; check the connection and retry');
    assert.equal(hand.attached, false);
  }

  // EditorHand with empty capabilities (missing tool-servers)
  {
    const c = new Companion('/nowhere');
    (c as any).ask = async (method: string) => method === 'about' ? { ok: true, caps: [] } : null;
    const hand = new EditorHand(c, '/workspace');
    await hand.offer();
    assert.equal(hand.why, 'this companion cannot take an editor hand (no tool-servers door)');
    assert.equal(hand.attached, false);
  }
});
