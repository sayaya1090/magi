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

/**
 * Starting a companion this window cannot own is not a failure — but it is not silent either.
 *
 * docs/CLIENT_LIFECYCLE §4 blocks a new launch that REQUIRES the owned mode and, in the next
 * sentence, forbids falling back silently. A plain `--daemon` start is the lifetime this tree had
 * before the mode existed, and §4 preserves it; what it loses is the one thing a process handle
 * cannot do — an extension host that is KILLED runs no `deactivate`, and the daemon outlives the
 * window. A person is owed that difference (follow-up review R5).
 *
 * Once per start, because a window polls every fifteen seconds and a warning on every poll is
 * noise — which is how a real warning stops being read.
 */
test('a core that cannot be owned is reported once, not swallowed', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-unowned-'));
  const old = new OwnedCompanion(dir, async () => new Set(['raw-socket-v1']));
  await old.start(path.join(dir, 'no-such-magi')).catch(() => { /* a stand-in never comes up */ });
  assert.equal(old.unowned(), true, '소유할 수 없는 코어로 띄웠는데 아무 말이 없다');
  assert.equal(old.unowned(), false, '폴마다 되풀이한다 — 경고가 소음이 되면 안 읽힌다');
  await old.close();

  // A core that CAN be owned says nothing: there is nothing to tell.
  const now = new OwnedCompanion(dir, async () => new Set(['owned-daemon-v1']));
  await now.start(path.join(dir, 'no-such-magi')).catch(() => { /* same */ });
  assert.equal(now.unowned(), false, '소유 모드로 띄웠는데 경고가 뜬다');
  await now.close();
});

/**
 * And the window actually says it. `src/ide/start.ts` imports `vscode`, so it cannot be loaded
 * here — the call site is read off the source the way the JetBrains guards read Kotlin.
 *
 * ⚠ Pinned as the whole statement, not just the call: a version that asked `unowned()` and threw
 * the answer away would keep every word a looser check looks for.
 */
test('the start path shows the unowned-core warning', () => {
  const repo = path.join(__dirname, '..', '..', '..', '..');
  const src = fs.readFileSync(path.join(repo, 'clients/vscode/src/ide/start.ts'), 'utf8');
  assert.ok(/if \(owner\.unowned\(\)\)[^\n]*showWarningMessage\(/.test(src),
    'a core that cannot be owned is started and nobody is told — §4 forbids a silent fall-back');
  assert.ok(src.includes('UNOWNED_CORE'), 'the warning has no text of its own');
  // The sentence has to say what is LOST, or it is a notice nobody can act on.
  assert.ok(/killed/.test(src) && /Update magi/.test(src),
    'the warning does not say what the weaker lifetime costs, or what to do about it');
});
