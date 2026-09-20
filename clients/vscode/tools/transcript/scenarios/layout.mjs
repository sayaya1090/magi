import assert from 'node:assert/strict';
import { createRowsMessage, createStateMessage } from '../../transcript-fixtures.mjs';

export const layoutScenarios = [
      {
        id: 'layout_state_note_and_transient_note_do_not_erase_each_other',
        name: '지속 안내(#state-note)와 일시 알림(#note)이 서로를 안 지운다 (§6.9)',
        run: async (page) => {
          /* 둘이 한 칸에 살던 동안 서로를 지웠다: 컴패니언이 죽어 「없습니다」와 시작 단추가 떠
             있을 때 무언가 보내면 그것이 덮이고, 4초 뒤 타이머가 지우고, **다음 state 사건이 올
             때까지 안 돌아왔다.** 그래서 여기서 재는 것은 「문구가 뜨나」가 아니라 **교차 전이**다. */
          const stateText = () => page.locator('#state-note').textContent();
          const noteText = () => page.locator('#note').textContent();
          const startBtn = () => page.locator('#state-note button');

          for (const rows of [[], [{ who: 'agent', label: 'magi', text: 'turn 1' }]]) {
            // 행 0 과 행 n 에서 같은 경로를 각각 밟는다 — 행이 있으면 안내가 다른 자리로 가지 않는다.
            await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({ rows }));
            await page.evaluate((msg) => window.postMessage(msg, '*'),
              createStateMessage('not-running', { text: '이 폴더에서 도는 컴패니언이 없습니다.', offerStart: true }));
            await page.waitForFunction(() => document.querySelector('#state-note')?.textContent?.includes('컴패니언이 없습니다') === true);
            assert.equal(await startBtn().count(), 1, `행 ${rows.length}: 시작 단추가 지속 안내에 붙는다`);

            // 일시 알림이 떠도 지속 안내와 단추는 그대로.
            await page.evaluate(() => { document.getElementById('note').textContent = 'sending…'; });
            assert.ok((await stateText()).includes('컴패니언이 없습니다'), `행 ${rows.length}: 일시 알림이 지속 안내를 안 덮는다`);
            assert.equal(await startBtn().count(), 1, `행 ${rows.length}: 일시 알림이 시작 단추를 안 지운다`);

            // 일시 알림 만료 → 지속 안내와 단추는 **남는다**. 이것이 고치기 전 사라지던 자리다.
            await page.evaluate(() => { document.getElementById('note').textContent = ''; });
            assert.ok((await stateText()).includes('컴패니언이 없습니다'), `행 ${rows.length}: 만료 뒤에도 지속 안내가 남는다`);
            assert.equal(await startBtn().count(), 1, `행 ${rows.length}: 만료 뒤에도 시작 단추가 남는다`);

            // 일시 알림이 떠 있는 동안 state 가 갱신돼도 일시 알림은 **안 지워진다**.
            await page.evaluate(() => { document.getElementById('note').textContent = 'sending…'; });
            await page.evaluate((msg) => window.postMessage(msg, '*'),
              createStateMessage('unknown', { text: '컴패니언 상태를 아직 알 수 없습니다.', offerStart: true }));
            await page.waitForFunction(() => document.querySelector('#state-note')?.textContent?.includes('아직 알 수 없습니다') === true);
            assert.equal((await noteText()).trim(), 'sending…', `행 ${rows.length}: 상태 갱신이 일시 알림을 안 지운다`);

            // attached 로 가면 **지속 안내만** 사라지고 일시 알림은 남는다.
            await page.evaluate((msg) => window.postMessage(msg, '*'),
              createStateMessage('attached', { text: '', offerStart: false }));
            await page.waitForFunction(() => document.querySelector('#state-note')?.textContent === '');
            assert.equal((await noteText()).trim(), 'sending…', `행 ${rows.length}: attached 전환이 일시 알림까지 지우지 않는다`);
            await page.evaluate(() => { document.getElementById('note').textContent = ''; });
          }
        }
      },
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
      },
      {
        id: 'layout_both_notices_do_not_cover_the_composer',
        name: '지속·일시 안내가 같이 서도 입력창과 승인 단추를 안 가린다 (§6.9)',
        run: async (page) => {
          /* 둘이 **동시에** 설 수 있다 — 「컴패니언이 없다」와 「방금 보냈다」는 서로를 부정하지
             않는다. 그래서 배치는 둘 다 선 채로 재야 한다. ⚠ 한쪽을 숨겨서 푸는 것은 답이 아니다:
             그러면 이 시험은 초록이 되고 사람은 나갈 길을 잃는다. */
          for (const [w, h] of [[320, 600], [1200, 200]]) {
            await page.setViewportSize({ width: w, height: h });
            await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: {
                kind: 'permission',
                callId: 'perm-layout',
                what: 'edit',
                filePath: 'a.txt',
                args: JSON.stringify({ path: 'a.txt', old: 'x', new: 'y' })
              }
            }));
            await page.evaluate((msg) => window.postMessage(msg, '*'),
              createStateMessage('not-running', { text: '이 폴더에서 도는 컴패니언이 없습니다.', offerStart: true }));
            await page.evaluate(() => { document.getElementById('note').textContent = 'sending…'; });
            await page.waitForFunction(() => document.querySelector('#state-note button') !== null);

            const box = async (sel) => page.locator(sel).first().boundingBox();
            const [say, approve, stateNote, note] = await Promise.all(
              [box('#say'), box('#ask-controls .acts button.approval-btn'), box('#state-note'), box('#note')]);
            for (const [name, b] of [['입력창', say], ['승인 단추', approve]]) {
              assert.ok(b && b.width > 0 && b.height > 0, `${w}×${h}: ${name}이 그려진다`);
              assert.ok(b.y + b.height <= h + 0.5,
                `${w}×${h}: ${name}이 화면 아래로 밀려난다 — bottom=${(b.y + b.height).toFixed(1)}, `
                + `state-note=${stateNote?.height?.toFixed(1)}, note=${note?.height?.toFixed(1)}, `
                + `ask=${(await box('#ask-controls'))?.height?.toFixed(1)}, scroll=${(await box('#scroll'))?.height?.toFixed(1)}`);
            }
            // 둘 다 실제로 서 있는 채로 잰 것이어야 한다 — 하나가 숨어 있으면 이 측정은 무효다.
            assert.ok(stateNote && stateNote.height > 0, `${w}×${h}: 지속 안내가 서 있다`);
            assert.ok(note && note.height > 0, `${w}×${h}: 일시 알림이 서 있다`);
            /* 이 뷰포트의 바깥 오버플로가 **이 변경 탓인지** 먼저 가른다: 지속 안내를 지운 상태와
               세운 상태를 같은 자리에서 재서, 늘어난 만큼만 이 변경의 몫으로 센다. */
            const measure = () => page.evaluate(() => document.documentElement.scrollHeight - innerHeight);
            const withBoth = await measure();
            await page.evaluate((msg) => window.postMessage(msg, '*'),
              createStateMessage('attached', { text: '', offerStart: false }));
            await page.waitForFunction(() => document.querySelector('#state-note')?.textContent === '');
            const withoutState = await measure();
            assert.ok(withBoth <= Math.max(withoutState, 0) + 1,
              `${w}×${h}: 지속 안내가 바깥 오버플로를 키운다 — 둘 다=${withBoth}px, 지속 안내 없이=${withoutState}px`);
            await page.evaluate((msg) => window.postMessage(msg, '*'),
              createStateMessage('not-running', { text: '이 폴더에서 도는 컴패니언이 없습니다.', offerStart: true }));
            await page.waitForFunction(() => document.querySelector('#state-note button') !== null);
            await page.evaluate(() => { document.getElementById('note').textContent = ''; });
          }
        }
      }
];

export const layoutBundle = {
  name: 'layout',
  description: '레이아웃·보고서',
  scenarios: layoutScenarios,
};
