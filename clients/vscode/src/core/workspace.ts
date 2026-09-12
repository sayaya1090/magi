import * as os from 'os';
import * as path from 'path';
import * as fs from 'fs';

/**
 * A workspace's name in one short, stable string — the same one the core computes.
 *
 * Getting this wrong produces no error anywhere. This side looks for a socket nobody is on, says
 * "not running", and offers to start a second companion on a tree that already has one. Both sides
 * behave; they are just not talking about the same workspace.
 *
 * The rule is `internal/adapter/daemon/publish.go`'s, symlink resolution included, and it is
 * pinned by goldens taken from that Go function rather than worked out here.
 */
export function workspaceKey(workdir: string): string {
  let abs: string;
  try {
    abs = path.resolve(workdir);
  } catch {
    abs = workdir;
  }
  // Symlinks resolved, or one directory gets two names. macOS makes this the ordinary case rather
  // than an exotic one: /tmp is a link to /private/tmp, so a shell that cd'd to the logical path
  // and a process that resolved it would hash two different strings for one directory.
  try {
    abs = fs.realpathSync(abs);
  } catch {
    // Not there is not a reason to refuse: the key is a name, not a claim that the directory
    // exists. The core does the same — a failed symlink walk leaves the path as it was.
  }
  // Cased once, and everything below reads the cased string: the name carries the base directory
  // and the hash carries the whole path, and a key whose two halves disagreed about the spelling
  // would be a third answer again — a workspace opened at a drive root is exactly where that shows.
  abs = driveCased(abs);
  return sanitize(baseName(abs)) + '-' + shortHash(abs);
}

/**
 * The drive letter, spelled the way the core spells it.
 *
 * ⚠ **VS Code hands this extension a lowercase drive letter, and the hash is over the string.**
 * `Uri.fsPath` answers `c:\Users\…` where every other path on the machine reads `C:\Users\…`, and
 * neither `path.resolve` nor `fs.realpathSync` changes it — Node keeps the case it was given.
 * Go's `filepath.EvalSymlinks`, which the core uses, canonicalises it to the uppercase form. So the
 * two sides hashed two different strings for one directory: measured 2026-09-12 in a real VS Code
 * on Windows, the window looked for `daemon-magi-dwj5mk5h.sock` while its daemon was on
 * `daemon-magi-x7wu42uu.sock`.
 *
 * That failure is silent by construction and it is the one this whole function exists to avoid:
 * the window finds no socket, says "not running" about a workspace that has a companion, and offers
 * to start a second one on it. Only a real editor produces the lowercase spelling — a unit test
 * passing its own string gets the uppercase one out of `path.resolve` and agrees with the core.
 */
function driveCased(p: string): string {
  return /^[a-z]:/.test(p) ? p[0].toUpperCase() + p.slice(1) : p;
}

/**
 * Go's `filepath.Base`, which is not Node's `path.basename`.
 *
 * They agree on ordinary paths and part ways at the root: Go answers "/" and Node answers "". The
 * key for the root then loses a character, and this is not a curiosity — a workspace opened at a
 * volume root is a real thing on Windows, and `/` is where a container often puts its checkout.
 * The golden for "/" is "--ov1j1jmu": two dashes, because sanitize turns Go's "/" into one.
 */
function baseName(p: string): string {
  const b = path.basename(p);
  if (b) return b;
  return p.startsWith(path.sep) ? path.sep : p;
}

/** Anything outside [A-Za-z0-9_-] becomes '-'. */
function sanitize(s: string): string {
  let out = '';
  for (const ch of s) out += /[A-Za-z0-9_-]/.test(ch) ? ch : '-';
  return out;
}

/**
 * The core's hash: FNV-1a in shape, base-36 in eight digits, least significant first.
 *
 * ⚠ **The starting value is NOT the textbook FNV-1a offset basis, and must not be "corrected".**
 * The standard is 14695981039346656037; the core uses 1469598103934665603 — the same digits with
 * one dropped. Whether that was meant does not matter here. It is what the daemon computes, so it
 * is what the socket is named, so it is what this must compute. Change it to the textbook value
 * and every client silently stops finding its companion: no error, just "not running" on a
 * workspace that has one.
 *
 * This port had it right-by-the-book and wrong-by-the-daemon on the first try. The goldens caught
 * it, which is the entire reason they are taken from the Go function rather than reasoned out.
 *
 * BigInt rather than Number: the core multiplies in 64 bits and lets it wrap, and a double loses
 * the low bits of that product after a few characters — agreeing on short paths and disagreeing on
 * real ones. The bytes are UTF-8, because that is what Go indexes when it walks a string.
 */
