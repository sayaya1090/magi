import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { rows } from '../core/transcript';
import { Event } from '../core/protocol';
import { councilFold, councilLine, councilKey, decidedConvenes, hiddenByCouncil } from '../web/chat_adapter';

// A council is one line in the transcript, accepted or not; its convene row and three verdicts open
// only when that line is clicked. Asked 2026-11-15. Each convene folds and opens on its own.
test('a council stands as one closing line and opens per convene', () => {
  let seq = 0;
  const events: Event[] = [{ seq: ++seq, type: 'prompt.submitted', data: { messageId: 'm1', parts: [{ kind: 'text', text: 'done?' }] } }];
  const council = (decision: string) => {
    events.push({ seq: ++seq, type: 'council.convened', data: { round: 1, members: ['Melchior', 'Balthasar', 'Casper'], task: 't' } });
    for (const m of ['Melchior', 'Balthasar', 'Casper'])
      events.push({ seq: ++seq, type: 'council.verdict', data: { round: 1, member: m, decision, rationale: m + ' says' } });
    events.push({ seq: ++seq, type: 'council.decided', data: { round: 1, decision, tally: { done: decision === 'done' ? 3 : 0, continue: decision === 'done' ? 0 : 3 } } });
  };
  council('continue');
  council('done');
  const rs = rows(events);
  const seats = rs.filter(r => r.who === 'council');
  assert.equal(seats.length, 10, 'two councils of five rows; the rows themselves stay');
  const closings = seats.filter(r => r.closing);
  assert.equal(closings.length, 2, 'the decided row is each council\'s one line');
  assert.ok(seats.slice(0, 5).every(r => r.convene === closings[0].convene), 'one convene number across its five rows');
  assert.equal(closings[1].convene, (closings[0].convene ?? 0) + 1);

  const decided = decidedConvenes(rs);
  const folded = rs.filter(r => !hiddenByCouncil(r, new Set(), decided));
  assert.deepEqual(folded.filter(r => r.who === 'council'), closings, 'folded: one line per council');
  assert.ok(folded.some(r => r.who === 'user'), 'nothing else is hidden');

  const open = new Set([councilKey(closings[1])]);
  assert.equal(rs.filter(r => !hiddenByCouncil(r, open, decided) && r.who === 'council').length, 1 + 5, 'only the second opens');
});

// The webview's side: the adapter folds the rows it is handed and words the closing line.
test('the webview folds a council to its closing line and words it in the panel language', () => {
  const seat = { seq: 2, who: 'council' as const, text: 'no', member: 'Melchior', decision: 'continue', convene: 1 };
  const held = { seq: 3, who: 'council' as const, text: '0 done / 3 continue', decision: 'continue', convene: 1, closing: true };
  const user = { seq: 1, who: 'user' as const, text: 'done?' };
  assert.deepEqual(councilFold([user, seat, held], new Set()), [user, held]);
  assert.deepEqual(councilFold([user, seat, held], new Set([councilKey(held)])), [user, seat, held]);
  assert.deepEqual(councilLine(held, 'ko'), { text: '카운슬: 반대', accepted: false });
  assert.deepEqual(councilLine({ ...held, decision: 'done' }, 'en'), { text: 'Council: accepted', accepted: true });
});

// Only a decided council folds. One still deliberating, or cut before its outcome, has no closing
// line to open it from — folding it would hide its opinions with no way back (review, 2026-11-16).
test('a council without an outcome stays open: in progress, or cut before the next one', () => {
  let seq = 0;
  const ev: Event[] = [{ seq: ++seq, type: 'prompt.submitted', data: { messageId: 'm1', parts: [{ kind: 'text', text: 'done?' }] } }];
  const convene = () => ev.push({ seq: ++seq, type: 'council.convened', data: { round: 1, members: ['Melchior', 'Balthasar', 'Casper'], task: 't' } });
  const verdict = (m: string, d = 'continue') => ev.push({ seq: ++seq, type: 'council.verdict', data: { round: 1, member: m, decision: d, rationale: m + ' says' } });
  convene(); verdict('Melchior'); verdict('Balthasar');
  const live = rows(ev).filter(r => r.who === 'council');
  assert.equal(live.length, 3, 'convene and two verdicts');
  assert.deepEqual(councilFold(rows(ev), new Set()).filter(r => r.who === 'council'), live, 'still deliberating: nothing hidden');

  convene();
  for (const m of ['Melchior', 'Balthasar', 'Casper']) verdict(m, 'done');
  ev.push({ seq: ++seq, type: 'council.decided', data: { round: 1, decision: 'done', tally: { done: 3, continue: 0 } } });
  const shown = councilFold(rows(ev), new Set()).filter(r => r.who === 'council');
  assert.deepEqual(shown.slice(0, 3).map(r => r.seq), live.map(r => r.seq), 'the cut council keeps its rows');
  assert.equal(shown.length, 3 + 1, 'the decided one folds to its closing line');
  assert.ok(shown[3].closing);
});

test('a repeated declaration rejection stays independent of the previous council', () => {
  const rs = rows([
    { seq: 1, type: 'council.convened', data: { round: 1, task: 't' } },
    { seq: 2, type: 'council.decided', data: { round: 1, decision: 'continue' } },
    { seq: 3, type: 'council.decided', data: { round: 1, decision: 'continue', note: 'redeclared' } },
  ]);
  const shown = councilFold(rs, new Set());
  assert.deepEqual(shown.map(r => [r.seq, r.convene, r.closing]), [
    [2, 1, true], [3, undefined, undefined],
  ]);
  assert.equal(shown[1].text, 'redeclared');
});
