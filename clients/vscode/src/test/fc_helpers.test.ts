import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { spawnSync, execSync } from 'node:child_process';
import * as path from 'node:path';
import * as fc from 'fast-check';
import {
  getFcParameters,
  getReproductionExecution,
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
    else delete process.env.MAGI_FC_SEED;
    if (origPath !== undefined) process.env.MAGI_FC_PATH = origPath;
    else delete process.env.MAGI_FC_PATH;
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
    else delete process.env.MAGI_FC_SEED;
    if (origPath !== undefined) process.env.MAGI_FC_PATH = origPath;
    else delete process.env.MAGI_FC_PATH;
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
    else delete process.env.MAGI_FC_SEED;
    if (origPath !== undefined) process.env.MAGI_FC_PATH = origPath;
    else delete process.env.MAGI_FC_PATH;
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

test('fc_helpers: getReproductionExecution executes safely via spawnSync without shell', () => {
  // Locate repo root safely
  let repoRoot = __dirname;
  while (repoRoot !== path.dirname(repoRoot)) {
    if (require('node:fs').existsSync(path.join(repoRoot, 'clients', 'vscode', 'package.json'))) {
      break;
    }
    repoRoot = path.dirname(repoRoot);
  }

  // 1. Discover the initial failure seed and shrunk counterexamplePath for the fixture property
  const failingProp = fc.property(fc.integer({ min: 0, max: 100000 }), (n) => n < 10);
  const initialRun = fc.check(failingProp, { seed: 42, numRuns: 100, endOnFailure: false });
  assert.equal(initialRun.failed, true);
  assert.ok(initialRun.counterexamplePath !== null);
  assert.deepEqual(initialRun.counterexample, [10]);

  // 2. Build cross-platform ReproductionExecution targeting the fixture
  const propertyName = 'fixture: [fail] "error: empty" {key: "val"} (special) 의도적 실패 속성';
  const fixtureRelPath = 'clients/vscode/out/test/fixtures/fc_repro_fixture.js';
  const execution = getReproductionExecution(
    propertyName,
    initialRun.seed,
    initialRun.counterexamplePath,
    fixtureRelPath
  );

  assert.equal(execution.executable, process.execPath);
  assert.ok(execution.args.includes('--test'));
  assert.ok(execution.args.some((a) => a.startsWith('--test-name-pattern=')));
  assert.equal(execution.env.MAGI_FC_SEED, String(initialRun.seed));
  assert.equal(execution.env.MAGI_FC_PATH, initialRun.counterexamplePath);

  // 3. Execute child process using spawnSync with shell: false (cross-platform, Windows & POSIX safe)
  const childEnv = { ...process.env, ...execution.env };
  delete childEnv.NODE_TEST_CONTEXT;

  const res = spawnSync(execution.executable, execution.args, {
    cwd: repoRoot,
    env: childEnv,
    shell: false,
    encoding: 'utf8',
  });

  // Verify child execution result:
  // - Exit code must be 1 because the intentional failing property fails
  assert.equal(res.status, 1, `Child process should exit with code 1, stderr:\n${res.stderr}`);
  const combinedOutput = `${res.stdout}\n${res.stderr}`;

  // - Exactly the target failing test was executed (and NOT the passing test in the same fixture)
  assert.ok(combinedOutput.includes('fixture: [fail] "error: empty"'));
  assert.ok(!combinedOutput.includes('fixture: [pass] "quoted"'));
  assert.ok(combinedOutput.includes('fail 1'));

  // - The reproduced failure output reports test 1 and counterexample [10]
  assert.ok(combinedOutput.includes('Failure Run     : test 1 of'));
  assert.ok(combinedOutput.includes('Counterexample  : [10]'));

  // 4. Verify buildReproductionCommand parity with execution.displayCommand
  assert.equal(
    buildReproductionCommand(propertyName, initialRun.seed, initialRun.counterexamplePath, fixtureRelPath),
    execution.displayCommand
  );

  // 5. If on POSIX platform, also test displayCommand execution via POSIX shell
  if (process.platform !== 'win32') {
    assert.ok(execution.displayCommand.startsWith(`MAGI_FC_SEED=${initialRun.seed}`));
    assert.ok(execution.displayCommand.includes(`MAGI_FC_PATH='${initialRun.counterexamplePath}'`));
    let posixFailed = false;
    try {
      execSync(execution.displayCommand, {
        cwd: repoRoot,
        env: childEnv,
        encoding: 'utf8',
      });
    } catch (err: any) {
      posixFailed = true;
      assert.equal(err.status, 1);
      const posixOutput = `${err.stdout || ''}\n${err.stderr || ''}`;
      assert.ok(posixOutput.includes('fixture: [fail] "error: empty"'));
      assert.ok(posixOutput.includes('fail 1'));
    }
    assert.equal(posixFailed, true, 'POSIX reproduction command must fail with status 1');
  }
});

