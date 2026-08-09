// The page with no mouse at all.
//
// Every probe so far has clicked. A keyboard user tabs, presses Enter, presses Escape — and the
// failures there are different: focus that lands on nothing, a dialog that traps you, a control
// reachable by pointer and not by Tab, an order that jumps around the screen.
import { chromium } from 'playwright';
const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const SCREENS = { fleet: '', board: '?v=board', shared: '?v=skills', companion: '?d=/demo/design.sock' };
const browser = await chromium.launch();
const bad = [];
for (const [name, q] of Object.entries(SCREENS)) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
  page.on('pageerror', e => bad.push(`${name}: ${String(e).slice(0, 90)}`));
  await page.goto(URL + q, { waitUntil: 'networkidle' });
  await page.waitForTimeout(1400);
  await page.evaluate(() => document.body.focus());
  const seen = [], order = [];
  for (let i = 0; i < 45; i++) {
    await page.keyboard.press('Tab');
    await page.waitForTimeout(60);
    const at = await page.evaluate(() => {
      const deep = e => (e && e.shadowRoot && e.shadowRoot.activeElement) ? deep(e.shadowRoot.activeElement) : e;
      const a = document.activeElement;
      if (!a || a === document.body) return { lost: true };
      const t = deep(a), r = t.getBoundingClientRect();
      return { id: a.id || a.tagName.toLowerCase(), x: Math.round(r.x), y: Math.round(r.y),
               w: Math.round(r.width), h: Math.round(r.height) };
    });
    // Leaving the page after the last control is what Tab DOES — it goes to the browser's own
    // chrome. That is not a defect, and calling it one was this probe's first answer. What matters
    // is whether every control a pointer can reach was among the stops.
    if (at.lost) break;
    if (!at.w || !at.h) bad.push(`${name}: stop ${i} (${at.id}) has no box — focus on something invisible`);
    order.push(at);
    seen.push(at.id);
  }
  // Everything a pointer can reach, reachable by Tab. A control that answers a click and not a
  // Tab is one a keyboard user simply does not have.
  const pointer = await page.evaluate(() => [...document.querySelectorAll(
    'a[href],button,md-icon-button,md-text-button,md-filled-button,md-filled-tonal-button,md-primary-tab,md-filter-chip,md-outlined-text-field,md-outlined-select,.raili')]
    .filter(e => e.getClientRects().length && !e.closest('[hidden]') && !e.hasAttribute('disabled'))
    .map(e => e.id || e.tagName.toLowerCase()));
  const missing = pointer.filter(p => !seen.includes(p));
  if (missing.length) bad.push(`${name}: reachable by pointer, never by Tab — ${[...new Set(missing)].slice(0, 5).join(', ')}`);

  // The order must read down the page, roughly. A stop that jumps far UP after going down is the
  // shape that makes a keyboard user lose their place.
  let jumps = 0;
  for (let i = 1; i < order.length; i++) if (order[i].y < order[i - 1].y - 200) jumps++;
  if (jumps > 3) bad.push(`${name}: the tab order jumps back up the page ${jumps} times`);
  // Escape must not leave a dialog open.
  const dlg = await page.evaluate(() => { const o = document.querySelector('.mcpopen'); if (o) { o.click(); return true; } return false; });
  if (dlg) {
    await page.waitForTimeout(500);
    await page.keyboard.press('Escape');
    await page.waitForTimeout(500);
    const open = await page.evaluate(() => document.getElementById('mcpDialog').open);
    if (open) bad.push(`${name}: Escape did not close the dialog`);
  }
  console.log(`${name.padEnd(10)} ${seen.length} stops`);
  await page.close();
}
await browser.close();
if (bad.length) { [...new Set(bad)].forEach(b => console.log('  ⚠ ' + b)); process.exitCode = 1; }
else console.log('키보드만으로 문제 없음');
