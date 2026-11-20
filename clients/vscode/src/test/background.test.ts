import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { rows } from '../core/transcript';
import { stagesStrip, updateStages } from '../web/chat_adapter';
import { Event } from '../core/protocol';

test('background launch stays busy until its own process exit; replay keeps terminal outcome', () => {
  const events: Event[] = [
    { seq: 1, type: 'part.appended', data: { part: { kind: 'tool-call', toolCall: { callId: 'job', name: 'bash' } } } },
    { seq: 2, type: 'part.appended', data: { part: { kind: 'tool-result', toolResult: { callId: 'job', background: true, advisory: true, isError: true, content: 'started' } } } },
  ];
  assert.equal(rows(events)[0].background, true);
  assert.equal(rows(events)[0].ok, undefined);
  const unrelated = { seq: 3, type: 'tool.background', data: { callId: 'other', state: 'succeeded' } };
  assert.equal(rows([...events, unrelated])[0].background, true);
  for (const state of ['succeeded', 'failed', 'killed', 'unknown']) {
    const result = rows([...events, { seq: 4, type: 'tool.background', data: { callId: 'job', state } }])[0];
    assert.equal(result.background, undefined);
    if (state !== 'succeeded') assert.equal(result.note, undefined);
    assert.equal(result.ok, state === 'unknown' ? undefined : state === 'succeeded');
  }
});

test('the steps a tool reports ride its own row, and a strip keeps a node per step, changing only what changed', () => {
  const events = [
    { seq: 1, type: 'part.appended', data: { part: { kind: 'tool-call', toolCall: { callId: 'run1', name: 'long_job' } } } },
    { seq: 2, type: 'part.appended', data: { part: { kind: 'tool-call', toolCall: { callId: 'other', name: 'read' } } } },
    { seq: 0, type: 'tool.progress', data: { callId: 'run1', name: 'long_job', text: 'a', stages: [{ id: 's1', label: 'a', state: 'running' }] } },
    { seq: 0, type: 'tool.progress', data: { callId: 'run1', name: 'long_job', text: 'b', stages: [{ id: 's1', label: 'a', state: 'done' }, { id: 's2', label: 'b', state: 'running' }] } },
    { seq: 0, type: 'tool.progress', data: { callId: 'other', name: 'read', text: 'no steps here' } },
  ] as unknown as Event[];
  const rs = rows(events);
  assert.equal(rs.length, 2, 'steps are not a row of their own');
  assert.deepEqual(rs.find((r) => r.callId === 'run1')?.stages?.map((s) => `${s.id}:${s.state}`), ['s1:done', 's2:running']);
  assert.equal(rs.find((r) => r.callId === 'other')?.stages, undefined);
  type FakeEl = { tagName: string; attrs: Record<string, string>; children: FakeEl[]; className: string; textContent: string };
  const doc = { createElement(tag: string) {
    return { tagName: tag.toUpperCase(), attrs: {} as Record<string, string>, children: [] as FakeEl[], className: '', textContent: '',
      setAttribute(k: string, v: string) { this.attrs[k] = v; }, getAttribute(k: string) { return k in this.attrs ? this.attrs[k] : null; },
      replaceChildren(...c: FakeEl[]) { this.children = c; } };
  } } as unknown as Document;
  const strip = stagesStrip(doc, 'ko');
  updateStages(doc, strip, [{ id: 's1', label: 'list', kind: 'tool', state: 'running' }, { id: 's2', label: 'check', state: 'waiting' }]);
  const first = [...(strip as unknown as FakeEl).children];
  assert.deepEqual(first.map((c) => c.attrs['data-stage-id']).filter(Boolean), ['s1', 's2'], 'every step from the first report');
  assert.equal(first[0].textContent, '\uC2DC\uC791');
  updateStages(doc, strip, [{ id: 's1', label: 'list', kind: 'tool', state: 'done' }, { id: 's2', label: 'check', kind: 'subagent', state: 'running', done: 1, total: 3, running: 2 }]);
  const second = (strip as unknown as FakeEl).children;
  assert.ok(first.length === second.length && first.every((c, i) => c === second[i]), 'each step keeps its node');
  assert.equal(second[1].attrs['data-state'], 'done');
  assert.equal(second[2].attrs['data-state'], 'running');
  assert.match(second[2].textContent, /1\/3/);
});
