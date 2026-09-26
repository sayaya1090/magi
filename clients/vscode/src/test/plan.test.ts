import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { planLines, todos } from '../core/transcript';
import { Event } from '../core/protocol';

const changed = (seq: number, list: unknown[]): Event =>
  ({ seq, type: 'todos.changed', data: { todos: list } });

/**
 * ★ The panel is called Plan and had no plan in it.
 *
 * The UI design promises "todo and its state"; the panel drew now/context/jobs/scheduled/fleet — five
 * dials and no list. The JetBrains client reads `todos.changed` for exactly this, and this one ignored
 * the event: nothing failed, nothing appeared. The same class as the rest, one layer up — a fact the
 * daemon sends that no reader picks up.
 */
test('the plan is read off the conversation stream', () => {
  const list = todos([changed(1, [
    { content: 'read the parser', status: 'completed' },
    { content: 'fix the anchor', status: 'in_progress' },
    { content: 'write the test', status: 'pending' },
  ])]);
  assert.equal(list.length, 3);
  assert.deepEqual(list[1], { content: 'fix the anchor', status: 'in_progress' });
});

/**
 * ⚠ The LAST event wins. Every `todos.changed` carries the whole list, so a reader that accumulated
 * would be wrong from the second event onwards — it would show items the agent had dropped.
 */
test('a later plan replaces the earlier one rather than adding to it', () => {
  const list = todos([
    changed(1, [{ content: 'a', status: 'pending' }, { content: 'b', status: 'pending' }]),
    changed(2, [{ content: 'a', status: 'completed' }]),
  ]);
  assert.equal(list.length, 1);
  assert.equal(list[0].status, 'completed');
});

/** An item with no words is not an item. A blank row in a plan reads as the plan being broken. */
test('items with no content are dropped', () => {
  const list = todos([changed(1, [{ status: 'pending' }, { content: '', status: 'x' }, { content: 'real' }])]);
  assert.deepEqual(list, [{ content: 'real', status: '' }]);
});

/** No such event, no plan — and that is not an empty plan, it is nothing to draw. */
test('a stream with no plan events yields none', () => {
  assert.deepEqual(todos([{ seq: 1, type: 'part.appended', data: {} }]), []);
  assert.deepEqual(todos([]), []);
});

/** The three states a person reads at a glance. An unknown state draws as not-done rather than blank. */
test('each state has its own mark', () => {
  const drawn = planLines([
    { content: 'done', status: 'completed' },
    { content: 'now', status: 'in_progress' },
    { content: 'later', status: 'pending' },
    { content: 'odd', status: 'something-else' },
  ]).split('\n');
  assert.match(drawn[0], /^✓ done/);
  assert.match(drawn[1], /^◐ now/);
  assert.match(drawn[2], /^☐ later/);
  assert.match(drawn[3], /^☐ odd/, 'an unrecognised state must still draw a box, not a blank');
});

/** And the panel actually has the section. The defect was that it did not. */
test('the panel draws a plan section', () => {
  const body = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'plan.ts'), 'utf8');
  assert.match(body, /section\('plan',/, 'the plan panel has no plan section');
});

/**
 * The editor's own tools: three outcomes, and a person can tell them apart.
 *
 * Measured. `offer()` can end three ways and, until this guard, all three looked identical from a
 * chair: the daemon does not advertise `tool-servers` (it returned in silence), no loopback port
 * was free, or the attach was refused because another window on this workspace is already the hand.
 * The first said nothing anywhere; the other two reached `console.warn` — the extension-host log,
 * which nobody opens. So the symptom of every one was "the agent does not use my editor" with
 * nowhere to find out why.
 *
 * The JetBrains client tells these apart and says why in its own words: NOT BEING the hand and
 * BEING BROKEN are different events. This pins that the sentence exists, that the three branches
 * really are three, and that it reaches a screen.
 */
test('the editor hand says which of the three things happened', () => {
  const hand = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'hand.ts'), 'utf8');
  const offer = hand.slice(hand.indexOf('async offer('), hand.indexOf('handWhy()'));
  assert.ok(offer.length > 200, 'offer() was not found — this guard is reading nothing');

  // Each branch writes its own sentence. One shared sentence would be the defect back with a note.
  const said = [...offer.matchAll(/this\.why\s*=/g)].length;
  assert.ok(said >= 3, `only ${said} of the three outcomes says anything — the other(s) are silent`);

  // The capless return is the one that used to say nothing at all, so it is pinned by name.
  const capless = offer.slice(0, offer.indexOf('Hand.start'));
  assert.ok(/tool-servers/.test(capless) && /this\.why\s*=/.test(capless),
    'a daemon without tool-servers still returns in silence — that was outcome one');

  // And the port failure is its own outcome, not folded into "refused".
  assert.ok(/catch\s*\(/.test(offer), 'a port that cannot be opened is not caught, so it has no sentence');

  // The sentence has to reach a screen. Reading it into a field nobody draws is the same defect.
  const ext = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'extension.ts'), 'utf8');
  assert.ok(/showHand\(/.test(ext), 'nothing hands the outcome to the panel');
  const plan = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'plan.ts'), 'utf8');
  assert.ok(/showHand\(/.test(plan), 'the panel has no way in for it');
  assert.ok(/section\(["'][^"']*editor[^"']*["']/.test(plan), 'the panel never draws a section for it');

  // The attach answer carries WHAT attached. Two things have to hold and the first is worthless
  // alone: the wire type declares `tools` (undeclared, it cannot be read at all), AND the code
  // reads it. Declaring without reading passed the first cut of this guard — a mutation that
  // replaced the sentence with a flat "attached" survived it, which is the same blind spot as
  // "the answer is bound, so it must be looked at". Bound is not read; declared is not read.
  const proto = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'protocol.ts'), 'utf8');
  assert.ok(/^\s{2}tools\?:/m.test(proto), 'Response does not declare `tools` — mcp-attach fills it');
  assert.ok(/\br\??\.?\??\.tools\b/.test(offer) || /\.tools\b/.test(offer),
    'the attach answer names the tools it attached and nothing reads them — the screen can only say "attached"');
});
