import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { workspaceKey, configDir, socketDir, socketPath, socketThere, tooLong } from '../core/workspace';

/**
 * The keys below were printed by the CORE's own `daemon.WorkspaceKey`, not worked out here and not
 * copied from the Kotlin port. That matters more here than anywhere else in this extension: a key
 * that disagrees with the daemon's produces no error at all — this side looks for a socket nobody
 * is on, says "not running", and offers to start a second companion on a tree that has one.
 *
 * ⚠ **Two tables, because an absolute path is not portable and this agreement has to hold on both
 * shapes.** The POSIX goldens below are absolute POSIX paths; on Windows `path.resolve('/tmp')`
 * is `C:\tmp`, so they key differently there and asserting them would be asserting about a path
 * the machine does not have. Windows was therefore the one platform where NOTHING checked that
 * this port agrees with the daemon — the acceptance environment the lifecycle work names first
 * (docs/CLIENT_LIFECYCLE §1, L02: spaces, Hangul, long paths must identify one workspace).
 *
 * ⚠ **Both tables use paths that exist on no machine, and that is the load-bearing part.** A path
 * that IS there resolves through its real name, so the answer stops being a function of the string:
 * `/tmp` is a link to `/private/tmp` on macOS and a real directory on Linux, and its key is
 * therefore different on the two. This table once held `/tmp`, `/etc`, `/var/log` and `/tmp/café`,
 * printed on a Mac — so `test-vscode` was red on every CI run from the day it landed, saying
 * `tmp-d6rgiolj !== tmp-pstqlaw1` about a port that was correct. A golden that is not the same on
 * two machines is not a golden. What these pin is the portable part — resolve, basename, sanitize
 * by rune, hash the bytes — which is where a port drifts.
 */
const posixGoldens: Record<string, string> = {
  '/magi-golden/repo': 'repo-58blxov5',
  '/magi-golden': 'magi-golden-8ubiljdj',
  // A space, the ordinary case L02 names.
  '/magi-golden/with space': 'with-space-b5xwsad5',
  // The root. basename("/") is "/", which sanitizes to "-", so the name opens with two dashes —
  // the case a port written around "the basename will be empty" gets wrong. The one existing path
  // here, and the one whose resolution is the same everywhere.
  '/': '--ov1j1jmu',
  // Non-ASCII, and not decoration. This user's paths are Korean, and latin1-vs-utf8 is invisible
  // on every ASCII golden above — a port that hashed the wrong bytes would pass all of them and
  // fail on the first real workspace. Note the name: sanitize walks RUNES, so three Hangul
  // syllables become three dashes, not nine.
  '/프로젝트/마기': '---qeqa8hvh',
  '/magi-golden/프로젝트': '-----op09n61k',
  '/magi-golden/café': 'caf--tdiwwzm6',
  '/magi-golden/日本語': '----bpwhz3ji',
  '/nowhere/日本語': '----5xmmh9oj',
};

const windowsGoldens: Record<string, string> = {
  "C:\\magi-golden\\repo": "repo-ebbv3uo1",
  // A space in the path, which is the ordinary case on Windows and the one L02 names.
  "C:\\magi-golden\\with space": "with-space-o6qjctoi",
  "C:\\magi-golden\\프로젝트": "-----jsjof5n5",
  "C:\\magi-golden\\café": "caf--4o431cvu",
  // A different volume is a different path, not a different rule.
  "D:\\magi-golden\\repo": "repo-7kb5z9h9",
  // ⚠ No UNC share here, though one is an ordinary Windows workspace. `\\server\share`
  // is resolved over the network: the row cost 4.7s on a machine with no such server, and on one
  // where the name DOES resolve realpath succeeds and the key changes — a golden that is not the
  // same on two machines is not a golden. The agreement on UNC paths was measured by hand
  // (2026-09-11, Go and this port both `magi-golden-zscunmhr`) and wants a test that can reach a
  // share, not a constant.
};

test('the workspace key is the one the core computes', () => {
  const golden = process.platform === 'win32' ? windowsGoldens : posixGoldens;
  for (const [dir, want] of Object.entries(golden)) {
    assert.equal(workspaceKey(dir), want, `${dir} must key the same as the daemon`);
  }
});

