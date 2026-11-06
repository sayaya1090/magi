import assert from 'node:assert/strict';
import { test } from './fixtures.mjs';
import { createRowsMessage } from '../transcript-fixtures.mjs';

test('긴 전사는 DOM 상한 안에서 양 끝으로 이동하고 읽던 행을 보존한다', async ({ page }) => {
  const rows = Array.from({ length: 10000 }, (_, i) => ({
    seq: i + 1, who: 'agent', label: 'magi', text: `history ${i}: ` + 'content '.repeat(i % 7 + 1),
  }));
  const send = () => page.evaluate(m => window.postMessage(m, '*'), createRowsMessage({ rows }));
  await send();
  await page.waitForFunction(() => document.querySelectorAll('#rows > .row').length > 0);
  assert.ok(await page.locator('#rows > .row').count() <= 250);
  await page.waitForFunction(() => document.querySelector('#rows').textContent.includes('history 9999:'));
  await page.locator('#scroll').evaluate(s => {
    s.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, bubbles: true }));
    s.scrollTop = 0;
    s.dispatchEvent(new Event('scroll'));
  });
  await page.waitForFunction(() => document.querySelector('#rows').textContent.includes('history 0:'));
  assert.ok(await page.locator('#rows > .row').count() <= 250);
  const before = await page.locator('#rows > .row').first().evaluate(e => ({ text: e.textContent, top: e.getBoundingClientRect().top }));
  rows.push({ seq: 10001, who: 'agent', label: 'magi', text: 'new tail' });
  await send();
  await page.waitForTimeout(50);
  const after = await page.locator('#rows > .row').first().evaluate(e => ({ text: e.textContent, top: e.getBoundingClientRect().top }));
  assert.equal(after.text, before.text);
  assert.ok(Math.abs(after.top - before.top) <= 1);
  await page.locator('#scroll').evaluate(s => { s.scrollTop = s.scrollHeight; s.dispatchEvent(new Event('scroll')); });
  await page.waitForFunction(() => document.querySelector('#rows').textContent.includes('new tail'));
  assert.ok(await page.locator('#rows > .row').count() <= 250);
});

test('화면 밖으로 나간 도구의 펼침 상태와 대화 전환을 보존한다', async ({ page }) => {
  const rows = Array.from({ length: 1000 }, (_, i) => ({ who: 'agent', label: 'magi', text: `history ${i}` }));
  rows[999] = { who: 'tool', label: 'bash', callId: 'tail-tool', text: 'run', args: 'summary', rawArgs: '{"command":"full command"}' };
  const send = (session, values) => page.evaluate(m => window.postMessage(m, '*'), createRowsMessage({ session, rows: values }));
  await send('long', rows);
  const toggle = page.locator('[data-call-id="tail-tool"]');
  await toggle.click();
  assert.equal(await toggle.getAttribute('aria-expanded'), 'true');
  await page.locator('#scroll').evaluate(s => { s.dispatchEvent(new WheelEvent('wheel', {deltaY:-100})); s.scrollTop=0; s.dispatchEvent(new Event('scroll')); });
  await page.waitForFunction(() => document.querySelector('#rows').textContent.includes('history 0'));
  await page.locator('#scroll').evaluate(s => { s.scrollTop=s.scrollHeight; s.dispatchEvent(new Event('scroll')); });
  await toggle.waitFor();
  assert.equal(await toggle.getAttribute('aria-expanded'), 'true');
  await send('short', [{who:'agent', label:'magi', text:'another conversation'}]);
  await page.waitForFunction(() => document.querySelector('#rows').textContent.includes('another conversation'));
  assert.equal(await page.locator('#rows > .row').count(), 1);
  assert.equal(await page.locator('#rows').evaluate(e => e.textContent.includes('history')), false);
});
