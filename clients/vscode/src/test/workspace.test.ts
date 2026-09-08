import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as path from 'path';
import { workspaceKey, configDir, socketDir, socketPath, tooLong } from '../core/workspace';

/**
 * The keys below were printed by the CORE's own `daemon.WorkspaceKey`, not worked out here and not
 * copied from the Kotlin port. That matters more here than anywhere else in this extension: a key
 * that disagrees with the daemon's produces no error at all — this side looks for a socket nobody
 * is on, says "not running", and offers to start a second companion on a tree that has one.
 */
test('the workspace key is the one the core computes', () => {
  const golden: Record<string, string> = {
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
  for (const [dir, want] of Object.entries(golden)) {
    assert.equal(workspaceKey(dir), want, `${dir} must key the same as the daemon`);
  }
});

test('symlinks are resolved, which on macOS is the ordinary case', () => {
  // /tmp is a link to /private/tmp. If this port skipped the resolution the two would hash
  // differently and the goldens above would not match — so this asserts the same fact from the
  // other side, in case someone "fixes" the golden instead of the code.
  assert.equal(workspaceKey('/tmp'), workspaceKey('/private/tmp'));
});

test('a path that is not there still gets a name', () => {
  assert.match(workspaceKey('/nowhere-at-all-1234/deep'), /^deep-[0-9a-z]{8}$/);
});

test('two checkouts of one repo are two companions', () => {
  assert.notEqual(workspaceKey('/a/magi'), workspaceKey('/b/magi'));
});

test('the config directory follows the core three-way rule', () => {
  assert.equal(configDir({ MAGI_CONFIG_DIR: '/short' }, 'darwin', '/h'), '/short');
  assert.equal(configDir({}, 'darwin', '/h'), '/h/Library/Application Support/magi');
  assert.equal(configDir({}, 'linux', '/h'), '/h/.config/magi');
  assert.equal(configDir({ XDG_CONFIG_HOME: '/x' }, 'linux', '/h'), '/x/magi');
  assert.equal(configDir({ AppData: 'C:\\A' }, 'win32', 'C:\\h'), path.join('C:\\A', 'magi'));
});

test('the sockets can move without the settings', () => {
  assert.equal(socketDir({ MAGI_SOCKET_DIR: '/s' }, '/cfg'), '/s');
  assert.equal(socketDir({}, '/cfg'), '/cfg');
});

test('the socket path is the directory plus the key', () => {
  assert.equal(socketPath('/tmp', { MAGI_SOCKET_DIR: '/s' }), '/s/daemon-tmp-pstqlaw1.sock');
});

test('a path the OS will refuse says so, with the length and the way out', () => {
  assert.equal(tooLong('/s/daemon-x.sock'), null);
  const said = tooLong('/' + 'x'.repeat(120) + '.sock');
  assert.ok(said?.includes('126') && said.includes('MAGI_CONFIG_DIR'), `got: ${said}`);
});
