import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
const Module = require('module');

// What `workspace.ts` actually reaches for at construction time, and nothing more.
const origLoad = (Module as any)._load;
(Module as any)._load = function (request: string, parent: any, isMain: boolean) {
  if (request === 'vscode') {
    return {
      EventEmitter: class {
        event = () => ({ dispose() {} });
        fire() {}
        dispose() {}
      },
    };
  }
  return origLoad.call(this, request, parent, isMain);
};
const { Companion } = require('../ide/workspace');

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
test('caps says it could not ask, rather than answering for a daemon it never reached', async () => {
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
test('caps distinguishes a daemon that refused from a daemon that is not there', async () => {
  const c = new Companion('/nowhere');
  (c as any).ask = async () => ({ ok: false, error: 'no' });
  assert.equal(await c.caps(), null, 'a refused about is no more an answer than silence is');
});

/**
 * ★ The sentence itself, held where it is spoken.
 *
 * The fix above only helps if the caller keeps the two apart, and the caller is the one with the
 * words. Read as source because that is where the wording lives; a mutation that puts the old
 * sentence back on the null branch fails here.
 */
test('the command gate names the missing daemon, not a missing door', () => {
  const src = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'doors.ts'), 'utf8');
  const gate = src.slice(src.indexOf('const has = async (cap: string'));
  const body = gate.slice(0, gate.indexOf('\n  };'));
  assert.match(body, /caps === null/, 'the gate must test for "could not ask" at all');
  const nullBranch = body.slice(body.indexOf('caps === null'), body.indexOf('caps.has(cap)'));
  assert.match(nullBranch, /no companion is listening/,
    'an unreachable companion must be reported as unreachable, not as a build without the door');
  assert.doesNotMatch(nullBranch, /does not offer/,
    'the null branch must not claim to know what the companion offers');
});
