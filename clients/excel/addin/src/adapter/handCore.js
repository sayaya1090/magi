/**
 * 두 손(ExcelHand·FakeHand)이 같이 쓰는 뼈대 — 도구 이름표, 인자 읽기, 거절, 봉투, 열거형 옮기기.
 *
 * 헬퍼의 catalogue(clients/excel/helper/tools.go)가 광고하는 이름과 여기 ALL_OPS 는 같은 집합이어야 한다 —
 * smoke 가 헬퍼 소스를 읽어 대조한다. 광고한 것을 손이 모르면 「고쳤습니다」 없이 「모릅니다」로 끝나고, 손이
 * 아는 것을 광고 안 하면 아무도 못 부른다.
 */

export const READ_OPS = Object.freeze([
  'list_sheets', 'describe_sheet', 'read_range', 'find', 'read_table', 'read_chart', 'render_range', 'render_chart',
  'read_comments', 'read_names', 'read_validation', 'read_conditional_formats', 'describe_style', 'snapshot_range',
  'read_tags', 'read_suggestions', 'trace_cell', 'advise', 'clear_advice',
]);
export const WRITE_OPS = Object.freeze([
  'write_range', 'set_cell_style', 'replace_all', 'copy_range', 'fill_range', 'remove_duplicates', 'set_number_format', 'format_range', 'clear_range', 'merge_cells', 'unmerge_cells', 'insert_cells',
  'delete_cells', 'autofit', 'set_hyperlink',
  'add_sheet', 'delete_sheet', 'rename_sheet', 'move_sheet', 'copy_sheet', 'set_sheet_visibility', 'activate_sheet',
  'freeze_panes', 'set_rows_columns', 'set_tab_color', 'set_sheet_view', 'set_workbook_properties', 'set_page_setup', 'protect_workbook', 'protect_sheet', 'unprotect_sheet',
  'add_table', 'set_table_cells', 'add_table_rows', 'edit_table', 'remove_table', 'sort_range', 'filter_table',
  'add_chart', 'format_chart', 'delete_chart',
  'add_conditional_format', 'clear_conditional_formats', 'set_validation', 'set_name', 'delete_name',
  'add_comment', 'resolve_comment', 'add_image', 'add_pivot', 'refresh_pivot', 'insert_sheets_from_file', 'import_csv',
  'restore_range', 'set_tag', 'suggest', 'drop_suggestion',
]);
export const ALL_OPS = Object.freeze([...READ_OPS, ...WRITE_OPS]);

/** 제안으로 누를 수 있는 손 — helper/tools.go 의 suggest 설명과 domain/Suggestion.js 의 FIXABLE 과 같은 목록. */
export const FIX_TOOLS = Object.freeze(['write_range', 'format_range', 'set_number_format', 'autofit', 'add_conditional_format', 'sort_range']);
export const FIX_PREFIX = 'MAGI.FIX.';
export const BOOK_SETTING_KEY = 'MAGI.BOOK';

/** 거절 — 도구가 「안 했다」고 말하는 길. 조용한 no-op 은 없다. */
export class Refusal extends Error {}
export const refuse = (msg) => { throw new Refusal(msg); };

