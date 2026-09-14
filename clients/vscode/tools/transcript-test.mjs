import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
const require = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const { chromium } = require('playwright');
const source = await readFile(new URL('../src/ide/chat.ts', import.meta.url), 'utf8');
const start = source.indexOf('    const nonce =', source.indexOf('private html('));
const end = source.indexOf('</script></body></html>`;', start) + '</script></body></html>`;'.length;
assert.ok(start > 0 && end > start);
const html = new Function('w', source.slice(start, end))({ cspSource: "'self'" });
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 420, height: 600 } });
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => console.log('[PAGE]', m.text()));
  await page.addInitScript(() => {
    window.__posted = [];
    window.acquireVsCodeApi = () => ({
      postMessage(m) { window.__posted.push(m); },
      getState() {},
      setState() {}
    });
  });
  await page.route('http://magi.test/', (route) => route.fulfill({ contentType: 'text/html; charset=utf-8', body: html }));
  await page.goto('http://magi.test/');
  await page.evaluate(() => window.postMessage({ kind: 'rows', rows: [{who:'agent', label:'magi', text:'long answer\n'.repeat(1000)}, {who:'council', label:'Council', text:'review', thought:'council think\n'.repeat(1000)}], refs: [] }, '*'));
  await page.waitForSelector('.row.agent');
  const bounds = await page.locator('.row.agent').evaluate((el) => ({ height: el.clientHeight, scroll: el.scrollHeight }));
  assert.ok(bounds.height > 10000);
  assert.equal(bounds.height, bounds.scroll);
  const thought = await page.locator('.thought').evaluate(el => ({height:el.clientHeight, scroll:el.scrollHeight}));
  assert.ok(thought.height > 10000); assert.equal(thought.height, thought.scroll);
  assert.deepEqual(errors, []);
  await page.locator('#scroll').evaluate((el) => { el.scrollTop = 0; });
  await page.mouse.move(200, 250); await page.mouse.wheel(0, 500);
  await page.waitForFunction(() => document.querySelector('#scroll').scrollTop > 0);
  await page.setViewportSize({ width: 280, height: 400 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight), true);
  console.log('PASS: full long answer, outer wheel scrolling, narrow viewport');

  await page.locator('#scroll').evaluate((el) => { el.scrollTop = el.scrollHeight; });
  await page.evaluate(() => window.postMessage({
    kind: 'rows',
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
  }, '*'));

  await page.waitForSelector('#ask-body:not([hidden])');
  await page.waitForSelector('#ask-controls:not([hidden])');

  // Condition 1 & 2: Report items and custom keys preserved in order and without vertical scroll limit
  const grounds = await page.locator('#ask-body .ground').allInnerTexts();
  assert.equal(grounds.length, 4);
  assert.ok(grounds[0].startsWith('tried:'));
  assert.ok(grounds[1].startsWith('stakes:'));
  assert.ok(grounds[2].startsWith('lean:'));
  assert.ok(grounds[3].startsWith('custom_order: 임의의 커스텀 키 본문 보존 확인'));

  // Condition 3: Question arrival when at bottom auto-scrolls to bottom
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
  await page.evaluate(() => window.postMessage({
    kind: 'rows',
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
  }, '*'));
  const keptScroll = await page.locator('#scroll').evaluate((el) => el.scrollTop);
  assert.equal(keptScroll, 50, 'past turn reading position was preserved');

  // Condition 5: Jump to question button scrolls to question body
  await page.locator('#ask-controls .jump-btn').click();
  await page.waitForFunction(() => document.querySelector('#scroll').scrollTop > 50);
  console.log('PASS: report items, auto-scroll, position retention, jump button');

  // Condition 6: Focus preservation on repeated ask
  const btn = page.locator('#ask-controls .acts button').first();
  await btn.focus();
  const focusedBefore = await page.evaluate(() => document.activeElement?.textContent);
  assert.equal(focusedBefore, '선택 A');

  await page.evaluate(() => window.postMessage({
    kind: 'rows',
    rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }],
    ask: {
      kind: 'question',
      callId: 'q1',
      what: '어느 방식을 선택할까요?',
      options: ['선택 A', '선택 B 긴 설명이 들어간 두 번째 옵션'],
      index: 1,
      total: 2
    }
  }, '*'));
  const focusedAfter = await page.evaluate(() => document.activeElement?.textContent);
  assert.equal(focusedAfter, '선택 A', 'focus was preserved on repeated ask with same callId');

  // Condition 7: Ask replacement and dismissal
  await page.evaluate(() => window.postMessage({
    kind: 'rows',
    rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }],
    ask: {
      kind: 'permission',
      callId: 'call-perm-99',
      what: 'bash',
      args: 'git diff\n'.repeat(50),
      reason: 'check status',
      diff: 'diff --git a/f b/f\n+new line\n'.repeat(100)
    }
  }, '*'));

  await page.waitForSelector('#ask-body pre.diff');
  // Check that diff has no vertical max-height (scrollHeight == clientHeight)
  const diffScroll = await page.locator('#ask-body pre.diff').evaluate((el) => ({ height: el.clientHeight, scroll: el.scrollHeight }));
  assert.equal(diffScroll.height, diffScroll.scroll, 'diff has no separate vertical scrollbar');

  // Click allow and check posted message
  await page.locator('#ask-controls button:text("allow")').click();
  const posted = await page.evaluate(() => window.__posted);
  assert.ok(posted.some((m) => m.kind === 'answer' && m.callId === 'call-perm-99' && m.decision === 'allow'));

  // Dismiss ask
  await page.evaluate(() => window.postMessage({ kind: 'rows', rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }], ask: null }, '*'));
  await page.waitForFunction(() => document.getElementById('ask-body').hidden && document.getElementById('ask-controls').hidden);
  assert.equal(await page.locator('#ask-body').evaluate((el) => el.textContent), '');
  assert.equal(await page.locator('#ask-controls').evaluate((el) => el.textContent), '');
  console.log('PASS: focus preservation, permission diff without max-height, ask replacement/dismissal');

  // Condition 8: Narrow/short viewport doesn't hide composer or blow up panel
  await page.setViewportSize({ width: 250, height: 350 });
  await page.evaluate(() => window.postMessage({
    kind: 'rows',
    rows: [{ who: 'agent', label: 'magi', text: 'narrow test' }],
    ask: {
      kind: 'question',
      callId: 'q3',
      what: '긴 질문 제목이 좁은 패널에서 표시되는지 확인하는 테스트',
      options: ['선택 1', '선택 2', '선택 3', '선택 4', '선택 5', '선택 6', '선택 7', '선택 8']
    }
  }, '*'));
  await page.waitForSelector('#ask-controls .acts button');
  const barVisible = await page.locator('#say').isVisible();
  assert.ok(barVisible, 'input composer is visible in narrow and short viewport');
  const fits = await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight);
  assert.ok(fits, 'page fits within viewport height without outer body overflow');
  console.log('PASS: narrow and short viewport layout');
} finally { await browser.close(); }
