import { readFile, writeFile, mkdtemp, rm } from 'node:fs/promises';
import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { createRowsMessage, createReplyResultMessage } from './transcript-fixtures.mjs';
import {
  runPreflight,
  toDirectoryUrl,
  checkRequiredBundles,
  REQUIRED_BUNDLES,
} from './asset-preflight.mjs';

const execFileAsync = promisify(execFile);
const require = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const { chromium } = require('playwright');

// Single source of truth for asset routing and URL resolution (§2.1)
export const TEST_ORIGIN = 'http://magi.test';
export const ASSET_PATHS = {
  document: ['/', '/index.html'],
  answerState: '/out/web/answer_state.js',
  adapterBundle: '/out/web/chat_adapter.bundle.js',
};
export const ASSET_URLS = {
  document: `${TEST_ORIGIN}/`,
  answerState: `${TEST_ORIGIN}${ASSET_PATHS.answerState}`,
  adapterBundle: `${TEST_ORIGIN}${ASSET_PATHS.adapterBundle}`,
};

// Re-export preflight checker for consumers
export { checkRequiredBundles as verifyRequiredBundles };

// Pre-flight check: Ensure all required compiled assets exist before importing or launching browser (§2.2)
runPreflight();

// Dynamic import evaluated strictly AFTER pre-flight check succeeds (§2.2)
const { renderChatHtml } = await import('../out/web/chat_html.js');

const nonce = 'test-nonce';
const html = renderChatHtml({
  cspSource: `'self' ${TEST_ORIGIN}`,
  nonce,
  scriptUri: ASSET_URLS.answerState,
  adapterUri: ASSET_URLS.adapterBundle,
});

/**
 * Installs exact asset routing on a Playwright page.
 * Shared between verifyAssetRoutesAndPreflight and scenario runners (§2.1).
 *
 * @param {import('playwright').Page} page
 * @param {object} options
 * @param {string} options.html Compiled HTML content for root/document requests.
 * @param {(url: string) => void} [options.onUnregistered] Callback invoked on rejected requests.
 */
export async function installAssetRouter(page, options = {}) {
  const { html, onUnregistered } = options;
  await page.route(`${TEST_ORIGIN}/**`, async (route) => {
    const reqUrl = new URL(route.request().url());
    if (reqUrl.origin === TEST_ORIGIN) {
      if (ASSET_PATHS.document.includes(reqUrl.pathname)) {
        return route.fulfill({ contentType: 'text/html; charset=utf-8', body: html });
      }
      if (reqUrl.pathname === ASSET_PATHS.answerState) {
        const js = await readFile(new URL('../out/web/answer_state.js', import.meta.url), 'utf8');
        return route.fulfill({ contentType: 'application/javascript; charset=utf-8', body: js });
      }
      if (reqUrl.pathname === ASSET_PATHS.adapterBundle) {
        const js = await readFile(new URL('../out/web/chat_adapter.bundle.js', import.meta.url), 'utf8');
        return route.fulfill({ contentType: 'application/javascript; charset=utf-8', body: js });
      }
    }
    if (onUnregistered) {
      onUnregistered(reqUrl.href);
    }
    return route.fulfill({ status: 404, body: 'Not Found' });
  });
}

/**
 * Verifies preflight missing bundle detection and exact route matching / rejection (§2.1, §2.2).
 * Runs in an isolated browser context and temp directory without mutating test state.
 */
