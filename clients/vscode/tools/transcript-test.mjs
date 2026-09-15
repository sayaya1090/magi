import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { renderChatHtml } from '../out/web/chat_html.js';
import { createRowsMessage } from './transcript-fixtures.mjs';

const require = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const { chromium } = require('playwright');

// Pre-flight check: Ensure all required compiled assets exist before launching browser
const requiredBundles = [
  new URL('../out/web/chat_html.js', import.meta.url),
  new URL('../out/web/answer_state.js', import.meta.url),
  new URL('../out/web/chat_adapter.bundle.js', import.meta.url),
];
for (const b of requiredBundles) {
  if (!existsSync(b)) {
    console.error(`Missing required webview asset bundle: ${b.pathname}\nRun 'npm run build --prefix clients/vscode' first.`);
    process.exit(1);
  }
}

const nonce = 'test-nonce';
const html = renderChatHtml({
  cspSource: "'self' http://magi.test",
  nonce,
  scriptUri: 'http://magi.test/out/web/answer_state.js',
  adapterUri: 'http://magi.test/out/web/chat_adapter.bundle.js',
});

// Define scenario suites across the 4 specified bundles (§2.3)
const bundles = [
  {
    name: 'layout',
    description: '레이아웃·보고서',
    scenarios: [
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
    ]
  },
  {
    name: 'asks',
    description: '질문·초안',
    scenarios: [
      {
        id: 'asks_free_text_answer_mode',
        name: '자유 텍스트 질문 자동 답변 모드 진입 및 일반 초안 격리·복원 (Condition 9)',
        run: async (page) => {
          await page.setViewportSize({ width: 420, height: 600 });
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
            text: '거절될 답변 내용'
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
            text: '재시도할 답변'
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
            text: '구 질문 답변'
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
          await page.locator('#ask-controls button:text("1. 옵션 Alpha")').click();
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#send').click();
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…');

          // Condition 16: User edits to '수정된 답변 B', late failure for attempt 1 arrives
          await page.locator('#say').fill('수정된 답변 B');
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-stale',
            attemptId: att.attemptId,
            ok: false,
            error: 'network timeout',
            text: '답변 A'
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
            ok: true
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
            text: '답변 A'
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
            text: '답변 A'
          }, '*'), attemptA);

          assert.equal(await page.locator('#say').inputValue(), '답변 B', 'draft B preserved against stale attempt A arrival');
          assert.equal(await page.locator('#reply-mode').isVisible(), true, 'reply-mode preserved against stale attempt A arrival');

          // In-flight lock for B remains active
          await page.locator('#send').click();
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…', 'lock for attempt B still active');
          await page.locator('#ask-controls button:text("1. 옵션 1")').click();
          assert.equal(await page.locator('#note').textContent(), 'reply already in flight…', 'choice click blocked by in-flight lock B');

          // Step 5: B의 실제 성공 응답 도착 -> 정상 처리 및 잠금 해제
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-resend-test',
            attemptId: att.attemptId,
            ok: true
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
        name: '비정상 payload 디스패치 시 파서 거부 및 기존 DOM·입력 상태 보존',
        run: async (page) => {
          await page.locator('#say').fill('보존되어야 할 입력');
          await page.evaluate(() => {
            window.postMessage({ kind: 'rows', rows: 'invalid-non-array' }, '*');
            window.postMessage({ kind: 'unknown_kind_never_seen' }, '*');
            window.postMessage(null, '*');
            window.postMessage(12345, '*');
            window.postMessage({ kind: 'replyResult', callId: '' }, '*');
          });
          assert.equal(await page.locator('#say').inputValue(), '보존되어야 할 입력', 'input composer was not cleared by malformed messages');
          assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode was not triggered by malformed messages');
        }
      }
    ]
  },
  {
    name: 'autocomplete',
    description: '자동완성',
    scenarios: [
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
          await page.evaluate((att) => window.postMessage({ kind: 'replyResult', callId: 'q-ac1', attemptId: att.attemptId, ok: true }, '*'), postedQAc1);

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
    ]
  },
  {
    name: 'diff',
    description: 'diff·파일 이동',
    scenarios: [
      {
        id: 'diff_permission_ask_dismissal',
        name: '권한 질문 diff 자체 스크롤바 없음, 승인 및 기각 처리 (Condition 7)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }],
            ask: {
              kind: 'permission',
              callId: 'call-perm-99',
              what: 'bash',
              args: 'git diff\n'.repeat(50),
              reason: 'check status',
              diff: 'diff --git a/f b/f\n+new line\n'.repeat(100)
            }
          }));

          await page.waitForSelector('#ask-body pre.diff');
          const diffScroll = await page.locator('#ask-body pre.diff').evaluate((el) => ({ height: el.clientHeight, scroll: el.scrollHeight }));
          assert.equal(diffScroll.height, diffScroll.scroll, 'diff has no separate vertical scrollbar');

          // Click allow
          await page.locator('#ask-controls button:text("allow")').click();
          const posted = await page.evaluate(() => window.__posted);
          assert.ok(posted.some((m) => m.kind === 'answer' && m.callId === 'call-perm-99' && m.decision === 'allow'));

          // Dismiss ask
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: 'turn 1' }],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-body').hidden && document.getElementById('ask-controls').hidden);
          assert.equal(await page.locator('#ask-body').evaluate((el) => el.textContent), '');
          assert.equal(await page.locator('#ask-controls').evaluate((el) => el.textContent), '');
        }
      },
      {
        id: 'diff_syntax_highlighting_and_preservation',
        name: 'diff 구문 강조, 분류, 원문 보존, HTML 안전성 및 unparsed fallback (Condition 19)',
        run: async (page) => {
          const complexDiff = [
            'diff --git a/src/math.cpp b/src/math.cpp',
            'index e69de29..4b825dc 100644',
            '--- a/src/math.cpp',
            '+++ b/src/math.cpp',
            '@@ -1,7 +1,7 @@',
            ' void calc() {',
            '   int x = 10;',
            '---x;',
            '+++x;',
            '',
            '-  <div id="unescaped-old">old html</div>',
            '+  <div id="unescaped-new">new html</div>',
            ' }',
            '\\ No newline at end of file',
            'diff --git a/src/util.py b/src/util.py',
            '--- a/src/util.py',
            '+++ b/src/util.py',
            '@@ -1,2 +1,2 @@',
            '-old_code()',
            '+new_code()'
          ].join('\n');

          await page.evaluate(({ msg, diffText }) => {
            msg.ask.diff = diffText;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: {
                kind: 'permission',
                callId: 'perm-diff-test',
                what: 'git apply',
                args: 'patch.diff',
                reason: 'apply math and util updates',
              }
            }),
            diffText: complexDiff
          });

          await page.waitForSelector('#ask-body pre.diff .diff-line');

          // Classification checks
          const fileHeaders = await page.locator('#ask-body pre.diff .diff-file-header').allInnerTexts();
          assert.ok(fileHeaders.length >= 6);
          assert.ok(fileHeaders[0].includes('diff --git a/src/math.cpp'));
          assert.ok(fileHeaders[2].includes('--- a/src/math.cpp'));
          assert.ok(fileHeaders[3].includes('+++ b/src/math.cpp'));
          assert.ok(fileHeaders[4].includes('diff --git a/src/util.py'));

          const hunkHeaders = await page.locator('#ask-body pre.diff .diff-hunk-header').allInnerTexts();
          assert.equal(hunkHeaders.length, 2);
          assert.ok(hunkHeaders[0].includes('@@ -1,7 +1,7 @@'));
          assert.ok(hunkHeaders[1].includes('@@ -1,2 +1,2 @@'));

          const deletedLines = await page.locator('#ask-body pre.diff .diff-deleted').allInnerTexts();
          assert.ok(deletedLines.some(t => t.includes('---x;')), '---x; is classified as diff-deleted, not file header');
          assert.ok(deletedLines.some(t => t.includes('<div id="unescaped-old">')), 'deleted html line is diff-deleted');
          assert.ok(deletedLines.some(t => t.includes('old_code()')));

          const addedLines = await page.locator('#ask-body pre.diff .diff-added').allInnerTexts();
          assert.ok(addedLines.some(t => t.includes('+++x;')), '+++x; is classified as diff-added, not file header');
          assert.ok(addedLines.some(t => t.includes('<div id="unescaped-new">')), 'added html line is diff-added');
          assert.ok(addedLines.some(t => t.includes('new_code()')));

          const contextLines = await page.locator('#ask-body pre.diff .diff-context').allInnerTexts();
          assert.ok(contextLines.some(t => t.includes('void calc()')));
          assert.ok(contextLines.some(t => t.includes('\\ No newline at end of file')));

          // Raw text exact match
          const renderedText = await page.locator('#ask-body pre.diff').evaluate(el => el.textContent);
          assert.equal(renderedText, complexDiff, 'diff textContent exactly equals the input raw diff');

          // HTML escape security
          const unescapedDiv = await page.locator('#unescaped-old').count();
          assert.equal(unescapedDiv, 0, 'HTML tag inside diff was not executed/injected as a DOM element');

          // No vertical scrollbar
          const diffScrollBounds = await page.locator('#ask-body pre.diff').evaluate((el) => ({ height: el.clientHeight, scroll: el.scrollHeight }));
          assert.equal(diffScrollBounds.height, diffScrollBounds.scroll, 'diff has no separate vertical scrollbar');

          // Theme style distinction
          const addedBg = await page.locator('#ask-body pre.diff .diff-added').first().evaluate(el => getComputedStyle(el).backgroundColor);
          const deletedBg = await page.locator('#ask-body pre.diff .diff-deleted').first().evaluate(el => getComputedStyle(el).backgroundColor);
          assert.notEqual(addedBg, 'rgba(0, 0, 0, 0)', 'added line has non-transparent background');
          assert.notEqual(deletedBg, 'rgba(0, 0, 0, 0)', 'deleted line has non-transparent background');
          assert.notEqual(addedBg, deletedBg, 'added and deleted backgrounds are visually distinct');

          // Diff without trailing newline
          const noTrailingNlDiff = 'diff --git a/f b/f\n@@ -1 +1 @@\n-old\n+new';
          await page.evaluate(({ msg, diffText }) => {
            msg.ask.diff = diffText;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: { kind: 'permission', callId: 'perm-no-nl', what: 'test' }
            }),
            diffText: noTrailingNlDiff
          });
          await page.waitForSelector('#ask-body pre.diff .diff-line');
          const noNlRendered = await page.locator('#ask-body pre.diff').evaluate(el => el.textContent);
          assert.equal(noNlRendered, noTrailingNlDiff, 'diff without trailing newline matches textContent exactly');

          // Unparsed fallback
          const unparsedDiff = 'Custom raw patch metadata without diff markers\nsome random text';
          await page.evaluate(({ msg, diffText }) => {
            msg.ask.diff = diffText;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: { kind: 'permission', callId: 'perm-unparsed', what: 'test' }
            }),
            diffText: unparsedDiff
          });
          await page.waitForSelector('#ask-body pre.diff .diff-plain');
          const unparsedRendered = await page.locator('#ask-body pre.diff').evaluate(el => el.textContent);
          assert.equal(unparsedRendered, unparsedDiff, 'unparsed text preserved in raw form');

          await page.locator('#ask-controls button:text("allow")').click();
        }
      },
      {
        id: 'diff_hunk_line_count_and_multifile',
        name: 'Hunk 줄 수 추적, --- a/example diff-deleted/added 분류, a/ b/ 없는 다중 파일 (Condition 20)',
        run: async (page) => {
          const countTrackDiff = [
            '@@ -1 +1 @@',
            '--- a/example',
            '+++ b/example',
            '--- file2.txt',
            '+++ file2.txt',
            '@@ -1 +1 @@',
            '-foo',
            '+bar'
          ].join('\n');

          await page.evaluate(({ msg, diffText }) => {
            msg.ask.diff = diffText;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: { kind: 'permission', callId: 'perm-count-diff', what: 'git apply' }
            }),
            diffText: countTrackDiff
          });

          await page.waitForFunction(() => document.querySelector('#ask-body pre.diff')?.textContent.includes('--- a/example'));

          const countDeleted = await page.locator('#ask-body pre.diff .diff-deleted').allInnerTexts();
          assert.ok(countDeleted.some(t => t.includes('--- a/example')), '--- a/example in hunk is classified as diff-deleted');
          assert.ok(countDeleted.some(t => t.includes('-foo')), '-foo is classified as diff-deleted');

          const countAdded = await page.locator('#ask-body pre.diff .diff-added').allInnerTexts();
          assert.ok(countAdded.some(t => t.includes('+++ b/example')), '+++ b/example in hunk is classified as diff-added');
          assert.ok(countAdded.some(t => t.includes('+bar')), '+bar is classified as diff-added');

          const countFileHeaders = await page.locator('#ask-body pre.diff .diff-file-header').allInnerTexts();
          assert.ok(countFileHeaders.some(t => t.includes('--- file2.txt')), '--- file2.txt recognized as file header');
          assert.ok(countFileHeaders.some(t => t.includes('+++ file2.txt')), '+++ file2.txt recognized as file header');

          const countRendered = await page.locator('#ask-body pre.diff').evaluate(el => el.textContent);
          assert.equal(countRendered, countTrackDiff, 'textContent of countTrackDiff matches input exactly');

          await page.locator('#ask-controls button:text("allow")').click();
        }
      },
      {
        id: 'diff_approval_panel_and_inspection_click',
        name: '승인 패널 대상 파일명, 설명, 변경 보기 버튼 및 순수 조회 클릭 (Condition 21)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-native',
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'permission',
              callId: 'perm-edit-native',
              diffKind: 'sides',
              what: 'edit',
              filePath: 'src/model/user.ts',
              args: JSON.stringify({ path: 'src/model/user.ts', old: 'const a = 1;\n', new: 'const a = 2;\n' }),
              reason: 'update user version property'
            }
          }));

          await page.waitForFunction(() => document.querySelector('#ask-controls .summary-text')?.textContent.includes('user.ts'));
          const sumTextNative = await page.locator('#ask-controls .summary-text').textContent();
          assert.ok(sumTextNative.includes('user.ts'), 'summary row includes target filename');
          assert.ok(sumTextNative.includes('edit'), 'summary row includes what');

          const fileTarget = await page.locator('#ask-body .file-target').textContent();
          assert.equal(fileTarget, '파일: src/model/user.ts', 'target file path is displayed in ask body');

          const actsBtns = await page.locator('#ask-controls .acts button').allTextContents();
          assert.deepEqual(actsBtns, ['변경 보기', 'allow', 'deny', 'always'], 'actions include 변경 보기 and approval buttons');

          // Clicking '변경 보기' posts kind: 'diff' without approving
          const postedLenBefore = await page.evaluate(() => window.__posted.length);
          await page.locator('#ask-controls button:text("변경 보기")').click();
          const postedAfterDiff = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterDiff.length, postedLenBefore + 1, 'posted exactly one message on diff click');
          const lastPosted = postedAfterDiff[postedAfterDiff.length - 1];
          assert.deepEqual(lastPosted, { kind: 'diff', session: 'sess-perm-native', callId: 'perm-edit-native' });
          assert.ok(!postedAfterDiff.some(m => m.kind === 'answer' && m.callId === 'perm-edit-native'), 'diff click does not approve');

          // Clicking target file in approval body posts kind: 'open'
          await page.locator('#ask-body .file-target button.file-nav-btn').click();
          const postedAfterOpen = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterOpen.length, postedAfterDiff.length + 1, 'posted exactly one message on open file click');
          const openPosted = postedAfterOpen[postedAfterOpen.length - 1];
          assert.deepEqual(openPosted, { kind: 'open', session: 'sess-perm-native', callId: 'perm-edit-native' });
          assert.ok(!postedAfterOpen.some(m => m.kind === 'answer' && m.callId === 'perm-edit-native'), 'open file click does not approve');

          // Raw patch permission also shows 변경 보기
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-native',
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'permission',
              callId: 'perm-patch-native',
              diffKind: 'patch',
              what: 'write',
              filePath: 'README.md',
              args: JSON.stringify({ path: 'README.md' }),
              diff: '--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-# Old\n+# New\n'
            }
          }));
          await page.waitForFunction(() => document.querySelector('#ask-controls .summary-text')?.textContent.includes('README.md'));
          await page.locator('#ask-controls button:text("변경 보기")').click();
          const postedPatchDiff = await page.evaluate(() => window.__posted);
          assert.ok(postedPatchDiff.some(m => m.kind === 'diff' && m.callId === 'perm-patch-native'), 'patch shows and triggers diff');

          // Permission with diffKind: 'none' (e.g. replaceAll: 'TRUE') never shows phantom 변경 보기 button
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-native',
            rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
            ask: {
              kind: 'permission',
              callId: 'perm-edit-rejected',
              diffKind: 'none',
              what: 'edit',
              filePath: 'src/config.ts',
              args: JSON.stringify({ path: 'src/config.ts', old: 'a', new: 'b', replaceAll: 'TRUE' })
            }
          }));
          await page.waitForFunction(() => document.querySelector('#ask-controls .summary-text')?.textContent.includes('config.ts'));
          const rejectedBtns = await page.locator('#ask-controls .acts button').allTextContents();
          assert.deepEqual(rejectedBtns, ['allow', 'deny', 'always'], 'no phantom 변경 보기 button when host flags diffKind as none');
        }
      },
      {
        id: 'diff_tool_row_navigation_and_command_args',
        name: '도구 행 파일 및 줄 이동 링크, 명령 행 일반 인자 (Condition 22)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-42',
            rows: [
              { who: 'tool', label: 'read ✓', text: 'read', seq: 42, callId: 'c-42', fileNav: { path: 'src/app.ts', line: 15 }, args: '{"limit":30,"offset":15,"path":"src/app.ts"}' },
              { who: 'tool', label: 'bash ✓', text: 'bash', seq: 43, args: 'npm test' }
            ],
            ask: null
          }));
          await page.waitForSelector('#rows .row.tool');

          const fileRowBtn = page.locator('#rows .row.tool button.file-nav-btn');
          await fileRowBtn.waitFor();
          const fileBtnText = await fileRowBtn.textContent();
          assert.equal(fileBtnText, 'src/app.ts:15', 'tool row renders fileNav with line');

          const toolArgs = await page.locator('#rows .row.tool:nth-child(1) span.args').textContent();
          assert.ok(toolArgs.includes('limit'), 'args remains accessible alongside fileNav button');
          assert.ok(toolArgs.includes('15'), 'offset remains accessible');

          // Click tool row file button
          const postedBeforeRowClick = await page.evaluate(() => window.__posted.length);
          await fileRowBtn.click();
          const postedAfterRowClick = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterRowClick.length, postedBeforeRowClick + 1, 'posted one message on tool row file click');
          const rowPosted = postedAfterRowClick[postedAfterRowClick.length - 1];
          assert.deepEqual(rowPosted, { kind: 'open', seq: 42, session: 'sess-42', callId: 'c-42' });

          // Bash row has plain args span
          const bashArgs = await page.locator('#rows .row.tool:nth-child(2) span.args').textContent();
          assert.equal(bashArgs, 'npm test', 'command tool renders plain args');
          const bashBtnCount = await page.locator('#rows .row.tool:nth-child(2) button.file-nav-btn').count();
          assert.equal(bashBtnCount, 0, 'command tool has no file-nav-btn');
        }
      },
      {
        id: 'diff_long_args_raw_toggle_expansion',
        name: '긴 인자 rawArgs 접기/펼치기, END_OF_NEW 보존, 추가 스크롤 컨테이너 없음 (Condition 23)',
        run: async (page) => {
          const longOldText = 'x'.repeat(150);
          const rawEditArgs = JSON.stringify({ path: 'src/main.ts', old: longOldText, new: 'END_OF_NEW' }, null, 2);
          const summaryEditArgs = '{"path":"src/main.ts","old":"' + 'x'.repeat(70) + '…';

          await page.evaluate(({ msg, rawArgs, summaryArgs }) => {
            msg.rows[0].rawArgs = rawArgs;
            msg.rows[0].args = summaryArgs;
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              session: 'sess-toggle-test',
              rows: [{
                who: 'tool',
                label: 'edit ✓',
                text: 'edit',
                seq: 50,
                callId: 'c-50',
                fileNav: { path: 'src/main.ts' }
              }],
              ask: null
            }),
            rawArgs: rawEditArgs,
            summaryArgs: summaryEditArgs
          });

          await page.waitForSelector('.row.tool .args-toggle-btn');
          const toggleBtn = page.locator('.row.tool .args-toggle-btn');
          const rawBox = page.locator('.row.tool pre.raw-args');

          // Initially folded
          assert.equal(await rawBox.isHidden(), true, 'raw-args block is initially hidden');
          assert.equal(await toggleBtn.getAttribute('aria-expanded'), 'false');

          // Expand
          await toggleBtn.click();
          assert.equal(await rawBox.isVisible(), true, 'raw-args block is visible after toggle click');
          assert.equal(await toggleBtn.getAttribute('aria-expanded'), 'true');
          const renderedRaw = await rawBox.textContent();
          assert.ok(renderedRaw.includes(longOldText), 'raw-args preserves 150-character old argument');
          assert.ok(renderedRaw.includes('END_OF_NEW'), 'raw-args preserves END_OF_NEW to the very end');

          // No separate vertical scroll container
          const rawBounds = await rawBox.evaluate(el => ({ height: el.clientHeight, scroll: el.scrollHeight }));
          assert.equal(rawBounds.height, rawBounds.scroll, 'raw-args has no internal vertical scroll container');

          // Fold again
          await toggleBtn.click();
          assert.equal(await rawBox.isHidden(), true, 'raw-args is folded again after second click');
        }
      },
      {
        id: 'diff_unconfirmed_session_and_missing_filepath',
        name: '미확인 세션 버튼 비활성화, filePath 누락 시 가상 버튼 없음 (Condition 24)',
        run: async (page) => {
          // Explicitly set empty session to test unconfirmed session contract
          await page.evaluate(() => window.postMessage({
            kind: 'rows',
            session: '',
            rows: [
              { who: 'tool', label: 'edit ✓', text: 'edit', seq: 60, callId: 'c-60', fileNav: { path: 'src/test.ts' }, args: 'src/test.ts' }
            ],
            ask: {
              kind: 'permission',
              callId: 'perm-no-filepath',
              what: 'edit',
              args: JSON.stringify({ path: 'src/phantom.ts', old: '1', new: '2' })
            },
            refs: []
          }, '*'));

          await page.waitForSelector('.row.tool button.file-nav-btn:has-text("src/test.ts")');
          const unconfirmedToolBtn = page.locator('.row.tool button.file-nav-btn:has-text("src/test.ts")');
          assert.equal(await unconfirmedToolBtn.isDisabled(), true, 'tool row button is disabled when session is unconfirmed');

          const phantomBtnCount = await page.locator('#ask-body .file-target').count();
          assert.equal(phantomBtnCount, 0, 'webview does not create file-target when host omits filePath');
        }
      },
      {
        id: 'diff_render_time_session_binding',
        name: '렌더 시점 세션 클로저 바인딩 (Condition 25)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-original',
            rows: [
              { who: 'tool', label: 'edit ✓', text: 'edit', seq: 70, callId: 'c-70', fileNav: { path: 'src/frozen.ts' }, args: 'src/frozen.ts' }
            ],
            ask: {
              kind: 'permission',
              callId: 'perm-frozen',
              filePath: 'src/frozen_ask.ts',
              what: 'edit'
            }
          }));

          await page.waitForSelector('.row.tool button.file-nav-btn:has-text("src/frozen.ts")');
          const frozenToolBtn = page.locator('.row.tool button.file-nav-btn:has-text("src/frozen.ts")');
          const frozenAskBtn = page.locator('#ask-body .file-target button.file-nav-btn');

          await frozenToolBtn.click();
          const postedAfterFrozenTool = await page.evaluate(() => window.__posted);
          const toolMsg = postedAfterFrozenTool[postedAfterFrozenTool.length - 1];
          assert.equal(toolMsg.session, 'sess-original', 'tool button click preserved bound session from render time');

          await frozenAskBtn.click();
          const postedAfterFrozenAsk = await page.evaluate(() => window.__posted);
          const askMsg = postedAfterFrozenAsk[postedAfterFrozenAsk.length - 1];
          assert.equal(askMsg.session, 'sess-original', 'ask button click preserved bound session from render time');
        }
      },
      {
        id: 'diff_redraw_retention_and_session_switch',
        name: 'rawArgs 펼침 및 버튼 포커스 redraw 간 보존, 세션 전환 시 초기화 및 격리 (Condition 26, 27)',
        run: async (page) => {
          const longText = 'k'.repeat(120);
          await page.evaluate(({ msg, longText }) => {
            msg.rows[0].rawArgs = JSON.stringify({ path: 'src/persist.ts', old: longText, new: 'END_OF_NEW' });
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              session: 'sess-persisted',
              rows: [{
                who: 'tool',
                label: 'edit ✓',
                text: 'edit',
                seq: 80,
                callId: 'c-80-persisted',
                fileNav: { path: 'src/persist.ts' },
                args: 'src/persist.ts {"old":"clipped..."}'
              }],
              ask: null
            }),
            longText
          });

          await page.waitForSelector('.row.tool .args-toggle-btn[data-call-id="c-80-persisted"]');
          const persistToggle = page.locator('.row.tool .args-toggle-btn[data-call-id="c-80-persisted"]');
          const persistRaw = page.locator('.row.tool pre.raw-args');

          // Initially hidden
          assert.equal(await persistRaw.isHidden(), true);

          // Expand and focus
          await persistToggle.click();
          assert.equal(await persistRaw.isVisible(), true);
          await persistToggle.focus();
          const focusedBeforeRedraw = await page.evaluate(() => document.activeElement?.dataset?.callId);
          assert.equal(focusedBeforeRedraw, 'c-80-persisted', 'toggle button is focused before redraw');

          // Redraw with new event added
          await page.evaluate(({ msg, longText }) => {
            msg.rows[0].rawArgs = JSON.stringify({ path: 'src/persist.ts', old: longText, new: 'END_OF_NEW' });
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              session: 'sess-persisted',
              rows: [
                {
                  who: 'tool',
                  label: 'edit ✓',
                  text: 'edit',
                  seq: 80,
                  callId: 'c-80-persisted',
                  fileNav: { path: 'src/persist.ts' },
                  args: 'src/persist.ts {"old":"clipped..."}'
                },
                {
                  who: 'tool',
                  label: 'bash ✓',
                  text: 'bash',
                  seq: 81,
                  callId: 'c-81-new',
                  args: 'echo done'
                }
              ],
              ask: null
            }),
            longText
          });

          await page.waitForSelector('.row.tool:has-text("echo done")');
          const persistRawAfter = page.locator('.row.tool pre.raw-args');
          assert.equal(await persistRawAfter.isVisible(), true, 'raw-args remains visible after redraw');
          const rawTextAfter = await persistRawAfter.textContent();
          assert.ok(rawTextAfter.includes('END_OF_NEW'), 'raw-args still displays unclipped content to END_OF_NEW');

          const focusedAfterRedraw = await page.evaluate(() => document.activeElement?.dataset?.callId);
          assert.equal(focusedAfterRedraw, 'c-80-persisted', 'focus was restored to the toggle button after redraw');

          // Condition 27: Session switch isolates and clears expanded state
          await page.evaluate(({ msg, longText }) => {
            msg.rows[0].rawArgs = JSON.stringify({ path: 'src/persist.ts', old: longText, new: 'END_OF_NEW' });
            window.postMessage(msg, '*');
          }, {
            msg: createRowsMessage({
              session: 'sess-brand-new',
              rows: [{
                who: 'tool',
                label: 'edit ✓',
                text: 'edit',
                seq: 80,
                callId: 'c-80-persisted', // identical callId in new session
                fileNav: { path: 'src/persist.ts' },
                args: 'src/persist.ts {"old":"clipped..."}'
              }],
              ask: null
            }),
            longText
          });

          await page.waitForSelector('.row.tool .args-toggle-btn[data-call-id="c-80-persisted"]');
          const newSessionRaw = page.locator('.row.tool pre.raw-args');
          const newSessionToggle = page.locator('.row.tool .args-toggle-btn[data-call-id="c-80-persisted"]');
          assert.equal(await newSessionRaw.isHidden(), true, 'raw-args is folded again in brand new session');
          assert.equal(await newSessionToggle.getAttribute('aria-expanded'), 'false');
        }
      },
      {
        id: 'diff_button_session_binding_and_unconfirmed_disable',
        name: 'diff 버튼 세션 바인딩 및 미확인 세션 비활성화 (Condition 28)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-diff-bound',
            rows: [],
            ask: {
              kind: 'permission',
              callId: 'perm-diff-test',
              diffKind: 'sides',
              what: 'edit',
              filePath: 'src/diff_target.ts'
            }
          }));

          await page.waitForSelector('button.diff-btn');
          const diffBtn = page.locator('button.diff-btn');
          assert.equal(await diffBtn.isDisabled(), false);
          await diffBtn.click();
          const postedAfterDiffCond28 = await page.evaluate(() => window.__posted);
          const diffMsg = postedAfterDiffCond28[postedAfterDiffCond28.length - 1];
          assert.equal(diffMsg.kind, 'diff');
          assert.equal(diffMsg.session, 'sess-diff-bound');
          assert.equal(diffMsg.callId, 'perm-diff-test');

          // Unconfirmed session on diffBtn (explicitly empty session)
          await page.evaluate(() => window.postMessage({
            kind: 'rows',
            session: '',
            rows: [],
            ask: {
              kind: 'permission',
              callId: 'perm-diff-no-sess',
              diffKind: 'sides',
              what: 'edit'
            },
            refs: []
          }, '*'));

          await page.waitForSelector('button.diff-btn:disabled');
          const diffBtnUnconfirmed = page.locator('button.diff-btn');
          assert.equal(await diffBtnUnconfirmed.isDisabled(), true, 'diff button is disabled when session is unconfirmed');
        }
      }
    ]
  }
];

