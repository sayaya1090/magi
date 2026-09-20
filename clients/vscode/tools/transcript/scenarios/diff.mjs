import assert from 'node:assert/strict';
import { createRowsMessage } from '../../transcript-fixtures.mjs';
import { measureActiveButtonRing } from '../dom-helpers.mjs';


/*
 * 승인 단추는 **접근 이름**으로 잡는다.
 *
 * 표시와 전송 토큰이 갈려 있다(§6.7): 사람이 보고 낭독기가 읽는 이름은 한국어(허용·거절·항상 허용)
 * 이고, 실제로 보내는 값은 그대로 allow/deny/always 다. 그래서 이 파일은 **이름으로 잡고 토큰은
 * 따로 단언한다** — 글자로 잡으면 표시를 바꿀 때마다 시험이 깨지고(실제로 깨졌다), 클래스로만
 * 잡으면 사람이 읽는 이름이 틀려도 초록이다. 둘 다 재야 한다.
 *
 * ⚠ `exact` 가 필요하다: '허용' 은 '항상 허용' 의 부분 문자열이라, 없으면 둘이 같이 잡힌다.
 */
const APPROVAL_LABEL = { allow: '허용', deny: '거절', always: '항상 허용' };
const approvalBtn = (page, decision, scope = '#ask-controls') =>
  page.locator(scope).getByRole('button', { name: APPROVAL_LABEL[decision], exact: true });

