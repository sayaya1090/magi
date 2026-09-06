// 진짜 손(ExcelHand)을 **가짜 Office.js 위에서** 도구 61개 전부 한 번씩 돌린다. `node tools/excelhand.mjs`
//
// 여기 stub 은 호스트가 아니다 — 어떤 속성을 읽어도 그럴듯한 값을 주고, 어떤 메서드를 불러도 받아 준다.
// 그래서 재는 것은 「Excel 이 그렇게 답하는가」가 아니라 **「우리 코드가 Office.js 를 잘못 부르지는 않는가」**
// (없는 메서드·틀린 인자 순서·load 없이 읽기 같은 TypeError)다. 거절(Refusal)은 결과지 고장이 아니다 —
// 표로 적어 사람이 읽는다. 진짜 답은 헬퍼를 띄우고 Excel 에서 봐야 한다(docs/TESTING.ko.md).
import { ExcelHand } from '../src/adapter/ExcelHand.js';
import { ALL_OPS } from '../src/adapter/handCore.js';
import { Refusal } from '../src/adapter/handCore.js';
import { parseAddress, rangeName, cellName } from '../src/adapter/a1.js';

const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
const SUGGESTION = JSON.stringify({ what: '천 단위', why: 'w', fix: { tool: 'set_number_format', args: { sheet: 'Sheet1', address: 'B2', format: '#,##0' } } });

/** 읽으면 나오는 값. 배열은 매번 새로 준다(손이 고쳐 써도 다음 읽기가 안 더러워지게). */
/** 이 경로가 어느 범위를 가리키는가 — `getRange("B2")` 의 인자를 경로에 실어 둔다. 없으면 A1:B2. */
const rangeOf = (path) => { const m = [...path.matchAll(/getRange\(([^)]*)\)/g)].pop(); return m ? m[1] : 'A1:B2'; };
const boxOf = (path) => { try { return parseAddress(rangeOf(path)); } catch { return { rows: 2, cols: 2 }; } };
const scalar = (prop, path) => {
  switch (prop) {
    case 'name': return path.endsWith('.worksheet') || path.includes('worksheets') ? 'Sheet1' : 'Thing1';
    case 'address': return `Sheet1!${rangeOf(path)}`;
    case 'removed': return 1; case 'uniqueRemaining': return 3; case 'rowCount': return boxOf(path).rows; case 'columnCount': return boxOf(path).cols; case 'cellCount': return boxOf(path).rows * boxOf(path).cols;
    case 'values': return [['h1', 'h2'], [1, 2]];
    case 'formulas': return [['h1', 'h2'], [1, '=A2*2']];
    case 'numberFormat': return [['General', 'General'], ['General', '#,##0']];
    case 'text': return path.endsWith('.title') || path.includes('axes') || path.includes('Title') ? '제목' : [['h1', 'h2'], ['1', '2']];
    case 'formula': return '=Sheet1!$B$2';
    case 'isNullObject': return false;
    case 'position': return 0; case 'index': return 0; case 'count': return path.endsWith('worksheets') ? 2 : 1;
    case 'chartType': return 'ColumnClustered';
    case 'width': return 100; case 'height': return 60; case 'top': return 0; case 'left': return 0;
    case 'visibility': return 'Visible'; case 'key': return 'MAGI.FIX.k1'; case 'id': return 'id-1';
    case 'style': return 'TableStyleMedium2'; case 'resolved': return false; case 'content': return '메모';
    case 'authorName': return '사람'; case 'type': return 'List'; case 'valid': return true;
    case 'showHeaders': return true; case 'hyperlink': return null; case 'protected': return false;
    case 'altTextDescription': return ''; case 'bold': return true; case 'color': return '#000000';
    case 'size': return 11; case 'italic': return false; case 'horizontalAlignment': return 'General';
    case 'verticalAlignment': return 'Bottom'; case 'wrapText': return false; case 'columnWidth': return 64;
    case 'rowHeight': return 15; case 'legend': return undefined; case 'visible': return true;
    case 'value': return path.endsWith('getImage') ? PNG : path.includes('settings') ? SUGGESTION : 1;
    case 'creationDate': return '2026-09-06T00:00:00Z'; case 'source': return 'Sheet1!A1:B2';
    case 'operator': return 'Between'; case 'formula1': return '1'; case 'formula2': return '9';
    case 'isEntireColumn': return false; case 'isEntireRow': return false;
    default: return undefined;
  }
};
const seen = [];
const prop_of = (path) => path.split('.').pop();
/** 호출도 되고 속성도 읽히는 것. `ws.freezePanes.freezeRows(1)` 과 `ws.getRange('A1').values` 가 다 지나간다. */
function thing(path) {
  const fn = function () {};
  return new Proxy(fn, {
    get(_, prop) {
      if (typeof prop === 'symbol') return undefined;
      if (prop === 'then') return undefined; // await 가 이것을 값으로 본다
      if (prop === 'toJSON') return () => ({ stub: path });
      if (prop === 'items') return path.endsWith('worksheets') ? [thing(`${path}.items[0]`), thing(`${path}.items[1]`)] : [thing(`${path}.items[0]`)];
      const v = scalar(prop, path);
      if (v !== undefined) return v;
      if (prop === 'sync') return async () => {};
      return thing(`${path}.${prop}`);
    },
    set(_, prop, v) { seen.push(`${path}.${String(prop)} = ${JSON.stringify(v)}`); return true; },
    apply(_, __, args) {
      seen.push(`${path}(${args.map((a) => JSON.stringify(a)).join(', ')})`);
      // 범위 산수는 경로에 싣는다 — `getRange("A1").getResizedRange(1, 1)` 이 A1:B2 로 읽히게.
      const me = prop_of(path);
      if (me === 'getRange') return thing(`${path}(${args[0]})`);
      if (me === 'getResizedRange') { const b = boxOf(path); return thing(`${path}.getRange(${rangeName(b.top ?? 0, b.left ?? 0, b.rows + (args[0] ?? 0), b.cols + (args[1] ?? 0))})`); }
      if (me === 'getCell') { const b = boxOf(path); return thing(`${path}.getRange(${cellName((b.top ?? 0) + (args[0] ?? 0), (b.left ?? 0) + (args[1] ?? 0))})`); }
      return thing(path);
    },
  });
}
const context = () => ({ workbook: thing('workbook'), sync: async () => {} });

