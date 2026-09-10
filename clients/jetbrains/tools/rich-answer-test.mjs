// Run after npm ci in clients/web/e2e: node clients/jetbrains/tools/rich-answer-test.mjs
import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
const require = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const { chromium } = require('playwright');
const source = await readFile(new URL('../plugin/intellij/src/main/kotlin/dev/sayaya/magi/ide/ui/RichAnswer.kt', import.meta.url), 'utf8');
const match = source.match(/val js = """([\s\S]*?)"""\.trimIndent\(\)/);
assert.ok(match, 'production measurement script must exist');
const js = match[1].replace(/\$\{query\.inject\("([^"\n]*)"\)\}/g, (_, expression) => `window.reports.push(${expression});`);
assert.ok(!js.includes('${'), 'all JVM bridge calls must be replaced');
const browser = await chromium.launch({headless: true});
try {
  const page = await browser.newPage({viewport: {width: 500, height: 400}});
  await page.setContent('<style>body{font:16px/24px sans-serif}pre{max-height:200px;overflow:auto}</style><pre>' + 'line\n'.repeat(300) + '</pre>');
  await page.evaluate(() => { window.reports = []; });
  await page.evaluate(js);
  await page.waitForFunction(() => window.reports.some(x => Number(x) > 7000));
  const height = await page.evaluate(() => Number(window.reports.at(-1)));
  await page.setViewportSize({width: 500, height: height + 3});
  await page.waitForTimeout(150);
  assert.equal(await page.evaluate(() => Number(window.reports.at(-1))), height, 'resizing holder must not feed back into content height');
  await page.evaluate(() => { document.body.innerHTML = '<p>short reply</p>'; });
  await page.waitForTimeout(100);
  await page.waitForFunction(() => Number(window.reports.at(-1)) < 100);
  await page.setViewportSize({width: 500, height: 400});
  await page.evaluate(() => { document.body.innerHTML = '<p>' + 'wrapped words '.repeat(100) + '</p>'; });
  await page.waitForFunction(() => Number(window.reports.at(-1)) > 100);
  const wide = await page.evaluate(() => Number(window.reports.at(-1)));
  await page.setViewportSize({width: 250, height: 400});
  await page.waitForFunction(w => Number(window.reports.at(-1)) > w, wide);
  await page.evaluate(() => { const image = document.createElement('div'); image.style.height = '9000px'; document.body.appendChild(image); });
  await page.waitForFunction(() => Number(window.reports.at(-1)) > 9000);
  const canceled = await page.evaluate(() => !document.body.dispatchEvent(new WheelEvent('wheel', {deltaY: 120, cancelable: true, bubbles: true})));
  assert.ok(canceled, 'inner browser must not consume vertical scrolling');
  assert.equal(await page.evaluate(() => window.reports.at(-1)), 'wheel:120');
  console.log('PASS: long reply, stable sizing, shrink, wrapping, late content, wheel forwarding');
} finally { await browser.close(); }
