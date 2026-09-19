import assert from 'node:assert/strict';
import { createRowsMessage } from '../../transcript-fixtures.mjs';
import {
  A11Y_THEMES,
  injectA11yTheme,
  runA11yStateAudit,
} from '../a11y-helpers.mjs';

export function createA11yScenarios({ reverseThemes = false } = {}) {
  return [
      {
        id: 'a11y_state_1_conversation',
        name: '일반 대화 화면 3개 테마 × 2개 뷰포트 axe-core 감사 (§5.8)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [
              { who: 'user', label: 'User', text: '프로젝트 설정을 확인해줘' },
              { who: 'agent', label: 'Magi', text: '다음과 같이 설정을 확인했습니다:\n- 포트: 8080\n- 모드: 프로덕션' },
              { who: 'council', label: 'Council', text: '합의 완료', cite: 'diff --git a/config.json b/config.json', keep: '기존 타임아웃 유지' },
            ],
            refs: ['src/config.ts']
          }));
          await page.waitForSelector('.row.agent');
          await runA11yStateAudit(page, {
            stateId: 'state_1_conversation', reverseThemes,
          });
        }
      },
      {
        id: 'a11y_state_2_multiple_choice',
        name: '선택형 질문 화면 3개 테마 × 2개 뷰포트 axe-core 감사 (§5.8)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'Magi', text: '배포 환경을 선택해 주세요.' }],
            ask: {
              kind: 'question',
              callId: 'q-mc-a11y',
              what: '배포 대상 클러스터를 선택하세요',
              options: ['클러스터 A (서울 리전)', '클러스터 B (도쿄 리전)'],
              index: 1,
              total: 1,
              report: [{ key: 'tried', text: '사전 헬스체크 통과' }]
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 클러스터 A (서울 리전)")');
          await runA11yStateAudit(page, {
            stateId: 'state_2_multiple_choice', reverseThemes,
          });
        }
      },
      {
        id: 'a11y_state_3_answer_mode',
        name: '답변 모드 화면 3개 테마 × 2개 뷰포트 axe-core 감사 (§5.8)',
        run: async (page) => {
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.waitForFunction(() => !document.getElementById('reply-mode').hidden);
          await runA11yStateAudit(page, {
            stateId: 'state_3_answer_mode', reverseThemes,
          });
          await page.keyboard.press('Escape');
          await page.waitForFunction(() => document.getElementById('reply-mode').hidden);
        }
      },
      {
        id: 'a11y_state_4_permission_diff',
        name: '승인 패널 (diff) 화면 3개 테마 × 2개 뷰포트 axe-core 감사 (§5.8)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-a11y',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'Magi', text: '다음 파일 변경을 승인하시겠습니까?' }],
            ask: {
              kind: 'permission',
              callId: 'perm-a11y-1',
              what: 'write_file',
              filePath: 'src/main.ts',
              diffKind: 'patch',
              diff: '--- a/src/main.ts\n+++ b/src/main.ts\n@@ -1,3 +1,3 @@\n-const v = 1;\n+const v = 2;\n',
              reason: '버전 범프 적용'
            }
          }));
          await page.waitForSelector('#ask-controls .acts button.approval-btn');
          await runA11yStateAudit(page, {
            stateId: 'state_4_permission_diff', reverseThemes,
          });
        }
      },
      {
        id: 'a11y_state_5_recovery_and_append',
        name: '복구 목록 및 이어 붙이기 확인 화면 3개 테마 × 2개 뷰포트 axe-core 감사 (§5.8)',
        run: async (page) => {
          // Send answer to generate failed recovery item
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-rec-a11y-bundle',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: '복구 감사용 질문' }],
            ask: {
              kind: 'question',
              callId: 'q-rec-a11y-bundle',
              what: '복구 항목 생성 질문',
              options: ['선택 1']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("직접 입력")');
          await page.locator('#ask-controls button:text("직접 입력")').click();
          await page.locator('#say').fill('실패할 답변 내용');
          await page.locator('#send').click();

          const replyMsg = await page.evaluate(() => window.__posted.filter(m => m.kind === 'reply' && m.callId === 'q-rec-a11y-bundle').slice(-1)[0]);
          await page.evaluate((att) => window.postMessage({
            kind: 'replyResult',
            callId: 'q-rec-a11y-bundle',
            attemptId: att.attemptId,
            ok: false,
            error: '전송 실패 오류',
            companionKey: att.companionKey || '/workspace',
            session: att.session || 'sess-rec-a11y-bundle',
            generation: att.generation ?? 0,
            webviewId: att.webviewId || 'test-webview',
          }, '*'), replyMsg);

          await page.waitForFunction(() => document.getElementById('recovery-btn').textContent.includes('1'));
          await page.locator('#recovery-btn').click();
          await page.waitForFunction(() => !document.getElementById('recovery-panel').hidden);

          // Exit answer mode, write general draft, click copy to show confirm box
          await page.locator('#reply-cancel').click();
          await page.locator('#say').fill('기존 작성 중이던 일반 초안');
          await page.locator('.recovery-item .copy-btn').click();
          await page.waitForSelector('.recovery-confirm-box');

          await runA11yStateAudit(page, {
            stateId: 'state_5_recovery_and_append', reverseThemes,
          });

          // Clean up confirm box and close recovery panel
          const cancelBtn = page.locator('.recovery-confirm-box .confirm-cancel-btn');
          if (await cancelBtn.count() > 0) {
            await cancelBtn.click();
          }
          await page.locator('#recovery-btn').click();
          await page.locator('#say').fill('');
        }
      },
      {
        id: 'a11y_state_6_inflight_question',
        name: '질문 전송 중 화면 3개 테마 × 2개 뷰포트 axe-core 감사 (§5.8)',
        run: async (page) => {
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-inflight-a11y-bundle',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: '작업 진행 확인' }],
            ask: {
              kind: 'question',
              callId: 'q-inflight-bundle',
              what: '실행 확인 질문',
              options: ['진행', '취소']
            }
          }));
          await page.waitForSelector('#ask-controls button:text("1. 진행")');
          await page.locator('#ask-controls button:text("1. 진행")').click();
          await page.waitForFunction(() => document.querySelector('#ask-controls').getAttribute('aria-busy') === 'true');

          await runA11yStateAudit(page, {
            stateId: 'state_6_inflight_question', reverseThemes,
          });

          // Dismiss question
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-inflight-a11y-bundle',
            companionKey: '/workspace',
            rows: [{ who: 'agent', label: 'magi', text: '완료' }],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);
        }
      },
      {
        id: 'a11y_state_7_theme_transition_cleanliness',
        name: '고대비 -> 다크 -> 라이트 전환 시 잔류 변수 완전 제거 검증 (§5.8 Item A)',
        run: async (page) => {
          // 1. Inject highContrast
          await injectA11yTheme(page, A11Y_THEMES.highContrast);
          let borders = await page.evaluate(() => ({
            cb: document.documentElement.style.getPropertyValue('--vscode-contrastBorder'),
            bb: document.documentElement.style.getPropertyValue('--vscode-button-border'),
          }));
          assert.equal(borders.cb, '#6fc3df', 'highContrast must define --vscode-contrastBorder');
          assert.equal(borders.bb, '#6fc3df', 'highContrast must define --vscode-button-border');

          // 2. Switch to dark: highContrast-specific borders must be completely wiped
          await injectA11yTheme(page, A11Y_THEMES.dark);
          borders = await page.evaluate(() => ({
            cb: document.documentElement.style.getPropertyValue('--vscode-contrastBorder'),
            bb: document.documentElement.style.getPropertyValue('--vscode-button-border'),
          }));
          assert.equal(borders.cb, '', 'dark theme must not retain --vscode-contrastBorder from highContrast');
          assert.equal(borders.bb, '', 'dark theme must not retain --vscode-button-border from highContrast');

          // 3. Switch to light: borders must still be wiped
          await injectA11yTheme(page, A11Y_THEMES.light);
          borders = await page.evaluate(() => ({
            cb: document.documentElement.style.getPropertyValue('--vscode-contrastBorder'),
            bb: document.documentElement.style.getPropertyValue('--vscode-button-border'),
          }));
          assert.equal(borders.cb, '', 'light theme must not retain --vscode-contrastBorder from highContrast');
          assert.equal(borders.bb, '', 'light theme must not retain --vscode-button-border from highContrast');

          // 4. Reverse sequence: highContrast -> light -> dark
          await injectA11yTheme(page, A11Y_THEMES.highContrast);
          await injectA11yTheme(page, A11Y_THEMES.light);
          borders = await page.evaluate(() => ({
            cb: document.documentElement.style.getPropertyValue('--vscode-contrastBorder'),
            bb: document.documentElement.style.getPropertyValue('--vscode-button-border'),
          }));
          assert.equal(borders.cb, '', 'light theme must not retain --vscode-contrastBorder in reverse sequence');
          assert.equal(borders.bb, '', 'light theme must not retain --vscode-button-border in reverse sequence');

          await injectA11yTheme(page, A11Y_THEMES.dark);
          borders = await page.evaluate(() => ({
            cb: document.documentElement.style.getPropertyValue('--vscode-contrastBorder'),
            bb: document.documentElement.style.getPropertyValue('--vscode-button-border'),
          }));
          assert.equal(borders.cb, '', 'dark theme must not retain --vscode-contrastBorder in reverse sequence');
          assert.equal(borders.bb, '', 'dark theme must not retain --vscode-button-border in reverse sequence');
        }
      },
      {
        id: 'a11y_high_contrast_control_edges',
        name: '고대비에서 채워진 조작의 경계 — axe 가 보지 못하는 것 (§5.8)',
        run: async (page) => {
          // ⚠ **axe 는 이것을 못 본다.** 바로 위 감사 일곱이 고대비에서 위반 0 을 내는 동안, 실측
          // 2026-09-19 에 이 화면의 `Send` 와 선택 버튼들은 **테두리 없는 검정 사각형이 검정 배경 위에**
          // 놓여 있었다. 글자 대비는 흰색 대 검정이라 완벽하고, axe 가 재는 것은 글자 대비다.
          //
          // VS Code 고대비 테마가 `--vscode-button-background` 를 배경과 같은 `#000000` 으로 주는 것은
          // 실수가 아니다 — 그 테마의 약속은 **모든 조작이 `contrastBorder` 로 경계를 그린다**는 것이고,
          // 그래서 색이 아니라 선이 경계를 만든다. 같은 스타일시트가 `.approval-btn`·`.inspect-btn`
          // 두 곳에서는 이미 그 약속을 지켰고, 기본 `button` 규칙만 `border:none` 으로 남아 있었다.
          //
          // 그러니 이 시나리오가 재는 것은 「채워진 조작이 배경과 같은 색이면서 선이 없지는 않은가」다.
          // 글자만 떠 있는 버튼(`#more`, `.jump-btn`)은 배경이 투명해 애초에 채워진 적이 없으므로 제외한다 —
          // 그것들은 색으로 말하고, 여기서 재는 것은 사각형이다.
          await injectA11yTheme(page, A11Y_THEMES.highContrast);
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            rows: [{ who: 'agent', label: 'magi', text: '무엇을 할지 정해 주세요.' }],
            ask: {
              kind: 'question', callId: 'hc1', what: '어느 방식으로 갈까요?',
              options: ['예, 고쳐 주세요', '아니요', '직접 입력'], index: 1, total: 1,
            }
          }));
          await page.waitForSelector('#ask-controls:not([hidden])');

          const vanished = await page.evaluate(() => {
            const bodyBg = getComputedStyle(document.body).backgroundColor;
            const out = [];
            for (const el of document.querySelectorAll('button, textarea, input')) {
              const cs = getComputedStyle(el);
              if (cs.display === 'none' || cs.visibility === 'hidden' || el.closest('[hidden]')) continue;
              const rc = el.getBoundingClientRect();
              if (rc.width === 0 || rc.height === 0) continue;
              const filled = cs.backgroundColor !== 'rgba(0, 0, 0, 0)' && cs.backgroundColor !== 'transparent';
              const edgeless = cs.borderTopStyle === 'none' || cs.borderTopWidth === '0px'
                || cs.borderTopColor === 'rgba(0, 0, 0, 0)' || cs.borderTopColor === 'transparent';
              if (filled && edgeless && cs.backgroundColor === bodyBg) {
                out.push(`${el.id ? '#' + el.id : '.' + String(el.className).split(' ')[0]} "${(el.textContent || '').trim().slice(0, 14)}"`);
              }
            }
            return { bodyBg, out };
          });

          assert.deepEqual(vanished.out, [],
            `고대비에서 배경(${vanished.bodyBg})과 같은 색이면서 테두리가 없는 조작: ${vanished.out.join(', ')}`);
        }
      }
  ];
}

export function createA11yBundle({ reverseThemes = false } = {}) {
  return {
    name: 'a11y',
    description: 'axe-core 접근성 감사 (§5.8)',
    scenarios: createA11yScenarios({ reverseThemes }),
  };
}
