// Measure the console in a real browser, without a person or a browser extension.
//
// # Why this exists
//
// Most of what this page spends its code on cannot be seen from the tests: the fleet poll, the
// board rebuilding itself, the transcript arriving a frame at a time, a tooltip appearing on focus,
// a live region announcing a change. render_test.go runs the module against a FAKE dom, which has
// diverged from a browser six times and always in the direction that hides the bug. So a whole
// class of work — anything about layout, timing, or what a browser actually computes — could only
// be reasoned about.
//
// This runs the emitted demo in headless Chromium and reads the real computed box. The demo's mock
// moves on a timer, so the live parts are watchable rather than frozen.
//
// # Use
//
//     go run ./cmd/magi-web -emit-demo /tmp/demo
//     (cd /tmp/demo && python3 -m http.server 8765 &)
//     cd scratchpad && npm i playwright@1.62 && npx playwright install chromium
//     node measure.mjs                 # the default probes
//     node measure.mjs '.wcard'        # or any selector
//
// ⚠ Re-emit after every page.go change. The demo is a snapshot — measuring a stale one is how a
// fix looks like it did nothing.
import { chromium } from 'playwright';

const sel = process.argv[2];
const URL = process.env.DEMO_URL || 'http://localhost:8765/';

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });

// Errors are the point as much as the numbers. An undefined helper called from a handler the tests
// never invoke shows up here and nowhere else.
const errs = [];
page.on('pageerror', e => errs.push('page: ' + e));
page.on('console', m => { if (m.type() === 'error') errs.push('console: ' + m.text()); });

await page.goto(URL, { waitUntil: 'networkidle' });
await page.waitForTimeout(2500);   // let the mock tick at least once

const box = (s) => page.evaluate((s) => [...document.querySelectorAll(s)].map(e => {
  const r = e.getBoundingClientRect(), cs = getComputedStyle(e);
  return {
    text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 40),
    w: +r.width.toFixed(1), h: +r.height.toFixed(1),
    font: cs.fontSize + '/' + cs.lineHeight, pad: cs.padding, radius: cs.borderRadius,
  };
}), s);

if (sel) {
  console.log(sel, JSON.stringify(await box(sel), null, 1));
} else {
  // The probes worth having by default: the ones with a number in the spec.
  for (const s of ['md-filter-chip', '#rail', '#tabs md-primary-tab', 'md-outlined-text-field',
                   '.wcard', '#plan md-linear-progress']) {
    const got = await box(s);
    console.log(s.padEnd(26), got.length ? JSON.stringify(got[0]) : '(none on this screen)');
  }
  // Structure this page has been wrong about before.
  console.log('\nstructure', JSON.stringify(await page.evaluate(() => ({
    headings: [...document.querySelectorAll('h1,h2,h3,h4,h5,h6')].map(h => h.tagName + ':' + h.textContent.trim().slice(0, 18)),
    navsWithoutLabel: [...document.querySelectorAll('nav')].filter(n => !n.getAttribute('aria-label')).length,
    liveRegions: document.querySelectorAll('[aria-live]').length,
    tabindexed: document.querySelectorAll('[tabindex]').length,
  })), null, 1));
}

console.log('\nERRORS:', errs.length ? errs.slice(0, 8) : 'none');
await browser.close();
