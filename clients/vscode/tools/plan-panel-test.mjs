import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const Module = require('module');
const originalLoad = Module._load;
let language = 'ko';
Module._load = function (id, ...args) {
  if (id === 'vscode') return { env: { get language() { return language; } } };
  return originalLoad.call(this, id, ...args);
};
const { Plan } = require('../out/ide/plan');
Module._load = originalLoad;
const { chromium } = createRequire(new URL('../../web/e2e/package.json', import.meta.url))('@playwright/test');
const browser = await chromium.launch({ headless: true });
try {
  for (language of ['ko', 'en']) {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const plan = new Plan({ onChanged: () => ({ dispose() {} }) });
    try {
      await page.evaluate(() => { window.acquireVsCodeApi = () => ({ postMessage() {} }); });
      await page.setContent(plan.html({ cspSource: '' }));
      const model = { kind: 'plan', state: 'idle', connection: 'attached', plan: '', jobs: '',
        handed: '', context: '', gauge: null, session: 's1', settings: null,
        choices: { models: [], profiles: [], sessions: [] }, fleet: [], cron: '', hand: '',
        outgoing: [
          { receipt: 'r1', to: 'sayaya-ui', request: 'bundle?\nMore context', started: '' },
          { receipt: 'r2', to: 'sayaya-ui', request: 'porting?', started: '' },
        ] };
      await page.evaluate(data => window.dispatchEvent(new MessageEvent('message', { data })), model);
      assert.equal(await page.locator('.handoff-row').count(), 2);
      assert.match(await page.locator('.handoff-row').first().textContent(), /bundle\?/);
      assert.match(await page.locator('.handoff-row').last().textContent(), /porting\?/);
      assert.equal(await page.locator('.handoff-row').first().getAttribute('aria-busy'), 'true');
      assert.match(await page.locator('.handoff-row').first().getAttribute('title'), /More context\nr1$/);
      assert.equal(await page.locator('.handoff-spinner').count(), 2);
      await page.evaluate(data => window.dispatchEvent(new MessageEvent('message', { data })), { ...model, outgoing: [] });
      assert.equal(await page.locator('.handoff-row').count(), 0);
      assert.deepEqual(errors, []);
      console.log(language + ': distinct requests, pending spinners and delivery removal passed');
    } finally { plan.dispose(); await page.close(); }
  }
} finally { await browser.close(); }
