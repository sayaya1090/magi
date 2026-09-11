import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import {
  Launches, Policy, Verdict, backoffMs,
  BACKOFF_JITTER, BACKOFF_STEPS_MS, BACKOFF_CAP_MS, DEFAULT_POLICY,
} from '../core/launches';
import { whyNoRelay } from '../core/binary';
import { REPLACE_BY_MS } from '../core/lifecycle';

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

/**
 * The numbers this client ACTUALLY runs with are the contract's.
 *
 * ⚠ **Every case above is run against `contract.policy`, not against what ships.** `new Launches()`
 * takes `DEFAULT_POLICY`, and nothing compared the two — so the cases proved that the ALGORITHM
 * obeys the contract while saying nothing about the numbers fed to it in production. Measured
 * 2026-09-11: setting `DEFAULT_POLICY` to `windowMs: 1, spawnsPerWindow: 99, failuresToBlock: 99,
 * graceMs: 0, stableMs: 1` left the whole suite green.
 *
 * That is the same shape the contract exists to end, one level over. The sibling has it too —
 * `LaunchesTest.policy()` builds its Launches from the contract and the Kotlin defaults
 * (`windowMs: Long = 60_000`, …) are equally unread.
 *
 * Field by field rather than a deep-equal, so a mismatch names which number drifted.
 */
test('the shipped defaults are the contract', () => {
  const p = contract.policy as Record<string, number>;
  for (const k of ['windowMs', 'spawnsPerWindow', 'failuresToBlock', 'graceMs', 'stableMs'] as const) {
    assert.equal(DEFAULT_POLICY[k], p[k],
      `DEFAULT_POLICY.${k} is ${DEFAULT_POLICY[k]}, the contract says ${p[k]} — a client running ` +
      'this obeys a rule its own tests never measured');
  }
  // And every key the type carries is checked: adding a field to Policy without adding it here
  // would leave the new number in the same unread position the five above were in.
  assert.deepEqual(Object.keys(DEFAULT_POLICY).sort(),
    ['failuresToBlock', 'graceMs', 'spawnsPerWindow', 'stableMs', 'windowMs'],
    'Policy grew a field — add it to the loop above, or it ships unmeasured');
});

/**
 * The backoff the client runs is the contract's ladder, not a copy that happens to look like it.
 *
 * `BACKOFF_STEPS_MS` is six long and the contract lists eight attempts — the last steps repeat, and
 * the function clamps past the end. So the ladder is compared where it is defined rather than by
 * length: every attempt the contract names must land in the band the contract names for it, which
 * the cases above already do — what was missing is that the STEPS themselves are the contract's.
 */
test('the backoff ladder is the contract', () => {
  const want = contract.policy.backoffMs as number[];
  for (let i = 0; i < want.length; i++) {
    const attempt = i + 1;
    const got = BACKOFF_STEPS_MS[Math.min(attempt, BACKOFF_STEPS_MS.length) - 1];
    assert.equal(got, want[i],
      `attempt ${attempt} steps to ${got}ms, the contract says ${want[i]}ms`);
  }
  assert.equal(BACKOFF_CAP_MS, contract.policy.backoffCapMs,
    'the cap this client applies is not the contract\'s');
});

/**
 * The policy being right is not the same as the window using it.
 *
 * `OwnedCompanion` had its own rule — a bare array of spawn timestamps — and the contract tests
 * above would stay green forever while the extension kept the old one. So the CALL SITE is pinned
 * too. `lifecycle.ts` imports `vscode`-free code but `OwnedCompanion` spawns processes, so this
 * reads the source the way the JetBrains side reads its Kotlin.
 */
test('the owned companion asks the policy instead of keeping its own rule', () => {
  const src = fs.readFileSync(path.join(REPO, 'clients/vscode/src/core/lifecycle.ts'), 'utf8');
  assert.ok(src.includes('new Launches('),
    'the window does not use the shared policy at all — the contract never reaches the screen');
  assert.ok(!/this\.attempts/.test(src),
    'the old timestamp array is still there — two rules in one file is how they drift');
  assert.ok(src.includes('.connected('),
    'nothing tells the policy the daemon is up, so the stable window is never counted and the '
    + 'budget never comes back');
  assert.ok(src.includes('.may(') && src.includes('.spawned('),
    'it spawns without asking, or spawns without saying so — the rolling window counts nothing');
  assert.ok(src.includes('.failed(') && src.includes('.lost('),
    'a failed start or a lost daemon is never reported, so a crashloop is never blocked');
  // ⚠ The numbers live in the contract. A literal here is a second copy that only one person edits.
  assert.ok(!/60_000|30_000\s*\)/.test(src.replace(/Date\.now\(\) \+ 30_000/g, '')),
    'a policy number is written in the window again — it belongs to the contract');
});