export async function verifyAssetRoutesAndPreflight(browser, compiledHtml) {
  console.log('\n▶ Asset Route & Preflight Verification (§2.1, §2.2, §2.3)');

  // 1. Path normalization verification (Windows drive letters, spaces, '#') (§2.3)
  const winUrl = toDirectoryUrl('C:\\magi test dir#1\\assets');
  assert.equal(winUrl.href, 'file:///C:/magi%20test%20dir%231/assets/');
  assert.equal(winUrl.pathname, '/C:/magi%20test%20dir%231/assets/');
  assert.equal(winUrl.hash, '');

  const posixUrl = toDirectoryUrl('/tmp/magi test dir#1/assets');
  assert.equal(posixUrl.pathname, '/tmp/magi%20test%20dir%231/assets/');
  assert.equal(posixUrl.hash, '');

  // 2. Preflight check in an isolated temporary directory asserting exit code 1, missing path, and build instruction (§2.3)
  const tempDir = await mkdtemp(path.join(tmpdir(), 'magi-test-assets-'));
  const preflightScript = fileURLToPath(new URL('./asset-preflight.mjs', import.meta.url));
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
  console.log('  PASS: [preflight] Isolated bundle missing checks verify exit code 1, missing path, and build instructions');

  // 3. Shared exact URL and route rejection verification in an isolated browser context (§2.1)
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    const rejectedUrls = [];
    await installAssetRouter(page, {
      html: compiledHtml,
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
  } finally {
    await context.close();
  }
  console.log('  PASS: [asset_routing] Shared exact asset router serves valid assets and rejects old/broken URLs with 404');

  // 4. Run harness lifecycle verification using real runHarness function (§2.2)
  await verifyRunnerLifecycle();
}

/**
 * Verifies that runHarness guarantees browser and context teardown across:
 * 1) Normal completion
 * 2) Early return on verifyAssetsOnly
 * 3) Scenario exception
 * Asserts that mock browser and context close() methods are called exactly once,
 * and that scenario failure is not converted into success.
 */
export async function verifyRunnerLifecycle() {
  // 1. Normal execution: verify context and browser close called exactly once
  let browserCloseCalls = 0;
  let contextCloseCalls = 0;
  const mockContext = {
    newPage: async () => ({
      on: () => {},
      addInitScript: async () => {},
      route: async () => {},
      goto: async () => {},
    }),
    close: async () => { contextCloseCalls++; },
  };
  const mockBrowser = {
    newContext: async () => mockContext,
    close: async () => { browserCloseCalls++; },
  };
  const dummyBundle = {
    name: 'mock_normal',
    description: 'mock normal bundle',
    scenarios: [{
      id: 'mock_sc',
      name: 'mock scenario',
      run: async () => {},
    }],
  };
  const normalResult = await runHarness({
    launchBrowser: async () => mockBrowser,
    bundles: [dummyBundle],
    html: '<html></html>',
    verifyAssets: false,
    logBundle: false,
  });
  assert.equal(normalResult.totalPassed, 1, 'Dummy scenario passes in runHarness');
  assert.equal(normalResult.totalFailed, 0, 'No failures in dummy scenario');
  assert.equal(contextCloseCalls, 1, 'context.close called exactly once on normal completion');
  assert.equal(browserCloseCalls, 1, 'browser.close called exactly once on normal completion');

  // 2. Early return on verifyAssetsOnly: verify browser.close called exactly once and no context leak
  browserCloseCalls = 0;
  contextCloseCalls = 0;
  let verifyFnCalls = 0;
  const earlyResult = await runHarness({
    launchBrowser: async () => mockBrowser,
    bundles: [dummyBundle],
    html: '<html></html>',
    verifyAssets: true,
    verifyAssetsOnly: true,
    verifyFn: async () => { verifyFnCalls++; },
    logBundle: false,
  });
  assert.equal(verifyFnCalls, 1, 'verifyFn called on verifyAssetsOnly');
  assert.equal(earlyResult.totalPassed, 0, 'No bundle scenarios run on early return');
  assert.equal(contextCloseCalls, 0, 'No context created on early return');
  assert.equal(browserCloseCalls, 1, 'browser.close called exactly once on early return');

  // 3. Scenario exception: verify context.close and browser.close called exactly once and exception propagates
  browserCloseCalls = 0;
  contextCloseCalls = 0;
  const failingBundle = {
    name: 'mock_fail',
    description: 'mock failing bundle',
    scenarios: [{
      id: 'mock_fail_sc',
      name: 'mock failing scenario',
      run: async () => { throw new Error('forced scenario failure'); },
    }],
  };
  await assert.rejects(async () => {
    await runHarness({
      launchBrowser: async () => mockBrowser,
      bundles: [failingBundle],
      html: '<html></html>',
      verifyAssets: false,
      logBundle: false,
    });
  }, /forced scenario failure/, 'Scenario exception is not swallowed or converted to success');
  assert.equal(contextCloseCalls, 1, 'context.close called exactly once on scenario failure');
  assert.equal(browserCloseCalls, 1, 'browser.close called exactly once on scenario failure');

  console.log('  PASS: [lifecycle] runHarness guarantees context and browser close across normal run, early return, and error');
}

// Define scenario suites across the 4 specified bundles (§2.3)
const bundles = [
  {
    name: 'layout',
    description: '레이아웃·보고서',
    scenarios: [
      {
        id: 'layout_long_answer_and_scroll',
        name: '긴 답변/Think, 공통 세로 스크롤, 좁은 뷰포트',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [
              { who: 'agent', label: 'magi', text: 'long answer\n'.repeat(1000) },
              { who: 'council', label: 'Council', text: 'review', thought: 'council think\n'.repeat(1000) }
            ],
            refs: []
          }));
          await page.waitForSelector('.row.agent');
          const bounds = await page.locator('.row.agent').evaluate((el) => ({ height: el.clientHeight, scroll: el.scrollHeight }));
          assert.ok(bounds.height > 10000, 'agent row expands to full height without inner scrollbar');
          assert.equal(bounds.height, bounds.scroll);
          const thought = await page.locator('.thought').evaluate((el) => ({ height: el.clientHeight, scroll: el.scrollHeight }));
          assert.ok(thought.height > 10000, 'thought expands to full height without inner scrollbar');
          assert.equal(thought.height, thought.scroll);

          await page.locator('#scroll').evaluate((el) => { el.scrollTop = 0; });
          await page.mouse.move(200, 250);
          await page.mouse.wheel(0, 500);
          await page.waitForFunction(() => document.querySelector('#scroll').scrollTop > 0);

          await page.setViewportSize({ width: 280, height: 400 });
          assert.equal(await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight), true);
        }
      },
      {
        id: 'layout_report_items_and_jump',
        name: '보고서 항목 보존, 자동스크롤, 과거 읽기 위치 유지, 질문 이동 버튼 (Condition 1-5)',
        run: async (page) => {
          await page.locator('#scroll').evaluate((el) => { el.scrollTop = el.scrollHeight; });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }],
            ask: {
              kind: 'question',
              callId: 'q1',
              what: '어느 방식을 선택할까요?',
              options: ['선택 A', '선택 B 긴 설명이 들어간 두 번째 옵션'],
              index: 1,
              total: 2,
              since: '2026-09-14T10:00:00Z',
              report: [
                { key: 'tried', text: '실제 시도한 것 요약\n'.repeat(20) },
                { key: 'stakes', text: '영향 분석\n'.repeat(20) },
                { key: 'lean', text: '추천 사유\n'.repeat(20) },
                { key: 'custom_order', text: '임의의 커스텀 키 본문 보존 확인' }
              ]
            }
          }));

          await page.waitForSelector('#ask-body:not([hidden])');
          await page.waitForSelector('#ask-controls:not([hidden])');

          // Condition 1 & 2: Report items and custom keys preserved in order
          const grounds = await page.locator('#ask-body .ground').allInnerTexts();
          assert.equal(grounds.length, 4);
          assert.ok(grounds[0].startsWith('tried:'));
          assert.ok(grounds[1].startsWith('stakes:'));
          assert.ok(grounds[2].startsWith('lean:'));
          assert.ok(grounds[3].startsWith('custom_order: 임의의 커스텀 키 본문 보존 확인'));

          // Condition 3: Question arrival auto-scrolls to bottom
          const atBottom = await page.locator('#scroll').evaluate((el) => {
            return Math.abs(el.scrollHeight - el.scrollTop - el.clientHeight) < 5;
          });
          assert.ok(atBottom, 'auto-scroll reached bottom after ask arrived');

          // Full options text in body choices
          const choices = await page.locator('#ask-body ol.choices li').allInnerTexts();
          assert.deepEqual(choices, ['선택 A', '선택 B 긴 설명이 들어간 두 번째 옵션']);

          // Summary row in fixed controls
          const sumText = await page.locator('#ask-controls .summary-text').innerText();
          assert.ok(sumText.includes('답변 대기: 어느 방식을 선택할까요? (1/2)'));

          // Condition 4: User reading older rows maintains position on update
          await page.locator('#scroll').evaluate((el) => { el.scrollTop = 50; });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }, { who: 'agent', label: 'magi', text: 'streaming chunk' }],
            ask: {
              kind: 'question',
              callId: 'q1',
              what: '어느 방식을 선택할까요?',
              options: ['선택 A', '선택 B 긴 설명이 들어간 두 번째 옵션'],
              index: 1,
              total: 2,
              since: '2026-09-14T10:00:00Z',
              report: [{ key: 'tried', text: '...' }]
            }
          }));
          const keptScroll = await page.locator('#scroll').evaluate((el) => el.scrollTop);
          assert.equal(keptScroll, 50, 'past turn reading position was preserved');

          // Condition 5: Jump to question button scrolls to question body
          await page.locator('#ask-controls .jump-btn').click();
          await page.waitForFunction(() => document.querySelector('#scroll').scrollTop > 50);
        }
      },
      {
        id: 'layout_focus_preservation',
        name: '동일 질문 반복 도착 시 버튼 포커스 보존 (Condition 6)',
        run: async (page) => {
          const btn = page.locator('#ask-controls .acts button').first();
          await btn.focus();
          const focusedBefore = await page.evaluate(() => document.activeElement?.textContent);
          assert.equal(focusedBefore, '1. 선택 A');

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }],
            ask: {
              kind: 'question',
              callId: 'q1',
              what: '어느 방식을 선택할까요?',
              options: ['선택 A', '선택 B 긴 설명이 들어간 두 번째 옵션'],
              index: 1,
              total: 2
            }
          }));
          const focusedAfter = await page.evaluate(() => document.activeElement?.textContent);
          assert.equal(focusedAfter, '1. 선택 A', 'focus was preserved on repeated ask with same callId');
        }
      },
      {
        id: 'layout_narrow_short_viewport',
        name: '좁고 낮은 뷰포트에서 입력창 노출 및 외부 오버플로 방지 (Condition 8)',
        run: async (page) => {
          await page.setViewportSize({ width: 250, height: 350 });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'narrow test' }],
            ask: {
              kind: 'question',
              callId: 'q3',
              what: '긴 질문 제목이 좁은 패널에서 표시되는지 확인하는 테스트',
              options: ['선택 1', '선택 2', '선택 3', '선택 4', '선택 5', '선택 6', '선택 7', '선택 8']
            }
          }));
          await page.waitForSelector('#ask-controls .acts button');
          const barVisible = await page.locator('#say').isVisible();
          assert.ok(barVisible, 'input composer is visible in narrow and short viewport');
          const fits = await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight);
          assert.ok(fits, 'page fits within viewport height without outer body overflow');
        }
      }
    ]
  },
  {
    name: 'asks',
    description: '질문·초안',
    scenarios: [
      {
        id: 'asks_free_text_answer_mode',
        name: '자유 텍스트 질문 자동 답변 모드 진입 및 일반 초안 격리·복원 (Condition 9)',
        run: async (page) => {
          await page.setViewportSize({ width: 420, height: 600 });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [],
            ask: null
          }));
          await page.locator('#say').fill('새 작업 초안 작성 중...');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'free-q1',
              what: '이 파일의 이름을 무엇으로 변경할까요?',
              options: []
            }
          }));
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          const replyTag = await page.locator('#reply-mode .reply-target').textContent();
          assert.match(replyTag, /이 파일의 이름을 무엇으로 변경할까요\?/);
          const sayInAnswerMode = await page.locator('#say').inputValue();
          assert.equal(sayInAnswerMode, '', 'input was cleared for question answer');
          const placeholder = await page.locator('#say').getAttribute('placeholder');
          assert.match(placeholder, /답변을 입력하세요/);

          // Send answer from composer
          await page.locator('#say').fill('user-profile.ts');
          await page.locator('#send').click();
          const postedAfterReply = await page.evaluate(() => window.__posted);
          assert.ok(postedAfterReply.some((m) => m.kind === 'reply' && m.callId === 'free-q1' && m.text === 'user-profile.ts'));
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode closed after send');
          const sayRestored = await page.locator('#say').inputValue();
          assert.equal(sayRestored, '새 작업 초안 작성 중...', 'general draft restored after answering question');
        }
      },
      {
        id: 'asks_multiple_choice_and_escape',
        name: '선택형 질문, 직접 입력 버튼, 질문 초안 보존, Esc 취소 및 선택지 클릭 (Condition 10)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'choice-q2',
              what: '배포 환경을 선택하세요',
              options: ['스테이징 환경', '운영(프로덕션) 환경']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 스테이징 환경")');
          const btnTexts = await page.locator('#ask-controls .acts button').allTextContents();
          assert.deepEqual(btnTexts, ['1. 스테이징 환경', '2. 운영(프로덕션) 환경', '직접 입력']);

          // Click '직접 입력'
          await page.locator('#ask-controls button:text("직접 입력")').click();
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'reply mode opened via 직접 입력');
          assert.equal(await page.locator('#say').inputValue(), '', 'input empty for new question draft');

          // Type partial answer then press Escape
          await page.locator('#say').fill('카나리 배포 10%');
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode exited on Escape');
          assert.equal(await page.locator('#say').inputValue(), '새 작업 초안 작성 중...', 'general draft restored on Escape');

          // Re-enter answer mode, verify question draft was saved
          await page.locator('#ask-controls button:text("직접 입력")').click();
          assert.equal(await page.locator('#say').inputValue(), '카나리 배포 10%', 'question draft preserved across cancellation');

          // Click choice button directly - sends choice and restores general draft
          await page.locator('#ask-controls button:text("1. 스테이징 환경")').click();
          const postedAfterChoice = await page.evaluate(() => window.__posted);
          assert.ok(postedAfterChoice.some((m) => m.kind === 'reply' && m.callId === 'choice-q2' && m.text === '스테이징 환경'));
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode closed after choice click');
          assert.equal(await page.locator('#say').inputValue(), '새 작업 초안 작성 중...', 'general draft restored after choice click');
        }
      },
      {
        id: 'asks_send_rejection_and_disconnect',
        name: '전송 거절 및 연결 단절 시 질문 초안 복원과 일반 초안 오염 방지 (Condition 11, 12)',
        run: async (page) => {
          await page.locator('#say').fill('원래 일반 프롬프트 초안');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-reject',
              what: '거절 테스트 질문',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('거절될 답변 내용');
          await page.locator('#send').click();

          // While in flight, say restores general draft
          assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안');
          const postedReject1 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-reject').slice(-1)[0]);
          // Daemon sends refusal replyResult
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-reject',
            attemptId: att.attemptId,
            ok: false,
            error: 'companion refused to accept answer',
            text: '거절될 답변 내용',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedReject1);

          // Webview re-enters answer mode for q-reject and restores failed draft
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await page.locator('#say').inputValue(), '거절될 답변 내용', 'failed reply restored into answer mode');
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안', 'general draft intact without failed answer prepended');

          // Connection disconnect (Condition 12)
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('재시도할 답변');
          await page.locator('#send').click();
          const postedReject2 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-reject').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-reject',
            attemptId: att.attemptId,
            ok: false,
            error: 'no companion is listening on this workspace.',
            text: '재시도할 답변',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedReject2);
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await page.locator('#say').inputValue(), '재시도할 답변', 'disconnected reply restored into answer mode');
          await page.keyboard.press('Escape');
        }
      },
      {
        id: 'asks_late_failure_after_question_replacement',
        name: '질문 교체 뒤 늦은 실패 응답 무시 및 현재 질문 초안 보호 (Condition 13)',
        run: async (page) => {
          // Send answer for q-reject
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('구 질문 답변');
          await page.locator('#send').click();
          const postedReject3 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-reject').slice(-1)[0]);

          // Question replaced by q-new before q-reject failure arrives
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-new',
              what: '새로운 질문',
              options: ['신규 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 신규 1")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('신규 질문에 타이핑 중인 답변');

          // Late failure for q-old arrives!
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-reject',
            attemptId: att.attemptId,
            ok: false,
            error: 'timeout',
            text: '구 질문 답변',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedReject3);

          assert.equal(await page.locator('#say').inputValue(), '신규 질문에 타이핑 중인 답변', 'late failure response from old question did not overwrite current question draft');
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안');
        }
      },
      {
        id: 'asks_duplicate_reply_and_stale_failure_isolation',
        name: '중복 전송 차단, 리비전 버전 격리 및 신규 시도 성공 (Condition 16, 17)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-stale',
              what: '버전 격리 테스트 질문',
              options: ['옵션 Alpha', '옵션 Beta']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('답변 A');

          // Send 답변 A -> attempt 1
          await page.locator('#send').click();
          const replyAttempt1 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-stale').slice(-1)[0]);
          assert.ok(replyAttempt1);
          assert.equal(replyAttempt1.text, '답변 A');
          assert.ok(replyAttempt1.attemptId > 0);

          // Condition 17: Duplicate reply blocked while attempt 1 is in-flight
          await page.locator('#ask-controls button:text("1. 옵션 Alpha")').click();
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#send').click();
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…');

          // Condition 16: User edits to '수정된 답변 B', late failure for attempt 1 arrives
          await page.locator('#say').fill('수정된 답변 B');
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-stale',
            attemptId: att.attemptId,
            ok: false,
            error: 'network timeout',
            text: '답변 A',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), replyAttempt1);

          assert.equal(await page.locator('#say').inputValue(), '수정된 답변 B', 'fresh revision B was NOT overwritten by stale failure of A');

          // Lock released, send attempt 2
          await page.locator('#send').click();
          const replyAttempt2 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-stale').slice(-1)[0]);
          assert.equal(replyAttempt2.text, '수정된 답변 B');
          assert.ok(replyAttempt2.attemptId > replyAttempt1.attemptId, 'attemptId incremented for new attempt');

          // Simulate success for attempt 2
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-stale',
            attemptId: att.attemptId,
            ok: true,
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), replyAttempt2);

          // Dismiss question
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'all done' }],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);
        }
      },
      {
        id: 'asks_resend_isolation_and_idless_response',
        name: 'A 실패 → B 재전송 → A 결과 재도착 격리 및 ID 없는 응답 무시 (Condition 18)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-resend-test',
              what: '재전송 격리 테스트 질문',
              options: ['옵션 1', '옵션 2']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('답변 A');

          // Step 1: Send 답변 A
          await page.locator('#send').click();
          const attemptA = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-resend-test').slice(-1)[0]);
          assert.ok(attemptA && attemptA.attemptId);

          // Step 2: A failure arrives -> restored into answer mode
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            attemptId: att.attemptId,
            ok: false,
            error: 'initial failure',
            text: '답변 A',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), attemptA);
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await page.locator('#say').inputValue(), '답변 A');

          // Step 3: Modify to 답변 B and resend
          await page.locator('#say').fill('답변 B');
          await page.locator('#send').click();
          const attemptB = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-resend-test').slice(-1)[0]);
          assert.ok(attemptB && attemptB.attemptId > attemptA.attemptId);

          await page.locator('#ask-controls button:text("직접 입력")').click();
          assert.equal(await page.locator('#say').inputValue(), '답변 B');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'answer mode active for B');

          // Step 4-1: ID-less response arrives -> must be ignored
          await page.evaluate(() => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            ok: false,
            error: 'malformed no-id response',
            text: '오염 텍스트'
          }, '*'));
          assert.equal(await page.locator('#say').inputValue(), '답변 B', 'ID-less response ignored; draft preserved');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'answer mode preserved after ID-less response');

          // Step 4-2: Stale attempt A arrives -> must be ignored due to attemptId mismatch
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            attemptId: att.attemptId,
            ok: false,
            error: 'stale duplicate error for A',
            text: '답변 A',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), attemptA);

          assert.equal(await page.locator('#say').inputValue(), '답변 B', 'draft B preserved against stale attempt A arrival');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'reply-mode preserved against stale attempt A arrival');

          // In-flight lock for B remains active
          await page.locator('#send').click();
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…', 'lock for attempt B still active');
          await page.locator('#ask-controls button:text("1. 옵션 1")').click();
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…', 'choice click blocked by in-flight lock B');

          // Step 5: B의 실제 성공 응답 도착 -> 정상 처리 및 잠금 해제
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            attemptId: att.attemptId,
            ok: true,
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), attemptB);
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn done' }],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);
        }
      },
      {
        id: 'asks_malformed_payload_rejected_without_mutation',
        name: '비정상 payload 디스패치 시 파서 거부 및 기존 DOM·질문·초안 상태 보존 (§2.3)',
        run: async (page) => {
          // 1. Setup normal rows, pending ask, and active draft
          const initialQuestion = {
            kind: 'question',
            callId: 'q-malformed-guard',
            what: '보존되어야 할 질문 제목',
            options: ['옵션 A', '옵션 B']
          };
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [
              { who: 'user', label: 'You', text: '이전 사용자 입력' },
              { who: 'agent', label: 'magi', text: '이전 에이전트 답변' }
            ],
            ask: initialQuestion,
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('작성 중인 답변 초안');

          // 2. Snapshot full DOM and state before malformed dispatch
          const beforeState = await page.evaluate(() => ({
            sayValue: document.getElementById('say').value,
            replyModeVisible: !document.getElementById('reply-mode').hidden,
            replyTarget: document.querySelector('#reply-mode .reply-target')?.textContent || '',
            rowsCount: document.querySelectorAll('#rows .row').length,
            rowsHtml: document.getElementById('rows').innerHTML,
            askBodyHtml: document.getElementById('ask-body').innerHTML,
            askControlsHtml: document.getElementById('ask-controls').innerHTML,
          }));
          assert.equal(beforeState.sayValue, '작성 중인 답변 초안');
          assert.equal(beforeState.replyModeVisible, true);
          assert.equal(beforeState.rowsCount, 2);

          // 3. Dispatch malformed messages directly without fixture correction
          await page.evaluate(() => {
            // Malformed rows (non-array)
            window.postMessage({ kind: 'rows', rows: 'invalid-non-array' }, '*');
            // Malformed rows (missing session)
            window.postMessage({ kind: 'rows', rows: [] }, '*');
            // Malformed rows (non-string refs)
            window.postMessage({ kind: 'rows', session: 's1', rows: [], refs: [123] }, '*');
            // Malformed replyResult (empty callId)
            window.postMessage({ kind: 'replyResult', callId: '', attemptId: 1, ok: true }, '*');
            // Malformed state (missing note)
            window.postMessage({ kind: 'state', state: {} }, '*');
            // Unknown kind
            window.postMessage({ kind: 'unknown_kind_never_seen' }, '*');
            // Primitive non-object payloads
            window.postMessage(null, '*');
            window.postMessage(12345, '*');
            window.postMessage('string payload', '*');
          });

          // 4. Test synchronization using postMessage FIFO ordering without arbitrary sleep
          await page.evaluate(() => new Promise((resolve) => {
            window.addEventListener('message', function onSync(e) {
              if (e.data && e.data.__syncGuard) {
                window.removeEventListener('message', onSync);
                resolve();
              }
            });
            window.postMessage({ __syncGuard: true }, '*');
          }));

          // 5. Verify full state preservation
          const afterState = await page.evaluate(() => ({
            sayValue: document.getElementById('say').value,
            replyModeVisible: !document.getElementById('reply-mode').hidden,
            replyTarget: document.querySelector('#reply-mode .reply-target')?.textContent || '',
            rowsCount: document.querySelectorAll('#rows .row').length,
            rowsHtml: document.getElementById('rows').innerHTML,
            askBodyHtml: document.getElementById('ask-body').innerHTML,
            askControlsHtml: document.getElementById('ask-controls').innerHTML,
          }));
          assert.deepEqual(afterState, beforeState, 'DOM, question, input composer, and draft were preserved without mutation');

          // Clean up: cancel answer mode and dismiss ask
          await page.keyboard.press('Escape');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);
        }
      },
      {
        id: 'asks_session_roundtrip_draft_and_autocomplete_invalidation',
        name: '세션 전환 왕복 시 일반·질문 초안 완벽 복원 및 자동완성 무효화 (§4.5 Item 5)',
        run: async (page) => {
          // 1. Session 1 초기화 및 일반 초안 작성
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-1',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 1 started' }],
            ask: null
          }));
          await page.locator('#say').fill('S1 일반 작업 메모');

          // S1에 질문 도착
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-1',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 1 started' }],
            ask: {
              kind: 'question',
              callId: 'q-s1-roundtrip',
              what: 'S1 전용 질문',
              options: ['선택지 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('S1 질문 답변 작성 중');

          // 자동완성 힌트 트리거 (입력 후 debounce 대기)
          await page.waitForFunction(() => window.__posted.some(m => m.kind === 'suggest' && m.target === 'q-s1-roundtrip'));
          const suggestMsg = await page.evaluate(() => window.__posted.filter(m => m.kind === 'suggest' && m.target === 'q-s1-roundtrip').slice(-1)[0]);
          await page.evaluate((req) => window.postMessage({
            kind: 'suggestion',
            text: '추천 문구',
            reqId: req.reqId,
            target: req.target
          }, '*'), suggestMsg);
          await page.waitForFunction(() => document.getElementById('hint').textContent.includes('추천 문구'));

          // 2. Session 2로 전환 (다른 세션)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-2',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 2 started' }],
            ask: null
          }));
          // 전환 시 자동완성 힌트 무효화 확인
          assert.equal(await page.locator('#hint').textContent(), '', 'hint invalidated on session change');
          await page.keyboard.press('Tab');
          assert.equal(await page.locator('#say').inputValue(), '', 'tab did not insert stale suggestion from previous session');
          // Session 2는 답변 모드가 아니며 초안이 비어 있어야 함
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode inactive in S2');
          assert.equal(await page.locator('#say').inputValue(), '', 'S2 composer starts empty');

          // Session 2에서 일반 초안 작성
          await page.locator('#say').fill('S2 일반 작업 메모');

          // 3. Session 1으로 다시 전환 (S1 복원)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-1',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 1 active' }],
            ask: {
              kind: 'question',
              callId: 'q-s1-roundtrip',
              what: 'S1 전용 질문',
              options: ['선택지 1']
            }
          }));
          // S1의 질문 초안과 답변 모드가 그대로 복원되어야 함
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await page.locator('#say').inputValue(), 'S1 질문 답변 작성 중', 'S1 question draft restored');

          // 답변 모드 취소 시 S1의 일반 초안 복원 확인
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode exited on Escape');
          assert.equal(await page.locator('#say').inputValue(), 'S1 일반 작업 메모', 'S1 general draft restored');

          // 4. Session 2로 다시 전환 (S2 복원)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-2',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 2 active' }],
            ask: null
          }));

          await page.waitForFunction(() => document.getElementById('say').value === 'S2 일반 작업 메모');
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode inactive in S2');
          assert.equal(await page.locator('#say').inputValue(), 'S2 일반 작업 메모', 'S2 general draft restored');

          // 정리
          await page.locator('#say').fill('');
        }
      },
      {
        id: 'asks_recovery_list_ui_and_continuous_workflow',
        name: '실패 답변 등록 → 복구 목록 열기 → 전문 → 답변 모드에서 복사 취소/확정(이어 붙이기) → 삭제 및 0회 전송 검증 (§4.6)',
        run: async (page) => {
          // 1. 초기 상태 검증: #recovery-btn 텍스트가 '복구 초안 0', #recovery-panel hidden
          const recoveryBtn = page.locator('#recovery-btn');
          const recoveryPanel = page.locator('#recovery-panel');
          assert.equal(await recoveryBtn.textContent(), '복구 초안 0');
          assert.equal(await recoveryPanel.evaluate((el) => el.hidden), true);

          // 2. 세션 1에 질문 등록 및 답변 작성, 전송
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-rec-1',
            rows: [{ who: 'agent', label: 'magi', text: 'question ask' }],
            ask: {
              kind: 'question',
              callId: 'q-rec-1',
              what: '작업을 진행할까요?',
              options: ['예', '아니오']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();

          const failedAnswerText = '진행하겠습니다. <script>alert("xss")</script>\nLine 2';
          await page.locator('#say').fill(failedAnswerText);

          // #send 클릭으로 전송 -> in-flight 등록
          const postedLenBeforeSend = await page.evaluate(() => window.__posted.length);
          await page.locator('#send').click();
          await page.waitForFunction((len) => window.__posted.length > len, postedLenBeforeSend);
          const replyMsg = await page.evaluate(() => window.__posted.filter((m) => m.kind === 'reply' && m.callId === 'q-rec-1').slice(-1)[0]);
          assert.ok(replyMsg, 'reply message must be dispatched');

          // 3. 실패 결과(replyResult ok: false) 수신
          await page.evaluate((payload) => window.postMessage(payload, '*'), createReplyResultMessage({
            callId: 'q-rec-1',
            ok: false,
            error: 'daemon communication failure',
          }, replyMsg));

          // 복구 뱃지 갱신 확인
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');
          assert.equal(await recoveryBtn.textContent(), '복구 초안 1');

          // 4. 복구 버튼 클릭 -> 패널 열림 확인
          await recoveryBtn.click();
          assert.equal(await recoveryPanel.evaluate((el) => el.hidden), false);
          assert.equal(await recoveryBtn.getAttribute('aria-expanded'), 'true');

          // 안내 문구 및 항목 검증
          const notice = page.locator('.recovery-notice');
          assert.equal(await notice.textContent(), '이 창에서 임시 보관 중');
          const recoveryItem = page.locator('.recovery-item');
          assert.equal(await recoveryItem.count(), 1);
          assert.ok((await recoveryItem.locator('.recovery-reason').textContent()).includes('답변 전송을 확인하지 못함'));

          // 5. 전문 보기 클릭 -> XSS 안전성 및 원문 보존 검증
          const fulltextBtn = recoveryItem.locator('.fulltext-btn');
          assert.equal(await fulltextBtn.textContent(), '전문 보기');
          await fulltextBtn.click();
          await page.waitForSelector('.recovery-full-text');
          assert.equal(await fulltextBtn.textContent(), '전문 닫기');

          const preText = await recoveryItem.locator('.recovery-full-text').evaluate((el) => el.textContent);
          assert.equal(preText, failedAnswerText, 'full text must match raw text verbatim including script tags and newlines');

          // 전문 접기
          await fulltextBtn.click();
          assert.equal(await recoveryItem.locator('.recovery-full-text').count(), 0);

          // 6. 답변 모드 및 일반 초안 G가 있는 상태에서 복사 시도
          // 먼저 일반 모드로 나가서 일반 초안 G 입력
          await page.locator('#reply-cancel').click(); // exit answer mode
          await page.waitForFunction(() => document.getElementById('reply-mode').hidden);
          const existingG = '기존에 작성 중이던 일반 초안 메모';
          await page.locator('#say').fill(existingG);

          // 다시 답변 모드 진입하여 질문 답변 Q 입력
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          const currentAnswerDraft = '새로 입력 중인 답변 Q';
          await page.locator('#say').fill(currentAnswerDraft);

          // 복사 버튼 클릭 -> 일반 초안 G가 있으므로 인라인 확인 상자("이어 붙이기 / 취소") 노출
          const postMessagesBeforeCopy = await page.evaluate(() => window.__posted.length);
          const copyBtn = recoveryItem.locator('.copy-btn');
          await copyBtn.click();

          await page.waitForSelector('.recovery-confirm-box');
          const confirmBox = recoveryItem.locator('.recovery-confirm-box');
          assert.ok(await confirmBox.isVisible());

          // 6A. 취소 클릭
          const cancelBtn = confirmBox.locator('.confirm-cancel-btn');
          await cancelBtn.click();
          assert.equal(await recoveryItem.locator('.recovery-confirm-box').count(), 0, 'confirm box dismissed on cancel');
          assert.equal(await page.locator('#say').inputValue(), currentAnswerDraft, 'answer draft untouched on cancel');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'still in answer mode on cancel');

          // 6B. 다시 복사 클릭 -> 이어 붙이기 클릭
          await copyBtn.click();
          await page.waitForSelector('.recovery-confirm-box');
          const appendBtn = recoveryItem.locator('.confirm-append-btn');
          await appendBtn.click();

          // 검증: G + "\n\n" + failedAnswerText, 답변 모드 해제(일반 모드), 자동완성 무효화
          const expectedCombined = existingG + '\n\n' + failedAnswerText;
          await page.waitForFunction((exp) => document.getElementById('say').value === exp, expectedCombined);
          assert.equal(await page.locator('#reply-mode').evaluate((el) => el.hidden), true, 'switched to general mode');
          assert.equal(await page.locator('#say').inputValue(), expectedCombined);

          // 단언: 복구 조작 중 호스트로 say 또는 reply postMessage가 0회 발생했는지 확인 (§4.6.5)
          const postMessagesAfterAppend = await page.evaluate(() => window.__posted.length);
          assert.equal(postMessagesAfterAppend, postMessagesBeforeCopy, 'recovery copy must NOT post any say or reply messages');

          // 7. 명시적 삭제: .delete-btn 클릭 -> 항목 제거 및 뱃지 0 갱신
          const deleteBtn = recoveryItem.locator('.delete-btn');
          await deleteBtn.click();

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 0');
          assert.equal(await page.locator('.recovery-item').count(), 0);
          assert.ok(await page.locator('.recovery-empty').isVisible());
          assert.equal(await page.locator('#say').inputValue(), expectedCombined, 'composer input preserved after delete');

          // 8. 320×600 및 420×700 뷰포트에서 레이아웃 접근 검증 (§4.6.5)
          await page.setViewportSize({ width: 320, height: 600 });
          assert.ok(await page.locator('#topbar').isVisible());
          assert.ok(await page.locator('#say').isVisible());

          await page.setViewportSize({ width: 420, height: 700 });
          assert.ok(await page.locator('#topbar').isVisible());
          assert.ok(await page.locator('#say').isVisible());

          // 패널 닫기 및 정리
          await recoveryBtn.click();
          assert.equal(await recoveryPanel.evaluate((el) => el.hidden), true);
          await page.locator('#say').fill('');
        }
      },
      {
        id: 'asks_recovery_node_focus_and_context_switch',
        name: '복구 항목 DOM 노드·포커스 보존, 삭제 시 포커스 이동, 세션 전환 시 확인 즉시 취소 (§4.6.3)',
        run: async (page) => {
          const recoveryBtn = page.locator('#recovery-btn');
          const recoveryPanel = page.locator('#recovery-panel');

          // 1. 세션 session-focus 준비 및 2개 실패 등록
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'focus test ready' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-1',
              what: '질문 1 포커스 검증',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('포커스 첫번째 답변');
          await page.locator('#send').click();

          const postedF1 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-1').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-1',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 1',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF1);

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');

          // 두번째 실패 등록
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'turn 2' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-2',
              what: '질문 2 포커스 검증',
              options: ['선택 2']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('포커스 두번째 답변');
          await page.locator('#send').click();

          const postedF2 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-2').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-2',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 2',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF2);

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 2');

          // 2. 패널 열기: 발생순에 따라 [B, A] (B: 최신 항목 0번, A: 아래쪽 항목 1번)
          await recoveryBtn.click();
          assert.equal(await recoveryPanel.evaluate((el) => el.hidden), false);

          const items = page.locator('.recovery-item');
          assert.equal(await items.count(), 2);
          const itemBText = await items.nth(0).locator('.recovery-preview').textContent();
          const itemAText = await items.nth(1).locator('.recovery-preview').textContent();
          assert.ok(itemBText.includes('포커스 두번째 답변'), 'Item B is top item');
          assert.ok(itemAText.includes('포커스 첫번째 답변'), 'Item A is bottom item');

          // 아래쪽 항목 A의 DOM 노드에 마커를 붙여 노드 재사용(인스턴스 불변) 검증 준비
          await items.nth(1).evaluate((el) => {
            el.__marker_id = 'preserved_node_A';
          });

          // 전문(full text) 열기
          await items.nth(1).locator('.fulltext-btn').click();
          const preA = items.nth(1).locator('.recovery-full-text');
          assert.ok(await preA.isVisible());

          // 2-1. 전문 열린 상태에서 사유(recovery-reason) 선택 후 동일 rows 갱신 시 사유 선택 보존 (§4.6 Item 4)
          await page.evaluate(() => {
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const reasonEl = itemAEl?.querySelector('.recovery-reason');
            const tn = reasonEl?.firstChild || reasonEl;
            window.getSelection().setBaseAndExtent(tn, 0, tn, 5);
          });
          assert.equal(await page.evaluate(() => window.getSelection()?.toString()), '답변 전송');

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update for reason selection' }],
          }));
          await page.waitForTimeout(50);

          const reasonCheck = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const reasonEl = itemAEl?.querySelector('.recovery-reason');
            const tn = reasonEl?.firstChild || reasonEl;
            return {
              text: sel.toString(),
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
            };
          });
          assert.equal(reasonCheck.text, '답변 전송', 'selection must remain on reason text, not converted to full text');
          assert.equal(reasonCheck.anchorMatches, true, 'anchorNode must still be reasonEl text node');
          assert.equal(reasonCheck.focusMatches, true, 'focusNode must still be reasonEl text node');
          assert.equal(reasonCheck.anchorOffset, 0);
          assert.equal(reasonCheck.focusOffset, 5);

          // 2-2. 제목(recovery-title) 선택 후 rows 갱신 시 선택 보존 (§4.6 Item 4)
          await page.evaluate(() => {
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const titleEl = itemAEl?.querySelector('.recovery-title');
            const tn = titleEl?.firstChild || titleEl;
            window.getSelection().setBaseAndExtent(tn, 0, tn, 4);
          });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update for title selection' }],
          }));
          await page.waitForTimeout(50);
          const titleCheck = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const titleEl = itemAEl?.querySelector('.recovery-title');
            const tn = titleEl?.firstChild || titleEl;
            return {
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
            };
          });
          assert.equal(titleCheck.anchorMatches, true, 'anchorNode must still be titleEl text node');
          assert.equal(titleCheck.focusMatches, true, 'focusNode must still be titleEl text node');
          assert.equal(titleCheck.anchorOffset, 0);
          assert.equal(titleCheck.focusOffset, 4);

          // 2-3. 미리보기(recovery-preview) 선택 후 rows 갱신 시 선택 보존 (§4.6 Item 4)
          await page.evaluate(() => {
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const prevEl = itemAEl?.querySelector('.recovery-preview');
            const tn = prevEl?.firstChild || prevEl;
            window.getSelection().setBaseAndExtent(tn, 0, tn, 4);
          });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update for preview selection' }],
          }));
          await page.waitForTimeout(50);
          const prevCheck = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const prevEl = itemAEl?.querySelector('.recovery-preview');
            const tn = prevEl?.firstChild || prevEl;
            return {
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
            };
          });
          assert.equal(prevCheck.anchorMatches, true, 'anchorNode must still be previewEl text node');
          assert.equal(prevCheck.focusMatches, true, 'focusNode must still be previewEl text node');
          assert.equal(prevCheck.anchorOffset, 0);
          assert.equal(prevCheck.focusOffset, 4);

          // 2-4. 두 항목에 걸친 선택 시 안전 갱신 (§4.6 Item 4)
          await page.evaluate(() => {
            const items = document.querySelectorAll('.recovery-item');
            const tnB = items[0].querySelector('.recovery-title')?.firstChild || items[0];
            const tnA = items[1].querySelector('.recovery-title')?.firstChild || items[1];
            window.getSelection().setBaseAndExtent(tnB, 0, tnA, 2);
          });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update cross item' }],
          }));
          await page.waitForTimeout(50);

          // 2-5. 목록 밖 선택은 갱신 과정에서 지우거나 교체하지 않음 (§4.6 Item 4)
          await page.evaluate(() => {
            const row = document.querySelector('.row-agent');
            const tn = row?.firstChild || row;
            window.getSelection().setBaseAndExtent(tn, 0, tn, 5);
          });
          const outsideTextBefore = await page.evaluate(() => window.getSelection()?.toString());
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update outside selection' }],
          }));
          await page.waitForTimeout(50);
          const outsideTextAfter = await page.evaluate(() => window.getSelection()?.toString());
          assert.equal(outsideTextAfter, outsideTextBefore, 'outside selection must remain untouched across refresh');

          // 3. 전문 내부 역방향 선택(backwards selection) 및 moveBefore 재정렬 검증 (§4.6 Item 4, 5)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1 retry' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-1',
              what: '질문 1 포커스 재시도',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('포커스 첫번째 답변');
          await page.locator('#send').click();

          const postedF1Retry = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-1').slice(-1)[0]);
          assert.ok(postedF1Retry, 'in-flight retry for item A posted');

          // in-flight 상태에서 아래쪽 항목 A(인덱스 1)의 복사 버튼에 포커스 설정 및 pre 내부 역방향 선택 설정 (anchor: 3, focus: 0 -> '포커스')
          await items.nth(1).locator('.copy-btn').focus();
          await page.evaluate(() => {
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const preEl = itemAEl?.querySelector('.recovery-full-text');
            if (preEl) {
              const textNode = preEl.firstChild || preEl;
              window.getSelection().setBaseAndExtent(textNode, 3, textNode, 0);
            }
          });

          const backwardBeforeReorder = await page.evaluate(() => {
            const sel = window.getSelection();
            const active = document.activeElement;
            return {
              className: active?.className || '',
              parentItemText: active?.closest('.recovery-item')?.querySelector('.recovery-preview')?.textContent || '',
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
              text: sel.toString(),
            };
          });
          assert.ok(backwardBeforeReorder.className.includes('copy-btn'));
          assert.ok(backwardBeforeReorder.parentItemText.includes('포커스 첫번째 답변'));
          assert.equal(backwardBeforeReorder.anchorOffset, 3);
          assert.equal(backwardBeforeReorder.focusOffset, 0);
          assert.equal(backwardBeforeReorder.text, '포커스');

          // 4. A 재실패 응답 도착 -> [B, A]에서 [A, B]로 순서 재정렬 (moveBefore 경로)
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-1',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 1 again',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF1Retry);
          await page.waitForTimeout(50);

          // DOM 순서가 [A, B]로 바뀌었음을 확인
          const itemsAfterReorder = page.locator('.recovery-item');
          assert.equal(await itemsAfterReorder.count(), 2);
          const firstText = await itemsAfterReorder.nth(0).locator('.recovery-preview').textContent();
          const secondText = await itemsAfterReorder.nth(1).locator('.recovery-preview').textContent();
          assert.ok(firstText.includes('포커스 첫번째 답변'), 'Item A must now be at index 0');
          assert.ok(secondText.includes('포커스 두번째 답변'), 'Item B must now be at index 1');

          // A의 DOM 노드가 새로 생성되지 않고 기존 인스턴스 그대로 유지됨을 확인
          const markerAfter = await itemsAfterReorder.nth(0).evaluate((el) => el.__marker_id);
          assert.equal(markerAfter, 'preserved_node_A', 'Item A DOM node instance must be preserved across reorder');

          // document.activeElement가 여전히 A의 '복사' 버튼을 가리키고 있음을 확인 (body나 상위 컨테이너로 튀지 않음)
          const focusedAfterReorder = await page.evaluate(() => {
            const active = document.activeElement;
            return {
              tagName: active?.tagName,
              className: active?.className || '',
              parentItemText: active?.closest('.recovery-item')?.querySelector('.recovery-preview')?.textContent || ''
            };
          });
          assert.equal(focusedAfterReorder.tagName, 'BUTTON');
          assert.ok(focusedAfterReorder.className.includes('copy-btn'), 'activeElement must remain on copy-btn');
          assert.ok(focusedAfterReorder.parentItemText.includes('포커스 첫번째 답변'), 'activeElement must remain on item A');

          // 전문(full text) 역방향 텍스트 선택이 재정렬 후에도 방향 및 오프셋 유지 검사 (§4.6 Item 4)
          const backwardAfterReorder = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemAEl = document.querySelectorAll('.recovery-item')[0];
            const preEl = itemAEl?.querySelector('.recovery-full-text');
            const tn = preEl?.firstChild || preEl;
            return {
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
              text: sel.toString(),
            };
          });
          assert.equal(backwardAfterReorder.anchorMatches, true, 'anchorNode preserved in backward selection');
          assert.equal(backwardAfterReorder.focusMatches, true, 'focusNode preserved in backward selection');
          assert.equal(backwardAfterReorder.anchorOffset, 3, 'anchorOffset must remain 3 (backward start)');
          assert.equal(backwardAfterReorder.focusOffset, 0, 'focusOffset must remain 0 (backward end)');
          assert.equal(backwardAfterReorder.text, '포커스');

          // Enter 키 입력 시 복사/확인 정상 1회 트리거 및 0회 say/reply 전송 확인
          const postedLenBeforeEnter = await page.evaluate(() => window.__posted.length);
          await page.keyboard.press('Enter');
          await page.waitForTimeout(50);

          const confirmBoxCount = await page.locator('.recovery-confirm-box').count();
          const sayVal = await page.locator('#say').inputValue();
          assert.ok(confirmBoxCount > 0 || sayVal.includes('포커스 첫번째 답변'), 'copy action must be triggered via Enter on focused button');

          const postedAfterEnter = await page.evaluate(() => window.__posted);
          const newTransmissions = postedAfterEnter.slice(postedLenBeforeEnter).filter((m) => m.kind === 'say' || m.kind === 'reply');
          assert.equal(newTransmissions.length, 0, '0 backend transmissions during recovery copy');

          if (confirmBoxCount > 0) {
            await page.locator('.confirm-cancel-btn').click();
          }

          // 5. moveBefore 비활성화 환경 (insertBefore 폴백 경로) 검증 (§4.6 Item 5)
          await page.evaluate(() => {
            const listEl = document.getElementById('recovery-items');
            if (listEl) {
              listEl.__saved_moveBefore = listEl.moveBefore;
              listEl.moveBefore = undefined; // Force insertBefore fallback path
            }
          });

          // 이제 항목 B의 재실패로 [A, B]에서 다시 [B, A]로 재정렬 발생
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'turn 2 retry' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-2',
              what: '질문 2 포커스 재시도',
              options: ['선택 2']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('포커스 두번째 답변');
          await page.locator('#send').click();

          const postedF2Retry = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-2').slice(-1)[0]);
          assert.ok(postedF2Retry);

          // 현재 [A, B]에서 아래쪽 항목 B(인덱스 1)의 복사 버튼 포커스 및 전문 열기/선택
          await itemsAfterReorder.nth(1).locator('.fulltext-btn').click();
          await itemsAfterReorder.nth(1).locator('.copy-btn').focus();
          await page.evaluate(() => {
            const itemBEl = document.querySelectorAll('.recovery-item')[1];
            const preEl = itemBEl?.querySelector('.recovery-full-text');
            if (preEl) {
              const tn = preEl.firstChild || preEl;
              window.getSelection().setBaseAndExtent(tn, 0, tn, 3); // '포커스'
            }
          });

          // B 재실패 도착 -> insertBefore 폴백을 통해 [A, B]에서 [B, A]로 재정렬
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-2',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 2 again fallback',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF2Retry);
          await page.waitForTimeout(50);

          // insertBefore 폴백 경로에서도 DOM 순서가 [B, A]로 변경되었음을 확인
          const itemsFallback = page.locator('.recovery-item');
          assert.equal(await itemsFallback.count(), 2);
          const fbFirstText = await itemsFallback.nth(0).locator('.recovery-preview').textContent();
          const fbSecondText = await itemsFallback.nth(1).locator('.recovery-preview').textContent();
          assert.ok(fbFirstText.includes('포커스 두번째 답변'), 'Item B is now at index 0 via insertBefore fallback');
          assert.ok(fbSecondText.includes('포커스 첫번째 답변'), 'Item A is now at index 1 via insertBefore fallback');

          // insertBefore 폴백에서도 document.activeElement가 B의 복사 버튼으로 동기 복원됨을 확인
          const activeFallback = await page.evaluate(() => {
            const active = document.activeElement;
            return {
              tagName: active?.tagName,
              className: active?.className || '',
              parentItemText: active?.closest('.recovery-item')?.querySelector('.recovery-preview')?.textContent || ''
            };
          });
          assert.equal(activeFallback.tagName, 'BUTTON');
          assert.ok(activeFallback.className.includes('copy-btn'), 'activeElement must remain on copy-btn via fallback');
          assert.ok(activeFallback.parentItemText.includes('포커스 두번째 답변'), 'activeElement must remain on item B via fallback');

          // insertBefore 폴백에서도 전문 텍스트 선택이 동기 복원됨을 확인
          const selFallback = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemBEl = document.querySelectorAll('.recovery-item')[0];
            const preEl = itemBEl?.querySelector('.recovery-full-text');
            const tn = preEl?.firstChild || preEl;
            return {
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
              text: sel.toString(),
            };
          });
          assert.equal(selFallback.anchorMatches, true, 'anchorNode restored via fallback');
          assert.equal(selFallback.focusMatches, true, 'focusNode restored via fallback');
          assert.equal(selFallback.anchorOffset, 0);
          assert.equal(selFallback.focusOffset, 3);
          assert.equal(selFallback.text, '포커스');

          // Space 키 입력 시 복사/확인 정상 1회 트리거 및 0회 전송 확인
          const postedLenBeforeSpace = await page.evaluate(() => window.__posted.length);
          await page.keyboard.press('Space');
          await page.waitForTimeout(50);

          const confirmBoxFbCount = await page.locator('.recovery-confirm-box').count();
          const sayValFb = await page.locator('#say').inputValue();
          assert.ok(confirmBoxFbCount > 0 || sayValFb.includes('포커스 두번째 답변'), 'copy action must be triggered via Space on focused button');

          const postedAfterSpace = await page.evaluate(() => window.__posted);
          const spaceTransmissions = postedAfterSpace.slice(postedLenBeforeSpace).filter((m) => m.kind === 'say' || m.kind === 'reply');
          assert.equal(spaceTransmissions.length, 0, '0 backend transmissions during recovery copy via Space');

          if (confirmBoxFbCount > 0) {
            await page.locator('.confirm-cancel-btn').click();
          }

          // moveBefore 복구
          await page.evaluate(() => {
            const listEl = document.getElementById('recovery-items');
            if (listEl && listEl.__saved_moveBefore !== undefined) {
              listEl.moveBefore = listEl.__saved_moveBefore;
              delete listEl.__saved_moveBefore;
            } else if (listEl) {
              delete listEl.moveBefore;
            }
          });

          // 6. composer(#say) 포커스 보호 및 선택 항목 삭제 시 정상 삭제 동작 검증 (§4.6 Item 3, 4)
          await page.locator('#say').focus();
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say');

          // rows 수신 시 composer 포커스 유지
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'streamed row after fallback' }],
          }));
          await page.waitForTimeout(50);
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say', 'focus must stay on #say after rows update');

          // 인접 항목 A(인덱스 1)의 제목 텍스트 선택 후 삭제 -> 선택 해제/무효화 및 composer 포커스 유지
          await page.evaluate(() => {
            const items = document.querySelectorAll('.recovery-item');
            if (items[1]) {
              const tn = items[1].querySelector('.recovery-title')?.firstChild || items[1];
              window.getSelection().setBaseAndExtent(tn, 0, tn, 4);
            }
          });
          assert.ok((await page.evaluate(() => window.getSelection()?.toString().length)) > 0);

          await page.evaluate(() => {
            const items = document.querySelectorAll('.recovery-item');
            if (items[1]) {
              const del = items[1].querySelector('.delete-btn');
              if (del) del.click();
            }
          });
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say', 'focus must stay on #say after adjacent item deletion');

          // 선택 항목이 삭제되었으므로 옛 오프셋이 남아 있는 항목 B에 clamp되어 재지정되지 않음 확인 (§4.6 Item 3, 4)
          const selAfterItemDeleted = await page.evaluate(() => {
            const sel = window.getSelection();
            return {
              isCollapsed: sel.isCollapsed,
              rangeCount: sel.rangeCount,
              text: sel.toString(),
            };
          });
          assert.ok(selAfterItemDeleted.isCollapsed || selAfterItemDeleted.rangeCount === 0 || selAfterItemDeleted.text === '', 'selection must not be clamped onto surviving item');

          // 7. 남은 항목 B의 삭제 버튼에 포커스 후 삭제 시 recovery-btn 복귀
          await page.locator('.recovery-item .delete-btn').focus();
          await page.locator('.recovery-item .delete-btn').click();
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 0');
          const focusedAfterAllDel = await page.evaluate(() => document.activeElement?.id);
          assert.equal(focusedAfterAllDel, 'recovery-btn', 'focus must return to #recovery-btn when last item deleted');

          // 6. Context change cancellation:
          // Register 1 failure in session-focus
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'turn 4' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-3',
              what: '취소 검증 질문',
              options: ['선택']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('취소 검증 실패 답변');
          await page.locator('#send').click();

          const postedF3 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-3').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-3',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 3',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF3);

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');

          // Prepare general draft in session-focus
          await page.keyboard.press('Escape');
          await page.locator('#say').fill('세션 포커스의 일반 초안');

          // Click copy -> confirm box appears
          await page.locator('.recovery-item .copy-btn').click();
          await page.waitForSelector('.recovery-confirm-box');
          assert.ok(await page.locator('.recovery-confirm-box').isVisible());

          // Switch context to session-other via rows
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-other',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'other session' }],
          }));
          await page.waitForTimeout(50);
          assert.equal(await page.locator('.recovery-confirm-box').count(), 0, 'confirm box dismissed on switching to session-other');

          // Switch back to session-focus via rows
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'back to session-focus' }],
          }));
          await page.waitForTimeout(50);
          assert.equal(await page.locator('.recovery-confirm-box').count(), 0, 'confirm box not re-opened on returning to session-focus');
          assert.equal(await page.locator('#say').inputValue(), '세션 포커스의 일반 초안', 'general draft untouched, no append occurred');

          // Clean up
          await page.locator('.recovery-item .delete-btn').click();
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 0');
          await recoveryBtn.click();
          await page.locator('#say').fill('');
        }
      }
    ]
  },
  {
    name: 'autocomplete',
    description: '자동완성',
    scenarios: [
      {
        id: 'autocomplete_late_after_mode_switch_ignored',
        name: '모드 전환 뒤 늦은 자동완성 무시 및 타이머 취소 (Condition 14)',
        run: async (page) => {
          // Setup active question for mode switching
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-ac-mode',
              what: '자동완성 모드 테스트 질문',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');

          // Step A: User in general mode types, suggest request fired after debounce
          await page.locator('#say').fill('myFunc');
          await page.waitForFunction(() => window.__posted.some(m => m.kind === 'suggest' && m.target === 'general'));
          const suggestMsg = await page.evaluate(() => window.__posted.filter(m => m.kind === 'suggest' && m.target === 'general').slice(-1)[0]);
          assert.ok(suggestMsg, 'suggest message was posted');
          assert.equal(suggestMsg.target, 'general', 'suggest target was general mode');

          // Step B: User switches to answer mode BEFORE suggestion arrives
          await page.locator('#ask-controls button:text("직접 입력")').click();
          assert.equal(await page.locator('#hint').textContent(), '', 'hint cleared on enterAnswerMode');
          await page.locator('#say').fill('');

          // Step C: Late suggestion for general mode arrives while in answer mode
          await page.evaluate((req) => window.postMessage({
            kind: 'suggestion',
            text: 'tion() { return 42; }',
            reqId: req.reqId,
            target: req.target
          }, '*'), suggestMsg);

          assert.equal(await page.locator('#hint').textContent(), '', 'late suggestion ignored after mode switch');
          await page.keyboard.press('Tab');
          assert.equal(await page.locator('#say').inputValue(), '', 'tab did not insert suggestion from previous mode');

          // Step D: User types in answer mode, exits mode, late suggestion for answer mode arrives in general mode
          await page.locator('#say').fill('answer');
          await page.waitForFunction(() => window.__posted.some(m => m.kind === 'suggest' && m.target === 'q-ac-mode'));
          const answerSuggestMsg = await page.evaluate(() => window.__posted.filter(m => m.kind === 'suggest' && m.target === 'q-ac-mode').slice(-1)[0]);
          assert.equal(answerSuggestMsg.target, 'q-ac-mode');

          // Exit answer mode
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#hint').textContent(), '', 'hint cleared on exitAnswerMode');

          // Late suggestion arrives for answer mode while now in general mode
          await page.evaluate((req) => window.postMessage({
            kind: 'suggestion',
            text: 'wer to question',
            reqId: req.reqId,
            target: req.target
          }, '*'), answerSuggestMsg);
          assert.equal(await page.locator('#hint').textContent(), '', 'late suggestion for answer mode ignored in general mode');
          await page.keyboard.press('Tab');
          assert.equal(await page.locator('#say').inputValue(), 'myFunc', 'tab did not append answer suggestion into general draft');
        }
      },
      {
        id: 'autocomplete_invalidated_on_send_and_choice',
        name: 'send() 및 선택지 클릭 시 자동완성 무효화 (Condition 15A, 15B)',
        run: async (page) => {
          await page.locator('#say').fill('general note');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-ac1',
              what: '자동완성 테스트 질문',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('answering');
          await page.waitForFunction(() => window.__posted.some(m => m.kind === 'suggest' && m.target === 'q-ac1'));
          const qAc1Suggest = await page.evaluate(() => window.__posted.filter(m => m.kind === 'suggest' && m.target === 'q-ac1').slice(-1)[0]);
          assert.ok(qAc1Suggest);
          await page.evaluate((req) => window.postMessage({
            kind: 'suggestion',
            text: 'wer for q1',
            reqId: req.reqId,
            target: req.target
          }, '*'), qAc1Suggest);
          assert.equal(await page.locator('#hint').textContent(), 'Tab: wer for q1');

          // Click Send to submit answer
          await page.locator('#send').click();
          assert.equal(await page.locator('#hint').textContent(), '');
          assert.equal(await page.locator('#say').inputValue(), 'general note');
          await page.keyboard.press('Tab');
          assert.equal(await page.locator('#say').inputValue(), 'general note', 'Tab did not insert leftover answer suggestion into general draft');

          // Clear in-flight reply for q-ac1
          const postedQAc1 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-ac1').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-ac1',
            attemptId: att.attemptId,
            ok: true,
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedQAc1);

          // 15B: Choice click also clears autocompletion
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('opt-draft');
          await page.waitForFunction(() => window.__posted.filter(m => m.kind === 'suggest' && m.target === 'q-ac1').length >= 2);
          const qAc2Suggest = await page.evaluate(() => window.__posted.filter(m => m.kind === 'suggest' && m.target === 'q-ac1').slice(-1)[0]);
          await page.evaluate((req) => window.postMessage({
            kind: 'suggestion',
            text: '-completion',
            reqId: req.reqId,
            target: req.target
          }, '*'), qAc2Suggest);
          assert.equal(await page.locator('#hint').textContent(), 'Tab: -completion');

          // Click choice button
          await page.locator('#ask-controls button:text("1. 선택 1")').click();
          assert.equal(await page.locator('#hint').textContent(), '');
          await page.keyboard.press('Tab');
          assert.equal(await page.locator('#say').inputValue(), 'general note', 'Tab after choice click did not insert leftover suggestion');
        }
      }
    ]
  },
  {
    name: 'diff',
    description: 'diff·파일 이동',
    scenarios: [
      {
        id: 'diff_permission_ask_dismissal',
        name: '권한 질문 diff 자체 스크롤바 없음, 승인 및 기각 처리 (Condition 7)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }],
            ask: {
              kind: 'permission',
              callId: 'call-perm-99',
              what: 'bash',
              args: 'git diff\n'.repeat(50),
              reason: 'check status',
              diff: 'diff --git a/f b/f\n+new line\n'.repeat(100)
            }
          }));

          await page.waitForSelector('#ask-body pre.diff');
          const diffScroll = await page.locator('#ask-body pre.diff').evaluate((el) => ({ height: el.clientHeight, scroll: el.scrollHeight }));
          assert.equal(diffScroll.height, diffScroll.scroll, 'diff has no separate vertical scrollbar');

          // Click allow
          await page.locator('#ask-controls button:text("allow")').click();
          const posted = await page.evaluate(() => window.__posted);
          assert.ok(posted.some((m) => m.kind === 'answer' && m.callId === 'call-perm-99' && m.decision === 'allow'));

          // Dismiss ask
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-body').hidden && document.getElementById('ask-controls').hidden);
          assert.equal(await page.locator('#ask-body').evaluate((el) => el.textContent), '');
          assert.equal(await page.locator('#ask-controls').evaluate((el) => el.textContent), '');
        }
      },
      {
        id: 'diff_syntax_highlighting_and_preservation',
        name: 'diff 구문 강조, 분류, 원문 보존, HTML 안전성 및 unparsed fallback (Condition 19)',
        run: async (page) => {
          const complexDiff = [
            'diff --git a/src/math.cpp b/src/math.cpp',
            'index e69de29..4b825dc 100644',
            '--- a/src/math.cpp',
            '+++ b/src/math.cpp',
            '@@ -1,7 +1,7 @@',
            ' void calc() {',
            '   int x = 10;',
            '---x;',
            '+++x;',
            '',
            '-  <div id="unescaped-old">old html</div>',
            '+  <div id="unescaped-new">new html</div>',
            ' }',
            '\\ No newline at end of file',
            'diff --git a/src/util.py b/src/util.py',
            '--- a/src/util.py',
            '+++ b/src/util.py',
            '@@ -1,2 +1,2 @@',
            '-old_code()',
            '+new_code()'
          ].join('\n');

          await page.evaluate(({ msg, diffText }) => {
            msg.ask.diff = diffText;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: {
                kind: 'permission',
                callId: 'perm-diff-test',
                what: 'git apply',
                args: 'patch.diff',
                reason: 'apply math and util updates',
              }
            }),
            diffText: complexDiff
          });

          await page.waitForSelector('#ask-body pre.diff .diff-line');

          // Classification checks
          const fileHeaders = await page.locator('#ask-body pre.diff .diff-file-header').allInnerTexts();
          assert.ok(fileHeaders.length >= 6);
          assert.ok(fileHeaders[0].includes('diff --git a/src/math.cpp'));
          assert.ok(fileHeaders[2].includes('--- a/src/math.cpp'));
          assert.ok(fileHeaders[3].includes('+++ b/src/math.cpp'));
          assert.ok(fileHeaders[4].includes('diff --git a/src/util.py'));

          const hunkHeaders = await page.locator('#ask-body pre.diff .diff-hunk-header').allInnerTexts();
          assert.equal(hunkHeaders.length, 2);
          assert.ok(hunkHeaders[0].includes('@@ -1,7 +1,7 @@'));
          assert.ok(hunkHeaders[1].includes('@@ -1,2 +1,2 @@'));

          const deletedLines = await page.locator('#ask-body pre.diff .diff-deleted').allInnerTexts();
          assert.ok(deletedLines.some(t => t.includes('---x;')), '---x; is classified as diff-deleted, not file header');
          assert.ok(deletedLines.some(t => t.includes('<div id="unescaped-old">')), 'deleted html line is diff-deleted');
          assert.ok(deletedLines.some(t => t.includes('old_code()')));

          const addedLines = await page.locator('#ask-body pre.diff .diff-added').allInnerTexts();
          assert.ok(addedLines.some(t => t.includes('+++x;')), '+++x; is classified as diff-added, not file header');
          assert.ok(addedLines.some(t => t.includes('<div id="unescaped-new">')), 'added html line is diff-added');
          assert.ok(addedLines.some(t => t.includes('new_code()')));

          const contextLines = await page.locator('#ask-body pre.diff .diff-context').allInnerTexts();
          assert.ok(contextLines.some(t => t.includes('void calc()')));
          assert.ok(contextLines.some(t => t.includes('\\ No newline at end of file')));

          // Raw text exact match
          const renderedText = await page.locator('#ask-body pre.diff').evaluate(el => el.textContent);
          assert.equal(renderedText, complexDiff, 'diff textContent exactly equals the input raw diff');

          // HTML escape security
          const unescapedDiv = await page.locator('#unescaped-old').count();
          assert.equal(unescapedDiv, 0, 'HTML tag inside diff was not executed/injected as a DOM element');

          // No vertical scrollbar
          const diffScrollBounds = await page.locator('#ask-body pre.diff').evaluate((el) => ({ height: el.clientHeight, scroll: el.scrollHeight }));
          assert.equal(diffScrollBounds.height, diffScrollBounds.scroll, 'diff has no separate vertical scrollbar');

          // Theme style distinction
          const addedBg = await page.locator('#ask-body pre.diff .diff-added').first().evaluate(el => getComputedStyle(el).backgroundColor);
          const deletedBg = await page.locator('#ask-body pre.diff .diff-deleted').first().evaluate(el => getComputedStyle(el).backgroundColor);
          assert.notEqual(addedBg, 'rgba(0, 0, 0, 0)', 'added line has non-transparent background');
          assert.notEqual(deletedBg, 'rgba(0, 0, 0, 0)', 'deleted line has non-transparent background');
          assert.notEqual(addedBg, deletedBg, 'added and deleted backgrounds are visually distinct');

          // Diff without trailing newline
          const noTrailingNlDiff = 'diff --git a/f b/f\n@@ -1 +1 @@\n-old\n+new';
          await page.evaluate(({ msg, diffText }) => {
            msg.ask.diff = diffText;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: { kind: 'permission', callId: 'perm-no-nl', what: 'test' }
            }),
            diffText: noTrailingNlDiff
          });
          await page.waitForSelector('#ask-body pre.diff .diff-line');
          const noNlRendered = await page.locator('#ask-body pre.diff').evaluate(el => el.textContent);
          assert.equal(noNlRendered, noTrailingNlDiff, 'diff without trailing newline matches textContent exactly');

          // Unparsed fallback
          const unparsedDiff = 'Custom raw patch metadata without diff markers\nsome random text';
          await page.evaluate(({ msg, diffText }) => {
            msg.ask.diff = diffText;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: { kind: 'permission', callId: 'perm-unparsed', what: 'test' }
            }),
            diffText: unparsedDiff
          });
          await page.waitForSelector('#ask-body pre.diff .diff-plain');
          const unparsedRendered = await page.locator('#ask-body pre.diff').evaluate(el => el.textContent);
          assert.equal(unparsedRendered, unparsedDiff, 'unparsed text preserved in raw form');

          await page.locator('#ask-controls button:text("allow")').click();
        }
      },
      {
        id: 'diff_hunk_line_count_and_multifile',
        name: 'Hunk 줄 수 추적, --- a/example diff-deleted/added 분류, a/ b/ 없는 다중 파일 (Condition 20)',
        run: async (page) => {
          const countTrackDiff = [
            '@@ -1 +1 @@',
            '--- a/example',
            '+++ b/example',
            '--- file2.txt',
            '+++ file2.txt',
            '@@ -1 +1 @@',
            '-foo',
            '+bar'
          ].join('\n');

          await page.evaluate(({ msg, diffText }) => {
            msg.ask.diff = diffText;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: { kind: 'permission', callId: 'perm-count-diff', what: 'git apply' }
            }),
            diffText: countTrackDiff
          });

          await page.waitForFunction(() => document.querySelector('#ask-body pre.diff')?.textContent.includes('--- a/example'));

          const countDeleted = await page.locator('#ask-body pre.diff .diff-deleted').allInnerTexts();
          assert.ok(countDeleted.some(t => t.includes('--- a/example')), '--- a/example in hunk is classified as diff-deleted');
          assert.ok(countDeleted.some(t => t.includes('-foo')), '-foo is classified as diff-deleted');

          const countAdded = await page.locator('#ask-body pre.diff .diff-added').allInnerTexts();
          assert.ok(countAdded.some(t => t.includes('+++ b/example')), '+++ b/example in hunk is classified as diff-added');
          assert.ok(countAdded.some(t => t.includes('+bar')), '+bar is classified as diff-added');

          const countFileHeaders = await page.locator('#ask-body pre.diff .diff-file-header').allInnerTexts();
          assert.ok(countFileHeaders.some(t => t.includes('--- file2.txt')), '--- file2.txt recognized as file header');
          assert.ok(countFileHeaders.some(t => t.includes('+++ file2.txt')), '+++ file2.txt recognized as file header');

          const countRendered = await page.locator('#ask-body pre.diff').evaluate(el => el.textContent);
          assert.equal(countRendered, countTrackDiff, 'textContent of countTrackDiff matches input exactly');

          await page.locator('#ask-controls button:text("allow")').click();
        }
      },
      {
        id: 'diff_approval_panel_and_inspection_click',
        name: '승인 패널 대상 파일명, 설명, 변경 보기 버튼 및 순수 조회 클릭 (Condition 21)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-native',
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'permission',
              callId: 'perm-edit-native',
              diffKind: 'sides',
              what: 'edit',
              filePath: 'src/model/user.ts',
              args: JSON.stringify({ path: 'src/model/user.ts', old: 'const a = 1;\n', new: 'const a = 2;\n' }),
              reason: 'update user version property'
            }
          }));

          await page.waitForFunction(() => document.querySelector('#ask-controls .summary-text')?.textContent.includes('user.ts'));
          const sumTextNative = await page.locator('#ask-controls .summary-text').textContent();
          assert.ok(sumTextNative.includes('user.ts'), 'summary row includes target filename');
          assert.ok(sumTextNative.includes('edit'), 'summary row includes what');

          const fileTarget = await page.locator('#ask-body .file-target').textContent();
          assert.equal(fileTarget, '파일: src/model/user.ts', 'target file path is displayed in ask body');

          const actsBtns = await page.locator('#ask-controls .acts button').allTextContents();
          assert.deepEqual(actsBtns, ['변경 보기', 'allow', 'deny', 'always'], 'actions include 변경 보기 and approval buttons');

          // Clicking '변경 보기' posts kind: 'diff' without approving
          const postedLenBefore = await page.evaluate(() => window.__posted.length);
          await page.locator('#ask-controls button:text("변경 보기")').click();
          const postedAfterDiff = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterDiff.length, postedLenBefore + 1, 'posted exactly one message on diff click');
          const lastPosted = postedAfterDiff[postedAfterDiff.length - 1];
          assert.deepEqual(lastPosted, { kind: 'diff', session: 'sess-perm-native', callId: 'perm-edit-native' });
          assert.ok(!postedAfterDiff.some(m => m.kind === 'answer' && m.callId === 'perm-edit-native'), 'diff click does not approve');

          // Clicking target file in approval body posts kind: 'open'
          await page.locator('#ask-body .file-target button.file-nav-btn').click();
          const postedAfterOpen = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterOpen.length, postedAfterDiff.length + 1, 'posted exactly one message on open file click');
          const openPosted = postedAfterOpen[postedAfterOpen.length - 1];
          assert.deepEqual(openPosted, { kind: 'open', session: 'sess-perm-native', callId: 'perm-edit-native' });
          assert.ok(!postedAfterOpen.some(m => m.kind === 'answer' && m.callId === 'perm-edit-native'), 'open file click does not approve');

          // Raw patch permission also shows 변경 보기
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-native',
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'permission',
              callId: 'perm-patch-native',
              diffKind: 'patch',
              what: 'write',
              filePath: 'README.md',
              args: JSON.stringify({ path: 'README.md' }),
              diff: '--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-# Old\n+# New\n'
            }
          }));
          await page.waitForFunction(() => document.querySelector('#ask-controls .summary-text')?.textContent.includes('README.md'));
          await page.locator('#ask-controls button:text("변경 보기")').click();
          const postedPatchDiff = await page.evaluate(() => window.__posted);
          assert.ok(postedPatchDiff.some(m => m.kind === 'diff' && m.callId === 'perm-patch-native'), 'patch shows and triggers diff');

          // Permission with diffKind: 'none' (e.g. replaceAll: 'TRUE') never shows phantom 변경 보기 button
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-native',
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'permission',
              callId: 'perm-edit-rejected',
              diffKind: 'none',
              what: 'edit',
              filePath: 'src/config.ts',
              args: JSON.stringify({ path: 'src/config.ts', old: 'a', new: 'b', replaceAll: 'TRUE' })
            }
          }));
          await page.waitForFunction(() => document.querySelector('#ask-controls .summary-text')?.textContent.includes('config.ts'));
          const rejectedBtns = await page.locator('#ask-controls .acts button').allTextContents();
          assert.deepEqual(rejectedBtns, ['allow', 'deny', 'always'], 'no phantom 변경 보기 button when host flags diffKind as none');
        }
      },
      {
        id: 'diff_tool_row_navigation_and_command_args',
        name: '도구 행 파일 및 줄 이동 링크, 명령 행 일반 인자 (Condition 22)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-42',
            rows: [
              { who: 'tool', label: 'read ✓', text: 'read', seq: 42, callId: 'c-42', fileNav: { path: 'src/app.ts', line: 15 }, args: '{"limit":30,"offset":15,"path":"src/app.ts"}' },
              { who: 'tool', label: 'bash ✓', text: 'bash', seq: 43, args: 'npm test' }
            ],
            ask: null
          }));
          await page.waitForSelector('#rows .row.tool');

          const fileRowBtn = page.locator('#rows .row.tool button.file-nav-btn');
          await fileRowBtn.waitFor();
          const fileBtnText = await fileRowBtn.textContent();
          assert.equal(fileBtnText, 'src/app.ts:15', 'tool row renders fileNav with line');

          const toolArgs = await page.locator('#rows .row.tool:nth-child(1) span.args').textContent();
          assert.ok(toolArgs.includes('limit'), 'args remains accessible alongside fileNav button');
          assert.ok(toolArgs.includes('15'), 'offset remains accessible');

          // Click tool row file button
          const postedBeforeRowClick = await page.evaluate(() => window.__posted.length);
          await fileRowBtn.click();
          const postedAfterRowClick = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterRowClick.length, postedBeforeRowClick + 1, 'posted one message on tool row file click');
          const rowPosted = postedAfterRowClick[postedAfterRowClick.length - 1];
          assert.deepEqual(rowPosted, { kind: 'open', seq: 42, session: 'sess-42', callId: 'c-42' });

          // Bash row has plain args span
          const bashArgs = await page.locator('#rows .row.tool:nth-child(2) span.args').textContent();
          assert.equal(bashArgs, 'npm test', 'command tool renders plain args');
          const bashBtnCount = await page.locator('#rows .row.tool:nth-child(2) button.file-nav-btn').count();
          assert.equal(bashBtnCount, 0, 'command tool has no file-nav-btn');
        }
      },
      {
        id: 'diff_long_args_raw_toggle_expansion',
        name: '긴 인자 rawArgs 접기/펼치기, END_OF_NEW 보존, 추가 스크롤 컨테이너 없음 (Condition 23)',
        run: async (page) => {
          const longOldText = 'x'.repeat(150);
          const rawEditArgs = JSON.stringify({ path: 'src/main.ts', old: longOldText, new: 'END_OF_NEW' }, null, 2);
          const summaryEditArgs = '{"path":"src/main.ts","old":"' + 'x'.repeat(70) + '…';

          await page.evaluate(({ msg, rawArgs, summaryArgs }) => {
            msg.rows[0].rawArgs = rawArgs;
            msg.rows[0].args = summaryArgs;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              session: 'sess-toggle-test',
              rows: [{
                who: 'tool',
                label: 'edit ✓',
                text: 'edit',
                seq: 50,
                callId: 'c-50',
                fileNav: { path: 'src/main.ts' }
              }],
              ask: null
            }),
            rawArgs: rawEditArgs,
            summaryArgs: summaryEditArgs
          });

          await page.waitForSelector('.row.tool .args-toggle-btn');
          const toggleBtn = page.locator('.row.tool .args-toggle-btn');
          const rawBox = page.locator('.row.tool pre.raw-args');

          // Initially folded
          assert.equal(await rawBox.isHidden(), true, 'raw-args block is initially hidden');
          assert.equal(await toggleBtn.getAttribute('aria-expanded'), 'false');

          // Expand
          await toggleBtn.click();
          assert.equal(await rawBox.isVisible(), true, 'raw-args block is visible after toggle click');
          assert.equal(await toggleBtn.getAttribute('aria-expanded'), 'true');
          const renderedRaw = await rawBox.textContent();
          assert.ok(renderedRaw.includes(longOldText), 'raw-args preserves 150-character old argument');
          assert.ok(renderedRaw.includes('END_OF_NEW'), 'raw-args preserves END_OF_NEW to the very end');

          // No separate vertical scroll container
          const rawBounds = await rawBox.evaluate(el => ({ height: el.clientHeight, scroll: el.scrollHeight }));
          assert.equal(rawBounds.height, rawBounds.scroll, 'raw-args has no internal vertical scroll container');

          // Fold again
          await toggleBtn.click();
          assert.equal(await rawBox.isHidden(), true, 'raw-args is folded again after second click');
        }
      },
      {
        id: 'diff_unconfirmed_session_and_missing_filepath',
        name: '미확인 세션 버튼 비활성화, filePath 누락 시 가상 버튼 없음 (Condition 24)',
        run: async (page) => {
          // Explicitly set empty session to test unconfirmed session contract
          await page.evaluate(() => window.postMessage({
            kind: 'rows',
            session: '',
            rows: [
              { who: 'tool', label: 'edit ✓', text: 'edit', seq: 60, callId: 'c-60', fileNav: { path: 'src/test.ts' }, args: 'src/test.ts' }
            ],
            ask: {
              kind: 'permission',
              callId: 'perm-no-filepath',
              what: 'edit',
              args: JSON.stringify({ path: 'src/phantom.ts', old: '1', new: '2' })
            },
            refs: []
          }, '*'));

          await page.waitForSelector('.row.tool button.file-nav-btn:has-text("src/test.ts")');
          const unconfirmedToolBtn = page.locator('.row.tool button.file-nav-btn:has-text("src/test.ts")');
          assert.equal(await unconfirmedToolBtn.isDisabled(), true, 'tool row button is disabled when session is unconfirmed');

          const phantomBtnCount = await page.locator('#ask-body .file-target').count();
          assert.equal(phantomBtnCount, 0, 'webview does not create file-target when host omits filePath');
        }
      },
      {
        id: 'diff_render_time_session_binding',
        name: '렌더 시점 세션 클로저 바인딩 (Condition 25)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-original',
            rows: [
              { who: 'tool', label: 'edit ✓', text: 'edit', seq: 70, callId: 'c-70', fileNav: { path: 'src/frozen.ts' }, args: 'src/frozen.ts' }
            ],
            ask: {
              kind: 'permission',
              callId: 'perm-frozen',
              filePath: 'src/frozen_ask.ts',
              what: 'edit'
            }
          }));

          await page.waitForSelector('.row.tool button.file-nav-btn:has-text("src/frozen.ts")');
          const frozenToolBtn = page.locator('.row.tool button.file-nav-btn:has-text("src/frozen.ts")');
          const frozenAskBtn = page.locator('#ask-body .file-target button.file-nav-btn');

          await frozenToolBtn.click();
          const postedAfterFrozenTool = await page.evaluate(() => window.__posted);
          const toolMsg = postedAfterFrozenTool[postedAfterFrozenTool.length - 1];
          assert.equal(toolMsg.session, 'sess-original', 'tool button click preserved bound session from render time');

          await frozenAskBtn.click();
          const postedAfterFrozenAsk = await page.evaluate(() => window.__posted);
          const askMsg = postedAfterFrozenAsk[postedAfterFrozenAsk.length - 1];
          assert.equal(askMsg.session, 'sess-original', 'ask button click preserved bound session from render time');
        }
      },
      {
        id: 'diff_redraw_retention_and_session_switch',
        name: 'rawArgs 펼침 및 버튼 포커스 redraw 간 보존, 세션 전환 시 초기화 및 격리 (Condition 26, 27)',
        run: async (page) => {
          const longText = 'k'.repeat(120);
          await page.evaluate(({ msg, longText }) => {
            msg.rows[0].rawArgs = JSON.stringify({ path: 'src/persist.ts', old: longText, new: 'END_OF_NEW' });
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              session: 'sess-persisted',
              rows: [{
                who: 'tool',
                label: 'edit ✓',
                text: 'edit',
                seq: 80,
                callId: 'c-80-persisted',
                fileNav: { path: 'src/persist.ts' },
                args: 'src/persist.ts {"old":"clipped..."}'
              }],
              ask: null
            }),
            longText
          });

          await page.waitForSelector('.row.tool .args-toggle-btn[data-call-id="c-80-persisted"]');
          const persistToggle = page.locator('.row.tool .args-toggle-btn[data-call-id="c-80-persisted"]');
          const persistRaw = page.locator('.row.tool pre.raw-args');

          // Initially hidden
          assert.equal(await persistRaw.isHidden(), true);

          // Expand and focus
          await persistToggle.click();
          assert.equal(await persistRaw.isVisible(), true);
          await persistToggle.focus();
          const focusedBeforeRedraw = await page.evaluate(() => document.activeElement?.dataset?.callId);
          assert.equal(focusedBeforeRedraw, 'c-80-persisted', 'toggle button is focused before redraw');

          // Redraw with new event added
          await page.evaluate(({ msg, longText }) => {
            msg.rows[0].rawArgs = JSON.stringify({ path: 'src/persist.ts', old: longText, new: 'END_OF_NEW' });
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              session: 'sess-persisted',
              rows: [
                {
                  who: 'tool',
                  label: 'edit ✓',
                  text: 'edit',
                  seq: 80,
                  callId: 'c-80-persisted',
                  fileNav: { path: 'src/persist.ts' },
                  args: 'src/persist.ts {"old":"clipped..."}'
                },
                {
                  who: 'tool',
                  label: 'bash ✓',
                  text: 'bash',
                  seq: 81,
                  callId: 'c-81-new',
                  args: 'echo done'
                }
              ],
              ask: null
            }),
            longText
          });

          await page.waitForSelector('.row.tool:has-text("echo done")');
          const persistRawAfter = page.locator('.row.tool pre.raw-args');
          assert.equal(await persistRawAfter.isVisible(), true, 'raw-args remains visible after redraw');
          const rawTextAfter = await persistRawAfter.textContent();
          assert.ok(rawTextAfter.includes('END_OF_NEW'), 'raw-args still displays unclipped content to END_OF_NEW');

          const focusedAfterRedraw = await page.evaluate(() => document.activeElement?.dataset?.callId);
          assert.equal(focusedAfterRedraw, 'c-80-persisted', 'focus was restored to the toggle button after redraw');

          // Condition 27: Session switch isolates and clears expanded state
          await page.evaluate(({ msg, longText }) => {
            msg.rows[0].rawArgs = JSON.stringify({ path: 'src/persist.ts', old: longText, new: 'END_OF_NEW' });
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              session: 'sess-brand-new',
              rows: [{
                who: 'tool',
                label: 'edit ✓',
                text: 'edit',
                seq: 80,
                callId: 'c-80-persisted', // identical callId in new session
                fileNav: { path: 'src/persist.ts' },
                args: 'src/persist.ts {"old":"clipped..."}'
              }],
              ask: null
            }),
            longText
          });

          await page.waitForSelector('.row.tool .args-toggle-btn[data-call-id="c-80-persisted"]');
          const newSessionRaw = page.locator('.row.tool pre.raw-args');
          const newSessionToggle = page.locator('.row.tool .args-toggle-btn[data-call-id="c-80-persisted"]');
          assert.equal(await newSessionRaw.isHidden(), true, 'raw-args is folded again in brand new session');
          assert.equal(await newSessionToggle.getAttribute('aria-expanded'), 'false');
        }
      },
      {
        id: 'diff_button_session_binding_and_unconfirmed_disable',
        name: 'diff 버튼 세션 바인딩 및 미확인 세션 비활성화 (Condition 28)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-diff-bound',
            rows: [],
            ask: {
              kind: 'permission',
              callId: 'perm-diff-test',
              diffKind: 'sides',
              what: 'edit',
              filePath: 'src/diff_target.ts'
            }
          }));

          await page.waitForSelector('button.diff-btn');
          const diffBtn = page.locator('button.diff-btn');
          assert.equal(await diffBtn.isDisabled(), false);
          await diffBtn.click();
          const postedAfterDiffCond28 = await page.evaluate(() => window.__posted);
          const diffMsg = postedAfterDiffCond28[postedAfterDiffCond28.length - 1];
          assert.equal(diffMsg.kind, 'diff');
          assert.equal(diffMsg.session, 'sess-diff-bound');
          assert.equal(diffMsg.callId, 'perm-diff-test');

          // Unconfirmed session on diffBtn (explicitly empty session)
          await page.evaluate(() => window.postMessage({
            kind: 'rows',
            session: '',
            rows: [],
            ask: {
              kind: 'permission',
              callId: 'perm-diff-no-sess',
              diffKind: 'sides',
              what: 'edit'
            },
            refs: []
          }, '*'));

          await page.waitForSelector('button.diff-btn:disabled');
          const diffBtnUnconfirmed = page.locator('button.diff-btn');
          assert.equal(await diffBtnUnconfirmed.isDisabled(), true, 'diff button is disabled when session is unconfirmed');
        }
      },
      {
        id: 'output_open_button_session_binding_and_action_dispatch',
        name: '편집창에서 열기 버튼 세션 바인딩, 액션 전송 및 미확인 세션 비활성화 (§3.3, §3.4)',
        run: async (page) => {
          // 1. Deliver confirmed session with assistant row (with outputId), tool row (with outputId), and draft row (no outputId)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-output-bound',
            rows: [
              { who: 'agent', label: 'magi', text: 'finalized assistant answer', outputId: 'assistant:42' },
              { who: 'agent', label: 'magi', text: 'draft streaming answer', pending: true },
              { who: 'tool', label: 'tool (read_file)', text: 'read_file', callId: 'call-read-1', outputId: 'tool:call-read-1:45' }
            ],
            refs: []
          }));

          await page.waitForSelector('.row.agent .output-open-btn');
          const agentBtn = page.locator('.row.agent .output-open-btn');
          assert.equal(await agentBtn.count(), 1, 'only finalized assistant row has output open button');
          assert.equal(await agentBtn.textContent(), '편집창에서 열기');
          assert.equal(await agentBtn.isDisabled(), false, 'button is enabled when session is confirmed');

          await page.waitForSelector('.row.tool .output-open-btn');
          const toolBtn = page.locator('.row.tool .output-open-btn');
          assert.equal(await toolBtn.count(), 1, 'tool row with outputId has output open button');
          assert.equal(await toolBtn.textContent(), '편집창에서 열기');
          assert.equal(await toolBtn.isDisabled(), false);

          // Click assistant button: verify postMessage sends kind: 'output' with exact session and outputId
          const postedLenBefore = await page.evaluate(() => window.__posted.length);
          await agentBtn.click();
          const postedAfterAgent = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterAgent.length, postedLenBefore + 1, 'exactly one message dispatched');
          const agentMsg = postedAfterAgent[postedAfterAgent.length - 1];
          assert.deepEqual(agentMsg, {
            kind: 'output',
            session: 'sess-output-bound',
            outputId: 'assistant:42'
          }, 'correct output action payload dispatched for assistant');

          // Click tool button: verify postMessage sends kind: 'output' with tool outputId
          await toolBtn.click();
          const postedAfterTool = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterTool.length, postedLenBefore + 2, 'second message dispatched');
          const toolMsg = postedAfterTool[postedAfterTool.length - 1];
          assert.deepEqual(toolMsg, {
            kind: 'output',
            session: 'sess-output-bound',
            outputId: 'tool:call-read-1:45'
          }, 'correct output action payload dispatched for tool');

          // Confirm neither click touched composer or ask mode
          const sayValue = await page.locator('#say').inputValue();
          assert.equal(sayValue, '', 'composer input untouched');
          const askControlsHidden = await page.locator('#ask-controls').evaluate((el) => el.hidden);
          assert.ok(askControlsHidden, 'ask controls remain hidden');

          // 2. Deliver unconfirmed session (session: '') with outputId
          await page.evaluate(() => window.postMessage({
            kind: 'rows',
            session: '',
            rows: [
              { who: 'agent', label: 'magi', text: 'unconfirmed assistant', outputId: 'assistant:99' }
            ],
            refs: []
          }, '*'));

          await page.waitForSelector('.row.agent .output-open-btn:disabled');
          const disabledBtn = page.locator('.row.agent .output-open-btn');
          assert.equal(await disabledBtn.isDisabled(), true, 'output button is disabled when session is unconfirmed');
        }
      },
      {
        id: 'diff_approval_and_inspection_styling_keyboard_and_themes',
        name: '승인 패널 조회 보조 조작 스타일, 승인 분리, 키보드 접근 및 테마/뷰포트 가림 검증 (§5.6)',
        run: async (page) => {
          // 1. Deliver permission ask with diff and file target
          const permAsk = {
            kind: 'permission',
            callId: 'perm-inspect-56',
            diffKind: 'sides',
            what: 'edit src/auth.ts',
            filePath: 'src/auth.ts',
            args: JSON.stringify({ path: 'src/auth.ts', old: 'var token = ""', new: 'const token = "secure"' }),
            reason: 'harden auth token security',
            diff: '--- a/src/auth.ts\n+++ b/src/auth.ts\n@@ -1 +1 @@\n-var token = ""\n+const token = "secure"\n'
          };
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [{ who: 'agent', label: 'magi', text: '승인 요청' }],
            ask: permAsk
          }));

          await page.waitForSelector('#ask-controls .acts');
          const diffBtn = page.locator('#ask-controls .acts button.diff-btn');
          await diffBtn.waitFor();
          const openBtn = page.locator('#ask-body .file-target button.file-nav-btn');
          await openBtn.waitFor();

          const allowBtn = page.locator('#ask-controls .acts button:text("allow")');
          const denyBtn = page.locator('#ask-controls .acts button:text("deny")');
          const alwaysBtn = page.locator('#ask-controls .acts button:text("always")');

          // 2. Assert inspection vs approval classes
          assert.equal(await diffBtn.evaluate((el) => el.classList.contains('inspect-btn')), true, 'diff button has inspect-btn class');
          assert.equal(await diffBtn.evaluate((el) => el.classList.contains('approval-btn')), false, 'diff button does not have approval-btn class');
          assert.equal(await openBtn.evaluate((el) => el.classList.contains('inspect-btn')), true, 'file nav button has inspect-btn class');
          assert.equal(await openBtn.evaluate((el) => el.classList.contains('approval-btn')), false, 'file nav button does not have approval-btn class');

          for (const btn of [allowBtn, denyBtn, alwaysBtn]) {
            assert.equal(await btn.evaluate((el) => el.classList.contains('approval-btn')), true, 'decision button has approval-btn class');
            assert.equal(await btn.evaluate((el) => el.classList.contains('inspect-btn')), false, 'decision button does not have inspect-btn class');
          }

          // 3. Inspection clicks dispatch open / diff only (0 answer, 0 reply, 0 say)
          const postedBefore = await page.evaluate(() => window.__posted.length);
          await openBtn.click();
          await diffBtn.click();
          const postedAfterInspect = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterInspect.length, postedBefore + 2, 'inspection clicks dispatched 2 messages');
          assert.deepEqual(postedAfterInspect[postedBefore], { kind: 'open', session: 'sess-perm-56', callId: 'perm-inspect-56' });
          assert.deepEqual(postedAfterInspect[postedBefore + 1], { kind: 'diff', session: 'sess-perm-56', callId: 'perm-inspect-56' });
          assert.ok(!postedAfterInspect.slice(postedBefore).some((m) => m.kind === 'answer' || m.kind === 'reply' || m.kind === 'say'), 'inspection clicks never answer/reply/say');

          // 4. Keyboard Tab / Enter / Space navigation & decision dispatch
          // 4A. Focus openBtn and press Enter -> kind: 'open'
          await openBtn.focus();
          await page.keyboard.press('Enter');
          const postedAfterKbOpen = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterKbOpen[postedAfterKbOpen.length - 1].kind, 'open');

          // 4B. Focus diffBtn and press Enter -> kind: 'diff'
          await diffBtn.focus();
          await page.keyboard.press('Enter');
          const postedAfterKbDiff = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterKbDiff[postedAfterKbDiff.length - 1].kind, 'diff');

          // 4C. Focus allowBtn and press Space -> kind: 'answer', decision: 'allow' (exactly 1)
          await allowBtn.focus();
          await page.keyboard.press('Space');
          const postedAfterAllow = await page.evaluate(() => window.__posted);
          const allowMsg = postedAfterAllow[postedAfterAllow.length - 1];
          assert.deepEqual(allowMsg, { kind: 'answer', callId: 'perm-inspect-56', decision: 'allow' });

          // 4D. Deliver next ask and test deny with Enter key
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [{ who: 'agent', label: 'magi', text: '두 번째 승인 요청' }],
            ask: { ...permAsk, callId: 'perm-inspect-57' }
          }));
          await page.waitForSelector('#ask-controls .acts');
          const denyBtn2 = page.locator('#ask-controls .acts button:text("deny")');
          await denyBtn2.focus();
          await page.keyboard.press('Enter');
          const postedAfterDeny = await page.evaluate(() => window.__posted);
          const denyMsg = postedAfterDeny[postedAfterDeny.length - 1];
          assert.deepEqual(denyMsg, { kind: 'answer', callId: 'perm-inspect-57', decision: 'deny' });

          // 4E. Deliver third ask and test always with Space key
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [{ who: 'agent', label: 'magi', text: '세 번째 승인 요청' }],
            ask: { ...permAsk, callId: 'perm-inspect-58' }
          }));
          await page.waitForSelector('#ask-controls .acts');
          const alwaysBtn3 = page.locator('#ask-controls .acts button:text("always")');
          await alwaysBtn3.focus();
          await page.keyboard.press('Space');
          const postedAfterAlways = await page.evaluate(() => window.__posted);
          const alwaysMsg = postedAfterAlways[postedAfterAlways.length - 1];
          assert.deepEqual(alwaysMsg, { kind: 'answer', callId: 'perm-inspect-58', decision: 'always' });

          // Deliver final ask for theme & viewport inspection
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [{ who: 'agent', label: 'magi', text: '테마 측정 승인 요청' }],
            ask: { ...permAsk, callId: 'perm-inspect-59' }
          }));
          await page.waitForSelector('#ask-controls .acts');
          const activeDiff = page.locator('#ask-controls .acts button.diff-btn');
          const activeAllow = page.locator('#ask-controls .acts button:text("allow")');
          const activeDeny = page.locator('#ask-controls .acts button:text("deny")');
          const activeAlways = page.locator('#ask-controls .acts button:text("always")');

          // 5. Theme tokens verification: Dark, Light, High Contrast
          // 5A. Dark theme variables
          await page.evaluate(() => {
            document.documentElement.style.setProperty('--vscode-button-background', '#0e639c');
            document.documentElement.style.setProperty('--vscode-button-foreground', '#ffffff');
            document.documentElement.style.setProperty('--vscode-button-secondaryBackground', '#3a3d41');
            document.documentElement.style.setProperty('--vscode-button-secondaryForeground', '#ffffff');
            document.documentElement.style.setProperty('--vscode-focusBorder', '#007fd4');
            document.documentElement.style.removeProperty('--vscode-contrastBorder');
          });

          const darkDiffBg = await activeDiff.evaluate((el) => window.getComputedStyle(el).backgroundColor);
          const darkAllowBg = await activeAllow.evaluate((el) => window.getComputedStyle(el).backgroundColor);
          const darkDenyBg = await activeDeny.evaluate((el) => window.getComputedStyle(el).backgroundColor);
          const darkAlwaysBg = await activeAlways.evaluate((el) => window.getComputedStyle(el).backgroundColor);

          assert.equal(darkDiffBg, 'rgb(58, 61, 65)', 'diff secondary background matches dark token #3a3d41');
          assert.equal(darkAllowBg, 'rgb(14, 99, 156)', 'allow primary background matches dark token #0e639c');
          assert.equal(darkDenyBg, 'rgb(14, 99, 156)', 'deny has same primary background as allow (no danger red)');
          assert.equal(darkAlwaysBg, 'rgb(14, 99, 156)', 'always has same primary background as allow');

          // 5B. Light theme variables
          await page.evaluate(() => {
            document.documentElement.style.setProperty('--vscode-button-background', '#005fb8');
            document.documentElement.style.setProperty('--vscode-button-foreground', '#ffffff');
            document.documentElement.style.setProperty('--vscode-button-secondaryBackground', '#e5e5e5');
            document.documentElement.style.setProperty('--vscode-button-secondaryForeground', '#3b3b3b');
          });

          const lightDiffBg = await activeDiff.evaluate((el) => window.getComputedStyle(el).backgroundColor);
          const lightAllowBg = await activeAllow.evaluate((el) => window.getComputedStyle(el).backgroundColor);
          assert.equal(lightDiffBg, 'rgb(229, 229, 229)', 'diff secondary background matches light token #e5e5e5');
          assert.equal(lightAllowBg, 'rgb(0, 95, 184)', 'allow primary background matches light token #005fb8');

          // 5C. High contrast variables & border
          await page.evaluate(() => {
            document.documentElement.style.setProperty('--vscode-contrastBorder', '#6fc1ff');
            document.documentElement.style.setProperty('--vscode-focusBorder', '#007fd4');
          });

          const hcBorderColor = await activeDiff.evaluate((el) => window.getComputedStyle(el).borderColor);
          assert.equal(hcBorderColor, 'rgb(111, 193, 255)', 'contrastBorder is applied to secondary diff button');
          const hcAllowBorderColor = await activeAllow.evaluate((el) => window.getComputedStyle(el).borderColor);
          assert.equal(hcAllowBorderColor, 'rgb(111, 193, 255)', 'contrastBorder is applied to primary approval button');

          // 6. Viewports testing: 320x600 and 420x700
          for (const [vpW, vpH] of [[320, 600], [420, 700]]) {
            await page.setViewportSize({ width: vpW, height: vpH });
            const controlsRect = await page.locator('#ask-controls').evaluate((el) => {
              const r = el.getBoundingClientRect();
              return { width: r.width, height: r.height, right: r.right, bottom: r.bottom };
            });
            assert.ok(controlsRect.width > 0 && controlsRect.height > 0, 'ask controls is visible');
            assert.ok(controlsRect.right <= vpW, `ask controls does not overflow right at ${vpW}x${vpH}`);
            assert.ok(controlsRect.bottom <= vpH, `ask controls does not overflow bottom at ${vpW}x${vpH}`);

            for (const [name, locator] of [['diff', activeDiff], ['allow', activeAllow], ['deny', activeDeny], ['always', activeAlways]]) {
              const rect = await locator.evaluate((el) => {
                const r = el.getBoundingClientRect();
                return { width: r.width, height: r.height, right: r.right, bottom: r.bottom };
              });
              assert.ok(rect.width > 0 && rect.height > 0, `${name} button has positive dimensions`);
              assert.ok(rect.right <= vpW, `${name} button does not overflow horizontally at ${vpW}x${vpH}`);
              assert.ok(rect.bottom <= vpH, `${name} button does not overflow vertically at ${vpW}x${vpH}`);
            }
          }

          // Clean up styles and restore default viewport
          await page.evaluate(() => {
            document.documentElement.style.removeProperty('--vscode-contrastBorder');
            document.documentElement.style.removeProperty('--vscode-button-background');
            document.documentElement.style.removeProperty('--vscode-button-foreground');
            document.documentElement.style.removeProperty('--vscode-button-secondaryBackground');
            document.documentElement.style.removeProperty('--vscode-button-secondaryForeground');
          });
          await page.setViewportSize({ width: 420, height: 700 });
        }
      }
    ]
  }
];

