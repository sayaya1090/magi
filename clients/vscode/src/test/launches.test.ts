import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { Launches, Policy, Verdict, backoffMs, BACKOFF_JITTER } from '../core/launches';

const REPO = path.join(__dirname, '..', '..', '..', '..');
const contract = JSON.parse(
  fs.readFileSync(path.join(REPO, 'clients/contract/lifecycle-policy.json'), 'utf8'),
);

/**
 * ⚠ **The contract is the file, not this test.**
 *
 * Two editors have to follow one rule, and writing the cases separately in each client's tests gives
 * "both green, different rules" — which is exactly the state this policy was in on 2026-09-11. So
 * the cases live in `clients/contract/lifecycle-policy.json` and both sides read THAT.
 * docs/CLIENT_LIFECYCLE §7: the shared fixture carries behaviour, not any implementation's source
 * text.
 *
 * The clock is virtual. One of these budgets is a minute long, so a test that really waits cannot
 * be written — and one that does not measure is the same as no test.
 */
test('every case in the shared contract passes', () => {
  const p: Policy = contract.policy;
  assert.ok(contract.cases.length >= 10,
    `only ${contract.cases.length} cases — the contract file is missing or empty`);
  for (const c of contract.cases) {
    const l = new Launches(p);
    for (const s of c.steps) {
      if ('ask' in s) {
        const got: Verdict = l.may(s.ask, s.manual === true);
        assert.equal(got, s.want, `«${c.name}» at ${s.ask}ms`);
      } else if ('spawned' in s) l.spawned(s.spawned);
      else if ('ready' in s) l.ready(s.ready);
      else if ('connected' in s) l.connected(s.connected);
      else if ('stable' in s) l.stable(s.stable);
      else if ('lost' in s) l.lost(s.lost);
      else if ('failed' in s) l.failed(s.failed);
      else if ('replaced' in s) l.replaced(s.replaced);
      else if ('replaceFailed' in s) l.replaceFailed(s.replaceFailed);
      else if ('userStopped' in s) l.userStopped(s.userStopped);
      else assert.fail(`«${c.name}» has a step nobody handles: ${JSON.stringify(s)}`);
    }
  }
});

/**
 * The backoff is the contract's too. **The edges are fed directly** — a test that rolls random
 * numbers and says "about right" cannot see that ±20% on the 30s step reaches 36s.
 */
test('the backoff stays inside the contract, and never past the cap', () => {
  const steps = contract.backoff.steps;
  assert.ok(steps.length >= 6, `only ${steps.length} backoff cases`);
  for (const s of steps) {
    assert.equal(backoffMs(s.attempt, 0), s.min, `attempt ${s.attempt}, low edge`);
    assert.equal(backoffMs(s.attempt, 1), s.max, `attempt ${s.attempt}, high edge`);
    assert.ok(backoffMs(s.attempt, 1) <= contract.policy.backoffCapMs,
      `attempt ${s.attempt} goes past the ${contract.policy.backoffCapMs}ms cap`);
  }
  assert.equal(BACKOFF_JITTER, contract.policy.jitterFraction,
    'the jitter fraction here disagrees with the contract');
});

/**
 * And the two implementations are held to the same file — which is only true while both actually
 * read it. A sibling that stopped reading it would pass its own tests forever.
 */
test('the JetBrains client reads the same contract file', () => {
  const kt = fs.readFileSync(
    path.join(REPO, 'clients/jetbrains/plugin/core/src/test/kotlin/dev/sayaya/magi/ide/usecase/LaunchesTest.kt'),
    'utf8');
  assert.ok(kt.includes('clients/contract/lifecycle-policy.json'),
    'the JetBrains test no longer reads the shared contract — the two rules can drift again');
});
