import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { split, numbered, AMBIENT, ambient } from '../core/look';
import { WINDOW, around, usable } from '../core/complete';

test('a reply that keeps the contract hangs on its lines', () => {
  const { anchored, loose } = split('12\tthis never returns\n40\tthe lock is not released');
  assert.deepEqual(anchored, [[12, 'this never returns'], [40, 'the lock is not released']]);
  assert.equal(loose, '');
});

/**
 * The separator is not only a tab, and this is measured rather than assumed. The JetBrains client
 * saw `5broken link missing colon` live: no tab, so it was read as a remark about the whole file
 * and drew a banner where an inlay belonged. A person reported it as "it shows up in the same blue
 * box", which is what a wrong home for a remark looks like from the outside.
 */
test('a reply that uses a space or a colon still hangs on its line', () => {
  for (const s of ['5 broken link missing colon', '5:broken link', '5. broken link', '5)broken link',
                   '5broken link']) {
    const { anchored } = split(s);
    assert.equal(anchored.length, 1, `not read as anchored: ${s}`);
    assert.equal(anchored[0][0], 5);
    assert.ok(anchored[0][1].includes('broken link'), `text lost: ${s}`);
  }
});

test('a remark with no line to hang on stays loose', () => {
  const { anchored, loose } = split('this file mixes two responsibilities');
  assert.deepEqual(anchored, []);
  assert.equal(loose, 'this file mixes two responsibilities');
});

test('nothing to say is nothing at all', () => {
  const { anchored, loose } = split('   \n\n  ');
  assert.deepEqual(anchored, []);
  assert.equal(loose, '');
});

test('a number with no words is not a remark', () => {
  // "42" alone hangs on nothing and says nothing. Reading it as an anchored note would draw an
  // empty inlay at line 42.
  const { anchored, loose } = split('42');
  assert.deepEqual(anchored, []);
  assert.equal(loose, '42');
});

test('the buffer is numbered from one, absolute', () => {
  assert.equal(numbered('a\nb\nc'), '1\ta\n2\tb\n3\tc');
});

test('the buffer is cut on a line boundary, never mid-line', () => {
  const big = Array.from({ length: 500 }, (_, i) => `line ${i}`).join('\n');
  const out = numbered(big, 200);
  assert.ok(Buffer.byteLength(out, 'utf8') <= 200, 'over budget');
  // Every row still reads as <n><TAB><code>. A mid-line cut would renumber everything after it.
  for (const row of out.split('\n')) assert.match(row, /^\d+\t/, `cut mid-line: ${row}`);
});

test('a completion is asked with both sides of the cursor', () => {
  const { prefix, suffix } = around((f, t) => 'abcdef'.slice(f, t), 3);
  assert.equal(prefix, 'abc');
  assert.equal(suffix, 'def');
});

/**
 * ★ **Only the window is read, not the buffer.**
 *
 * It took the whole text, and the one caller produced that text with `doc.getText()` — the entire
 * document, on every pause in typing, with all but the window thrown away straight after. The
 * function's own sentence ("the file is not") was true of what it SENT and false of what was read.
 * The core says the same thing about the cost — "the buffer travels on every pause in typing" — and
 * the JetBrains client had the same shape in the same place, fixed in the same wave.
 *
 * So the reader is watched: it must be asked for two bounded slices around the cursor and nothing
 * else. A test that only checked the returned strings could not see this — the old one passed
 * throughout.
 */
test('only the window around the cursor is read', () => {
  const asked: [number, number][] = [];
  const { prefix, suffix } = around((f, t) => { asked.push([f, t]); return 'x'.repeat(t - f); }, 50_000, 4000);
  assert.deepEqual(asked, [[46_000, 50_000], [50_000, 54_000]],
    'the reader was asked for something other than the two windows');
  assert.equal(prefix.length, 4000);
  assert.equal(suffix.length, 4000);
});

/**
 * ★ And the DEFAULT window is the one that ships.
 *
 * The tests above pass a budget explicitly, so the default was never exercised — a mutation raising
 * it to four million passed every one of them. The number nobody names in a call is the number every
 * keystroke uses, so it is pinned here: bounded, and small enough that a large file is not copied.
 */
test('the default window is bounded and small', () => {
  const asked: [number, number][] = [];
  around((f, t) => { asked.push([f, t]); return ''; }, 1_000_000);
  const [[pFrom, pTo], [sFrom, sTo]] = asked;
  assert.equal(pTo - pFrom, WINDOW, 'the prefix window is not the declared one');
  assert.equal(sTo - sFrom, WINDOW, 'the suffix window is not the declared one');
  assert.ok(WINDOW > 0 && WINDOW <= 32_768,
    `the default window is ${WINDOW} — a completion needs the code around the cursor, not the file`);
});

/** At the very start there is nothing behind the cursor — and no negative offset is asked for. */
test('the window is clamped at the start of the buffer', () => {
  const asked: [number, number][] = [];
  around((f, t) => { asked.push([f, t]); return ''; }, 10, 4000);
  assert.deepEqual(asked, [[0, 10], [10, 4010]], 'a negative offset was asked for');
});

test('the overlap a model repeats is not drawn twice', () => {
  // It re-emits the tail it was given. Drawing that would show the person their own text as a
  // suggestion.
  assert.equal(usable('return x;', 'const f = () => ret'), 'urn x;');
  assert.equal(usable('brand new', 'nothing alike '), 'brand new');
  assert.equal(usable('   ', 'anything'), '');
});

/**
 * ★ The ambient buffer is cut to the head, and the core keeps exactly that head.
 *
 * `open-file` goes out on every pause in typing — always, because it is ambient context and nobody
 * presses anything for it. The core keeps only `ambientCap` (8KB) of the HEAD and says why in the
 * memory it saves: "holding the whole of a 40MB buffer per session for the daemon's life is memory
 * for nothing". It clamps on STORE, so its memory was safe while the socket carried the whole file
 * every 900ms.
 *
 * Content-neutral, and that is the point: the core keeps the head, this sends the head. Counted in
 * characters against a byte cap deliberately — a character is never fewer than a byte, so this always
 * carries at least the bytes the core keeps and the kept slice is identical.
 */
test('the ambient buffer is cut to its head', () => {
  // ⚠ The head and the tail must be TELLABLE APART. A mutation proved why: with a uniform
  // 'x'.repeat() buffer, `slice(-AMBIENT)` — the tail — has the same length and still passes
  // `startsWith`, so keeping the wrong end read as correct.
  const big = 'HEAD' + 'x'.repeat(AMBIENT * 3) + 'TAIL';
  assert.equal(ambient(big).length, AMBIENT, 'the whole buffer travels as ambient context');
  assert.ok(ambient(big).startsWith('HEAD'), 'the tail was kept instead of the head — the core keeps the head');
  assert.ok(!ambient(big).includes('TAIL'), 'the end of the buffer travelled — the core would throw it away');
  // Under the cap nothing is touched: a small file must arrive whole.
  assert.equal(ambient('short'), 'short');
  // Never fewer bytes than the core keeps, whatever the script.
  const ko = '한'.repeat(AMBIENT);
  assert.ok(Buffer.byteLength(ambient(ko), 'utf8') >= AMBIENT,
    'a multi-byte buffer is cut below what the core would keep — the model would see less');
});
