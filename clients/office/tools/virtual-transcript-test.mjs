import assert from 'node:assert/strict';
import http from 'node:http';
import fs from 'node:fs/promises';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const require = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const { chromium } = require('@playwright/test');
const root = fileURLToPath(new URL('../../../', import.meta.url));
const server = http.createServer(async (req, res) => {
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (pathname === '/') { res.setHeader('Content-Type', 'text/html'); res.end('<!doctype html><body></body>'); return; }
  try { res.setHeader('Content-Type', 'text/javascript'); res.end(await fs.readFile(root + pathname)); }
  catch { res.statusCode = 404; res.end(); }
});
await new Promise(r => server.listen(0, '127.0.0.1', r));
const browser = await chromium.launch({ headless: true });
try {
  for (const app of ['word', 'excel', 'powerpoint']) {
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${server.address().port}/`);
    await page.setContent('<div id="scroll" style="height:500px;overflow:auto;overflow-anchor:none"><div id="turns"></div></div>');
    await page.evaluate(async app => {
      const { View } = await import(`/clients/${app}/addin/src/ui/view.js`);
      window.v = Object.create(View.prototype);
      window.historyRows = Array.from({ length: 10000 }, (_, i) => ({ kind: 'assistant', seq: i, text: `history ${i}` }));
      v.keepingEnd(() => v.renderRows(historyRows));
    }, app);
    assert.ok(await page.locator('#turns > .turn').count() <= 250, `${app}: DOM row limit`);
    await page.waitForFunction(() => document.querySelector('#turns').textContent.includes('history 9999'));
    await page.locator('#scroll').evaluate(s => {
      s.dispatchEvent(new WheelEvent('wheel', { deltaY: -100 })); s.scrollTop = 0; s.dispatchEvent(new Event('scroll'));
    });
    await page.waitForFunction(() => document.querySelector('#turns').textContent.includes('history 0'));
    assert.ok(await page.locator('#turns > .turn').count() <= 250);
    await page.evaluate(() => { historyRows.push({kind:'assistant', seq:10000, text:'new tail'}); v.keepingEnd(() => v.renderRows(historyRows)); });
    assert.ok(await page.locator('#scroll').evaluate(s => s.scrollTop < 2));
    await page.locator('#scroll').evaluate(s => { s.scrollTop = s.scrollHeight; s.dispatchEvent(new Event('scroll')); });
    await page.waitForFunction(() => document.querySelector('#turns').textContent.includes('new tail'));
    assert.ok(await page.locator('#turns > .turn').count() <= 250);
    await page.evaluate(() => {
      historyRows[10000] = {kind:'think', messageId:'tail-thought', seq:10000, text:'expanded thought'};
      v.keepingEnd(() => v.renderRows(historyRows));
    });
    const fold = page.locator('#turns details').last();
    await fold.locator('summary').click();
    await page.waitForFunction(() => v.opened?.has('think:tail-thought'));
    await page.locator('#scroll').evaluate(s => {
      s.dispatchEvent(new WheelEvent('wheel', {deltaY:-100})); s.scrollTop=0; s.dispatchEvent(new Event('scroll'));
    });
    await page.waitForFunction(() => document.querySelector('#turns').textContent.includes('history 0'));
    await page.locator('#scroll').evaluate(s => { s.scrollTop=s.scrollHeight; s.dispatchEvent(new Event('scroll')); });
    await fold.waitFor();
    assert.equal(await fold.evaluate(e => e.open), true, `${app}: fold state survives virtualization`);
    await page.evaluate(() => {
      v.boundSession='another-session';
      v.keepingEnd(() => v.renderRows([{kind:'assistant', text:'another conversation'}]));
    });
    assert.equal(await page.locator('#turns > .turn').count(), 1);
    assert.equal(await page.locator('#turns').evaluate(e => e.textContent.includes('history')), false);
    console.log(`PASS ${app}: 10000 rows, bounded DOM, both ends, reading position, fold state, session switch`);
    await page.close();
  }
} finally { await browser.close(); await new Promise(r => server.close(r)); }
