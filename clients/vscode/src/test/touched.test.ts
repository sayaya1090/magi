import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
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
/**
 * The ask carries WHICH KIND it is, and both kinds are seen.
 *
 * ⚠ This test used to pin the defect. It asserted `{callId, what}` — the shape without `kind` — and
 * stayed green while the screen was broken: `drawAsk` tests `kind === 'permission'` to put up
 * allow/deny/always, so every permission prompt fell through to the QUESTION branch and drew the
 * tool name with a free-text box. The three buttons were in the code and never ran once.
 *
 * And only one kind was ever built. `question.requested` — the `ask_user` tool, with its options —
 * was written by the core and read by nothing here, so a question raised no ask box at all. The two
 * are answered through different doors (`permission` takes a verdict, `answer` takes a sentence),
 * which is exactly why the kind has to travel.
 */
test('a permission stands until it is decided, and says it is a permission', () => {
  const asked: Event = { seq: 1, type: 'permission.requested', data: { callId: 'p1', name: 'bash' } };
  assert.deepEqual(pendingAsk([asked]), { kind: 'permission', callId: 'p1', what: 'bash' });
  assert.equal(pendingAsk([asked, { seq: 2, type: 'permission.decided', data: {} }]), null);
});

test('a question raises an ask, with its options', () => {
  const asked: Event = { seq: 1, type: 'question.requested',
    data: { callId: 'q1', question: 'which branch?', options: ['main', 'dev'], index: 2, total: 3 } };
  const a = pendingAsk([asked]);
  assert.ok(a, 'a question raised no ask at all — the core wrote it and nothing read it');
  assert.equal(a!.kind, 'question', 'a question is drawn as a permission — the wrong three buttons');
  assert.equal(a!.what, 'which branch?');
  assert.deepEqual(a!.options, ['main', 'dev'], 'the shortcuts are dropped and only a text box is left');
  assert.equal(a!.index, 2);
  assert.equal(a!.total, 3);

  // Answered closes it, the same way a decision closes a permission.
  assert.equal(pendingAsk([asked, { seq: 2, type: 'question.answered', data: {} }]), null);

  // And the screen tells the two apart — the branch exists and must keep its input.
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  assert.ok(/a\.kind === 'permission'/.test(chat), 'the screen no longer tells the two kinds apart');
  assert.ok(/a\.options/.test(chat), "the screen never draws a question's shortcuts");
});