// Parse CLI arguments: --bundle=<name>, --reverse
const args = process.argv.slice(2);
let selectedBundleName = null;
let isReverse = false;

for (const arg of args) {
  if (arg.startsWith('--bundle=')) {
    selectedBundleName = arg.slice('--bundle='.length).trim();
  } else if (arg === '--reverse') {
    isReverse = true;
  }
}

let runBundles = [...bundles];
if (selectedBundleName) {
  runBundles = bundles.filter(b => b.name === selectedBundleName);
  if (runBundles.length === 0) {
    console.error(`Unknown bundle: "${selectedBundleName}". Available bundles: ${bundles.map(b => b.name).join(', ')}`);
    process.exit(1);
  }
}
if (isReverse) {
  runBundles.reverse();
}

console.log(`Starting transcript-test browser harness (bundles: ${runBundles.map(b => b.name).join(', ')})...`);

const browser = await chromium.launch({ headless: true });
let totalPassed = 0;
let totalFailed = 0;
const failures = [];

try {
  for (const bundle of runBundles) {
    console.log(`\n▶ Bundle: ${bundle.name} (${bundle.description})`);
    const context = await browser.newContext();
    try {
      const page = await context.newPage({ viewport: { width: 420, height: 600 } });
      const errors = [];
      const routeErrors = [];

      page.on('pageerror', (e) => errors.push(e.message));
      page.on('console', (m) => {
        if (m.type() === 'error') errors.push(m.text());
      });

      // Strict acquireVsCodeApi mock without postMessage auto-patching (§2.1, §2.3)
      await page.addInitScript(() => {
        window.__posted = [];
        window.acquireVsCodeApi = () => ({
          postMessage(m) { window.__posted.push(m); },
          getState() {},
          setState() {}
        });
      });

      // Strict route mapping: unknown asset requests reject with 404 and record failure
      await page.route('http://magi.test/**', async (route) => {
        const url = route.request().url();
        if (url === 'http://magi.test/' || url === 'http://magi.test/index.html') {
          return route.fulfill({ contentType: 'text/html; charset=utf-8', body: html });
        }
        if (url.includes('out/web/answer_state.js')) {
          const js = await readFile(new URL('../out/web/answer_state.js', import.meta.url), 'utf8');
          return route.fulfill({ contentType: 'application/javascript; charset=utf-8', body: js });
        }
        if (url.includes('out/web/chat_adapter.bundle.js') || url.includes('out/web/chat_adapter.js')) {
          const js = await readFile(new URL('../out/web/chat_adapter.bundle.js', import.meta.url), 'utf8');
          return route.fulfill({ contentType: 'application/javascript; charset=utf-8', body: js });
        }
        routeErrors.push(`Unregistered asset requested: ${url}`);
        return route.fulfill({ status: 404, body: 'Not Found' });
      });

      await page.goto('http://magi.test/');

      for (const scenario of bundle.scenarios) {
        try {
          await scenario.run(page);
          assert.deepEqual(errors, [], `Page error occurred in [${bundle.name}] ${scenario.id}`);
          assert.deepEqual(routeErrors, [], `Unregistered route error occurred in [${bundle.name}] ${scenario.id}`);
          totalPassed++;
          console.log(`  PASS: [${scenario.id}] ${scenario.name}`);
        } catch (err) {
          totalFailed++;
          failures.push({ bundle: bundle.name, id: scenario.id, error: err });
          console.error(`  FAIL: [${scenario.id}] ${scenario.name}:`, err);
          throw err;
        }
      }
    } finally {
      await context.close();
    }
  }

  console.log(`\nSUMMARY: ${totalPassed} passed, ${totalFailed} failed across ${runBundles.length} bundle(s).`);
  if (totalFailed > 0) {
    process.exit(1);
  }
} finally {
  await browser.close();
}
