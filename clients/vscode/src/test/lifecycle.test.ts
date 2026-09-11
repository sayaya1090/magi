import { test } from 'node:test';
import { ChildProcess } from 'child_process';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { DEFERRED_REPLACE_BY_MS, OwnedCompanion, REPLACE_BY_MS, sameGeneration } from '../core/lifecycle';
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
 * The same race, asked about what it LEAVES BEHIND.
 *
 * ⚠ **Losing the race used to cost a file handle each time.** The log fd was opened at the top of
 * `launch`, before the feature probe — and the `closed` checks that R1 added return between that
 * open and the `finally` that closes it. Nothing failed; the count only went one way, in an
 * extension host that lives as long as the window and races this every reload
 * (docs/CLIENT_LIFECYCLE_REVIEW_2026-09-11, R7).
 *
 * Counted, not inspected: the one honest question is whether this process holds more open
 * descriptors afterwards, and /dev/fd answers it. POSIX only — Windows has no such directory, and
 * the leak is not platform-specific, so measuring it on one platform is enough.
 */
test('a launch that loses the close race leaves no file handle behind',
  { skip: !fs.existsSync('/dev/fd') }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-lifecycle-'));
  const open = () => fs.readdirSync('/dev/fd').length;

  // One full race first, so anything one-off (the directory, module state) is already paid for.
  const once = async () => {
    let answer!: (f: Set<string>) => void;
    const held = new Promise<Set<string>>((resolve) => { answer = resolve; });
    const c = new OwnedCompanion(dir, () => held);
    const started = c.start(path.join(dir, 'no-such-magi.exe'));
    await pause(20);
    await c.close();
    answer(new Set());
    await started;
  };
  await once();

  const before = open();
  for (let i = 0; i < 8; i++) await once();
  const after = open();

  assert.ok(after <= before + 1,
    `eight lost races left ${after - before} more open descriptors — each one opened the daemon log ` +
    'and returned before the close');
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
/**
 * The OTHER weaker lifetime: the core HAS the owned mode, and this window could not take a pipe of
 * its own.
 *
 * ⚠ **Nobody was reading `held`.** `ownerChannel` falls back to Node's `'pipe'` when it cannot serve
 * one — deliberately, because refusing to start over a pipe name somebody else took would hand that
 * person the outage — but the lifetime it falls back TO is exactly the one R2 fixed: on Windows an
 * update alone ends the companion while its window is still open. `OwnerChannel.held` was declared
 * and had no reader anywhere in the client, so the question could be asked and nobody asked it
 * (issue #189, review R8).
 *
 * Once per start, for the reason the unowned-core notice is: a window polls every fifteen seconds,
 * and a warning on every poll is how a real warning stops being read.
 *
 * The channel is injected, not squatted. `owner.test.ts` produces the real failure against a name
 * taken first; what is under test HERE is whether this class carries the reason out, and a window
 * that draws eight random hex digits cannot be made to fail on cue.
 */
test('a window that could not take its own owner pipe says so once', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-unpiped-'));
  const owned = async () => new Set(['owned-daemon-v1']);

  // A start that DID take its pipe has nothing to say.
  const held = new OwnedCompanion(dir, owned,
    async () => ({ stdin: 'pipe' as const, held: true, why: '', close() {} }));
  await held.start(path.join(dir, 'no-such-magi.exe')).catch(() => { /* a stand-in never comes up */ });
  assert.equal(held.unpiped(), '', '파이프를 쥐었는데 경고가 뜬다');
  await held.close();

  // And one that could not.
  const taken = 'could not listen on \\\\.\\pipe\\magi-owner-x: listen EADDRINUSE';
  const fell = new OwnedCompanion(dir, owned,
    async () => ({ stdin: 'pipe' as const, held: false, why: taken, close() {} }));
  await fell.start(path.join(dir, 'no-such-magi.exe')).catch(() => { /* same */ });
  assert.equal(fell.unpiped(), taken,
    '노드의 파이프로 물러섰는데 아무 말이 없다 — 다음 업데이트가 컴패니언을 끝낸다');
  assert.equal(fell.unpiped(), '', '폴마다 되풀이한다 — 경고가 소음이 되면 안 읽힌다');
  await fell.close();
});

/**
 * And the start path actually draws it. Measured from source for the reason the unowned-core guard
 * below is: `start.ts` imports `vscode`, so this suite cannot call it.
 */
