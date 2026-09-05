/**
 * A1 주소 산수 — 가짜 손과 가짜 통합 문서가 같이 쓴다. 진짜 Excel 은 제 것으로 계산하므로 여기 값은
 * 목업·시험용 사실이다. 열 이름은 A..Z, AA..ZZ …; 행은 1부터.
 */
export function colIndex(letters) {
  let n = 0;
  for (const ch of letters.toUpperCase()) n = n * 26 + (ch.charCodeAt(0) - 64);
  return n - 1; // 0-based
}
export function colName(index) {
  let n = index + 1; let s = '';
  while (n > 0) { const r = (n - 1) % 26; s = String.fromCharCode(65 + r) + s; n = Math.floor((n - 1) / 26); }
  return s;
}
export function cellName(row, col) { return `${colName(col)}${row + 1}`; }
/** "B2:E9" → { top, left, rows, cols } (0-based top/left). 열 전체·행 전체는 넉넉한 상한으로 편다. */
export function parseAddress(address) {
  const a = String(address ?? '').trim().replace(/^\$/, '').replace(/\$/g, '');
  if (!a) throw new Error('주소가 비었습니다');
  const [p, q] = a.split(':');
  const cell = /^([A-Za-z]{1,3})(\d+)$/; const col = /^([A-Za-z]{1,3})$/; const row = /^(\d+)$/;
  const one = (s) => {
    let m;
    if ((m = cell.exec(s))) return { r: Number(m[2]) - 1, c: colIndex(m[1]) };
    if ((m = col.exec(s))) return { r: null, c: colIndex(m[1]) };
    if ((m = row.exec(s))) return { r: Number(m[1]) - 1, c: null };
    throw new Error(`주소를 못 읽었습니다: ${address}`);
  };
  const s = one(p); const e = q ? one(q) : s;
  const top = s.r ?? 0; const left = s.c ?? 0;
  const bottom = e.r ?? (s.r ?? 999); const right = e.c ?? (s.c ?? 25);
  if (bottom < top || right < left) throw new Error(`주소가 거꾸로입니다: ${address}`);
  return { top, left, rows: bottom - top + 1, cols: right - left + 1 };
}
export function rangeName(top, left, rows, cols) {
  const a = cellName(top, left);
  if (rows === 1 && cols === 1) return a;
  return `${a}:${cellName(top + rows - 1, left + cols - 1)}`;
}
