import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import {
  TEST_ORIGIN,
  prepareChatHtml,
  installAssetRouter,
  installAcquireVsCodeApi,
  attachErrorCollectors,
} from './environment.mjs';

const require = createRequire(new URL('../../../web/e2e/package.json', import.meta.url));
const { test: base, expect } = require('@playwright/test');

/**
 * Shared Playwright fixture module for transcript browser testing (§5.8.6).
 *
 * Provides:
 * 1. Preflight check and dynamic HTML preparation (throws structured Error if bundles missing)
 * 2. acquireVsCodeApi mock without postMessage auto-patching
 * 3. Exact asset routing with unregistered request rejection
 * 4. Error collection with listener lifecycle guarantees
 * 5. Diagnostic connection to testInfo: avoids masking primary test failures with
 *    secondary teardown assertions while preserving collected errors as attachments.
 */
export const test = base.extend({
  page: async ({ page }, use, testInfo) => {
    // 1. Prepare chat HTML strictly after preflight check succeeds (§2.2, §5.8.6 Item 1)
    // Throws structured Error with missing path & build instructions without process.exit
    const html = await prepareChatHtml();

    // 2. Attach error collectors for pageerror, console.error, and unregistered requests
    const collector = attachErrorCollectors(page);

    try {
      // 3. Mock acquireVsCodeApi without postMessage auto-patching
      await installAcquireVsCodeApi(page);

      // 4. Install exact asset router with unregistered request rejection
      await installAssetRouter(page, {
        html,
        onUnregistered: collector.handleUnregisteredRequest,
      });

      // 5. Navigate to test origin
      await page.goto(`${TEST_ORIGIN}/`);

      // 6. Yield page to test body
      await use(page);
    } finally {
      // 7. Guaranteed listener detachment
      collector.detach();

      // 8. Connect to testInfo result (§5.8.6 Item 2):
      // If the test body already failed or timed out, do NOT assert collector errors,
      // avoiding duplicate or masking assertion errors. Save collected errors as diagnostic attachment.
      const alreadyFailed = testInfo.status !== 'passed' || (testInfo.errors && testInfo.errors.length > 0);
      if (alreadyFailed) {
        if (collector.errors.length > 0 || collector.routeErrors.length > 0) {
          const artifactPath = testInfo.outputPath('collector-errors.json');
          await writeFile(artifactPath, JSON.stringify({
            errors: collector.errors,
            routeErrors: collector.routeErrors,
          }, null, 2), 'utf8');
          await testInfo.attach('collector-errors.json', {
            path: artifactPath,
            contentType: 'application/json',
          });
        }
      } else {
        // Normal test body passed: assert zero unregistered route errors and zero page/console errors (§5.8.6 Item 2)
        assert.deepEqual(collector.routeErrors, [], `Unregistered route errors occurred during test: ${collector.routeErrors.join('; ')}`);
        assert.deepEqual(collector.errors, [], `Page/console errors occurred during test: ${collector.errors.join('; ')}`);
      }
    }
  },
});

export { expect, TEST_ORIGIN };
