// 엑셀 76개 도구 전수 스윕 — 헬퍼(https://127.0.0.1:3000/xl)에 붙은 **첫 통장**에 순서대로 다 불러 보고 표로 낸다.
//
//   node clients/excel/addin/tools/sweep.mjs [--deck <wb-…>] [--xlsx <다른 통장.xlsx>] [--origin https://127.0.0.1:3000/xl]
//
// 「스윕」시트를 만들어 그 안에서만 놀고 끝에 지운다 — 원본 시트는 안 건드린다(통장 속성·메모·제안은 되돌린다).
// 토큰은 헬퍼 페이지에서, 통장은 /api/documents 에서 얻는다. 그림 답은 이 파일 옆 sweep_<도구>.png 로 떨어진다.
// insert_sheets_from_file 은 --xlsx 로 준 파일을 넣는다(없으면 건너뛰고 그렇게 적는다).
// 실측 2026-09-07(Office LTSC 2021 · 창이 손): 76/76 · 101호출, 오류는 resolve_comment{resolved} 하나(2021 은 메모 대신 COM 노트라 해결 표시가 없다) — 피벗은 표의 열을 고치기 앞에
// 만들어 2021 의 버릇을 피한다(docs/TESTING.ko.md §5.10). 365 에서는 전부 통과가 기대값이다.
import { writeFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
const here = dirname(fileURLToPath(import.meta.url));
const opt = { deck: '', xlsx: '', origin: 'https://127.0.0.1:3000/xl' };
for (let i = 2; i < process.argv.length; i += 2) { const k = process.argv[i].replace(/^--/, ''); if (k in opt) opt[k] = process.argv[i + 1] ?? ''; }
const page = await (await fetch(opt.origin + '/taskpane.html')).text();
const m = page.match(/token[^a-zA-Z0-9]{1,6}([A-Za-z0-9_-]{16,})/);
if (!m) { console.log('헬퍼 페이지에서 토큰을 못 찾았다 — 헬퍼가 떠 있나?'); process.exit(2); }
const TOK = m[1];
const H = { authorization: 'Bearer ' + TOK, 'content-type': 'application/json' };
const docs = (await (await fetch(opt.origin + '/api/documents', { headers: H })).json()).documents ?? [];
const DECK = opt.deck || docs[0]?.document || '';
if (!DECK) { console.log('붙은 작업창이 없다 — Excel 에서 magi 작업창을 열어라'); process.exit(2); }
console.log('workbook', DECK, 'of', docs.map((d) => d.document).join(','));
const IMG = join(here, 'sweep-image.png');
if (!existsSync(IMG)) writeFileSync(IMG, Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64'));
const CSV = join(here, 'sweep.csv');
writeFileSync(CSV, '이름,수량\n감,7\n귤,9\n');
const S = '스윕';
const rows = []; const done = new Set(); const skipped = [];
async function call(name, args, note = '') {
  const t0 = Date.now(); let resp;
  try {
    const r = await fetch(`${opt.origin}/mcp?deck=${DECK}`, { method: 'POST', headers: H, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } }) });
    resp = await r.json();
  } catch (e) { rows.push([name, 'ERR', ('transport: ' + e.message).slice(0, 120), note, 0]); return null; }
  if (resp.error) { rows.push([name, 'ERR', ('rpc: ' + JSON.stringify(resp.error)).slice(0, 120), note, (Date.now() - t0) / 1000]); return null; }
  const res = resp.result ?? {}; const err = !!res.isError;
  const txt = (res.content ?? []).filter((b) => b.type === 'text').map((b) => b.text ?? '').join('');
  const img = (res.content ?? []).filter((b) => b.type === 'image');
  let x; try { x = JSON.parse(txt); } catch { x = { _raw: txt }; }
  if (img.length) writeFileSync(join(here, `sweep_${name}.png`), Buffer.from(img[0].data, 'base64'));
  let summary = (x && typeof x === 'object' && Array.isArray(x.changed) && x.changed.length ? x.changed.join(' | ') : txt).slice(0, 120).replace(/\n/g, ' ');
  if (img.length) summary = `image ${Math.floor(img[0].data.length / 1024)}KB ` + summary;
  rows.push([name, err ? 'ERR' : 'ok', summary, note, (Date.now() - t0) / 1000]); done.add(name);
  return err ? null : x;
}
const sheetNames = async (note) => ((await call('list_sheets', {}, note)) ?? {}).sheets?.map((s) => s.name ?? s) ?? [];

