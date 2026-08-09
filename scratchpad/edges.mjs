// Nothing, and everything broken.
//
// The mock always answers with a fleet, a history, some skills. A console on its first day answers
// with none of that, and a console whose daemon is unwell answers with a 500 — two shapes nothing
// here has ever been drawn against.
import { chromium } from 'playwright';
const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const SCREENS = { fleet: '', board: '?v=board', shared: '?v=skills' };
const browser = await chromium.launch();
const bad = [];
for (const [mode, patch] of [
  ['비어 있음', () => { const real = globalThis.fetch;
    globalThis.fetch = async (u, i) => (/i18n/.test(String(u))) ? real(u, i)
      : ({ ok: true, status: 200, json: async () => [], text: async () => '[]' }); }],
  ['서버 오류', () => { const real = globalThis.fetch;
    globalThis.fetch = async (u, i) => (/i18n/.test(String(u))) ? real(u, i)
      : ({ ok: false, status: 500, json: async () => null, text: async () => 'the daemon fell over' }); }],
  ['닿지 않음', () => { const real = globalThis.fetch;
    globalThis.fetch = async (u, i) => (/i18n/.test(String(u))) ? real(u, i)
      : Promise.reject(new Error('network')); }],
]) {
  for (const [name, q] of Object.entries(SCREENS)) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
    page.on('pageerror', e => bad.push(`${mode}/${name}: threw ${String(e).slice(0, 80)}`));
    // ⚠ After the page has loaded, not before. The demo installs its own fetch inside an IIFE at
    // parse time, so an init script is overwritten by it — the first version of this probe reported
    // three identical results and was measuring the ordinary mock all three times.
    await page.goto(URL + q, { waitUntil: 'networkidle' });
    await page.waitForTimeout(1200);
    await page.evaluate(patch);
    // And make it ask again: the loaders run on a poll or on a view change, not on their own.
    await page.evaluate(() => { const r = document.getElementById('railSkills'); if (r) r.click(); });
    await page.waitForTimeout(600);
    await page.goto(URL + q, { waitUntil: 'domcontentloaded' }).catch(() => {});
    await page.waitForTimeout(400);
    await page.evaluate(patch);
    await page.waitForTimeout(3600);
    const said = await page.evaluate(() => {
      const main = document.querySelector('main');
      const text = (main.innerText || '').replace(/\s+/g, ' ').trim();
      return { chars: text.length, note: (document.getElementById('note') || {}).textContent || '',
               dot: document.getElementById('state').className,
               empty: !!main.querySelector('.empty, .emptystate, [class*=empty]') };
    });
    // A screen that says nothing at all is the failure. It has to say what is missing, or that it
    // cannot reach anything — silence is the state a person cannot act on.
    if (said.chars < 24 && !said.note) bad.push(`${mode}/${name}: the screen says nothing (${said.chars} chars)`);
    if (mode !== '비어 있음' && !said.note && said.dot !== 'lost')
      bad.push(`${mode}/${name}: no message and the dot is "${said.dot || 'clear'}" — it looks fine and is not`);
    console.log(`${mode.padEnd(8)} ${name.padEnd(8)} ${said.chars}자  note="${said.note.slice(0, 34)}"  dot=${said.dot || '-'}`);
    await page.close();
  }
}
await browser.close();
if (bad.length) { [...new Set(bad)].slice(0, 10).forEach(b => console.log('  ⚠ ' + b)); process.exitCode = 1; }
else console.log('빈 상태·오류 경로 모두 말을 한다');
