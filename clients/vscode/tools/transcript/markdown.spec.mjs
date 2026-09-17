import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import {
  TEST_ORIGIN,
  prepareChatHtml,
  installAssetRouter,
  installAcquireVsCodeApi,
  attachErrorCollectors,
} from './environment.mjs';
import { markdownScenario } from './scenarios/markdown.mjs';

const require = createRequire(new URL('../../../web/e2e/package.json', import.meta.url));
const { test: base, expect } = require('@playwright/test');

/**
 * Custom Playwright test fixture extending base page with:
 * 1. Preflight check and dynamic HTML preparation
 * 2. Error collectors for pageerror and console.error
 * 3. Mock acquireVsCodeApi
 * 4. Exact asset routing with rejection of unregistered/external requests
 * 5. Lifecycle teardown without masking primary assertions (§5.8.6)
 */
export const test = base.extend({
  page: async ({ page }, use) => {
    // 1. Prepare chat HTML strictly after preflight check succeeds (§2.2, §5.8.6)
    const html = await prepareChatHtml();
    if (!html) {
      throw new Error('Preflight check failed: chat HTML could not be prepared.');
    }

    // 2. Attach observers for console.error and pageerror
    const collector = attachErrorCollectors(page);

    // 3. Mock acquireVsCodeApi without postMessage auto-patching
    await installAcquireVsCodeApi(page);

    // 4. Install exact asset router with unregistered request rejection
    await installAssetRouter(page, {
      html,
      onUnregistered: collector.handleUnregisteredRequest,
    });

    // 5. Navigate to test page
    await page.goto(`${TEST_ORIGIN}/`);

    let executionError = null;
    try {
      await use(page);
    } catch (err) {
      executionError = err;
      throw err;
    } finally {
      // 6. Detach listeners
      collector.detach();

      // Only assert collected errors if body execution did not fail,
      // ensuring underlying assertion errors are not masked by teardown errors (§5.8.6 Item 2)
      if (!executionError) {
        assert.deepEqual(collector.errors, [], `Page/console errors occurred during test: ${collector.errors.join('; ')}`);
        assert.deepEqual(collector.routeErrors, [], `Unregistered route errors occurred during test: ${collector.routeErrors.join('; ')}`);
      }
    }
  },
});

export { expect };

test(`[${markdownScenario.id}] ${markdownScenario.name}`, async ({ page }) => {
  // Test failure induction for isolated diagnostic and lifecycle verification (§5.8.6 Item 3)
  if (process.env.MAGI_INJECT_FAILURE === 'bad_expectation') {
    assert.equal('actual_state', 'wrong_injected_expectation', 'Injected wrong expectation failure for test diagnostics');
  } else if (process.env.MAGI_INJECT_FAILURE === 'external_request') {
    await page.evaluate(() => {
      fetch('https://unauthorized.example.com/beacon.json').catch(() => {});
    });
    await page.waitForTimeout(50);
  }

  await markdownScenario.run(page);
});