const names = await sheetNames('');
if (names.includes(S)) await call('delete_sheet', { sheet: S }, '지난 스윕 정리');
await call('describe_sheet', { sheet: names[0] }, names[0]);
await call('describe_style', {}, '');
await call('add_sheet', { name: S }, '');
await call('write_range', { sheet: S, address: 'A1', values: [['이름', '수량', '단가'], ['감', 7, 1200], ['귤', 9, 800], ['감', 7, 1200], ['배', 5, 1500]] }, '4행');
await call('write_range', { sheet: S, address: 'D1', values: [['금액']] }, '머리');
await call('write_range', { sheet: S, address: 'D2', formulas: [['=B2*C2']] }, '수식');
await call('fill_range', { sheet: S, address: 'D2', to: 'D2:D5', fill: 'copy' }, 'D2→D5');
await call('read_range', { sheet: S }, 'used');
await call('find', { sheet: S, text: '감' }, '');
await call('replace_all', { sheet: S, find: '배', replace: '참외', whole_cell: true }, '배→참외');
await call('copy_range', { sheet: S, source: 'A1:D5', address: 'F1', mode: 'values' }, 'A1:D5→F1');
await call('remove_duplicates', { sheet: S, address: 'F1:I5' }, '감 중복');
await call('set_number_format', { sheet: S, address: 'C2:D5', format: '#,##0' }, '');
await call('format_range', { sheet: S, address: 'A1:D1', bold: true, fill: '#1E3A8A', color: '#FFFFFF', align: 'Center', borders: '#94A3B8' }, '머리행');
await call('set_cell_style', { sheet: S, address: 'F1:I1', style: 'Heading2' }, '');
await call('autofit', { sheet: S }, '');
await call('merge_cells', { sheet: S, address: 'K1:M1' }, '');
await call('unmerge_cells', { sheet: S, address: 'K1:M1' }, '');
await call('insert_cells', { sheet: S, address: '7:7', shift: 'down' }, '7행');
await call('delete_cells', { sheet: S, address: '7:7', shift: 'up' }, '7행');
await call('set_hyperlink', { sheet: S, address: 'K3', url: 'https://example.com', text: '예시', screen_tip: '예' }, '');
const snap = (await call('snapshot_range', { sheet: S, address: 'A1:D5' }, '')) ?? {};
const snapId = snap.snapshot ?? snap.id ?? snap.snapshot_id;
await call('write_range', { sheet: S, address: 'B2', values: [[999]] }, '망치기');
if (snapId) await call('restore_range', { snapshot: snapId }, String(snapId));
// 피벗은 표의 열을 고치기 **전에** 만든다 — 2021 은 열을 붙이거나 지운 시트의 범위로는 피벗을 못 만든다(TESTING §5.10).
await call('add_pivot', { sheet: S, source: `${S}!F1:I4`, destination: 'K10', rows: ['이름'], values: [{ field: '수량', function: 'Sum' }], name: '스윕피벗' }, 'F1:I4→K10');
await call('add_table', { sheet: S, address: 'A1:D5', name: '스윕표', table_style: 'TableStyleMedium2' }, '');
await call('read_table', { table: '스윕표' }, '');
await call('set_table_cells', { table: '스윕표', cells: [{ row: 0, column: '수량', value: 8 }] }, '');
await call('add_table_rows', { table: '스윕표', rows: [['대추', 2, 500, null]] }, '');
await call('edit_table', { table: '스윕표', add_columns: ['비고'], show_totals: true }, '열+합계');
await call('sort_range', { table: '스윕표', by: [{ column: '수량', ascending: false }] }, '수량 내림');
await call('filter_table', { table: '스윕표', column: '이름', values: ['감', '귤'] }, '감·귤');
await call('filter_table', { table: '스윕표', column: '이름', values: [] }, '해제');
await call('refresh_pivot', { sheet: S, name: '스윕피벗' }, '');
await call('add_chart', { sheet: S, source: 'A1:B6', chart_type: 'ColumnClustered', name: '스윕차트', title: '스윕', left: 420, top: 160, width: 300, height: 200 }, '');
await call('read_chart', { sheet: S, chart: '스윕차트' }, '');
await call('format_chart', { sheet: S, chart: '스윕차트', legend: 'Bottom', data_labels: true, x_title: '이름', y_title: '수량' }, '');
await call('render_chart', { sheet: S, chart: '스윕차트', max_width: 400 }, '');
await call('render_range', { sheet: S, address: 'A1:D7', max_width: 400 }, '');
await call('add_conditional_format', { sheet: S, address: 'B2:B6', cf_kind: 'cell_value', operator: 'GreaterThan', value: '5', fill: '#FEF3C7' }, '>5');
await call('read_conditional_formats', { sheet: S, address: 'B2:B6' }, '');
await call('clear_conditional_formats', { sheet: S, address: 'B2:B6' }, '');
await call('set_validation', { sheet: S, address: 'A2:A6', validation_kind: 'list', values: ['감', '귤', '참외', '대추'] }, 'list');
await call('read_validation', { sheet: S, address: 'A2:A6' }, '');
await call('set_name', { name: '스윕수량', refers_to: `='${S}'!$B$2:$B$6` }, '');
await call('read_names', {}, '');
await call('delete_name', { name: '스윕수량' }, '');
await call('add_comment', { sheet: S, address: 'A2', text: '스윕 댓글' }, '2021: 노트로');
await call('add_comment', { sheet: S, address: 'A2', text: '답글 시험' }, '2021: 덧붙임');
await call('read_comments', { sheet: S }, '2021: 노트');
await call('resolve_comment', { sheet: S, address: 'A2' }, '해결(2021 노트는 거절)');
await call('resolve_comment', { sheet: S, address: 'A2', delete: true }, '삭제');
await call('add_image', { sheet: S, path: IMG, address: 'K5', alt: '스윕 그림', width: 40, height: 40 }, 'png');
await call('trace_cell', { sheet: S, address: 'D2', what: 'precedents' }, '');
await call('trace_cell', { sheet: S, address: 'B2', what: 'dependents' }, '');
await call('freeze_panes', { sheet: S, rows: 1 }, '1행');
await call('freeze_panes', { sheet: S }, '해제(둘 다 생략)');
await call('set_rows_columns', { sheet: S, columns: 'F:I', hidden: true }, '숨김');
await call('set_rows_columns', { sheet: S, columns: 'F:I', hidden: false, width: 60 }, '보임+너비');
await call('set_rows_columns', { sheet: S, rows: '2:3', group: true }, '묶기');
await call('set_rows_columns', { sheet: S, rows: '2:3', group: false }, '풀기');
await call('set_tab_color', { sheet: S, color: '#0284C7' }, '');
await call('set_sheet_view', { sheet: S, gridlines: false }, '');
await call('set_sheet_view', { sheet: S, gridlines: true }, '');
await call('set_workbook_properties', { title: '스윕' }, '');
await call('set_workbook_properties', { title: '' }, '되돌림');
await call('set_page_setup', { sheet: S, orientation: 'Landscape', fit_width: 1, fit_height: 0, print_area: 'A1:D7' }, '');
await call('protect_sheet', { sheet: S }, '');
await call('unprotect_sheet', { sheet: S }, '');
await call('protect_workbook', { protected: true }, '');
await call('protect_workbook', { protected: false }, '');
await call('rename_sheet', { sheet: S, name: S + '2' }, '');
await call('rename_sheet', { sheet: S + '2', name: S }, '되돌림');
await call('copy_sheet', { sheet: S, name: S + '복사' }, '');
await call('move_sheet', { sheet: S + '복사', to: 1 }, '→1');
await call('set_sheet_visibility', { sheet: S + '복사', visibility: 'Hidden' }, '');
await call('set_sheet_visibility', { sheet: S + '복사', visibility: 'Visible' }, '');
await call('delete_sheet', { sheet: S + '복사' }, '');
await call('activate_sheet', { sheet: S, address: 'A1' }, '');
let before = await sheetNames('csv 전');
await call('import_csv', { path: CSV }, '');
let after = await sheetNames('csv 후');
for (const n of after.filter((x) => !before.includes(x))) await call('delete_sheet', { sheet: n }, 'csv 시트 ' + n);
if (opt.xlsx) {
  before = await sheetNames('xlsx 전');
  await call('insert_sheets_from_file', { path: opt.xlsx }, '');
  after = await sheetNames('xlsx 후');
  for (const n of after.filter((x) => !before.includes(x))) await call('delete_sheet', { sheet: n }, 'xlsx 시트 ' + n);
} else skipped.push('insert_sheets_from_file(--xlsx 없음)');
await call('set_tag', { key: 'sweep', value: '1' }, '');
await call('read_tags', {}, '');
await call('set_tag', { key: 'sweep' }, '삭제');
await call('suggest', { sheet: S, address: 'A1', what: '제목을 굵게', why: '눈에 띄게', fix: { tool: 'format_range', args: { sheet: S, address: 'A1', bold: true } } }, '');
const sg = (await call('read_suggestions', {}, '')) ?? {};
let key = null;
for (const k of ['suggestions', 'items']) if (Array.isArray(sg[k]) && sg[k].length) { key = sg[k][0].key; break; }
if (key) await call('drop_suggestion', { key }, String(key));
await call('advise', { items: [{ message: '합계 확인', why: '수식이 없다', sheet: S, address: 'D6' }] }, '');
await call('clear_advice', {}, '');
await call('clear_range', { sheet: S, address: 'K1:M3', what: 'all' }, '');
await call('remove_table', { table: '스윕표' }, '');
await call('delete_chart', { sheet: S, chart: '스윕차트' }, '');
await call('delete_sheet', { sheet: S }, '');
await call('activate_sheet', { sheet: names[0] }, names[0]);
const lst = await (await fetch(`${opt.origin}/mcp?deck=${DECK}`, { method: 'POST', headers: H, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list' }) })).json();
const all = lst.result.tools.map((t) => t.name);
const missing = all.filter((t) => !done.has(t));
console.log(`호출 ${rows.length} · 도구 ${done.size}/${all.length} · 안 부른 것: ${JSON.stringify(missing)}${skipped.length ? ' · 건너뜀: ' + skipped.join(', ') : ''}`);
const errs = rows.filter((r) => r[1] === 'ERR');
console.log(`오류 ${errs.length} · 총 ${(rows.reduce((s, r) => s + r[4], 0)).toFixed(1)}s`);
for (const r of rows) console.log(`${r[1].padEnd(3)} ${r[0].padEnd(24)} ${String(r[4].toFixed(1)).padStart(5)}s  ${String(r[3]).padEnd(12)} ${r[2]}`);
process.exit(errs.length || missing.filter((t) => !skipped.some((s) => s.startsWith(t))).length ? 1 : 0);
