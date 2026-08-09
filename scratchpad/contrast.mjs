// Contrast of what is actually painted, in both themes.
//
// Reads every text node's computed colour and walks up for the first non-transparent background,
// which is what a reader's eye does. Reported against WCAG 1.4.3: 4.5:1 for text, 3:1 for large.
import { chromium } from 'playwright';
const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const b = await chromium.launch();
for (const scheme of ['dark', 'light']) {
  const p = await b.newPage({ viewport: { width: 1280, height: 1000 }, colorScheme: scheme });
  const probe = () => p.evaluate(() => {
    const rgb = s => { const m = s.match(/[\d.]+/g); return m ? m.slice(0,3).map(Number).concat(m[3]===undefined?1:+m[3]) : null; };
    const lum = c => { const f = v => { v/=255; return v<=0.03928 ? v/12.92 : Math.pow((v+0.055)/1.055,2.4); };
      return 0.2126*f(c[0])+0.7152*f(c[1])+0.0722*f(c[2]); };
    const ratio = (a,b) => { const [x,y]=[lum(a),lum(b)].sort((m,n)=>n-m); return (x+0.05)/(y+0.05); };
    // Walking up must cross the shadow boundary too: stopping at it finds no background,
    // and a default of black turns every light-theme label into a fake violation.
    const up = e => e.parentElement || (e.parentNode instanceof ShadowRoot ? e.parentNode.host : null);
    const bgOf = el => { for (let e=el; e; e=up(e)) { const c=rgb(getComputedStyle(e).backgroundColor);
      if (c && c[3] > 0.5) return c; } return [0,0,0,1]; };
    // Most of this page is md-* components, and their text lives in a shadow root — a plain
    // querySelectorAll walks straight past it and reports a clean sheet for a page it never read.
    const walk = (root, acc) => {
      root.querySelectorAll('*').forEach(e => { acc.push(e); if (e.shadowRoot) walk(e.shadowRoot, acc); });
      return acc;
    };
    const out = [];
    walk(document, []).forEach(el => {
      const t = [...el.childNodes].filter(n=>n.nodeType===3&&n.textContent.trim()).map(n=>n.textContent.trim()).join(' ');
      if (!t) return;
      const cs = getComputedStyle(el); if (cs.visibility==='hidden'||cs.display==='none') return;
      const r = el.getBoundingClientRect(); if (!r.width || !r.height) return;
      const fg = rgb(cs.color); if (!fg || fg[3] < 0.5) return;
      const px = parseFloat(cs.fontSize), bold = parseInt(cs.fontWeight,10) >= 700;
      const large = px >= 24 || (bold && px >= 18.66);
      const need = large ? 3 : 4.5, got = ratio(fg, bgOf(el));
      if (got < need) out.push({ sel: el.tagName.toLowerCase()+(el.className&&typeof el.className==='string'?'.'+el.className.trim().split(/\s+/).slice(0,2).join('.'):''),
        text: t.slice(0,26), got: +got.toFixed(2), need, px: +px.toFixed(0), fg: cs.color });
    });
    const seen = new Set();
    return out.filter(o => { const k=o.sel+o.fg; if (seen.has(k)) return false; seen.add(k); return true; });
  });
  const SCREENS = { fleet: '', board: '?v=board', shared: '?v=skills', companion: '?d=/demo/design.sock' };
  for (const [name, q] of Object.entries(SCREENS)) {
    await p.goto(URL + q, { waitUntil: 'networkidle' }); await p.waitForTimeout(1400);
    const bad = await probe();
    console.log('== ' + scheme + ' / ' + name.padEnd(10) + (bad.length ? bad.length + ' 건' : '위반 없음'));
    bad.sort((a,b)=>a.got-b.got).forEach(o => console.log(`     ${String(o.got).padStart(5)} < ${o.need}  ${o.sel}  ${o.px}px  ${o.fg}  "${o.text}"`));
  }
  await p.close();
}
await b.close();