/**
 * ⚠ **The drive letter is part of the string being hashed, and VS Code spells it lowercase.**
 *
 * `Uri.fsPath` answers `c:\Users\…` where the rest of the machine reads `C:\Users\…`, and Node keeps
 * whatever case it was given — `path.resolve` and `fs.realpathSync` both. Go's `EvalSymlinks`, which
 * the core uses, canonicalises to the uppercase form. So the window hashed one string and its daemon
 * another, for one directory.
 *
 * Measured in a real VS Code on Windows 11 (2026-09-12): the live self-check failed with `no daemon
 * socket at the path this window computes: …\daemon-magi-dwj5mk5h.sock` while the daemon for that
 * very workspace was on `…\daemon-magi-x7wu42uu.sock`. Nothing above could see it — a test that
 * writes its own path gets the uppercase form out of `path.resolve` and agrees with the core. Only
 * the editor produces the other spelling.
 */
test('a lowercase drive letter is the same workspace', (t) => {
  if (process.platform !== 'win32') {
    t.skip('drive letters are a Windows spelling');
    return;
  }
  for (const dir of Object.keys(windowsGoldens)) {
    if (!/^[A-Z]:/.test(dir)) continue;
    const lower = dir[0].toLowerCase() + dir.slice(1);
    assert.equal(workspaceKey(lower), workspaceKey(dir),
      `${lower} and ${dir} are one directory; two keys means the window starts a second companion`);
  }
});

test('symlinks are resolved, which on macOS is the ordinary case', (t) => {
  // /tmp is a link to /private/tmp. If this port skipped the resolution the two would hash
  // differently and the goldens above would not match — so this asserts the same fact from the
  // other side, in case someone "fixes" the golden instead of the code.
  //
  // Skipped where those two paths are not the same directory — asked of the filesystem rather than
  // of the platform name. On Windows neither is a path at all; on Linux `/tmp` is a real directory
  // and `/private/tmp` is nothing, so the two keys differ for a reason that has nothing to do with
  // symlink resolution. Naming platforms here is how this test came to fail on CI while being right.
  if (!samePlace('/tmp', '/private/tmp')) {
    t.skip('/tmp and /private/tmp are not one directory here — the symlink rule is asserted by the goldens');
    return;
  }
  assert.equal(workspaceKey('/tmp'), workspaceKey('/private/tmp'));
});

/**
 * Symlink resolution, pinned on **a link this test makes** rather than on one the machine happens to
 * have.
 *
 * ⚠ **The goldens above cannot carry this.** They are paths that exist nowhere, so `realpathSync`
 * fails on every one of them and a port that skipped resolution entirely would still match all of
 * them — measured: dropping the resolution left the whole table green. `/tmp` carried it before, and
 * that is exactly why it was in the table and why the table was red on Linux. Made here, the fact is
 * the same on every machine that can make a link.
 */
