import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as v from 'valibot';
import { HostToWebviewMessageSchema } from '../core/webview_protocol';
import { planStrip } from '../web/chat_adapter';

const t = (content: string, status: string) => ({ content, status });

/**
 * The plan used to live only in the side panel, so while reading the conversation nobody could see
 * how far along it was. The strip at the top says which step of how many, and what it is on.
 */
test('the plan strip names the step in progress, or the next one, or says it is all done', () => {
  const now = planStrip([t('a', 'completed'), t('b', 'completed'), t('c', 'in_progress'), t('d', 'pending')]);
  assert.deepEqual([now?.step, now?.total, now?.done, now?.current, now?.finished], [3, 4, 2, 'c', false]);
  assert.match(now?.text ?? '', /4\ub2e8\uacc4 \uc911 3\ub2e8\uacc4/);
  const next = planStrip([t('a', 'completed'), t('b', 'pending')]);
  assert.deepEqual([next?.step, next?.current], [2, 'b']);
  const all = planStrip([t('a', 'completed'), t('b', 'completed')]);
  assert.equal(all?.finished, true);
  assert.equal(planStrip([]), null);
  assert.equal(planStrip(null), null);
});

test('the plan message reaches the webview with only well-formed steps', () => {
  const m = v.parse(HostToWebviewMessageSchema, { kind: 'plan', todos: [t('a', 'in_progress'), { status: 'x' }, null] });
  assert.deepEqual(m, { kind: 'plan', todos: [t('a', 'in_progress')] });
});
