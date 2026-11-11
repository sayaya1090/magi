import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as v from 'valibot';
import { HostToWebviewMessageSchema } from '../core/webview_protocol';
import { planStrip, planStripView } from '../web/chat_adapter';

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

/** The least DOM the strip touches: text, hidden, attributes, children, click listeners. */
class FakeEl {
  textContent = '';
  hidden = false;
  className = '';
  children: FakeEl[] = [];
  private attrs: Record<string, string> = {};
  private on: Record<string, (() => void)[]> = {};
  constructor(public tag: string) {}
  setAttribute(k: string, v: string) { this.attrs[k] = String(v); }
  getAttribute(k: string) { return this.attrs[k] ?? null; }
  replaceChildren(...c: FakeEl[]) { this.children = c; }
  addEventListener(t: string, f: () => void) { (this.on[t] ??= []).push(f); }
  click() { (this.on.click ?? []).forEach((f) => f()); }
}

test('the strip is drawn at the top, a click shows and hides the steps, and no plan hides it', () => {
  const strip = new FakeEl('div'), head = new FakeEl('button'), list = new FakeEl('ol');
  strip.hidden = true; list.hidden = true;
  const doc = { createElement: (t: string) => new FakeEl(t) } as unknown as Document;
  const view = planStripView({ strip, head, list } as unknown as { strip: HTMLElement; head: HTMLElement; list: HTMLElement }, doc);
  view.draw([t('a', 'completed'), t('b', 'completed'), t('build it', 'in_progress'), t('d', 'pending')]);
  assert.equal(strip.hidden, false, 'a plan raises the strip');
  assert.match(head.textContent, /4\ub2e8\uacc4 \uc911 3\ub2e8\uacc4 \u00b7 build it/);
  assert.equal(list.hidden, true, 'the steps start folded');
  assert.equal(list.children.length, 4);
  assert.equal(list.children.filter((c) => c.className === 'done').length, 2);
  head.click();
  assert.equal(list.hidden, false, 'one click shows the steps');
  assert.equal(head.getAttribute('aria-expanded'), 'true');
  view.draw([t('a', 'completed'), t('b', 'completed'), t('build it', 'completed'), t('d', 'in_progress')]);
  assert.equal(list.hidden, false, 'a redraw keeps them open');
  assert.match(head.textContent, /4\ub2e8\uacc4 \uc911 4\ub2e8\uacc4 \u00b7 d/);
  head.click();
  assert.equal(list.hidden, true, 'a second click hides them');
  view.draw([]);
  assert.equal(strip.hidden, true, 'no plan, no strip');
});
