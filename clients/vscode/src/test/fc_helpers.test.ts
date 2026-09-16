import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { execSync } from 'node:child_process';
import * as path from 'node:path';
import * as fc from 'fast-check';
import {
  getFcParameters,
  buildReproductionCommand,
} from './support/fc_helpers';

test('fc_helpers: getFcParameters parses defaults and enforces endOnFailure: false for shrinking', () => {
  const origSeed = process.env.MAGI_FC_SEED;
  const origPath = process.env.MAGI_FC_PATH;
  try {
    delete process.env.MAGI_FC_SEED;
    delete process.env.MAGI_FC_PATH;

    const params = getFcParameters(100);
    assert.equal(params.numRuns, 100);
    assert.equal(params.seed, undefined);
    assert.equal(params.path, undefined);
    assert.equal(params.endOnFailure, false, 'endOnFailure must be false to allow counterexample shrinking');
  } finally {
    if (origSeed !== undefined) process.env.MAGI_FC_SEED = origSeed;
    if (origPath !== undefined) process.env.MAGI_FC_PATH = origPath;
  }
});

test('fc_helpers: getFcParameters validates MAGI_FC_SEED and rejects invalid values', () => {
  const origSeed = process.env.MAGI_FC_SEED;
  const origPath = process.env.MAGI_FC_PATH;
  try {
    delete process.env.MAGI_FC_PATH;

    // Valid integer seeds
    process.env.MAGI_FC_SEED = '42';
    assert.equal(getFcParameters().seed, 42);

    process.env.MAGI_FC_SEED = '-2147483648';
    assert.equal(getFcParameters().seed, -2147483648);

    process.env.MAGI_FC_SEED = '2147483647';
    assert.equal(getFcParameters().seed, 2147483647);

    process.env.MAGI_FC_SEED = '  100  ';
    assert.equal(getFcParameters().seed, 100);

    // Invalid seeds: partial parsing, decimals, scientific notation, out-of-bounds
    const invalidSeeds = [
      '12junk',
      'junk12',
      '1.5',
      '1e3',
      'NaN',
      'Infinity',
      '99999999999999999999',
      '2147483648',
      '-2147483649',
    ];

    for (const bad of invalidSeeds) {
      process.env.MAGI_FC_SEED = bad;
      assert.throws(
        () => getFcParameters(),
        /Invalid MAGI_FC_SEED/,
        `Expected rejection for seed "${bad}"`
      );
    }
  } finally {
    if (origSeed !== undefined) process.env.MAGI_FC_SEED = origSeed;
    if (origPath !== undefined) process.env.MAGI_FC_PATH = origPath;
  }
});

test('fc_helpers: getFcParameters validates MAGI_FC_PATH and enforces seed dependency', () => {
  const origSeed = process.env.MAGI_FC_SEED;
  const origPath = process.env.MAGI_FC_PATH;
  try {
    // 1. Path without seed must fail
    delete process.env.MAGI_FC_SEED;
    process.env.MAGI_FC_PATH = '0:1:2';
    assert.throws(
      () => getFcParameters(),
      /MAGI_FC_PATH environment variable requires MAGI_FC_SEED to be set/
    );

    // 2. Valid path with seed
    process.env.MAGI_FC_SEED = '42';
    process.env.MAGI_FC_PATH = '0';
    assert.equal(getFcParameters().path, '0');

    process.env.MAGI_FC_PATH = '0:1:0:0:2:0';
    assert.equal(getFcParameters().path, '0:1:0:0:2:0');

    // 3. Invalid paths
    const invalidPaths = [
      '0:junk',
      'junk',
      '0::1',
      '-1',
      ':0',
      '0:',
      '1.5',
    ];

    for (const bad of invalidPaths) {
      process.env.MAGI_FC_PATH = bad;
      assert.throws(
        () => getFcParameters(),
        /Invalid MAGI_FC_PATH/,
        `Expected rejection for path "${bad}"`
      );
    }
  } finally {
    if (origSeed !== undefined) process.env.MAGI_FC_SEED = origSeed;
    if (origPath !== undefined) process.env.MAGI_FC_PATH = origPath;
  }
});

test('fc_helpers: shrinking is active and exact counterexample path reproduces failure in 1 run', () => {
  // Property that fails for n >= 10 on a range of 0..100000 with seed 42
  const prop = fc.property(fc.integer({ min: 0, max: 100000 }), (n) => n < 10);

  const initialRun = fc.check(prop, { seed: 42, numRuns: 100, endOnFailure: false });
  assert.equal(initialRun.failed, true);
  assert.ok(initialRun.numShrinks > 0, `Expected numShrinks > 0, got ${initialRun.numShrinks}`);
  assert.deepEqual(initialRun.counterexample, [10], 'Counterexample must be shrunk to minimal bound 10');
  assert.ok(initialRun.counterexamplePath !== null);

  // Exact reproduction with seed and counterexamplePath
  const replayRun = fc.check(prop, {
    seed: initialRun.seed,
    path: initialRun.counterexamplePath!,
    numRuns: 1,
  });

  assert.equal(replayRun.failed, true);
  assert.equal(replayRun.numRuns, 1, 'Replay must fail on the very first test');
  assert.deepEqual(replayRun.counterexample, [10]);
});

test('fc_helpers: buildReproductionCommand safely escapes quotes, brackets, and regex metacharacters', () => {
  const propertyName = '§5.8.4 Property: Blank and whitespace-only text is rejected by submitReply with { ok: false, error: "empty" }';
  const cmd = buildReproductionCommand(propertyName, 12345, '0:1:2');

  assert.ok(cmd.startsWith('MAGI_FC_SEED=12345 MAGI_FC_PATH=\'0:1:2\''));
  assert.ok(cmd.includes('--test-name-pattern='));

  // Run the command via child_process from repository root without inheriting NODE_TEST_CONTEXT
  let repoRoot = __dirname;
  while (repoRoot !== path.dirname(repoRoot)) {
    if (require('node:fs').existsSync(path.join(repoRoot, 'clients', 'vscode', 'package.json'))) {
      break;
    }
    repoRoot = path.dirname(repoRoot);
  }
  const env = { ...process.env };
  delete env.NODE_TEST_CONTEXT;

  const stdout = execSync(cmd, { cwd: repoRoot, env, encoding: 'utf8' });
  assert.ok(stdout.includes('Blank and whitespace-only text is rejected'));
  assert.ok(stdout.includes('pass 1') || stdout.includes('pass 2'));
});
