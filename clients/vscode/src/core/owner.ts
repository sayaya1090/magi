import { randomBytes } from 'crypto';
import * as net from 'net';

/**
 * The owner channel: the pipe whose EOF tells a client-owned daemon that its window is gone.
 *
 * docs/CLIENT_LIFECYCLE §4 states the requirement in one line — the IDE **exclusively owns** the
 * write end of the child's stdin pipe and hands it to no other child. Node cannot keep that promise
 * with `stdio: 'pipe'`: `child_process` owns what it makes, and `ChildProcess` destroys `child.stdin`
 * the moment the child exits. The write end's lifetime is then the CHILD's, not the window's.
 *
 * ⚠ **On Windows that turns an update into a kill.** There is no execve, so the daemon's own restart
 * spawns a successor carrying the same read end and ends this process
 * (`internal/graceful/graceful_windows.go`). Node sees the child exit, destroys the write end, and the
 * successor — which has done nothing wrong and whose window is still open — reads EOF and stops
 * itself. Measured on Windows 11, 2026-09-11, on the real binary: the daemon log read
 *
 *     magi: daemon on ...sock (session s_ddbd...) — attach with `magi --attach` in this directory
 *     magi: daemon on ...sock stopped — the owner closed its pipe
 *
 * two lines apart, while the owner was running and had closed nothing. Fixed in commit 07358567
 * (docs/CLIENT_LIFECYCLE.md §4), which had only a macOS stand-in until then.
 *
 * So the window makes the pipe itself and hands the child a stream Node did not create: `child.stdin`
 * is then null, the destroy-on-exit path never runs, and the write end lives exactly as long as this
 * extension host does — killed hosts included, because the kernel closes it either way.
 */
export interface OwnerChannel {
  /** What to give the child as its stdin. */
  readonly stdin: net.Socket | 'pipe';
  /** Whether this is the window's own pipe rather than one `child_process` will destroy. */
  readonly held: boolean;
  /**
   * Why it is not held, when it is not — the step that failed and what it said.
   *
   * ⚠ **Falling back is allowed; falling back quietly is not.** docs/CLIENT_LIFECYCLE §4 says it in
   * the sentence after the one that permits the weaker lifetime, and R5 already built the shape for
   * saying so once per start. Without this the four ways to fail — create, listen, dial, accept —
   * all left the same answer and no trace, on the one platform where the fallback costs the
   * companion its life on the next update.
   */
  readonly why: string;
  /** Let go on purpose: the daemon — and any successor it handed the read end to — reads EOF. */
  close(): void;
}

/**
 * Today's channel, unchanged: Node makes the pipe and owns it — and destroys it when the child
 * exits, which is the lifetime R2 is about. `why` carries how we ended up here.
 */
const nodes = (why: string): OwnerChannel =>
  ({ stdin: 'pipe', held: false, why, close() { /* child_process owns it */ } });

/** The name this window would serve. Its own function so a test can take it first. */
export function ownerPipeName(key: string): string {
  return "\\\\.\\pipe\\magi-owner-" + `${key}-${process.pid}-${randomBytes(4).toString('hex')}`;
}

/**
 * How long the accept may lag the dial.
 *
 * Both ends are in this process, so the gap is one event-loop turn in practice. It is bounded
 * anyway because the alternative is a promise nobody settles: the old code awaited the accept with
 * no deadline at all, and a window that never finishes starting has no symptom to report.
 */
const ACCEPT_MS = 5_000;

/** What a step said, without the stack — this ends up in a sentence a person reads. */
const said = (e: unknown): string => (e instanceof Error ? e.message : String(e));