test('a directory reached through a link keys the same as the directory', (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-link-'));
  const real = path.join(root, 'real');
  const link = path.join(root, 'link');
  fs.mkdirSync(real);
  try { fs.symlinkSync(real, link, 'dir'); } catch {
    t.skip('this machine will not make a symlink (Windows without the privilege)');
    return;
  }
  try {
    assert.equal(workspaceKey(link), workspaceKey(real),
      'one directory got two keys — the daemon and the extension would look at different sockets');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

/** Whether two paths name one directory on this machine. Answered by the filesystem, not guessed. */
function samePlace(a: string, b: string): boolean {
  try { return fs.realpathSync(a) === fs.realpathSync(b); } catch { return false; }
}

test('a path that is not there still gets a name', () => {
  assert.match(workspaceKey('/nowhere-at-all-1234/deep'), /^deep-[0-9a-z]{8}$/);
});

test('two checkouts of one repo are two companions', () => {
  assert.notEqual(workspaceKey('/a/magi'), workspaceKey('/b/magi'));
});

test('the config directory follows the core three-way rule', () => {
  // path.join, not a "/" spelled into the expectation: these functions return NATIVE paths, so on
  // Windows the darwin and linux answers come back with backslashes and a literal string would be
  // asserting about this machine's separator rather than about the rule. The rule is which base
  // wins and what is appended to it.
  assert.equal(configDir({ MAGI_CONFIG_DIR: '/short' }, 'darwin', '/h'), '/short');
  assert.equal(configDir({}, 'darwin', '/h'), path.join('/h', 'Library', 'Application Support', 'magi'));
  assert.equal(configDir({}, 'linux', '/h'), path.join('/h', '.config', 'magi'));
  assert.equal(configDir({ XDG_CONFIG_HOME: '/x' }, 'linux', '/h'), path.join('/x', 'magi'));
  assert.equal(configDir({ AppData: 'C:\A' }, 'win32', 'C:\h'), path.join('C:\A', 'magi'));
});

test('the sockets can move without the settings', () => {
  assert.equal(socketDir({ MAGI_SOCKET_DIR: '/s' }, '/cfg'), '/s');
  assert.equal(socketDir({}, '/cfg'), '/cfg');
});

test('the socket path is the directory plus the key', () => {
  // Built the same way the function builds it — the key from the core's own rule, the separator
  // from this platform. Spelling the whole answer as a POSIX string asserted that the separator is
  // "/" , which is a fact about the machine and not about the path.
  const key = workspaceKey('/tmp');
  assert.equal(socketPath('/tmp', { MAGI_SOCKET_DIR: '/s' }), path.join('/s', `daemon-${key}.sock`));
  // And the key is really in it, so a join that dropped it would not pass.
  assert.ok(socketPath('/tmp', { MAGI_SOCKET_DIR: '/s' }).includes(key));
});

test('a path the OS will refuse says so, with the length and the way out', () => {
  assert.equal(tooLong('/s/daemon-x.sock'), null);
  const said = tooLong('/' + 'x'.repeat(120) + '.sock');
  // ★ MAGI_SOCKET_DIR, not MAGI_CONFIG_DIR. This assertion pinned the wrong one — and pinning it
  // is what kept the wrong advice alive: moving the whole config tree somewhere short is the thing
  // that caused the incident MAGI_SOCKET_DIR exists because of (the Office companions then read a
  // config.toml the person's usual magi never wrote).
  assert.ok(said?.includes('126'), `the length is not named: ${said}`);
  assert.ok(said?.includes('MAGI_SOCKET_DIR'), `the way out is not named: ${said}`);
  assert.ok(!said?.includes('MAGI_CONFIG_DIR'), `it still sends people to the config tree: ${said}`);
});

/**
 * An older reading must not overwrite a newer one.
 *
 * Two askers write the same screen: the poll, which chains itself, and `refresh()`, fired by a
 * command that just changed something. The wire does not promise they come back in the order they
 * were asked — different connections, and one may be waiting on a slow daemon. So the poll's
 * in-flight reading, asked BEFORE the change, can land after the refresh and put the old value
 * back: the "the change did not work" that `refresh` exists to prevent.
 *
 * The JetBrains client guards the same overlap with a sequence checked before drawing, in its own
 * words: "더 새 틱이 이미 섰다 — 낡은 그림 금지".
 */
test('a status answer that a newer one overtook is not drawn', () => {
  const src = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'workspace.ts'), 'utf8');

  // Both askers take a number and both check it. One without the other is the defect with a note.
  const takes = [...src.matchAll(/\+\+this\.asked/g)].length;
  assert.equal(takes, 2, `${takes} askers take a sequence — the poll and refresh must both take one`);
  const checks = [...src.matchAll(/mine !== this\.asked/g)].length;
  assert.equal(checks, 2, `${checks} askers check it — one that takes a number and ignores it guards nothing`);

  // The check must sit BETWEEN the await and the write, or it guards nothing.
  for (const m of src.matchAll(/const mine = \+\+this\.asked;([\s\S]{0,400}?)this\.set\(/g)) {
    const between = m[1];
    assert.ok(/await this\.ask\('status'/.test(between), 'the number is taken but nothing is awaited after it');
    assert.ok(/mine !== this\.asked[\s\S]*?return/.test(between),
      'the answer is written without asking whether a newer reading already landed');
  }
});

/**
 * ⚠ **`fs.existsSync` answers NO about a live socket on Windows**, which is what every discovery
 * path used to ask.
 *
 * It is `stat` underneath and Windows refuses to stat an AF_UNIX socket file. Measured 2026-09-12
 * against a running daemon: `existsSync` false, `statSync`/`lstatSync` EACCES, `accessSync(F_OK)`
 * ok, `readdir` lists the name. So a window on this platform saw no companion ever — it drew "not
 * running" without dialling and offered to start a second one on a tree that had one.
 *
 * Measured here against a socket this test binds, not against a platform name: the question is what
 * the filesystem answers, and a test that branched on `process.platform` would be asserting the
 * belief rather than the behaviour.
 */
test('a socket that is there is found', async (t) => {
  const net = await import('net');
  // Somewhere the platform will let AF_UNIX bind — and on Windows there is nowhere, because Node
  // maps `listen(path)` to a NAMED PIPE there and a filesystem path is not a pipe name (EACCES).
  // So this half runs on POSIX, and the Windows half is carried by the live self-check, which asks
  // the same helper about a socket a real daemon made (src/live/selfcheck.ts).
  const roots = [os.tmpdir(), os.homedir(), process.cwd()];
  let p = '';
  let server: import('net').Server | null = null;
  for (const root of roots) {
    const dir = fs.mkdtempSync(path.join(root, 'magi-sockcheck-'));
    const candidate = path.join(dir, 's.sock');
    try {
      server = await new Promise<import('net').Server>((ok, no) => {
        const s = net.createServer();
        s.on('error', no);
        s.listen(candidate, () => ok(s));
      });
      p = candidate;
      break;
    } catch {
      try { fs.rmSync(dir, { recursive: true, force: true }); } catch { /* nothing to clean */ }
    }
  }
  if (!server) {
    t.skip('nothing here accepts an AF_UNIX bind (on Windows Node listens on named pipes) — the live self-check carries this');
    return;
  }
  try {
    assert.equal(socketThere(p).there, true, 'a bound socket must be found, or the window never dials');
  } finally {
    await new Promise<void>((ok) => server!.close(() => ok()));
    try { fs.rmSync(path.dirname(p), { recursive: true, force: true }); } catch { /* gone already */ }
  }
});

/** And a name nothing made is still absent — the other half, or "found" would mean nothing. */
test('a socket that is not there is not found', () => {
  const look = socketThere(path.join(os.tmpdir(), 'magi-nothing-here-' + Date.now() + '.sock'));
  assert.equal(look.there, false);
  assert.equal(look.why, undefined, 'a plain absence must not arrive with a reason — that is the OTHER answer');
});

/**
 * **"Nothing is there" and "I could not look" are different answers, and one of them is not a fact.**
 *
 * ⚠ The helper folded every error into `false`, so a refused directory or an I/O error read as an
 * empty workspace — and the caller draws a CONCLUSION from that: it says "not running" about a tree
 * it never managed to see, and offers to start a second companion on it. This is the same confusion
 * one state over that `core/activity` was written for: *"Unknown is a real answer and not a shrug."*
 *
 * Measured against a directory this test makes unreadable, not against a platform name — the question
 * is what the filesystem answers. Root can read anything, so the case only exists for a normal user.
 */
test('a path that cannot be looked at is not reported as an empty workspace', (t) => {
  if (typeof process.getuid === 'function' && process.getuid() === 0) {
    t.skip('root can read a directory with no permissions — there is no refusal to measure here');
    return;
  }
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-noaccess-'));
  const inner = path.join(dir, 'inner');
  fs.mkdirSync(inner);
  const p = path.join(inner, 'd.sock');
  try {
    fs.chmodSync(inner, 0o000);
    // ⚠ **`chmod` 는 플랫폼마다 같은 일을 하지 않는다.** 윈도우에서 Node 는 쓰기 비트만 바꾸므로
    // (Node 문서: "only the write permission bit"), 이 디렉터리는 여전히 읽힌다 — 그러면 없는
    // `d.sock` 조회가 ENOENT 가 되어 이 시험은 「사유가 없다」로 실패한다. 그것은 결함이 아니라
    // **이 시험이 잴 것이 없다는 뜻**이다.
    //
    // 그래서 플랫폼 이름을 묻지 않고 **파일시스템에 실제로 물어본다**(이 파일의 다른 건너뛰기들과
    // 같은 방식이다): 거절이 정말 났는지 확인하고, 안 났으면 사유를 적고 건너뛴다. 윈도우의 ACL 로
    // 같은 상황을 만드는 것은 따로 할 일이다.
    let refused = false;
    try { fs.readdirSync(inner); } catch { refused = true; }
    if (!refused) {
      t.skip('this filesystem still lets us in after chmod 000 (on Windows Node changes only the write bit) '
        + '— the refusal case needs an ACL-based test');
      return;
    }
    const look = socketThere(p);
    assert.equal(look.there, false, 'it claimed to have found a socket it could not look for');
    assert.ok(look.why, 'a refused directory came back as a plain absence — the window then states a '
      + 'fact about a companion out of a failure to look');
    assert.match(look.why!, /cannot tell/, 'the reason does not say what happened');
  } finally {
    try { fs.chmodSync(inner, 0o700); } catch { /* nothing to restore */ }
    try { fs.rmSync(dir, { recursive: true, force: true }); } catch { /* gone already */ }
  }
});

/**
 * And the callers act on the difference. A source scan, because the branch is inside a poll loop and
 * a class that needs a live socket and a webview to instantiate — what must be pinned is that the
 * two answers reach two different states, which is exactly what a scan can see.
 */
test('the discovery paths draw unknown, not not-running, from a failure to look', () => {
  const code = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'workspace.ts'), 'utf8');
  const arms = code.match(/if \(!look\.there\) \{[\s\S]*?\n {4,6}\}/g) ?? [];
  assert.ok(arms.length >= 2, `expected both discovery paths to branch on the look, found ${arms.length}`);
  for (const arm of arms) {
    assert.match(arm, /look\.why/,
      'this path reports "not running" whatever the reason — a refused directory then reads as an empty workspace');
    assert.match(arm, /State\.Unknown/, 'the failure to look does not reach the unknown state');
  }
});

/**
 * **And nobody asks that question the old way again.**
 *
 * ⚠ The fix above is one helper, and the defect was that the WRONG question was asked in several
 * places. A helper does not stop the next discovery path from reaching for `fs.existsSync` — it is
 * the obvious call, it reads correctly, and on POSIX it even works. The failure only appears on the
 * platform nobody develops on, and it appears as "this workspace has no companion", which reads like
 * a fact rather than a bug.
 *
 * So the files that decide whether a companion is THERE must ask through the helper and must not
 * stat. Comments are stripped first: the files explain the trap in prose, and a scan that matched
 * prose would fail on the very explanation that keeps the rule legible.
 */
test('the discovery paths ask about a socket only through the helper', () => {
  const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
  const askers = ['ide/workspace.ts', 'live/selfcheck.ts'];
  for (const rel of askers) {
    const code = strip(fs.readFileSync(path.join(__dirname, '..', '..', 'src', rel), 'utf8'));
    assert.ok(code.includes('socketThere'),
      `${rel} decides whether a companion is there without the helper — on Windows stat says no about a live socket`);
    for (const wrong of ['existsSync', 'statSync', 'lstatSync']) {
      assert.ok(!code.includes(wrong),
        `${rel} asks the filesystem with ${wrong} — Windows refuses that about an AF_UNIX socket, so the answer is always "no companion"`);
    }
  }
  // And the helper itself must not be built on the call it exists to replace.
  const helper = strip(fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'workspace.ts'), 'utf8'));
  assert.ok(/accessSync\(/.test(helper), 'the helper no longer asks the directory entry — the one question Windows answers');
  for (const wrong of ['existsSync', 'lstatSync']) {
    assert.ok(!helper.includes(wrong), `the helper fell back to ${wrong}`);
  }
});
