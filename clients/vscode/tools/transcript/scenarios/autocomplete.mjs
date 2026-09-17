import assert from 'node:assert/strict';
import { createRowsMessage } from '../../transcript-fixtures.mjs';

export const autocompleteScenarios = [
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
];

export const autocompleteBundle = {
  name: 'autocomplete',
  description: '자동완성',
  scenarios: autocompleteScenarios,
};
