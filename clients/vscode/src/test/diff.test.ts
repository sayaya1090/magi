import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import {
  extractEditSides,
  approvalDiffUri,
  approvalDiffTitle,
  ApprovalSnapshots,
  determineApprovalDiffKind,
  AskStore,
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

  // replaceAll explicitly falsy is accepted (case-insensitive and common falsy representations)
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'false' })?.old, 'a');
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'FALSE' })?.old, 'a');
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'False' })?.old, 'a');
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: '0' })?.old, 'a');
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'no' })?.old, 'a');
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: 'off' })?.old, 'a');
  assert.equal(extractEditSides('edit', { old: 'a', new: 'b', replaceAll: '' })?.old, 'a');

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

test('determineApprovalDiffKind identifies diff eligibility without phantom buttons', () => {
  // Valid edit sides
  assert.equal(
    determineApprovalDiffKind({
      what: 'edit',
      args: { path: 'a.ts', old: 'x', new: 'y' },
    }),
    'sides'
  );

  // Edit with replaceAll: 'FALSE' is valid sides
  assert.equal(
    determineApprovalDiffKind({
      what: 'edit',
      args: { path: 'a.ts', old: 'x', new: 'y', replaceAll: 'FALSE' },
    }),
    'sides'
  );

  // Edit with replaceAll: 'TRUE' and no diff is none (never show button if host cannot diff)
  assert.equal(
    determineApprovalDiffKind({
      what: 'edit',
      args: { path: 'a.ts', old: 'x', new: 'y', replaceAll: 'TRUE' },
    }),
    'none'
  );

  // Anchored edit with no diff is none
  assert.equal(
    determineApprovalDiffKind({
      what: 'edit',
      args: { path: 'a.ts', old: 'x', new: 'y', at: 10 },
    }),
    'none'
  );

  // Write tool with unified diff is patch
  assert.equal(
    determineApprovalDiffKind({
      what: 'write',
      args: { path: 'README.md' },
      diff: '--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-a\n+b\n',
    }),
    'patch'
  );

  // Bash tool without diff is none
  assert.equal(
    determineApprovalDiffKind({
      what: 'bash',
      args: { command: 'ls -la' },
    }),
    'none'
  );

  // Empty or null ask is none
  assert.equal(determineApprovalDiffKind(null), 'none');
  assert.equal(determineApprovalDiffKind(undefined), 'none');
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

test('ApprovalSnapshots guarantees immutability across repeated writes with same key', () => {
  const cache = new ApprovalSnapshots(5);
  const first = cache.put('uri://doc1', 'first-content');
  assert.equal(first, true, 'first write accepted');
  assert.equal(cache.get('uri://doc1'), 'first-content');

  // Second write with same key must not overwrite (truly immutable)
  const second = cache.put('uri://doc1', 'modified-content');
  assert.equal(second, false, 'overwrite rejected');
  assert.equal(cache.get('uri://doc1'), 'first-content', 'original snapshot content retained');
});

test('ApprovalSnapshots protects open pinned tabs when cache capacity is exceeded', () => {
  const pinned = new Set<string>(['uri://tab1']);
  const cache = new ApprovalSnapshots(2, (key) => pinned.has(key));

  cache.put('uri://tab1', 'content-tab1');
  cache.put('uri://tab2', 'content-tab2');

  // Adding 3rd entry exceeds capacity (2).
  // tab1 is pinned (currently open in editor tab). tab2 is unpinned.
  cache.put('uri://tab3', 'content-tab3');

  assert.equal(cache.get('uri://tab1'), 'content-tab1', 'open tab document must be protected from eviction');
  assert.equal(cache.has('uri://tab2'), false, 'unpinned document was evicted');
  assert.equal(cache.get('uri://tab3'), 'content-tab3');

  // If both remaining entries are pinned, neither is evicted
  pinned.add('uri://tab3');
  cache.put('uri://tab4', 'content-tab4');

  assert.equal(cache.has('uri://tab1'), true, 'pinned tab1 retained even when capacity exceeded');
  assert.equal(cache.has('uri://tab3'), true, 'pinned tab3 retained even when capacity exceeded');
  assert.equal(cache.has('uri://tab4'), true, 'new entry added');

  // Once tab1 is closed / unpinned, it can now be evicted
  pinned.delete('uri://tab1');
  cache.put('uri://tab5', 'content-tab5');
  assert.equal(cache.has('uri://tab1'), false, 'unpinned document evicted in subsequent put');

  // Delete and clear
  cache.delete('uri://tab3');
  assert.equal(cache.has('uri://tab3'), false);
  cache.clear();
  assert.equal(cache.size, 0);
});

test('ApprovalSnapshots.protectTemp protects both left and right sides during creation (limit 1 + 1 pinned)', () => {
  const pinned = new Set<string>(['uri://existing1']);
  // Limit 1, and 1 existing pinned document
  const cache = new ApprovalSnapshots(1, (k) => pinned.has(k));
  cache.put('uri://existing1', 'existing-content');

  // Without protectTemp:
  // Storing left then right would evict left before it can even be opened
  const unprotect = cache.protectTemp(['uri://new-left', 'uri://new-right']);
  try {
    cache.put('uri://new-left', 'left-content');
    cache.put('uri://new-right', 'right-content');

    // Both sides must be present in cache simultaneously for vscode.diff
    assert.equal(cache.get('uri://new-left'), 'left-content', 'left side must not be evicted before diff opens');
    assert.equal(cache.get('uri://new-right'), 'right-content', 'right side must be present');
    assert.equal(cache.get('uri://existing1'), 'existing-content', 'existing pinned document protected');

    // Simulate successful open in editor: both are now pinned
    pinned.add('uri://new-left');
    pinned.add('uri://new-right');
  } finally {
    unprotect();
  }

  // After unprotect, both remain because they are now in pinned set
  assert.equal(cache.get('uri://new-left'), 'left-content');
  assert.equal(cache.get('uri://new-right'), 'right-content');
});

test('ApprovalSnapshots.protectTemp protects both sides under default limit 100 with 100 pinned documents', () => {
  const pinned = new Set<string>();
  const cache = new ApprovalSnapshots(100, (k) => pinned.has(k));

  // Fill with 100 pinned documents
  for (let i = 0; i < 100; i++) {
    const k = `uri://pinned-${i}`;
    pinned.add(k);
    cache.put(k, `content-${i}`);
  }
  assert.equal(cache.size, 100);

  // New diff arrives
  const leftKey = 'uri://new-left-100';
  const rightKey = 'uri://new-right-100';
  const unprotect = cache.protectTemp([leftKey, rightKey]);
  try {
    cache.put(leftKey, 'left-100');
    cache.put(rightKey, 'right-100');

    assert.equal(cache.get(leftKey), 'left-100', 'new left side preserved under 100 pinned docs');
    assert.equal(cache.get(rightKey), 'right-100', 'new right side preserved under 100 pinned docs');
  } finally {
    unprotect();
  }
});

test('ApprovalSnapshots.protectTemp does not leak temporary protection on failure', () => {
  const pinned = new Set<string>();
  const cache = new ApprovalSnapshots(1, (k) => pinned.has(k));
  cache.put('uri://pinned-only', 'pinned-content');
  pinned.add('uri://pinned-only');

  const leftKey = 'uri://fail-left';
  const rightKey = 'uri://fail-right';

  const unprotect = cache.protectTemp([leftKey, rightKey]);
  assert.equal(cache.tempPinnedSize, 2);

  try {
    cache.put(leftKey, 'fail-left-content');
    cache.put(rightKey, 'fail-right-content');
    throw new Error('Simulated vscode.diff launch failure');
  } catch {
    // caught failure
  } finally {
    unprotect();
  }

  // Temporary protection must be completely released
  assert.equal(cache.tempPinnedSize, 0, 'no temporary protection leaked on error');
  // Excess entries pruned back to maxEntries (1)
  assert.equal(cache.has('uri://pinned-only'), true);
  assert.equal(cache.size, 1, 'unpinned failed entries pruned after unprotect');
});

test('ApprovalSnapshots.evictExcess prunes closed documents without needing new puts', () => {
  const pinned = new Set<string>(['uri://tabA', 'uri://tabB', 'uri://tabC']);
  const cache = new ApprovalSnapshots(1, (k) => pinned.has(k));

  cache.put('uri://tabA', 'contentA');
  cache.put('uri://tabB', 'contentB');
  cache.put('uri://tabC', 'contentC');

  assert.equal(cache.size, 3, 'all 3 pinned tabs preserved despite maxEntries=1');

  // User closes tabA: document close event occurs
  pinned.delete('uri://tabA');
  const evictedCount = cache.evictExcess();

  assert.equal(evictedCount, 1, 'evictExcess immediately prunes tabA');
  assert.equal(cache.has('uri://tabA'), false, 'tabA evicted without waiting for a new put');
  assert.equal(cache.has('uri://tabB'), true);
  assert.equal(cache.has('uri://tabC'), true);

  // User closes tabB and tabC
  pinned.delete('uri://tabB');
  pinned.delete('uri://tabC');
  cache.evictExcess();

  assert.equal(cache.size, 1, 'pruned back down to maxEntries=1');
  assert.equal(cache.has('uri://tabC'), true, 'newest tab remains within capacity');
});

test('AskStore associates asks with companion and session at arrival time', () => {
  const store = new AskStore(3);
  const ask1 = {
    kind: 'permission' as const,
    callId: 'call-1',
    what: 'edit',
    args: '{"path":"a.ts","old":"x","new":"y"}',
  };

  store.record(ask1, '/workspace/project-a', 'session-100');
  const stored1 = store.get('call-1');
  assert.ok(stored1);
  assert.equal(stored1.companionId, '/workspace/project-a');
  assert.equal(stored1.sessionId, 'session-100');

  // Even after session switch, stored ask retains session-100
  assert.equal(store.get('call-1')?.sessionId, 'session-100');

  // Bounded capacity: FIFO eviction
  store.record({ kind: 'permission', callId: 'call-2', what: 'write' }, '/workspace/project-a', 'session-100');
  store.record({ kind: 'permission', callId: 'call-3', what: 'bash' }, '/workspace/project-a', 'session-100');
  assert.equal(store.size, 3);

  // 4th ask evicts oldest (call-1)
  store.record({ kind: 'permission', callId: 'call-4', what: 'edit' }, '/workspace/project-a', 'session-100');
  assert.equal(store.has('call-1'), false, 'oldest ask evicted when exceeding capacity');
  assert.equal(store.has('call-4'), true);

  store.delete('call-2');
  assert.equal(store.has('call-2'), false);

  store.clear();
  assert.equal(store.size, 0);
});
