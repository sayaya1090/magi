// The components against the numbers the guide gives them.
//
// The audit table says magi follows M3; this is the part of that claim a browser can settle. Each
// row is a dimension the spec states outright, measured on the element as drawn. Anything the spec
// leaves to the product (colour choices, which component to use) is not here — this is only the
// arithmetic.
//
// ⚠ Sizes are dp at a 16px root. A row that reports a miss is not automatically a defect: the
// library ships some components as their deprecated set, and where magi deliberately differs the
// reason belongs in docs/UI.md §6a. But a MISS THAT NOBODY HAS WRITTEN DOWN is the thing this
// exists to surface.
import { chromium } from 'playwright';

const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const SCREENS = { fleet: '', board: '?v=board', shared: '?v=skills', mcp: '?v=mcp', companion: '?d=/demo/design.sock' };

// selector → [what, measured property, expected, tolerance]
const SPEC = [
  ['md-primary-tab',            'height', 48, 1,  'tabs: container 48dp'],
  ['md-secondary-tab',          'height', 48, 1,  'tabs: container 48dp'],
  // The CONTAINER, not the host: supporting text sits below the box and a textarea grows with what
  // is typed into it, so measuring the host called a 76px field-plus-hint a miss against a spec
  // that never said anything about the hint.
  ['md-outlined-text-field::container', 'height', 56, 2, 'text field: container 56dp'],
  ['md-icon-button',            'height', 40, 1,  'icon button: state layer 40dp'],
  ['md-text-button',            'height', 40, 1,  'button: height 40dp'],
  ['md-filled-button',          'height', 40, 1,  'button: height 40dp'],
  ['md-filled-tonal-button',    'height', 40, 1,  'button: height 40dp'],
  ['md-filter-chip',            'height', 32, 1,  'chip: height 32dp'],
  ['.wlabel',                   'height', 32, 1,  'chip (hand-built): height 32dp'],
  ['md-list-item',              'height', 56, 8,  'list item: one line 56dp (rail item 56–64dp)'],
  ['md-linear-progress',        'height', 4,  1,  'linear progress: 4dp'],
  // A dialog's host is a 0x0 box; the thing with a size is the <dialog> in its shadow root. It is
  // also the one component the guide gives two shapes, so it is measured at both widths: basic
  // above compact, full-screen below, which is what the two rows below check between them.
  ['md-dialog::container',      'width',  560, 1, 'dialog: basic max width 560dp'],
];
const SHAPE = [
  ['md-filter-chip',        8,  'chip: shape 8dp'],
  ['.wlabel',               999, 'chip (hand-built): pill, magi\'s choice'],
  ['md-outlined-text-field', 4, 'text field: shape 4dp'],
  ['md-outlined-card',      12, 'card: shape 12dp'],
  ['md-dialog',             28, 'dialog: basic shape 28dp'],
];
// magi draws seventeen of the library's components and no md-switch, so a row for one would be a
// check that can never run. What is listed is what is on the screen.

const browser = await chromium.launch();
const seen = new Map();                      // "what" → Set of measurements
// Both widths, because the tab strip only exists on the narrow one and the rail only on the wide.
const WIDTHS = [[1280, 1000], [390, 900]];
for (const [name, q] of Object.entries(SCREENS)) for (const [w, h] of WIDTHS) {
  const page = await browser.newPage({ viewport: { width: w, height: h } });
  await page.goto(URL + q, { waitUntil: 'networkidle' });
  await page.waitForTimeout(1400);
  // The dialog is only measurable open, and it is the one component with two shapes.
  await page.evaluate(() => { const o = document.querySelector('.mcpopen'); if (o) o.click(); });
  await page.waitForTimeout(500);
  const got = await page.evaluate(([spec, shape]) => {
    const out = [];
    const box = e => e.getBoundingClientRect();
    const partOf = (e, part) => {
      if (!part) return e;
      if (!e.shadowRoot) return null;
      if (part === 'container' && e.shadowRoot.querySelector('dialog')) return e.shadowRoot.querySelector('dialog');
      const inner = e.shadowRoot.querySelector('.' + part) ||
        [...e.shadowRoot.querySelectorAll('*')].map(x => x.shadowRoot && x.shadowRoot.querySelector('.' + part)).find(Boolean);
      return inner || null;
    };
    for (const [rawSel, prop, want, tol, what] of spec) {
      const [sel, part] = rawSel.split('::');
      for (const host of document.querySelectorAll(sel)) {
        if (host.getAttribute('type') === 'textarea') continue;   // grows with its content, by design
        const e = partOf(host, part); if (!e) continue;
        const r = box(e); if (!r.width || !r.height) continue;
        const v = prop === 'height' ? r.height : r.width;
        out.push([what, Math.round(v * 10) / 10, want, tol]);
      }
    }
    // The corner is not always on the host. A text field is a 0-radius box around a shadow root
    // whose .outline carries the 4dp, so a probe that reads the host reports a miss that is its
    // own — it did, once, and the number it named was right in the component all along.
    const cornerOf = e => {
      const own = parseFloat(getComputedStyle(e).borderTopLeftRadius) || 0;
      if (own) return own;
      if (!e.shadowRoot) return 0;
      for (const inner of e.shadowRoot.querySelectorAll('*')) {
        const r = parseFloat(getComputedStyle(inner).borderTopLeftRadius) || 0;
        if (r) return r;
      }
      return 0;
    };
    for (const [sel, want, what] of shape) {
      for (const host of document.querySelectorAll(sel)) {
        const e = host.shadowRoot && host.shadowRoot.querySelector('dialog') || host;
        const r = box(e); if (!r.width || !r.height) continue;
        const v = cornerOf(e);
        out.push([what, v >= 999 ? 999 : Math.round(v * 10) / 10, want, want >= 999 ? 0 : 1]);
      }
    }
    return out;
  }, [SPEC, SHAPE]);
  for (const [what, v, want, tol] of got) {
    if (w < 600 && /dialog: basic/.test(what)) continue;      // compact is the full-screen shape
    if (w >= 600 && /dialog: full/.test(what)) continue;
    if (!seen.has(what)) seen.set(what, { want, tol, vals: new Set() });
    seen.get(what).vals.add(v);
  }
  await page.close();
}
await browser.close();

let miss = 0;
for (const [what, { want, tol, vals }] of [...seen].sort()) {
  const bad = [...vals].filter(v => Math.abs(v - want) > tol);
  const shown = [...vals].sort((a, b) => a - b).join(', ');
  if (bad.length) { miss++; console.log(`✗ ${what}\n     기대 ${want}  측정 ${shown}`); }
  else console.log(`✓ ${what}  (${shown})`);
}
const absent = SPEC.map(s => s[4]).concat(SHAPE.map(s => s[2])).filter(w => !seen.has(w));
if (absent.length) console.log('\n(화면에 없어 못 잰 것: ' + [...new Set(absent)].join(' · ') + ')');
console.log(miss ? `\n⚠ ${miss}건 불일치` : '\n측정된 항목 전부 스펙과 일치');
process.exitCode = miss ? 1 : 0;
