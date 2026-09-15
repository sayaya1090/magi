import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { Event } from '../core/protocol';
import {
  makeAssistantOutputId,
  makeToolResultOutputId,
  parseOutputId,
  resolveOutputItem,
  outputUri,
  defaultOutputFilename,
  OutputSnapshots,
} from '../core/output';

test('makeAssistantOutputId and makeToolResultOutputId create valid IDs', () => {
  assert.equal(makeAssistantOutputId(42), 'assistant:42');
  assert.equal(makeToolResultOutputId('call_1'), 'tool:call_1');
  assert.equal(makeToolResultOutputId('call_1', 99), 'tool:call_1:99');

  assert.deepEqual(parseOutputId('assistant:42'), { kind: 'assistant', seq: 42 });
  assert.deepEqual(parseOutputId('tool:call_1'), { kind: 'tool-result', callId: 'call_1', resultSeq: undefined });
  assert.deepEqual(parseOutputId('tool:call_1:99'), { kind: 'tool-result', callId: 'call_1', resultSeq: 99 });

  // Invalid IDs
  assert.equal(parseOutputId(''), null);
  assert.equal(parseOutputId('assistant:'), null);
  assert.equal(parseOutputId('assistant:abc'), null);
  assert.equal(parseOutputId('assistant:-5'), null);
  assert.equal(parseOutputId('tool:'), null);
  assert.equal(parseOutputId('tool::99'), null);
  assert.equal(parseOutputId('other:123'), null);
});

test('resolveOutputItem preserves assistant raw text without trim, clip, or line-ending changes (>100 chars, newlines, trailing newline)', () => {
  const longText = '  line 1: leading spaces preserved\n' +
    'line 2: ' + 'x'.repeat(150) + '\n' +
    'line 3: end of multiline response\n\n';

  const events: Event[] = [
    {
      seq: 10,
      type: 'part.appended',
      data: {
        role: 'assistant',
        messageId: 'msg_1',
        part: { kind: 'text', text: longText },
      },
    },
  ];

  const item = resolveOutputItem(events, 'assistant:10');
  assert.ok(item, 'assistant output item must be resolved');
  assert.equal(item.kind, 'assistant');
  assert.equal(item.language, 'markdown');
  assert.equal(item.title, '모델 답변 (seq 10)');
  assert.equal(item.content, longText, 'verbatim text must be preserved without clipping to 100 chars or trimming whitespace');
});

test('resolveOutputItem supports empty string as valid assistant output', () => {
  const events: Event[] = [
    {
      seq: 15,
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: { kind: 'text', text: '' },
      },
    },
  ];

  const item = resolveOutputItem(events, 'assistant:15');
  assert.ok(item);
  assert.equal(item.content, '');
  assert.equal(item.language, 'markdown');
});

test('resolveOutputItem rejects unfinalized, draft, or non-assistant parts', () => {
  const events: Event[] = [
    { seq: 1, type: 'part.delta', data: { kind: 'text', text: 'streaming chunk' } },
    { seq: 2, type: 'part.appended', data: { role: 'user', part: { kind: 'text', text: 'user prompt' } } },
    { seq: 3, type: 'part.appended', data: { role: 'assistant', part: { kind: 'reasoning', text: 'thinking' } } },
  ];

  assert.equal(resolveOutputItem(events, 'assistant:1'), null, 'draft delta must not be resolvable');
  assert.equal(resolveOutputItem(events, 'assistant:2'), null, 'user prompt must not be resolvable');
  assert.equal(resolveOutputItem(events, 'assistant:3'), null, 'reasoning must not be resolvable as assistant text');
  assert.equal(resolveOutputItem(events, 'assistant:999'), null, 'non-existent seq returns null');
});

test('resolveOutputItem distinguishes tool call seq from tool result seq and resolves tool name from call', () => {
  const events: Event[] = [
    {
      seq: 20, // Tool call event seq
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: {
          kind: 'tool-call',
          toolCall: { callId: 'call_bash_1', name: 'bash', args: { command: 'git status' } },
        },
      },
    },
    {
      seq: 21, // Intervening unrelated event
      type: 'todos.changed',
      data: { todos: [] },
    },
    {
      seq: 25, // Tool result event seq (distinct and non-adjacent to call seq 20)
      type: 'part.appended',
      data: {
        role: 'tool',
        part: {
          kind: 'tool-result',
          toolResult: {
            callId: 'call_bash_1',
            content: 'On branch main\nChanges not staged for commit:\n\tmodified: foo.ts\n',
            isError: false,
          },
        },
      },
    },
  ];

  // Trying to resolve by tool call seq (20) must fail because 20 is not a tool-result
  assert.equal(resolveOutputItem(events, 'tool:call_bash_1:20'), null);

  // Resolving by exact tool result seq (25)
  const item = resolveOutputItem(events, 'tool:call_bash_1:25');
  assert.ok(item, 'must resolve tool result using result seq');
  assert.equal(item.kind, 'tool-result');
  assert.equal(item.language, 'plaintext');
  assert.equal(item.title, 'bash 결과');
  assert.equal(item.content, 'On branch main\nChanges not staged for commit:\n\tmodified: foo.ts\n');

  // Resolving by callId alone also finds the tool-result event and tool name
  const itemByCall = resolveOutputItem(events, 'tool:call_bash_1');
  assert.ok(itemByCall);
  assert.equal(itemByCall.title, 'bash 결과');
  assert.equal(itemByCall.content, item.content);
});

