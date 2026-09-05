/** 브라우저 목업의 시트 격자 — 셀을 클릭해 잡고(Shift 로 범위) 「선택 인용」을 눌러 본다. */
import { colName, cellName, rangeName } from '../adapter/a1.js';

export function mountFakeCanvas(book, root) {
  const host = document.createElement('div');
  root.append(host);
  let anchor = null;
  const render = () => {
    host.replaceChildren();
    const strip = document.createElement('div');
    strip.className = 'strip';
    for (const s of book.model.sheets) {
      const b = document.createElement('button');
      b.className = 'thumb' + (s.name === book.currentSheet ? ' on' : '');
      b.textContent = s.name;
      b.addEventListener('click', () => book.goTo(s.name));
      strip.append(b);
    }
    const sheet = book.sheet(book.currentSheet);
    const table = document.createElement('table');
    table.className = 'grid';
    const rows = 10; const cols = 6;
    const head = document.createElement('tr');
    head.append(document.createElement('th'));
    for (let c = 0; c < cols; c += 1) { const th = document.createElement('th'); th.textContent = colName(c); head.append(th); }
    table.append(head);
    const sel = book.selected;
    for (let r = 0; r < rows; r += 1) {
      const tr = document.createElement('tr');
      const th = document.createElement('th'); th.textContent = String(r + 1); tr.append(th);
      for (let c = 0; c < cols; c += 1) {
        const td = document.createElement('td');
        const name = cellName(r, c);
        const cell = sheet.cells[name];
        td.textContent = cell?.f ?? (cell?.v ?? '');
        if (inRange(sel, r, c)) td.className = 'sel';
        td.addEventListener('click', (e) => {
          if (e.shiftKey && anchor) {
            const top = Math.min(anchor.r, r); const left = Math.min(anchor.c, c);
            book.select(rangeName(top, left, Math.abs(anchor.r - r) + 1, Math.abs(anchor.c - c) + 1));
          } else { anchor = { r, c }; book.select(name); }
        });
        tr.append(td);
      }
      table.append(tr);
    }
    const hint = document.createElement('p');
    hint.className = 'hint';
    hint.textContent = '셀을 클릭해 잡고(Shift 로 범위) 오른쪽에서 「선택 인용」을 누릅니다. 진짜 Excel 에서는 이 자리가 시트입니다.';
    host.append(strip, table, hint);
  };
  book.onChange(render);
  render();
}
function inRange(address, r, c) {
  const m = /^([A-Z]+)(\d+)(?::([A-Z]+)(\d+))?$/.exec(address ?? '');
  if (!m) return false;
  const col = (s) => { let n = 0; for (const ch of s) n = n * 26 + (ch.charCodeAt(0) - 64); return n - 1; };
  const c1 = col(m[1]); const r1 = Number(m[2]) - 1; const c2 = m[3] ? col(m[3]) : c1; const r2 = m[4] ? Number(m[4]) - 1 : r1;
  return r >= Math.min(r1, r2) && r <= Math.max(r1, r2) && c >= Math.min(c1, c2) && c <= Math.max(c1, c2);
}
