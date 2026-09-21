import assert from 'node:assert/strict';
import { createRowsMessage, createStateMessage } from '../../transcript-fixtures.mjs';

export const layoutScenarios = [
      {
        id: 'layout_empty_transcript_notice',
        name: '빈 전사 안내 — 상태 다섯 × 행 0/n, 질문 유무, 메시지 순서 (§6.10)',
        run: async (page) => {
          const EMPTY = '아직 주고받은 말이 없습니다';
          const shown = () => page.evaluate(() => {
            const e = document.getElementById('empty-note');
            return { hidden: e.hidden, text: e.textContent.trim() };
          });
          const rowsMsg = (rows, ask) => page.evaluate((m) => window.postMessage(m, '*'),
            createRowsMessage({ session: 'sess-empty', rows, ask: ask || null }));
          const stateMsg = (st, text, offerStart) => page.evaluate((m) => window.postMessage(m, '*'),
            createStateMessage(st, { text: text || '', offerStart: !!offerStart }));
          const oneRow = [{ who: 'agent', label: 'magi', text: 'turn 1' }];
          const someAsk = { kind: 'question', callId: 'q-empty', what: '무엇을 할까요?' };

          /* 상태 다섯 × 행 0/n. ⚠ 행이 0 인 것으로 데몬 부재를 추정하지 않는다 — not-running 과
             unknown 은 이 자리에서 **침묵하고**, 문구와 나가는 길은 state-note 가 혼자 맡는다. */
          for (const [st, speaks] of [['attached', true], ['working', true], ['waiting', true],
                                      ['not-running', false], ['unknown', false]]) {
            await rowsMsg([]);
            await stateMsg(st, st === 'attached' ? '' : '상태 문구', st === 'not-running' || st === 'unknown');
            await page.waitForFunction((want) => {
              const e = document.getElementById('empty-note');
              return (!e.hidden && e.textContent.includes('아직 주고받은')) === want;
            }, speaks);
            const s0 = await shown();
            assert.equal(!s0.hidden, speaks, `${st}·행 0: 빈 안내 표시 = ${speaks}`);
            if (speaks) assert.ok(s0.text.includes(EMPTY), `${st}: 문구가 전사에 대한 사실만 말한다`);

            await rowsMsg(oneRow);
            await page.waitForFunction(() => document.getElementById('empty-note').hidden === true);
            assert.equal((await shown()).hidden, true, `${st}·행 n: 행이 있으면 안 뜬다`);
          }

          // 대기 질문이 있으면 「아무 말도 없다」는 거짓이므로 안 띄운다.
          await stateMsg('attached');
          await rowsMsg([], someAsk);
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === true);
          assert.equal((await shown()).hidden, true, '대기 질문이 있으면 빈 대화 문구를 안 띄운다');

          /* ⚠ 메시지 **순서가 뒤바뀌어도** 최신 조합으로 다시 판정한다. 한쪽만 보고 그리면 늦게 온
             쪽이 반영 안 된 화면이 남는다. */
          await rowsMsg([]);                       // 먼저 rows, 나중 state
          await stateMsg('attached');
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);
          await stateMsg('attached');              // 먼저 state, 나중 rows
          await rowsMsg(oneRow);
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === true);
          await rowsMsg([]);
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);

          // 세션을 바꾸면 그 세션의 행 수로 다시 정해진다 — 옛 세션의 빈 안내가 안 남는다.
          await page.evaluate((m) => window.postMessage(m, '*'),
            createRowsMessage({ session: 'sess-other', rows: oneRow }));
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === true);

          // 끊겨도 이미 있는 전사는 그대로 — 지속 상태는 state-note 가 맡는다.
          await stateMsg('not-running', '이 폴더에서 도는 컴패니언이 없습니다.', true);
          await page.waitForFunction(() => document.querySelector('#state-note button') !== null);
          assert.equal((await shown()).hidden, true, '행이 있으면 끊겨도 빈 안내는 안 선다');
          assert.equal(await page.locator('#rows').locator('.row, [class*=row]').count() > 0, true, '전사가 그대로 남는다');
          // ⚠ 시작 단추는 **한 곳에만** 있다.
          assert.equal(await page.locator('#state-note button').count(), 1, '시작 단추는 지속 안내에 하나');
          assert.equal(await page.locator('#empty-note button').count(), 0, '빈 안내에는 단추가 없다');

          // 치우고 나간다(page 공유).
          await stateMsg('attached');
          await page.evaluate((m) => window.postMessage(m, '*'), createRowsMessage({ rows: [], ask: null }));
        }
      },
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
      },
      {
        id: 'layout_empty_note_state_transition_ordering',
        name: '메시지 순서를 상태 변화로 검증 — unknown + 행 n → rows([]) → attached, 역순, 왕복 (§6.11-1)',
        run: async (page) => {
          const rowsMsg = (rows, ask) => page.evaluate((m) => window.postMessage(m, '*'),
            createRowsMessage({ rows, ask: ask || null }));
          const stateMsg = (st, text, offerStart) => page.evaluate((m) => window.postMessage(m, '*'),
            createStateMessage(st, { text: text || '', offerStart: !!offerStart }));
          const isHidden = () => page.evaluate(() => document.getElementById('empty-note').hidden);
          const oneRow = [{ who: 'agent', label: 'magi', text: 'turn 1' }];
          let syncSeq = 0;
          /** 직전 postMessage 가 처리된 뒤 resolve 한다 — FIFO 순서 보장. */
          const fifoSync = () => {
            const key = `__sync_transition_${++syncSeq}`;
            return page.evaluate((k) => new Promise((resolve) => {
              window.addEventListener('message', function onSync(e) {
                if (e.data && e.data[k]) { window.removeEventListener('message', onSync); resolve(); }
              });
              window.postMessage({ [k]: true }, '*');
            }), key);
          };

          /* ① unknown + 행 n → rows([]) → attached: 중간에는 숨김, 마지막에는 표시. */
          await stateMsg('unknown', '컴패니언 상태를 아직 알 수 없습니다.', true);
          await rowsMsg(oneRow);
          await fifoSync();
          assert.equal(await isHidden(), true, 'unknown + 행 n: 빈 안내 숨김');

          await rowsMsg([]);
          await fifoSync();
          assert.equal(await isHidden(), true, 'unknown + 행 0 → rows([]) 중간: 아직 숨김 (unknown 침묵)');

          await stateMsg('attached');
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);
          assert.equal(await isHidden(), false, 'unknown→rows([])→attached 마지막: 표시');

          /* ② 역순: unknown + 행 n → attached → rows([]) */
          await stateMsg('unknown', '컴패니언 상태를 아직 알 수 없습니다.', true);
          await rowsMsg(oneRow);
          await fifoSync();
          assert.equal(await isHidden(), true, '② 초기: unknown + 행 n = 숨김');

          await stateMsg('attached');
          await fifoSync();
          assert.equal(await isHidden(), true, 'unknown→attached→(행 아직 있음) 중간: 숨김');

          await rowsMsg([]);
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);
          assert.equal(await isHidden(), false, 'unknown→attached→rows([]) 마지막: 표시');

          /* ③ 행 0에서 attached → not-running → attached 왕복. */
          await rowsMsg([]);
          await stateMsg('attached');
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);
          assert.equal(await isHidden(), false, '왕복 시작: attached + 행 0 = 표시');

          await stateMsg('not-running', '이 폴더에서 도는 컴패니언이 없습니다.', true);
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === true);
          assert.equal(await isHidden(), true, '왕복 중: not-running = 숨김');

          await stateMsg('attached');
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);
          assert.equal(await isHidden(), false, '왕복 끝: attached 복귀 = 표시');

          // 치우고 나간다.
          await stateMsg('attached');
          await rowsMsg([], null);
          await fifoSync();
        }
      },
      {
        id: 'layout_empty_note_visible_bounds',
        name: '빈 안내가 표시된 화면에서 안내·스크롤·입력창·Send 경계를 잰다 (§6.11-2)',
        run: async (page) => {
          let syncSeq = 0;
          const fifoSync = () => {
            const key = `__sync_bounds_${++syncSeq}`;
            return page.evaluate((k) => new Promise((resolve) => {
              window.addEventListener('message', function onSync(e) {
                if (e.data && e.data[k]) { window.removeEventListener('message', onSync); resolve(); }
              });
              window.postMessage({ [k]: true }, '*');
            }), key);
          };
          const rowsMsg = (rows, ask) => page.evaluate((m) => window.postMessage(m, '*'),
            createRowsMessage({ rows, ask: ask || null }));
          const stateMsg = (st, text, offerStart) => page.evaluate((m) => window.postMessage(m, '*'),
            createStateMessage(st, { text: text || '', offerStart: !!offerStart }));
          const someAsk = {
            kind: 'permission', callId: 'perm-bounds',
            what: 'edit', filePath: 'b.txt',
            args: JSON.stringify({ path: 'b.txt', old: 'x', new: 'y' })
          };

          try {
            for (const [w, h] of [[320, 600], [1200, 200]]) {
              await page.setViewportSize({ width: w, height: h });

              // attached + 행 0 + ask 없음 → empty-note 표시
              await stateMsg('attached');
              await rowsMsg([]);
              await fifoSync();
              await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);
              assert.equal(
                await page.evaluate(() => document.getElementById('empty-note').hidden), false,
                `${w}×${h}: 빈 안내가 표시된다`
              );

              // 경계를 잰다
              const box = async (sel) => page.locator(sel).first().boundingBox();
              const [emptyBox, , sayBox, sendBox] = await Promise.all([
                box('#empty-note'), box('#scroll'), box('#say'), box('#send')
              ]);

              // 안내가 화면에 그려진다
              assert.ok(emptyBox && emptyBox.width > 0 && emptyBox.height > 0,
                `${w}×${h}: 빈 안내가 크기를 갖는다`);

              // 입력·Send 상하좌우 뷰포트 경계 확인
              for (const [label, b] of [['입력창', sayBox], ['Send', sendBox]]) {
                assert.ok(b && b.width > 0 && b.height > 0,
                  `${w}×${h}: ${label}가 양의 크기를 갖는다`);
                assert.ok(b.x >= 0 && b.y >= 0,
                  `${w}×${h}: ${label}의 좌상 꼭짓점이 뷰포트 안이다`);
                assert.ok(b.x + b.width <= w + 0.5 && b.y + b.height <= h + 0.5,
                  `${w}×${h}: ${label}의 우하 꼭짓점이 뷰포트 안이다`);
              }

              // 스크롤 컨테이너의 clientHeight 가 양수인지 확인
              const scrollMetrics = await page.evaluate(() => {
                const el = document.getElementById('scroll');
                return { clientHeight: el.clientHeight, scrollHeight: el.scrollHeight };
              });
              assert.ok(scrollMetrics.clientHeight > 0,
                `${w}×${h}: 스크롤 clientHeight 가 양수`);

              // 넘칠 때 스크롤 실측: scrollTop 변화와 안내 시작·끝이 가시 영역에 들어오는지
              if (scrollMetrics.scrollHeight > scrollMetrics.clientHeight) {
                const scrollCheck = await page.evaluate(() => {
                  const el = document.getElementById('scroll');
                  const note = document.getElementById('empty-note');
                  // 맨 위로 올려 안내 시작 확인
                  el.scrollTop = 0;
                  const topRect = note.getBoundingClientRect();
                  const startVisible = topRect.top >= el.getBoundingClientRect().top;
                  // 맨 아래로 내려 안내 끝 확인
                  el.scrollTop = el.scrollHeight;
                  const bottomRect = note.getBoundingClientRect();
                  const endVisible = bottomRect.bottom <= el.getBoundingClientRect().bottom + 1;
                  // scrollTop 이 실제로 변했는지
                  const scrollMoved = el.scrollTop > 0;
                  el.scrollTop = 0;
                  return { startVisible, endVisible, scrollMoved };
                });
                assert.ok(scrollCheck.scrollMoved,
                  `${w}×${h}: 스크롤이 실제로 움직인다`);
                assert.ok(scrollCheck.startVisible,
                  `${w}×${h}: 맨 위에서 빈 안내 시작이 보인다`);
                assert.ok(scrollCheck.endVisible,
                  `${w}×${h}: 맨 아래에서 빈 안내 끝이 보인다`);
              }

              // 승인 질문을 도착시켜 빈 안내 숨김과 승인 단추 접근을 확인한다
              await rowsMsg([{ who: 'agent', label: 'magi', text: 'turn' }], someAsk);
              await fifoSync();
              await page.waitForFunction(() => document.getElementById('empty-note').hidden === true);
              assert.equal(
                await page.evaluate(() => document.getElementById('empty-note').hidden), true,
                `${w}×${h}: 승인 도착 → 빈 안내 숨김`
              );

              // 승인 단추에 키보드 포커스 → Enter → decision 1회 게시 확인
              const postedBefore = await page.evaluate(() =>
                window.__posted.filter(m => m.kind === 'answer').length);
              const approveBtn = page.locator('#ask-controls .acts button.approval-btn').first();
              const approveBtnBox = await approveBtn.boundingBox();
              assert.ok(approveBtnBox && approveBtnBox.width > 0,
                `${w}×${h}: 승인 단추가 크기를 갖는다`);
              assert.ok(approveBtnBox.x >= 0 && approveBtnBox.y >= 0
                && approveBtnBox.x + approveBtnBox.width <= w + 0.5
                && approveBtnBox.y + approveBtnBox.height <= h + 0.5,
                `${w}×${h}: 승인 단추가 뷰포트 안에 있다`);
              await approveBtn.focus();
              await page.keyboard.press('Enter');
              await page.waitForFunction((prev) =>
                window.__posted.filter(m => m.kind === 'answer').length > prev, postedBefore);
              const answerCount = await page.evaluate((prev) =>
                window.__posted.filter(m => m.kind === 'answer').length - prev, postedBefore);
              assert.equal(answerCount, 1,
                `${w}×${h}: Enter 로 answer 가 정확히 1회 게시된다`);

              // 다음 반복을 위해 질문을 해제한다
              await rowsMsg([], null);
              await fifoSync();
            }
          } finally {
            // 치우고 나간다
            await page.setViewportSize({ width: 800, height: 600 });
            await stateMsg('attached');
            await rowsMsg([], null);
            await fifoSync();
          }
        }
      },
      {
        id: 'layout_draft_survives_empty_note_transitions',
        name: '빈 안내 표시·제거·재렌더 중 초안과 포커스가 보존된다 (§6.11-3)',
        run: async (page) => {
          let syncSeq = 0;
          const fifoSync = () => {
            const key = `__sync_draft_${++syncSeq}`;
            return page.evaluate((k) => new Promise((resolve) => {
              window.addEventListener('message', function onSync(e) {
                if (e.data && e.data[k]) { window.removeEventListener('message', onSync); resolve(); }
              });
              window.postMessage({ [k]: true }, '*');
            }), key);
          };
          const rowsMsg = (rows, ask) => page.evaluate((m) => window.postMessage(m, '*'),
            createRowsMessage({ rows, ask: ask || null }));
          const stateMsg = (st, text, offerStart) => page.evaluate((m) => window.postMessage(m, '*'),
            createStateMessage(st, { text: text || '', offerStart: !!offerStart }));
          const oneRow = [{ who: 'agent', label: 'magi', text: 'turn 1' }];

          // 일반 초안을 작성하고 입력창에 포커스를 둔다
          await page.locator('#say').fill('일반 초안 A');
          await page.locator('#say').focus();
          const postedBefore = await page.evaluate(() => window.__posted.length);

          // 빈 안내 표시 (attached + 행 0)
          await stateMsg('attached');
          await rowsMsg([]);
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);

          // 값·포커스·게시 수 확인
          assert.equal(await page.locator('#say').inputValue(), '일반 초안 A', '빈 안내 표시 후 초안 보존');
          const focusedAfterShow = await page.evaluate(() => document.activeElement?.id);
          assert.equal(focusedAfterShow, 'say', '빈 안내 표시 후 포커스 보존');
          assert.equal(await page.evaluate(() => window.__posted.length), postedBefore,
            '빈 안내 표시가 메시지를 보내지 않는다');

          // 빈 안내 제거 (행 도착)
          await rowsMsg(oneRow);
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === true);
          assert.equal(await page.locator('#say').inputValue(), '일반 초안 A', '빈 안내 제거 후 초안 보존');
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say', '빈 안내 제거 후 포커스 보존');

          // 같은 세션 재렌더 (같은 행을 다시 보낸다)
          await rowsMsg(oneRow);
          await fifoSync();
          assert.equal(await page.locator('#say').inputValue(), '일반 초안 A', '재렌더 후 초안 보존');

          // 선택형 질문 → 직접 입력으로 답변 B 작성 → 잠금 → 재제출 차단
          const someAsk = { kind: 'question', callId: 'q-draft', what: '무엇을 할까요?', options: ['선택 A', '선택 B'] };
          await rowsMsg(oneRow, someAsk);
          await fifoSync();
          await page.waitForSelector('#ask-body:not([hidden])');
          // 직접 입력으로 진입하면 일반 초안 A가 별도로 보관된다.

          await page.locator('#ask-controls .direct-btn').click();
          // 답변 B 를 실제로 작성한다
          await page.locator('#say').fill('질문 답변 B');
          assert.equal(await page.locator('#say').inputValue(), '질문 답변 B', '답변 B 작성 확인');

          // state 변경을 보낸다 — 답변 B·답변 모드·포커스가 보존되는지 확인
          await stateMsg('working', '작업 중');
          await fifoSync();
          assert.equal(await page.locator('#say').inputValue(), '질문 답변 B',
            'state 변경이 답변 B 를 바꾸지 않는다');
          assert.equal(await page.locator('#reply-mode').isVisible(), true);
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say');

          // 같은 요청의 rows 재전송 — 답변 B·모드 유지 확인
          await rowsMsg(oneRow, someAsk);
          await fifoSync();
          await page.waitForSelector('#ask-body:not([hidden])');
          assert.equal(await page.locator('#say').inputValue(), '질문 답변 B',
            'rows 재전송이 답변 B 를 바꾸지 않는다');

          assert.equal(await page.locator('#reply-mode').isVisible(), true);
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say');

          // Send 로 답변 B 를 제출한다 — reply 게시와 잠금 확인
          const postedBeforeReply = await page.evaluate(() => window.__posted.length);
          await page.locator('#send').click();
          await page.waitForFunction((prev) => window.__posted.length > prev, postedBeforeReply);
          const replyAttempt = await page.evaluate(() =>
            window.__posted.filter(m => m.kind === 'reply').slice(-1)[0]);
          assert.ok(replyAttempt, 'reply 가 게시되었다');
          assert.equal(replyAttempt.text, '질문 답변 B', 'reply 본문이 B 이다');

          // 잠금 상태 확인: reply 전송 직후 pendingQuestion 이 null 이 되므로
          // answer 모드에서는 탈출하지만 inFlightReplies 에 기록은 남아 aria-busy 는 유지된다.
          const askControls = page.locator('#ask-controls');
          assert.equal(await askControls.getAttribute('aria-busy'), 'true', '답변 전송 후 aria-busy');

          // reply 전송 뒤 일반 초안 'A' 가 동기적으로 복원되었는지 확인
          assert.equal(await page.locator('#say').inputValue(), '일반 초안 A',
            'reply 전송 후 일반 초안 복원');

          // 잠긴 상태에서 같은 갱신을 보낸다 — aria-busy 유지 확인
          await rowsMsg(oneRow, someAsk);
          await fifoSync();
          assert.equal(await askControls.getAttribute('aria-busy'), 'true',
            '갱신 뒤에도 잠금 유지');

          // 답변 모드로 재진입한 뒤 갱신해도 중복 제출은 차단된다.
          await page.locator('#ask-controls .direct-btn').click();
          await page.locator('#say').fill('대기 중 수정 초안 C');
          await stateMsg('working', '작업 중');
          await rowsMsg(oneRow, someAsk);
          await fifoSync();
          assert.equal(await page.locator('#reply-mode').isVisible(), true);
          assert.equal(await page.locator('#say').inputValue(), '대기 중 수정 초안 C');
          assert.equal(await page.locator('#send').isDisabled(), true);
          const choice = page.locator('#ask-controls .choice-btn').first();
          assert.equal(await choice.isDisabled(), true);
          const replyCount = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply').length);
          await page.locator('#say').press('Enter');
          await choice.evaluate(el => el.click()); // disabled 버튼의 표준 DOM 클릭은 실행되지 않는다.
          await fifoSync();
          assert.equal(await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply').length), replyCount);
          await page.locator('#say').press('Escape');

          // 정상 replyResult 로 잠금을 해제한다
          await page.evaluate((att) => {
            window.postMessage({
              kind: 'replyResult',
              callId: att.callId, attemptId: att.attemptId,
              ok: true, session: att.session,
              companionKey: att.companionKey,
              generation: att.generation,
              webviewId: att.webviewId,
            }, '*');
          }, replyAttempt);
          await fifoSync();
          assert.equal(await askControls.getAttribute('aria-busy'), null,
            'replyResult 후 잠금 해제');

          // 질문이 사라지면 일반 초안 'A' 가 복원된다
          await rowsMsg(oneRow);
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('ask-body')?.hidden === true);
          assert.equal(await page.locator('#say').inputValue(), '일반 초안 A',
            '질문 해제 후 일반 초안이 복원된다');

          // 치우고 나간다 — 다음 시나리오에 질문·초안을 남기지 않는다
          await stateMsg('attached');
          await rowsMsg([], null);
          await fifoSync();
          await page.locator('#say').fill('');
        }
      },
      {
        id: 'layout_first_row_arrival_and_scroll_tracking',
        name: '빈 안내에서 첫 행으로 바뀔 때 하단 추적, 위로 읽을 때 scrollTop 보존 (§6.11-4)',
        run: async (page) => {
          let syncSeq = 0;
          const fifoSync = () => {
            const key = `__sync_scroll_${++syncSeq}`;
            return page.evaluate((k) => new Promise((resolve) => {
              window.addEventListener('message', function onSync(e) {
                if (e.data && e.data[k]) { window.removeEventListener('message', onSync); resolve(); }
              });
              window.postMessage({ [k]: true }, '*');
            }), key);
          };
          const rowsMsg = (rows, ask) => page.evaluate((m) => window.postMessage(m, '*'),
            createRowsMessage({ rows, ask: ask || null }));
          const stateMsg = (st, text, offerStart) => page.evaluate((m) => window.postMessage(m, '*'),
            createStateMessage(st, { text: text || '', offerStart: !!offerStart }));
          const manyRows = Array.from({ length: 30 }, (_, i) =>
            ({ who: 'agent', label: 'magi', text: `turn ${i + 1}\n`.repeat(5) }));

          // 빈 안내를 세운다
          await stateMsg('attached');
          await rowsMsg([]);
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === false);

          /* 첫 행 도착 → 하단 추적 유지: 바닥에 있었으면 새 내용도 바닥에 따라간다. */
          await page.evaluate(() => {
            const el = document.getElementById('scroll');
            el.scrollTop = el.scrollHeight;
          });
          await rowsMsg(manyRows.slice(0, 1));
          await fifoSync();
          await page.waitForFunction(() => document.getElementById('empty-note').hidden === true);
          await page.waitForFunction(() => document.querySelectorAll('#rows .row').length > 0);
          const atBottom1 = await page.evaluate(() => {
            const el = document.getElementById('scroll');
            return Math.abs(el.scrollHeight - el.scrollTop - el.clientHeight) < 5;
          });
          assert.ok(atBottom1, '첫 행 도착 후 하단 추적 유지');

          /* 많은 행을 넣어 스크롤을 길게 만든다 */
          await rowsMsg(manyRows);
          await fifoSync();
          await page.waitForFunction((n) => document.querySelectorAll('#rows .row').length >= n, manyRows.length);
          const atBottom2 = await page.evaluate(() => {
            const el = document.getElementById('scroll');
            return Math.abs(el.scrollHeight - el.scrollTop - el.clientHeight) < 5;
          });
          assert.ok(atBottom2, '많은 행 도착 후에도 하단 추적 유지');

          /* 위로 올려 읽는다 — state 재수신으로 scrollTop 을 빼앗지 않는다 */
          await page.evaluate(() => { document.getElementById('scroll').scrollTop = 50; });
          const scrollBefore = await page.evaluate(() => document.getElementById('scroll').scrollTop);
          assert.equal(scrollBefore, 50, '스크롤을 50 으로 올렸다');

          // state 재수신
          await stateMsg('working', '작업 중');
          await fifoSync();
          const scrollAfterState = await page.evaluate(() => document.getElementById('scroll').scrollTop);
          assert.equal(scrollAfterState, 50, 'state 재수신이 scrollTop 을 빼앗지 않는다');

          // 같은 행을 다시 보내도 위로 읽는 위치가 유지된다
          await rowsMsg(manyRows);
          await fifoSync();
          await page.waitForFunction((n) => document.querySelectorAll('#rows .row').length >= n, manyRows.length);
          const scrollAfterRows = await page.evaluate(() => document.getElementById('scroll').scrollTop);
          assert.equal(scrollAfterRows, 50, 'rows 재전송이 scrollTop 을 빼앗지 않는다');

          // 치우고 나간다
          await stateMsg('attached');
          await rowsMsg([], null);
          await fifoSync();
        }
      }
];

export const layoutBundle = {
  name: 'layout',
  description: '레이아웃·보고서',
  scenarios: layoutScenarios,
};
