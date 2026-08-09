// A geometry snapshot: every visible box, keyed by a stable path. Diffing two runs says what a
// change actually moved, in pixels, instead of "it looks fine".
import { chromium } from 'playwright';
const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const SCREENS = { fleet: '', board: '?v=board', shared: '?v=skills', companion: '?d=/demo/design.sock' };
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1280, height: 1000 }, colorScheme: 'dark' });
const all = {};
for (const [name, q] of Object.entries(SCREENS)) {
  await p.goto(URL + q, { waitUntil: 'networkidle' }); await p.waitForTimeout(1400);
  all[name] = await p.evaluate(() => {
    const path = el => { const s = []; for (let e = el; e && e.tagName; e = e.parentElement) {
      const i = e.parentElement ? [...e.parentElement.children].indexOf(e) : 0;
      s.unshift(e.tagName.toLowerCase() + (e.id ? '#' + e.id : '') + ':' + i); } return s.join('>'); };
    const o = {};
    document.querySelectorAll('body *').forEach(e => { const r = e.getBoundingClientRect();
      if (r.width || r.height) o[path(e)] = [Math.round(r.x), Math.round(r.y), Math.round(r.width), Math.round(r.height)]; });
    return o;
  });
}
console.log(JSON.stringify(all));
await b.close();
