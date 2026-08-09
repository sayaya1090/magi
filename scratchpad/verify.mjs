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

// What a screen reader is actually told.
//
// Not what the DOM says — what the accessibility tree says, which is a different document and the
// only one that matters here. The rail was built out of md-list-item because Material Web ships no
// navigation rail, and that component writes role="listitem" on the anchor it renders: link
// semantics gone, name-from-content gone, and the whole navigation exposed as two unnamed nodes
// with every child ignored. Nothing in the DOM looked wrong. The tree is where it showed.
// Both widths, because the tab strip only exists on the narrow one, and with whatever dialog the
// screen can open — a control inside a closed dialog is not in the tree, so it is never asked.
for (const [screen, q] of Object.entries(SCREENS)) for (const w of [1280, 390]) {
  const page = await browser.newPage({ viewport: { width: w, height: 1000 } });
  await page.goto(URL + q, { waitUntil: 'networkidle' }); await page.waitForTimeout(1500);
  await page.evaluate(() => { const o = document.querySelector('.mcpopen'); if (o) o.click(); });
  await page.waitForTimeout(600);
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Accessibility.enable');
  const { nodes } = await cdp.send('Accessibility.getFullAXTree');
  const by = new Map(nodes.map(n => [n.nodeId, n]));
  const nameOf = n => (n.name && n.name.value) || '';
  const roleOf = n => (n.role && n.role.value) || '';
  // How many things a person can reach with the keyboard inside each navigation, from the DOM.
  const canFocus = await page.evaluate(() => {
    const out = {};
    for (const nav of document.querySelectorAll('nav')) {
      const label = nav.getAttribute('aria-label') || nav.id || 'nav';
      out[label] = [...nav.querySelectorAll('a[href],button,md-icon-button,md-list-item,md-primary-tab,md-secondary-tab')]
        .filter(e => e.getClientRects().length).length;
    }
    return out;
  });
  const bad = [];
  for (const nav of nodes.filter(n => roleOf(n) === 'navigation' && !n.ignored)) {
    let named = 0;
    const walk = id => { const n = by.get(id); if (!n) return;
      if (!n.ignored && /^(link|button|tab|menuitem)$/.test(roleOf(n))) {
        if (nameOf(n).trim()) named++;
        else bad.push(`${nameOf(nav) || 'nav'}: a ${roleOf(n)} with no name`);
      }
      (n.childIds || []).forEach(walk); };
    walk(nav.nodeId);
    // The tree has to account for every one of them. A component that names itself listitem takes
    // its whole subtree out of the tree while leaving the DOM looking correct, and counting is the
    // only way to see that from here.
    const want = canFocus[nameOf(nav)] ?? canFocus[Object.keys(canFocus)[0]] ?? 0;
    if (named < want) bad.push(`${nameOf(nav) || 'nav'}: ${want} things to reach, ${named} named in the tree`);
  }
  // And everything else a keyboard reaches, anywhere on the screen. A control the tree cannot name
  // is a control somebody listening cannot choose — the rail proved that a whole region can go
  // dark while the DOM reads correctly, so the question is asked of every one of them.
  for (const n of nodes) {
    if (n.ignored || !/^(link|button|tab|checkbox|switch|textbox|combobox|menuitem)$/.test(roleOf(n))) continue;
    if (nameOf(n).trim()) continue;
    const at = (n.backendDOMNodeId || 0);
    bad.push(`a ${roleOf(n)} with no name (#${at})`);
  }
  if (bad.length) note(`a11y-tree/${screen}@${w}`, 'namelessControls', [...new Set(bad)].slice(0, 6));
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

// Reduced motion must reduce MOVEMENT, and still let the page say something changed.
//
// The old form of this check asked for no duration at all, which is a different rule — the guide
// asks for subtle fades in place of sliding and scaling, not for a page that swaps its contents
// between two frames. So: nothing may be displaced, and the fades that remain must be short.
{
  const page = await browser.newPage({ reducedMotion: 'reduce' });
  await page.goto(URL, { waitUntil: 'networkidle' }); await page.waitForTimeout(900);
  const bad = await page.evaluate(() => {
    const out = [];
    for (const e of document.querySelectorAll('*')) {
      if (!e.getClientRects().length) continue;
      const c = getComputedStyle(e);
      const name = e.tagName.toLowerCase() + (e.id ? '#' + e.id : '');
      if (/transform|left|top|inset|margin|translate/.test(c.transitionProperty) &&
          parseFloat(c.transitionDuration) > 0.05) out.push(name + ' 이동 transition:' + c.transitionProperty);
      const dur = parseFloat(c.animationDuration);
      if (dur > 0.05 && dur > 0.2) out.push(name + ' 애니메이션 ' + c.animationDuration);
    }
    return [...new Set(out)].slice(0, 6);
  });
  if (bad.length) note('reduced-motion', 'stillMoving', bad);
  await page.close();
}
await browser.close();

if (!findings.length) console.log('검증 항목 전부 통과 — 지적할 것 없음');
for (const f of findings) console.log(`⚠ ${f.screen.padEnd(20)} ${f.rule}\n     ${f.detail.join('\n     ')}`);
process.exitCode = findings.length ? 1 : 0;
