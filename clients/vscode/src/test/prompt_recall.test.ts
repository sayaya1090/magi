import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { PromptRecall, onEdgeLine } from '../core/prompt_recall';

test('up walks older, down to the bottom hands back what was being typed', () => {
  const r = new PromptRecall();
  r.load(['third', 'second', 'first']);
  assert.equal(r.older('draft'), 'third');
  assert.equal(r.older('third'), 'second');
  assert.equal(r.older('second'), 'first');
  assert.equal(r.older('first'), undefined, 'nothing older leaves the input alone');
  assert.equal(r.newer(), 'second');
  assert.equal(r.newer(), 'third');
  assert.equal(r.newer(), 'draft');
  assert.equal(r.newer(), undefined);
  assert.equal(r.browsing, false);
});

test('a sent line is first at once, and only once', () => {
  const r = new PromptRecall();
  r.load(['old', 'same']);
  r.remember('same');
  assert.equal(r.older(''), 'same');
  assert.equal(r.older('same'), 'old');
});

test('a list arriving mid-browse does not move the reader', () => {
  const r = new PromptRecall();
  r.load(['a', 'b']);
  r.older('');
  r.load(['x', 'a', 'b']);
  assert.equal(r.older('a'), 'b');
});

test('recall only from the edge line, so multi-line editing keeps its arrows', () => {
  assert.equal(onEdgeLine('one\ntwo', 2, true), true);
  assert.equal(onEdgeLine('one\ntwo', 5, true), false);
  assert.equal(onEdgeLine('one\ntwo', 5, false), true);
  assert.equal(onEdgeLine('one\ntwo', 2, false), false);
});
