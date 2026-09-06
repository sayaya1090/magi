/**
 * 마크다운 → DOM. **마크업을 읽는 길 없이**(마크업을 읽는 함수 없이, smoke 가 잰다) 노드를 직접 짓는다.
 *
 * 왜 있나: 모델의 답과 카운슬 판정, 플러그인이 넣는 줄은 마크다운으로 오는데 창은 그것을 글자
 * 그대로 보였다 — `**굵게**`, `|---|`, 백틱이 화면에 찍혔다(사용자 지적 2026-09-05). 이 창이
 * 다루는 것은 제목·문단·굵게/기울임/코드·코드 블록·목록·표·가로줄·http 링크까지다. 그 밖의
 * 것(원시 HTML, 그림, 각주)은 글자로 남긴다 — 못 그리는 것을 지우는 것보다 낫다.
 *
 * 두 층이다. `parseMd` 는 순수 함수라 노드 없이 시험하고, `mdToDom` 은 그 결과를 `document` 로
 * 짓는다. 파서를 고치면 시험이 울리고, 짓는 쪽은 열 줄이라 눈으로 본다.
 */

/** 인라인: 텍스트·코드·굵게·기울임·링크. 중첩은 굵게/기울임 안의 코드까지만. */
export function inlines(text) {
  const out = [];
  let s = String(text ?? '');
  const push = (t, v, extra) => { if (v !== '') out.push({ t, text: v, ...(extra ?? {}) }); };
  let buf = '';
  let i = 0;
  const flush = () => { push('text', buf); buf = ''; };
  while (i < s.length) {
    const rest = s.slice(i);
    let m;
    if ((m = /^`([^`]+)`/.exec(rest))) { flush(); push('code', m[1]); i += m[0].length; continue; }
    // 모델이 표 셀 안 줄바꿈을 <br> 로 쓴다(실물 2026-09-05). 원시 HTML 은 안 읽지만 이것 하나는 줄바꿈이다.
    if ((m = /^<br\s*\/?>/i.exec(rest))) { flush(); out.push({ t: 'br' }); i += m[0].length; continue; }
    if ((m = /^\*\*(.+?)\*\*/.exec(rest))) { flush(); out.push({ t: 'strong', kids: inlines(m[1]) }); i += m[0].length; continue; }
    if ((m = /^(?:\*|_)([^*_\n]+?)(?:\*|_)(?![\w가-힣])/.exec(rest))) { flush(); out.push({ t: 'em', kids: inlines(m[1]) }); i += m[0].length; continue; }
    if ((m = /^\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/.exec(rest))) { flush(); push('link', m[1], { href: m[2] }); i += m[0].length; continue; }
    buf += s[i]; i += 1;
  }
  flush();
  return out;
}

const TABLE_SEP = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;
const splitRow = (line) => {
  let l = line.trim();
  if (l.startsWith('|')) l = l.slice(1);
  if (l.endsWith('|')) l = l.slice(0, -1);
  return l.split('|').map((c) => c.trim());
};

/** 블록: heading · para · code · list · table · hr. 줄 단위로 읽는다. */
export function parseMd(text) {
  const lines = String(text ?? '').replace(/\r\n?/g, '\n').split('\n');
  const blocks = [];
  let i = 0;
  let para = [];
  const endPara = () => { if (para.length) { blocks.push({ t: 'para', kids: inlines(para.join('\n')) }); para = []; } };
  while (i < lines.length) {
    const line = lines[i];
    let m;
    if ((m = /^\s*```\s*([\w+-]*)\s*$/.exec(line))) {
      endPara();
      const code = [];
      i += 1;
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) { code.push(lines[i]); i += 1; }
      i += 1;
      blocks.push({ t: 'code', lang: m[1] || '', text: code.join('\n') });
      continue;
    }
    if ((m = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line))) {
      endPara(); blocks.push({ t: 'heading', level: m[1].length, kids: inlines(m[2]) }); i += 1; continue;
    }
    if (/^\s{0,3}(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) { endPara(); blocks.push({ t: 'hr' }); i += 1; continue; }
    if (line.includes('|') && i + 1 < lines.length && TABLE_SEP.test(lines[i + 1])) {
      endPara();
      const head = splitRow(line).map(inlines);
      i += 2;
      const rows = [];
      while (i < lines.length && lines[i].includes('|') && lines[i].trim() !== '') { rows.push(splitRow(lines[i]).map(inlines)); i += 1; }
      blocks.push({ t: 'table', head, rows });
      continue;
    }
    if ((m = /^\s{0,3}([-*+]|\d+[.)])\s+(.*)$/.exec(line))) {
      endPara();
      const ordered = /\d/.test(m[1]);
      const items = [];
      while (i < lines.length && (m = /^\s{0,3}([-*+]|\d+[.)])\s+(.*)$/.exec(lines[i])) && /\d/.test(m[1]) === ordered) {
        let item = m[2];
        i += 1;
        // 들여쓴 이어지는 줄은 같은 항목이다.
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !/^\s{0,3}([-*+]|\d+[.)])\s+/.test(lines[i])) { item += '\n' + lines[i].trim(); i += 1; }
        items.push(inlines(item));
      }
      blocks.push({ t: 'list', ordered, items });
      continue;
    }
    if (line.trim() === '') { endPara(); i += 1; continue; }
    para.push(line);
    i += 1;
  }
  endPara();
  return blocks;
}

