import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { execFileSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { CoreRelease } from '../core/release';
import { cachedAt, fetchCore, findFile, sha256, unpack, Wire } from '../core/fetch';

/**
 * Fetching the core, measured **without a network and with real archives**.
 *
 * The wire is an interface with two methods, so a test implements the contract rather than mocking a
 * library. Everything else is real: a real tar.gz made by `tar`, a real sha256, a real extraction,
 * and the file really landing where the resolver will find it.
 */

const hasTar = (() => {
  try { execFileSync('tar', ['--version'], { stdio: 'ignore' }); return true; } catch { return false; }
})();

/** A release archive, the shape the core actually publishes: one executable inside. */
function archive(dir: string, body = "#!/bin/sh\necho 'magi test'\n"): string {
  const stage = path.join(dir, 'stage');
  fs.mkdirSync(stage, { recursive: true });
  fs.writeFileSync(path.join(stage, 'magi'), body, { mode: 0o755 });
  fs.writeFileSync(path.join(stage, 'README.md'), 'not the binary\n');
  const out = path.join(dir, 'magi_linux_amd64.tar.gz');
  execFileSync('tar', ['-czf', out, '-C', stage, 'magi', 'README.md']);
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
  assert.ok(fs.statSync(got).mode & 0o111, 'the fetched core is not executable');
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
