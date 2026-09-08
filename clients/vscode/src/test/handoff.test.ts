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

test('work still running is not over', () => {
  const r = handState({ ok: true, handover: { done: false, state: 'reading the tests' } });
  assert.equal(r.over, false);
  assert.match(r.line, /reading the tests/);
});

test('an answer ends it, and the first line is what a panel shows', () => {
  const r = handState({ ok: true, handover: { done: true, answer: 'fixed it in a.go\nand also b.go' } });
  assert.equal(r.over, true);
  assert.match(r.line, /fixed it in a\.go/);
  assert.ok(!r.line.includes('b.go'), 'the panel line should be one line');
});

/** A failure ends it too — and says so, rather than reading as still working for ever. */
test('a failure ends it', () => {
  const r = handState({ ok: true, handover: { error: 'the turn crashed' } });
  assert.equal(r.over, true);
  assert.match(r.line, /the turn crashed/);
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
