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

  // Property names declared directly in a block body (no nested blocks in it).
  const names = body => { const out = []; let name = '', paren = 0;
    for (let i = 0; i < body.length; i++) { const k = skip(body, i); if (k > 0) { i = k - 1; continue; }
      const c = body[i];
      if (c === '(') { paren++; name += c; } else if (c === ')') { paren--; name += c; }
      else if (c === ':' && !paren) { out.push(name.trim()); name = '';
        for (; i < body.length; i++) { const k2 = skip(body, i); if (k2 > 0) { i = k2 - 1; continue; }
          if (body[i] === '(') paren++; else if (body[i] === ')') paren--;
          else if (body[i] === ';' && !paren) break; } }
      else if (c === ';') name = ''; else name += c; }
    return out.filter(n => n && !/[{}]/.test(n)); };

  // @font-face and friends hold descriptors, not properties; CSS.supports does not know them.
  const DESCRIPTOR_AT = /^@(font-face|font-feature-values|counter-style|property|page|viewport)/;
  const out = [];
  for (const el of document.querySelectorAll('style')) {
    const src = el.textContent, unknown = [];
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
          for (const n of names(body)) {
            if (n.startsWith('--') || CSS.supports(n, 'initial')) continue;
            unknown.push(n.replace(/[ \t\n]+/g, ' ').slice(0, 64));
          }
        }
        i = end;
      }
    })(0, src.length);
    for (let i = 0; i < src.length; i++) { const k = skip(src, i); if (k > 0) { i = k - 1; continue; }
      if (src[i] === '{') balance++; else if (src[i] === '}') balance--; }
    out.push({ balance, rules: el.sheet ? el.sheet.cssRules.length : -1,
               unknown: [...new Set(unknown)] });
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
  } else console.log(`${tag}규칙 ${s.rules}개 · 모든 프로퍼티 이름이 유효`);
});
process.exitCode = bad ? 1 : 0;
await browser.close();
