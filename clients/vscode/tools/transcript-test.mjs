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
  assert.equal(focusedBefore, '1. 선택 A');

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
  assert.equal(focusedAfter, '1. 선택 A', 'focus was preserved on repeated ask with same callId');

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

  // Condition 9: Free-text question auto-enters answer mode, preserves general draft
  await page.setViewportSize({ width: 420, height: 600 });
  // User types general draft
  await page.locator('#say').fill('새 작업 초안 작성 중...');
  await page.evaluate(() => window.postMessage({
    kind: 'rows',
    rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
    ask: {
      kind: 'question',
      callId: 'free-q1',
      what: '이 파일의 이름을 무엇으로 변경할까요?',
      options: []
    }
  }, '*'));
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

  // Condition 10: Multiple-choice question, direct input button, draft retention and Esc cancel
  await page.evaluate(() => window.postMessage({
    kind: 'rows',
    rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
    ask: {
      kind: 'question',
      callId: 'choice-q2',
      what: '배포 환경을 선택하세요',
      options: ['스테이징 환경', '운영(프로덕션) 환경']
    }
  }, '*'));
  await page.waitForSelector('#ask-controls button:text("1. 스테이징 환경")');
  // Choice buttons are numbered
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
  console.log('PASS: answer mode, draft preservation, escape cancel, and choice buttons');

  // Condition 11: 전송 거절 (Send rejection) - keeps question draft and doesn't pollute general draft
  await page.locator('#say').fill('원래 일반 프롬프트 초안');
  await page.evaluate(() => window.postMessage({
    kind: 'rows',
    rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
    ask: {
      kind: 'question',
      callId: 'q-reject',
      what: '거절 테스트 질문',
      options: ['선택 1']
    }
  }, '*'));
  await page.waitForSelector('#ask-controls button:text("직접 입력")');
  await page.locator('#ask-controls button:text("직접 입력")').click();
  await page.locator('#say').fill('거절될 답변 내용');
  await page.locator('#send').click();
  // While in flight, say restores general draft
  assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안');
  // Daemon sends refusal replyResult
  await page.evaluate(() => window.postMessage({
    kind: 'replyResult',
    callId: 'q-reject',
    ok: false,
    error: 'companion refused to accept answer',
    text: '거절될 답변 내용'
  }, '*'));
  // Webview re-enters answer mode for q-reject and restores failed draft
  await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
  assert.equal(await page.locator('#say').inputValue(), '거절될 답변 내용', 'failed reply restored into answer mode');
  // Esc cancels answer mode and restores general draft without failed reply being prepended
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안', 'general draft intact without failed answer prepended');
  console.log('PASS: send rejection preserves question draft without polluting general draft');

  // Condition 12: 연결 단절 (Connection disconnect)
  await page.locator('#ask-controls button:text("직접 입력")').click();
  await page.locator('#say').fill('재시도할 답변');
  await page.locator('#send').click();
  await page.evaluate(() => window.postMessage({
    kind: 'replyResult',
    callId: 'q-reject',
    ok: false,
    error: 'no companion is listening on this workspace.',
    text: '재시도할 답변'
  }, '*'));
  await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
  assert.equal(await page.locator('#say').inputValue(), '재시도할 답변', 'disconnected reply restored into answer mode');
  await page.keyboard.press('Escape');
  console.log('PASS: connection disconnect preserves question draft');

  // Condition 13: 질문 교체 뒤 늦은 실패 응답 (Late failure response after question replacement)
  // Step A: Send answer for q-reject
  await page.locator('#ask-controls button:text("직접 입력")').click();
  await page.locator('#say').fill('구 질문 답변');
  await page.locator('#send').click();
  // Step B: Question is replaced by q-new before q-reject failure arrives
  await page.evaluate(() => window.postMessage({
    kind: 'rows',
    rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
    ask: {
      kind: 'question',
      callId: 'q-new',
      what: '새로운 질문',
      options: ['신규 1']
    }
  }, '*'));
  await page.waitForSelector('#ask-controls button:text("1. 신규 1")');
  await page.locator('#ask-controls button:text("직접 입력")').click();
  await page.locator('#say').fill('신규 질문에 타이핑 중인 답변');
  // Step C: Late failure for q-old arrives!
  await page.evaluate(() => window.postMessage({
    kind: 'replyResult',
    callId: 'q-reject',
    ok: false,
    error: 'timeout',
    text: '구 질문 답변'
  }, '*'));
  // Ensure say.value is NOT overwritten by q-reject!
  assert.equal(await page.locator('#say').inputValue(), '신규 질문에 타이핑 중인 답변', 'late failure response from old question did not overwrite current question draft');
  // Cancel q-new
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안');
  console.log('PASS: late failure response after question replacement does not interrupt current draft');

  // Condition 14: 모드 전환 뒤 늦은 자동완성 무시 (Late autocompletion after mode switch is ignored)
  // Step A: User in general mode types, suggest request fired
  await page.locator('#say').fill('myFunc');
  // Wait for typing debounce (450ms)
  await page.waitForTimeout(500);
  const suggestMsg = await page.evaluate(() => window.__posted.filter(m => m.kind === 'suggest').slice(-1)[0]);
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

  // Assertion: Hint is still empty, Tab does not insert late suggestion
  assert.equal(await page.locator('#hint').textContent(), '', 'late suggestion ignored after mode switch');
  await page.keyboard.press('Tab');
  assert.equal(await page.locator('#say').inputValue(), '', 'tab did not insert suggestion from previous mode');

  // Step D: User types in answer mode, exits mode, late suggestion for answer mode arrives in general mode
  await page.locator('#say').fill('answer');
  await page.waitForTimeout(500);
  const answerSuggestMsg = await page.evaluate(() => window.__posted.filter(m => m.kind === 'suggest').slice(-1)[0]);
  assert.equal(answerSuggestMsg.target, 'q-new');
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
  console.log('PASS: late autocompletion after mode switch is invalidated and ignored');
} finally { await browser.close(); }