test('resolveOutputItem formats structured tool results with JSON and marks (JSON) in title', () => {
  const structuredData = {
    status: 'ok',
    count: 3,
    items: ['item1', 'item2', 'item3'],
    details: { nested: true },
  };

  const events: Event[] = [
    {
      seq: 30,
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: {
          kind: 'tool-call',
          toolCall: { callId: 'call_lookup_1', name: 'search_symbols', args: {} },
        },
      },
    },
    {
      seq: 35,
      type: 'part.appended',
      data: {
        role: 'tool',
        part: {
          kind: 'tool-result',
          toolResult: {
            callId: 'call_lookup_1',
            content: structuredData,
            isError: false,
          },
        },
      },
    },
  ];

  const item = resolveOutputItem(events, 'tool:call_lookup_1:35');
  assert.ok(item);
  assert.equal(item.kind, 'tool-result');
  assert.equal(item.language, 'json');
  assert.equal(item.title, 'search_symbols 결과 (JSON)');
  assert.equal(item.content, JSON.stringify(structuredData, null, 2));
});

test('resolveOutputItem handles tool failure result, empty string result, and null/undefined content', () => {
  const events: Event[] = [
    // 1. Tool failure result with string content (isError: true)
    {
      seq: 40,
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: { kind: 'tool-call', toolCall: { callId: 'call_fail', name: 'cargo_check' } },
      },
    },
    {
      seq: 41,
      type: 'part.appended',
      data: {
        role: 'tool',
        part: {
          kind: 'tool-result',
          toolResult: { callId: 'call_fail', content: 'error[E0425]: cannot find value `foo`', isError: true },
        },
      },
    },
    // 2. Tool result with empty string content
    {
      seq: 42,
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: { kind: 'tool-call', toolCall: { callId: 'call_empty', name: 'touch' } },
      },
    },
    {
      seq: 43,
      type: 'part.appended',
      data: {
        role: 'tool',
        part: {
          kind: 'tool-result',
          toolResult: { callId: 'call_empty', content: '', isError: false },
        },
      },
    },
    // 3. Tool result with null / undefined content
    {
      seq: 44,
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: { kind: 'tool-call', toolCall: { callId: 'call_null', name: 'noop' } },
      },
    },
    {
      seq: 45,
      type: 'part.appended',
      data: {
        role: 'tool',
        part: {
          kind: 'tool-result',
          toolResult: { callId: 'call_null', content: null, isError: false },
        },
      },
    },
  ];

  // Tool failure result is readable and opens normally
  const failItem = resolveOutputItem(events, 'tool:call_fail:41');
  assert.ok(failItem);
  assert.equal(failItem.title, 'cargo_check 결과');
  assert.equal(failItem.content, 'error[E0425]: cannot find value `foo`');
  assert.equal(failItem.language, 'plaintext');

  // Empty string is valid empty result
  const emptyItem = resolveOutputItem(events, 'tool:call_empty:43');
  assert.ok(emptyItem);
  assert.equal(emptyItem.content, '');
  assert.equal(emptyItem.language, 'plaintext');

  // null content is treated as no data -> returns null
  assert.equal(resolveOutputItem(events, 'tool:call_null:45'), null);
});

