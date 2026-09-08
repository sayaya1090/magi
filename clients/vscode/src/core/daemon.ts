import * as net from 'net';
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
  private sock: net.Socket;
  private buf = '';
  private waiting: ((r: Response) => void)[] = [];
  private onFrame: ((r: Response) => void) | null = null;
  private closed = false;
  private onClose: (() => void)[] = [];

  private constructor(sock: net.Socket) {
    this.sock = sock;
    sock.setEncoding('utf8');
    sock.on('data', (chunk: string) => this.take(chunk));
    sock.on('close', () => this.finish());
    sock.on('error', () => this.finish());
  }

  static connect(path: string, connectMs = 5000): Promise<Daemon> {
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

  /** One question, one answer. */
  exchange(req: Request, deadlineMs = 30_000): Promise<Response> {
    return new Promise((resolve, reject) => {
      if (this.closed) return reject(new Error('the connection is closed'));
      const timer = setTimeout(() => {
        // Take this waiter out rather than leaving it to catch somebody else's reply: replies come
        // back in order, and a timed-out waiter left in the queue would hand the NEXT answer to the
        // wrong caller — a wrong answer is worse than a slow one.
        const i = this.waiting.indexOf(done);
        if (i >= 0) this.waiting.splice(i, 1);
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