// 도구마다 유효한 인자 한 벌 — smoke.mjs 가 가짜 손에 준 것과 같은 모양.
const ARGS = {
  list_sheets: {}, describe_sheet: { sheet: 'Sheet1' }, read_range: { sheet: 'Sheet1', address: 'A1:B2' },
  find: { text: 'h1' }, read_table: { table: 'Thing1' }, read_chart: { sheet: 'Sheet1', chart: 'Thing1' },
  render_range: { sheet: 'Sheet1', address: 'A1:B2' }, render_chart: { sheet: 'Sheet1', chart: 'Thing1' },
  read_comments: { sheet: 'Sheet1' }, read_names: {}, read_validation: { sheet: 'Sheet1', address: 'A1' },
  read_conditional_formats: { sheet: 'Sheet1', address: 'A1:B2' }, describe_style: {}, snapshot_range: { sheet: 'Sheet1', address: 'A1:B2' },
  read_tags: {}, read_suggestions: {}, advise: { items: [{ message: 'm', why: 'w' }] }, clear_advice: {},
  write_range: { sheet: 'Sheet1', address: 'A1', values: [['x', 'y'], [1, 2]] },
  set_number_format: { sheet: 'Sheet1', address: 'B2', format: '#,##0' },
  format_range: { sheet: 'Sheet1', address: 'A1:B1', bold: true, fill: '#DDEBF7', align: 'Center', border_style: 'Continuous', font_color: '#FFFFFF', size: 12, wrap: true, column_width: 80 },
  clear_range: { sheet: 'Sheet1', address: 'A1', what: 'contents' }, replace_all: { find: '매출', replace: '판매' }, copy_range: { sheet: 'Sheet1', source: 'A1:B2', address: 'D1', mode: 'values' }, fill_range: { sheet: 'Sheet1', address: 'A1:A2', to: 'A1:A5', fill: 'series' }, remove_duplicates: { sheet: 'Sheet1', address: 'A1:B5', columns: [0] }, merge_cells: { sheet: 'Sheet1', address: 'A8:C8' }, unmerge_cells: { sheet: 'Sheet1', address: 'A8:C8' },
  insert_cells: { sheet: 'Sheet1', address: 'A2:C2', shift: 'down' }, delete_cells: { sheet: 'Sheet1', address: 'A2:C2', shift: 'up' },
  autofit: { sheet: 'Sheet1', address: 'A1:B2', what: 'both' }, set_hyperlink: { sheet: 'Sheet1', address: 'C4', url: 'https://example.com', text: '링크' },
  add_sheet: { name: '요약', after: 'Sheet1' }, delete_sheet: { sheet: 'Sheet1' }, rename_sheet: { sheet: 'Sheet1', name: '요약2' }, move_sheet: { sheet: 'Sheet1', to: 1 },
  copy_sheet: { sheet: 'Sheet1', name: '사본' }, set_sheet_visibility: { sheet: 'Sheet1', visibility: 'Hidden' }, activate_sheet: { sheet: 'Sheet1', address: 'B2' },
  freeze_panes: { sheet: 'Sheet1', rows: 1 }, set_rows_columns: { sheet: 'Sheet1', rows: '3:5', hidden: true }, set_tab_color: { sheet: 'Sheet1', color: '#FF0000' }, set_sheet_view: { sheet: 'Sheet1', gridlines: false }, set_workbook_properties: { title: '보고' }, protect_sheet: { sheet: 'Sheet1' }, unprotect_sheet: { sheet: 'Sheet1' },
  add_table: { sheet: 'Sheet1', address: 'D1:E3', name: '새표', table_style: 'TableStyleMedium2' },
  set_table_cells: { table: 'Thing1', cells: [{ row: 0, column: 'h1', value: 'v' }] }, add_table_rows: { table: 'Thing1', rows: [['a', 1]] },
  remove_table: { table: 'Thing1' }, sort_range: { sheet: 'Sheet1', address: 'A1:B2', by: [{ column: 1, ascending: false }], has_header: true },
  filter_table: { table: 'Thing1', column: 'h1', values: ['h1'] },
  add_chart: { sheet: 'Sheet1', source: 'A1:B2', chart_type: '막대', title: '차트', name: '새차트' },
  format_chart: { sheet: 'Sheet1', chart: 'Thing1', legend: 'none', y_title: '억원', title: '새 제목', y_min: 0, y_format: '#,##0', series: [{ index: 0, color: '#FF0000', trendline: 'linear' }] }, delete_chart: { sheet: 'Sheet1', chart: 'Thing1' },
  add_conditional_format: { sheet: 'Sheet1', address: 'B2', cf_kind: 'cell_value', operator: 'GreaterThan', value: '1', fill: '#FFC7CE' },
  clear_conditional_formats: { sheet: 'Sheet1', address: 'B2' },
  set_validation: { sheet: 'Sheet1', address: 'C2', validation_kind: 'list', values: ['a', 'b'] },
  set_name: { name: '합계', refers_to: '=Sheet1!$B$2' }, delete_name: { name: 'Thing1' },
  add_comment: { sheet: 'Sheet1', address: 'B2', text: '확인' }, resolve_comment: { sheet: 'Sheet1', address: 'B2' },
  add_image: { sheet: 'Sheet1', path: '/x.png', image_base64: PNG, alt: '로고' },
  add_pivot: { sheet: 'Sheet1', source: 'Sheet1!A1:B2', destination: 'H2', rows: ['h1'], values: [{ field: 'h2', function: 'Sum', number_format: '#,##0', name: '합계' }] }, trace_cell: { sheet: 'Sheet1', address: 'B2' }, insert_sheets_from_file: { file_base64: 'UEsDBA==', file_name: 'x.xlsx' }, import_csv: { csv_rows: [['a', '1'], ['b', '2']], file_name: 'x.csv' }, refresh_pivot: { sheet: 'Sheet1' },
  restore_range: null, set_tag: { key: 'made.by', value: 'magi' },
  suggest: { sheet: 'Sheet1', address: 'B2', what: '천 단위', fix: { tool: 'set_number_format', args: { format: '#,##0' } } },
  drop_suggestion: { key: 'MAGI.FIX.k1' },
};