test('the start path shows the unheld-pipe warning', () => {
  const repo = path.join(__dirname, '..', '..', '..', '..');
  const src = fs.readFileSync(path.join(repo, 'clients/vscode/src/ide/start.ts'), 'utf8');
  assert.match(src, /owner\.unpiped\(\)/,
    'nothing reads why the window fell back — the companion is on the old lifetime in silence');
  assert.match(src, /UNHELD_PIPE/, 'the warning has no text of its own');
  // The sentence has to say what is LOST, or it is a notice nobody can act on.
  assert.match(src, /END it/,
    'the warning does not say that updating or restarting will end this companion');
});

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

type Inner = {
  close(): Promise<void>;
  start(binary: string, manual?: boolean): Promise<void>;
  replacing(now?: number): void;
  replacingWhenIdle(now?: number): void;
  ended(now: number, child?: ChildProcess): void;
  theirs(): boolean;
  budget: { may(now: number, manual?: boolean): string; ready(now: number): void };
};
/** One window, reached at the seam. Not an intersection: a private field would reduce it to never. */
const window_ = (dir: string): Inner =>
  new OwnedCompanion(dir, async () => new Set()) as unknown as Inner;

/**
 * The Restart button is not a crash.
 *
 * ⚠ **On Windows it looked exactly like one.** `magi.updateCore` and `magi.restartDaemon` make the
 * daemon restart itself, and Windows has no execve — `internal/graceful/graceful_windows.go` spawns
 * a successor and ends this process. So the child this window owns EXITS, and the exit handler
 * counted `lost`: a consecutive failure, three of which are `failuresToBlock`. Three presses of
 * Restart inside one stable window and the companion is Blocked, with this window refusing to start
 * it automatically ever again — for a button the person pressed on purpose. On Unix the image is
 * replaced in place and the PID never changes, so none of this was visible there.
 *
 * Measured 2026-09-11 on Windows 11 against the real binary: the owned child exited 27ms after the
 * `restart` door answered `{"ok":true}`.
 *
 * The contract has had the case the whole time — "an update replacement the person asked for is not
 * a failure" — and `Launches.replaced` was dead code: nothing in this client called it.
 *
 * Driven through `ended` rather than a live daemon on purpose. What is under test is what an ending
 * MEANS, and reaching it through a process would need the one platform where the process is the
 * thing that goes away.
 */
test('three replacements the person asked for do not block, three losses do', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-replace-'));
  const asked = window_(dir);
  const crashed = window_(dir);

  // Three of each, every one of them an ending of a daemon that had connected.
  for (let i = 0; i < 3; i++) {
    const t = i * 10_000;
    asked.budget.ready(t); asked.replacing(t); asked.ended(t + 30);
    crashed.budget.ready(t); crashed.ended(t + 30);
  }
  assert.equal(crashed.budget.may(100_000, false), 'blocked',
    'three unexplained endings must still block — the pardon has to be for the button, not for everything');
  assert.equal(asked.budget.may(100_000, false), 'allow',
    'the person pressed Restart three times and this window now refuses to start their companion');
});

/**
 * And the pardon expires. `update` answers "already up to date" without restarting anything, so an
 * arm that outlived the door would hand the next real crash a free life — on a flag nobody can see.
 */
test('a replacement that never came is not a pardon for what crashes later', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-replace-late-'));
  const c = window_(dir);
  c.budget.ready(0);
  c.replacing(0);                 // asked, and the daemon said "already up to date"
  c.ended(REPLACE_BY_MS + 1);     // what died later died on its own
  assert.equal(c.theirs(), true,
    'an ending past the shutdown budget was treated as the replacement that never happened');
});

/**
 * A replacement that waits for a quiet moment is still the person's own button.
 *
 * ⚠ **Five seconds is the wrong window for this one.** `update` with `idle` puts the build on disk
 * now and restarts when the companion next goes quiet — which is minutes away if a turn is running,
 * and that is exactly the case somebody chooses it for. Counted against the immediate window, the
 * ending that finally arrives is read as a crash: the defect `c5373a08` fixed for the other path,
 * coming back through the new door.
 */
test('an update that waits for a quiet moment is not a crash when it finally lands', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-replace-idle-'));
  const c = window_(dir);
  c.budget.ready(0);
  c.replacingWhenIdle(0);
  // Four minutes of turn, then the companion goes quiet and the deferred restart happens.
  c.ended(4 * 60_000);
  assert.equal(c.theirs(), false,
    'the deferred restart was counted as a crash — three of those and this window stops starting it');
});

/** And that latch is still bounded: a replacement that never came pardons nothing a day later. */
test('a deferred replacement that never came is not a pardon forever', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-replace-never-'));
  const c = window_(dir);
  c.budget.ready(0);
  c.replacingWhenIdle(0);
  c.ended(DEFERRED_REPLACE_BY_MS + 1);
  assert.equal(c.theirs(), true, 'a stale latch pardoned a real crash');
});

