import * as net from 'net';
import { Duplex } from 'stream';
import { spawn } from 'child_process';
import { features, found, whyNoRelay } from './binary';
import { Request, Response } from './protocol';

/**
 * One connection to one companion, speaking line-delimited JSON.
 *
 * The daemon writes with `json.NewEncoder(conn)` and reads with a `bufio.Scanner`: one request per
 * line, one response per line. Stream doors (`transcript`, `watch`) keep answering on the same
 * connection until it closes.
 *
 * ⚠ **Never half-close the write side.** A one-shot client that shuts write after sending — the
 * `nc -w` shape — reads to the daemon as a hang-up and the stream ends with zero frames. Keeping
 * the write half open while you read is half the stream contract (`docs/CLIENTS`).
 */
export class Daemon {
  private sock: Duplex;
  private buf = '';
  private waiting: ((r: Response) => void)[] = [];
  private onFrame: ((r: Response) => void) | null = null;
  private closed = false;
  private onClose: (() => void)[] = [];

  private constructor(sock: Duplex) {
    this.sock = sock;
    sock.setEncoding('utf8');
    sock.on('data', (chunk: string) => this.take(chunk));
    sock.on('close', () => this.finish());
    sock.on('error', () => this.finish());
  }

  static connect(path: string, connectMs = 5000): Promise<Daemon> {
    if (process.platform === 'win32') return Daemon.bridge(path, connectMs);
    return new Promise((resolve, reject) => {
      const sock = net.createConnection({ path });
      const timer = setTimeout(() => {
        sock.destroy();
        reject(new Error(`nothing answered at ${path} within ${connectMs}ms`));
      }, connectMs);
      sock.once('connect', () => { clearTimeout(timer); resolve(new Daemon(sock)); });
      sock.once('error', (e) => { clearTimeout(timer); reject(e); });
    });
  }

  /** Go owns AF_UNIX on Windows; Node's path transport uses named pipes there. */
  static async bridge(path: string, connectMs = 5000, binary = found()): Promise<Daemon> {
    if (!binary) throw new Error('magi.exe is needed to connect on Windows; put it on PATH.');
    // ⚠ **Ask whether this build can relay before spawning one that cannot.**
    //
    // A core older than `--raw-socket` refuses the flag and exits 2, and everything downstream then
    // reports a connection that "nothing answered" — a sentence about the daemon, for a problem in
    // the executable beside it. docs/CLIENT_LIFECYCLE §2 names this gap for the Windows transport:
    // "구형 코어의 relay 지원 여부를 연결 전에 확인".
    //
    // Costs one short probe per connection on Windows only: it contacts no daemon and touches no
    // disk, and an answer that does not come inside the contract's five seconds is a binary that
    // was not going to work anyway.
    const refusal = whyNoRelay(await features(binary), binary);
    if (refusal) throw new Error(refusal);
    const child = spawn(binary, ['ide-bridge', '--raw-socket', path], { windowsHide: true, stdio: 'pipe' });
    const sock = Duplex.from({ readable: child.stdout, writable: child.stdin });
    let why = '';
    child.stderr.on('data', (b: Buffer) => { why = (why + b.toString()).slice(-4096); });
    child.on('error', (e) => sock.destroy(e));
    child.on('exit', () => sock.destroy());
    sock.once('close', () => { child.kill(); });
    const d = new Daemon(sock);
    try {
      const hello = await d.exchange({ method: 'about' }, connectMs);
      if (!hello.ok) throw new Error(why.trim() || hello.error || 'the bridge could not connect');
      return d;
    } catch (e) { d.close(); throw e; }
  }

  /** One question, one answer. */
  exchange(req: Request, deadlineMs = 30_000): Promise<Response> {
    return new Promise((resolve, reject) => {
      if (this.closed) return reject(new Error('the connection is closed'));
      const timer = setTimeout(() => {
        // ⚠ **A timeout puts the connection out of step, and nothing on it can be trusted again.**
        //
        // This wire is lock-step: one request, one reply, in order, and `read` hands each line to
        // the head of the queue. The old code took the timed-out waiter OUT of the queue — and the
        // comment there argued that leaving it would misdeliver the next answer. It is the other
        // way round. The late reply still arrives; with its waiter gone, the queue is one short and
        // every caller after it receives the PREVIOUS call's answer — a `status` reply read as
        // `jobs`, for the life of the connection, with nothing failing.
        //
        // So hang up. The stream cannot be repaired by rearranging waiters, and both siblings do
        // exactly this: the JetBrains client throws `DaemonGone("...끊었다")` and the ide-bridge
        // calls `hangUp()` with the same sentence — "a reply that never came leaves the stream out
        // of step, and reusing it would hand the next caller this call's answer".
        //
        // `close()` settles everyone still waiting, this one included, so nobody is left hanging.
        this.close();
        reject(new Error(`the companion did not answer ${req.method} within ${deadlineMs}ms`));
      }, deadlineMs);
      const done = (r: Response) => { clearTimeout(timer); resolve(r); };
      this.waiting.push(done);
      this.sock.write(JSON.stringify(req) + '\n');
    });
  }

