import { test } from 'node:test';
import { ChildProcess } from 'child_process';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { OwnedCompanion } from '../core/lifecycle';
import { Daemon } from '../core/daemon';

const pause = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

const binary = process.env.MAGI_VSCODE_TEST_BINARY;
test('owned companion, byte relay, external owner and shutdown races', { skip: !binary, timeout: 60_000 }, async () => {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(process.platform === 'win32' ? os.tmpdir() : '/tmp', 'mv-')));
  const previous = { ...process.env };
  Object.assign(process.env, {
    MAGI_CONFIG_DIR: path.join(root, 'cfg'), MAGI_SOCKET_DIR: path.join(root, 's'),
    MAGI_MODEL: 'test', MAGI_BASE_URL: 'http://127.0.0.1:9/v1', MAGI_NO_UPDATE_CHECK: '1',
    PATH: path.dirname(binary!) + path.delimiter + process.env.PATH,
  });
  const work = path.join(root, 'work'); fs.mkdirSync(work);
  const owner = new OwnedCompanion(work);
  const external = new OwnedCompanion(work);
  try {
    await Promise.all([owner.start(binary!), owner.start(binary!)]);
    const pid = JSON.parse(fs.readFileSync(owner.socket + '.session', 'utf8')).pid;
    const relay = await Daemon.bridge(owner.socket, 5000, binary);
    try {
      assert.equal((await relay.exchange({ method: 'about' })).ok, true);
      assert.equal((await relay.exchange({ method: 'status' })).ok, true);
    } finally { relay.close(); }
    const stream = await Daemon.bridge(owner.socket, 5000, binary);
    try {
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('stream frame did not arrive')), 3000);
        stream.stream({ method: 'about' }, (r) => { clearTimeout(timer); r.ok ? resolve() : reject(new Error(r.error)); });
      });
    } finally { stream.close(); }
    await external.start(binary!);
    await external.close();
    process.kill(pid, 0);
    await Promise.all([owner.close(), owner.close()]);
    for (let i = 0; i < 100 && fs.existsSync(owner.socket); i++) await new Promise((r) => setTimeout(r, 20));
    assert.equal(fs.existsSync(owner.socket), false, 'owned daemon must remove its socket');
    await owner.start(binary!);
    assert.equal(fs.existsSync(owner.socket), false, 'closed owner must not restart');
    const missing = new OwnedCompanion(work);
    try { await assert.rejects(missing.start(path.join(root, 'missing')), /failed to start/); }
    finally { await missing.close(); }
    const racing = new OwnedCompanion(work);
    const starting = racing.start(binary!);
    await racing.close(); await starting;
    assert.equal(fs.existsSync(owner.socket), false, 'closing during connect must prevent late spawn');
    await assert.rejects(Daemon.bridge(path.join(root, 'absent.sock'), 1000, binary));
  } finally {
    await owner.close(); await external.close();
    for (const k of Object.keys(process.env)) if (!(k in previous)) delete process.env[k];
    Object.assign(process.env, previous);
    fs.rmSync(root, { recursive: true, force: true });
  }
});

/**
 * A window that closed while the binary was being asked what it can do owns nothing afterwards.
 *
 * ⚠ **`close()` used to finish having stopped nothing, and the spawn still happened.** The guard at
 * the top of `launch` runs BEFORE the feature probe is awaited; `close()` stops `this.child`, and
 * during that wait there is no child yet — so it returned, its promise resolved, the window was
 * gone, and a daemon started anyway. Nothing was left to stop it: no `deactivate` runs twice, and
 * the owner pipe's write end belongs to an extension host that has finished with this companion.
 *
 * Measured 2026-09-11 (docs/CLIENT_LIFECYCLE_REVIEW_2026-09-11, R1): start → the probe waits →
 * `close()` resolves → the probe answers, and a child appeared with nothing left to stop it. This
 * test reproduces that order exactly, by holding the probe open until close has returned.
 *
 * The binary named here does not exist, which is deliberate: what is under test is whether the
 * window still tries to own a process after closing, and that question is answered before the
 * process would have to be real. A live daemon here would make the test need one.
 */
test('a window that closed during the feature probe starts nothing it does not stop', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-lifecycle-'));
  let answer!: (f: Set<string>) => void;
  const held = new Promise<Set<string>>((resolve) => { answer = resolve; });
  const c = new OwnedCompanion(dir, () => held);

  const started = c.start(path.join(dir, 'no-such-magi.exe'));
  await pause(50);          // the probe is in flight
  await c.close();          // and the window goes first
  answer(new Set());        // only now does the probe answer
  await started;

  const child = (c as unknown as { child?: ChildProcess }).child;
  assert.equal(child, undefined,
    'a companion was started after close() had already returned — nothing will ever stop it');
});
