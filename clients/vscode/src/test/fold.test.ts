import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as activity from '../core/activity';
import { StateMessageSchema } from '../core/webview_protocol';
import { foldBar } from '../web/chat_adapter';
import * as v from 'valibot';

/**
 * A compaction is one long model call between steps, and nothing else is drawn while it runs. The
 * daemon now sends the counted figures beside the words, so the panel can draw a bar. Counted only:
 * with one summary part nobody knows how long it will run, and a bar that shows 60% then is a lie.
 */
test('the count behind a progress note rides with it, and garbage is not turned into a count', () => {
  const a = activity.of({ ok: true, doing: 'compacting', progress: { done: 1, total: 3, chars: 840, elapsedMs: 12500 } } as never);
  assert.deepEqual(a.progress, { done: 1, total: 3, chars: 840, elapsedMs: 12500 });
  // Words only: no count is invented.
  assert.equal(activity.of({ ok: true, doing: 'check 6' } as never).progress, undefined);
  // A count without words is the count of something that ended.
  assert.equal(activity.of({ ok: true, progress: { done: 1, total: 3 } } as never).progress, undefined);
  assert.equal(activity.progressOf('nope'), undefined);
  assert.deepEqual(activity.progressOf({ done: 'x', total: 2 }), { done: 0, total: 2, chars: 0, elapsedMs: 0 });
});

test('the webview state message keeps the count, and drops it when there are no words', () => {
  const ok = v.parse(StateMessageSchema, { kind: 'state', state: { state: 'working', doing: 'compacting', progress: { done: 0, total: 1, chars: 10, elapsedMs: 2000 } }, note: { text: '' } });
  assert.deepEqual(ok.state.progress, { done: 0, total: 1, chars: 10, elapsedMs: 2000 });
  const bare = v.parse(StateMessageSchema, { kind: 'state', state: { state: 'working', progress: { done: 0, total: 1 } }, note: { text: '' } });
  assert.equal(bare.state.progress, undefined);
});

test('the bar fills by finished parts, and never invents a share for a single part', () => {
  const many = foldBar({ done: 1, total: 3, chars: 840, elapsedMs: 12500 });
  assert.ok(many);
  assert.ok(Math.abs((many.fraction ?? -1) - 1 / 3) < 1e-9);
  assert.match(many.text, /2\/3/);
  assert.match(many.text, /840/);
  assert.match(many.text, /12/);
  const one = foldBar({ done: 0, total: 1, chars: 120, elapsedMs: 3000 });
  assert.equal(one?.fraction, null);
  assert.match(one?.text ?? '', /1\/1/);
  assert.equal(foldBar(null), null);
});
