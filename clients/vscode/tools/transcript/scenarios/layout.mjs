import assert from 'node:assert/strict';
import { createRowsMessage } from '../../transcript-fixtures.mjs';

export const layoutScenarios = [
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
];

export const layoutBundle = {
  name: 'layout',
  description: '레이아웃·보고서',
  scenarios: layoutScenarios,
};
