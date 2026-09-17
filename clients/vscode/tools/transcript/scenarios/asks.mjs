import assert from 'node:assert/strict';
import { createRowsMessage, createReplyResultMessage } from '../../transcript-fixtures.mjs';

export const asksScenarios = [
      {
        id: 'asks_free_text_answer_mode',
        name: '자유 텍스트 질문 자동 답변 모드 진입 및 일반 초안 격리·복원 (Condition 9)',
        run: async (page) => {
          await page.setViewportSize({ width: 420, height: 600 });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [],
            ask: null
          }));
          await page.locator('#say').fill('새 작업 초안 작성 중...');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'free-q1',
              what: '이 파일의 이름을 무엇으로 변경할까요?',
              options: []
            }
          }));
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
        }
      },
      {
        id: 'asks_multiple_choice_and_escape',
        name: '선택형 질문, 직접 입력 버튼, 질문 초안 보존, Esc 취소 및 선택지 클릭 (Condition 10)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'choice-q2',
              what: '배포 환경을 선택하세요',
              options: ['스테이징 환경', '운영(프로덕션) 환경']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 스테이징 환경")');
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
        }
      },
      {
        id: 'asks_send_rejection_and_disconnect',
        name: '전송 거절 및 연결 단절 시 질문 초안 복원과 일반 초안 오염 방지 (Condition 11, 12)',
        run: async (page) => {
          await page.locator('#say').fill('원래 일반 프롬프트 초안');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-reject',
              what: '거절 테스트 질문',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('거절될 답변 내용');
          await page.locator('#send').click();

          // While in flight, say restores general draft
          assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안');
          const postedReject1 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-reject').slice(-1)[0]);
          // Daemon sends refusal replyResult
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-reject',
            attemptId: att.attemptId,
            ok: false,
            error: 'companion refused to accept answer',
            text: '거절될 답변 내용',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedReject1);

          // Webview re-enters answer mode for q-reject and restores failed draft
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await page.locator('#say').inputValue(), '거절될 답변 내용', 'failed reply restored into answer mode');
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안', 'general draft intact without failed answer prepended');

          // Connection disconnect (Condition 12)
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('재시도할 답변');
          await page.locator('#send').click();
          const postedReject2 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-reject').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-reject',
            attemptId: att.attemptId,
            ok: false,
            error: 'no companion is listening on this workspace.',
            text: '재시도할 답변',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedReject2);
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await page.locator('#say').inputValue(), '재시도할 답변', 'disconnected reply restored into answer mode');
          await page.keyboard.press('Escape');
        }
      },
      {
        id: 'asks_late_failure_after_question_replacement',
        name: '질문 교체 뒤 늦은 실패 응답 무시 및 현재 질문 초안 보호 (Condition 13)',
        run: async (page) => {
          // Send answer for q-reject
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('구 질문 답변');
          await page.locator('#send').click();
          const postedReject3 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-reject').slice(-1)[0]);

          // Question replaced by q-new before q-reject failure arrives
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-new',
              what: '새로운 질문',
              options: ['신규 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 신규 1")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('신규 질문에 타이핑 중인 답변');

          // Late failure for q-old arrives!
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-reject',
            attemptId: att.attemptId,
            ok: false,
            error: 'timeout',
            text: '구 질문 답변',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedReject3);

          assert.equal(await page.locator('#say').inputValue(), '신규 질문에 타이핑 중인 답변', 'late failure response from old question did not overwrite current question draft');
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#say').inputValue(), '원래 일반 프롬프트 초안');
        }
      },
      {
        id: 'asks_duplicate_reply_and_stale_failure_isolation',
        name: '중복 전송 차단, 리비전 버전 격리 및 신규 시도 성공 (Condition 16, 17)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-stale',
              what: '버전 격리 테스트 질문',
              options: ['옵션 Alpha', '옵션 Beta']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('답변 A');

          // Send 답변 A -> attempt 1
          await page.locator('#send').click();
          const replyAttempt1 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-stale').slice(-1)[0]);
          assert.ok(replyAttempt1);
          assert.equal(replyAttempt1.text, '답변 A');
          assert.ok(replyAttempt1.attemptId > 0);

          // Condition 17: Duplicate reply blocked while attempt 1 is in-flight
          const alphaBtn = page.locator('#ask-controls button:text("1. 옵션 Alpha")');
          assert.equal(await alphaBtn.isDisabled(), true, 'choice button is disabled while attempt 1 is in-flight');
          assert.equal(await page.locator('#ask-controls').getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          assert.equal(await page.locator('#send').isDisabled(), true, 'send button disabled in answer mode while in-flight');
          await page.locator('#say').press('Enter');
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…');

          // Condition 16: User edits to '수정된 답변 B', late failure for attempt 1 arrives
          await page.locator('#say').fill('수정된 답변 B');
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-stale',
            attemptId: att.attemptId,
            ok: false,
            error: 'network timeout',
            text: '답변 A',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), replyAttempt1);

          assert.equal(await page.locator('#say').inputValue(), '수정된 답변 B', 'fresh revision B was NOT overwritten by stale failure of A');

          // Lock released, send attempt 2
          await page.locator('#send').click();
          const replyAttempt2 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-stale').slice(-1)[0]);
          assert.equal(replyAttempt2.text, '수정된 답변 B');
          assert.ok(replyAttempt2.attemptId > replyAttempt1.attemptId, 'attemptId incremented for new attempt');

          // Simulate success for attempt 2
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-stale',
            attemptId: att.attemptId,
            ok: true,
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), replyAttempt2);

          // Dismiss question
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'all done' }],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);
        }
      },
      {
        id: 'asks_resend_isolation_and_idless_response',
        name: 'A 실패 → B 재전송 → A 결과 재도착 격리 및 ID 없는 응답 무시 (Condition 18)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'question',
              callId: 'q-resend-test',
              what: '재전송 격리 테스트 질문',
              options: ['옵션 1', '옵션 2']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('답변 A');

          // Step 1: Send 답변 A
          await page.locator('#send').click();
          const attemptA = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-resend-test').slice(-1)[0]);
          assert.ok(attemptA && attemptA.attemptId);

          // Step 2: A failure arrives -> restored into answer mode
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            attemptId: att.attemptId,
            ok: false,
            error: 'initial failure',
            text: '답변 A',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), attemptA);
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await page.locator('#say').inputValue(), '답변 A');

          // Step 3: Modify to 답변 B and resend
          await page.locator('#say').fill('답변 B');
          await page.locator('#send').click();
          const attemptB = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-resend-test').slice(-1)[0]);
          assert.ok(attemptB && attemptB.attemptId > attemptA.attemptId);

          await page.locator('#ask-controls button:text("직접 입력")').click();
          assert.equal(await page.locator('#say').inputValue(), '답변 B');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'answer mode active for B');

          // Step 4-1: ID-less response arrives -> must be ignored
          await page.evaluate(() => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            ok: false,
            error: 'malformed no-id response',
            text: '오염 텍스트'
          }, '*'));
          assert.equal(await page.locator('#say').inputValue(), '답변 B', 'ID-less response ignored; draft preserved');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'answer mode preserved after ID-less response');

          // Step 4-2: Stale attempt A arrives -> must be ignored due to attemptId mismatch
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            attemptId: att.attemptId,
            ok: false,
            error: 'stale duplicate error for A',
            text: '답변 A',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), attemptA);

          assert.equal(await page.locator('#say').inputValue(), '답변 B', 'draft B preserved against stale attempt A arrival');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'reply-mode preserved against stale attempt A arrival');

          // In-flight lock for B remains active
          assert.equal(await page.locator('#send').isDisabled(), true, 'send button disabled in answer mode while in-flight');
          await page.locator('#say').press('Enter');
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…', 'lock for attempt B still active');
          const opt1Btn = page.locator('#ask-controls button:text("1. 옵션 1")');
          assert.equal(await opt1Btn.isDisabled(), true, 'choice click blocked by in-flight lock B');

          // Step 5: B의 실제 성공 응답 도착 -> 정상 처리 및 잠금 해제
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            attemptId: att.attemptId,
            ok: true,
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'test-session',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), attemptB);
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn done' }],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);
        }
      },
      {
        id: 'asks_malformed_payload_rejected_without_mutation',
        name: '비정상 payload 디스패치 시 파서 거부 및 기존 DOM·질문·초안 상태 보존 (§2.3)',
        run: async (page) => {
          // 1. Setup normal rows, pending ask, and active draft
          const initialQuestion = {
            kind: 'question',
            callId: 'q-malformed-guard',
            what: '보존되어야 할 질문 제목',
            options: ['옵션 A', '옵션 B']
          };
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [
              { who: 'user', label: 'You', text: '이전 사용자 입력' },
              { who: 'agent', label: 'magi', text: '이전 에이전트 답변' }
            ],
            ask: initialQuestion,
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('작성 중인 답변 초안');

          // 2. Snapshot full DOM and state before malformed dispatch
          const beforeState = await page.evaluate(() => ({
            sayValue: document.getElementById('say').value,
            replyModeVisible: !document.getElementById('reply-mode').hidden,
            replyTarget: document.querySelector('#reply-mode .reply-target')?.textContent || '',
            rowsCount: document.querySelectorAll('#rows .row').length,
            rowsHtml: document.getElementById('rows').innerHTML,
            askBodyHtml: document.getElementById('ask-body').innerHTML,
            askControlsHtml: document.getElementById('ask-controls').innerHTML,
          }));
          assert.equal(beforeState.sayValue, '작성 중인 답변 초안');
          assert.equal(beforeState.replyModeVisible, true);
          assert.equal(beforeState.rowsCount, 2);

          // 3. Dispatch malformed messages directly without fixture correction
          await page.evaluate(() => {
            // Malformed rows (non-array)
            window.postMessage({ kind: 'rows', rows: 'invalid-non-array' }, '*');
            // Malformed rows (missing session)
            window.postMessage({ kind: 'rows', rows: [] }, '*');
            // Malformed rows (non-string refs)
            window.postMessage({ kind: 'rows', session: 's1', rows: [], refs: [123] }, '*');
            // Malformed ask: valid rows/session/refs context but unregistered ask.kind (§5.8.3)
            window.postMessage({
              kind: 'rows',
              session: 's-default',
              rows: [
                { who: 'user', label: 'You', text: '이전 사용자 입력' },
                { who: 'agent', label: 'magi', text: '이전 에이전트 답변' }
              ],
              refs: [],
              ask: {
                kind: 'unregistered_ask_kind',
                callId: 'q-malformed-guard',
                what: '보존되어야 할 질문 제목',
                options: ['옵션 A', '옵션 B']
              }
            }, '*');
            // Malformed replyResult (empty callId)
            window.postMessage({ kind: 'replyResult', callId: '', attemptId: 1, ok: true }, '*');
            // Malformed state (missing note)
            window.postMessage({ kind: 'state', state: {} }, '*');
            // Unknown kind
            window.postMessage({ kind: 'unknown_kind_never_seen' }, '*');
            // Primitive non-object payloads
            window.postMessage(null, '*');
            window.postMessage(12345, '*');
            window.postMessage('string payload', '*');
          });

          // 4. Test synchronization using postMessage FIFO ordering without arbitrary sleep
          await page.evaluate(() => new Promise((resolve) => {
            window.addEventListener('message', function onSync(e) {
              if (e.data && e.data.__syncGuard) {
                window.removeEventListener('message', onSync);
                resolve();
              }
            });
            window.postMessage({ __syncGuard: true }, '*');
          }));

          // 5. Verify full state preservation
          const afterState = await page.evaluate(() => ({
            sayValue: document.getElementById('say').value,
            replyModeVisible: !document.getElementById('reply-mode').hidden,
            replyTarget: document.querySelector('#reply-mode .reply-target')?.textContent || '',
            rowsCount: document.querySelectorAll('#rows .row').length,
            rowsHtml: document.getElementById('rows').innerHTML,
            askBodyHtml: document.getElementById('ask-body').innerHTML,
            askControlsHtml: document.getElementById('ask-controls').innerHTML,
          }));
          assert.deepEqual(afterState, beforeState, 'DOM, question, input composer, and draft were preserved without mutation');

          // 6. Submit answer to engage in-flight lock (§5.8.3)
          const postedBeforeReplyCount = await page.evaluate(() => window.__posted.length);
          await page.locator('#send').click();

          // Wait for reply to be posted to host
          await page.waitForFunction((prevCount) => window.__posted.length > prevCount, postedBeforeReplyCount);
          const replyAttempt = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply').slice(-1)[0]);
          assert.ok(replyAttempt, 'Reply attempt must have been dispatched');

          // Verify in-flight lock engaged: askControls aria-busy is true, choice buttons disabled
          const askControls = page.locator('#ask-controls');
          const choiceBtns = page.locator('#ask-controls button.choice-btn');
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);

          // Enter direct answer mode to test draft & mode retention while in-flight
          await page.locator('#ask-controls button.direct-btn').click();
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          const sendBtn = page.locator('#send');
          assert.equal(await sendBtn.isDisabled(), true, 'Send button must be disabled in answer mode while in-flight');

          // Write new modification draft while in-flight
          await page.locator('#say').fill('수정 답변 초안 보존');
          const postedAfterReplyCount = await page.evaluate(() => window.__posted.length);

          // 7. Inject malformed replyResult with all context fields matched but attemptId: 0
          await page.evaluate((att) => {
            window.postMessage({
              kind: 'replyResult',
              callId: att.callId,
              attemptId: 0, // invalid attemptId (must be integer >= 1)
              ok: true,
              session: att.session,
              companionKey: att.companionKey,
              generation: att.generation,
              webviewId: att.webviewId,
            }, '*');
          }, replyAttempt);

          // FIFO sync to ensure malformed message has been processed by window listener
          await page.evaluate(() => new Promise((resolve) => {
            window.addEventListener('message', function onSync(e) {
              if (e.data && e.data.__syncGuardReply) {
                window.removeEventListener('message', onSync);
                resolve();
              }
            });
            window.postMessage({ __syncGuardReply: true }, '*');
          }));

          // Verify state, draft, mode, body, button lock, and posted count are strictly maintained
          assert.equal(await page.locator('#say').inputValue(), '수정 답변 초안 보존', 'Draft must be preserved after malformed replyResult');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'Answer mode must remain active');
          assert.equal(await askControls.getAttribute('aria-busy'), 'true', 'aria-busy must remain true');
          assert.equal(await sendBtn.isDisabled(), true, 'Send button must remain locked');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true, 'Choice buttons must remain locked');
          assert.equal(await page.evaluate(() => window.__posted.length), postedAfterReplyCount, 'No unexpected message dispatched');
          assert.match(await page.locator('#ask-body').textContent(), /보존되어야 할 질문 제목/, 'Question body preserved');

          // 8. Send valid replyResult to verify normal unlock and release
          await page.evaluate((att) => {
            window.postMessage({
              kind: 'replyResult',
              callId: att.callId,
              attemptId: att.attemptId,
              ok: true,
              session: att.session,
              companionKey: att.companionKey,
              generation: att.generation,
              webviewId: att.webviewId,
            }, '*');
          }, replyAttempt);

          // FIFO sync
          await page.evaluate(() => new Promise((resolve) => {
            window.addEventListener('message', function onSync(e) {
              if (e.data && e.data.__syncGuardNormal) {
                window.removeEventListener('message', onSync);
                resolve();
              }
            });
            window.postMessage({ __syncGuardNormal: true }, '*');
          }));

          // In-flight released
          assert.equal(await askControls.getAttribute('aria-busy'), null, 'aria-busy must be cleared after valid replyResult');
          assert.equal(await sendBtn.isDisabled(), false, 'Send button unlocked after valid replyResult');

          // Clean up: cancel answer mode and dismiss ask
          await page.keyboard.press('Escape');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);
        }
      },
      {
        id: 'asks_session_roundtrip_draft_and_autocomplete_invalidation',
        name: '세션 전환 왕복 시 일반·질문 초안 완벽 복원 및 자동완성 무효화 (§4.5 Item 5)',
        run: async (page) => {
          // 1. Session 1 초기화 및 일반 초안 작성
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-1',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 1 started' }],
            ask: null
          }));
          await page.locator('#say').fill('S1 일반 작업 메모');

          // S1에 질문 도착
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-1',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 1 started' }],
            ask: {
              kind: 'question',
              callId: 'q-s1-roundtrip',
              what: 'S1 전용 질문',
              options: ['선택지 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('S1 질문 답변 작성 중');

          // 자동완성 힌트 트리거 (입력 후 debounce 대기)
          await page.waitForFunction(() => window.__posted.some(m => m.kind === 'suggest' && m.target === 'q-s1-roundtrip'));
          const suggestMsg = await page.evaluate(() => window.__posted.filter(m => m.kind === 'suggest' && m.target === 'q-s1-roundtrip').slice(-1)[0]);
          await page.evaluate((req) => window.postMessage({
            kind: 'suggestion',
            text: '추천 문구',
            reqId: req.reqId,
            target: req.target
          }, '*'), suggestMsg);
          await page.waitForFunction(() => document.getElementById('hint').textContent.includes('추천 문구'));

          // 2. Session 2로 전환 (다른 세션)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-2',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 2 started' }],
            ask: null
          }));
          // 전환 시 자동완성 힌트 무효화 확인
          assert.equal(await page.locator('#hint').textContent(), '', 'hint invalidated on session change');
          await page.keyboard.press('Tab');
          assert.equal(await page.locator('#say').inputValue(), '', 'tab did not insert stale suggestion from previous session');
          // Session 2는 답변 모드가 아니며 초안이 비어 있어야 함
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode inactive in S2');
          assert.equal(await page.locator('#say').inputValue(), '', 'S2 composer starts empty');

          // Session 2에서 일반 초안 작성
          await page.locator('#say').fill('S2 일반 작업 메모');

          // 3. Session 1으로 다시 전환 (S1 복원)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-1',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 1 active' }],
            ask: {
              kind: 'question',
              callId: 'q-s1-roundtrip',
              what: 'S1 전용 질문',
              options: ['선택지 1']
            }
          }));
          // S1의 질문 초안과 답변 모드가 그대로 복원되어야 함
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await page.locator('#say').inputValue(), 'S1 질문 답변 작성 중', 'S1 question draft restored');

          // 답변 모드 취소 시 S1의 일반 초안 복원 확인
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode exited on Escape');
          assert.equal(await page.locator('#say').inputValue(), 'S1 일반 작업 메모', 'S1 general draft restored');

          // 4. Session 2로 다시 전환 (S2 복원)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-roundtrip-2',
            rows: [{ who: 'agent', label: 'magi', text: 'sess 2 active' }],
            ask: null
          }));

          await page.waitForFunction(() => document.getElementById('say').value === 'S2 일반 작업 메모');
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode inactive in S2');
          assert.equal(await page.locator('#say').inputValue(), 'S2 일반 작업 메모', 'S2 general draft restored');

          // 정리
          await page.locator('#say').fill('');
        }
      },
      {
        id: 'asks_recovery_list_ui_and_continuous_workflow',
        name: '실패 답변 등록 → 복구 목록 열기 → 전문 → 답변 모드에서 복사 취소/확정(이어 붙이기) → 삭제 및 0회 전송 검증 (§4.6)',
        run: async (page) => {
          // 1. 초기 상태 검증: #recovery-btn 텍스트가 '복구 초안 0', #recovery-panel hidden
          const recoveryBtn = page.locator('#recovery-btn');
          const recoveryPanel = page.locator('#recovery-panel');
          assert.equal(await recoveryBtn.textContent(), '복구 초안 0');
          assert.equal(await recoveryPanel.evaluate((el) => el.hidden), true);

          // 2. 세션 1에 질문 등록 및 답변 작성, 전송
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-rec-1',
            rows: [{ who: 'agent', label: 'magi', text: 'question ask' }],
            ask: {
              kind: 'question',
              callId: 'q-rec-1',
              what: '작업을 진행할까요?',
              options: ['예', '아니오']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();

          const failedAnswerText = '진행하겠습니다. <script>alert("xss")</script>\nLine 2';
          await page.locator('#say').fill(failedAnswerText);

          // #send 클릭으로 전송 -> in-flight 등록
          const postedLenBeforeSend = await page.evaluate(() => window.__posted.length);
          await page.locator('#send').click();
          await page.waitForFunction((len) => window.__posted.length > len, postedLenBeforeSend);
          const replyMsg = await page.evaluate(() => window.__posted.filter((m) => m.kind === 'reply' && m.callId === 'q-rec-1').slice(-1)[0]);
          assert.ok(replyMsg, 'reply message must be dispatched');

          // 3. 실패 결과(replyResult ok: false) 수신
          await page.evaluate((payload) => window.postMessage(payload, '*'), createReplyResultMessage({
            callId: 'q-rec-1',
            ok: false,
            error: 'daemon communication failure',
          }, replyMsg));

          // 복구 뱃지 갱신 확인
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');
          assert.equal(await recoveryBtn.textContent(), '복구 초안 1');

          // 4. 복구 버튼 클릭 -> 패널 열림 확인
          await recoveryBtn.click();
          assert.equal(await recoveryPanel.evaluate((el) => el.hidden), false);
          assert.equal(await recoveryBtn.getAttribute('aria-expanded'), 'true');

          // 안내 문구 및 항목 검증
          const notice = page.locator('.recovery-notice');
          assert.equal(await notice.textContent(), '이 창에서 임시 보관 중');
          const recoveryItem = page.locator('.recovery-item');
          assert.equal(await recoveryItem.count(), 1);
          assert.ok((await recoveryItem.locator('.recovery-reason').textContent()).includes('답변 전송을 확인하지 못함'));

          // 5. 전문 보기 클릭 -> XSS 안전성 및 원문 보존 검증
          const fulltextBtn = recoveryItem.locator('.fulltext-btn');
          assert.equal(await fulltextBtn.textContent(), '전문 보기');
          await fulltextBtn.click();
          await page.waitForSelector('.recovery-full-text');
          assert.equal(await fulltextBtn.textContent(), '전문 닫기');

          const preText = await recoveryItem.locator('.recovery-full-text').evaluate((el) => el.textContent);
          assert.equal(preText, failedAnswerText, 'full text must match raw text verbatim including script tags and newlines');

          // 전문 접기
          await fulltextBtn.click();
          assert.equal(await recoveryItem.locator('.recovery-full-text').count(), 0);

          // 6. 답변 모드 및 일반 초안 G가 있는 상태에서 복사 시도
          // 먼저 일반 모드로 나가서 일반 초안 G 입력
          await page.locator('#reply-cancel').click(); // exit answer mode
          await page.waitForFunction(() => document.getElementById('reply-mode').hidden);
          const existingG = '기존에 작성 중이던 일반 초안 메모';
          await page.locator('#say').fill(existingG);

          // 다시 답변 모드 진입하여 질문 답변 Q 입력
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          const currentAnswerDraft = '새로 입력 중인 답변 Q';
          await page.locator('#say').fill(currentAnswerDraft);

          // 복사 버튼 클릭 -> 일반 초안 G가 있으므로 인라인 확인 상자("이어 붙이기 / 취소") 노출
          const postMessagesBeforeCopy = await page.evaluate(() => window.__posted.length);
          const copyBtn = recoveryItem.locator('.copy-btn');
          await copyBtn.click();

          await page.waitForSelector('.recovery-confirm-box');
          const confirmBox = recoveryItem.locator('.recovery-confirm-box');
          assert.ok(await confirmBox.isVisible());

          // 6A. 취소 클릭
          const cancelBtn = confirmBox.locator('.confirm-cancel-btn');
          await cancelBtn.click();
          assert.equal(await recoveryItem.locator('.recovery-confirm-box').count(), 0, 'confirm box dismissed on cancel');
          assert.equal(await page.locator('#say').inputValue(), currentAnswerDraft, 'answer draft untouched on cancel');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'still in answer mode on cancel');

          // 6B. 다시 복사 클릭 -> 이어 붙이기 클릭
          await copyBtn.click();
          await page.waitForSelector('.recovery-confirm-box');
          const appendBtn = recoveryItem.locator('.confirm-append-btn');
          await appendBtn.click();

          // 검증: G + "\n\n" + failedAnswerText, 답변 모드 해제(일반 모드), 자동완성 무효화
          const expectedCombined = existingG + '\n\n' + failedAnswerText;
          await page.waitForFunction((exp) => document.getElementById('say').value === exp, expectedCombined);
          assert.equal(await page.locator('#reply-mode').evaluate((el) => el.hidden), true, 'switched to general mode');
          assert.equal(await page.locator('#say').inputValue(), expectedCombined);

          // 단언: 복구 조작 중 호스트로 say 또는 reply postMessage가 0회 발생했는지 확인 (§4.6.5)
          const postMessagesAfterAppend = await page.evaluate(() => window.__posted.length);
          assert.equal(postMessagesAfterAppend, postMessagesBeforeCopy, 'recovery copy must NOT post any say or reply messages');

          // 7. 명시적 삭제: .delete-btn 클릭 -> 항목 제거 및 뱃지 0 갱신
          const deleteBtn = recoveryItem.locator('.delete-btn');
          await deleteBtn.click();

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 0');
          assert.equal(await page.locator('.recovery-item').count(), 0);
          assert.ok(await page.locator('.recovery-empty').isVisible());
          assert.equal(await page.locator('#say').inputValue(), expectedCombined, 'composer input preserved after delete');

          // 8. 320×600 및 420×700 뷰포트에서 레이아웃 접근 검증 (§4.6.5)
          await page.setViewportSize({ width: 320, height: 600 });
          assert.ok(await page.locator('#topbar').isVisible());
          assert.ok(await page.locator('#say').isVisible());

          await page.setViewportSize({ width: 420, height: 700 });
          assert.ok(await page.locator('#topbar').isVisible());
          assert.ok(await page.locator('#say').isVisible());

          // 패널 닫기 및 정리
          await recoveryBtn.click();
          assert.equal(await recoveryPanel.evaluate((el) => el.hidden), true);
          await page.locator('#say').fill('');
        }
      },
      {
        id: 'asks_recovery_node_focus_and_context_switch',
        name: '복구 항목 DOM 노드·포커스 보존, 삭제 시 포커스 이동, 세션 전환 시 확인 즉시 취소 (§4.6.3)',
        run: async (page) => {
          const recoveryBtn = page.locator('#recovery-btn');
          const recoveryPanel = page.locator('#recovery-panel');

          // 1. 세션 session-focus 준비 및 2개 실패 등록
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'focus test ready' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-1',
              what: '질문 1 포커스 검증',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('포커스 첫번째 답변');
          await page.locator('#send').click();

          const postedF1 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-1').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-1',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 1',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF1);

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');

          // 두번째 실패 등록
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'turn 2' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-2',
              what: '질문 2 포커스 검증',
              options: ['선택 2']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('포커스 두번째 답변');
          await page.locator('#send').click();

          const postedF2 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-2').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-2',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 2',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF2);

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 2');

          // 2. 패널 열기: 발생순에 따라 [B, A] (B: 최신 항목 0번, A: 아래쪽 항목 1번)
          await recoveryBtn.click();
          assert.equal(await recoveryPanel.evaluate((el) => el.hidden), false);

          const items = page.locator('.recovery-item');
          assert.equal(await items.count(), 2);
          const itemBText = await items.nth(0).locator('.recovery-preview').textContent();
          const itemAText = await items.nth(1).locator('.recovery-preview').textContent();
          assert.ok(itemBText.includes('포커스 두번째 답변'), 'Item B is top item');
          assert.ok(itemAText.includes('포커스 첫번째 답변'), 'Item A is bottom item');

          // 아래쪽 항목 A의 DOM 노드에 마커를 붙여 노드 재사용(인스턴스 불변) 검증 준비
          await items.nth(1).evaluate((el) => {
            el.__marker_id = 'preserved_node_A';
          });

          // 전문(full text) 열기
          await items.nth(1).locator('.fulltext-btn').click();
          const preA = items.nth(1).locator('.recovery-full-text');
          assert.ok(await preA.isVisible());

          // 2-1. 전문 열린 상태에서 사유(recovery-reason) 선택 후 동일 rows 갱신 시 사유 선택 보존 (§4.6 Item 4)
          await page.evaluate(() => {
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const reasonEl = itemAEl?.querySelector('.recovery-reason');
            const tn = reasonEl?.firstChild || reasonEl;
            window.getSelection().setBaseAndExtent(tn, 0, tn, 5);
          });
          assert.equal(await page.evaluate(() => window.getSelection()?.toString()), '답변 전송');

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update for reason selection' }],
          }));
          await page.waitForTimeout(50);

          const reasonCheck = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const reasonEl = itemAEl?.querySelector('.recovery-reason');
            const tn = reasonEl?.firstChild || reasonEl;
            return {
              text: sel.toString(),
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
            };
          });
          assert.equal(reasonCheck.text, '답변 전송', 'selection must remain on reason text, not converted to full text');
          assert.equal(reasonCheck.anchorMatches, true, 'anchorNode must still be reasonEl text node');
          assert.equal(reasonCheck.focusMatches, true, 'focusNode must still be reasonEl text node');
          assert.equal(reasonCheck.anchorOffset, 0);
          assert.equal(reasonCheck.focusOffset, 5);

          // 2-2. 제목(recovery-title) 선택 후 rows 갱신 시 선택 보존 (§4.6 Item 4)
          await page.evaluate(() => {
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const titleEl = itemAEl?.querySelector('.recovery-title');
            const tn = titleEl?.firstChild || titleEl;
            window.getSelection().setBaseAndExtent(tn, 0, tn, 4);
          });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update for title selection' }],
          }));
          await page.waitForTimeout(50);
          const titleCheck = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const titleEl = itemAEl?.querySelector('.recovery-title');
            const tn = titleEl?.firstChild || titleEl;
            return {
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
            };
          });
          assert.equal(titleCheck.anchorMatches, true, 'anchorNode must still be titleEl text node');
          assert.equal(titleCheck.focusMatches, true, 'focusNode must still be titleEl text node');
          assert.equal(titleCheck.anchorOffset, 0);
          assert.equal(titleCheck.focusOffset, 4);

          // 2-3. 미리보기(recovery-preview) 선택 후 rows 갱신 시 선택 보존 (§4.6 Item 4)
          await page.evaluate(() => {
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const prevEl = itemAEl?.querySelector('.recovery-preview');
            const tn = prevEl?.firstChild || prevEl;
            window.getSelection().setBaseAndExtent(tn, 0, tn, 4);
          });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update for preview selection' }],
          }));
          await page.waitForTimeout(50);
          const prevCheck = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const prevEl = itemAEl?.querySelector('.recovery-preview');
            const tn = prevEl?.firstChild || prevEl;
            return {
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
            };
          });
          assert.equal(prevCheck.anchorMatches, true, 'anchorNode must still be previewEl text node');
          assert.equal(prevCheck.focusMatches, true, 'focusNode must still be previewEl text node');
          assert.equal(prevCheck.anchorOffset, 0);
          assert.equal(prevCheck.focusOffset, 4);

          // 2-4. 두 항목에 걸친 선택 시 안전 갱신 (§4.6 Item 4)
          await page.evaluate(() => {
            const items = document.querySelectorAll('.recovery-item');
            const tnB = items[0].querySelector('.recovery-title')?.firstChild || items[0];
            const tnA = items[1].querySelector('.recovery-title')?.firstChild || items[1];
            window.getSelection().setBaseAndExtent(tnB, 0, tnA, 2);
          });
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update cross item' }],
          }));
          await page.waitForTimeout(50);

          // 2-5. 목록 밖 선택은 갱신 과정에서 지우거나 교체하지 않음 (§4.6 Item 4)
          await page.evaluate(() => {
            const row = document.querySelector('.row-agent');
            const tn = row?.firstChild || row;
            window.getSelection().setBaseAndExtent(tn, 0, tn, 5);
          });
          const outsideTextBefore = await page.evaluate(() => window.getSelection()?.toString());
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'rows update outside selection' }],
          }));
          await page.waitForTimeout(50);
          const outsideTextAfter = await page.evaluate(() => window.getSelection()?.toString());
          assert.equal(outsideTextAfter, outsideTextBefore, 'outside selection must remain untouched across refresh');

          // 3. 전문 내부 역방향 선택(backwards selection) 및 moveBefore 재정렬 검증 (§4.6 Item 4, 5)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1 retry' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-1',
              what: '질문 1 포커스 재시도',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('포커스 첫번째 답변');
          await page.locator('#send').click();

          const postedF1Retry = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-1').slice(-1)[0]);
          assert.ok(postedF1Retry, 'in-flight retry for item A posted');

          // in-flight 상태에서 아래쪽 항목 A(인덱스 1)의 복사 버튼에 포커스 설정 및 pre 내부 역방향 선택 설정 (anchor: 3, focus: 0 -> '포커스')
          await items.nth(1).locator('.copy-btn').focus();
          await page.evaluate(() => {
            const itemAEl = document.querySelectorAll('.recovery-item')[1];
            const preEl = itemAEl?.querySelector('.recovery-full-text');
            if (preEl) {
              const textNode = preEl.firstChild || preEl;
              window.getSelection().setBaseAndExtent(textNode, 3, textNode, 0);
            }
          });

          const backwardBeforeReorder = await page.evaluate(() => {
            const sel = window.getSelection();
            const active = document.activeElement;
            return {
              className: active?.className || '',
              parentItemText: active?.closest('.recovery-item')?.querySelector('.recovery-preview')?.textContent || '',
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
              text: sel.toString(),
            };
          });
          assert.ok(backwardBeforeReorder.className.includes('copy-btn'));
          assert.ok(backwardBeforeReorder.parentItemText.includes('포커스 첫번째 답변'));
          assert.equal(backwardBeforeReorder.anchorOffset, 3);
          assert.equal(backwardBeforeReorder.focusOffset, 0);
          assert.equal(backwardBeforeReorder.text, '포커스');

          // 4. A 재실패 응답 도착 -> [B, A]에서 [A, B]로 순서 재정렬 (moveBefore 경로)
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-1',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 1 again',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF1Retry);
          await page.waitForTimeout(50);

          // DOM 순서가 [A, B]로 바뀌었음을 확인
          const itemsAfterReorder = page.locator('.recovery-item');
          assert.equal(await itemsAfterReorder.count(), 2);
          const firstText = await itemsAfterReorder.nth(0).locator('.recovery-preview').textContent();
          const secondText = await itemsAfterReorder.nth(1).locator('.recovery-preview').textContent();
          assert.ok(firstText.includes('포커스 첫번째 답변'), 'Item A must now be at index 0');
          assert.ok(secondText.includes('포커스 두번째 답변'), 'Item B must now be at index 1');

          // A의 DOM 노드가 새로 생성되지 않고 기존 인스턴스 그대로 유지됨을 확인
          const markerAfter = await itemsAfterReorder.nth(0).evaluate((el) => el.__marker_id);
          assert.equal(markerAfter, 'preserved_node_A', 'Item A DOM node instance must be preserved across reorder');

          // document.activeElement가 여전히 A의 '복사' 버튼을 가리키고 있음을 확인 (body나 상위 컨테이너로 튀지 않음)
          const focusedAfterReorder = await page.evaluate(() => {
            const active = document.activeElement;
            return {
              tagName: active?.tagName,
              className: active?.className || '',
              parentItemText: active?.closest('.recovery-item')?.querySelector('.recovery-preview')?.textContent || ''
            };
          });
          assert.equal(focusedAfterReorder.tagName, 'BUTTON');
          assert.ok(focusedAfterReorder.className.includes('copy-btn'), 'activeElement must remain on copy-btn');
          assert.ok(focusedAfterReorder.parentItemText.includes('포커스 첫번째 답변'), 'activeElement must remain on item A');

          // 전문(full text) 역방향 텍스트 선택이 재정렬 후에도 방향 및 오프셋 유지 검사 (§4.6 Item 4)
          const backwardAfterReorder = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemAEl = document.querySelectorAll('.recovery-item')[0];
            const preEl = itemAEl?.querySelector('.recovery-full-text');
            const tn = preEl?.firstChild || preEl;
            return {
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
              text: sel.toString(),
            };
          });
          assert.equal(backwardAfterReorder.anchorMatches, true, 'anchorNode preserved in backward selection');
          assert.equal(backwardAfterReorder.focusMatches, true, 'focusNode preserved in backward selection');
          assert.equal(backwardAfterReorder.anchorOffset, 3, 'anchorOffset must remain 3 (backward start)');
          assert.equal(backwardAfterReorder.focusOffset, 0, 'focusOffset must remain 0 (backward end)');
          assert.equal(backwardAfterReorder.text, '포커스');

          // Enter 키 입력 시 복사/확인 정상 1회 트리거 및 0회 say/reply 전송 확인
          const postedLenBeforeEnter = await page.evaluate(() => window.__posted.length);
          await page.keyboard.press('Enter');
          await page.waitForTimeout(50);

          const confirmBoxCount = await page.locator('.recovery-confirm-box').count();
          const sayVal = await page.locator('#say').inputValue();
          assert.ok(confirmBoxCount > 0 || sayVal.includes('포커스 첫번째 답변'), 'copy action must be triggered via Enter on focused button');

          const postedAfterEnter = await page.evaluate(() => window.__posted);
          const newTransmissions = postedAfterEnter.slice(postedLenBeforeEnter).filter((m) => m.kind === 'say' || m.kind === 'reply');
          assert.equal(newTransmissions.length, 0, '0 backend transmissions during recovery copy');

          if (confirmBoxCount > 0) {
            await page.locator('.confirm-cancel-btn').click();
          }

          // 5. moveBefore 비활성화 환경 (insertBefore 폴백 경로) 검증 (§4.6 Item 5)
          await page.evaluate(() => {
            const listEl = document.getElementById('recovery-items');
            if (listEl) {
              listEl.__saved_moveBefore = listEl.moveBefore;
              listEl.moveBefore = undefined; // Force insertBefore fallback path
            }
          });

          // 이제 항목 B의 재실패로 [A, B]에서 다시 [B, A]로 재정렬 발생
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'turn 2 retry' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-2',
              what: '질문 2 포커스 재시도',
              options: ['선택 2']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('포커스 두번째 답변');
          await page.locator('#send').click();

          const postedF2Retry = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-2').slice(-1)[0]);
          assert.ok(postedF2Retry);

          // 현재 [A, B]에서 아래쪽 항목 B(인덱스 1)의 복사 버튼 포커스 및 전문 열기/선택
          await itemsAfterReorder.nth(1).locator('.fulltext-btn').click();
          await itemsAfterReorder.nth(1).locator('.copy-btn').focus();
          await page.evaluate(() => {
            const itemBEl = document.querySelectorAll('.recovery-item')[1];
            const preEl = itemBEl?.querySelector('.recovery-full-text');
            if (preEl) {
              const tn = preEl.firstChild || preEl;
              window.getSelection().setBaseAndExtent(tn, 0, tn, 3); // '포커스'
            }
          });

          // B 재실패 도착 -> insertBefore 폴백을 통해 [A, B]에서 [B, A]로 재정렬
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-2',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 2 again fallback',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF2Retry);
          await page.waitForTimeout(50);

          // insertBefore 폴백 경로에서도 DOM 순서가 [B, A]로 변경되었음을 확인
          const itemsFallback = page.locator('.recovery-item');
          assert.equal(await itemsFallback.count(), 2);
          const fbFirstText = await itemsFallback.nth(0).locator('.recovery-preview').textContent();
          const fbSecondText = await itemsFallback.nth(1).locator('.recovery-preview').textContent();
          assert.ok(fbFirstText.includes('포커스 두번째 답변'), 'Item B is now at index 0 via insertBefore fallback');
          assert.ok(fbSecondText.includes('포커스 첫번째 답변'), 'Item A is now at index 1 via insertBefore fallback');

          // insertBefore 폴백에서도 document.activeElement가 B의 복사 버튼으로 동기 복원됨을 확인
          const activeFallback = await page.evaluate(() => {
            const active = document.activeElement;
            return {
              tagName: active?.tagName,
              className: active?.className || '',
              parentItemText: active?.closest('.recovery-item')?.querySelector('.recovery-preview')?.textContent || ''
            };
          });
          assert.equal(activeFallback.tagName, 'BUTTON');
          assert.ok(activeFallback.className.includes('copy-btn'), 'activeElement must remain on copy-btn via fallback');
          assert.ok(activeFallback.parentItemText.includes('포커스 두번째 답변'), 'activeElement must remain on item B via fallback');

          // insertBefore 폴백에서도 전문 텍스트 선택이 동기 복원됨을 확인
          const selFallback = await page.evaluate(() => {
            const sel = window.getSelection();
            const itemBEl = document.querySelectorAll('.recovery-item')[0];
            const preEl = itemBEl?.querySelector('.recovery-full-text');
            const tn = preEl?.firstChild || preEl;
            return {
              anchorMatches: sel.anchorNode === tn,
              focusMatches: sel.focusNode === tn,
              anchorOffset: sel.anchorOffset,
              focusOffset: sel.focusOffset,
              text: sel.toString(),
            };
          });
          assert.equal(selFallback.anchorMatches, true, 'anchorNode restored via fallback');
          assert.equal(selFallback.focusMatches, true, 'focusNode restored via fallback');
          assert.equal(selFallback.anchorOffset, 0);
          assert.equal(selFallback.focusOffset, 3);
          assert.equal(selFallback.text, '포커스');

          // Space 키 입력 시 복사/확인 정상 1회 트리거 및 0회 전송 확인
          const postedLenBeforeSpace = await page.evaluate(() => window.__posted.length);
          await page.keyboard.press('Space');
          await page.waitForTimeout(50);

          const confirmBoxFbCount = await page.locator('.recovery-confirm-box').count();
          const sayValFb = await page.locator('#say').inputValue();
          assert.ok(confirmBoxFbCount > 0 || sayValFb.includes('포커스 두번째 답변'), 'copy action must be triggered via Space on focused button');

          const postedAfterSpace = await page.evaluate(() => window.__posted);
          const spaceTransmissions = postedAfterSpace.slice(postedLenBeforeSpace).filter((m) => m.kind === 'say' || m.kind === 'reply');
          assert.equal(spaceTransmissions.length, 0, '0 backend transmissions during recovery copy via Space');

          if (confirmBoxFbCount > 0) {
            await page.locator('.confirm-cancel-btn').click();
          }

          // moveBefore 복구
          await page.evaluate(() => {
            const listEl = document.getElementById('recovery-items');
            if (listEl && listEl.__saved_moveBefore !== undefined) {
              listEl.moveBefore = listEl.__saved_moveBefore;
              delete listEl.__saved_moveBefore;
            } else if (listEl) {
              delete listEl.moveBefore;
            }
          });

          // 6. composer(#say) 포커스 보호 및 선택 항목 삭제 시 정상 삭제 동작 검증 (§4.6 Item 3, 4)
          await page.locator('#say').focus();
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say');

          // rows 수신 시 composer 포커스 유지
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'streamed row after fallback' }],
          }));
          await page.waitForTimeout(50);
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say', 'focus must stay on #say after rows update');

          // 인접 항목 A(인덱스 1)의 제목 텍스트 선택 후 삭제 -> 선택 해제/무효화 및 composer 포커스 유지
          await page.evaluate(() => {
            const items = document.querySelectorAll('.recovery-item');
            if (items[1]) {
              const tn = items[1].querySelector('.recovery-title')?.firstChild || items[1];
              window.getSelection().setBaseAndExtent(tn, 0, tn, 4);
            }
          });
          assert.ok((await page.evaluate(() => window.getSelection()?.toString().length)) > 0);

          await page.evaluate(() => {
            const items = document.querySelectorAll('.recovery-item');
            if (items[1]) {
              const del = items[1].querySelector('.delete-btn');
              if (del) del.click();
            }
          });
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');
          assert.equal(await page.evaluate(() => document.activeElement?.id), 'say', 'focus must stay on #say after adjacent item deletion');

          // 선택 항목이 삭제되었으므로 옛 오프셋이 남아 있는 항목 B에 clamp되어 재지정되지 않음 확인 (§4.6 Item 3, 4)
          const selAfterItemDeleted = await page.evaluate(() => {
            const sel = window.getSelection();
            return {
              isCollapsed: sel.isCollapsed,
              rangeCount: sel.rangeCount,
              text: sel.toString(),
            };
          });
          assert.ok(selAfterItemDeleted.isCollapsed || selAfterItemDeleted.rangeCount === 0 || selAfterItemDeleted.text === '', 'selection must not be clamped onto surviving item');

          // 7. 남은 항목 B의 삭제 버튼에 포커스 후 삭제 시 recovery-btn 복귀
          await page.locator('.recovery-item .delete-btn').focus();
          await page.locator('.recovery-item .delete-btn').click();
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 0');
          const focusedAfterAllDel = await page.evaluate(() => document.activeElement?.id);
          assert.equal(focusedAfterAllDel, 'recovery-btn', 'focus must return to #recovery-btn when last item deleted');

          // 6. Context change cancellation:
          // Register 1 failure in session-focus
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'turn 4' }],
            ask: {
              kind: 'question',
              callId: 'q-focus-3',
              what: '취소 검증 질문',
              options: ['선택']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('취소 검증 실패 답변');
          await page.locator('#send').click();

          const postedF3 = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-focus-3').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-focus-3',
            attemptId: att.attemptId,
            ok: false,
            error: 'fail 3',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'session-focus',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), postedF3);

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');

          // Prepare general draft in session-focus
          await page.keyboard.press('Escape');
          await page.locator('#say').fill('세션 포커스의 일반 초안');

          // Click copy -> confirm box appears
          await page.locator('.recovery-item .copy-btn').click();
          await page.waitForSelector('.recovery-confirm-box');
          assert.ok(await page.locator('.recovery-confirm-box').isVisible());

          // Switch context to session-other via rows
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-other',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'other session' }],
          }));
          await page.waitForTimeout(50);
          assert.equal(await page.locator('.recovery-confirm-box').count(), 0, 'confirm box dismissed on switching to session-other');

          // Switch back to session-focus via rows
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'session-focus',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: 'back to session-focus' }],
          }));
          await page.waitForTimeout(50);
          assert.equal(await page.locator('.recovery-confirm-box').count(), 0, 'confirm box not re-opened on returning to session-focus');
          assert.equal(await page.locator('#say').inputValue(), '세션 포커스의 일반 초안', 'general draft untouched, no append occurred');

          // Clean up
          await page.locator('.recovery-item .delete-btn').click();
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 0');
          await recoveryBtn.click();
          await page.locator('#say').fill('');
        }
      },
      {
        id: 'asks_synthetic_ime_composition_and_recovery_lock',
        name: '브라우저 합성 IME 이벤트 조합 중 Enter 억제, 완료 후 전송, 복구 초안 버퍼 보호 검증 (§5.6)',
        run: async (page) => {
          // Scope: Synthetic DOM events (CompositionEvent, KeyboardEvent keyCode 229, isComposing: true).
          // OS native IME remains unverified.
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-ime-synth',
            rows: [{ who: 'agent', label: 'magi', text: '합성 IME 테스트' }]
          }));

          const say = page.locator('#say');
          await say.focus();

          // 1. Synthetic composition 'ㅎ' -> '하' -> '한' with intermediate Enter (keyCode 229, isComposing: true)
          await page.evaluate(() => {
            const el = document.getElementById('say');
            el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true, data: '' }));
            el.value = 'ㅎ';
            el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: 'ㅎ' }));
            el.value = '하';
            el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: '하' }));
            el.value = '한';
            el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: '한' }));

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
          assert.equal(posted.filter((m) => m.kind === 'say').length, 0, 'Enter during synthetic composition must NOT send message');

          // Complete syllable '한' and compose '글'
          await page.evaluate(() => {
            const el = document.getElementById('say');
            el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '한' }));
            el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true, data: '' }));
            el.value = '한글';
            el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: '글' }));
            el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '글' }));
            el.dispatchEvent(new Event('input', { bubbles: true }));

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
          const sayMessages = posted.filter((m) => m.kind === 'say');
          assert.equal(sayMessages.length, 1, 'Exactly one message sent on final Enter');
          assert.equal(sayMessages[0].text, '한글', 'Sent text must match exact Korean text without duplication');
          assert.equal(await say.inputValue(), '', 'Textarea must be cleared after send');

          // 2. Recovery draft copy button protection during active composition
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-ime-synth',
            ask: {
              kind: 'question',
              callId: 'q-ime-synth-fail',
              what: '복구 초안 보호 확인',
              options: ['확인']
            }
          }));

          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await say.fill('실패할 답변 본문');

          const postedBefore = await page.evaluate(() => window.__posted.length);
          await page.locator('#send').click();
          await page.waitForFunction((len) => window.__posted.length > len, postedBefore);

          const replyMsg = await page.evaluate(() => window.__posted.filter((m) => m.kind === 'reply' && m.callId === 'q-ime-synth-fail').slice(-1)[0]);
          assert.ok(replyMsg);

          await page.evaluate((payload) => window.postMessage(payload, '*'), createReplyResultMessage({
            callId: 'q-ime-synth-fail',
            ok: false,
            error: 'simulated error',
          }, replyMsg));

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');
          await page.locator('#recovery-btn').click();
          await page.waitForSelector('#recovery-panel:not([hidden])');

          const copyBtn = page.locator('#recovery-items .copy-btn');
          await copyBtn.waitFor();
          assert.equal(await copyBtn.isDisabled(), false, 'Copy button initially enabled');

          // Active composition in composer disables copy button
          await page.evaluate(() => {
            const el = document.getElementById('say');
            el.value = '작성 ';
            el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true, data: '' }));
            el.value = '작성 중';
            el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: '중' }));
          });
          await page.waitForTimeout(50);
          assert.equal(await copyBtn.isDisabled(), true, 'Copy button disabled during active composition to protect buffer');

          // End composition re-enables copy button
          await page.evaluate(() => {
            const el = document.getElementById('say');
            el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '중' }));
            el.dispatchEvent(new Event('input', { bubbles: true }));
          });
          await page.waitForTimeout(50);
          assert.equal(await copyBtn.isDisabled(), false, 'Copy button re-enabled after compositionend');

          // Cleanup
          await page.locator('#recovery-items .delete-btn').click();
          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 0');
          await page.locator('#recovery-btn').click();
          await say.fill('');
        }
      },
      {
        id: 'asks_choice_numbering_and_numeric_body_preservation',
        name: '선택지 번호 판정, CSS 목록 마커 제어, 숫자 본문 보존 및 버튼 라벨/전송값 검증 (§5.6)',
        run: async (page) => {
          // 1. 이미 번호가 있는 목록 (Format A: `1. `, `2. `)
          // 본문 li는 원문 그대로 유지, ol.choices는 hide-marker 클래스 부여 및 computed listStyleType === 'none'
          // 버튼은 중복 번호 없이 순번 유지, 첫 줄 및 20자 축약, title은 원문 전체
          const numberedOptions = [
            '1. 첫 번째 항목\n상세한 설명 줄',
            '2. 두 번째 일반 항목',
            '3. 세 번째 아주아주 긴 옵션 텍스트로 버튼 라벨에서 말줄임표로 축약되는 항목입니다'
          ];
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-choices',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: '선택지 번호 검증' }],
            ask: {
              kind: 'question',
              callId: 'q-choices-numbered',
              what: '진행 방식을 선택해주세요',
              options: numberedOptions
            }
          }));

          await page.waitForSelector('#ask-controls button:text("1. 첫 번째 항목")');
          const olNumbered = page.locator('#ask-body ol.choices');
          assert.equal(await olNumbered.evaluate((el) => el.classList.contains('hide-marker')), true, 'already-numbered ol must have hide-marker class');
          const numberedListStyle = await olNumbered.evaluate((el) => window.getComputedStyle(el).listStyleType);
          assert.equal(numberedListStyle, 'none', 'already-numbered ol must have listStyleType none');

          // 본문 li textContent는 원문 그대로 보존
          const liTextsNumbered = await page.locator('#ask-body ol.choices li').allTextContents();
          assert.deepEqual(liTextsNumbered, numberedOptions, 'li textContent must match raw options verbatim');

          // 버튼 라벨: 중복 번호 없이 1. 첫 번째 항목, 2. 두 번째 일반 항목, 3. 세 번째 아주아주 긴…
          const btnTextsNumbered = await page.locator('#ask-controls .acts button').allTextContents();
          assert.equal(btnTextsNumbered[0], '1. 첫 번째 항목');
          assert.equal(btnTextsNumbered[1], '2. 두 번째 일반 항목');
          assert.equal(btnTextsNumbered[2], '3. 세 번째 아주아주 긴 옵션 텍스트로…');
          assert.equal(btnTextsNumbered[3], '직접 입력');

          // 버튼 title은 원문 전체
          const btn1Title = await page.locator('#ask-controls .acts button').nth(0).getAttribute('title');
          const btn3Title = await page.locator('#ask-controls .acts button').nth(2).getAttribute('title');
          assert.equal(btn1Title, numberedOptions[0]);
          assert.equal(btn3Title, numberedOptions[2]);

          // 2. 다른 정상 번호 형식 검증: paren `1) ` 및 bracket `(1) `
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-choices',
            companionKey: '/workspace',
            ask: {
              kind: 'question',
              callId: 'q-choices-paren',
              what: '괄호 번호 선택지',
              options: ['1) 알파 옵션', '2) 베타 옵션']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 알파 옵션")');
          assert.equal(await page.locator('#ask-body ol.choices').evaluate((el) => el.classList.contains('hide-marker')), true);
          assert.equal(await page.locator('#ask-body ol.choices').evaluate((el) => window.getComputedStyle(el).listStyleType), 'none');
          assert.deepEqual(await page.locator('#ask-controls .acts button').allTextContents(), ['1. 알파 옵션', '2. 베타 옵션', '직접 입력']);

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-choices',
            companionKey: '/workspace',
            ask: {
              kind: 'question',
              callId: 'q-choices-bracket',
              what: '대괄호 번호 선택지',
              options: ['(1) 첫 번째', '(2) 두 번째']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 첫 번째")');
          assert.equal(await page.locator('#ask-body ol.choices').evaluate((el) => el.classList.contains('hide-marker')), true);
          assert.equal(await page.locator('#ask-body ol.choices').evaluate((el) => window.getComputedStyle(el).listStyleType), 'none');
          assert.deepEqual(await page.locator('#ask-controls .acts button').allTextContents(), ['1. 첫 번째', '2. 두 번째', '직접 입력']);

          // 3. 숫자 본문 보존: `1.5배`, `2026. 계획`, `123.txt`
          // 번호 있는 목록으로 분류되지 않아야 하며 접두사를 제거하지 않음
          // ol.choices는 hide-marker가 없고 listStyleType !== 'none'
          const numericOptions = ['1.5배 성능 향상', '2026. 계획 수립', '123.txt 파일 처리'];
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-choices',
            companionKey: '/workspace',
            ask: {
              kind: 'question',
              callId: 'q-choices-numeric',
              what: '숫자 본문 선택지',
              options: numericOptions
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 1.5배 성능 향상")');
          const olNumeric = page.locator('#ask-body ol.choices');
          assert.equal(await olNumeric.evaluate((el) => el.classList.contains('hide-marker')), false, 'numeric bodies must NOT have hide-marker class');
          const numericStyle = await olNumeric.evaluate((el) => window.getComputedStyle(el).listStyleType);
          assert.notEqual(numericStyle, 'none', 'numeric bodies ol must preserve default decimal marker');

          // li textContent 및 버튼 라벨 보존
          const liTextsNumeric = await page.locator('#ask-body ol.choices li').allTextContents();
          assert.deepEqual(liTextsNumeric, numericOptions);

          const btnTextsNumeric = await page.locator('#ask-controls .acts button').allTextContents();
          assert.deepEqual(btnTextsNumeric, [
            '1. 1.5배 성능 향상',
            '2. 2026. 계획 수립',
            '3. 123.txt 파일 처리',
            '직접 입력'
          ]);
          assert.equal(await page.locator('#ask-controls .acts button').nth(0).getAttribute('title'), '1.5배 성능 향상');

          // 4. 대표 버튼 클릭 시 원문 전체 reply 전송 및 일반 초안 보존
          await page.locator('#say').fill('보존되어야 하는 일반 작업 초안');
          const postedLenBeforeClick = await page.evaluate(() => window.__posted.length);
          await page.locator('#ask-controls .acts button').nth(0).click(); // Click '1. 1.5배 성능 향상'

          const postedAfterClick = await page.evaluate(() => window.__posted);
          const replyMsgs = postedAfterClick.slice(postedLenBeforeClick).filter((m) => m.kind === 'reply');
          assert.equal(replyMsgs.length, 1, 'Exactly one reply sent on choice button click');
          assert.equal(replyMsgs[0].callId, 'q-choices-numeric');
          assert.equal(replyMsgs[0].text, '1.5배 성능 향상', 'Sent reply text must match verbatim option text');

          // 일반 초안 보존 단언
          assert.equal(await page.locator('#say').inputValue(), '보존되어야 하는 일반 작업 초안', 'General draft must be preserved after choice submission');

          // 5. 320x600 및 420x700 뷰포트에서 긴 선택지, 직접 입력 버튼 접근, Tab 이동 및 본문 전문 접근
          const longChoiceOptions = [
            '1. 아주아주 긴 첫 번째 배포 전략 선택지로 상세 설명이 긴 본문입니다\n부연설명',
            '2. 두 번째 아주아주 긴 롤백 전략 선택지',
            '3. 세 번째 카나리 테스트 배포 옵션'
          ];
          for (const vp of [{ width: 320, height: 600 }, { width: 420, height: 700 }]) {
            await page.setViewportSize(vp);
            await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
              session: 'sess-choices',
              companionKey: '/workspace',
              ask: {
                kind: 'question',
                callId: `q-long-${vp.width}`,
                what: `긴 선택지 뷰포트 ${vp.width}x${vp.height} 접근성 검증`,
                options: longChoiceOptions
              }
            }));
            await page.waitForFunction((expected) => {
              const sum = document.querySelector('#ask-controls .summary-text');
              return sum && sum.textContent && sum.textContent.includes(expected);
            }, `긴 선택지 뷰포트 ${vp.width}x${vp.height}`);

            // 본문 전문 DOM 접근 확인
            const liItems = await page.locator('#ask-body ol.choices li').allTextContents();
            assert.deepEqual(liItems, longChoiceOptions, `li items intact at ${vp.width}x${vp.height}`);

            // 직접 입력 버튼 노출 및 접근성 확인
            const directInputBtn = page.locator('#ask-controls button:text("직접 입력")');
            assert.equal(await directInputBtn.isVisible(), true, `direct input button visible at ${vp.width}x${vp.height}`);

            // Tab 이동 검증: jumpBtn -> 버튼 1 -> 버튼 2 -> 버튼 3 -> 직접 입력
            const jumpBtn = page.locator('#ask-controls .jump-btn');
            await jumpBtn.focus();
            assert.equal(await page.evaluate(() => document.activeElement.classList.contains('jump-btn')), true);

            const choiceBtns = page.locator('#ask-controls .acts button');
            const totalActsBtns = await choiceBtns.count(); // 3 choice btns + 1 직접 입력 = 4
            assert.equal(totalActsBtns, 4);

            for (let bIdx = 0; bIdx < totalActsBtns; bIdx++) {
              await page.keyboard.press('Tab');
              const isActive = await choiceBtns.nth(bIdx).evaluate((el) => document.activeElement === el);
              assert.ok(isActive, `Tab at index ${bIdx} (${await choiceBtns.nth(bIdx).textContent()}) must be active at ${vp.width}x${vp.height}`);
            }
          }

          // Reset viewport and cleanup
          await page.setViewportSize({ width: 420, height: 700 });
          await page.locator('#say').fill('');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-choices',
            companionKey: '/workspace',
            rows: [],
            ask: null
          }));
        }
      },
      {
        id: 'asks_in_flight_progress_indicator_and_disabled_controls',
        name: '질문 답변 전송 중 표시, disabled 범위 격리, rows 재수신 보존 및 세션 문맥 전환 (§5.6)',
        run: async (page) => {
          // 1. 일반 초안 G가 있는 선택형 질문 수신
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-inflight-1',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: '전송 중 표시 검증 질문' }],
            ask: {
              kind: 'question',
              callId: 'q-inflight-controls',
              what: '배포 환경을 선택해주세요',
              options: ['1. 프로덕션 환경', '2. 스테이징 환경']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 프로덕션 환경")');

          // 초기 상태: aria-busy 없음, .ask-status 비어있음, 선택지 버튼 활성화
          const askControls = page.locator('#ask-controls');
          assert.equal(await askControls.getAttribute('aria-busy'), null);
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '');
          const choiceBtns = page.locator('#ask-controls button.choice-btn');
          assert.equal(await choiceBtns.count(), 2);
          assert.equal(await choiceBtns.nth(0).isDisabled(), false);
          assert.equal(await choiceBtns.nth(1).isDisabled(), false);

          // 일반 작업 초안 G 입력
          const say = page.locator('#say');
          await say.fill('일반 작업 초안 G');
          const sendBtn = page.locator('#send');
          assert.equal(await sendBtn.isDisabled(), false);

          // 2. 선택지 버튼(1. 프로덕션 환경) 클릭 -> reply 1회 전송
          const postedLen1 = await page.evaluate(() => window.__posted.length);
          await choiceBtns.nth(0).click();

          const postedAfter1 = await page.evaluate(() => window.__posted);
          const replyMsgs1 = postedAfter1.slice(postedLen1).filter(m => m.kind === 'reply' && m.callId === 'q-inflight-controls');
          assert.equal(replyMsgs1.length, 1, 'Exactly one reply sent on choice button click');
          const attempt1 = replyMsgs1[0];
          assert.equal(attempt1.text, '1. 프로덕션 환경');
          assert.ok(attempt1.attemptId > 0);

          // 전송 중 상태 단언: aria-busy === 'true', .ask-status === '답변 전송 중…'
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');

          // 선택지 버튼만 disabled === true, 라벨 및 title 유지
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);
          assert.equal(await choiceBtns.nth(1).isDisabled(), true);
          assert.equal(await choiceBtns.nth(0).textContent(), '1. 프로덕션 환경');
          assert.equal(await choiceBtns.nth(0).getAttribute('title'), '1. 프로덕션 환경');

          // 직접 입력 버튼, 본문 이동 버튼은 disabled === false (유지)
          const directBtn = page.locator('#ask-controls button.direct-btn');
          const jumpBtn = page.locator('#ask-controls button.jump-btn');
          assert.equal(await directBtn.isDisabled(), false);
          assert.equal(await jumpBtn.isDisabled(), false);

          // 일반 모드이므로 일반 초안 G 보존 및 composer sendBtn 활성화 (일반 작업 전송 가능)
          assert.equal(await say.inputValue(), '일반 작업 초안 G');
          assert.equal(await sendBtn.isDisabled(), false);

          // 3. 직접 입력 진입 -> 답변 모드에서는 sendBtn disabled === true (재제출 차단), 새 초안 B 작성 가능
          await directBtn.click();
          assert.equal(await page.locator('#reply-mode').isVisible(), true);
          assert.equal(await sendBtn.isDisabled(), true, 'composer send button disabled in answer mode while in-flight');

          await say.fill('수정 초안 B');
          // Enter 시도 시 in-flight 가드 작동
          await say.press('Enter');
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…');

          // Esc 취소 -> 일반 모드 복귀: 초안 G 복원, sendBtn 활성화, 선택지 버튼은 계속 disabled
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#reply-mode').isHidden(), true);
          assert.equal(await say.inputValue(), '일반 작업 초안 G');
          assert.equal(await sendBtn.isDisabled(), false);
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');

          // 4. Stale 및 ID 없는 replyResult 주입 -> in-flight 해제되지 않음
          await page.evaluate(() => window.postMessage({
            kind: 'replyResult',
            callId: 'q-inflight-controls',
            ok: false,
            error: 'no id response',
            text: '무효'
          }, '*'));
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);

          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-inflight-controls',
            attemptId: 999999,
            ok: true,
            session: att.session || 'sess-inflight-1',
            companionKey: att.companionKey || '/workspace',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview'
          }, '*'), attempt1);
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);

          // 5. 같은 callId를 가진 다른 세션으로 이동 -> 진행 표시 및 disabled 미혼합
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-inflight-2',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: '세션 2 다른 질문' }],
            ask: {
              kind: 'question',
              callId: 'q-inflight-controls',
              what: '세션 2 배포 환경',
              options: ['선택지 S2-A', '선택지 S2-B']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 선택지 S2-A")');
          assert.equal(await askControls.getAttribute('aria-busy'), null);
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '');
          const choiceBtnsS2 = page.locator('#ask-controls button.choice-btn');
          assert.equal(await choiceBtnsS2.nth(0).isDisabled(), false);
          assert.equal(await choiceBtnsS2.nth(1).isDisabled(), false);

          // 원래 세션으로 복귀 -> 저장소 상태에 맞게 진행 표시 및 disabled 복원
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-inflight-1',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: '전송 중 표시 검증 질문' }],
            ask: {
              kind: 'question',
              callId: 'q-inflight-controls',
              what: '배포 환경을 선택해주세요',
              options: ['1. 프로덕션 환경', '2. 스테이징 환경']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 프로덕션 환경")');
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);

          // 6. 전송 중 rows 재수신 -> 진행 표시 및 disabled 상태 보존
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-inflight-1',
            companionKey: '/workspace',
            rows: [
              { who: 'agent', label: 'magi', text: '전송 중 표시 검증 질문' },
              { who: 'agent', label: 'magi', text: '추가 업데이트 스트림' }
            ],
            ask: {
              kind: 'question',
              callId: 'q-inflight-controls',
              what: '배포 환경을 선택해주세요',
              options: ['1. 프로덕션 환경', '2. 스테이징 환경']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 프로덕션 환경")');
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);

          // 7. 유효 결과 도착 -> 진행 표시 해제 및 버튼 활성화
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-inflight-controls',
            attemptId: att.attemptId,
            ok: true,
            session: att.session || 'sess-inflight-1',
            companionKey: att.companionKey || '/workspace',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview'
          }, '*'), attempt1);

          assert.equal(await askControls.getAttribute('aria-busy'), null);
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '');
          assert.equal(await choiceBtns.nth(0).isDisabled(), false);
          assert.equal(await choiceBtns.nth(1).isDisabled(), false);

          // 8. 320x600 및 420x700 뷰포트에서 Tab 탐색 및 본문/컨트롤 접근성 확인
          for (const vp of [{ width: 320, height: 600 }, { width: 420, height: 700 }]) {
            await page.setViewportSize(vp);
            await jumpBtn.focus();
            assert.equal(await page.evaluate(() => document.activeElement.classList.contains('jump-btn')), true);
            await page.keyboard.press('Tab');
            assert.equal(await page.evaluate(() => document.activeElement === document.querySelectorAll('#ask-controls .acts button')[0]), true);
            await page.keyboard.press('Tab');
            assert.equal(await page.evaluate(() => document.activeElement === document.querySelectorAll('#ask-controls .acts button')[1]), true);
            await page.keyboard.press('Tab');
            assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls button.direct-btn')), true);
          }

          // Cleanup
          await page.setViewportSize({ width: 420, height: 700 });
          await say.fill('');
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-inflight-1',
            companionKey: '/workspace',
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);
        }
      },
      {
        id: 'asks_recovery_copy_and_append_unlocks_general_send_while_in_flight',
        name: '복구 초안 복사 및 이어 붙이기 후 일반 전송 버튼 해제 및 전송 중 격리 (§5.6)',
        run: async (page) => {
          // 1. 세션 및 질문 준비: 옵션이 있는 선택형 질문 수신
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-rec-inflight',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: '복구 및 전송 중 검증 질문' }],
            ask: {
              kind: 'question',
              callId: 'q-rec-inflight',
              what: '배포 대상 클러스터를 선택하세요',
              options: ['1. 운영 클러스터', '2. 검증 클러스터']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 운영 클러스터")');

          const say = page.locator('#say');
          const sendBtn = page.locator('#send');
          const askControls = page.locator('#ask-controls');
          const recoveryBtn = page.locator('#recovery-btn');
          const recoveryPanel = page.locator('#recovery-panel');
          const replyMode = page.locator('#reply-mode');

          // 2. 직접 입력으로 진입하여 첫 번째 답변 A 작성 후 전송 -> 실패 처리
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);

          const draftA = '답변 A: 운영 배포 승인 요청';
          await say.fill(draftA);

          const postedLenBeforeA = await page.evaluate(() => window.__posted.length);
          await sendBtn.click();
          await page.waitForFunction((len) => window.__posted.length > len, postedLenBeforeA);

          const replyA = await page.evaluate(() =>
            window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-rec-inflight').slice(-1)[0]
          );
          assert.ok(replyA, 'Reply A must be posted');

          // 실패 결과(replyResult ok: false) 주입 -> 복구 항목 생성 확인
          await page.evaluate((payload) => window.postMessage(payload, '*'), createReplyResultMessage({
            callId: 'q-rec-inflight',
            ok: false,
            error: 'network timeout',
          }, replyA));

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent === '복구 초안 1');
          assert.equal(await recoveryBtn.textContent(), '복구 초안 1');

          // 3. 같은 질문에 대해 답변 B 재전송 (선택지 1 클릭) -> in-flight 진입
          const choiceBtns = page.locator('#ask-controls button.choice-btn');
          const postedLenBeforeB = await page.evaluate(() => window.__posted.length);
          await choiceBtns.nth(0).click();
          await page.waitForFunction((len) => window.__posted.length > len, postedLenBeforeB);

          const replyB = await page.evaluate(() =>
            window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-rec-inflight').slice(-1)[0]
          );
          assert.ok(replyB, 'Reply B must be posted');

          // In-flight 상태 확인: aria-busy, ask-status, choice buttons disabled
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);
          assert.equal(await choiceBtns.nth(1).isDisabled(), true);

          // 4. 직접 입력으로 재진입 -> 답변 모드에서 sendBtn.disabled === true 확인
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await sendBtn.textContent(), '답변');
          assert.equal(await sendBtn.isDisabled(), true, 'composer send button must be disabled in answer mode while in-flight');

          // 5. 복구 패널 열기 및 초안 A 복사 (일반 초안이 비어있는 상태)
          await recoveryBtn.click();
          await page.waitForFunction(() => !document.getElementById('recovery-panel').hidden);

          const recoveryItem = page.locator('.recovery-item').first();
          const copyBtn = recoveryItem.locator('.copy-btn');
          await copyBtn.click();

          // 단언 (Requirement 1):
          // - 일반 모드로 복귀 (reply-mode hidden, sendBtn 텍스트 'Send')
          await page.waitForFunction(() => document.getElementById('reply-mode').hidden);
          assert.equal(await sendBtn.textContent(), 'Send');
          assert.equal(await say.inputValue(), draftA);
          // - P2 결함 수정 단언: 일반 전송 버튼이 해제(enabled)되어야 함!
          assert.equal(await sendBtn.isDisabled(), false, 'general send button must be released after copying recovery draft');
          // - 전송 중인 질문은 계속 busy, 선택지는 disabled 유지
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await page.locator('#ask-controls .ask-status').textContent(), '답변 전송 중…');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);
          // - 복구 원문 유지 (뱃지 카운트 1 유지)
          assert.equal(await recoveryBtn.textContent(), '복구 초안 1');

          // 6. 일반 작업 실제 클릭 전송 (Requirement 3)
          const postedLenBeforeSay = await page.evaluate(() => window.__posted.length);
          await sendBtn.click();
          await page.waitForFunction((len) => window.__posted.length > len, postedLenBeforeSay);

          const newPosts = await page.evaluate((len) => window.__posted.slice(len), postedLenBeforeSay);
          const sayMsgs = newPosts.filter(m => m.kind === 'say');
          const replyMsgs = newPosts.filter(m => m.kind === 'reply');
          assert.equal(sayMsgs.length, 1, 'Exactly one say message posted');
          assert.equal(sayMsgs[0].text, draftA);
          assert.equal(replyMsgs.length, 0, 'No reply message posted for general say');

          // 같은 질문 직접 입력 재진입 시 답변 전송 버튼 다시 잠금 확인
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await sendBtn.textContent(), '답변');
          assert.equal(await sendBtn.isDisabled(), true, 'send button must re-lock upon re-entering answer mode');

          // 7. 일반 초안 G가 있는 상태에서 이어 붙이기 확인 (Requirement 2)
          // 답변 모드 취소 후 일반 초안 G 작성
          await page.keyboard.press('Escape');
          await page.waitForFunction(() => document.getElementById('reply-mode').hidden);
          const draftG = '일반 초안 메모 G';
          await say.fill(draftG);

          // 다시 답변 모드 진입하여 답변 작성 중인 상태
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          assert.equal(await sendBtn.isDisabled(), true);
          await say.fill('작성 중 임시 답변');

          // 복구 패널에서 복사 클릭 -> 일반 초안 G가 있으므로 확인 상자 표시
          if (await recoveryPanel.evaluate((el) => el.hidden)) {
            await recoveryBtn.click();
            await page.waitForFunction(() => !document.getElementById('recovery-panel').hidden);
          }
          await copyBtn.click();
          await page.waitForSelector('.recovery-confirm-box');
          const confirmBox = recoveryItem.locator('.recovery-confirm-box');

          // A) 취소 클릭: 초안 G 및 답변 초안 보존, 답변 모드 및 잠금 유지
          const cancelBtn = confirmBox.locator('.confirm-cancel-btn');
          await cancelBtn.click();
          await page.waitForFunction(() => document.querySelectorAll('.recovery-confirm-box').length === 0);
          assert.equal(await replyMode.isVisible(), true);
          assert.equal(await sendBtn.textContent(), '답변');
          assert.equal(await sendBtn.isDisabled(), true);
          assert.equal(await say.inputValue(), '작성 중 임시 답변');

          // B) 다시 복사 클릭 -> 이어 붙이기 확정
          await copyBtn.click();
          await page.waitForSelector('.recovery-confirm-box');
          const appendBtn = confirmBox.locator('.confirm-append-btn');
          await appendBtn.click();

          // 검증: 일반 모드로 전환, Send 활성화, G + \n\n + draftA 결합
          const expectedCombined = draftG + '\n\n' + draftA;
          await page.waitForFunction((exp) => document.getElementById('say').value === exp, expectedCombined);
          assert.equal(await replyMode.evaluate((el) => el.hidden), true);
          assert.equal(await sendBtn.textContent(), 'Send');
          assert.equal(await sendBtn.isDisabled(), false, 'Send button enabled after confirmed append');
          assert.equal(await askControls.getAttribute('aria-busy'), 'true');
          assert.equal(await choiceBtns.nth(0).isDisabled(), true);
          assert.equal(await recoveryBtn.textContent(), '복구 초안 1');

          // Cleanup: in-flight 해제
          await page.evaluate((payload) => window.postMessage(payload, '*'), createReplyResultMessage({
            callId: 'q-rec-inflight',
            ok: true,
          }, replyB));

          await page.waitForFunction(() => !document.getElementById('ask-controls').hasAttribute('aria-busy'));
          assert.equal(await choiceBtns.nth(0).isDisabled(), false);
          if (!await recoveryPanel.evaluate((el) => el.hidden)) {
            await recoveryBtn.click();
            await page.waitForFunction(() => document.getElementById('recovery-panel').hidden);
          }
          await say.fill('');
        }
      }
];

export const asksBundle = {
  name: 'asks',
  description: '질문·초안',
  scenarios: asksScenarios,
};
