import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { rows } from '../core/transcript';
import { Event } from '../core/protocol';

test('background launch stays busy until its own process exit; replay keeps terminal outcome', () => {
  const events: Event[] = [
    { seq: 1, type: 'part.appended', data: { part: { kind: 'tool-call', toolCall: { callId: 'job', name: 'bash' } } } },
    { seq: 2, type: 'part.appended', data: { part: { kind: 'tool-result', toolResult: { callId: 'job', background: true, content: 'started' } } } },
  ];
  assert.equal(rows(events)[0].background, true);
  assert.equal(rows(events)[0].ok, undefined);
  const unrelated = { seq: 3, type: 'tool.background', data: { callId: 'other', state: 'succeeded' } };
  assert.equal(rows([...events, unrelated])[0].background, true);
  for (const state of ['succeeded', 'failed', 'killed', 'unknown']) {
    const result = rows([...events, { seq: 4, type: 'tool.background', data: { callId: 'job', state } }])[0];
    assert.equal(result.background, undefined);
    assert.equal(result.ok, state === 'unknown' ? undefined : state === 'succeeded');
  }
});
