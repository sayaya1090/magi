import assert from 'node:assert/strict';
import { test, expect, TEST_ORIGIN } from './fixtures.mjs';

export { expect };

/**
 * Isolated failure injection spec for testing diagnostic reporting,
 * error collector lifecycle, and testInfo connection (§5.8.6 Item 2).
 *
 * Each test validates a specific failure mode through the real Playwright runner and fixture:
 * 1. Normal body + console.error -> collector assertion failure (exit 1, trace & screenshot preserved)
 * 2. Assertion failure + console.error -> primary assertion preserved, collector assertion omitted,
 *    and collected errors attached to testInfo
 * 3. Normal body + unregistered asset request -> script request allowed by CSP, rejected by router 404
 *    and routeErrors assertion failure
 */

test('[failure_injection_1_console_error] 본문 정상 + console.error 발생 시 collector 단언 실패', async ({ page }) => {
  await page.evaluate(() => {
    console.error('Injected console error for diagnostic verification');
  });
});

test('[failure_injection_2_assertion_and_console] 본문 단언 실패 + console.error 발생 시 최초 단언 보존 및 collector 단언 생략', async ({ page }) => {
  await page.evaluate(() => {
    console.error('Injected companion console error');
  });
  assert.equal('actual_primary_state', 'expected_primary_state', 'Primary assertion failed');
});

test('[failure_injection_3_unregistered_route] 본문 정상 + 미등록 자산 요청 시 라우터 404 및 routeErrors 단언 실패', async ({ page }) => {
  const brokenUrl = `${TEST_ORIGIN}/out/web/chat_adapter.bundle.js.broken`;
  // Synchronize on script onerror permitted by script-src CSP (§5.8.6 Item 2)
  const result = await page.evaluate((url) => {
    return new Promise((resolve) => {
      const script = document.createElement('script');
      script.src = url;
      script.onload = () => resolve({ loaded: true });
      script.onerror = () => resolve({ loaded: false, error: true });
      document.head.appendChild(script);
    });
  }, brokenUrl);
  assert.equal(result.loaded, false, 'Unregistered script bundle must fail to load with 404');
});
