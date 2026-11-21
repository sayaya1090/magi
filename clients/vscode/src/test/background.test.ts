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
  type FakeEl = { tagName: string; attrs: Record<string, string>; children: FakeEl[]; className: string; textContent: string; innerHTML: string };
  const doc = { createElement(tag: string) {
    return { tagName: tag.toUpperCase(), attrs: {} as Record<string, string>, children: [] as FakeEl[], className: '', textContent: '', innerHTML: '',
      setAttribute(k: string, v: string) { this.attrs[k] = v; }, getAttribute(k: string) { return k in this.attrs ? this.attrs[k] : null; },
      append(...c: FakeEl[]) { this.children.push(...c); }, replaceChildren(...c: FakeEl[]) { this.children = c; } };
  } } as unknown as Document;
  const strip = stagesStrip(doc, 'ko');
  const kids = () => (strip as unknown as FakeEl).children;
  const role = (r: string) => kids().filter((c) => c.attrs['data-role'] === r);
  updateStages(doc, strip, [{ id: 's1', label: 'list', kind: 'tool', state: 'running' }, { id: 's2', label: 'check', state: 'waiting' }]);
  assert.deepEqual(kids().map((c) => c.attrs['data-role']), ['start', 'arrow', 'step', 'arrow', 'step', 'arrow', 'end'], 'start, each step and end, joined by arrows');
  const first = role('step');
  assert.deepEqual(first.map((c) => c.attrs['data-stage-id']), ['s1', 's2'], 'every step from the first report');
  for (const n of [...role('start'), ...first, ...role('end')]) {
    assert.equal(n.children.length, 2, 'a node is a mark over its label');
    assert.match(n.children[0].innerHTML, /<svg/, 'the mark is drawn, not a character');
  }
  assert.equal(role('start')[0].children[1].textContent, '\uC2DC\uC791');
  assert.equal(role('start')[0].attrs['data-state'], 'running', 'start runs while the run does');
  assert.equal(role('end')[0].attrs['data-state'], 'waiting');
  updateStages(doc, strip, [{ id: 's1', label: 'list', kind: 'tool', state: 'done' }, { id: 's2', label: 'check', kind: 'subagent', state: 'running', done: 1, total: 3, running: 2 }]);
  const second = role('step');
  assert.ok(first.length === second.length && first.every((c, i) => c === second[i]), 'each step keeps its node');
  assert.equal(second[0].attrs['data-state'], 'done');
  assert.equal(second[1].attrs['data-state'], 'running');
  assert.match(second[1].children[1].textContent, /1\/3/);
  updateStages(doc, strip, [{ id: 's1', label: 'list', kind: 'tool', state: 'done' }, { id: 's2', label: 'check', kind: 'subagent', state: 'done', done: 3, total: 3 }]);
  assert.equal(role('start')[0].attrs['data-state'], 'done', 'start is done when the run is');
  assert.equal(role('end')[0].attrs['data-state'], 'done', 'end lights when every step is done');
  updateStages(doc, strip, [{ id: 's1', label: 'list', kind: 'tool', state: 'done' }, { id: 's2', label: 'check', state: 'failed' }]);
  assert.equal(role('end')[0].attrs['data-state'], 'failed', 'end shows a failure');

  const branched = [
    { id: 's1', label: 'fast', state: 'done', after: [] },
    { id: 'slow', label: 'slow', state: 'running', after: [] },
    { id: 'next', label: 'next', state: 'done', after: ['s1'] },
    { id: 'join', label: 'join', state: 'waiting', after: ['next', 'slow'] },
  ];
  updateStages(doc, strip, branched);
  const graph = kids()[0];
  assert.equal(graph.className, 'stage-graph');
  assert.equal(graph.children[0].innerHTML.match(/<path /g)?.length, 6);
  const graphNode = (id: string) => graph.children.find((child) => child.attrs['data-stage-id'] === id);
  assert.equal(graphNode('s1')?.attrs.style, 'grid-column:2;grid-row:1');
  assert.equal(graphNode('slow')?.attrs.style, 'grid-column:2;grid-row:2');
  assert.equal(graphNode('join')?.attrs['data-state'], 'waiting');
  assert.equal(graph.children.find((child) => child.attrs['data-role'] === 'end')?.attrs['data-state'], 'waiting');
  assert.equal(graphNode('s1'), first[0]);
  updateStages(doc, strip, branched.map((step) => ({ ...step, state: step.id === 'slow' ? 'failed' : step.id === 'join' ? 'skipped' : 'done' })));
  assert.equal(kids()[0], graph);
  assert.equal(graphNode('join')?.attrs['data-state'], 'skipped');
  assert.equal(graph.children.find((child) => child.attrs['data-role'] === 'end')?.attrs['data-state'], 'failed');
});

