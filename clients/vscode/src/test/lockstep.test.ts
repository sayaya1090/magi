import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as net from 'net';
import * as os from 'os';
import * as path from 'path';
import * as fs from 'fs';
import { Daemon } from '../core/daemon';

/**
 * The wire is lock-step, and a timeout breaks the step.
 *
 * One request, one reply, in order: the reader hands each line to the head of the queue. So a
 * request whose answer is late cannot simply be forgotten — the answer still arrives, and if its
 * waiter is gone the queue is one short and EVERY later caller receives the previous call's answer.
 * A `status` reply read as `jobs`, for the life of the connection, with nothing failing.
 *
 * The old code did exactly that, and its comment argued for it the wrong way round ("a timed-out
 * waiter left in the queue would hand the NEXT answer to the wrong caller"). Both siblings hang up
 * instead — the JetBrains client throws `DaemonGone("…끊었다")`, and the ide-bridge calls `hangUp()`
 * saying "a reply that never came leaves the stream out of step, and reusing it would hand the next
 * caller this call's answer".
 *
 * Run against a real socket, because this is about ordering between a timer and a reader — the
 * thing a source-reading guard cannot see.
 *
 * ⚠ **The fake daemon must answer IN REQUEST ORDER.** The first cut of this test answered the
 * second request immediately and the first one late; under that (impossible) ordering the old code
 * looks correct and the mutation restoring it survived. A surviving mutation is a question about
 * the test first — re-modelled to the ordering the wire actually guarantees, the defect reproduces
 * every time.
 *
 * Leaving the timed-out waiter in the queue would also absorb the late reply, and was rejected: it
 * leaves a corpse at the head of a queue that never drains if the daemon answers nothing at all,
 * and both siblings hang up instead.
 */
test('a timed-out call does not hand its answer to the next caller', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-lockstep-'));
  const sock = path.join(dir, 'd.sock');
  const late: net.Socket[] = [];
  const server = net.createServer((c) => {
    late.push(c);
    let n = 0;
    c.on('data', () => {
      n++;
      // **In request order** — this wire is lock-step and a real daemon answers one at a time. The
      // first answer is LATE (past its caller's deadline); the second follows it. So the late reply
      // is the next line to arrive, and whoever is at the head of the queue gets it.
      if (n === 1) setTimeout(() => c.write(JSON.stringify({ ok: true, out: 'FIRST' }) + '\n'), 120);
      else setTimeout(() => c.write(JSON.stringify({ ok: true, out: 'SECOND' }) + '\n'), 140);
    });
  });
  await new Promise<void>((r) => server.listen(sock, r));

  try {
    const d = await Daemon.connect(sock);
    const first = d.exchange({ method: 'status' }, 30).then(() => 'answered').catch((e) => String(e.message));
    assert.match(await first, /did not answer status/, 'the deadline did not fire');

    // The connection is out of step now. Whatever happens next must not be the first call's answer.
    const second = await d.exchange({ method: 'jobs' }, 200).catch((e) => ({ ok: false, error: String(e.message) }));
    assert.notEqual((second as { out?: string }).out, 'FIRST',
      'the second caller received the FIRST call\'s answer — every reply after a timeout is off by one');
    d.close();
  } finally {
    for (const c of late) c.destroy();
    server.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
