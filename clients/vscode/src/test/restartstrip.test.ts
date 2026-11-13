import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as v from 'valibot';
import { HostToWebviewMessageSchema, parseWebviewToHostMessage } from '../core/webview_protocol';
import { restartStrip, restartStripView } from '../web/chat_adapter';

/*
 * The restart strip: what the last stop (a restart or a cut) left in this conversation, between the
 * plan line and the conversation. Named "restart" here because this client already has a "recovery"
 * - the drafts it keeps for a send that never left (recovery_view.ts). The daemon doors keep their
 * names: recovery-state, resolve-call, recovery-retry.
 */

test('the restart message reaches the webview with only well-formed calls', () => {
  const m = v.parse(HostToWebviewMessageSchema, {
    kind: 'restart',
    state: { kind: 'needs_check', uncertain: [{ callId: 'c_w', tool: 'write', target: 'notes.md' }, { tool: 'bash' }, null] },
  });
  assert.deepEqual(m, {
    kind: 'restart',
    state: { kind: 'needs_check', reason: '', uncertain: [{ callId: 'c_w', tool: 'write', target: 'notes.md', verdict: '' }] },
  });
  const none = v.parse(HostToWebviewMessageSchema, { kind: 'restart', state: null });
  assert.deepEqual(none, { kind: 'restart', state: null });
});

test('a verdict the daemon does not know never leaves the webview', () => {
  assert.deepEqual(parseWebviewToHostMessage({ kind: 'settle', callId: 'c_w', verdict: 'not_ran' }),
    { kind: 'settle', callId: 'c_w', verdict: 'not_ran' });
  assert.deepEqual(parseWebviewToHostMessage({ kind: 'settle', retry: true }), { kind: 'settle', retry: true });
  assert.equal(parseWebviewToHostMessage({ kind: 'settle', callId: 'c_w', verdict: 'maybe' }), undefined);
});

test('nothing left, or a kind this client does not know, draws no strip', () => {
  assert.equal(restartStrip(null), null);
  assert.equal(restartStrip({ kind: 'none', reason: '', uncertain: [] }), null);
  assert.equal(restartStrip({ kind: 'later_kind', reason: '', uncertain: [] }), null);
  assert.equal(restartStrip({ kind: 'resumable', reason: '', uncertain: [] })?.kind, 'resumable');
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
const all = (e: FakeEl): FakeEl[] => [e, ...e.children.flatMap(all)];
const text = (e: FakeEl): string => all(e).map((x) => x.textContent).join(' ');

test('the strip names each call to check, offers four verdicts, and sends the one clicked', () => {
  const strip = new FakeEl('div'), head = new FakeEl('div'), list = new FakeEl('div');
  strip.hidden = true;
  const doc = { createElement: (t: string) => new FakeEl(t) } as unknown as Document;
  const sent: unknown[] = [];
  const view = restartStripView({ strip, head, list } as unknown as { strip: HTMLElement; head: HTMLElement; list: HTMLElement },
    doc, (m) => sent.push(m));

  view.draw({ kind: 'needs_check', reason: '', uncertain: [
    { callId: 'c_w', tool: 'write', target: 'notes.md', verdict: '' },
    { callId: 'c_bash', tool: 'bash', target: 'make deploy', verdict: 'deferred' },
  ] });
  assert.equal(strip.hidden, false, 'calls to check raise the strip');
  assert.equal(strip.getAttribute('data-kind'), 'needs_check');
  assert.ok(head.textContent.includes('2'), 'the head says how many calls: ' + head.textContent);
  const shown = text(list);
  for (const w of ['write', 'notes.md', 'bash', 'make deploy']) assert.ok(shown.includes(w), w + ' is not shown: ' + shown);
  const verdicts = all(list).filter((e) => e.getAttribute('data-verdict'));
  assert.deepEqual(verdicts.map((b) => b.getAttribute('data-call') + ':' + b.getAttribute('data-verdict')), [
    'c_w:ran', 'c_w:not_ran', 'c_w:rerun_ok', 'c_w:deferred',
    'c_bash:ran', 'c_bash:not_ran', 'c_bash:rerun_ok', 'c_bash:deferred',
  ]);
  verdicts[1].click();
  assert.deepEqual(sent, [{ kind: 'settle', callId: 'c_w', verdict: 'not_ran' }]);

  view.draw({ kind: 'resumable', reason: '', uncertain: [] });
  assert.equal(strip.hidden, false);
  assert.equal(strip.getAttribute('data-kind'), 'resumable');
  assert.match(head.textContent, /continue/);
  assert.equal(all(list).filter((e) => e.getAttribute('data-verdict')).length, 0, 'a cut turn asks for no verdicts');

  view.draw({ kind: 'recovery_failed', reason: 'disk full', uncertain: [] });
  assert.equal(strip.getAttribute('data-kind'), 'recovery_failed');
  assert.ok(text(list).includes('disk full'), 'the reason is the daemon\'s own words');
  const retry = all(list).filter((e) => e.getAttribute('data-retry') === 'true');
  assert.equal(retry.length, 1);
  retry[0].click();
  assert.deepEqual(sent[1], { kind: 'settle', retry: true });

  view.draw(null);
  assert.equal(strip.hidden, true, 'nothing left, no strip');
});