// ── 인자 읽기: 없는 키는 null, 틀린 형은 관대하게(문자열 숫자도 숫자로) ─────────────────────────────
export const str = (a, k) => (a?.[k] == null ? null : String(a[k]));
export const num = (a, k) => {
  const v = a?.[k];
  if (v == null || v === '') return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : refuse(`${k} 는 숫자여야 합니다 — ${JSON.stringify(v)}`);
};
export const int = (a, k) => { const n = num(a, k); return n == null ? null : Math.round(n); };
export const bool = (a, k) => {
  const v = a?.[k];
  if (v == null) return null;
  if (typeof v === 'boolean') return v;
  if (v === 'true' || v === 1 || v === '1') return true;
  if (v === 'false' || v === 0 || v === '0') return false;
  return refuse(`${k} 는 true/false 여야 합니다 — ${JSON.stringify(v)}`);
};
export const arr = (a, k) => (Array.isArray(a?.[k]) ? a[k] : null);
export const need = (a, k, what = k) => {
  const v = a?.[k];
  if (v == null || v === '') refuse(`${what} 가 없습니다`);
  return v;
};
/** 2차원 배열인지 재고, 모양(행 수·열 수)을 돌려준다. 들쭉날쭉한 줄은 거절. */
export function grid(a, k) {
  const v = a?.[k];
  if (v == null) return null;
  if (!Array.isArray(v) || !v.every(Array.isArray)) refuse(`${k} 는 줄마다 배열인 2차원 배열이어야 합니다 — [[a, b], [c, d]]`);
  if (v.length === 0) refuse(`${k} 가 비었습니다`);
  const cols = v[0].length;
  if (cols === 0) refuse(`${k} 의 첫 줄이 비었습니다`);
  if (!v.every((r) => r.length === cols)) refuse(`${k} 의 줄 길이가 들쭉날쭉합니다 — 모든 줄이 ${cols}칸이어야 합니다`);
  return { rows: v.length, cols, cells: v };
}
export const hex = (a, k, noneOk = false) => {
  const v = str(a, k);
  if (v == null) return null;
  if (noneOk && v.toLowerCase() === 'none') return 'none';
  const h = v.replace(/^#/, '');
  if (!/^[0-9a-fA-F]{6}$/.test(h)) refuse(`${k} 는 #RRGGBB 로 주세요 — '${v}'`);
  return '#' + h.toUpperCase();
};

/** 차트 종류 — 한국어 이름도 받는다. 헬퍼 enums.go 의 chartTypes 와 같은 집합 + 별칭. */
export const CHART_ALIASES = new Map(Object.entries({
  bar: 'ColumnClustered', column: 'ColumnClustered', 막대: 'ColumnClustered', 세로막대: 'ColumnClustered',
  hbar: 'BarClustered', 가로막대: 'BarClustered', line: 'Line', 꺾은선: 'Line', 선: 'Line',
  pie: 'Pie', 원: 'Pie', 파이: 'Pie', doughnut: 'Doughnut', 도넛: 'Doughnut',
  area: 'Area', 영역: 'Area', scatter: 'XYScatter', 분산: 'XYScatter', radar: 'Radar', 방사형: 'Radar',
  stacked: 'ColumnStacked', 누적: 'ColumnStacked', waterfall: 'Waterfall', 폭포: 'Waterfall',
}));
export const CHART_TYPES = Object.freeze([
  'ColumnClustered', 'ColumnStacked', 'ColumnStacked100', 'BarClustered', 'BarStacked', 'BarStacked100',
  'Line', 'LineMarkers', 'Pie', 'Doughnut', 'Area', 'AreaStacked', 'XYScatter', 'XYScatterLines',
  'Radar', 'Waterfall', 'Treemap', 'Sunburst', 'Funnel', 'Histogram', 'BoxWhisker',
]);
export const CHART_KO = new Map(Object.entries({
  ColumnClustered: '세로 막대', ColumnStacked: '누적 세로 막대', ColumnStacked100: '100% 누적 세로 막대',
  BarClustered: '가로 막대', BarStacked: '누적 가로 막대', BarStacked100: '100% 누적 가로 막대',
  Line: '꺾은선', LineMarkers: '꺾은선(표식)', Pie: '원', Doughnut: '도넛', Area: '영역', AreaStacked: '누적 영역',
  XYScatter: '분산', XYScatterLines: '분산(선)', Radar: '방사형', Waterfall: '폭포', Treemap: '트리맵',
  Sunburst: '선버스트', Funnel: '깔때기', Histogram: '히스토그램', BoxWhisker: '상자 수염',
}));
export function chartTypeOf(name) {
  if (name == null || name === '') return 'ColumnClustered';
  const raw = String(name).trim();
  const alias = CHART_ALIASES.get(raw.toLowerCase()) ?? CHART_ALIASES.get(raw);
  const got = alias ?? CHART_TYPES.find((t) => t.toLowerCase() === raw.toLowerCase());
  return got ?? refuse(`모르는 차트 종류입니다: ${raw} — ${CHART_TYPES.join(', ')} (막대·가로막대·꺾은선·원·분산·영역도 됩니다)`);
}

/** 봉투 — 헬퍼의 HandResult 와 같은 모양. `changed` 는 사람이 읽는 한국어 한 줄씩. */
export function envelope(hand, result, changed = []) {
  return { document: hand.document, label: hand.labelText, result, changed, epoch: hand.epoch, count: hand.count };
}

export const clip = (s, n = 40) => { const t = String(s ?? '').replace(/\s+/g, ' '); return t.length > n ? t.slice(0, n - 1) + '…' : t; };
export const isFormula = (v) => typeof v === 'string' && v.startsWith('=');
export const nowEpoch = () => Math.floor(Date.now() / 1000) % 2147483647;
