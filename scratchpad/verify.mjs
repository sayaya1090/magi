// A conformance pass over the console: the rules that can be measured, measured.
//
// Every check below is one a browser can answer. Rules that need a person's judgement are not
// here, and neither are rules with no failing case in this page — a check that cannot fail is not
// a check. Each is mutation-tested before it is trusted; a clean run only means something once
// breaking the thing on purpose makes it speak.
import { chromium } from 'playwright';

const URL = process.env.DEMO_URL || 'http://localhost:8780/';
const SCREENS = { fleet: '', board: '?v=board', shared: '?v=skills', companion: '?d=/demo/design.sock' };
const findings = [];
const note = (screen, rule, detail) => findings.push({ screen, rule, detail });

const IN_PAGE = {
  // Every interactive control is at least 48x48. The library draws a .touch expander inside its
  // own shadow root, so the box that counts is the union of the host and that expander.
  // What a finger actually reaches, not what the box says.
  //
  // A target may be drawn small and still be 48dp: the library puts a .touch span in its shadow
  // root and this page puts an ::after under the control. Neither is a DOM box you can measure —
  // the pseudo-element is not in the tree at all — so a probe that reads getBoundingClientRect
  // reports a chip as 23px when the press area is 48. Ask the document instead: at the edges of
  // the 48dp box this control should own, does elementFromPoint come back to it?
  touchTargets: () => {
    const bad = [];
    const owns = (el, x, y) => {
      const hit = document.elementFromPoint(x, y);
      if (!hit) return false;
      if (hit === el || el.contains(hit)) return true;
      const root = hit.getRootNode();
      return root instanceof ShadowRoot && (root.host === el || el.contains(root.host));
    };
    for (const e of document.querySelectorAll('*')) {
      const tag = e.tagName.toLowerCase();
      const interactive = /^(a|button|input|select|textarea)$/.test(tag) ||
        /^md-(icon-button|(text|filled|filled-tonal|outlined|elevated)-button|primary-tab|secondary-tab|list-item|(filter|assist|input|suggestion)-chip|switch|checkbox|radio|(branded-)?fab)$/.test(tag);
      if (!interactive) continue;
      if (e.closest('[hidden]') || !e.getClientRects().length) continue;
      const r = e.getBoundingClientRect();
      if (!r.width || !r.height) continue;                  // laid out but not drawn
      // A disabled control is not a target. Material marks it by turning its touch span off, so
      // a probe that does not skip it reports the library defending itself as a defect.
      if (e.disabled || e.hasAttribute('disabled') || e.getAttribute('aria-disabled') === 'true') continue;
      // Inline links inside running text are exempt: their height is the line height of the prose
      // around them, and the rule says so. A link is inline when its own box is narrower than the
      // block it sits in AND it shares that block with other text.
      const cs = getComputedStyle(e);
      if (tag === 'a' && (cs.display === 'inline' || e.getClientRects().length > 1)) continue;
      const cx = r.x + r.width / 2, cy = r.y + r.height / 2;
      const half = 23;                        // 48 minus a pixel of rounding, each way
      const reach = [[cx, cy - half], [cx, cy + half]].every(([x, y]) =>
        y < 0 || y > innerHeight || owns(e, x, y));
      const wide = r.width >= 47.5 || [[cx - half, cy], [cx + half, cy]].every(([x, y]) =>
        x < 0 || x > innerWidth || owns(e, x, y));
      if (!reach || !wide) bad.push(`${tag}${e.id ? '#' + e.id : ''}${typeof e.className === 'string' && e.className ? '.' + e.className.trim().split(/\s+/)[0] : ''} ${Math.round(r.width)}x${Math.round(r.height)}${!reach ? ' 세로' : ''}${!wide ? ' 가로' : ''}`);
    }
    return [...new Set(bad)];
  },

  // Focus rings are checked from the KEYBOARD, in the harness below — element.focus() does not
  // always set :focus-visible, and a check built on it reported every library control as ringless
  // on three screens and none on the other five. A rule that answers differently to the same page
  // is not measuring the page.

  // Text that is cut off must be readable some other way — a tooltip, a title, or a link.
  truncatedWithoutRecourse: () => {
    const bad = [];
    document.querySelectorAll('*').forEach(e => {
      const cs = getComputedStyle(e);
      if (cs.textOverflow !== 'ellipsis') return;
      if (e.scrollWidth <= e.clientWidth + 1) return;            // not actually cut
      if (e.title || e.hasAttribute('data-tip') || e.closest('a[href]')) return;
      if (e.id === 'tip' || e.closest('#tip')) return;            // the tooltip IS the recourse
      bad.push((e.id ? '#' + e.id : e.tagName.toLowerCase()) + ' "' + e.textContent.trim().slice(0, 24) + '"');
    });
    return [...new Set(bad)];
  },

  // Anything that can be clicked should be reachable by keyboard: a div with a handler is not.
  clickableNotFocusable: () => {
    const bad = [];
    document.querySelectorAll('div[onclick],span[onclick],li[onclick]').forEach(e => {
      if (e.tabIndex >= 0 || e.getAttribute('role')) return;
      bad.push((e.id ? '#' + e.id : e.tagName.toLowerCase() + '.' + String(e.className).split(' ')[0]));
    });
    return [...new Set(bad)];
  },
};

