import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { rows } from '../core/transcript';
import { usage } from '../core/panel';
import { Event } from '../core/protocol';

let seq = 0;
const delta = (messageId: string, kind: string, text: string): Event =>
  ({ seq: seq++, type: 'part.delta', data: { messageId, kind, text } });
const appended = (messageId: string, kind: string, text: string): Event =>
  ({ seq: seq++, type: 'part.appended', data: { messageId, role: 'assistant', part: { kind, text } } });
const prompt = (text: string): Event =>
  ({ seq: seq++, type: 'prompt.submitted', data: { parts: [{ text }] } });
const finished = (): Event => ({ seq: seq++, type: 'turn.finished', data: {} });

/**
 * ★ The conversation has to move WHILE the model answers.
 *
 * `part.delta` carries each chunk; the `part.appended` fact is only written once the stream finishes.
 * This client ignored the deltas, so a long answer on a local model was a frozen panel with a status
 * bar that said "working" — nothing failed, nothing appeared. The JetBrains client has streamed all
 * along.
 */
test('a reply appears while it is still arriving', () => {
  const got = rows([delta('m1', 'text', 'Look'), delta('m1', 'text', 'ing at')]);
  assert.equal(got.length, 1);
  assert.equal(got[0].who, 'agent');
  assert.equal(got[0].text, 'Looking at');
  assert.equal(got[0].draft, true);
});

/**
 * ⚠ The fact REPLACES its draft. The appended part carries the whole text, so keeping both shows
 * the answer twice — and the second copy is the one a person would quote.
 */
test('the finished part replaces the draft rather than adding to it', () => {
  const got = rows([
    delta('m1', 'text', 'Look'), delta('m1', 'text', 'ing at it'),
    appended('m1', 'text', 'Looking at it'),
  ]);
  assert.equal(got.length, 1, `the answer is drawn twice: ${JSON.stringify(got)}`);
  assert.equal(got[0].text, 'Looking at it');
  assert.ok(!got[0].draft, 'the row is still marked a draft after the fact landed');
});

/** One message streams reasoning and text as two drafts; writing one must not drop the other. */
test('reasoning and text are separate drafts of one message', () => {
  const got = rows([
    delta('m1', 'reasoning', 'thinking…'), delta('m1', 'text', 'the answer'),
    appended('m1', 'text', 'the answer'),
  ]);
  assert.equal(got.length, 2);
  assert.equal(got[0].who, 'thinking');
  assert.equal(got[0].draft, true, 'the reasoning draft was dropped by the text fact');
  assert.equal(got[1].text, 'the answer');
});

/** Reasoning is folded shut, streamed or not — the same rule the finished part follows. */
test('a streamed reasoning draft is folded', () => {
  assert.equal(rows([delta('m1', 'reasoning', 'hm')])[0].folded, true);
});

/**
 * ★ Orphan drafts are swept when the turn ends.
 *
 * The core streams chunks and then does not write the fact on several paths — a reply the spin guard
 * discarded, a tool call that arrived as text, an interrupt or a provider error. Left standing, a
 * half-answer sits on the screen of whichever window was attached and nowhere else.
 */
test('a half-streamed answer does not outlive its turn', () => {
  const got = rows([prompt('do it'), delta('m1', 'text', 'I will start by'), finished()]);
  assert.deepEqual(got.map((r) => r.who), ['user'], `an orphan draft survived: ${JSON.stringify(got)}`);
});

/** A streamed reply answers the prompt above it — the pending bar must come down as text arrives. */
test('the pending bar comes down when the first chunk lands', () => {
  const got = rows([prompt('do it'), delta('m1', 'text', 'starting')]);
  assert.equal(got[0].who, 'user');
  assert.ok(!got[0].pending, 'the prompt still shows as unanswered while the reply is streaming');
});

/** An empty chunk is not a row, and a kind this screen does not draw is not one either. */
test('empty and unknown chunks make no row', () => {
  assert.deepEqual(rows([delta('m1', 'text', '')]), []);
  assert.deepEqual(rows([delta('m1', 'tool-call', 'x')]), []);
});

const usageEv = (tokens: number, window: number, percent?: number): Event =>
  ({ seq: seq++, type: 'context.usage', data: { tokens, window, percent } });
const folded = (before: number, after: number): Event =>
  ({ seq: seq++, type: 'compaction', data: { tokensBefore: before, tokensAfter: after, summary: 's' } });

