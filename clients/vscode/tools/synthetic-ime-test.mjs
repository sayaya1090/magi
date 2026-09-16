/**
 * Synthetic Browser IME Composition and Buffer Protection Test Harness.
 *
 * NOTE ON VERIFICATION SCOPE:
 * This test drives synthetic DOM events (CompositionEvent, KeyboardEvent with keyCode 229,
 * isComposing: true) in Chromium to verify chat_adapter's event guards.
 * It does NOT execute OS-level native IME manager hooks (e.g. macOS Apple 2SetKorean or
 * Windows MS-IME). Actual OS-level native IME remains unverified (미검증).
 */

import { createRequire } from 'node:module';
import * as assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(new URL('../../../clients/web/e2e/package.json', import.meta.url));
const { chromium } = require('playwright');
const { renderChatHtml } = await import('../out/web/chat_html.js');
const { createRowsMessage, createReplyResultMessage } = await import('./transcript-fixtures.mjs');

const TEST_ORIGIN = 'http://magi.test';
const nonce = 'synthetic-ime-nonce';
const html = renderChatHtml({
  cspSource: `'self' ${TEST_ORIGIN}`,
  nonce,
  scriptUri: `${TEST_ORIGIN}/out/web/answer_state.js`,
  adapterUri: `${TEST_ORIGIN}/out/web/chat_adapter.bundle.js`,
});

console.log('--- Starting Browser Synthetic IME Verification ---');
console.log('Scope: Synthetic CompositionEvent and KeyboardEvent guards (OS native IME: unverified)');

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 420, height: 700 } });
const page = await context.newPage();

await page.addInitScript(() => {
  window.__posted = [];
  window.acquireVsCodeApi = () => ({
    postMessage(m) { window.__posted.push(m); },
    getState() {},
    setState() {}
  });
});

await page.route(`${TEST_ORIGIN}/**`, async (route) => {
  const url = new URL(route.request().url());
  if (url.pathname === '/' || url.pathname === '/index.html') {
    return route.fulfill({ contentType: 'text/html; charset=utf-8', body: html });
  }
  if (url.pathname === '/out/web/answer_state.js') {
    const js = await readFile(path.resolve(__dirname, '../out/web/answer_state.js'), 'utf8');
    return route.fulfill({ contentType: 'application/javascript; charset=utf-8', body: js });
  }
  if (url.pathname === '/out/web/chat_adapter.bundle.js') {
    const js = await readFile(path.resolve(__dirname, '../out/web/chat_adapter.bundle.js'), 'utf8');
    return route.fulfill({ contentType: 'application/javascript; charset=utf-8', body: js });
  }
  return route.fulfill({ status: 404 });
});

await page.goto(`${TEST_ORIGIN}/`);
await page.waitForSelector('#say');

// Deliver setup and active session
await page.evaluate(() => {
  window.postMessage({
    kind: 'setup',
    companionKey: '/workspace',
    sessionId: 'sess-synthetic-ime',
    userLabel: 'User',
    model: 'test-model',
    permission: 'full',
    council: 'test-council'
  }, '*');
});

await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
  session: 'sess-synthetic-ime',
  rows: [{ who: 'agent', label: 'magi', text: '대기 중' }]
}));

// Test 1: Korean syllable composition ('ㅎ' -> '하' -> '한') and Enter suppression (isComposing: true, keyCode: 229)
console.log('1. Testing synthetic Korean composition and Enter suppression...');
const say = page.locator('#say');
await say.focus();

await page.evaluate(() => {
  const el = document.getElementById('say');
  el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true, data: '' }));
  el.value = 'ㅎ';
  el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: 'ㅎ' }));
  el.value = '하';
  el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: '하' }));
  el.value = '한';
  el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: '한' }));

  // Synthetic intermediate Enter while composing (keyCode 229, isComposing true)
  const composingEnter = new KeyboardEvent('keydown', {
    key: 'Enter',
    code: 'Enter',
    keyCode: 229,
    which: 229,
    isComposing: true,
    bubbles: true,
    cancelable: true
  });
  el.dispatchEvent(composingEnter);
});

