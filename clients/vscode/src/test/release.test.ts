import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import { CoreRelease, checksums, contractFile, newer, pickLatest, shipped } from '../core/release';

/**
 * Where the core is fetched from — measured against **the file that actually ships**.
 *
 * ⚠ **The same file the JetBrains plugin reads** (`clients/contract/core-release.properties`, copied
 * into the plugin jar by its Gradle build). `CoreReleaseTest` over there asserts the same six asset
 * names against the same bytes. That is the point: a copy per editor drifts, and two editors quietly
 * fetching different cores is the kind of defect this tree has paid for before.
 */
const conf = shipped();

test('the shipped configuration is actually being read', () => {
  assert.ok(fs.existsSync(contractFile()), `${contractFile()} is missing — this suite is measuring nothing`);
  assert.ok(Object.keys(conf).length > 4, 'the properties file parsed to almost nothing');
});

test('the shipped configuration carries all four keys', () => {
  for (const k of ['core.version', 'core.asset', 'core.url', 'core.checksums']) {
    assert.ok((conf[k] ?? '').trim() !== '', `no "${k}" in the configuration — there is nowhere to fetch from`);
  }
});

test('these machines get the names the release actually publishes', () => {
  const r = new CoreRelease(conf);
  // The same six the JetBrains suite pins, measured against the v0.29.0 release.
  assert.equal(r.asset('darwin', 'arm64'), 'magi_darwin_arm64.tar.gz');
  assert.equal(r.asset('darwin', 'x64'), 'magi_darwin_amd64.tar.gz');
  assert.equal(r.asset('linux', 'arm64'), 'magi_linux_arm64.tar.gz');
  assert.equal(r.asset('linux', 'x64'), 'magi_linux_amd64.tar.gz');
  assert.equal(r.asset('win32', 'x64'), 'magi_windows_amd64.zip');
  assert.equal(r.asset('win32', 'arm64'), 'magi_windows_arm64.zip');
});

test('a machine we do not know says so instead of inventing a name', () => {
  const r = new CoreRelease(conf);
  assert.equal(r.asset('sunos', 'arm64'), null, 'fetching a build that does not exist reads as a network error');
  assert.equal(r.asset('linux', 'riscv64'), null);
  // And a name we could not finish filling in is an INVENTED one. Today's template has no such
  // placeholder, so only a changed configuration reaches this — which is exactly the person this
  // guard is for, and asserting it only against the shipped value would leave them unmeasured.
  const odd = new CoreRelease({ ...conf, 'core.asset': 'magi_{os}_{arch}_{flavour}.{ext}' });
  assert.equal(odd.asset('linux', 'x64'), null, 'a half-filled name ends as a quiet 404');
});

test('a url with a placeholder left in it is not handed out', () => {
  const r = new CoreRelease(conf);
  const u = r.url('magi_linux_amd64.tar.gz')!;
  assert.ok(u.startsWith('https://'), `the download url is not https: ${u}`);
  assert.ok(u.includes('magi_linux_amd64.tar.gz') && u.includes(conf['core.version']), u);
  const bare = { 'core.version': '1.0' };
  assert.equal(new CoreRelease({ ...bare, 'core.url': 'https://x/{who}' }).url('a'), null);
  // The scheme is a RULE, not a property of today's values: somebody swapping in a mirror is the
  // person this guard exists for, and they will not be running the assertion above.
  assert.equal(new CoreRelease({ ...bare, 'core.url': 'http://x/{asset}' }).url('a'), null);
  assert.equal(new CoreRelease({ 'core.url': 'https://x/{asset}' }).url('a'), null, 'an empty version is a quiet 404');
});

test('the checksums and the file have to come from one place', () => {
  const r = new CoreRelease(conf);
  assert.ok(r.sameOrigin('magi_linux_amd64.tar.gz'), 'the checksums arrive from somewhere else than the file');
  const split = new CoreRelease({
    'core.version': '1.0', 'core.url': 'https://a/{asset}', 'core.checksums': 'https://b/sums',
  });
  assert.equal(split.sameOrigin('x'), false);
});

