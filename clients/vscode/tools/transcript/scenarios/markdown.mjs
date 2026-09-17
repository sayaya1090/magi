import assert from 'node:assert/strict';
import { createRowsMessage } from '../../transcript-fixtures.mjs';
import { TEST_ORIGIN } from '../environment.mjs';

/**
 * Scenario: markdown_streaming_and_security_contract (§5.8.5, §5.8.6).
 * Verifies markdown rendering, incomplete->completed code streaming, newline counterexamples,
 * 50-line and wide code blocks, nested lists, tables with escaped pipes, safe links,
 * rejection of dangerous links (command, javascript, data), raw HTML/img script isolation,
 * zero external network requests, zero CSP violations, native output open action,
 * and question/answer mode transition with draft preservation.
 */
export const markdownScenario = {
  id: 'markdown_streaming_and_security_contract',
  name: '미완성 코드→완성 답변 연속 스트리밍, 개행 반례, 표/목록, 보안 링크/이미지 차단, 초안 보존, 원문 열기 액션',
  run: async (page) => {
    await page.setViewportSize({ width: 420, height: 700 });

    // Observe all HTTP(S) requests and CSP violations from before first message (§5.8.5.2)
    const observedRequests = [];
    const onRequest = (req) => observedRequests.push(req.url());
    page.on('request', onRequest);

    await page.evaluate(() => {
      window.__cspViolations = [];
      document.addEventListener('securitypolicyviolation', (e) => {
        window.__cspViolations.push({
          blockedURI: e.blockedURI,
          violatedDirective: e.violatedDirective,
        });
      });
    });

    try {
      // 1. Initialize session and general draft in composer
      await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
        session: 's-md-stream',
        rows: [],
        refs: []
      }));
      await page.locator('#say').fill('작업 초안 원문');
      assert.equal(await page.locator('#say').inputValue(), '작업 초안 원문');

      // 2. Stream Phase 1: Incomplete streaming code fence
      await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
        session: 's-md-stream',
        rows: [
          {
            who: 'agent',
            label: 'magi',
            text: '스트리밍 중:\n```ts\nconst x = 1;',
          }
        ],
        refs: []
      }));

      await page.waitForSelector('.row.agent pre code');
      const code1 = await page.locator('.row.agent pre code').textContent();
      assert.equal(code1, 'const x = 1;', 'unclosed streaming fence must render code text');
      assert.equal(await page.locator('#say').inputValue(), '작업 초안 원문', 'general draft must not be mutated');

      // 3. Stream Phase 2: Completed response containing newline counterexamples, tables, lists, links, image syntax, HTML, 50-line code, wide code, and outputId
      const expected50Lines = Array.from({ length: 50 }, (_, i) => `const line${i} = ${i};`).join('\n');
      const completedMd = [
        '# Markdown 테스트 완성 본문',
        '',
        '```javascript',
        expected50Lines,
        '```',
        '',
        '```ts',
        'const wideStatement = "' + 'X'.repeat(250) + '";',
        '```',
        '',
        '- 부모 목록 1',
        '  - 자식 목록 1.1',
        '- 부모 목록 2',
        '',
        '| 항목 | 상세 | 비고 |',
        '| :--- | :---: | ---: |',
        '| 모듈 A | `코드 \\| 파이프` | 정상 |',
        '',
        '[안전 외부 링크](https://example.com)',
        '[안전 메일 링크](mailto:test@example.com)',
        '[안전 상대 링크](./README.md)',
        '[위험 명령 링크](command:workbench.action.reloadWindow)',
        '[위험 자바스크립트 링크](javascript:alert(1))',
        '[위험 데이터 링크](data:text/html,evil)',
        '',
        '<script>alert("xss")</script>',
        '<img src="https://example.com/evil.png" onerror="alert(1)">',
        '',
        '![아키텍처 다이어그램](https://example.com/arch.png)',
      ].join('\n');

      await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
        session: 's-md-stream',
        rows: [
          // Row 0: Counterexample 1 - fence inside blockquote
          { who: 'agent', label: 'magi-quote', text: '> ```ts\n> const x = 1;\n> ```' },
          // Row 1: Counterexample 2 - 4-space indented fence in code
          { who: 'agent', label: 'magi-indent', text: '```ts\nconst x = 1;\n    ```\n' },
          // Row 2: Comprehensive completed markdown with outputId
          { who: 'agent', label: 'magi-full', text: completedMd, outputId: 'out-md-doc-99' },
        ],
        refs: []
      }));

      // Assert Row 0 (blockquote fence):
      await page.waitForSelector('.row:has-text("magi-quote") blockquote pre code');
      const quoteCode = await page.locator('.row:has-text("magi-quote") blockquote pre code').textContent();
      assert.equal(quoteCode, 'const x = 1;', 'fence in quote must strip closing fence newline exactly');

      // Assert Row 1 (4-space fence):
      await page.waitForSelector('.row:has-text("magi-indent") pre code');
      const indentCode = await page.locator('.row:has-text("magi-indent") pre code').textContent();
      assert.equal(indentCode, 'const x = 1;\n    ```\n', '4-space indented fence must be treated as code body preserving trailing newline');

      // Assert Row 2 (comprehensive markdown):
      const fullRow = page.locator('.row:has-text("magi-full")');
      await fullRow.waitFor();

      // Heading:
      assert.equal(await fullRow.locator('h1').textContent(), 'Markdown 테스트 완성 본문');

      // 50-line code block: verbatim comparison of entire text line by line (§5.8.5.3)
      const longCodeText = await fullRow.locator('pre[data-lang="javascript"] code').textContent();
      assert.equal(longCodeText, expected50Lines, '50-line long code block text must match verbatim line by line');

      // Wide code block: horizontal scroll measurement and panel boundary check (§5.8.5.3)
      const widePre = fullRow.locator('pre[data-lang="ts"]');
      const scrollMetrics = await widePre.evaluate((el) => {
        const rect = el.getBoundingClientRect();
        return {
          clientWidth: el.clientWidth,
          scrollWidth: el.scrollWidth,
          rectWidth: rect.width,
          isScrollable: el.scrollWidth > el.clientWidth,
        };
      });
      assert.ok(
        scrollMetrics.isScrollable,
        `pre element must be horizontally scrollable: scrollWidth (${scrollMetrics.scrollWidth}) > clientWidth (${scrollMetrics.clientWidth})`
      );
      assert.ok(
        scrollMetrics.rectWidth > 0 && scrollMetrics.rectWidth <= 420,
        `pre element bounding rect width (${scrollMetrics.rectWidth}) must fit within panel viewport width (420)`
      );

      // Nested list:
      const subListText = await fullRow.locator('ul li ul li').textContent();
      assert.equal(subListText?.trim(), '자식 목록 1.1');

      // Table with escaped pipes:
      const tableHeaders = await fullRow.locator('table thead th').allTextContents();
      assert.deepEqual(tableHeaders.map(s => s.trim()), ['항목', '상세', '비고']);
      const tableCells = await fullRow.locator('table tbody td').allTextContents();
      assert.ok(tableCells.some(c => c.includes('코드 | 파이프')), 'table cell must preserve escaped pipe without corrupting columns');

      // Safe links:
      const safeExternal = fullRow.locator('a[href="https://example.com"]');
      assert.equal(await safeExternal.count(), 1);
      assert.equal(await safeExternal.getAttribute('target'), '_blank');
      assert.equal(await safeExternal.getAttribute('rel'), 'noreferrer noopener');

      const safeMailto = fullRow.locator('a[href="mailto:test@example.com"]');
      assert.equal(await safeMailto.count(), 1);

      const safeRelative = fullRow.locator('a[href="./README.md"]');
      assert.equal(await safeRelative.count(), 1);

      // Dangerous links rejected:
      assert.equal(await fullRow.locator('a[href^="command:"]').count(), 0, 'command: links must never be created as actionable <a>');
      assert.equal(await fullRow.locator('a[href^="javascript:"]').count(), 0, 'javascript: links must never be created as actionable <a>');
      assert.equal(await fullRow.locator('a[href^="data:"]').count(), 0, 'data: links must never be created as actionable <a>');
      const fullText = await fullRow.textContent();
      assert.ok(fullText?.includes('위험 명령 링크'), 'rejected link text must be preserved as inert text');
      assert.ok(fullText?.includes('위험 자바스크립트 링크'));
      assert.ok(fullText?.includes('위험 데이터 링크'));

      // Raw HTML & Images & External Request Isolation:
      assert.equal(await fullRow.locator('script').count(), 0, '<script> tag must not be executed or created in DOM');
      assert.equal(await page.locator('img').count(), 0, '<img> tag must not be created (0 DOM img elements)');
      assert.ok(fullText?.includes('[이미지: 아키텍처 다이어그램]'), 'image must render as safe text representation');

      // Assert zero external network requests and zero CSP violations (§5.8.5.2)
      const externalRequests = observedRequests.filter((u) => !u.startsWith(TEST_ORIGIN));
      assert.deepEqual(externalRequests, [], 'Zero external HTTP(S) requests must be made by markdown rendering');

      const cspViolations = await page.evaluate(() => window.__cspViolations);
      assert.deepEqual(cspViolations, [], 'Zero CSP security policy violations occurred during markdown rendering');

      // Native output open action:
      const openOutputBtn = fullRow.locator('.output-open-btn[data-output-id="out-md-doc-99"]');
      assert.equal(await openOutputBtn.count(), 1);
      await openOutputBtn.click();
      const postedMsgs = await page.evaluate(() => window.__posted);
      const outputMsg = postedMsgs.filter((m) => m.kind === 'output' && m.outputId === 'out-md-doc-99').slice(-1)[0];
      assert.ok(outputMsg, 'openOutput action must post output message to host');
      assert.equal(outputMsg.session, 's-md-stream');
      assert.equal(outputMsg.outputId, 'out-md-doc-99');

      // 4. Question & Answer Mode Transition with draft isolation:
      await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
        session: 's-md-stream',
        rows: [
          { who: 'agent', label: 'magi-full', text: completedMd, outputId: 'out-md-doc-99' },
        ],
        ask: {
          kind: 'question',
          callId: 'q-md-choice-1',
          what: '다음 단계를 선택해주세요',
          options: ['1. 빌드', '2. 배포', '3. 취소'],
        },
        refs: []
      }));

      await page.waitForSelector('#ask-controls button:text("직접 입력")');
      await page.locator('#ask-controls button:text("직접 입력")').click();
      assert.equal(await page.locator('#reply-mode').isVisible(), true, 'reply mode opened via 직접 입력');

      // Type answer draft:
      await page.locator('#say').fill('답변 작성 중 초안');
      assert.equal(await page.locator('#say').inputValue(), '답변 작성 중 초안');

      // New dialogue update arrives while in answer mode:
      await page.evaluate((msg) => window.postMessage(msg, '*'), createRowsMessage({
        session: 's-md-stream',
        rows: [
          { who: 'agent', label: 'magi-full', text: completedMd, outputId: 'out-md-doc-99' },
          { who: 'council', label: 'Council', text: '합의 완료: 다음 작업을 진행하세요.' },
        ],
        ask: {
          kind: 'question',
          callId: 'q-md-choice-1',
          what: '다음 단계를 선택해주세요',
          options: ['1. 빌드', '2. 배포', '3. 취소'],
        },
        refs: []
      }));

      // Answer mode and draft must be preserved:
      assert.equal(await page.locator('#reply-mode').isVisible(), true, 'reply mode must remain active after dialogue update');
      assert.equal(await page.locator('#say').inputValue(), '답변 작성 중 초안', 'answer draft must be preserved after dialogue update');

      // Cancel answer mode via Escape:
      await page.keyboard.press('Escape');
      assert.equal(await page.locator('#reply-mode').isVisible(), false, 'reply mode exited on Escape');
      assert.equal(await page.locator('#say').inputValue(), '작업 초안 원문', 'general draft must be restored on Escape');
    } finally {
      page.off('request', onRequest);
    }
  }
};
