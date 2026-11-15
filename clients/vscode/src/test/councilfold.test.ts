import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { rows } from '../core/transcript';
import { Event } from '../core/protocol';
import { councilFold, councilLine, councilKey, hiddenByCouncil } from '../web/chat_adapter';

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

  const folded = rs.filter(r => !hiddenByCouncil(r, new Set()));
  assert.deepEqual(folded.filter(r => r.who === 'council'), closings, 'folded: one line per council');
  assert.ok(folded.some(r => r.who === 'user'), 'nothing else is hidden');

  const open = new Set([councilKey(closings[1])]);
  assert.equal(rs.filter(r => !hiddenByCouncil(r, open) && r.who === 'council').length, 1 + 5, 'only the second opens');
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