  /**
   * A door that keeps answering. `each` sees every frame until the connection closes.
   *
   * The write half stays open — see the warning on this class. Stop by closing the whole thing.
   */
  stream(req: Request, each: (r: Response) => void): void {
    this.onFrame = each;
    this.sock.write(JSON.stringify(req) + '\n');
  }

  whenClosed(f: () => void): void {
    if (this.closed) f(); else this.onClose.push(f);
  }

  close(): void { this.sock.destroy(); }

  private take(chunk: string): void {
    this.buf += chunk;
    for (let nl = this.buf.indexOf('\n'); nl >= 0; nl = this.buf.indexOf('\n')) {
      const line = this.buf.slice(0, nl);
      this.buf = this.buf.slice(nl + 1);
      if (!line.trim()) continue;
      let resp: Response;
      try {
        resp = JSON.parse(line) as Response;
      } catch {
        // A line that is not JSON is not a reply to anything. Dropping it keeps the queue aligned;
        // treating it as an answer would hand a caller garbage and shift every later reply by one.
        continue;
      }
      const next = this.waiting.shift();
      if (next) next(resp);
      else if (this.onFrame) this.onFrame(resp);
    }
  }

  private finish(): void {
    if (this.closed) return;
    this.closed = true;
    // Everyone still waiting gets an answer, because a promise nobody settles is a screen that
    // says nothing for ever.
    const err: Response = { ok: false, error: 'the companion closed the connection' };
    for (const w of this.waiting.splice(0)) w(err);
    for (const f of this.onClose.splice(0)) f();
  }
}

/**
 * How long to wait before the next attempt to get a stream back.
 *
 * A rule, not a loop, so it can be MEASURED. The window's retry lives in a webview host with a
 * socket and a timer; a test can read its source and see that a wait exists, but not that the wait
 * grows or stops — a mutation that returned early from the retry walked straight past a
 * source-reading guard. So the schedule moves here, where a test can run it.
 *
 * Grows from a second and stops at thirty. The floor is because a daemon that just went is not
 * coming back this millisecond and a tight loop turns one restart into a busy panel; the ceiling
 * is because a person who starts it again should not wait minutes for the window to notice. The
 * JetBrains client uses the same two numbers, and this is the same fact in the other language.
 */
export function retryAfter(attempt: number): number {
  const first = 1_000;
  const cap = 30_000;
  if (attempt <= 0) return first;
  return Math.min(first * 2 ** attempt, cap);
}

/**
 * Doors whose answer waits on the MODEL, and therefore on somebody else's hardware.
 *
 * Named, not guessed: each of these runs a generation before it can reply — a completion, a ghost
 * line, a commit message, a look over a file, a pull-request write-up, a turn in a meeting. The
 * rest of the wire answers from memory or from disk and should be quick.
 */
const THINKS = new Set([
  'complete', 'suggest', 'git-msg', 'look-over', 'pr-msg', 'git-pr', 'meet', 'meet-join',
  // ⚠ **Folding is a generation, and the biggest one this client asks for.** The core says so in
  // `App.Compact`: manual compaction "replaces the whole conversation with a real model-written
  // brief (same summarizer the auto-compaction path uses)" — so the prompt is the ENTIRE
  // conversation, on somebody else's hardware, and it sat on the 30s deadline. What that costs is
  // written four lines down: a late reply does not merely go missing, it hangs up the socket under
  // whatever else was in flight. The JetBrains client has given it two minutes all along — its
  // `connect()` defaults to `PATIENCE_ASK` and only the pollers opt into 30s.
  'compact',
]);

/**
 * ⚠ **This list is written out, and there is nothing to derive it from.** Measured 2026-09-10: the
 * closest thing on the wire is which doors need the core's `Reviewer`, and that is a different set
 * — it groups doors by the engine capability they need, not by whether they generate. `open-file`
 * needs `Reviewer` and answers instantly (asked a running daemon; it returns before the round trip
 * is worth timing), and `meet` generates without needing it. So the list is judged door by door,
 * and `daemon.test.ts` pins the one claim that IS checkable: that the core still generates there.
 */

/**
 * How long to wait for one door's answer.
 *
 * Two numbers, because two different things go wrong when the wait is wrong. On the quick doors a
 * long deadline means a wedged daemon holds a poll for minutes; on the model doors a short one
 * turns a slow local model's CORRECT answer into a timeout — the JetBrains client's own words for
 * why its patience is two minutes: "짧게 잡으면 느린 로컬 모델의 정답이 시한 초과로 둔갑한다".
 *
 * This client had one number for everything (30s), and the cost of getting it wrong just went up:
 * a deadline now hangs up the connection, because a lock-step wire cannot be repaired once a reply
 * is late. So a slow completion did not merely lose its answer, it dropped the socket under
 * whatever else was in flight.
 */
export function deadlineFor(method: string): number {
  return THINKS.has(method) ? 120_000 : 30_000;
}
