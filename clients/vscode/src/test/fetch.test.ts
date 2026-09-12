import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { execFileSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { CoreRelease, fetchOffer } from '../core/release';
import { cachedAt, fetchCore, findFile, resolveLatest, sha256, tarExe, unpack, Wire } from '../core/fetch';

/**
 * Fetching the core, measured **without a network and with real archives**.
 *
 * The wire is an interface with two methods, so a test implements the contract rather than mocking a
 * library. Everything else is real: a real tar.gz made by `tar`, a real sha256, a real extraction,
 * and the file really landing where the resolver will find it.
 */

/**
 * 이 파일시스템이 실행 비트를 들고 있는가 — 플랫폼 이름이 아니라 파일시스템에 묻는다.
 *
 * NTFS 에는 POSIX 의 실행 비트가 없다. `chmodSync(0o755)` 는 읽기 전용 플래그만 건드리고
 * `statSync().mode` 는 `0o111` 을 돌려주지 않는다 — 윈도우에서 실행 가능성을 정하는 것은 확장자다.
 * 그래서 「받은 것이 실행 가능한가」라는 단언은 그 비트가 붙는 곳에서만 뜻이 있다.
 */
const modeBitsStick = (() => {
  try {
    const f = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'magi-mode-')), 'x');
    fs.writeFileSync(f, 'x');
    fs.chmodSync(f, 0o755);
    return (fs.statSync(f).mode & 0o111) !== 0;
  } catch { return false; }
})();

const hasTar = (() => {
  try { execFileSync(tarExe(), ['--version'], { stdio: 'ignore' }); return true; } catch { return false; }
})();

/** A release archive, the shape the core actually publishes: one executable inside. */
function archive(dir: string, body = "#!/bin/sh\necho 'magi test'\n"): string {
  const stage = path.join(dir, 'stage');
  fs.mkdirSync(stage, { recursive: true });
  fs.writeFileSync(path.join(stage, 'magi'), body, { mode: 0o755 });
  fs.writeFileSync(path.join(stage, 'README.md'), 'not the binary\n');
  const out = path.join(dir, 'magi_linux_amd64.tar.gz');
  // The same tar the product will use — on Windows a bare `tar` may be Git's MSYS one, which cannot
  // even WRITE to a C: path (it reads the drive as a remote host). A fixture that failed there said
  // nothing about the product.
  execFileSync(tarExe(), ['-czf', out, '-C', stage, 'magi', 'README.md']);
  return out;
}

function conf(extra: Record<string, string> = {}): Record<string, string> {
  return {
    'core.version': '9.9.9',
    'core.asset': 'magi_{os}_{arch}.{ext}',
    'core.url': 'https://example.invalid/download/v{version}/{asset}',
    'core.checksums': 'https://example.invalid/download/v{version}/checksums.txt',
    ...extra,
  };
}

/** The download scratch directories that exist right now. */
function scratch(): string[] {
  return fs.readdirSync(os.tmpdir()).filter((n) => n.startsWith('magi-core-'));
}

/** A wire serving one archive and one checksums table, counting what was asked for. */
function served(file: string | null, sums: string): Wire & { texts: string[]; files: string[] } {
  const w = {
    texts: [] as string[],
    files: [] as string[],
    async text(url: string) { w.texts.push(url); return sums; },
    async file(url: string, dest: string) {
      w.files.push(url);
      if (!file) throw new Error('nothing to serve');
      fs.copyFileSync(file, dest);
    },
  };
  return w;
}