/**
 * A successor this window asked for is still this window's companion.
 *
 * ⚠ **`!this.child` was standing in for "somebody else's daemon", and after a replacement it is
 * neither.** On Windows the successor is a process this window never spawned and holds no handle
 * for — but it inherited the owner pipe this extension host holds, so it is not a pre-existing
 * companion belonging to its caller. Read as external, `launch` returns at its first line for
 * anything not manual: the window would never start this workspace's companion again, and `close()`
 * would leave it to the pipe alone.
 */
test('the window does not disown the companion it just asked to replace itself', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-replace-own-'));
  const c = window_(dir);
  assert.equal(c.theirs(), true, 'with no child and nothing asked for, a live socket IS somebody else’s');
  c.budget.ready(0);
  c.replacing(0);
  c.ended(30);
  assert.equal(c.theirs(), false,
    'the window disowned the daemon it had just asked for — nothing automatic will ever start it again');
});

/**
 * "Something answered" is not "the child I started answered".
 *
 * ⚠ **Both ids were on the wire, and in this client's own `Response` type, the whole time — and
 * nothing read either.** docs/CLIENT_LIFECYCLE §4 asks readiness to require the child pid, the
 * resolved workspace AND a matching instance in the record and in `about`; this client matched the
 * pid and then asked only whether *anybody* was reachable. The record beside a socket and the
 * process on it can disagree: a previous daemon's record outliving it, a replacement mid-flight,
 * somebody else's companion on a path this window also resolved.
 *
 * Measured as a function rather than through a live daemon on purpose — producing "the record says
 * one generation and the socket answers another" with real processes means racing a replacement,
 * and the rule is what is under test.
 */
test('readiness needs the record and the socket to name one generation', () => {
  const child = 4242;
  // The ordinary case: our child, and the process answering is the one in the record.
  assert.equal(sameGeneration({ pid: child, instance: 'i-1' }, { instance: 'i-1' }, child), true);
  // The case this exists for: the record is ours, and something ELSE is answering.
  assert.equal(sameGeneration({ pid: child, instance: 'i-1' }, { instance: 'i-2' }, child), false,
    'a different process answered and the window called itself ready');
  // Nobody answered at all.
  assert.equal(sameGeneration({ pid: child, instance: 'i-1' }, null, child), false);
  // Somebody else's daemon, whatever it says about itself.
  assert.equal(sameGeneration({ pid: 9, instance: 'i-1' }, { instance: 'i-1' }, child), false);
  assert.equal(sameGeneration(null, { instance: 'i-1' }, child), false);
});

/** An older core writes no instance. It keeps the lifetime it always had rather than being failed. */
test('a core that names no generation still gets ready on the pid', () => {
  assert.equal(sameGeneration({ pid: 7 }, {}, 7), true);
  assert.equal(sameGeneration({ pid: 7, instance: 'i-1' }, {}, 7), true,
    'the record names one and the daemon does not — an old client cannot be held to a new field');
  assert.equal(sameGeneration({ pid: 8 }, {}, 7), false, 'the pid still has to be ours');
});

/**
 * And the lineage answers "is this still mine" for a successor this window never spawned.
 *
 * On Windows an update replaces the process, so the window holds a handle to something that has
 * exited. `ownsSuccessor` was a local flag standing in for that; the owner id is the same fact
 * taken from the wire, and it survives however many times the daemon replaces itself.
 */
test('a daemon publishing the lineage this window confirmed is not somebody else\'s', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-lineage-'));
  const c = window_(dir) as unknown as {
    theirs(): boolean; lineage?: string; ownsSuccessor: boolean; socket: string;
  };
  const record = (owner?: string) =>
    fs.writeFileSync(c.socket + '.session', JSON.stringify({ pid: 1, owner }));
  fs.mkdirSync(path.dirname(c.socket), { recursive: true });

  c.lineage = 'own-1';
  record('own-1');
  assert.equal(c.theirs(), false, 'the companion this window owns was disowned after it replaced itself');
  record('own-2');
  assert.equal(c.theirs(), true, 'a different lineage is somebody else\'s daemon');
  // No owner published at all: the old rule, unchanged.
  record(undefined);
  c.ownsSuccessor = true;
  assert.equal(c.theirs(), false);
});

/**
 * And the ready path really keeps it.
 *
 * ⚠ **A test that sets `lineage` by hand measures the branch that reads it, not the code that
 * writes it** — measured: deleting the line that remembers the lineage left every assertion above
 * green. So this drives the whole ready path with a real child and a fixture that behaves the way a
 * daemon does: publish a record, then answer `about` naming the same generation.
 *
 * POSIX only — the stand-in binary is a shell script.
 */
