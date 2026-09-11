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
 * two lines apart, while the owner was running and had closed nothing. This is review item R2
 * (docs/CLIENT_LIFECYCLE_REVIEW_2026-09-11), which had only a macOS stand-in until then.
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
  /** Let go on purpose: the daemon — and any successor it handed the read end to — reads EOF. */
  close(): void;
}

/** Today's channel, unchanged: Node makes the pipe and owns it. */
const nodes: OwnerChannel = { stdin: 'pipe', held: false, close() { /* child_process owns it */ } };

/**
 * A channel this window owns, or today's if this platform does not need one.
 *
 * ⚠ **POSIX deliberately keeps `'pipe'`.** The handover is what breaks, and POSIX has none:
 * `syscall.Exec` replaces the image and the PID never changes, so the child never exits and Node
 * never destroys anything. Swapping the channel there would be changing the one thing that stops
 * daemons leaking, on a platform with nothing to fix.
 *
 * A pipe that cannot be made falls back to today's, which is no worse than not trying: the name
 * carries this window's pid and eight random hex digits, so the realistic failure is the name being
 * taken by something impersonating it — and refusing to start the companion over that would hand
 * whoever did it the outage.
 */
export async function ownerChannel(key: string): Promise<OwnerChannel> {
  if (process.platform !== 'win32') return nodes;
  const name = "\\\\.\\pipe\\magi-owner-" +
    `${key}-${process.pid}-${randomBytes(4).toString('hex')}`;
  try {
    const server = net.createServer();
    // The window keeps the accepted side. The child gets the other one, and reads EOF when THIS
    // side goes — which is what "the window is gone" means.
    const ours = new Promise<net.Socket>((resolve) => server.once('connection', resolve));
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject);
      server.listen(name, () => resolve());
    });
    const theirs = await new Promise<net.Socket>((resolve, reject) => {
      const s = net.connect(name, () => resolve(s));
      s.once('error', reject);
    });
    const kept = await ours;
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
      close() { kept.destroy(); theirs.destroy(); },
    };
  } catch { return nodes; }
}
