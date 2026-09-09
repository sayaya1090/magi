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