/**
 * Windows cannot dial the daemon's socket at all, so the extension spawns `magi ide-bridge
 * --raw-socket` as a relay — and a core too old for that flag exits 2, leaving every layer above to
 * report "nothing answered" about a daemon that is fine (docs/CLIENT_LIFECYCLE §2).
 *
 * ⚠ **Measured by calling the decision, not by reading the source for it.** The first guard here
 * looked for the words `features(` and `raw-socket-v1` in `bridge()`, and a mutation that kept
 * every one of those words while switching the check off (`… && false`) passed it. So the decision
 * is its own function and this calls it.
 */
test('a binary that cannot relay is refused before anything is spawned', () => {
  assert.equal(whyNoRelay(new Set(['raw-socket-v1']), 'magi.exe'), null,
    'a build that CAN relay is refused — Windows would never connect');
  const why = whyNoRelay(new Set(['owned-daemon-v1']), 'C:/old/magi.exe');
  assert.ok(why, 'a build with no relay is allowed through — the failure reads as a dead daemon');
  assert.ok(why!.includes('C:/old/magi.exe'), `the refusal does not say which binary: ${why}`);
  assert.ok(why!.includes('raw-socket-v1'), `the refusal does not say what is missing: ${why}`);
  // An empty answer is what every probe failure looks like — refused flag, bad JSON, timeout — and
  // all of them mean the same thing here.
  assert.ok(whyNoRelay(new Set(), 'magi'), 'an unanswerable probe is treated as "it can relay"');
});

/**
 * And the relay actually asks. The call site is source-read because `bridge()` spawns a process.
 */
test('the Windows relay calls that decision before spawning', () => {
  const src = fs.readFileSync(path.join(REPO, 'clients/vscode/src/core/daemon.ts'), 'utf8');
  const at = src.indexOf('static async bridge(');
  assert.ok(at > 0, 'the relay is gone — this rule is reading nothing');
  const body = src.slice(at, at + 2000);
  const spawnAt = body.indexOf('spawn(');
  assert.ok(spawnAt > 0, 'the relay no longer spawns — the anchor is wrong');
  const before = body.slice(0, spawnAt);
  assert.ok(before.includes('whyNoRelay('),
    'the relay spawns before asking whether this build has one');
  // ⚠ **Asking is not refusing.** Pinned as the whole statement: a mutation that kept the call and
  // dropped the throw (`void refusal;`) passed a guard that only looked for the call — the same
  // shape this tree has been caught by before.
  // ⚠ `[^)]*` cannot cross the inner `features(binary)` call — the first spelling of this rejected
  // the real code. `[\s\S]*?` up to the statement's own semicolon.
  assert.ok(/const refusal = whyNoRelay\([\s\S]*?\);\s*if \(refusal\) throw new Error\(refusal\);/.test(before),
    'the relay asks and then ignores the answer — an old binary is spawned anyway');
});

/**
 * The replacement window is the contract's `shutdownMs`, not a number that looks like it.
 *
 * `REPLACE_BY_MS` decides how long after asking for an update or a restart an ending still counts
 * as that replacement rather than as a crash. It is the same question the contract answers with
 * `shutdownMs` — what a daemon asked to end is given to end — and a copy of a number is exactly
 * what `clients/contract/lifecycle-policy.json` exists to stop.
 */
test('the replacement window is the contract’s shutdown budget', () => {
  assert.equal(REPLACE_BY_MS, contract.policy.shutdownMs,
    `REPLACE_BY_MS is ${REPLACE_BY_MS} and the contract's shutdownMs is ${contract.policy.shutdownMs} — ` +
    'a pardon that outlives the shutdown budget forgives crashes nobody asked for');
});
