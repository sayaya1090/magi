import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import * as path from 'node:path';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';

const execFileAsync = promisify(execFile);

test('preflight: validates Windows drive letter, spaces, and # normalization to file URL (§2.3)', async () => {
  // @ts-ignore
  const { toDirectoryUrl } = await import('../../tools/asset-preflight.mjs');

  const winUrl = toDirectoryUrl('C:\\magi dir#1\\subdir');
  assert.equal(winUrl.href, 'file:///C:/magi%20dir%231/subdir/');
  assert.equal(winUrl.pathname, '/C:/magi%20dir%231/subdir/');
  assert.equal(winUrl.hash, '');

  const posixUrl = toDirectoryUrl('/tmp/magi dir#1/subdir');
  assert.equal(posixUrl.pathname, '/tmp/magi%20dir%231/subdir/');
  assert.equal(posixUrl.hash, '');
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

    // 4) All three bundles present -> exit code 0
    await writeFile(path.join(tempDir, 'chat_adapter.bundle.js'), '// adapter bundle');
    const res = await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
    assert.equal(res.stderr, '');
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});