test('a checksums table drops malformed lines rather than emptying itself', () => {
  const got = checksums([
    'a'.repeat(64) + '  magi_linux_amd64.tar.gz',
    'not a checksum line',
    'b'.repeat(64) + ' *magi_windows_amd64.zip',
    'c'.repeat(10) + '  too_short.tar.gz',
  ].join('\n'));
  assert.equal(got.get('magi_linux_amd64.tar.gz'), 'a'.repeat(64));
  assert.equal(got.get('magi_windows_amd64.zip'), 'b'.repeat(64), 'the leading * of binary mode was kept');
  assert.equal(got.size, 2, 'a malformed line took the whole table with it');
});

test('the newest release is picked by number, and only from this lane', () => {
  const r = new CoreRelease(conf);
  const json = JSON.stringify([
    { tag_name: 'web-v0.9.0' }, { tag_name: 'v0.9.0' }, { tag_name: 'v0.10.0' },
    { tag_name: 'jetbrains-v9.9.9' }, { tag_name: 'v0.2.0' },
  ]);
  assert.equal(pickLatest(json, r.tagPattern()), '0.10.0',
    'either another lane won, or 0.9 compared above 0.10 as a string');
  assert.equal(pickLatest('[]', r.tagPattern()), null);
});

test('version comparison is numeric', () => {
  assert.equal(newer('0.10.0', '0.9.0'), true);
  assert.equal(newer('0.9.0', '0.10.0'), false);
  assert.equal(newer('1.0', '1.0.0'), false);
  assert.equal(newer('1.0.1', '1.0'), true);
});

test("the core's own version line is read, and anything else is refused", () => {
  const r = new CoreRelease(conf);
  assert.equal(r.readLatest('v0.31.0\n'), '0.31.0');
  assert.equal(r.readLatest('0.31.0'), '0.31.0');
  // A 404 page starts with text, and "starts with a digit" would let plenty of it through.
  assert.equal(r.readLatest('<!DOCTYPE html>'), null);
  assert.equal(r.readLatest('404: Not Found'), null);
});

test('a backslash in the file survives to the regex', () => {
  // Java properties escape with backslashes, so `^v(\\d+…)` in the file is `^v(\d+…)` once loaded.
  // Read it wrong and the pattern matches nothing — which looks exactly like "no release exists".
  const r = new CoreRelease(conf);
  assert.ok(/\\d/.test(r.tagPattern()) === false || r.tagPattern().includes('\\d'),
    `the tag pattern did not survive unescaping: ${r.tagPattern()}`);
  assert.equal(pickLatest(JSON.stringify([{ tag_name: 'v1.2.3' }]), r.tagPattern()), '1.2.3',
    `the shipped tag pattern matches no release at all: ${r.tagPattern()}`);
});

/**
 * The file has to be **inside the packaged extension**, and it lives outside this directory.
 *
 * `vsce` packages what is under the extension root, and `clients/contract/` is not — so without a
 * copy step the VSIX ships without it, `shipped()` returns nothing, and a user is told there is
 * nowhere to fetch from. Nothing fails in development, where the fallback finds the repository copy:
 * the defect would exist only in the artifact.
 *
 * ⚠ **This reads the scripts rather than building a VSIX.** It pins the decision; it does not prove
 * the artifact. Packaging is checked by the release workflow actually running `npm run package`.
 */
test('the contract file is copied into what gets packaged', () => {
  const pkg = JSON.parse(fs.readFileSync(require('path').join(__dirname, '..', '..', 'package.json'), 'utf8'));
  assert.ok(String(pkg.scripts?.contract ?? '').includes('core-release.properties'),
    'there is no step that copies the contract file into the extension root');
  for (const s of ['build', 'package', 'test']) {
    assert.ok(String(pkg.scripts?.[s] ?? '').includes('npm run contract'),
      `the "${s}" script does not run the copy, so it can produce an extension without the contract`);
  }
  const ignore = fs.readFileSync(require('path').join(__dirname, '..', '..', '.vscodeignore'), 'utf8');
  assert.ok(!ignore.split('\n').some((l) => l.trim() === 'core-release.properties'),
    'the copy is made and then excluded from the package');
});