test('a verified archive lands where the resolver looks for it', { skip: !hasTar }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-fetch-'));
  const tgz = archive(dir);
  const sum = await sha256(tgz);
  const cfg = path.join(dir, 'config');
  const net = served(tgz, `${sum}  magi_linux_amd64.tar.gz\n`);
  const before = scratch();

  const got = await fetchCore(new CoreRelease(conf()), cfg, net, 'linux', 'x64');

  assert.equal(got, cachedAt(cfg, '9.9.9', 'linux'), 'it did not land in the per-version cache');
  assert.ok(fs.existsSync(got), 'nothing is there');
  // Only where the filesystem carries the bit — see modeBitsStick. The product chmods either way;
  // on NTFS that call is about the read-only flag and executability comes from the extension.
  if (modeBitsStick) assert.ok(fs.statSync(got).mode & 0o111, 'the fetched core is not executable');
  assert.equal(fs.readFileSync(got, 'utf8'), "#!/bin/sh\necho 'magi test'\n", 'the wrong file was taken out');
  // The checksums were asked for BEFORE the archive: nothing is fetched that nobody vouched for.
  assert.deepEqual(net.texts, ['https://example.invalid/download/v9.9.9/checksums.txt']);
  assert.deepEqual(net.files, ['https://example.invalid/download/v9.9.9/magi_linux_amd64.tar.gz']);
  // And the temporary directory is gone. Counted against a snapshot taken before, not against zero:
  // another test or another process may legitimately have one open, and a gate that depends on that
  // is a gate that fails for reasons nobody can act on.
  assert.deepEqual(scratch().filter((n) => !before.includes(n)), [],
    'a download directory was left behind — on a machine that keeps failing that fills the disk');
});

test('a checksum that does not match installs nothing', { skip: !hasTar }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-fetch-'));
  const tgz = archive(dir);
  const cfg = path.join(dir, 'config');
  const net = served(tgz, `${'0'.repeat(64)}  magi_linux_amd64.tar.gz\n`);

  await assert.rejects(fetchCore(new CoreRelease(conf()), cfg, net, 'linux', 'x64'),
    /does not match its published checksum/);
  assert.equal(fs.existsSync(cachedAt(cfg, '9.9.9', 'linux')), false,
    'a binary nobody vouched for was left on disk');
});

test('an asset nobody listed is refused before anything is fetched', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-fetch-'));
  const net = served(null, 'deadbeef  something_else.tar.gz\n');
  await assert.rejects(fetchCore(new CoreRelease(conf()), dir, net, 'linux', 'x64'),
    /not listed in checksums.txt/);
  assert.deepEqual(net.files, [], 'it downloaded first and asked afterwards');
});

test('checksums from a different host are not a check', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-fetch-'));
  const net = served(null, '');
  const split = conf({ 'core.checksums': 'https://somewhere.else.invalid/checksums.txt' });
  await assert.rejects(fetchCore(new CoreRelease(split), dir, net, 'linux', 'x64'), /that is not a check/);
  assert.deepEqual(net.texts, [], 'it fetched a table it had already decided not to trust');
});

test('a machine with no build is told that, not a network error', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-fetch-'));
  const net = served(null, '');
  await assert.rejects(fetchCore(new CoreRelease(conf()), dir, net, 'sunos', 'x64'), /no magi build for sunos/);
  assert.deepEqual(net.texts, []);
});

test('a build that could not read its configuration says so about itself', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-fetch-'));
  await assert.rejects(fetchCore(new CoreRelease({}), dir, served(null, ''), 'linux', 'x64'),
    /does not say where to fetch/);
});

test('turning verification off skips the table rather than pretending to check', { skip: !hasTar }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-fetch-'));
  const tgz = archive(dir);
  const cfg = path.join(dir, 'config');
  const net = served(tgz, 'this table would not match anything');
  const got = await fetchCore(new CoreRelease(conf({ 'core.insecure': 'true' })), cfg, net, 'linux', 'x64');
  assert.ok(fs.existsSync(got));
  assert.deepEqual(net.texts, [], 'it fetched a checksums table it was not going to use');
});

test('the binary is found however the archive nests it', { skip: !hasTar }, () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-find-'));
  fs.mkdirSync(path.join(dir, 'a', 'b'), { recursive: true });
  fs.writeFileSync(path.join(dir, 'a', 'b', 'magi'), 'x');
  assert.equal(findFile(dir, 'magi'), path.join(dir, 'a', 'b', 'magi'));
  assert.equal(findFile(dir, 'magi.exe'), null);
});

