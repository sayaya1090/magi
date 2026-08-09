// Did the browser accept every declaration in the stylesheet?
//
// A malformed rule does not throw and does not log: the parser discards what it cannot read and
// keeps going, so a broken block is indistinguishable from a block whose styles simply did not
// apply. Twice now a stray */ and a backtick have eaten a declaration and reached the browser
// through a green gate, found only because someone happened to measure the one box it moved.
//
// The check is NOT source-against-CSSOM. A probe that parses the text makes the same mistake the
// parser made, and the two agree about a rule neither read correctly — measured: that version
// stayed silent on the very defect it was written for. What survives the parser's mistake is the
// NAME: a stray */ turns prose into a declaration, and prose does not name a CSS property. So ask
// whether every name declared anywhere is one the browser has. Typos answer to this too.
import { chromium } from 'playwright';

const URL = process.env.DEMO_URL || 'http://localhost:8765/';
const browser = await chromium.launch();
const page = await browser.newPage();
await page.goto(URL, { waitUntil: 'networkidle' });

const sheets = await page.evaluate(() => {
  const skip = (s, i) => {                       // past a comment or a quoted string
    if (s[i] === '/' && s[i + 1] === '*') { const e = s.indexOf('*/', i + 2); return e < 0 ? s.length : e + 2; }
    if (s[i] === '"' || s[i] === "'") { const q = s[i]; let j = i + 1;
      while (j < s.length && s[j] !== q) j += s[j] === '\\' ? 2 : 1; return j + 1; }
    return -1;
  };
  const close = (s, at) => { let d = 0;          // index of the } matching the { at `at`
    for (let i = at; i < s.length; i++) { const k = skip(s, i); if (k > 0) { i = k - 1; continue; }
      if (s[i] === '{') d++; else if (s[i] === '}' && !--d) return i; } return s.length; };

  // Declarations (name and value) written directly in a block body, which has no nested blocks.
  const decls = body => { const out = []; let name = '', paren = 0;
    for (let i = 0; i < body.length; i++) { const k = skip(body, i); if (k > 0) { i = k - 1; continue; }
      const c = body[i];
      if (c === '(') { paren++; name += c; } else if (c === ')') { paren--; name += c; }
      else if (c === ':' && !paren) { const n = name.trim(); name = ''; const from = i + 1;
        for (i++; i < body.length; i++) { const k2 = skip(body, i); if (k2 > 0) { i = k2 - 1; continue; }
          if (body[i] === '(') paren++; else if (body[i] === ')') paren--;
          else if (body[i] === ';' && !paren) break; }
        out.push([n, body.slice(from, i).trim()]); }
      else if (c === ';') name = ''; else name += c; }
    return out.filter(([n]) => n && !/[{}]/.test(n)); };

  // @font-face and friends hold descriptors, not properties; CSS.supports does not know them.
  const DESCRIPTOR_AT = /^@(font-face|font-feature-values|counter-style|property|page|viewport)/;
  const out = [];
  for (const el of document.querySelectorAll('style')) {
    const src = el.textContent, unknown = [], bogus = [];
    let balance = 0;
    (function walk(from, to) {
      for (let i = from; i < to; i++) {
        const k = skip(src, i); if (k > 0) { i = k - 1; continue; }
        if (src[i] !== '{') continue;
        const end = close(src, i), body = src.slice(i + 1, end);
        const prelude = src.slice(src.lastIndexOf('}', i - 1) + 1, i).replace(/\/\*[\s\S]*?\*\//g, '').trim();
        const nested = /\{/.test(body.replace(/\/\*[\s\S]*?\*\//g, ''));
        if (nested) walk(i + 1, end);
        else if (!DESCRIPTOR_AT.test(prelude)) {
          for (const [n, v] of decls(body)) {
            if (n.startsWith('--')) continue;                      // a custom property takes anything
            if (!CSS.supports(n, 'initial')) { unknown.push(n.replace(/[ \t\n]+/g, ' ').slice(0, 64)); continue; }
            // The name can be real and the value still dead: -var(--x) is not a negative length,
            // and the browser drops the declaration without a word. Four of those shipped in one
            // commit because a substitution left the minus sign outside the var it moved.
            const bare = v.replace(/!\s*important\s*$/, '').trim();   // supports() rejects the flag
            if (bare && !CSS.supports(n, bare)) bogus.push((n + ':' + bare).replace(/[ \t\n]+/g, ' ').slice(0, 72));
          }
        }
        i = end;
      }
    })(0, src.length);
    for (let i = 0; i < src.length; i++) { const k = skip(src, i); if (k > 0) { i = k - 1; continue; }
      if (src[i] === '{') balance++; else if (src[i] === '}') balance--; }
    // A var() that resolves to nothing and has no fallback. The value is syntactically fine, so
    // CSS.supports says yes and the declaration is kept — and then it computes to nothing and the
    // element takes whatever it inherits. A rename that missed one file left a banner without a
    // font this way: valid, parsed, and 29px taller than it had been.
    const dangling = [];
    const rootStyle = getComputedStyle(document.documentElement);
    for (const m of src.matchAll(/var\(\s*(--[a-zA-Z0-9-]+)\s*([,)])/g)) {
      const [, name, next] = m;
      if (next === ',') continue;                       // has a fallback, so nothing is lost
      if (rootStyle.getPropertyValue(name).trim()) continue;
      // It may be defined on a subtree rather than the root; ask the elements that use it.
      let found = false;
      for (const el of document.querySelectorAll('*')) {
        if (getComputedStyle(el).getPropertyValue(name).trim()) { found = true; break; }
      }
      if (!found) dangling.push(name);
    }
    out.push({ balance, rules: el.sheet ? el.sheet.cssRules.length : -1,
               unknown: [...new Set(unknown)], bogus: [...new Set(bogus)],
               dangling: [...new Set(dangling)] });
  }
  return out;
});

let bad = 0;
sheets.forEach((s, i) => {
  const tag = sheets.length > 1 ? `<style ${i}> ` : '';
  if (s.balance !== 0) { console.log(`⚠ ${tag}중괄호 불균형 ${s.balance}`); bad++; }
  if (s.unknown.length) { bad++;
    console.log(`⚠ ${tag}CSS가 모르는 프로퍼티 ${s.unknown.length}개 — 오타이거나 주석이 깨진 것:`);
    s.unknown.forEach(n => console.log('   ', JSON.stringify(n)));
  }
  if (s.bogus.length) { bad++;
    console.log(`⚠ ${tag}브라우저가 버리는 값 ${s.bogus.length}개:`);
    s.bogus.forEach(n => console.log('   ', n));
  }
  if (s.dangling && s.dangling.length) { bad++;
    console.log(`⚠ ${tag}정의되지 않은 var() ${s.dangling.length}개 (폴백도 없음):`);
    s.dangling.forEach(n => console.log('   ', n));
  }
  if (!s.unknown.length && !s.bogus.length && !(s.dangling || []).length)
    console.log(`${tag}규칙 ${s.rules}개 · 모든 선언이 유효`);
});
process.exitCode = bad ? 1 : 0;
await browser.close();