function shortHash(s: string): string {
  const MASK = (1n << 64n) - 1n;
  let h = 1469598103934665603n;
  for (const b of Buffer.from(s, 'utf8')) {
    h = (h ^ BigInt(b)) & MASK;
    h = (h * 1099511628211n) & MASK;
  }
  const digits = '0123456789abcdefghijklmnopqrstuvwxyz';
  let out = '';
  for (let i = 0; i < 8; i++) {
    out += digits[Number(h % 36n)];
    h /= 36n;
  }
  return out;
}

/** Where magi keeps its config — the core's three-way rule, so nobody says it twice. */
export function configDir(env: NodeJS.ProcessEnv = process.env, platform: string = process.platform,
                          home: string = os.homedir()): string {
  const set = (env.MAGI_CONFIG_DIR ?? '').trim();
  if (set) return set;
  if (platform === 'darwin') return path.join(home, 'Library', 'Application Support', 'magi');
  if (platform === 'win32') {
    const appData = (env.AppData ?? '').trim();
    return path.join(appData || path.join(home, 'AppData', 'Roaming'), 'magi');
  }
  const xdg = (env.XDG_CONFIG_HOME ?? '').trim();
  return path.join(xdg || path.join(home, '.config'), 'magi');
}

/**
 * Where the sockets live. Separate from the config directory because a unix address holds about
 * 100 bytes and %AppData%\magi under a long user name is past it — so the core lets the sockets
 * move without moving the settings.
 */
export function socketDir(env: NodeJS.ProcessEnv = process.env, cfg: string = configDir(env)): string {
  return (env.MAGI_SOCKET_DIR ?? '').trim() || cfg;
}

/** The socket this workspace's companion listens on. */
export function socketPath(workdir: string, env: NodeJS.ProcessEnv = process.env): string {
  return path.join(socketDir(env), 'daemon-' + workspaceKey(workdir) + '.sock');
}

/**
 * Is a socket file there — asked in the one way Windows will answer.
 *
 * ⚠ **`fs.existsSync` says NO about a live socket on Windows.** It is `stat` underneath, and
 * Windows refuses to stat an AF_UNIX socket file: measured 2026-09-12 against a running daemon,
 * `existsSync` → `false`, `statSync` and `lstatSync` → `EACCES`, while `accessSync(F_OK)` → ok and
 * `readdir` lists the name. So the check every discovery path used could not see any companion on
 * this platform at all.
 *
 * What that cost: `ide/workspace.ts` draws "not running" from a missing socket file — a fact, and
 * the one place it is allowed to conclude that without asking. On Windows the answer was always
 * "missing", so a window never dialled, always said the workspace had no companion, and offered to
 * start a second one on a tree that had one. That is exactly the failure workspaceKey's comment
 * above describes, arrived at from the other side.
 *
 * `access(F_OK)` asks the directory entry rather than the file's attributes, which is the question
 * being asked anyway: is the name there. A socket whose owner is gone leaves the file behind too,
 * and telling those apart is the dial's job, not this one's (CLIENT_LIFECYCLE §4.2 — never judge by
 * the file alone).
 */
export function socketThere(p: string): boolean {
  try {
    fs.accessSync(p, fs.constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

export const MAX_SOCKET_PATH = 100;

/** What the OS will refuse, with the reason it will not give: past the limit it says "invalid argument". */
export function tooLong(p: string): string | null {
  const n = Buffer.byteLength(p, 'utf8');
  if (n <= MAX_SOCKET_PATH) return null;
  // ⚠ MAGI_SOCKET_DIR, not MAGI_CONFIG_DIR. This said the latter, copied from the core, which said
  // it too — and it is the advice that caused the incident MAGI_SOCKET_DIR exists because of: the
  // Office installer moved the whole config tree somewhere short, and those companions then read a
  // config.toml the person's usual magi had never written. Moving the sockets alone fixes the
  // length and leaves one config tree for the account.
  return `the socket path is ${n} bytes and the OS allows about ${MAX_SOCKET_PATH} — ` +
    `set MAGI_SOCKET_DIR to somewhere shorter: ${p}`;
}