// Parse CLI arguments: --bundle=<name>, --reverse, --verify-assets
const args = process.argv.slice(2);
let selectedBundleName = null;
let isReverse = false;
let verifyAssetsOnly = false;

for (const arg of args) {
  if (arg.startsWith('--bundle=')) {
    selectedBundleName = arg.slice('--bundle='.length).trim();
  } else if (arg === '--reverse') {
    isReverse = true;
  } else if (arg === '--verify-assets') {
    verifyAssetsOnly = true;
  }
}

let runBundles = [...bundles];
if (selectedBundleName) {
  runBundles = bundles.filter(b => b.name === selectedBundleName);
  if (runBundles.length === 0) {
    console.error(`Unknown bundle: "${selectedBundleName}". Available bundles: ${bundles.map(b => b.name).join(', ')}`);
    process.exit(1);
  }
}
if (isReverse) {
  runBundles.reverse();
}

/**
 * Executes the transcript harness with guaranteed browser and context teardown (§2.2).
 * Shared between main() and lifecycle verification so the exact same execution function is tested.
 *
 * @param {object} options
 * @param {() => Promise<any>} [options.launchBrowser] Browser launcher factory
 * @param {Array<any>} options.bundles Bundles to run
 * @param {string} options.html Compiled HTML content
 * @param {boolean} [options.verifyAssets] Whether to run preflight and asset routing checks
 * @param {boolean} [options.verifyAssetsOnly] If true, return after asset verification
 * @param {(browser: any, html: string) => Promise<void>} [options.verifyFn] Function to run for asset verification
 * @param {boolean} [options.logBundle] Whether to log bundle header and scenario passes
 * @returns {Promise<{ totalPassed: number, totalFailed: number, failures: Array<any> }>}
 */
