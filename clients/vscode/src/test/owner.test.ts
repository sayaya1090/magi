import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { spawn } from 'child_process';
import * as net from 'net';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { ownerChannel, ownerPipeName } from '../core/owner';

const pause = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));
const win = process.platform === 'win32';

/** A child that reports, into a file, the moment its stdin ends. Its own stdin is the channel. */
function reporter(dir: string, name: string): string {
  const mark = path.join(dir, name);
  const js = path.join(dir, name + '.js');
  fs.writeFileSync(js, [
    'const fs = require("fs");',
    'process.stdin.resume();',
    'process.stdin.on("end", () => { fs.writeFileSync(' + JSON.stringify(mark) + ', "eof"); process.exit(0); });',
    'setTimeout(() => {}, 20000);',
  ].join('\n'));
  return js;
}

/**
 * The fact everything else here rests on: **Node owns the pipe it makes, and destroys it when the
 * child exits.**
 *
 * docs/CLIENT_LIFECYCLE §4 requires the IDE to own the write end of the owner pipe exclusively. With
 * `stdio: 'pipe'` it does not — `child_process` does, and `ChildProcess` destroys `child.stdin` on
 * exit. So the write end's lifetime is the CHILD's, which is the whole of R2.
 *
 * Measured here rather than asserted in prose, and on every platform, because the day a Node release
 * stops doing this is the day the reason for `ownerChannel` is gone — and nobody would find that out
 * from a comment.
 */
test('Node destroys the stdin it made, the moment the child exits', async () => {
  const child = spawn(process.execPath, ['-e', 'setTimeout(()=>{},50)'], { stdio: ['pipe', 'ignore', 'ignore'] });
  assert.ok(child.stdin, 'stdio "pipe" is supposed to give the parent a write end');
  await new Promise<void>((r) => child.once('exit', () => r()));
  await pause(100);
  assert.equal(child.stdin!.destroyed, true,
    'Node no longer destroys child.stdin on exit — R2 is gone and ownerChannel can go with it');
});

/**
 * And the channel this window makes does not, which is the fix.
 *
 * ⚠ **The grandchild is the point, not the child.** Windows has no execve, so a daemon asked to
 * restart spawns a successor carrying the same read end and ends itself
 * (`internal/graceful/graceful_windows.go`). With Node's pipe the successor read EOF within
 * milliseconds and stopped — an update alone killed the companion, while its window was open and had
 * closed nothing. This reproduces that shape with plain Node processes: a predecessor that hands its
 * stdin on and exits.
 *
 * Then the other half, which is what the pipe is FOR: when the window lets go, the successor must
 * end. A tether that survives everything is not a tether.
 */
test('the owner channel survives a handover, and still ends the successor', { skip: !win ? 'Windows only: POSIX replaces the image in place, so no handover happens' : false }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-owner-'));
  const channel = await ownerChannel('guard');
  assert.equal(channel.held, true, 'the window did not manage to make a pipe of its own');

  const suc = reporter(dir, 'successor');
  const pre = path.join(dir, 'predecessor.js');
  fs.writeFileSync(pre, [
    'const { spawn } = require("child_process");',
    // Exactly graceful_windows.go: hand the successor this process's stdin, then go.
    'const b = spawn(process.execPath, [' + JSON.stringify(suc) + '], { stdio: [process.stdin, "ignore", "ignore"], detached: true });',
    'b.unref();',
    'setTimeout(() => process.exit(0), 200);',
  ].join('\n'));

  const a = spawn(process.execPath, [pre], { stdio: [channel.stdin, 'ignore', 'ignore'] });
  assert.equal(a.stdin, null, 'Node made a pipe of its own — it will destroy it and take the successor with it');
  await new Promise<void>((r) => a.once('exit', () => r()));
  await pause(700);
  assert.equal(fs.existsSync(path.join(dir, 'successor')), false,
    'the successor read EOF from its predecessor merely exiting — an update alone kills the companion');

  channel.close();
  let ended = false;
  for (let i = 0; i < 60 && !ended; i++) { await pause(100); ended = fs.existsSync(path.join(dir, 'successor')); }
  assert.equal(ended, true,
    'the window let go and the successor did not end — nothing stops a companion whose window is gone');
});

/**
 * The daemon is handed a HANDLE, never an address.
 *
 * ⚠ **That distinction is the security of this mode.** docs/CLIENT_LIFECYCLE §4: lifetime authority
 * travels only through the inherited pipe, because a pipe cannot be guessed, copied out of a file,
 * or read off another process's environment. A channel the core *dialled* by name would trade that
 * for a name anybody can dial — the cost §2.5 records against the "listen and let the core attach"
 * option. So the listening socket is gone before the child is even spawned.
 */
