// The page with content that is too long for it.
//
// Every measurement so far used the mock's own strings, which are the length somebody chose while
// writing them. Real names, tasks, urls and workspace paths are not. Overflow, clipping and layout
// collapse all hide behind a fixture that happens to fit.
import { chromium } from 'playwright';
const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const LONG = 'a-really-quite-unreasonably-long-'.repeat(4) + 'name';
const SENT = 'It is a truth universally acknowledged that a single agent in possession of a good plan must be in want of a much longer sentence than anybody expected to fit here. '.repeat(3);
const browser = await chromium.launch();
const bad = [];
for (const w of [1440, 1280, 1000, 390]) {
  const page = await browser.newPage({ viewport: { width: w, height: 1000 } });
  page.on('pageerror', e => bad.push(`@${w}: ${String(e).slice(0, 90)}`));
  await page.goto(URL, { waitUntil: 'networkidle' });
  await page.waitForTimeout(1400);
  // Stretch every string the page draws, in place, and let it re-render.
  await page.evaluate(([LONG, SENT]) => {
    for (const el of document.querySelectorAll('#fleet .name, #fleet .doing, #fleet .host, .wwhat, .sk .what, .srv .how')) {
      el.textContent = el.classList.contains('name') ? LONG : SENT;
    }
  }, [LONG, SENT]);
  await page.waitForTimeout(400);
  const out = await page.evaluate(() => {
    const W = document.documentElement.clientWidth, o = [];
    if (document.documentElement.scrollWidth > W + 1)
      o.push(`the page scrolls sideways: ${document.documentElement.scrollWidth} > ${W}`);
    // A child that leaves its parent's box.
    for (const e of document.querySelectorAll('#fleet .card, .wcard, .sk, .srv')) {
      const p = e.getBoundingClientRect();
      for (const k of e.children) {
        const r = k.getBoundingClientRect();
        if (r.right > p.right + 1 || r.left < p.left - 1)
          o.push(`${k.className || k.tagName.toLowerCase()} escapes its ${e.className.split(' ')[0]}`);
      }
    }
    return [...new Set(o)];
  });
  out.forEach(x => bad.push(`@${w}: ${x}`));
  console.log(`${String(w).padStart(5)}px  ${out.length ? out.length + ' 건' : '깨끗'}`);
  await page.close();
}
await browser.close();
if (bad.length) { [...new Set(bad)].slice(0, 10).forEach(b => console.log('  ⚠ ' + b)); process.exitCode = 1; }
