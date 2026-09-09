import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { DISPATCH_MARK, dispatchLabel, handState, peerLabel, peers } from '../core/handoff';

/**
 * ★ The mark is copied from the core, not invented.
 *
 * `fleet.go` says that constant is three things at once — the sentence a person reads, the fact the
 * no-chaining rule is read off, and the string the fleet package greps to rebuild who handed what
 * to whom — and that a marker with three readers must have exactly one spelling. A label sent
 * without it loses all three, and nothing errors.
 */
test('the dispatch label opens with the mark the core greps for', () => {
  assert.equal(DISPATCH_MARK, '— asked by ');
  assert.ok(dispatchLabel('web').startsWith(DISPATCH_MARK));
  assert.match(dispatchLabel('web'), /^— asked by web,/);
});

/** The far side answers in its own transcript. Not saying so hands somebody a reply channel that does not exist. */
test('the label says there is no way back', () => {
  assert.match(dispatchLabel('web'), /no reply channel/i);
});

/**
 * ★ A refusal and a dead connection are DIFFERENT.
 *
 * A refusal means stop asking: the far side restarted or the receipt expired, and it will never
 * know this one. A connection failure means we could not ask, and asking again is right. Folding
 * them together polls a dead receipt for the life of the window.
 */
test('a refusal ends the wait and a dead connection does not', () => {
  const refused = handState({ ok: false, error: 'no record of that request' });
  assert.equal(refused.over, true);
  assert.match(refused.line, /no record/);

  const unreachable = handState(null);
  assert.equal(unreachable.over, false, 'a companion we could not reach must be asked again');
});

/**
 * ⚠ **This test used to invent the wire.** It fed `state: 'reading the tests'` and asserted the
 * line came back with it — but `state` has never been a field of `handover`, so the branch it was
 * exercising could not run against a real daemon. The reader had the same two invented names
 * (`state`, `error`) and both of its branches were dead: an invented name reads as `undefined`,
 * `undefined` is falsy, and nothing anywhere said so. The wire shape is typed now, so a test that
 * makes one up stops compiling.
 */
test('work still running is not over', () => {
  const r = handState({ ok: true, handover: { done: false } });
  assert.equal(r.over, false);
  assert.match(r.line, /working/);
});

test('an answer ends it, and the first line is what a panel shows', () => {
  const r = handState({ ok: true, handover: { done: true, answer: 'fixed it in a.go\nand also b.go' } });
  assert.equal(r.over, true);
  assert.match(r.line, /fixed it in a\.go/);
  assert.ok(!r.line.includes('b.go'), 'the panel line should be one line');
});

/**
 * The OTHER ending. `done` means a turn finished and `answer` is what was said; `over` means
 * nothing is coming and `news` says why. The core spells out what folding them costs — "a caller
 * that collapsed them would report a crash as an empty answer" — and this reader read only `done`,
 * so a handover that died read as "working" and this window polled a receipt nobody would answer,
 * for as long as it was open.
 */
test('a handover that ended without finishing says so, and says why', () => {
  const r = handState({ ok: true, handover: { over: true, news: 'the companion restarted' } });
  assert.equal(r.over, true, 'nothing is coming and the window is still waiting for it');
  assert.match(r.line, /the companion restarted/, 'the reason came and was dropped');

  // Ended with no reason is still ended — silence here is what kept the poll alive.
  const bare = handState({ ok: true, handover: { over: true } });
  assert.equal(bare.over, true);
  assert.ok(bare.line.trim(), 'it ended and the line is blank — that reads as nothing happened');

  // And `over` is asked before `done`: an ending that did not finish must not report a finish.
  const both = handState({ ok: true, handover: { over: true, news: 'killed', done: true, answer: '' } });
  assert.match(both.line, /killed/, 'a crash was reported as an empty answer — the core names this exactly');
});

/** Nothing back yet is not an error, and not silence either. */
test('handed over with nothing back yet says that', () => {
  const r = handState({ ok: true });
  assert.equal(r.over, false);
  assert.match(r.line, /nothing back/);
});

/** A row with no socket cannot be dialled, so it is not offered. */
test('the roster drops rows with nothing to dial', () => {
  const list = peers({
    ok: true,
    roster: [{ socket: '/tmp/a.sock', name: 'web' }, { name: 'ghost' }, { socket: '' }],
  });
  assert.equal(list.length, 1);
  assert.equal(list[0].name, 'web');
});

/** The label falls back to the socket's own name — a companion with no name is still dialable. */
test('a companion with no name is named by its folder, then by its socket', () => {
  // The folder is the name a person knows it by; the socket carries that plus a hash nobody reads.
  assert.equal(peerLabel({ socket: '/x/daemon-web-abc.sock', workdir: '/home/me/web' }), 'web');
  assert.equal(peerLabel({ socket: '/x/daemon-web-abc.sock', workdir: '/home/me/web/' }), 'web');
  // No folder either — then the socket's filename is all there is.
  assert.equal(peerLabel({ socket: '/x/daemon-web-abc.sock' }), 'daemon-web-abc.sock');
  assert.equal(peerLabel({ socket: '/x/y.sock', name: '  ' }), 'y.sock');
  // A name always wins: somebody chose it.
  assert.equal(peerLabel({ socket: '/x/y.sock', name: 'hub', workdir: '/home/me/web' }), 'hub');
});

test('a refused roster yields nobody', () => {
  assert.deepEqual(peers({ ok: false, error: 'no' }), []);
  assert.deepEqual(peers(null), []);
});

/**
 * The hand-off list offers only companions this window can dial.
 *
 * ⚠ A socket string is not a door. A sighting is a row another machine signed and its socket is a
 * path over THERE — the core says "visible, not commandable — its socket is a path on a machine
 * this caller has no door to". The filter asked only whether the string was present, so the picker
 * offered companions on other machines; handing work to one reached nothing, and the receipt was
 * then polled until the window closed (the very failure `handState`'s comment is about).
 *
 * The JetBrains client asks for both halves in one breath: `it.live && !it.sighting`.
 */
test('a companion on another machine is not offered as a hand-off target', () => {
  const list = peers({ ok: true, roster: [
    { socket: '/here/daemon-web-1.sock', workdir: '/w' },
    { socket: '/over/there/daemon-ws-9.sock', workdir: '/x', sighting: true },
    { workdir: '/no-socket' },
  ] });
  assert.equal(list.length, 1, 'the picker offers somebody this window cannot reach');
  assert.equal(list[0].socket, '/here/daemon-web-1.sock');
});
