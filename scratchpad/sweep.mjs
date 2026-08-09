// Press everything, once, and see what throws.
//
// The MCP form called a method only the fake DOM has and threw the moment somebody pressed the
// button — every test passed, because no test pressed it in a browser. A page is a set of handlers
// and the only way to know they run is to run them.
//
// One press each: arm() makes a destructive control ask before it acts, so a single press is safe
// by construction. Anything that navigates is followed by going back.
import { chromium } from 'playwright';
const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const SCREENS = { fleet: '', board: '?v=board', shared: '?v=skills', companion: '?d=/demo/design.sock' };
const browser = await chromium.launch();
let pressed = 0; const bad = []; const still = [];
for (const [name, q] of Object.entries(SCREENS)) for (const w of [1280, 390]) {
  const page = await browser.newPage({ viewport: { width: w, height: 1000 } });
  page.on('pageerror', e => bad.push(`${name}@${w}: ${String(e).slice(0, 110)}`));
  page.on('console', m => { if (m.type() === 'error') bad.push(`${name}@${w} console: ${m.text().slice(0, 110)}`); });
  await page.goto(URL + q, { waitUntil: 'networkidle' });
  await page.waitForTimeout(1400);
  const n = await page.evaluate(() => document.querySelectorAll(
    'a[href],button,md-icon-button,md-text-button,md-filled-button,md-filled-tonal-button,md-primary-tab,md-filter-chip,.raili').length);
  for (let i = 0; i < n; i++) {
    const before = page.url();
    const sigBefore = await page.evaluate(() => location.href + '|' + document.body.innerHTML.length + '|' + document.documentElement.outerHTML.slice(0,200) + '|' +
      [...document.querySelectorAll('md-dialog')].map(d => d.open).join(',') + '|' +
      (document.activeElement && document.activeElement.id));
    const label = await page.evaluate((i) => {
      const els = [...document.querySelectorAll(
        'a[href],button,md-icon-button,md-text-button,md-filled-button,md-filled-tonal-button,md-primary-tab,md-filter-chip,.raili')];
      const e = els[i]; if (!e) return '?';
      return (e.id ? '#' + e.id : e.tagName.toLowerCase()) + ' "' + (e.textContent || '').trim().slice(0, 18) + '"';
    }, i);
    const ok = await page.evaluate((i) => {
      const els = [...document.querySelectorAll(
        'a[href],button,md-icon-button,md-text-button,md-filled-button,md-filled-tonal-button,md-primary-tab,md-filter-chip,.raili')];
      const e = els[i];
      if (!e || !e.getClientRects().length || e.hasAttribute('disabled')) return false;
      e.click();
      return true;
    }, i).catch(err => { bad.push(`${name}@${w} click ${i}: ${String(err).slice(0, 90)}`); return false; });
    if (ok) pressed++;
    await page.waitForTimeout(220);
    // ⚠ A press that throws is the easy case. Cancel on the MCP dialog did not throw — it did
    // NOTHING, quietly, and a sweep that only listens for errors called that a pass. So each press
    // has to change something: the url, the DOM, a dialog's open state, or what is focused. A
    // control that moves none of them is either dead or waiting for a second press by design.
    const moved = await page.evaluate(([sig]) => {
      const now = location.href + '|' + document.body.innerHTML.length + '|' + document.documentElement.outerHTML.slice(0,200) + '|' +
        [...document.querySelectorAll('md-dialog')].map(d => d.open).join(',') + '|' +
        (document.activeElement && document.activeElement.id);
      return now !== sig;
    }, [sigBefore]).catch(() => true);
    if (ok && !moved) still.push(`${name}@${w}: ${label}`);
    if (page.url() !== before) { await page.goto(URL + q, { waitUntil: 'networkidle' }); await page.waitForTimeout(900); }
  }
  await page.close();
}
await browser.close();
console.log(`${pressed}회 눌렀다`);
if (bad.length) { [...new Set(bad)].slice(0, 12).forEach(b => console.log('  ⚠ ' + b)); process.exitCode = 1; }
else console.log('던진 것 없음');
// A control that asks before it acts moves nothing on the first press, by design. Listed rather
// than failed: this is a question to look at, not an answer.
if (still.length) { console.log('아무것도 안 움직인 press (arm된 것은 정상):');
  [...new Set(still)].slice(0, 14).forEach(x => console.log('   ' + x)); }
