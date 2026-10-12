import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFile } from 'node:fs/promises';
const require = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const { chromium } = require('@playwright/test');
const browser = await chromium.launch({ headless: true });
try {
  for (const product of ['powerpoint', 'word', 'excel']) {
    const page = await browser.newPage();
    await page.setContent('<div id="advanced"></div><button id="provider"><span id="provider-value"></span></button><div id="provider-menu" hidden></div><button id="model"><span id="model-value"></span></button><div id="model-menu" hidden></div><p id="model-note"></p>');
    const root = new URL(`../../${product}/addin/src/`, import.meta.url);
    const screen = await readFile(new URL('ui/screen.js', root), 'utf8');
    const picker = screen.slice(screen.indexOf('export function modelPicker('), screen.indexOf('export function councilButton(')).replace('export function ', 'function ');
    const view = await readFile(new URL('ui/view.js', root), 'utf8');
    const render = view.slice(view.indexOf('  modelPicker(m) {'), view.indexOf('  menu(id, open) {')).replace('  modelPicker(m) {', 'function renderPicker(m) {');
    const main = await readFile(new URL('main.js', root), 'utf8');
    const send = main.slice(main.indexOf('    const sendModel = async'), main.indexOf('    // M3 메뉴:', main.indexOf('    const sendModel = async')));
    const handlersStart = main.indexOf("    for (const [btnId, menuId, key] of [['#provider'");
    const handlersEnd = main.indexOf('\n    }', handlersStart) + '\n    }'.length;
    const handlers = main.slice(handlersStart, handlersEnd);
    const module = await readFile(new URL('ui/providerProfiles.js', root), 'utf8');
    await page.addScriptTag({ content: picker + '\n' + render + '\n' + module.replace('export function ', 'function ') });
    await page.addScriptTag({ content: `
      const $ = s => document.querySelector(s);
      window.calls = []; window.saved = []; window.selected = ''; window.refreshes = 0;
      const api = {
        providerProfiles: async () => ({ profiles: saved }),
        setProviderProfile: async body => {
          calls.push({...body});
          if (body.op === 'save') saved = [{name: body.name, baseUrl: body.baseUrl, model: body.model, hasKey: Boolean(body.apiKey)}];
          if (body.op === 'apply') selected = body.name;
          return {profiles: saved};
        },
        setModel: async body => { throw new Error('saved profile must not use base-only switching'); },
        models: async () => ({ providers: [], profiles: saved, profile: selected, backend: selected ? saved[0]?.baseUrl || '' : '', model: saved[0]?.model || '', models: [saved[0]?.model].filter(Boolean) }),
      };
      const view = { where: () => {}, modelPicker: renderPicker, menu: (id,on) => { document.querySelector(id + '-menu').hidden = !on; } };
      const loadModels = async () => { refreshes++; view.modelPicker(await api.models()); };
      ${send}
      ${handlers}
      mountProviderProfiles({api, onSaved: loadModels});
    ` });
    await page.locator('#manage-providers').click();
    await page.locator('#profile-name').fill('my-provider');
    await page.locator('#profile-endpoint').fill('https://example.com/v1');
    await page.locator('#profile-model').fill('model-a');
    await page.locator('#profile-key').fill('test-secret');
    await page.locator('#profile-save').click();
    await page.waitForFunction(() => !document.querySelector('#provider-profiles-dialog').open && refreshes === 1);
    assert.equal(await page.locator('#profile-apply').count(), 0);
    await page.locator('#provider').click();
    const option = page.locator('#provider-menu [role="option"]');
    assert.equal(await option.innerText(), '✓my-provider');
    await option.click();
    await page.waitForFunction(() => calls.length === 2 && refreshes === 2);
    assert.deepEqual(await page.evaluate(() => calls.at(-1)), {op: 'apply', name: 'my-provider'});
    assert.equal(await page.locator('#provider-value').innerText(), 'my-provider');
    assert.equal(await page.locator('#provider-menu [role="option"]').getAttribute('aria-selected'), 'true');
    assert.equal(await page.locator('#model-value').innerText(), 'model-a');
    await page.close();
    console.log(`PASS ${product}: save closes, named profile refreshes picker, real picker handler applies name-only profile`);
  }
} finally { await browser.close(); }
