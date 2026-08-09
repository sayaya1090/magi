// The page left running, and the page walked around.
//
// A console sits open all day. Two things go wrong over that kind of time and neither shows in a
// screenshot: a poll or a listener that is armed again on every visit and never taken down, and a
// DOM that grows because something appends where it should replace.
import { chromium } from 'playwright';
const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
const errs = [];
page.on('pageerror', e => errs.push(String(e).slice(0, 90)));
await page.goto(URL, { waitUntil: 'networkidle' });
await page.waitForTimeout(1500);

// Count what is armed, by wrapping the timers before anything arms them.
await page.evaluate(() => {
  globalThis.__live = { intervals: 0, timeouts: 0, sockets: 0 };
  const si = globalThis.setInterval, ci = globalThis.clearInterval;
  globalThis.setInterval = (...a) => { globalThis.__live.intervals++; return si(...a); };
  globalThis.clearInterval = (h) => { if (h) globalThis.__live.intervals--; return ci(h); };
  const ES = globalThis.EventSource;
  globalThis.EventSource = class extends ES { constructor(...a) { super(...a); globalThis.__live.sockets++; }
    close() { globalThis.__live.sockets--; return super.close && super.close(); } };
});

const snap = () => page.evaluate(() => ({
  ...globalThis.__live, nodes: document.querySelectorAll('*').length,
}));
const dests = ['railFleet', 'railSkills'];
// Visit each screen once BEFORE the baseline. A destination's content is built when it is first
// reached and then kept, so counting from before that reads as growth and is not — this probe's
// first answer was 270 → 441 and the difference was the other screen existing.
for (const id of dests) { await page.evaluate(i => document.getElementById(i).click(), id); await page.waitForTimeout(700); }
await page.evaluate(() => { const r = document.querySelector('#fleet .card'); if (r) r.click(); });
await page.waitForTimeout(1200);
await page.evaluate(() => history.back());
await page.waitForTimeout(1000);
const first = await snap();
for (let round = 0; round < 8; round++) {
  for (const id of dests) {
    await page.evaluate((i) => document.getElementById(i).click(), id);
    await page.waitForTimeout(450);
  }
  // And into a companion and back, which is the other loop somebody does all day.
  await page.evaluate(() => { const r = document.querySelector('#fleet .card'); if (r) r.click(); });
  await page.waitForTimeout(600);
  await page.evaluate(() => history.back());
  await page.waitForTimeout(600);
}
await page.waitForTimeout(2000);
const last = await snap();
console.log('처음:', JSON.stringify(first));
console.log('16회 왕복 뒤:', JSON.stringify(last));
const bad = [];
if (last.intervals > first.intervals + 1) bad.push(`폴이 ${last.intervals - first.intervals}개 쌓였다`);
if (last.sockets > first.sockets + 1) bad.push(`스트림이 ${last.sockets - first.sockets}개 남았다`);
// A handful of nodes is the mock adding work while this runs; growth in PROPORTION to the number
// of round trips is the leak. Measured on this page: +4 over six and +7 over eighteen.
if (last.nodes > first.nodes + 40) bad.push(`DOM이 ${first.nodes} → ${last.nodes}로 자랐다`);
if (errs.length) bad.push('에러: ' + [...new Set(errs)].slice(0, 3).join(' | '));
if (bad.length) { bad.forEach(b => console.log('  ⚠ ' + b)); process.exitCode = 1; }
else console.log('쌓이는 것 없음');
await browser.close();
