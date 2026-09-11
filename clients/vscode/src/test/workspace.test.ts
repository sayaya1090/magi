import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { workspaceKey, configDir, socketDir, socketPath, tooLong } from '../core/workspace';

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
 * The Windows goldens were printed the same way, by the core, for paths that do not exist on any
 * machine: a path that IS there resolves through its real name and the answer stops being a
 * function of the string alone. What they pin is the part that is portable — resolve, basename,
 * sanitize by rune, hash the bytes — which is where a port drifts.
 */
const posixGoldens: Record<string, string> = {
  '/tmp': 'tmp-pstqlaw1',
  '/usr': 'usr-a74ztgfv',
  '/Users': 'Users-cghuwblp',
  '/var/log': 'log-clkllm93',
  '/etc': 'etc-ydppqbth',
  // The root. basename("/") is "/", which sanitizes to "-", so the name opens with two dashes —
  // the case a port written around "the basename will be empty" gets wrong.
  '/': '--ov1j1jmu',
  // Non-ASCII, and not decoration. This user's paths are Korean, and latin1-vs-utf8 is invisible
  // on every ASCII golden above — a port that hashed the wrong bytes would pass all of them and
  // fail on the first real workspace. Note the name: sanitize walks RUNES, so three Hangul
  // syllables become three dashes, not nine.
  '/프로젝트/마기': '---qeqa8hvh',
  '/tmp/café': 'caf--4y7p2jz0',
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

test('symlinks are resolved, which on macOS is the ordinary case', (t) => {
  // /tmp is a link to /private/tmp. If this port skipped the resolution the two would hash
  // differently and the goldens above would not match — so this asserts the same fact from the
  // other side, in case someone "fixes" the golden instead of the code.
  //
  // Skipped where those two paths are not the same directory. On Windows they are not paths at
  // all: both resolve under the current volume and neither is there, so the two keys differ for a
  // reason that has nothing to do with symlink resolution.
  if (process.platform === 'win32') {
    t.skip('/tmp and /private/tmp are not this platform\'s paths — the symlink rule is asserted by the goldens');
    return;
  }
  assert.equal(workspaceKey('/tmp'), workspaceKey('/private/tmp'));
});

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