test('outputUri encodes companion, session, kind, outputId safely and deterministically', () => {
  const uri1 = outputUri('/Users/test/repo', 'sess-123', 'assistant', 'assistant:42', '답변.md');
  assert.match(uri1, /^magi-output:\//);
  assert.ok(uri1.includes('sess-123'));
  assert.ok(uri1.includes(encodeURIComponent('assistant:42')));
  assert.ok(uri1.includes(encodeURIComponent('답변.md')));

  // Determinism: identical arguments yield identical URI
  const uri2 = outputUri('/Users/test/repo', 'sess-123', 'assistant', 'assistant:42', '답변.md');
  assert.equal(uri1, uri2);

  // Different session or companion yields different URI
  const uriDifferentSess = outputUri('/Users/test/repo', 'sess-456', 'assistant', 'assistant:42', '답변.md');
  assert.notEqual(uri1, uriDifferentSess);

  const uriDifferentCompanion = outputUri('/Users/other/repo', 'sess-123', 'assistant', 'assistant:42', '답변.md');
  assert.notEqual(uri1, uriDifferentCompanion);
});

test('defaultOutputFilename derives safe filename with correct extension', () => {
  assert.equal(
    defaultOutputFilename({ outputId: '1', kind: 'assistant', content: '', title: '모델 답변 (seq 10)', language: 'markdown' }),
    '모델 답변 (seq 10).md'
  );
  assert.equal(
    defaultOutputFilename({ outputId: '2', kind: 'tool-result', content: '', title: 'bash 결과', language: 'plaintext' }),
    'bash 결과.txt'
  );
  assert.equal(
    defaultOutputFilename({ outputId: '3', kind: 'tool-result', content: '', title: 'search 결과 (JSON)', language: 'json' }),
    'search 결과 (JSON).json'
  );
});

test('OutputSnapshots guarantees immutability across repeated writes with same key', () => {
  const snapshots = new OutputSnapshots(5);
  const first = snapshots.put('magi-output://key1', 'original-snapshot');
  assert.equal(first, true);
  assert.equal(snapshots.get('magi-output://key1'), 'original-snapshot');

  const second = snapshots.put('magi-output://key1', 'modified-snapshot');
  assert.equal(second, false, 'overwrite must be rejected');
  assert.equal(snapshots.get('magi-output://key1'), 'original-snapshot', 'original content preserved');
});

test('OutputSnapshots protects open pinned tabs and allows temporary overflow', () => {
  const pinned = new Set<string>(['key1']);
  const snapshots = new OutputSnapshots(2, (k) => pinned.has(k));

  snapshots.put('key1', 'content-1');
  snapshots.put('key2', 'content-2');
  assert.equal(snapshots.size, 2);

  // Adding key3 exceeds limit (2): key1 is pinned, key2 is unpinned -> key2 evicted
  snapshots.put('key3', 'content-3');
  assert.equal(snapshots.has('key1'), true, 'pinned key1 must not be evicted');
  assert.equal(snapshots.has('key2'), false, 'unpinned key2 was evicted');
  assert.equal(snapshots.get('key3'), 'content-3');

  // If both key1 and key3 are pinned, adding key4 allows temporary overflow
  pinned.add('key3');
  snapshots.put('key4', 'content-4');
  assert.equal(snapshots.size, 3, 'temporary overflow allowed when all are pinned');
  assert.equal(snapshots.has('key1'), true);
  assert.equal(snapshots.has('key3'), true);
  assert.equal(snapshots.has('key4'), true);

  // When key1 is unpinned and tab closes, evictExcess prunes back to maxEntries (2)
  pinned.delete('key1');
  const evicted = snapshots.evictExcess();
  assert.equal(evicted, 1);
  assert.equal(snapshots.has('key1'), false);
  assert.equal(snapshots.size, 2);
});

test('OutputSnapshots.protectTemp protects in-flight entry with limit 1 and cleans up in finally', () => {
  const pinned = new Set<string>();
  const snapshots = new OutputSnapshots(1, (k) => pinned.has(k));

  snapshots.put('key-existing', 'existing-content');
  pinned.add('key-existing');

  const inFlightKey = 'key-in-flight';
  const unprotect = snapshots.protectTemp([inFlightKey]);
  assert.equal(snapshots.tempPinnedSize, 1);

  try {
    snapshots.put(inFlightKey, 'in-flight-content');

    // Both entries must exist simultaneously without inFlightKey being evicted
    assert.equal(snapshots.get(inFlightKey), 'in-flight-content');
    assert.equal(snapshots.get('key-existing'), 'existing-content');
  } finally {
    unprotect();
  }

  // After unprotect, excess unpinned entry is pruned back to maxEntries (1)
  assert.equal(snapshots.tempPinnedSize, 0);
  assert.equal(snapshots.has('key-existing'), true);
  assert.equal(snapshots.has(inFlightKey), false);
  assert.equal(snapshots.size, 1);
});

test('OutputSnapshots.protectTemp supports nested concurrent protections with ref-counting and idempotent release', () => {
  const pinned = new Set<string>();
  const snapshots = new OutputSnapshots(1, (k) => pinned.has(k));
  snapshots.put('key-pinned', 'pinned-data');
  pinned.add('key-pinned');

  const sharedKey = 'key-shared';

  // Request 1 starts
  const unprotect1 = snapshots.protectTemp([sharedKey]);
  snapshots.put(sharedKey, 'shared-data');

  // Concurrent Request 2 starts on the same key
  const unprotect2 = snapshots.protectTemp([sharedKey]);

  // Request 1 finishes or fails
  unprotect1();

  // sharedKey must still be protected because Request 2 is active
  assert.equal(snapshots.get(sharedKey), 'shared-data');

  // Idempotent release check
  unprotect1();
  assert.equal(snapshots.get(sharedKey), 'shared-data');

  // Request 2 finishes: now pinned in editor
  pinned.add(sharedKey);
  unprotect2();

  assert.equal(snapshots.get(sharedKey), 'shared-data');
  assert.equal(snapshots.tempPinnedSize, 0);
});
