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
const { AxeBuilder } = require('@axe-core/playwright');
const localRequire = createRequire(import.meta.url);
const { evaluateAxeAudit } = localRequire('../out/test/support/a11y_evaluator.js');

import {
  TEST_ORIGIN,
  ASSET_PATHS,
  ASSET_URLS,
  installAssetRouter,
  installAcquireVsCodeApi,
  attachErrorCollectors,
  prepareChatHtml,
} from './transcript/environment.mjs';
import { markdownScenario } from './transcript/scenarios/markdown.mjs';

// Re-export for external consumers (§2.1, §5.8.6)
export {
  TEST_ORIGIN,
  ASSET_PATHS,
  ASSET_URLS,
  installAssetRouter,
  installAcquireVsCodeApi,
  attachErrorCollectors,
  prepareChatHtml,
};

// Re-export preflight checker for consumers
export { checkRequiredBundles as verifyRequiredBundles };

// Pre-flight check and dynamic HTML preparation (§2.2, §5.8.6)
let html;
try {
  html = await prepareChatHtml();
} catch (err) {
  console.error(err.message);
  process.exit(1);
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
  } finally {
    await context.close();
  }
  console.log('  PASS: [asset_routing] Shared exact asset router serves valid assets, rejects old/broken URLs with 404, and captures external HTTP/HTTPS requests');

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

export { measureActiveButtonRing } from "./transcript/dom-helpers.mjs";
import { createBundles } from "./transcript/registry.mjs";

// Parse CLI arguments: --bundle=<name>, --reverse, --reverse-themes, --verify-assets
const args = process.argv.slice(2);
let selectedBundleName = null;
let isReverse = false;
let isReverseThemes = false;
let verifyAssetsOnly = false;

for (const arg of args) {
  if (arg.startsWith("--bundle=")) {
    selectedBundleName = arg.slice("--bundle=".length).trim();
  } else if (arg === "--reverse") {
    isReverse = true;
  } else if (arg === "--reverse-themes") {
    isReverseThemes = true;
  } else if (arg === "--verify-assets") {
    verifyAssetsOnly = true;
  }
}

const bundles = createBundles({ reverseThemes: isReverseThemes });
let runBundles = [...bundles];
if (selectedBundleName) {
  runBundles = bundles.filter(b => b.name === selectedBundleName);
  if (runBundles.length === 0) {
    console.error(`Unknown bundle: "${selectedBundleName}". Available bundles: ${bundles.map(b => b.name).join(", ")}`);
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
        const collector = attachErrorCollectors(page);

        // Strict acquireVsCodeApi mock without postMessage auto-patching (§2.1, §2.3, §5.8.6)
        await installAcquireVsCodeApi(page);

        // Shared asset router: exact pathname matching only; unregistered requests invoke onUnregistered (§2.1, §5.8.6)
        await installAssetRouter(page, {
          html,
          onUnregistered: collector.handleUnregisteredRequest,
        });

        await page.goto(`${TEST_ORIGIN}/`);

        for (const scenario of bundle.scenarios) {
          try {
            await scenario.run(page);
            assert.deepEqual(collector.errors, [], `Page error occurred in [${bundle.name}] ${scenario.id}`);
            assert.deepEqual(collector.routeErrors, [], `Unregistered route error occurred in [${bundle.name}] ${scenario.id}`);
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