export async function runHarness(options) {
  const {
    launchBrowser = () => chromium.launch({ headless: true }),
    bundles = [],
    html,
    verifyAssets = false,
    verifyAssetsOnly = false,
    verifyFn = verifyAssetRoutesAndPreflight,
    logBundle = true,
  } = options;

  const browser = await launchBrowser();
  let totalPassed = 0;
  let totalFailed = 0;
  const failures = [];

  try {
    if (verifyAssets || verifyAssetsOnly) {
      await verifyFn(browser, html);
      if (verifyAssetsOnly) {
        return { totalPassed: 0, totalFailed: 0, failures: [] };
      }
    }

    for (const bundle of bundles) {
      if (logBundle) console.log(`\n▶ Bundle: ${bundle.name} (${bundle.description})`);
      const context = await browser.newContext();
      try {
        const page = await context.newPage({ viewport: { width: 420, height: 600 } });
        const errors = [];
        const routeErrors = [];

        page.on('pageerror', (e) => errors.push(e.message));
        page.on('console', (m) => {
          if (m.type() === 'error') errors.push(m.text());
        });

        // Strict acquireVsCodeApi mock without postMessage auto-patching (§2.1, §2.3)
        await page.addInitScript(() => {
          window.__posted = [];
          window.acquireVsCodeApi = () => ({
            postMessage(m) { window.__posted.push(m); },
            getState() {},
            setState() {}
          });
        });

        // Shared asset router: exact pathname matching only; unregistered requests invoke onUnregistered (§2.1)
        await installAssetRouter(page, {
          html,
          onUnregistered: (url) => routeErrors.push(`Unregistered asset requested: ${url}`),
        });

        await page.goto(`${TEST_ORIGIN}/`);

        for (const scenario of bundle.scenarios) {
          try {
            await scenario.run(page);
            assert.deepEqual(errors, [], `Page error occurred in [${bundle.name}] ${scenario.id}`);
            assert.deepEqual(routeErrors, [], `Unregistered route error occurred in [${bundle.name}] ${scenario.id}`);
            totalPassed++;
            if (logBundle) console.log(`  PASS: [${scenario.id}] ${scenario.name}`);
          } catch (err) {
            totalFailed++;
            failures.push({ bundle: bundle.name, id: scenario.id, error: err });
            if (logBundle) console.error(`  FAIL: [${scenario.id}] ${scenario.name}:`, err);
            throw err;
          }
        }
      } finally {
        await context.close();
      }
    }

    return { totalPassed, totalFailed, failures };
  } finally {
    await browser.close();
  }
}

async function main() {
  console.log(`Starting transcript-test browser harness (bundles: ${runBundles.map(b => b.name).join(', ')})...`);
  const shouldVerify = verifyAssetsOnly || !selectedBundleName;

  try {
    const result = await runHarness({
      launchBrowser: () => chromium.launch({ headless: true }),
      bundles: runBundles,
      html,
      verifyAssets: shouldVerify,
      verifyAssetsOnly,
      verifyFn: (b, h) => verifyAssetRoutesAndPreflight(b, h),
      logBundle: true,
    });

    if (verifyAssetsOnly) {
      console.log('\nSUMMARY: Asset verification passed successfully.');
      return;
    }

    console.log(`\nSUMMARY: ${result.totalPassed} passed, ${result.totalFailed} failed across ${runBundles.length} bundle(s).`);
    if (result.totalFailed > 0) {
      process.exitCode = 1;
    }
  } catch (err) {
    process.exitCode = 1;
    throw err;
  }
}

await main();


