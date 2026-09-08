import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { split, numbered } from '../core/look';
import { around, usable } from '../core/complete';

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
  const { prefix, suffix } = around('abcdef', 3);
  assert.equal(prefix, 'abc');
  assert.equal(suffix, 'def');
});

test('the overlap a model repeats is not drawn twice', () => {
  // It re-emits the tail it was given. Drawing that would show the person their own text as a
  // suggestion.
  assert.equal(usable('return x;', 'const f = () => ret'), 'urn x;');
  assert.equal(usable('brand new', 'nothing alike '), 'brand new');
  assert.equal(usable('   ', 'anything'), '');
});