test('plain steps before a graph run in one line and the graph branches from the last of them', () => {
  type FakeEl = { tagName: string; attrs: Record<string, string>; children: FakeEl[]; className: string; textContent: string; innerHTML: string };
  const doc = { createElement(tag: string) {
    return { tagName: tag.toUpperCase(), attrs: {} as Record<string, string>, children: [] as FakeEl[], className: '', textContent: '', innerHTML: '',
      setAttribute(k: string, v: string) { this.attrs[k] = v; }, getAttribute(k: string) { return k in this.attrs ? this.attrs[k] : null; },
      removeAttribute(k: string) { delete this.attrs[k]; },
      append(...c: FakeEl[]) { this.children.push(...c); }, replaceChildren(...c: FakeEl[]) { this.children = c; } };
  } } as unknown as Document;
  const strip = stagesStrip(doc, 'ko');
  updateStages(doc, strip, [
    { id: 's1', label: 'prepare', state: 'done' },
    { id: 's2', label: 'branch', state: 'running' },
    { id: 'g1:a1', label: 'A1', state: 'done', after: [] },
    { id: 'g1:b1', label: 'B1', state: 'running', after: [] },
    { id: 'g1:a2', label: 'A2', state: 'done', after: ['g1:a1'] },
    { id: 'g1:b2', label: 'B2', state: 'waiting', after: ['g1:b1'] },
    { id: 'g1:join', label: 'join', state: 'waiting', after: ['g1:a2', 'g1:b2'] },
  ]);
  const graph = (strip as unknown as FakeEl).children[0];
  const at = (id: string) => graph.children.find((c) => c.attrs['data-stage-id'] === id)?.attrs.style;
  assert.equal(at('s1'), 'grid-column:2;grid-row:1');
  assert.equal(at('s2'), 'grid-column:3;grid-row:1', 'the second plain step follows the first');
  assert.equal(at('g1:a1'), 'grid-column:4;grid-row:1', 'a graph root branches from the last plain step');
  assert.equal(at('g1:b1'), 'grid-column:4;grid-row:2');
  assert.equal(at('g1:join'), 'grid-column:6;grid-row:1');
  assert.equal(graph.children.find((c) => c.attrs['data-role'] === 'end')?.attrs.style, 'grid-column:7;grid-row:1');
  assert.equal(graph.children[0].innerHTML.match(/<path /g)?.length, 9, 'nine edges, none from a plain step to the end');
});

test('a plain step after a graph follows the graph end, not the step before it', () => {
  type FakeEl = { tagName: string; attrs: Record<string, string>; children: FakeEl[]; className: string; textContent: string; innerHTML: string };
  const doc = { createElement(tag: string) {
    return { tagName: tag.toUpperCase(), attrs: {} as Record<string, string>, children: [] as FakeEl[], className: '', textContent: '', innerHTML: '',
      setAttribute(k: string, v: string) { this.attrs[k] = v; }, getAttribute(k: string) { return k in this.attrs ? this.attrs[k] : null; },
      removeAttribute(k: string) { delete this.attrs[k]; },
      append(...c: FakeEl[]) { this.children.push(...c); }, replaceChildren(...c: FakeEl[]) { this.children = c; } };
  } } as unknown as Document;
  const strip = stagesStrip(doc, 'ko');
  updateStages(doc, strip, [
    { id: 's1', label: 'prepare', state: 'done' },
    { id: 's2', label: 'branch', state: 'running' },
    { id: 'g1:a1', label: 'A1', state: 'done', after: [] },
    { id: 'g1:b1', label: 'B1', state: 'running', after: [] },
    { id: 'g1:a2', label: 'A2', state: 'done', after: ['g1:a1'] },
    { id: 'g1:b2', label: 'B2', state: 'waiting', after: ['g1:b1'] },
    { id: 'g1:join', label: 'join', state: 'waiting', after: ['g1:a2', 'g1:b2'] },
    { id: 's3', label: 'tidy', state: 'waiting' },
  ]);
  const graph = (strip as unknown as FakeEl).children[0];
  const at = (id: string) => graph.children.find((c) => c.attrs['data-stage-id'] === id)?.attrs.style;
  assert.equal(at('g1:a1'), 'grid-column:4;grid-row:1', 'a graph root branches from the step that ran it');
  assert.equal(at('g1:join'), 'grid-column:6;grid-row:1');
  assert.equal(at('s3'), 'grid-column:7;grid-row:1', 'the step after the graph follows its join');
  assert.equal(graph.children.find((c) => c.attrs['data-role'] === 'end')?.attrs.style, 'grid-column:8;grid-row:1');
  const paths = graph.children[0].innerHTML.match(/<path d="[^"]*"/g) ?? [];
  assert.equal(paths.length, 10, 'ten edges, the graph feeding the next step');
  assert.ok(paths.some((d) => d.startsWith('<path d="M810 16 ') && d.includes(' 917 16 m')), 'join leads to the next step');
  assert.ok(!paths.some((d) => d.startsWith('<path d="M378 16 ') && d.includes(' 917 16 m')), 'the step before the graph does not skip it');
});