let failed = 0; let refused = 0;
const hand = new ExcelHand({ run: async (fn) => fn(context()), supports: () => true, document: 'book-stub', label: 'stub.xlsx' });
for (const op of ALL_OPS) {
  let args = ARGS[op];
  if (!(op in ARGS)) { console.log(`  FAIL ${op} — 인자 표에 없다`); failed += 1; continue; }
  if (op === 'restore_range') {
    const snap = await hand.run('snapshot_range', { sheet: 'Sheet1', address: 'A1:B2' });
    args = { snapshot: snap.result.snapshot };
  }
  seen.length = 0;
  try {
    const out = await hand.run(op, args);
    const bad = !out || typeof out !== 'object' || !('result' in out) || !Array.isArray(out.changed) || out.document !== 'book-stub';
    console.log(`  ${bad ? 'FAIL' : 'ok  '} ${op} — ${bad ? '봉투가 아니다: ' + JSON.stringify(out).slice(0, 120) : (out.changed[0] ?? Object.keys(out.result).slice(0, 6).join(','))}`);
    if (bad) failed += 1;
  } catch (e) {
    if (e instanceof Refusal) { refused += 1; console.log(`  REFUSED ${op} — ${e.message}`); continue; }
    failed += 1;
    console.log(`  FAIL ${op} — ${e?.constructor?.name}: ${e?.message}\n        마지막 호출: ${seen.slice(-3).join(' | ')}`);
    if (process.env.STACK) console.log(e.stack);
  }
}
console.log(failed ? `\n${failed} 실패 (거절 ${refused})` : `\n전부 지나감 (거절 ${refused})`);
process.exit(failed ? 1 : 0);
