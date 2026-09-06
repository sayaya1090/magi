/**
 * 브라우저 목업의 「문서」 — 문단을 세로로 그린다. Word 자리를 대신할 뿐이고 애드인에는 아무 뜻도 없다.
 * 문단을 누르면 선택이 그 문단으로 간다(Shift 로 범위).
 */
export function mountFakeCanvas(doc, root) {
  if (!root) return;
  const page = document.createElement('div');
  page.className = 'page';
  root.appendChild(page);
  const draw = () => {
    page.replaceChildren();
    doc.model.paragraphs.forEach((p, i) => {
      const n = i + 1;
      const el = document.createElement('div');
      el.className = `para style-${String(p.style || 'Normal').replace(/\s+/g, '-')}` + (n >= doc.from && n <= doc.to ? ' selected' : '');
      el.dataset.n = String(n);
      el.textContent = (p.list ? '• ' : '') + p.text;
      el.title = `문단 ${n} · ${p.style}`;
      el.addEventListener('click', (ev) => { if (ev.shiftKey) doc.select(Math.min(doc.from, n), Math.max(doc.to, n)); else doc.select(n); });
      page.appendChild(el);
      const t = doc.model.tables.find((x) => x.after === n);
      if (t) {
        const table = document.createElement('table'); table.className = 'ftable';
        t.values.forEach((row, r) => { const tr = document.createElement('tr'); row.forEach((c) => { const td = document.createElement(r === 0 && t.hasHeader ? 'th' : 'td'); td.textContent = c; tr.appendChild(td); }); table.appendChild(tr); });
        page.appendChild(table);
      }
    });
  };
  draw();
  doc.onChange(draw);
}