export const diffScenarios = [
      {
        id: 'approval_labels_are_korean_tokens_are_not',
        name: '승인 단추: 표시·접근 이름은 한국어, 보내는 값은 allow/deny/always 그대로, 각 1회',
        run: async (page) => {
          /* §6.7 에서 표시와 전송 토큰을 갈랐다. 그 둘이 **따로** 맞는지 여기서 잰다 — 하나만 재면
             나머지 하나가 틀려도 초록이다. 그리고 셋을 **독립 요청**으로 밟는다: 한 요청에서 셋을
             누르면 두 번째부터는 이미 답한 물음이라, 「각 단추가 제 값을 한 번 보낸다」를 못 잰다. */
          for (const [decision, label] of [['allow', '허용'], ['deny', '거절'], ['always', '항상 허용']]) {
            const callId = `perm-label-${decision}`;
            const filePath = `label-${decision}.txt`;
            await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
              session: 'sess-approval-labels',
              rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
              ask: {
                kind: 'permission',
                callId,
                what: 'edit',
                filePath,
                args: JSON.stringify({ path: filePath, old: 'x', new: 'y' })
              }
            }));
            /* 이 요청이 화면에 선 것을 **이 요청의 파일 이름으로** 기다린다. 단추 수만 세면 앞
               요청의 단추 셋을 보고 지나가 버리고, 그러면 다음 단언은 지난 물음을 재게 된다. */
            await page.waitForFunction(
              (name) => document.querySelector('#ask-controls .summary-text')?.textContent?.includes(name) === true,
              filePath);

            const btn = approvalBtn(page, decision, '#ask-controls .acts');
            assert.equal(await btn.count(), 1, `exactly one button is named ${label}`);
            assert.equal((await btn.textContent()).trim(), label, `${decision} button is drawn as ${label}`);
            assert.equal(await btn.getAttribute('aria-label'), label,
              `${decision} button is read as ${label} — drawn and announced must agree`);
            // 클래스는 토큰을 그대로 들고 있어야 한다: 표시가 바뀌어도 보내는 값은 안 바뀐다.
            assert.ok((await btn.getAttribute('class')).includes(`decision-${decision}`),
              `${label} still carries the ${decision} token in its class`);

            const before = await page.evaluate(() => window.__posted.length);
            await btn.click();
            const posted = await page.evaluate(() => window.__posted);
            const mine = posted.filter((m) => m.kind === 'answer' && m.callId === callId);
            assert.equal(posted.length, before + 1, `${label} posts exactly one message`);
            assert.equal(mine.length, 1, `${label} answers its own request exactly once`);
            assert.equal(mine[0].decision, decision,
              `${label} sends the token ${decision} — the display changed, the wire value did not`);
          }
        }
      },
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
          await approvalBtn(page, 'allow').click();
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

          await approvalBtn(page, 'allow').click();
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

          await approvalBtn(page, 'allow').click();
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
          assert.deepEqual(actsBtns, ['변경 보기', '허용', '거절', '항상 허용'], 'actions include 변경 보기 and approval buttons, labelled in Korean');
          // 사람이 읽는 이름도 같아야 한다 — 보이는 것과 낭독되는 것이 다른 조작은 그 자체가 결함이다.
          const actsNames = await page.locator('#ask-controls .acts button').evaluateAll(
            (els) => els.map((e) => e.getAttribute('aria-label') || ''));
          assert.deepEqual(actsNames, ['변경 보기 (승인 당시 비교 자료)', '허용', '거절', '항상 허용'],
            'accessible names match what is drawn');

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
          assert.deepEqual(rejectedBtns, ['허용', '거절', '항상 허용'], 'no phantom 변경 보기 button when host flags diffKind as none');
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
      },
      {
        id: 'output_open_button_session_binding_and_action_dispatch',
        name: '편집창에서 열기 버튼 세션 바인딩, 액션 전송 및 미확인 세션 비활성화 (§3.3, §3.4)',
        run: async (page) => {
          // 1. Deliver confirmed session with assistant row (with outputId), tool row (with outputId), and draft row (no outputId)
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-output-bound',
            rows: [
              { who: 'agent', label: 'magi', text: 'finalized assistant answer', outputId: 'assistant:42' },
              { who: 'agent', label: 'magi', text: 'draft streaming answer', pending: true },
              { who: 'tool', label: 'tool (read_file)', text: 'read_file', callId: 'call-read-1', outputId: 'tool:call-read-1:45' }
            ],
            refs: []
          }));

          await page.waitForSelector('.row.agent .output-open-btn');
          const agentBtn = page.locator('.row.agent .output-open-btn');
          assert.equal(await agentBtn.count(), 1, 'only finalized assistant row has output open button');
          assert.equal(await agentBtn.textContent(), '편집창에서 열기');
          assert.equal(await agentBtn.isDisabled(), false, 'button is enabled when session is confirmed');

          await page.waitForSelector('.row.tool .output-open-btn');
          const toolBtn = page.locator('.row.tool .output-open-btn');
          assert.equal(await toolBtn.count(), 1, 'tool row with outputId has output open button');
          assert.equal(await toolBtn.textContent(), '편집창에서 열기');
          assert.equal(await toolBtn.isDisabled(), false);

          // Click assistant button: verify postMessage sends kind: 'output' with exact session and outputId
          const postedLenBefore = await page.evaluate(() => window.__posted.length);
          await agentBtn.click();
          const postedAfterAgent = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterAgent.length, postedLenBefore + 1, 'exactly one message dispatched');
          const agentMsg = postedAfterAgent[postedAfterAgent.length - 1];
          assert.deepEqual(agentMsg, {
            kind: 'output',
            session: 'sess-output-bound',
            outputId: 'assistant:42'
          }, 'correct output action payload dispatched for assistant');

          // Click tool button: verify postMessage sends kind: 'output' with tool outputId
          await toolBtn.click();
          const postedAfterTool = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterTool.length, postedLenBefore + 2, 'second message dispatched');
          const toolMsg = postedAfterTool[postedAfterTool.length - 1];
          assert.deepEqual(toolMsg, {
            kind: 'output',
            session: 'sess-output-bound',
            outputId: 'tool:call-read-1:45'
          }, 'correct output action payload dispatched for tool');

          // Confirm neither click touched composer or ask mode
          const sayValue = await page.locator('#say').inputValue();
          assert.equal(sayValue, '', 'composer input untouched');
          const askControlsHidden = await page.locator('#ask-controls').evaluate((el) => el.hidden);
          assert.ok(askControlsHidden, 'ask controls remain hidden');

          // 2. Deliver unconfirmed session (session: '') with outputId
          await page.evaluate(() => window.postMessage({
            kind: 'rows',
            session: '',
            rows: [
              { who: 'agent', label: 'magi', text: 'unconfirmed assistant', outputId: 'assistant:99' }
            ],
            refs: []
          }, '*'));

          await page.waitForSelector('.row.agent .output-open-btn:disabled');
          const disabledBtn = page.locator('.row.agent .output-open-btn');
          assert.equal(await disabledBtn.isDisabled(), true, 'output button is disabled when session is unconfirmed');
        }
      },
      {
        id: 'diff_approval_and_inspection_styling_keyboard_and_themes',
        name: '승인 패널 조회 보조 조작 스타일, 승인 분리, 키보드 접근 및 테마/뷰포트 가림 검증 (§5.6)',
        run: async (page) => {
          // 1. Deliver permission ask with diff and file target
          const permAsk = {
            kind: 'permission',
            callId: 'perm-inspect-56',
            diffKind: 'sides',
            what: 'edit src/auth.ts',
            filePath: 'src/auth.ts',
            args: JSON.stringify({ path: 'src/auth.ts', old: 'var token = ""', new: 'const token = "secure"' }),
            reason: 'harden auth token security',
            diff: '--- a/src/auth.ts\n+++ b/src/auth.ts\n@@ -1 +1 @@\n-var token = ""\n+const token = "secure"\n'
          };
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [{ who: 'agent', label: 'magi', text: '승인 요청' }],
            ask: permAsk
          }));

          await page.waitForSelector('#ask-controls .acts');
          const diffBtn = page.locator('#ask-controls .acts button.diff-btn');
          await diffBtn.waitFor();
          const openBtn = page.locator('#ask-body .file-target button.file-nav-btn');
          await openBtn.waitFor();

          const allowBtn = approvalBtn(page, 'allow', '#ask-controls .acts');
          const denyBtn = approvalBtn(page, 'deny', '#ask-controls .acts');
          const alwaysBtn = approvalBtn(page, 'always', '#ask-controls .acts');

          // 2. Assert inspection vs approval classes
          assert.equal(await diffBtn.evaluate((el) => el.classList.contains('inspect-btn')), true, 'diff button has inspect-btn class');
          assert.equal(await diffBtn.evaluate((el) => el.classList.contains('approval-btn')), false, 'diff button does not have approval-btn class');
          assert.equal(await openBtn.evaluate((el) => el.classList.contains('inspect-btn')), true, 'file nav button has inspect-btn class');
          assert.equal(await openBtn.evaluate((el) => el.classList.contains('approval-btn')), false, 'file nav button does not have approval-btn class');

          for (const btn of [allowBtn, denyBtn, alwaysBtn]) {
            assert.equal(await btn.evaluate((el) => el.classList.contains('approval-btn')), true, 'decision button has approval-btn class');
            assert.equal(await btn.evaluate((el) => el.classList.contains('inspect-btn')), false, 'decision button does not have inspect-btn class');
          }

          // 3. Inspection clicks dispatch open / diff only (0 answer, 0 reply, 0 say)
          const postedBefore = await page.evaluate(() => window.__posted.length);
          await openBtn.click();
          await diffBtn.click();
          const postedAfterInspect = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterInspect.length, postedBefore + 2, 'inspection clicks dispatched 2 messages');
          assert.deepEqual(postedAfterInspect[postedBefore], { kind: 'open', session: 'sess-perm-56', callId: 'perm-inspect-56' });
          assert.deepEqual(postedAfterInspect[postedBefore + 1], { kind: 'diff', session: 'sess-perm-56', callId: 'perm-inspect-56' });
          assert.ok(!postedAfterInspect.slice(postedBefore).some((m) => m.kind === 'answer' || m.kind === 'reply' || m.kind === 'say'), 'inspection clicks never answer/reply/say');

          // 4. Sequential Keyboard Tab / Shift+Tab navigation, disabled skip, and focus ring boundary
          const jumpBtn = page.locator('#ask-controls button.jump-btn');
          await jumpBtn.waitFor();

          // 4A. Sequential Tab navigation chain: openBtn -> jumpBtn -> diffBtn -> allow -> deny -> always -> #say -> #send
          await openBtn.focus();
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-body .file-target button.file-nav-btn')), true, 'openBtn has initial focus');

          await page.keyboard.press('Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls button.jump-btn')), true, 'Tab lands on jumpBtn');

          await page.keyboard.press('Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.diff-btn')), true, 'Tab lands on diffBtn');

          await page.keyboard.press('Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.decision-allow')), true, 'Tab lands on allowBtn');

          await page.keyboard.press('Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.decision-deny')), true, 'Tab lands on denyBtn');

          await page.keyboard.press('Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.decision-always')), true, 'Tab lands on alwaysBtn');

          await page.keyboard.press('Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#say')), true, 'Tab lands on composer #say');

          await page.keyboard.press('Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#send')), true, 'Tab lands on #send');

          // 4B. Reverse Shift+Tab navigation chain: #send -> #say -> always -> deny -> allow -> diffBtn -> jumpBtn -> openBtn
          await page.keyboard.press('Shift+Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#say')), true, 'Shift+Tab lands back on #say');

          await page.keyboard.press('Shift+Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.decision-always')), true, 'Shift+Tab lands back on alwaysBtn');

          await page.keyboard.press('Shift+Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.decision-deny')), true, 'Shift+Tab lands back on denyBtn');

          await page.keyboard.press('Shift+Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.decision-allow')), true, 'Shift+Tab lands back on allowBtn');

          await page.keyboard.press('Shift+Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.diff-btn')), true, 'Shift+Tab lands back on diffBtn');

          await page.keyboard.press('Shift+Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls button.jump-btn')), true, 'Shift+Tab lands back on jumpBtn');

          await page.keyboard.press('Shift+Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-body .file-target button.file-nav-btn')), true, 'Shift+Tab lands back on openBtn');

          // 4C. Disabled diffBtn exclusion from Tab order
          await page.evaluate(() => {
            const d = document.querySelector('#ask-controls .acts button.diff-btn');
            d.disabled = true;
          });
          await jumpBtn.focus();
          await page.keyboard.press('Tab');
          assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls .acts button.decision-allow')), true, 'Tab from jumpBtn skips disabled diffBtn directly to allowBtn');
          await page.evaluate(() => {
            const d = document.querySelector('#ask-controls .acts button.diff-btn');
            d.disabled = false;
          });

          // 4D. Keyboard activation: Enter on openBtn, Enter on diffBtn, Space on allowBtn
          await openBtn.focus();
          await page.keyboard.press('Enter');
          const postedAfterKbOpen = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterKbOpen[postedAfterKbOpen.length - 1].kind, 'open');

          await diffBtn.focus();
          await page.keyboard.press('Enter');
          const postedAfterKbDiff = await page.evaluate(() => window.__posted);
          assert.equal(postedAfterKbDiff[postedAfterKbDiff.length - 1].kind, 'diff');

          await allowBtn.focus();
          await page.keyboard.press('Space');
          const postedAfterAllow = await page.evaluate(() => window.__posted);
          const allowMsg = postedAfterAllow[postedAfterAllow.length - 1];
          assert.deepEqual(allowMsg, { kind: 'answer', callId: 'perm-inspect-56', decision: 'allow' });

          // 4D. Deliver next ask and test deny with Enter key
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [{ who: 'agent', label: 'magi', text: '두 번째 승인 요청' }],
            ask: { ...permAsk, callId: 'perm-inspect-57' }
          }));
          await page.waitForSelector('#ask-controls .acts');
          const denyBtn2 = approvalBtn(page, 'deny', '#ask-controls .acts');
          await denyBtn2.focus();
          await page.keyboard.press('Enter');
          const postedAfterDeny = await page.evaluate(() => window.__posted);
          const denyMsg = postedAfterDeny[postedAfterDeny.length - 1];
          assert.deepEqual(denyMsg, { kind: 'answer', callId: 'perm-inspect-57', decision: 'deny' });

          // 4E. Deliver third ask and test always with Space key
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [{ who: 'agent', label: 'magi', text: '세 번째 승인 요청' }],
            ask: { ...permAsk, callId: 'perm-inspect-58' }
          }));
          await page.waitForSelector('#ask-controls .acts');
          const alwaysBtn3 = approvalBtn(page, 'always', '#ask-controls .acts');
          await alwaysBtn3.focus();
          await page.keyboard.press('Space');
          const postedAfterAlways = await page.evaluate(() => window.__posted);
          const alwaysMsg = postedAfterAlways[postedAfterAlways.length - 1];
          assert.deepEqual(alwaysMsg, { kind: 'answer', callId: 'perm-inspect-58', decision: 'always' });

          // Deliver final ask for theme & viewport inspection
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [{ who: 'agent', label: 'magi', text: '테마 측정 승인 요청' }],
            ask: { ...permAsk, callId: 'perm-inspect-59' }
          }));
          await page.waitForSelector('#ask-controls .acts');
          const activeDiff = page.locator('#ask-controls .acts button.diff-btn');
          const activeAllow = approvalBtn(page, 'allow', '#ask-controls .acts');
          const activeDeny = approvalBtn(page, 'deny', '#ask-controls .acts');
          const activeAlways = approvalBtn(page, 'always', '#ask-controls .acts');

          // 5. Theme tokens and full 4-direction focus ring boundary verification across Theme × Viewport matrix
          await page.mouse.move(0, 0);

          const themeConfigs = [
            {
              name: 'dark',
              apply: async () => {
                await page.evaluate(() => {
                  document.documentElement.style.setProperty('--vscode-button-background', '#0e639c');
                  document.documentElement.style.setProperty('--vscode-button-foreground', '#ffffff');
                  document.documentElement.style.setProperty('--vscode-button-secondaryBackground', '#3a3d41');
                  document.documentElement.style.setProperty('--vscode-button-secondaryHoverBackground', '#45494e');
                  document.documentElement.style.setProperty('--vscode-button-secondaryForeground', '#ffffff');
                  document.documentElement.style.setProperty('--vscode-focusBorder', '#007fd4');
                  document.documentElement.style.removeProperty('--vscode-contrastBorder');
                });
              },
              expectedDiffBg: 'rgb(58, 61, 65)',
              expectedAllowBg: 'rgb(14, 99, 156)',
            },
            {
              name: 'light',
              apply: async () => {
                await page.evaluate(() => {
                  document.documentElement.style.setProperty('--vscode-button-background', '#005fb8');
                  document.documentElement.style.setProperty('--vscode-button-foreground', '#ffffff');
                  document.documentElement.style.setProperty('--vscode-button-secondaryBackground', '#e5e5e5');
                  document.documentElement.style.setProperty('--vscode-button-secondaryHoverBackground', '#d0d0d0');
                  document.documentElement.style.setProperty('--vscode-button-secondaryForeground', '#3b3b3b');
                  document.documentElement.style.setProperty('--vscode-focusBorder', '#005fb8');
                  document.documentElement.style.removeProperty('--vscode-contrastBorder');
                });
              },
              expectedDiffBg: 'rgb(229, 229, 229)',
              expectedAllowBg: 'rgb(0, 95, 184)',
            },
            {
              name: 'hc',
              apply: async () => {
                await page.evaluate(() => {
                  document.documentElement.style.setProperty('--vscode-button-background', '#000000');
                  document.documentElement.style.setProperty('--vscode-button-foreground', '#ffffff');
                  document.documentElement.style.setProperty('--vscode-button-secondaryBackground', '#000000');
                  document.documentElement.style.setProperty('--vscode-button-secondaryHoverBackground', '#000000');
                  document.documentElement.style.setProperty('--vscode-button-secondaryForeground', '#ffffff');
                  document.documentElement.style.setProperty('--vscode-contrastBorder', '#6fc1ff');
                  document.documentElement.style.setProperty('--vscode-focusBorder', '#007fd4');
                });
              },
              expectedBorder: 'rgb(111, 193, 255)',
            },
          ];

          const viewports = [[320, 600], [420, 700]];
          const targets = [
            { name: 'diff', selector: '#ask-controls .acts button.diff-btn' },
            { name: 'allow', selector: '#ask-controls .acts button.decision-allow' },
            { name: 'deny', selector: '#ask-controls .acts button.decision-deny' },
            { name: 'always', selector: '#ask-controls .acts button.decision-always' },
          ];

          for (const theme of themeConfigs) {
            await theme.apply();

            // Verify theme colors on secondary inspection vs primary approval buttons
            const diffEl = page.locator('#ask-controls .acts button.diff-btn');
            const allowEl = page.locator('#ask-controls .acts button.decision-allow');
            const denyEl = page.locator('#ask-controls .acts button.decision-deny');
            const alwaysEl = page.locator('#ask-controls .acts button.decision-always');

            if (theme.name === 'dark' || theme.name === 'light') {
              const diffBg = await diffEl.evaluate((el) => window.getComputedStyle(el).backgroundColor);
              const allowBg = await allowEl.evaluate((el) => window.getComputedStyle(el).backgroundColor);
              const denyBg = await denyEl.evaluate((el) => window.getComputedStyle(el).backgroundColor);
              const alwaysBg = await alwaysEl.evaluate((el) => window.getComputedStyle(el).backgroundColor);

              assert.equal(diffBg, theme.expectedDiffBg, `${theme.name} diff secondary background matches token`);
              assert.equal(allowBg, theme.expectedAllowBg, `${theme.name} allow primary background matches token`);
              assert.equal(denyBg, theme.expectedAllowBg, `${theme.name} deny has same primary background as allow (neutral, no danger red)`);
              assert.equal(alwaysBg, theme.expectedAllowBg, `${theme.name} always has same primary background as allow`);
            } else if (theme.name === 'hc') {
              const diffBorder = await diffEl.evaluate((el) => window.getComputedStyle(el).borderColor);
              const allowBorder = await allowEl.evaluate((el) => window.getComputedStyle(el).borderColor);
              assert.equal(diffBorder, theme.expectedBorder, 'hc diff button has contrastBorder');
              assert.equal(allowBorder, theme.expectedBorder, 'hc allow button has contrastBorder');
            }

            for (const [vpW, vpH] of viewports) {
              await page.setViewportSize({ width: vpW, height: vpH });

              const controlsRect = await page.locator('#ask-controls').evaluate((el) => {
                const r = el.getBoundingClientRect();
                return { width: r.width, height: r.height, right: r.right, bottom: r.bottom };
              });
              assert.ok(controlsRect.width > 0 && controlsRect.height > 0, `[${theme.name} ${vpW}x${vpH}] ask controls is visible`);
              assert.ok(controlsRect.right <= vpW, `[${theme.name} ${vpW}x${vpH}] ask controls does not overflow right`);
              assert.ok(controlsRect.bottom <= vpH, `[${theme.name} ${vpW}x${vpH}] ask controls does not overflow bottom`);

              // Start from jumpBtn right before .acts to Tab into the container
              await jumpBtn.focus();
              assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('#ask-controls button.jump-btn')), true, 'jumpBtn has focus before entering .acts');

              // Tab through each target button inside .acts and measure focus ring
              for (const target of targets) {
                await page.keyboard.press('Tab');
                const m = await measureActiveButtonRing(page, target.selector);

                assert.equal(m.isActive, true, `[${theme.name} ${vpW}x${vpH}] ${target.name} must be document.activeElement after Tab`);
                assert.equal(m.isFocusVisible, true, `[${theme.name} ${vpW}x${vpH}] ${target.name} must match :focus-visible`);
                assert.equal(m.outlineStyle, 'solid', `[${theme.name} ${vpW}x${vpH}] ${target.name} outlineStyle must be solid (got ${m.outlineStyle})`);
                assert.equal(m.outlineWidth, 1, `[${theme.name} ${vpW}x${vpH}] ${target.name} outlineWidth must be exactly 1px (got ${m.outlineWidth})`);
                assert.equal(m.outlineOffset, 2, `[${theme.name} ${vpW}x${vpH}] ${target.name} outlineOffset must be exactly 2px (got ${m.outlineOffset})`);
                assert.equal(m.ringSpan, 3, `[${theme.name} ${vpW}x${vpH}] ${target.name} ringSpan must be 3px (got ${m.ringSpan})`);

                assert.equal(m.clippedTop, false, `[${theme.name} ${vpW}x${vpH}] ${target.name} top ring clipped by .acts (topMargin: ${m.topMargin}px vs ring: ${m.ringSpan}px)`);
                assert.equal(m.clippedBottom, false, `[${theme.name} ${vpW}x${vpH}] ${target.name} bottom ring clipped by .acts (bottomMargin: ${m.bottomMargin}px vs ring: ${m.ringSpan}px)`);
                assert.equal(m.clippedLeft, false, `[${theme.name} ${vpW}x${vpH}] ${target.name} left ring clipped by .acts (leftMargin: ${m.leftMargin}px vs ring: ${m.ringSpan}px)`);
                assert.equal(m.clippedRight, false, `[${theme.name} ${vpW}x${vpH}] ${target.name} right ring clipped by .acts (rightMargin: ${m.rightMargin}px vs ring: ${m.ringSpan}px)`);
              }
            }
          }

          // 6. Wrapped rows and scrollable .acts Tab focus auto-scroll ring verification
          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            rows: [],
            ask: null
          }));
          await page.waitForFunction(() => document.getElementById('ask-controls').hidden);

          await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
            session: 'sess-perm-56',
            ask: {
              kind: 'question',
              callId: 'q-multi-wrap',
              what: '다중 선택지 줄바꿈 및 스크롤 링 검증',
              options: [
                '선택지 1번 아주 긴 텍스트 항목',
                '선택지 2번 긴 텍스트 항목',
                '선택지 3번 긴 텍스트 항목',
                '선택지 4번 긴 텍스트 항목',
                '선택지 5번 긴 텍스트 항목',
                '선택지 6번 긴 텍스트 항목',
                '선택지 7번 긴 텍스트 항목',
                '선택지 8번 긴 텍스트 항목',
              ]
            }
          }));
          await page.waitForSelector('#ask-controls .acts button');
          await page.waitForFunction(() => document.querySelectorAll('#ask-controls .acts button').length === 9);
          await page.setViewportSize({ width: 320, height: 600 });

          const scrollMetrics = await page.evaluate(() => {
            const acts = document.querySelector('#ask-controls .acts');
            return {
              scrollHeight: acts.scrollHeight,
              clientHeight: acts.clientHeight,
              hasScroll: acts.scrollHeight > acts.clientHeight,
            };
          });
          assert.ok(scrollMetrics.hasScroll, '8 options in 320x600 must trigger overflow-y: auto in .acts');

          // Tab through from jumpBtn into first option through the last button ('직접 입력')
          await page.locator('#ask-controls button.jump-btn').focus();
          const optionCount = 8 + 1; // 8 choices + 1 직접 입력 button

          for (let i = 0; i < optionCount; i++) {
            await page.keyboard.press('Tab');
            const m = await measureActiveButtonRing(page, ':focus');

            assert.equal(m.isActive, true, `option button ${i + 1} (${m.text}) must be activeElement after Tab`);
            assert.equal(m.isFocusVisible, true, `option button ${i + 1} (${m.text}) must match :focus-visible`);

            // Distinguish generic button outline (browser default, e.g. auto/1px/0px) from approval explicit 1px+2px
            assert.ok(m.ringSpan >= 1, `option button ${i + 1} focus ringSpan >= 1px (got ${m.ringSpan})`);

            assert.equal(m.clippedTop, false, `option button ${i + 1} (${m.text}) top ring must not be clipped after auto-scroll (topMargin: ${m.topMargin}px vs ring: ${m.ringSpan}px)`);
            assert.equal(m.clippedBottom, false, `option button ${i + 1} (${m.text}) bottom ring must not be clipped after auto-scroll (bottomMargin: ${m.bottomMargin}px vs ring: ${m.ringSpan}px)`);
            assert.equal(m.clippedLeft, false, `option button ${i + 1} (${m.text}) left ring must not be clipped (leftMargin: ${m.leftMargin}px vs ring: ${m.ringSpan}px)`);
            assert.equal(m.clippedRight, false, `option button ${i + 1} (${m.text}) right ring must not be clipped (rightMargin: ${m.rightMargin}px vs ring: ${m.ringSpan}px)`);
          }

          // Clean up styles and restore default viewport
          await page.evaluate(() => {
            document.documentElement.style.removeProperty('--vscode-contrastBorder');
            document.documentElement.style.removeProperty('--vscode-button-background');
            document.documentElement.style.removeProperty('--vscode-button-foreground');
            document.documentElement.style.removeProperty('--vscode-button-secondaryBackground');
            document.documentElement.style.removeProperty('--vscode-button-secondaryHoverBackground');
            document.documentElement.style.removeProperty('--vscode-button-secondaryForeground');
            document.documentElement.style.removeProperty('--vscode-focusBorder');
          });
          await page.setViewportSize({ width: 420, height: 700 });
        }
      }
];

export const diffBundle = {
  name: 'diff',
  description: 'diff·파일 이동',
  scenarios: diffScenarios,
};