/**
 * A channel this window owns, or today's if this platform does not need one.
 *
 * ⚠ **POSIX deliberately keeps `'pipe'`.** The handover is what breaks, and POSIX has none:
 * `syscall.Exec` replaces the image and the PID never changes, so the child never exits and Node
 * never destroys anything. Swapping the channel there would be changing the one thing that stops
 * daemons leaking, on a platform with nothing to fix.
 *
 * A pipe that cannot be made falls back to today's, and refusing to start the companion over that
 * would be worse: the name carries this window's pid and eight random hex digits, so the realistic
 * failure is somebody taking it first, and an outage handed to whoever did that is a worse answer
 * than a weaker lifetime.
 *
 * ⚠ **But the fallback lands on R2.** `'pipe'` is the lifetime where an update alone kills the
 * companion on Windows, so this is not a quiet downgrade to make: every failure carries the step
 * that failed in `why`, and `OwnedCompanion.unowned()` is what puts it in front of a person, once
 * per start, the way R5 does for a core that has no owned mode at all.
 */
export async function ownerChannel(key: string, name = ownerPipeName(key)): Promise<OwnerChannel> {
  if (process.platform !== 'win32') return nodes('POSIX replaces the image in place; there is no handover to survive');

  // ⚠ **Everything created is registered before the next step can throw.** The old shape created a
  // server, listened, and then dialled inside one `try` with a single bare `catch` — so a dial that
  // failed left the server LISTENING under this name for the life of the window, and the accept
  // promise pending for ever. A free instance left waiting is exactly what stopping the listener is
  // there to prevent (see below), so the failure path was undoing the safety the success path takes
  // care to arrange. Reported as issue #189 / review R8, confirmed in source here.
  const opened: Array<() => void> = [];
  const giveUp = (step: string, e: unknown) => {
    for (const shut of opened.reverse()) { try { shut(); } catch { /* nothing left to do about it */ } }
    return nodes(`${step}: ${said(e)}`);
  };

  let server: net.Server;
  try {
    server = net.createServer();
    opened.push(() => server.close());
  } catch (e) { return giveUp('could not make a pipe server', e); }

  // Registered before the listen, because the accept is what the dial below completes.
  const ours = new Promise<net.Socket>((resolve) => server.once('connection', resolve));

  try {
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject);
      server.listen(name, () => resolve());
    });
  } catch (e) { return giveUp(`could not listen on ${name}`, e); }

  let theirs: net.Socket;
  try {
    theirs = await new Promise<net.Socket>((resolve, reject) => {
      const s = net.connect(name, () => resolve(s));
      s.once('error', reject);
    });
    opened.push(() => theirs.destroy());
  } catch (e) { return giveUp('could not dial the pipe this window just opened', e); }

  // The dial succeeded, so the accept has either landed or is one turn away — but "one turn away"
  // is not a promise, and a wait with no deadline is a window that never finishes starting.
  let kept: net.Socket;
  try {
    kept = await Promise.race([
      ours,
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error(`the dial connected but no accept arrived within ${ACCEPT_MS}ms`)), ACCEPT_MS).unref()),
    ]);
    opened.push(() => kept.destroy());
  } catch (e) { return giveUp('could not take the window side of the pipe', e); }

  // ⚠ **Stop listening the moment we have our one connection.** The daemon is handed a HANDLE, not
  // an address — it never learns this name, and neither does its successor, which inherits the
  // handle exactly as it does today. A free instance left waiting would be the one thing that
  // turns a pipe nobody can guess into an address anybody on this machine can dial, which is
  // precisely the cost docs/CLIENT_LIFECYCLE §2.5 records against the "listen and let the core
  // attach" option. The NAME stays visible while the connected instance lives — that is how
  // Windows lists pipes and it is not the question; `owner.test.ts` asks the question that is,
  // by dialling it.
  //
  // Connections already made are untouched, and the callback is deliberately not waited for: it
  // fires only once every connection has ended, which is the end of this window. `close()` stops
  // the listening socket synchronously, which is the part that matters.
  server.close();
  server.unref();
  return {
    stdin: theirs,
    held: true,
    why: '',
    close() { kept.destroy(); theirs.destroy(); },
  };
}