const browser = await chromium.launch();
for (const scheme of ['dark', 'light']) {
  for (const [name, q] of Object.entries(SCREENS)) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 1000 }, colorScheme: scheme });
    const errs = [];
    page.on('pageerror', e => errs.push(String(e).slice(0, 80)));
    await page.goto(URL + q, { waitUntil: 'networkidle' });
    await page.waitForTimeout(1400);
    for (const [rule, fn] of Object.entries(IN_PAGE)) {
      const bad = await page.evaluate(`(${fn.toString()})()`);
      if (bad.length) note(`${scheme}/${name}`, rule, bad.slice(0, 6));
    }
    if (errs.length) note(`${scheme}/${name}`, 'pageerror', errs);
    await page.close();
  }
}

// Focus, from the keyboard.
//
// Tab through the page and ask each stop what it drew. The library's controls answer with an
// md-focus-ring carrying `visible`; the page's own controls answer with an outline that was not
// there before. Both are checked after a real Tab, because element.focus() does not set
// :focus-visible and a check built on it calls every library control ringless.
//
// An earlier version diffed screenshots either side of the stop. It kept reporting rings that a
// direct image diff proves are drawn — the mock streams, so the tab order moves under the walk,
// and blur() does not take an md-focus-ring off. Two ways to be wrong about the same pixels.
for (const scheme of ['dark', 'light']) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 1000 }, colorScheme: scheme });
  await page.goto(URL, { waitUntil: 'networkidle' }); await page.waitForTimeout(1200);
  const blind = [];
  for (let i = 0; i < 30; i++) {
    await page.keyboard.press('Tab');
    await page.waitForTimeout(70);
    const r = await page.evaluate(() => {
      const a = document.activeElement;
      if (!a || a === document.body || a === document.documentElement) return null;
      const name = a.tagName.toLowerCase() + (a.id ? '#' + a.id : '');
      // A text field shows focus by thickening and recolouring its own outline, inside a nested
      // md-outlined-field shadow root, not with a focus ring. Checked by pixels once: focused and
      // unfocused shots of the same box differ, so it draws one. Asking it for a ring is asking
      // the wrong question, and the answer was a defect that is not there.
      if (/^md-(outlined|filled)-(text-field|select)$/.test(a.tagName.toLowerCase())) return null;
      const ring = a.shadowRoot && a.shadowRoot.querySelector('md-focus-ring');
      if (ring) return { name, drawn: ring.hasAttribute('visible') && getComputedStyle(ring).display !== 'none' };
      const cs = getComputedStyle(a);
      const outline = cs.outlineStyle !== 'none' && parseFloat(cs.outlineWidth) > 0;
      return { name, drawn: outline || cs.boxShadow !== 'none' };
    });
    if (r && !r.drawn) blind.push(r.name);
  }
  if (blind.length) note(scheme, 'focusInvisible', [...new Set(blind)]);
  await page.close();
}

// Reflow: a narrow window, and a doubled default font size. Neither may need sideways scrolling.
for (const [label, opts] of [['320px 폭', { viewport: { width: 320, height: 800 } }],
                             ['기본글꼴 32px', { viewport: { width: 1280, height: 900 }, font: 32 }]]) {
  for (const [name, q] of Object.entries(SCREENS)) {
    const page = await browser.newPage({ viewport: opts.viewport });
    if (opts.font) { const cdp = await page.context().newCDPSession(page);
      await cdp.send('Page.setFontSizes', { fontSizes: { standard: opts.font, fixed: opts.font } }); }
    await page.goto(URL + q, { waitUntil: 'networkidle' }); await page.waitForTimeout(1200);
    const over = await page.evaluate(() => {
      const d = document.documentElement;
      return d.scrollWidth > d.clientWidth ? [d.scrollWidth, d.clientWidth] : null;
    });
    if (over) note(`${label}/${name}`, 'reflow', [`가로 ${over[0]} > ${over[1]}`]);
    await page.close();
  }
}

// Reduced motion must actually reduce it.
{
  const page = await browser.newPage({ reducedMotion: 'reduce' });
  await page.goto(URL, { waitUntil: 'networkidle' }); await page.waitForTimeout(800);
  const moving = await page.evaluate(() => [...document.querySelectorAll('*')]
    .filter(e => { const c = getComputedStyle(e);
      return (parseFloat(c.transitionDuration) > 0.05 || parseFloat(c.animationDuration) > 0.05) && e.getClientRects().length; })
    .map(e => e.tagName.toLowerCase() + (e.id ? '#' + e.id : '')).slice(0, 6));
  if (moving.length) note('reduced-motion', 'stillMoving', [...new Set(moving)]);
  await page.close();
}
await browser.close();

if (!findings.length) console.log('검증 항목 전부 통과 — 지적할 것 없음');
for (const f of findings) console.log(`⚠ ${f.screen.padEnd(20)} ${f.rule}\n     ${f.detail.join('\n     ')}`);
process.exitCode = findings.length ? 1 : 0;