test('nothing else on this machine can dial the owner pipe', { skip: !win ? 'Windows only: the named-pipe namespace is a Windows thing' : false }, async () => {
  const channel = await ownerChannel('nameless');
  try {
    // The name stays VISIBLE while the connected instance lives — that is how Windows lists pipes,
    // and it is not the question. The question is whether a FREE instance is still waiting for a
    // caller, because that is the one an intruder could take.
    const name = fs.readdirSync('//./pipe/').find((n) => n.startsWith('magi-owner-nameless-'));
    assert.ok(name, 'the channel was not made at all');
    const dialled = await new Promise<string>((resolve) => {
      const s = net.connect(path.join('//./pipe/', name!), () => { s.destroy(); resolve('connected'); });
      s.once('error', (e: NodeJS.ErrnoException) => resolve(e.code ?? 'refused'));
      setTimeout(() => { s.destroy(); resolve('refused'); }, 2000);
    });
    assert.notEqual(dialled, 'connected',
      'the owner pipe is still accepting callers — anything on this machine can dial it');
  } finally { channel.close(); }
});

/**
 * POSIX keeps today's pipe on purpose. `syscall.Exec` replaces the image and the PID never changes,
 * so the child never exits, Node never destroys anything, and there is no handover to survive.
 */
test('POSIX is left exactly as it was', { skip: win ? 'this is the POSIX half' : false }, async () => {
  const channel = await ownerChannel('posix');
  assert.equal(channel.stdin, 'pipe');
  assert.equal(channel.held, false, 'POSIX grew a channel of its own — nothing there was broken');
  channel.close();
});


/**
 * A fallback always says why, on every platform.
 *
 * ⚠ **Four ways to fail all answered the same nothing.** `net.createServer`, `listen`, the dial and
 * the accept sat in one `try` under a bare `catch { return nodes; }` — same value, no trace, on the
 * one platform where the fallback costs the companion its life on the next update (issue #189,
 * review R8). POSIX falls back too, by design, and it owes the same sentence: a reader asking "why
 * is this companion on the old lifetime" must get an answer and not a shrug.
 */
test('a channel that is not held says why, and one that is held has nothing to explain', async () => {
  const held = await ownerChannel('explains');
  try {
    if (held.held) {
      assert.equal(held.why, '', 'a channel this window holds has nothing to apologise for');
    } else {
      assert.notEqual(held.why.trim(), '', 'fell back to Node’s pipe and said nothing about it');
    }
  } finally { held.close(); }
});

/**
 * A name somebody else took is a fallback, not a failure — and it says which step gave up.
 *
 * ⚠ **Four ways to fail answered one bare `catch { return nodes; }`.** `createServer`, `listen`, the
 * dial and the accept all produced the same value and left no trace, on the one platform where the
 * fallback costs the companion its life on the next update (issue #189, review R8).
 *
 * Injected rather than imagined: a squatter takes the name first, which is the realistic failure
 * this fallback exists for — the name carries this window's pid and eight random hex digits, so
 * somebody holding it already is the way it actually goes wrong.
 *
 * ⚠ **What this does NOT claim.** The dial and the accept cannot be made to fail from a test: both
 * ends are this process, and a name this window just bound is one it can always dial. Their cleanup
 * is structural instead — every step registers its undo before the next one can throw — and the
 * accept carries a deadline so the unreachable branch cannot hang a window instead of failing it.
 * Measured, not assumed, on the reachable one: Node closes its own failed bind (the stray handle is
 * gone within 50ms without anyone asking), so there is nothing here for a resource count to catch
 * and a count would be a test that cannot fail.
 */
test('a taken pipe name falls back, and names the step that gave up', { skip: !win ? 'Windows only: named pipes' : false }, async () => {
  const name = ownerPipeName('squatted');
  const squatter = net.createServer();
  await new Promise<void>((resolve, reject) => {
    squatter.once('error', reject);
    squatter.listen(name, () => resolve());
  });
  try {
    const fell = await ownerChannel('squatted', name);
    try {
      assert.equal(fell.held, false, 'the name was already taken — this window cannot have held it');
      assert.equal(fell.stdin, 'pipe', 'the fallback must be the channel that still works');
      assert.match(fell.why, /listen/i,
        `the reason does not name the step that gave up: ${JSON.stringify(fell.why)}`);
    } finally { fell.close(); }
  } finally {
    await new Promise<void>((resolve) => squatter.close(() => resolve()));
  }
});

/**
 * And a channel that IS held gives everything back when it is closed.
 *
 * The success path is the one that runs on every start of every window, so a socket it forgot would
 * accumulate for as long as a person keeps reloading. Counted after the loop has settled, because
 * Node tears its own handles down a turn or two late and a sample taken too early measures the
 * teardown rather than what survives it.
 */
test('opening and closing the channel a hundred times leaves nothing behind', { skip: !win ? 'Windows only: POSIX never opens one' : false }, async () => {
  const settle = async () => { for (let i = 0; i < 10; i++) await pause(20); };
  const live = () => process.getActiveResourcesInfo().filter((r) => r.includes('Pipe')).length;

  (await ownerChannel('cycle')).close();   // warm
  await settle();
  const before = live();
  for (let i = 0; i < 100; i++) {
    const c = await ownerChannel('cycle');
    assert.equal(c.held, true, `attempt ${i}: the window failed to take a pipe of its own — ${c.why}`);
    c.close();
  }
  await settle();
  assert.ok(live() <= before + 1,
    `a hundred open/close cycles left ${live() - before} pipe handles alive — close() is not ` +
    'giving back everything the open took');
});
