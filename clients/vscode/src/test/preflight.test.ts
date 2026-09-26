import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import * as path from 'node:path';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';

const execFileAsync = promisify(execFile);

test('preflight: foreign platform Windows drive letter path string normalization (§2.3)', async () => {
  // @ts-ignore
  const { toDirectoryUrl } = await import('../../tools/asset-preflight.mjs');

  const winUrl = toDirectoryUrl('C:\\magi dir#1\\subdir');
  assert.equal(winUrl.href, 'file:///C:/magi%20dir%231/subdir/');
  assert.equal(winUrl.pathname, '/C:/magi%20dir%231/subdir/');
  assert.equal(winUrl.hash, '');
});

test('preflight: current platform native path roundtrip and POSIX path handling (§2.3)', async () => {
  // @ts-ignore
  const { toDirectoryUrl } = await import('../../tools/asset-preflight.mjs');

  // Native path roundtrip on current platform
  const nativeDir = path.resolve(tmpdir(), 'magi dir#1', 'subdir');
  const nativeUrl = toDirectoryUrl(nativeDir);
  assert.ok(nativeUrl.href.endsWith('/'), 'Directory URL ends with trailing slash');
  assert.equal(nativeUrl.hash, '', 'Directory URL has no hash');
  assert.ok(nativeUrl.href.includes('%20'), 'Spaces are percent-encoded');
  assert.ok(nativeUrl.href.includes('%23'), '# is percent-encoded');
  const resolvedNative = fileURLToPath(nativeUrl);
  assert.equal(path.resolve(resolvedNative), nativeDir, 'fileURLToPath roundtrips native path');

  // POSIX-style path string check with platform-aware expectations
  const posixUrl = toDirectoryUrl('/tmp/magi dir#1/subdir');
  assert.equal(posixUrl.hash, '');
  if (process.platform === 'win32') {
    assert.ok(posixUrl.pathname.endsWith('/tmp/magi%20dir%231/subdir/'), 'Windows pathToFileURL resolves drive prefix');
  } else {
    assert.equal(posixUrl.pathname, '/tmp/magi%20dir%231/subdir/');
  }
});

test('preflight: child process exits with code 1 and logs missing bundle and build hint (§2.3)', async () => {
  const preflightScript = path.resolve(__dirname, '../../tools/asset-preflight.mjs');
  const tempDir = await mkdtemp(path.join(tmpdir(), 'magi-preflight-unit-'));

  try {
    // 1) Empty directory: missing chat_html.js -> exit code 1
    try {
      await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
      assert.fail('Expected process to exit with code 1 on empty directory');
    } catch (err: any) {
      assert.equal(err.code, 1);
      assert.ok(err.stderr.includes('chat_html.js'));
      assert.ok(err.stderr.includes("Run 'npm run build --prefix clients/vscode' first."));
    }

    // 2) chat_html.js present: missing answer_state.js -> exit code 1
    await writeFile(path.join(tempDir, 'chat_html.js'), 'export const renderChatHtml = () => "";');
    try {
      await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
      assert.fail('Expected process to exit with code 1 on missing answer_state.js');
    } catch (err: any) {
      assert.equal(err.code, 1);
      assert.ok(err.stderr.includes('answer_state.js'));
      assert.ok(err.stderr.includes("Run 'npm run build --prefix clients/vscode' first."));
    }

    // 3) answer_state.js present: missing chat_adapter.bundle.js -> exit code 1
    await writeFile(path.join(tempDir, 'answer_state.js'), '// answer state');
    try {
      await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
      assert.fail('Expected process to exit with code 1 on missing chat_adapter.bundle.js');
    } catch (err: any) {
      assert.equal(err.code, 1);
      assert.ok(err.stderr.includes('chat_adapter.bundle.js'));
      assert.ok(err.stderr.includes("Run 'npm run build --prefix clients/vscode' first."));
    }

    // 4) chat_adapter.bundle.js present: missing chat_view.bundle.js -> exit code 1
    //    (the view: without it the page draws its markup and never wires a single control)
    await writeFile(path.join(tempDir, 'chat_adapter.bundle.js'), '// adapter bundle');
    try {
      await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
      assert.fail('Expected process to exit with code 1 on missing chat_view.bundle.js');
    } catch (err: any) {
      assert.equal(err.code, 1);
      assert.ok(err.stderr.includes('chat_view.bundle.js'));
      assert.ok(err.stderr.includes("Run 'npm run build --prefix clients/vscode' first."));
    }

    // 5) All four bundles present -> exit code 0
    await writeFile(path.join(tempDir, 'chat_view.bundle.js'), '// view bundle');
    const res = await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
    assert.equal(res.stderr, '');
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});
