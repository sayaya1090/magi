import assert from 'node:assert/strict';
import { createRowsMessage, createStateMessage } from '../../transcript-fixtures.mjs';

export const layoutScenarios = [
      {
        id: 'layout_state_note_survives_the_real_transient_notice',
        name: '실제 전송이 만든 일시 알림이 4초로 만료돼도 지속 안내와 시작 단추가 남는다 (§6.9)',
        run: async (page) => {
          /* ⚠ **일시 알림을 손으로 짓지 않는다.** 앞 판은 `#note` 의 글자를 직접 넣고 직접 지운 것을
             「만료」라고 단언했다 — 생산자도 타이머도 안 지나므로 그 경로가 퇴행해도 초록이었다.
             재는 도구가 재려는 것을 비켜 가 있었다. 여기서는 **사람이 하는 대로** 입력줄에 쓰고
             Send 를 눌러 `sending…` 을 만들고, **그 타이머가 실제로 만료**하게 둔다.

             시계는 Playwright 로 진행시킨다: 타이머는 4000ms 하드코딩이고, 조합 넷을 실시간으로
             기다리면 이 묶음 혼자 16초를 쓴다. `install()` 뒤 `runFor` 로 진행시키고 **반드시
             `resume()` 으로 되돌린다** — 이 판은 묶음 안에서 page 를 공유하므로, 멈춘 시계를 두고
             나가면 뒤 시나리오가 제 타이머를 못 본다. */
          await page.clock.install();
          try {
            for (const rows of [[], [{ who: 'agent', label: 'magi', text: 'turn 1' }]]) {
              for (const [state, text] of [
                ['not-running', '이 폴더에서 도는 컴패니언이 없습니다.'],
                ['unknown', '컴패니언 상태를 아직 알 수 없습니다.'],
              ]) {
                const where = `행 ${rows.length}·${state}`;
                await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({ rows }));
                await page.evaluate((msg) => window.postMessage(msg, '*'),
                  createStateMessage(state, { text, offerStart: true }));
                await page.waitForFunction((t) => document.querySelector('#state-note')?.textContent?.includes(t) === true, text);
                assert.equal(await page.locator('#state-note button').count(), 1, `${where}: 시작 단추가 지속 안내에 붙는다`);

                // 진짜 생산자: 사람이 쓰고 보낸다.
                await page.locator('#say').fill('일시 알림을 만드는 실제 전송');
                await page.locator('#send').click();
                await page.waitForFunction(() => document.querySelector('#note')?.textContent === 'sending…');
                assert.ok((await page.locator('#state-note').textContent()).includes(text), `${where}: 일시 알림이 지속 안내를 안 덮는다`);
                assert.equal(await page.locator('#state-note button').count(), 1, `${where}: 일시 알림이 시작 단추를 안 지운다`);

                // 진짜 타이머가 만료한다 — 글자를 지우지도, 콜백을 베껴 부르지도 않는다.
                await page.clock.runFor(4100);
                await page.waitForFunction(() => document.querySelector('#note')?.textContent === '');
                assert.ok((await page.locator('#state-note').textContent()).includes(text), `${where}: 만료 뒤에도 지속 안내가 남는다`);
                assert.equal(await page.locator('#state-note button').count(), 1, `${where}: 만료 뒤에도 시작 단추가 남는다`);
              }
            }

            /* 호스트가 보내는 note 는 **타이머가 없다.** 같은 만료를 가정하지 않는다 — 가정하면
               「안 지워졌다」가 통과의 근거가 되고, 그건 아무것도 안 잰 것이다. */
            await page.evaluate((msg) => window.postMessage(msg, '*'),
              createStateMessage('not-running', { text: '이 폴더에서 도는 컴패니언이 없습니다.', offerStart: true }));
            await page.waitForFunction(() => document.querySelector('#state-note button') !== null);
            await page.evaluate(() => window.postMessage({ kind: 'note', text: '호스트가 보낸 안내' }, '*'));
            await page.waitForFunction(() => document.querySelector('#note')?.textContent === '호스트가 보낸 안내');
            await page.clock.runFor(8000);
            assert.equal((await page.locator('#note').textContent()).trim(), '호스트가 보낸 안내',
              '호스트 note 에는 4초 만료가 없다 — 있다고 가정하지 않는다');
            assert.equal(await page.locator('#state-note button').count(), 1, '호스트 note 가 시작 단추를 안 지운다');

            // 일시 알림이 떠 있는 동안 state 가 갱신돼도 일시 알림은 안 지워진다(기존 단언 유지).
            await page.evaluate((msg) => window.postMessage(msg, '*'),
              createStateMessage('unknown', { text: '컴패니언 상태를 아직 알 수 없습니다.', offerStart: true }));
            await page.waitForFunction(() => document.querySelector('#state-note')?.textContent?.includes('아직 알 수 없습니다') === true);
            assert.equal((await page.locator('#note').textContent()).trim(), '호스트가 보낸 안내', '상태 갱신이 일시 알림을 안 지운다');

            // attached 로 가면 지속 안내만 사라진다.
            await page.evaluate((msg) => window.postMessage(msg, '*'), createStateMessage('attached', { text: '', offerStart: false }));
            await page.waitForFunction(() => document.querySelector('#state-note')?.textContent === '');
            assert.equal((await page.locator('#note').textContent()).trim(), '호스트가 보낸 안내', 'attached 전환이 일시 알림까지 지우지 않는다');
          } finally {
            await page.clock.resume();
            await page.evaluate(() => window.postMessage({ kind: 'note', text: '' }, '*'));
            await page.evaluate((msg) => window.postMessage(msg, '*'), createStateMessage('attached', { text: '', offerStart: false }));
            await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({ rows: [], ask: null }));
            await page.locator('#say').fill('');
          }
        }
      },
      {
        id: 'layout_start_button_sends_once_and_keeps_drafts',
        name: '지속 안내의 시작 단추는 start 를 한 번 보내고, 누른 것만으로 연결을 그리지 않는다 (§6.9)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: { kind: 'question', callId: 'q-start', what: '무엇을 할까요?' }
          }));
          await page.evaluate((msg) => window.postMessage(msg, '*'),
            createStateMessage('not-running', { text: '이 폴더에서 도는 컴패니언이 없습니다.', offerStart: true }));
          await page.waitForFunction(() => document.querySelector('#state-note button') !== null);

          // 답변 모드로 들어가 초안을 만든 뒤 시작을 누른다 — 눌렀다고 사람이 쓰던 것이 사라지면 안 된다.
          await page.locator('#say').fill('질문 답변 B');
          const modeBefore = await page.locator('#reply-mode').isVisible();
          const draftBefore = await page.locator('#say').inputValue();
          const before = await page.evaluate(() => window.__posted.length);

          await page.locator('#state-note button').click();

          const posted = await page.evaluate(() => window.__posted);
          const starts = posted.filter((m) => m.kind === 'start');
          assert.equal(posted.length, before + 1, '시작 단추가 정확히 한 개의 메시지를 보낸다');
          assert.equal(starts.length, 1, '기존 start 메시지를 한 번 보낸다');
          /* ⚠ 누른 것은 **요청이지 결과가 아니다.** 클릭만으로 붙은 것처럼 그리면, 안 붙었을 때
             화면이 조용히 거짓말을 한다. */
          assert.ok((await page.locator('#state-note').textContent()).includes('컴패니언이 없습니다'),
            '클릭만으로 attached 로 그리지 않는다');
          assert.equal(await page.locator('#say').inputValue(), draftBefore, '초안이 그대로다');
          assert.equal(await page.locator('#reply-mode').isVisible(), modeBefore, '답변 모드가 그대로다');

          /* 치우고 나간다. 이 묶음은 page 를 공유하므로 남긴 지속 안내·열린 질문·초안이 **뒤
             시나리오의 높이를 바꾼다** — 실제로 뒤의 긴 답변 시험을 한 번 빨갛게 만들었다. */
          await page.evaluate((msg) => window.postMessage(msg, '*'), createStateMessage('attached', { text: '', offerStart: false }));
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({ rows: [], ask: null }));
          await page.locator('#say').fill('');
          await page.waitForFunction(() => document.querySelector('#state-note')?.textContent === '');
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
            await page.waitForFunction(() => document.querySelector('#state-note button') !== null);
            // 일시 알림도 **진짜 전송**으로 만든다 — 손으로 넣은 글자는 이 경로를 아무것도 안 잰다.
            await page.locator('#say').fill('낮은 판에서의 전송');
            await page.locator('#send').click();
            await page.waitForFunction(() => document.querySelector('#note')?.textContent === 'sending…');

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

            /* ⚠ **높이가 양수인 것만으로는 부족하다.** 지속 안내는 낮은 판에서 줄어들며 스크롤되므로,
               그 안의 시작 단추가 화면에 남아 있어도 **손이 닿지 않으면** 나가는 길이 없는 것과 같다.
               그래서 키보드로 닿아 실제로 눌리는지까지 잰다. */
            const startBtn = page.locator('#state-note button');
            await startBtn.focus();
            const focused = await page.evaluate(() => document.activeElement === document.querySelector('#state-note button'));
            assert.ok(focused, `${w}×${h}: 시작 단추에 키보드 포커스가 닿는다`);
            const beforeStart = await page.evaluate(() => window.__posted.length);
            await page.keyboard.press('Enter');
            const afterStart = await page.evaluate(() => window.__posted);
            assert.equal(afterStart.length, beforeStart + 1, `${w}×${h}: 키보드로 시작이 한 번 나간다`);
            assert.equal(afterStart[afterStart.length - 1].kind, 'start', `${w}×${h}: 나간 것이 start 다`);
            // 스크롤 안이어도 단추 자체가 뷰포트 밖으로 잘려 나가면 안 된다.
            const btnBox = await startBtn.boundingBox();
            assert.ok(btnBox && btnBox.y >= -0.5 && btnBox.y + btnBox.height <= h + 0.5,
              `${w}×${h}: 시작 단추가 화면 안에 있다 — y=${btnBox?.y?.toFixed(1)}, h=${btnBox?.height?.toFixed(1)}`);
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
