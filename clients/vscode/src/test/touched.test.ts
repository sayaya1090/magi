import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { touched, pendingAsk } from '../core/touched';
import { Event } from '../core/protocol';

let seq = 0;
const call = (name: string, callId: string, args: unknown): Event =>
  ({ seq: seq++, type: 'part.appended', data: { part: { kind: 'tool-call', toolCall: { callId, name, args } } } });
const result = (callId: string, isError = false): Event =>
  ({ seq: seq++, type: 'part.appended', data: { part: { kind: 'tool-result', toolResult: { callId, isError } } } });

/**
 * The text an edit inserted has to travel, or the marks have nothing to find.
 *
 * The field is `new`, copied from `internal/adapter/tool/builtin/edit.go`. A guessed name fails the
 * quiet way — JSON hands back undefined, the list comes out empty, and the editor marks nothing
 * while nothing errors. That is exactly how this feature sat unimplemented behind a setting that
 * claimed to switch it.
 */
test('an edit carries the text it inserted', () => {
  const t = touched([call('edit', 'c1', { path: '/a.ts', old: 'before', new: 'after' }), result('c1')]);
  assert.deepEqual(t.named, ['/a.ts']);
  assert.deepEqual(t.inserts, [{ path: '/a.ts', text: 'after' }]);
});

test('a multiedit carries every hunk it inserted', () => {
  const args = { path: '/b.ts', edits: [{ old: 'x', new: 'one' }, { old: 'y', new: 'two' }] };
  const t = touched([call('multiedit', 'c2', args), result('c2')]);
  assert.deepEqual(t.inserts.map((i) => i.text), ['one', 'two']);
});

/** Arguments arrive as an object or as the JSON text of one, depending on the tool. */
test('arguments that arrive as JSON text are read the same way', () => {
  const t = touched([call('edit', 'c3', JSON.stringify({ path: '/c.ts', new: 'body' })), result('c3')]);
  assert.deepEqual(t.inserts, [{ path: '/c.ts', text: 'body' }]);
});

/**
 * A whole file is not an insert to search for. Marking it would highlight everything, which says
 * nothing — the screen decides what a new file means, and it is not this.
 */
test('a file written whole yields no insert', () => {
  const t = touched([call('write', 'c4', { path: '/d.ts', content: 'all of it' }), result('c4')]);
  assert.deepEqual(t.named, ['/d.ts']);
  assert.deepEqual(t.inserts, []);
});

/** A tool that failed changed nothing. Marking its text would point at code that is not there. */
test('a failed edit leaves no mark and no reload', () => {
  const t = touched([call('edit', 'c5', { path: '/e.ts', new: 'never landed' }), result('c5', true)]);
  assert.deepEqual(t.named, []);
  assert.deepEqual(t.inserts, []);
});

/** bash may have written anything. There is no path to name, so none is invented. */
test('bash moves the tree without naming a file', () => {
  const t = touched([call('bash', 'c6', { command: 'make' }), result('c6')]);
  assert.equal(t.unnamed, true);
  assert.deepEqual(t.named, []);
});

/** A call with no result yet has not happened. Reloading on it would throw away unsaved work. */
test('an edit still running is not counted', () => {
  const t = touched([call('edit', 'c7', { path: '/f.ts', new: 'pending' })]);
  assert.deepEqual(t.named, []);
  assert.deepEqual(t.inserts, []);
});

/** What the companion is blocked on, and that a decision clears it. */
test('a permission stands until it is decided', () => {
  const asked: Event = { seq: 1, type: 'permission.requested', data: { callId: 'p1', name: 'bash' } };
  assert.deepEqual(pendingAsk([asked]), { callId: 'p1', what: 'bash' });
  assert.equal(pendingAsk([asked, { seq: 2, type: 'permission.decided', data: {} }]), null);
});
