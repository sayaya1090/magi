import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as activity from '../core/activity';

/**
 * Between tools the daemon's `status` says nothing, and the bar fell to "attached" in the middle of a
 * turn (measured on the JetBrains bar, 2026-09-27: two minutes of a running turn read as resting).
 * The open turn in the transcript lifts only that silence — never what the daemon did say.
 */
test('an open turn with nothing running reads as working, and never overrides what the daemon said', () => {
  const attached = activity.of({ ok: true } as never);
  assert.equal(attached.state, activity.State.Attached);
  assert.equal(activity.withTurn(attached, true).state, activity.State.Working);
  // With no transcript in view there is no fact to add.
  assert.equal(activity.withTurn(attached, false).state, activity.State.Attached);
  // Waiting beats it: a person is needed, and "working" would hide that.
  const waiting = activity.of({ ok: true, waiting: { what: 'bash' } } as never);
  assert.equal(activity.withTurn(waiting, true).state, activity.State.Waiting);
  // A named tool keeps its name.
  const doing = activity.of({ ok: true, doing: 'go test ./...' } as never);
  assert.deepEqual(activity.withTurn(doing, true), doing);
  // Not reachable is not working, whatever the transcript last showed.
  assert.equal(activity.withTurn(activity.cannotSay(), true).state, activity.State.Unknown);
});
