import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFile } from 'node:fs/promises';
const require = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const { chromium } = require('@playwright/test');
const browser = await chromium.launch({ headless: true });
try {
  for (const product of ['powerpoint', 'word', 'excel']) {
    const page = await browser.newPage({ viewport: { width: 320, height: 600 } });
    await page.setContent('<div id="advanced"></div>');
    await page.addStyleTag({ path: new URL(`../../${product}/addin/taskpane.css`, import.meta.url).pathname });
    const source = await readFile(new URL(`../../${product}/addin/src/ui/providerProfiles.js`, import.meta.url), 'utf8');
    await page.addScriptTag({ content: source.replace('export function ', 'function ') });
    await page.evaluate(() => {
      window.requests = []; window.fail = false;
      const profiles = [{ name: 'saved', baseUrl: 'https://example.com/v1', model: 'model-a', hasKey: true }];
      mountProviderProfiles({ api: {
        providerProfiles: async () => ({ profiles }),
        setProviderProfile: async body => { requests.push({ ...body }); if (window.fail) throw new Error('secret-should-not-render'); if (body.op === 'apply') return { note: '현재 대화에 적용했습니다.' }; return { profiles: body.op === 'delete' ? [] : [{ name: body.name, baseUrl: body.baseUrl, model: body.model, hasKey: !body.clearKey }] }; },
      } });
    });
    await page.locator('#manage-providers').click();
    await page.locator('#profile-list').selectOption('saved');
    assert.equal(await page.locator('#profile-key').inputValue(), '');
    assert.equal(await page.locator('#profile-key').getAttribute('type'), 'password');
    await page.locator('#profile-save').click();
    await page.waitForFunction(() => document.querySelector('#profile-status').textContent.includes('저장했습니다'));
    await page.waitForFunction(() => !document.querySelector('#provider-profiles-dialog').open);
    await page.locator('#manage-providers').click();
    await page.locator('#profile-list').selectOption('saved');
    assert.equal((await page.evaluate(() => requests.at(-1))).apiKey, '');
    assert.equal(await page.locator('#profile-name').getAttribute('readonly'), '');
    await page.locator('#profile-key').fill('new-secret');
    await page.locator('#profile-save').click();
    await page.waitForFunction(() => requests.length === 2 && !document.querySelector('#profile-save').disabled);
    assert.equal((await page.evaluate(() => requests.at(-1))).apiKey, 'new-secret');
    assert.equal(await page.locator('#profile-key').inputValue(), '');
    await page.waitForFunction(() => !document.querySelector('#provider-profiles-dialog').open);
    await page.locator('#manage-providers').click();
    await page.locator('#profile-list').selectOption('saved');
    await page.locator('#profile-clear-key').check();
    await page.locator('#profile-save').click();
    await page.waitForFunction(() => requests.length === 3 && !document.querySelector('#profile-save').disabled);
    assert.equal((await page.evaluate(() => requests.at(-1))).clearKey, true);
    await page.waitForFunction(() => !document.querySelector('#provider-profiles-dialog').open);
    await page.locator('#manage-providers').click();
    await page.locator('#profile-list').selectOption('saved');
    assert.equal(await page.locator('#profile-apply').count(), 0);
    await page.evaluate(() => window.fail = true);
    await page.locator('#profile-key').fill('failed-secret');
    await page.locator('#profile-save').click();
    await page.waitForFunction(() => document.querySelector('#profile-status').textContent.includes('저장하지 못했습니다'));
    assert.equal(await page.locator('#profile-key').inputValue(), '');
    assert.equal(await page.locator('#provider-profiles-dialog').evaluate(d => d.open), true);
    assert.ok(!(await page.locator('body').innerText()).includes('secret'));
    await page.evaluate(() => window.fail = false);
    page.once('dialog', d => d.dismiss()); await page.locator('#profile-delete').click();
    assert.equal(await page.evaluate(() => requests.length), 4);
    page.once('dialog', d => d.accept()); await page.locator('#profile-delete').click();
    await page.waitForFunction(() => requests.length === 5 && !document.querySelector('#profile-save').disabled);
    assert.equal((await page.evaluate(() => requests.at(-1))).op, 'delete');
    await page.locator('#profile-list').selectOption('');
    await page.locator('#profile-name').fill('new');
    await page.locator('#profile-endpoint').fill('https://new.example/v1');
    await page.locator('#profile-model').fill('new-model');
    await page.locator('#profile-save').click();
    await page.waitForFunction(() => requests.length === 6 && !document.querySelector('#profile-save').disabled);
    assert.equal((await page.evaluate(() => requests.at(-1))).name, 'new');
    await page.waitForFunction(() => !document.querySelector('#provider-profiles-dialog').open);
    await page.locator('#manage-providers').click();
    const box = await page.locator('#provider-profiles-dialog').boundingBox(); assert.ok(box.width <= 320);
    await page.locator('#profile-key').fill('close-secret');
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => !document.querySelector('#provider-profiles-dialog').open && document.querySelector('#profile-key').value === '' && document.activeElement.id === 'manage-providers');
    assert.equal(await page.locator('#profile-key').inputValue(), '');
    assert.equal(await page.evaluate(() => document.activeElement.id), 'manage-providers');
    await page.close(); console.log(`PASS ${product}: provider CRUD, preserve/clear key, error secrecy, close cleanup, narrow viewport`);
  }
} finally { await browser.close(); }