/**
 * ★ A fold leaves a gap, and the gap needs a reason.
 *
 * Without this row the transcript simply stops earlier than a person remembers: the fold replaces
 * everything up to a point with a summary, and scrolling back finds nothing that explains it.
 */
test('a fold says so where it happened', () => {
  const got = rows([folded(40000, 12000)]);
  assert.equal(got.length, 1);
  assert.equal(got[0].who, 'system');
  assert.match(got[0].text, /40000→12000/);
  assert.match(got[0].text, /−28000, −70%/);
});

/**
 * ⚠ A summary can come out LARGER than what it replaced, and that is the one outcome worth seeing.
 * Rendering it as "−0, −0%" — which clamping would — hides it.
 */
test('a fold that made things bigger says that', () => {
  assert.match(rows([folded(1000, 1500)])[0].text, /\+500, the summary is LARGER/);
});

test('a fold of nothing does not divide by zero', () => {
  assert.match(rows([folded(0, 0)])[0].text, /−0, −0%/);
});

/**
 * ★ The window meter comes off the STREAM, because the door for it is a capability a daemon may not
 * have — measured on a live one advertising nine capabilities and not that one.
 */
test('how full the window is comes off the stream', () => {
  assert.match(usage([usageEv(35103, 200000)]), /35103 \/ 200000 \(18%\)/);
});

/** The core's own percent wins when it sends one — it knows what it counted. */
test('the percent the core sent is the percent shown', () => {
  assert.match(usage([usageEv(1, 200000, 42)]), /\(42%\)/);
});

/** The LAST reading wins: this is a meter, not a log. */
test('a later reading replaces an earlier one', () => {
  assert.match(usage([usageEv(10, 100), usageEv(80, 100)]), /80 \/ 100 \(80%\)/);
});

/**
 * ⚠ It is transient, so a reattached window has none until a turn runs — and that unknown must not
 * draw as 0%. An empty window and an unmeasured one look nothing alike to somebody deciding whether
 * to fold.
 */
test('no reading says nothing rather than zero percent', () => {
  assert.equal(usage([]), '');
  assert.equal(usage([{ seq: 1, type: 'context.usage', data: { tokens: 5 } }]), '');
});

const submitted = (msgId: string, text: string): Event =>
  ({ seq: seq++, type: 'prompt.submitted', data: { messageId: msgId, parts: [{ text }] } });
const abandoned = (msgId: string): Event =>
  ({ seq: seq++, type: 'prompt.abandoned', data: { msgId } });

/**
 * ★ A cancelled prompt kept claiming it was waiting for an answer.
 *
 * `pending` is cleared by an assistant part or by `turn.finished`, and an interrupted prompt gets
 * neither — so the bar stood for the life of the window about a request that had been stopped. A
 * standing claim that has gone false is the shape this tree calls "sentences that age".
 */
test('a cancelled prompt stops claiming it is waiting', () => {
  const got = rows([submitted('m1', 'do it'), abandoned('m1')]);
  assert.equal(got.length, 1);
  assert.ok(!got[0].pending, 'the row still says it is waiting for an answer');
  assert.equal(got[0].abandoned, true, 'the row does not say it was cancelled');
});

/**
 * ⚠ The two events spell the same id differently: `messageId` on the prompt, `msgId` on the
 * abandonment. Reading either one for the other finds nothing and marks nothing — silently.
 */
test('the abandonment finds its prompt across the two spellings', () => {
  const proto = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'event', 'payload.go'), 'utf8');
  assert.match(proto, /MessageID string\s+`json:"messageId"/, 'the prompt no longer carries messageId');
  assert.match(proto, /MsgID string\s+`json:"msgId"/, 'the abandonment no longer carries msgId');
});

/** Only the named prompt. Marking every row would cancel work that is still running. */
test('an abandonment marks only its own prompt', () => {
  const got = rows([submitted('m1', 'first'), submitted('m2', 'second'), abandoned('m1')]);
  assert.equal(got[0].abandoned, true);
  assert.ok(!got[1].abandoned, 'the other prompt was marked too');
  assert.equal(got[1].pending, true, 'the other prompt stopped waiting');
});

/** An abandonment for a prompt this window never saw marks nothing rather than guessing. */
test('an abandonment with no matching prompt is ignored', () => {
  const got = rows([submitted('m1', 'first'), abandoned('nope')]);
  assert.ok(!got[0].abandoned);
});