test('the ready path keeps the lineage the daemon named',
  { skip: process.platform === 'win32' }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-ready-'));
  const bin = path.join(dir, 'fake-magi');
  fs.writeFileSync(bin, '#!/bin/sh\nsleep 5\n', { mode: 0o755 });

  // Not an intersection with the class: a private field reduces that to `never` (the same reason
  // `Inner` above is a plain shape).
  type Ready = { start(b: string, m?: boolean): Promise<void>; close(): Promise<void>;
                 lineage?: string; child?: ChildProcess; socket: string };
  let c!: Ready;
  const published = () => {
    // What a daemon does on the way up, in the order it does it: write the record, then answer for
    // the same generation. The pid is the child's, because that is the fact the record carries.
    const pid = c.child?.pid;
    if (pid) {
      fs.mkdirSync(path.dirname(c.socket), { recursive: true });
      fs.writeFileSync(c.socket + '.session', JSON.stringify({ pid, instance: 'i-7', owner: 'own-7' }));
    }
    return Promise.resolve({ instance: 'i-7', owner: 'own-7' });
  };
  c = new OwnedCompanion(dir, async () => new Set(), undefined, published) as unknown as Ready;

  try {
    await c.start(bin, true);
    assert.equal(c.lineage, 'own-7',
      'the window reached ready and forgot which lineage it had confirmed — a successor it never ' +
      'spawned is then read as somebody else\'s daemon');
  } finally { await c.close(); }
});

/**
 * Closing is an ORDER, and a step that fails does not cancel the rest of it.
 *
 * docs/CLIENT_LIFECYCLE §5: block new work → cancel launches and retries → try to release the
 * editor hand → clean up subscriptions and connections → close the owner pipe → confirm. "A failed
 * release does not skip the remaining cleanup", and "closing must give the same result when called
 * twice".
 *
 * ⚠ **The pipe is the step that matters most and the one easiest to skip.** `stop` talks to the
 * socket and then kills the process this window spawned — but after a replacement that process is
 * already gone and the daemon answering is a successor this window holds no handle for. The owner
 * pipe is the only thing that reaches THAT one. A `stop` that throws must not take it with it.
 */
test('a failing stop still closes the owner pipe', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-closing-'));
  let closed = 0;
  const channel = { stdin: 'pipe' as const, held: true, close() { closed++; } };
  const c = window_(dir) as unknown as {
    close(): Promise<void>; child?: unknown; channel?: unknown; stop(child: unknown): Promise<void>;
  };
  c.channel = channel;
  c.child = { pid: 1 };
  c.stop = () => Promise.reject(new Error('the daemon would not answer and the kill failed'));

  // It SETTLES. A rejection here is an error VS Code reports about a shutdown that went fine — and
  // the rejected promise is cached, so every later close rejects again with nobody awaiting it.
  await c.close();
  assert.equal(closed, 1,
    'the owner pipe was left open because stopping failed — after a replacement that pipe is the ' +
    'only thing that reaches the daemon, so it would keep running with nobody to end it');
  assert.match(String((c as unknown as { closeTrouble?: Error }).closeTrouble?.message),
    /would not answer/, 'the trouble was swallowed without a trace — nobody can say what went wrong');
});

/** And closing twice is the same as closing once — `dispose` is called more than once. */
test('closing twice settles the same way', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-closing-twice-'));
  let closed = 0;
  let stops = 0;
  const c = window_(dir) as unknown as {
    close(): Promise<void>; channel?: unknown; child?: unknown; stop(child: unknown): Promise<void>;
  };
  c.channel = { stdin: 'pipe' as const, held: true, close() { closed++; } };
  // ⚠ **With no child, a second close has nothing left to do and looks idempotent either way** —
  // measured: removing the guard entirely left this green. The child is what makes the repeat
  // visible: stopping twice means talking to a socket and killing a pid that is already gone.
  c.child = { pid: 1 };
  c.stop = () => { stops++; return Promise.resolve(); };

  await Promise.all([c.close(), c.close()]);
  await c.close();
  assert.equal(stops, 1, 'closing ran the shutdown again — dispose is called more than once');
  assert.equal(closed, 1, 'each close let go of the pipe again — the second one has nothing to let go of');
});

/**
 * And after closing, nothing starts. A window that has closed has no child, and a start that
 * slipped through would leave a daemon nobody stops (the R1 shape, asked at the other end).
 */
test('a closed window starts nothing', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-closed-start-'));
  const c = window_(dir) as unknown as {
    close(): Promise<void>; start(b: string, m?: boolean): Promise<void>; child?: unknown;
  };
  await c.close();
  await c.start(path.join(dir, 'no-such-magi'), true);
  assert.equal(c.child, undefined, 'a closed window spawned a companion nothing will ever stop');
});
