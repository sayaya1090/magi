import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';

const require = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const { chromium } = require('playwright');
const source = await readFile(new URL('../plugin/intellij/src/main/kotlin/dev/sayaya/magi/ide/ui/RichAnswer.kt', import.meta.url), 'utf8');
// Execute the production injection. Only the native telemetry/wheel bridge is stubbed.
const script = source.split('val js = """')[1]?.split('""".trimIndent()')[0]
  .replace(/\$\{query\.inject\([^\n]+\)\}/g, 'void 0');
assert.ok(script && !script.includes('${query.'), 'production script extraction failed');
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.setContent('<html><head></head><body></body></html>');
  await page.evaluate(script);
  // The IDE preview first loads an empty page, then inserts/replaces Markdown later.
  for (let i = 0; i < 2; i++) {
    await page.evaluate(() => {
      document.body.style.setProperty('background', 'white', 'important');
      document.body.innerHTML = '<main style="background:white!important"><div class="markdown-body" style="background:white!important"><p>report</p><table><tr><td>PASS</td></tr></table></div></main>';
    });
    await page.waitForFunction(() => [document.documentElement, document.body,
      document.querySelector('main'), document.querySelector('.markdown-body')]
      .every(e => getComputedStyle(e).backgroundColor === 'rgba(0, 0, 0, 0)'));
    assert.equal(await page.locator('td').textContent(), 'PASS');
  }
  console.log('PASS: delayed and replaced Markdown roots keep transparent backgrounds');
} finally {
  await browser.close();
}