/** 파스 결과를 노드로. `document` 를 받아 시험에서 가짜를 넣을 수 있다. */
export function mdToDom(document, text) {
  const root = document.createElement('div');
  root.className = 'md';
  const put = (parent, kids) => {
    for (const k of kids) {
      if (k.t === 'text') { parent.append(document.createTextNode(k.text)); continue; }
      if (k.t === 'code') { const c = document.createElement('code'); c.textContent = k.text; parent.append(c); continue; }
      if (k.t === 'br') { parent.append(document.createElement('br')); continue; }
      if (k.t === 'link') {
        const a = document.createElement('a'); a.textContent = k.text; a.href = k.href; a.target = '_blank'; a.rel = 'noopener';
        parent.append(a); continue;
      }
      const el = document.createElement(k.t === 'strong' ? 'strong' : 'em');
      put(el, k.kids ?? []);
      parent.append(el);
    }
  };
  for (const b of parseMd(text)) {
    if (b.t === 'heading') { const h = document.createElement(`h${Math.min(b.level + 2, 6)}`); put(h, b.kids); root.append(h); }
    else if (b.t === 'para') { const p = document.createElement('p'); put(p, b.kids); root.append(p); }
    else if (b.t === 'code') { const pre = document.createElement('pre'); const c = document.createElement('code'); c.textContent = b.text; pre.append(c); root.append(pre); }
    else if (b.t === 'hr') { root.append(document.createElement('hr')); }
    else if (b.t === 'list') {
      const l = document.createElement(b.ordered ? 'ol' : 'ul');
      for (const it of b.items) { const li = document.createElement('li'); put(li, it); l.append(li); }
      root.append(l);
    } else if (b.t === 'table') {
      const t = document.createElement('table');
      const thead = document.createElement('thead'); const hr = document.createElement('tr');
      for (const c of b.head) { const th = document.createElement('th'); put(th, c); hr.append(th); }
      thead.append(hr); t.append(thead);
      const tb = document.createElement('tbody');
      for (const r of b.rows) { const tr = document.createElement('tr'); for (const c of r) { const td = document.createElement('td'); put(td, c); tr.append(td); } tb.append(tr); }
      t.append(tb); root.append(t);
    }
  }
  return root;
}

/** 마크다운 표식이 하나라도 있나 — 없으면 굳이 파서를 안 거친다. */
export function looksLikeMd(text) {
  return /(\*\*|`|^#{1,6}\s|^\s*[-*+]\s|^\s*\d+[.)]\s|\|.*\|)/m.test(String(text ?? ''));
}
