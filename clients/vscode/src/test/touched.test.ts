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
  assert.deepEqual(pendingAsk([asked]), {
    kind: 'permission', callId: 'p1', what: 'bash',
    args: undefined, reason: undefined, diff: undefined,
  });
  assert.equal(pendingAsk([asked, { seq: 2, type: 'permission.decided', data: {} }]), null);
});

/**
 * A permission says WHAT is being allowed, not just which tool.
 *
 * The screen drew `magi wants to run: bash` and nothing else, so a person pressed allow without the
 * command — or approved an edit without seeing what it changes. The core carries the rest of the
 * request for exactly this reason, in its own words: "so a viewer draws the prompt rather than a
 * description of it". A tool name is the description.
 *
 * The JetBrains client calls the gap by its name — you press without knowing what you are allowing
 * — and notes that the treatment was inverted against the stakes: the place with the most riding on
 * it was the quiet one. It also fixed the empty case, which is the second half here: three buttons
 * over a blank space read as "there is nothing to it".
 */
test('a permission carries what it is allowing', () => {
  const ask = (d: Record<string, unknown>): Event =>
    ({ seq: 1, type: 'permission.requested', data: { callId: 'p1', name: 'bash', ...d } });

  const cmd = pendingAsk([ask({ args: '{"command":"rm -rf build"}' })])!;
  assert.equal(cmd.args, '{"command":"rm -rf build"}', 'the thing being allowed is not carried');

  // The VALUE, not its rendering — an object is stringified once, a string is already text.
  assert.equal(pendingAsk([ask({ args: { command: 'ls' } })])!.args, '{"command":"ls"}');

  const why = pendingAsk([ask({ reason: 'writes outside the workspace', diff: '- a\n+ b' })])!;
  assert.equal(why.reason, 'writes outside the workspace');
  assert.equal(why.diff, '- a\n+ b', 'the change is computed by the core and dropped here');

  // Blank is not a subject: an empty string must not draw as an empty line beside three buttons.
  const blank = pendingAsk([ask({ reason: '   ', diff: '' })])!;
  assert.equal(blank.reason, undefined);
  assert.equal(blank.diff, undefined);

  // And the screen draws all three, and says so when none came.
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const at = chat.indexOf("if (a.kind === 'permission') {");
  const branch = chat.slice(at, chat.indexOf('return;', at));
  // ⚠ Naming them is not drawing them. A mutation that emptied the loop's source array left the
  // three names sitting in the (now dead) literal and this guard green — presence is not effect,
  // for the eighth time in this session. So the iterated value itself is pinned.
  assert.ok(/\[\['args', a\.args\], \['reason', a\.reason\], \['diff', a\.diff\]\]/.test(branch),
    'the permission branch no longer walks all three parts of what is being decided');
  assert.ok(/\.append\(/.test(branch), 'it is read and never put on the screen');
  assert.ok(/!a\.args && !a\.reason && !a\.diff/.test(branch),
    'nothing came and the screen said nothing — three buttons over a blank space read as "there is nothing to it"');
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

  // Which of how many. The core says why it travels: a viewer "has no other way to know that
  // answering this one leads to another", and somebody who answers the first of five otherwise
  // believes they are done. Carried since the kind was fixed and drawn by nothing until now —
  // the ninth time in this session that a field reached a row and no screen.
  assert.ok(/a\.total > 1/.test(chat), 'the screen never says which of how many — and never that there are more');
  assert.ok(/a\.index/.test(chat), 'the position is not drawn, only the count');
  // Only when there is more than one: "(1/1)" beside a lone question is noise pretending to inform.
  assert.ok(!/a\.total >= 1|a\.total > 0/.test(chat), 'a lone question is labelled "(1/1)"');
});