test('a corrupt archive fails at unpacking rather than landing', { skip: !hasTar }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-fetch-'));
  const bad = path.join(dir, 'magi_linux_amd64.tar.gz');
  fs.writeFileSync(bad, 'not a gzip stream at all');
  await assert.rejects(unpack(bad, dir), /could not unpack/);
});

/** Answers a scripted body per URL, and counts what was asked. */
function answering(bodies: Record<string, string | Error>): Wire & { asked: string[] } {
  const w = {
    asked: [] as string[],
    async text(url: string) {
      w.asked.push(url);
      const b = bodies[url];
      if (b === undefined) throw new Error(`nothing scripted for ${url}`);
      if (b instanceof Error) throw b;
      return b;
    },
    async file() { throw new Error('not used'); },
  };
  return w;
}

const tracking = conf({
  'core.track': 'latest',
  'core.latest': 'https://example.invalid/core-latest.txt',
  'core.releases': 'https://example.invalid/releases',
  'core.tag': '^v(\\d+(?:\\.\\d+)*)$',
});

test("the core's own version line is asked first", async () => {
  const net = answering({ 'https://example.invalid/core-latest.txt': 'v9.10.0\n' });
  const got = await resolveLatest(new CoreRelease(tracking), net);
  assert.equal(got.version, '9.10.0');
  assert.deepEqual(net.asked, ['https://example.invalid/core-latest.txt'],
    'the releases listing was asked as well — that is a rate limit spent for nothing');
});

test('a missing version line falls through to the listing, not to a failure', async () => {
  const net = answering({
    'https://example.invalid/core-latest.txt': new Error('404'),
    'https://example.invalid/releases': JSON.stringify([
      { tag_name: 'web-v9.9.9' }, { tag_name: 'v9.10.0' }, { tag_name: 'v9.2.0' },
    ]),
  });
  const got = await resolveLatest(new CoreRelease(tracking), net);
  assert.equal(got.version, '9.10.0', 'either it gave up, or another release train won');
});

test('offline leaves the pinned floor rather than stopping the installation', async () => {
  const net = answering({
    'https://example.invalid/core-latest.txt': new Error('offline'),
    'https://example.invalid/releases': new Error('offline'),
  });
  const got = await resolveLatest(new CoreRelease(tracking), net);
  assert.equal(got.version, '9.9.9', 'a network outage took the whole installation with it');
});

test('a pinned configuration asks nothing at all', async () => {
  // ⚠ The addresses are present and only `core.track` says pinned. Asserting this against a
  // configuration with no addresses would pass for the wrong reason — there would be nothing to ask
  // either way, and removing the check entirely still looked right (measured).
  const net = answering({ 'https://example.invalid/core-latest.txt': 'v9.99.0\n' });
  const pinned = { ...tracking, 'core.track': 'pinned' };
  const got = await resolveLatest(new CoreRelease(pinned), net);
  assert.equal(got.version, '9.9.9', 'a pinned install moved itself onto the latest release');
  assert.deepEqual(net.asked, [], 'a pinned install went to the network anyway');
});

test('what a person is asked names the host and how it will be checked', () => {
  const offer = fetchOffer(new CoreRelease(conf()), 'linux', 'x64')!;
  assert.ok(offer.message.includes('example.invalid'), `the host is not in the question: ${offer.message}`);
  assert.ok(offer.message.includes('9.9.9'), offer.message);
  assert.ok(/SHA-256/.test(offer.detail), offer.detail);
});

test('with verification off, the question says what that costs', () => {
  const offer = fetchOffer(new CoreRelease(conf({ 'core.insecure': 'true' })), 'linux', 'x64')!;
  assert.ok(/NOT be verified/.test(offer.detail), `the dialog hid what insecure costs: ${offer.detail}`);
  assert.ok(/same connection/.test(offer.detail), offer.detail);
  assert.ok(!/SHA-256 will be checked/.test(offer.detail), 'it claims a check it is not doing');
});

test('there is nothing to ask when there is nothing to fetch', () => {
  assert.equal(fetchOffer(new CoreRelease({}), 'linux', 'x64'), null);
  assert.equal(fetchOffer(new CoreRelease(conf()), 'sunos', 'x64'), null);
});
