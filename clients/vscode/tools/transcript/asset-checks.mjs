import { writeFile, mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

import {
  toDirectoryUrl,
} from '../asset-preflight.mjs';
import {
  TEST_ORIGIN,
  ASSET_URLS,
  installAssetRouter,
  prepareChatHtml,
} from './environment.mjs';

const execFileAsync = promisify(execFile);

/**
 * Executes asset route verification, path normalization, and preflight missing bundle detection (§2.1, §2.2, §2.3, §5.8.6).
 * Must use a plain page fixture (not zero-error collector) because it intentionally triggers 404 responses.
 *
 * @param {import('playwright').Page} page Plain Playwright page fixture
 * @param {string} [compiledHtml] Optional pre-compiled HTML
 */
export async function runAssetChecks(page, compiledHtml) {
  // 1. Path normalization verification (Windows drive letters, spaces, '#') (§2.3)
  const winUrl = toDirectoryUrl('C:\\magi test dir#1\\assets');
  assert.equal(winUrl.href, 'file:///C:/magi%20test%20dir%231/assets/');
  assert.equal(winUrl.pathname, '/C:/magi%20test%20dir%231/assets/');
  assert.equal(winUrl.hash, '');

  // ⚠ **이 줄 하나가 이 레인 전체를 윈도우에서 못 돌게 하고 있었다.** 이 검사는 다른 여섯 묶음보다
  // 앞서 돌고, 여기서 죽으면 40개 시나리오가 「did not run」으로 남는다 — 실측 2026-09-19, 이 기계에서
  // 브라우저 레인이 한 번도 돈 적이 없던 이유다.
  //
  // 드라이브 문자가 없는 절대 경로를 `pathToFileURL` 은 윈도우에서 **현재 드라이브에 붙여** 푼다:
  // `/tmp/…` → `file:///C:/tmp/…`. 그것은 node 가 옳게 하는 일이고, 이 검사가 재려던 것도 드라이브가
  // 아니라 **인코딩**이다 — 공백이 %20 이 되고 `#` 가 %23 이 되어 조각(fragment)으로 잘리지 않는지.
  // 그래서 드라이브 앞자리는 있으면 허용하고, 재려던 것만 그대로 잰다.
  const posixUrl = toDirectoryUrl('/tmp/magi test dir#1/assets');
  assert.match(posixUrl.pathname, /^(\/[a-zA-Z]:)?\/tmp\/magi%20test%20dir%231\/assets\/$/,
    `포함해야 할 것은 인코딩이다: ${posixUrl.pathname}`);
  assert.equal(posixUrl.hash, '');

  // 2. Preflight check in an isolated temporary directory asserting exit code 1, missing path, and build instruction (§2.3)
  const tempDir = await mkdtemp(path.join(tmpdir(), 'magi-test-assets-'));
  const preflightScript = fileURLToPath(new URL('../asset-preflight.mjs', import.meta.url));
  try {
    // A) Empty temp dir: missing chat_html.js -> exit code 1
    try {
      await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
      assert.fail('Should have failed on empty directory');
    } catch (err) {
      assert.equal(err.code, 1, 'Preflight exit code is 1 when chat_html.js is missing');
      assert.ok(err.stderr.includes('chat_html.js'), 'Reports missing chat_html.js path');
      assert.ok(err.stderr.includes("Run 'npm run build --prefix clients/vscode' first."), 'Reports build command instruction');
    }

    // B) chat_html.js present: missing answer_state.js -> exit code 1
    await writeFile(path.join(tempDir, 'chat_html.js'), 'export const renderChatHtml = () => "";');
    try {
      await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
      assert.fail('Should have failed on missing answer_state.js');
    } catch (err) {
      assert.equal(err.code, 1, 'Preflight exit code is 1 when answer_state.js is missing');
      assert.ok(err.stderr.includes('answer_state.js'), 'Reports missing answer_state.js path');
      assert.ok(err.stderr.includes("Run 'npm run build --prefix clients/vscode' first."), 'Reports build command instruction');
    }

    // C) answer_state.js present: missing chat_adapter.bundle.js -> exit code 1
    await writeFile(path.join(tempDir, 'answer_state.js'), '// answer state');
    try {
      await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
      assert.fail('Should have failed on missing chat_adapter.bundle.js');
    } catch (err) {
      assert.equal(err.code, 1, 'Preflight exit code is 1 when chat_adapter.bundle.js is missing');
      assert.ok(err.stderr.includes('chat_adapter.bundle.js'), 'Reports missing chat_adapter.bundle.js path');
      assert.ok(err.stderr.includes("Run 'npm run build --prefix clients/vscode' first."), 'Reports build command instruction');
    }

    // D) All three bundles present -> exit code 0 and empty stderr
    await writeFile(path.join(tempDir, 'chat_adapter.bundle.js'), '// adapter bundle');
    const res = await execFileAsync(process.execPath, [preflightScript, `--dir=${tempDir}`]);
    assert.equal(res.stderr, '', 'Preflight succeeds with code 0 and empty stderr when all bundles exist');
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }

  // 3. Shared exact URL and route rejection verification in an isolated browser context (§2.1)
  const html = compiledHtml || await prepareChatHtml();
  const rejectedUrls = [];
  await installAssetRouter(page, {
    html,
    onUnregistered: (url) => rejectedUrls.push(url),
  });

  const probe = async (url) => {
    const res = await page.goto(url);
    return { status: res ? res.status() : 0, contentType: res ? (res.headers()['content-type'] || '') : '' };
  };

  // Valid assets return 200
  const resDoc = await probe(`${TEST_ORIGIN}/`);
  assert.equal(resDoc.status, 200);
  assert.ok(resDoc.contentType.includes('text/html'));

  const resAnswer = await probe(ASSET_URLS.answerState);
  assert.equal(resAnswer.status, 200);
  assert.ok(resAnswer.contentType.includes('application/javascript'));

  const resAdapter = await probe(ASSET_URLS.adapterBundle);
  assert.equal(resAdapter.status, 200);
  assert.ok(resAdapter.contentType.includes('application/javascript'));

  // Old alias returns 404 and is recorded by onUnregistered
  const resOldAlias = await probe(`${TEST_ORIGIN}/out/web/chat_adapter.js`);
  assert.equal(resOldAlias.status, 404, 'Old chat_adapter.js alias is rejected with 404');
  assert.ok(rejectedUrls.includes(`${TEST_ORIGIN}/out/web/chat_adapter.js`));

  // .broken suffix returns 404 and is recorded by onUnregistered
  const resBrokenAdapter = await probe(`${TEST_ORIGIN}/out/web/chat_adapter.bundle.js.broken`);
  assert.equal(resBrokenAdapter.status, 404, 'chat_adapter.bundle.js.broken is rejected with 404');
  assert.ok(rejectedUrls.includes(`${TEST_ORIGIN}/out/web/chat_adapter.bundle.js.broken`));

  const resBrokenAnswer = await probe(`${TEST_ORIGIN}/out/web/answer_state.js.broken`);
  assert.equal(resBrokenAnswer.status, 404, 'answer_state.js.broken is rejected with 404');
  assert.ok(rejectedUrls.includes(`${TEST_ORIGIN}/out/web/answer_state.js.broken`));

  // Non-existent path returns 404 and is recorded by onUnregistered
  const resUnknown = await probe(`${TEST_ORIGIN}/some/random/path.js`);
  assert.equal(resUnknown.status, 404);
  assert.ok(rejectedUrls.includes(`${TEST_ORIGIN}/some/random/path.js`));

  // External HTTP/HTTPS interception and observer validation (§5.8.5.2)
  // A) External fetch request intercepted and recorded without contacting external host
  await page.evaluate(async () => {
    try {
      await fetch('https://example.com/api/probe-test.json');
    } catch {}
  });
  assert.ok(
    rejectedUrls.includes('https://example.com/api/probe-test.json'),
    'External fetch request must be intercepted and reported by asset router'
  );

  // B) External Image constructor request intercepted and recorded without contacting external host
  await page.evaluate(() => {
    const img = new Image();
    img.src = 'https://example.com/assets/unauthorized-probe.png';
  });
  await new Promise((r) => setTimeout(r, 50));
  assert.ok(
    rejectedUrls.includes('https://example.com/assets/unauthorized-probe.png'),
    'External Image constructor request must be intercepted and reported by asset router'
  );

  // C) Failure induction: verify that an assertion requiring zero external requests strictly fails
  const assertZeroExternal = (urls) => {
    const external = urls.filter((u) => !u.startsWith(TEST_ORIGIN));
    if (external.length > 0) {
      throw new Error(`Unauthorized external requests detected: ${external.join(', ')}`);
    }
  };
  assert.throws(
    () => assertZeroExternal(rejectedUrls),
    /Unauthorized external requests detected/,
    'Observer verifier must reject when external requests are present'
  );
}