let posted = await page.evaluate(() => window.__posted);
assert.equal(posted.filter(m => m.kind === 'say').length, 0, 'Enter during synthetic composition must NOT send message');
console.log('  PASS: Synthetic Enter during composition suppressed (0 messages sent)');

// Complete first syllable '한' and compose second syllable '글'
await page.evaluate(() => {
  const el = document.getElementById('say');
  el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '한' }));
  el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true, data: '' }));
  el.value = '한글';
  el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: '글' }));
  el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '글' }));
  el.dispatchEvent(new Event('input', { bubbles: true }));

  // Final normal Enter after compositionend (keyCode 13, isComposing false)
  const normalEnter = new KeyboardEvent('keydown', {
    key: 'Enter',
    code: 'Enter',
    keyCode: 13,
    which: 13,
    isComposing: false,
    bubbles: true,
    cancelable: true
  });
  el.dispatchEvent(normalEnter);
});

posted = await page.evaluate(() => window.__posted);
const sayMessages = posted.filter(m => m.kind === 'say');
assert.equal(sayMessages.length, 1, 'Exactly one message sent on final Enter');
assert.equal(sayMessages[0].text, '한글', 'Sent text must match exact Korean text without duplication');
assert.equal(await say.inputValue(), '', 'Textarea must be cleared after send');
console.log('  PASS: Final Enter after compositionend dispatched exact text ("한글") with 0 duplication');

// Test 2: Recovery draft protection during active synthetic composition
console.log('2. Testing recovery draft protection during active synthetic composition...');
await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
  session: 'sess-synthetic-ime',
  ask: {
    kind: 'question',
    callId: 'q-ime-protect',
    what: '초안 보호 확인',
    options: ['확인']
  }
}));

await page.waitForSelector('#ask-controls button');
const customInputBtn = page.locator('#ask-controls button', { hasText: '직접 입력' });
await customInputBtn.click();
await page.locator('#say').fill('실패할 답변 본문');

const postedBeforeSend = await page.evaluate(() => window.__posted.length);
await page.locator('#send').click();
await page.waitForFunction((len) => window.__posted.length > len, postedBeforeSend);

const replyMsg = await page.evaluate(() => window.__posted.filter((m) => m.kind === 'reply' && m.callId === 'q-ime-protect').slice(-1)[0]);
assert.ok(replyMsg, 'reply message must be dispatched');

await page.evaluate((payload) => window.postMessage(payload, '*'), createReplyResultMessage({
  callId: 'q-ime-protect',
  ok: false,
  error: 'network error',
}, replyMsg));

await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');
await page.locator('#recovery-btn').click();
await page.waitForSelector('#recovery-panel:not([hidden])');

const copyBtn = page.locator('#recovery-items .copy-btn');
await copyBtn.waitFor();
assert.equal(await copyBtn.isDisabled(), false, 'Copy button initially enabled');

// Start active composition in composer
await page.evaluate(() => {
  const el = document.getElementById('say');
  el.value = '새 입력 ';
  el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true, data: '' }));
  el.value = '새 입력 글';
  el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: '글' }));
});
await page.waitForTimeout(50);

assert.equal(await copyBtn.isDisabled(), true, 'Copy button must be disabled during active synthetic composition');
console.log('  PASS: Recovery copy button disabled during active composition to protect buffer');

// End composition
await page.evaluate(() => {
  const el = document.getElementById('say');
  el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '글' }));
  el.dispatchEvent(new Event('input', { bubbles: true }));
});
await page.waitForTimeout(50);

assert.equal(await copyBtn.isDisabled(), false, 'Copy button re-enabled after compositionend');
console.log('  PASS: Recovery copy button re-enabled after compositionend');

await context.close();
await browser.close();
console.log('--- Synthetic IME Verification Passed Successfully ---');
