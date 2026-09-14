import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import {
  extractEditSides,
  approvalDiffUri,
  approvalDiffTitle,
  ApprovalSnapshots,
} from '../core/diff';

test('extractEditSides identifies valid unanchored edit calls', () => {
  const validArgs = JSON.stringify({
    path: 'src/utils.ts',
    old: 'function oldFn() {}',
    new: 'function newFn() {}',
  });
  const sides = extractEditSides('edit', validArgs);
  assert.ok(sides, 'sides should be extracted for standard edit call');
  assert.equal(sides.path, 'src/utils.ts');
  assert.equal(sides.old, 'function oldFn() {}');
  assert.equal(sides.new, 'function newFn() {}');

  // Accepts parsed object as well
  const objectArgs = {
    path: 'lib/app.py',
    old: 'x = 1',
    new: 'x = 2',
    replaceAll: false,
  };
  const objSides = extractEditSides('EDIT', objectArgs);
  assert.ok(objSides);
  assert.equal(objSides.path, 'lib/app.py');
});

test('extractEditSides rejects non-edit tools and non-substitution edits', () => {
  // Non-edit tools
  assert.equal(extractEditSides('bash', { old: 'a', new: 'b' }), null);
  assert.equal(extractEditSides('write', { path: 'a.txt', content: 'hello' }), null);
  assert.equal(extractEditSides(undefined, { old: 'a', new: 'b' }), null);

  // Anchored edits (at is specified and non-empty)
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', at: '10' }), null);
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', at: 10 }), null);
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', at: '   ' })?.old, 'a', 'empty at is ignored');

  // Full file replacement (replaceAll is truthy or unfamiliar)
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: true }), null);
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'true' }), null);
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'yes' }), null);
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: '1' }), null);
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'unknown-mode' }), null);

  // replaceAll explicitly falsy is accepted
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'false' })?.old, 'a');
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: '0' })?.old, 'a');

  // Missing or non-string old/new
  assert.equal(extractEditSides('edit', { old: 123, new: 'b' }), null);
  assert.equal(extractEditSides('edit', { old: 'a' }), null);
  assert.equal(extractEditSides('edit', '{invalid json'), null);
});

test('extractEditSides handles missing path gracefully', () => {
  const sides = extractEditSides('edit', { old: 'foo', new: 'bar' });
  assert.ok(sides);
  assert.equal(sides.path, '변경');
});

test('approvalDiffUri incorporates companion, session, callId, side, and filename safely', () => {
  const uri = approvalDiffUri('/Users/test/workspace', 'session-1', 'call-99', 'before', 'index.ts');
  assert.match(uri, /^magi-diff:\//);
  assert.ok(uri.includes('call-99'));
  assert.ok(uri.includes('before'));
  assert.ok(uri.includes('index.ts'));

  // Korean and special characters are encoded
  const koUri = approvalDiffUri('/프로젝트/경로', '세션-1', '호출-1', 'patch', '변경 내역.diff');
  assert.ok(!koUri.includes(' '));
  assert.ok(koUri.includes(encodeURIComponent('변경 내역.diff')));
});

test('approvalDiffTitle accurately labels chunk differences vs raw patch', () => {
  assert.equal(
    approvalDiffTitle('test.ts', true),
    'test.ts (치환 전 조각 ↔ 치환 후 조각)',
    'substitution chunks must be labelled as chunks, not full file'
  );
  assert.equal(
    approvalDiffTitle('test.ts', false),
    'magi 승인 — test.ts'
  );
});

test('ApprovalSnapshots retains immutable snapshots and respects capacity', () => {
  const cache = new ApprovalSnapshots(3);
  cache.put('k1', 'content1');
  cache.put('k2', 'content2');
  cache.put('k3', 'content3');

  assert.equal(cache.get('k1'), 'content1');
  assert.equal(cache.get('k2'), 'content2');

  // Overwriting with same key updates without dropping
  cache.put('k2', 'content2-updated');
  assert.equal(cache.get('k2'), 'content2-updated');

  // Exceeding capacity evicts oldest key
  cache.put('k4', 'content4');
  assert.equal(cache.has('k1'), false, 'oldest entry evicted');
  assert.equal(cache.get('k4'), 'content4');

  cache.clear();
  assert.equal(cache.has('k2'), false);
  assert.equal(cache.has('k3'), false);
  assert.equal(cache.has('k4'), false);
});
