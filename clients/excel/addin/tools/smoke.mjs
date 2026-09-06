// Excel 없이 도는 확인. `node tools/smoke.mjs`
//
// 파워포인트 판(clients/powerpoint/addin/tools/smoke.mjs)의 호스트 무관 구간을 옮겨 왔고(대화 스트림·권한·
// 펜딩·커서·전사 접기·아이콘 단추·마크다운), 엑셀 고유 구간(가짜 손 55개·인용·안내·제안·호스트 고르기·요구
// 집합·화면 라벨)은 새로 썼다. 유스케이스가 Office.js 를 모르므로 FakeWorkbook·FakeHand 만 갈아 끼우면 흐름이
// 끝까지 돈다. 진짜 Excel 이 어떻게 답하는지는 여기서 **안 잰다** — tools/excelhand.mjs 의 stub 도 호스트가
// 아니다.

import { Composer, promptOf } from '../src/domain/Composer.js';
import { HelperApi } from '../src/adapter/helperApi.js';
import { stableBookId, BOOK_SETTING, OfficeWorkbook, SAMPLE_ROWS } from '../src/adapter/OfficeWorkbook.js';
import { Quote } from '../src/domain/Quote.js';
import { Advice, targetLabel, SheetIndex } from '../src/domain/Advice.js';
import { foldAdvice, adviceNote } from '../src/domain/AdviceBoard.js';
import { fixLabel, FIXABLE } from '../src/domain/Suggestion.js';
import { WorkbookPort } from '../src/port/WorkbookPort.js';
import { FakeWorkbook } from '../src/adapter/FakeWorkbook.js';
import { FakeHand } from '../src/adapter/FakeHand.js';
import { ALL_OPS, READ_OPS, FIX_TOOLS, chartTypeOf } from '../src/adapter/handCore.js';
import { parseAddress, rangeName, cellName, colName } from '../src/adapter/a1.js';
import { pickBook, pickNote, lateNote, lateFailNote } from '../src/adapter/pickBook.js';
import { QuoteSelection, quoteNote } from '../src/usecase/QuoteSelection.js';
import { SendTurn, logShapeOf, sendNote } from '../src/usecase/SendTurn.js';
import { FakeChat } from '../src/adapter/FakeChat.js';
import { PointAtAdvice } from '../src/usecase/PointAtAdvice.js';
import { handRole, HAND_FLOOR } from '../src/usecase/HandRole.js';
import { readFileSync, readdirSync } from 'node:fs';
import { parseMd, inlines, mdToDom, looksLikeMd } from '../src/ui/md.js';
import { fixture } from '../src/ui/bookFixture.js';
import {
  headOf, noteHead, userBadge, rowHead, rowShape, rowClass, argsCell, endText, bodyText,
  isSendKey, askAction, askReveal, askKind, askHead, whatText, argsText, placeLine, doingLine,
  lastAskShape, decisionClass, failNote, noteLife, capsOf, capsText, streamLine,
  unknownLine, quoteBody, quoteMeta, adviceBoard, adviceTargetText, pretty, clip,
  capsSummary, capsQuiet, councilButton, brandState, resultCell, permissionText, councilBody, skippedLine,
  adapterText, readyText, guideBoard, planBoard, changedLines, toolLabel, labelledTools,
  planAnchor, reviewAsk, appendAsk, confirmAsk, thinkHead, oneLine, turnRunning, contextMeter, modelPicker, CONTEXT_PARTS, fixBoard, foldText,
} from '../src/ui/screen.js';
import { Transcript, isPluginNudge, PLUGIN_NUDGE_MARK } from '../src/domain/Transcript.js';
import { FakeTranscript } from '../src/adapter/FakeTranscript.js';
import { ReadTranscript } from '../src/usecase/ReadTranscript.js';
import { FakeStatus } from '../src/adapter/FakeStatus.js';
import { WatchPrompt, askSig } from '../src/usecase/WatchPrompt.js';
import { Pending, DECISIONS, CLEARED, clearedNote, askArgs } from '../src/domain/Pending.js';
import { Cursor } from '../src/domain/Cursor.js';
let failed = 0;
const ok = (name, cond, detail = '') => {
  console.log(`${cond ? '  ok  ' : '  FAIL'} ${name}${detail ? ' — ' + detail : ''}`);
  if (!cond) failed++;
};
// 훑어서 「전부 그렇다」를 묻는다. **빈 것에는 참을 안 준다.** (파워포인트 판의 설명 참고)
const everyOf = (arr, pred) => arr.length > 0 && !arr.some((x, i) => !pred(x, i));
const threw = async (fn) => { try { await fn(); return null; } catch (e) { return e?.message ?? String(e); } };

const book = new FakeWorkbook(structuredClone(fixture));
const conv = new Composer();
const quote = new QuoteSelection(book, conv);
const point = new PointAtAdvice(book);

// ── 가짜 손: 도구 61개 전부 ──────────────────────────────────────────────────
//
// 헬퍼가 광고하는 이름(helper/tools.go)과 손이 아는 이름은 같은 집합이어야 한다 — 어긋나면 「고쳤습니다」
// 없이 「모릅니다」로 끝나거나(손이 모름) 아무도 못 부른다(광고 없음).
{
  const go = readFileSync(new URL('../../../office/helper/xl_tools.go', import.meta.url), 'utf8');
  const advertised = [...go.matchAll(/^\t\t\tName: +"([a-z_]+)",/gm)].map((m) => m[1]);
  ok('헬퍼 도구 목록을 실제로 읽었다', advertised.length >= 40, String(advertised.length));
  const onlyHelper = advertised.filter((n) => !ALL_OPS.includes(n));
  const onlyHand = ALL_OPS.filter((n) => !advertised.includes(n));
  ok('헬퍼와 손이 같은 이름을 안다', onlyHelper.length === 0 && onlyHand.length === 0, `헬퍼에만: ${onlyHelper} / 손에만: ${onlyHand}`);
  // 도구 하나가 `{ Name … ReadOnly }` 한 블록이다 — 블록으로 갈라서 재야 이웃 도구의 ReadOnly 를 안 빌린다.
  const readOnlyGo = go.split(/\n\t\t\{\n/).filter((b) => /ReadOnly: true/.test(b)).map((b) => /Name: +"([a-z_]+)"/.exec(b)?.[1]).filter(Boolean);
  ok('읽기 도구 집합도 같다', READ_OPS.length === 19 && everyOf([...READ_OPS], (n) => readOnlyGo.includes(n)), `${READ_OPS.filter((n) => !readOnlyGo.includes(n))}`);
  const suggestDesc = /Name: +"suggest",[\s\S]*?Desc: ([\s\S]*?)Props:/.exec(go)?.[1] ?? '';
  ok('제안으로 누를 수 있는 손 목록이 헬퍼 설명·손·화면에서 같다',
    everyOf([...FIX_TOOLS], (n) => suggestDesc.includes(n) && FIXABLE.has(n)) && FIXABLE.size === FIX_TOOLS.length);

  const hand = new FakeHand(structuredClone(fixture));
  const done = new Set();
  const call = async (op, args = {}) => { done.add(op); return hand.run(op, args); };
  const list = await call('list_sheets');
  ok('목차: 탭 번호·활성 표시·사용 범위', list.result.sheets[0].index === 1 && list.result.sheets[0].active === true && list.result.sheets[0].used_range === 'A1:C6', JSON.stringify(list.result.sheets[0]));
  const desc = await call('describe_sheet', { sheet: '매출' });
  ok('시트 살펴보기: 머리글 값', desc.result.header?.values?.[0] === '분기', JSON.stringify(desc.result.header));
  const rr = await call('read_range', { address: 'A1:B2' });
  ok('범위 읽기: 2차원 값', rr.result.values[1][1] === 12000 && rr.result.rows === 2);
  const big = await call('read_range', { max_rows: 2 });
  ok('큰 범위는 잘라 말한다', big.result.truncated === true && big.result.rows === 2 && big.result.note.includes('max_rows'));
  const w = await call('write_range', { address: 'E1', values: [['x', 'y'], [1, 2]] });
  ok('왼쪽 위 셀 하나면 배열 크기로 잡는다', w.result.address === 'E1:F2' && w.changed[0].includes('2×2'));
  ok('배열과 주소가 어긋나면 거절', (await threw(() => hand.run('write_range', { address: 'A1:B2', values: [[1]] })))?.includes('1×1'));
  ok('들쭉날쭉한 배열은 거절', (await threw(() => hand.run('write_range', { address: 'A1', values: [[1, 2], [3]] })))?.includes('들쭉날쭉'));
  const ow = await call('write_range', { address: 'B2', values: [[99]] });
  ok('덮어쓰기를 센다', ow.result.overwrote === 1 && ow.changed[0].includes('덮어썼습니다'));
  const f = await call('write_range', { address: 'B6', formulas: [['=SUM(B2:B5)']] });
  ok('수식은 수식으로 센다', f.result.formulas === 1);
  ok('되읽으면 수식이 보인다', (await call('read_range', { address: 'B6' })).result.formulas.B6 === '=SUM(B2:B5)');
  await call('set_number_format', { address: 'B2:B6', format: '#,##0' });
  ok('표시 형식이 되읽힌다', (await call('read_range', { address: 'B2' })).result.number_formats.B2 === '#,##0');
  const fr = await call('format_range', { address: 'A1:C1', bold: true, fill: '#DDEBF7', align: 'Center' });
  ok('서식 한 줄이 무엇을 바꿨는지 적는다', fr.changed[0].includes('굵게') && fr.changed[0].includes('#DDEBF7'));
  ok('서식에 바꿀 것이 없으면 거절', (await threw(() => hand.run('format_range', { address: 'A1' })))?.includes('바꿀 것이'));
  ok('색은 #RRGGBB', (await threw(() => hand.run('format_range', { address: 'A1', fill: 'blue' })))?.includes('#RRGGBB'));
  const snap = await call('snapshot_range', { address: 'A1:B2' });
  await call('clear_range', { address: 'A1:B2' });
  await call('write_range', { sheet: '매출', address: 'H1', values: [['사과', 1], ['배', 2], ['사과', 1], ['감', 3]] });
  const ra = await call('replace_all', { sheet: '매출', find: '사과', replace: '풋사과' });
  ok('찾아 바꾸기: 셀 수와 시트', ra.result.cells === 2 && ra.result.sheets[0].sheet === '매출', JSON.stringify(ra.result));
  ok('찾아 바꾸기: 없으면 거절', (await threw(() => hand.run('replace_all', { find: '없는말', replace: 'x' })))?.includes('바꾼 것이 없습니다'));
  const cp = await call('copy_range', { sheet: '매출', source: 'H1:I2', address: 'K1', mode: 'values', transpose: true });
  ok('범위 복사: 행열 바꿔 값만', cp.result.address === 'K1:L2' && (await call('read_range', { sheet: '매출', address: 'K1:L2' })).result.values[0][1] === '배', JSON.stringify(cp.result));
  await call('write_range', { sheet: '매출', address: 'N1', values: [[10], [20]] });
  const fl = await call('fill_range', { sheet: '매출', address: 'N1:N2', to: 'N1:N5', fill: 'series' });
  ok('채우기: 등차로 늘린다', fl.result.filled === 3 && (await call('read_range', { sheet: '매출', address: 'N5' })).result.values[0][0] === 50, JSON.stringify(fl.result));
  ok('채우기: 씨앗을 안 품는 to 는 거절', (await threw(() => hand.run('fill_range', { sheet: '매출', address: 'N1:N2', to: 'N3:N5' })))?.includes('포함해야'));
  const rd = await call('remove_duplicates', { sheet: '매출', address: 'H1:I4', columns: [0], has_header: false });
  ok('중복 제거: 판단 열로 센다', rd.result.removed === 1 && rd.result.remaining === 3, JSON.stringify(rd.result));
  await call('clear_range', { sheet: '매출', address: 'H1:N5' });
  ok('지운 값 수를 센다', (await call('read_range', { address: 'A1:B2' })).result.values[0][0] === '');
  const back = await call('restore_range', { snapshot: snap.result.snapshot });
  ok('스냅숏으로 되돌린다', back.changed[0].includes('되돌렸습니다') && (await call('read_range', { address: 'A1' })).result.values[0][0] === '분기');
  ok('없는 스냅숏은 거절', (await threw(() => hand.run('restore_range', { snapshot: 'snap-없음' })))?.includes('그런 스냅숏'));
  await call('merge_cells', { address: 'A8:C8' }); await call('unmerge_cells', { address: 'A8:C8' });
  const ins = await call('insert_cells', { address: 'A2:C2', shift: 'down' });
  ok('끼워 넣으면 아래로 밀린다', ins.changed[0].includes('아래') && (await call('read_range', { address: 'A3' })).result.values[0][0] === '1분기');
  const del = await call('delete_cells', { address: 'A2:C2', shift: 'up' });
  ok('삭제하면 위로 당겨진다', (await call('read_range', { address: 'A2' })).result.values[0][0] === '1분기', JSON.stringify(del.result));
  ok('shift 값을 재고 거절', (await threw(() => hand.run('delete_cells', { address: 'A2', shift: 'down' })))?.includes('up 또는 left'));
  await call('autofit', {}); await call('set_hyperlink', { address: 'C4', url: 'https://example.com' }); await call('set_hyperlink', { address: 'C4' });
  const sh = await call('add_sheet', { name: '요약', after: '매출' });
  ok('시트를 만들면 탭 번호를 말한다', sh.result.index === 2 && sh.changed[0].includes('요약'));
  ok('같은 이름은 거절', (await threw(() => hand.run('add_sheet', { name: '요약' })))?.includes('이미'));
  ok('시트 이름 규칙', (await threw(() => hand.run('add_sheet', { name: 'a/b' })))?.includes('31자'));
  await call('rename_sheet', { sheet: '요약', name: '요약2' }); await call('move_sheet', { sheet: '요약2', to: 1 });
  ok('옮기면 1번이 된다', (await call('list_sheets')).result.sheets[0].name === '요약2');
  await call('copy_sheet', { sheet: '매출', name: '매출 사본' }); await call('set_sheet_visibility', { sheet: '매출 사본', visibility: 'Hidden' });
  ok('보이는 시트가 하나면 못 숨긴다 검사', (await threw(() => hand.run('set_sheet_visibility', { sheet: '매출', visibility: 'Nope' })))?.includes('Visible, Hidden'));
  await call('activate_sheet', { sheet: '매출', address: 'B2' }); await call('freeze_panes', { sheet: '매출', rows: 1 });
  const rcs = await call('set_rows_columns', { sheet: '매출', rows: '3:5', hidden: true, height: 20 });
  ok('행·열: 행 범위 숨김·높이', rcs.result.span === '3:5' && rcs.result.kind === 'rows' && rcs.changed[0].includes('숨김'), JSON.stringify(rcs));
  ok('행·열: 둘 다 주면 거절', (await threw(() => hand.run('set_rows_columns', { sheet: '매출', rows: '1', columns: 'A', hidden: true })))?.includes('중 하나'));
  ok('행·열: 행에 width 는 거절', (await threw(() => hand.run('set_rows_columns', { sheet: '매출', rows: '1', width: 30 })))?.includes('columns 에만'));
  ok('탭 색', (await call('set_tab_color', { sheet: '매출', color: '#FF0000' })).changed[0].includes('#FF0000'));
  ok('눈금선·머리글', (await call('set_sheet_view', { sheet: '매출', gridlines: false, headings: true })).changed[0].includes('눈금선 끔'));
  ok('문서 속성', (await call('set_workbook_properties', { title: '상반기 보고', author: '기획팀' })).result.title === '상반기 보고');
  await call('set_rows_columns', { sheet: '매출', rows: '3:5', hidden: false });
  ok('셀 스타일', (await call('set_cell_style', { sheet: '매출', address: 'A1:B1', style: 'Heading1' })).changed[0].includes('Heading1'));
  ok('인쇄 설정', (await call('set_page_setup', { sheet: '매출', orientation: 'Landscape', fit_width: 1, title_rows: '$1:$1' })).changed[0].includes('가로'));
  ok('인쇄 설정: 빈 호출은 거절', (await threw(() => hand.run('set_page_setup', { sheet: '매출' })))?.includes('바꿀 것이 없습니다'));
  ok('통합 문서 보호·해제', (await call('protect_workbook', { password: 'x' })).result.protected === true && (await call('protect_workbook', { protected: false, password: 'x' })).result.protected === false);
  ok('통합 문서 보호: 두 번은 거절', (await threw(() => hand.run('protect_workbook', { protected: false })))?.includes('보호되어 있지 않습니다'));
  await call('write_range', { sheet: '매출', address: 'R1', values: [['품목', '수량'], ['a', 1], ['b', 2]] }); const tbl = await call('add_table', { sheet: '매출', address: 'R1:S3', name: 'T편집' });
  const et = await call('edit_table', { table: 'T편집', add_columns: ['비고'], show_totals: true });
  ok('표 고치기: 열 추가·요약 행', et.result.columns.includes('비고') && et.changed[0].includes('요약 행 켬'), JSON.stringify(et.result));
  const et2 = await call('edit_table', { table: 'T편집', delete_columns: ['수량'] });
  ok('표 고치기: 열 삭제', !et2.result.columns.includes('수량') && et2.result.columns.includes('비고'), JSON.stringify(et2.result));
  ok('표 고치기: 없는 열은 거절', (await threw(() => hand.run('edit_table', { table: 'T편집', delete_columns: ['없음'] })))?.includes('열이 없습니다'));
  await call('remove_table', { table: 'T편집', delete_data: true });
  await call('write_range', { sheet: '매출', address: 'P1', values: [[1], [2]] }); await call('write_range', { sheet: '매출', address: 'P3', formulas: [['=P1+P2']] });
  const tc = await call('trace_cell', { sheet: '매출', address: 'P3' });
  ok('참조 추적: 수식이 읽는 셀', tc.result.what === 'precedents' && tc.result.cells.join(',') === '매출!P1,매출!P2', JSON.stringify(tc.result));
  const td = await call('trace_cell', { sheet: '매출', address: 'P1', what: 'dependents' });
  ok('참조 추적: 이 셀을 읽는 수식', td.result.cells.includes('매출!P3'), JSON.stringify(td.result));
  await call('clear_range', { sheet: '매출', address: 'P1:P3' });
  const isf = await call('insert_sheets_from_file', { file_base64: 'UEsDBA==', file_name: '외부.xlsx' });
  ok('다른 통합 문서의 시트 넣기', isf.result.count === 1 && (await call('list_sheets')).result.sheets.some((s) => s.name === isf.result.sheets[0]), JSON.stringify(isf.result));
  await call('delete_sheet', { sheet: isf.result.sheets[0] });
  const ic = await call('import_csv', { csv_rows: [['품목', '수량'], ['사과', '3'], ['배', '2.5']], file_name: '재고.csv' });
  ok('CSV 가져오기: 새 시트에 수는 수로', ic.result.new_sheet === true && ic.result.sheet === '재고' && (await call('read_range', { sheet: '재고', address: 'B3' })).result.values[0][0] === 2.5, JSON.stringify(ic.result));
  await call('delete_sheet', { sheet: '재고' });
  ok('CSV 가져오기: 줄이 없으면 거절', (await threw(() => hand.run('import_csv', { path: 'x.csv' })))?.includes('헬퍼가 읽어'));
  ok('틀 고정이 살펴보기에 보인다', (await call('describe_sheet', { sheet: '매출' })).result.frozen === 'A1');
  await call('protect_sheet', { sheet: '매출' }); await call('unprotect_sheet', { sheet: '매출' });
  await call('delete_sheet', { sheet: '매출 사본' });
  ok('마지막 시트는 못 지운다', (await threw(async () => { const h = new FakeHand({ sheets: [{ name: '하나', cells: {} }] }); await h.run('delete_sheet', { sheet: '하나' }); }))?.includes('마지막'));
  const t = await call('add_table', { sheet: '매출', address: 'A1:C6', name: '매출표', table_style: 'TableStyleMedium2' });
  ok('표를 만들면 이름을 준다', t.result.table === '매출표');
  ok('표 위에 또 표는 거절', (await threw(() => hand.run('add_table', { sheet: '매출', address: 'A1:B3' })))?.includes('이미'));
  const rt = await call('read_table', { table: '매출표' });
  ok('표 읽기: 머리글과 행', rt.result.headers[0] === '분기' && rt.result.row_count === 5, JSON.stringify(rt.result.headers));
  await call('set_table_cells', { table: '매출표', cells: [{ row: 0, column: '비고', value: '메모' }] });
  ok('머리글 이름으로 칸을 쓴다', (await call('read_range', { sheet: '매출', address: 'C2' })).result.values[0][0] === '메모');
  ok('없는 열은 거절', (await threw(() => hand.run('set_table_cells', { table: '매출표', cells: [{ row: 0, column: '없음', value: 1 }] })))?.includes('열이 아닙니다'));
  const ar = await call('add_table_rows', { table: '매출표', rows: [['5분기', 1, '']] });
  ok('행을 붙이면 표가 늘어난다', ar.result.added === 1 && (await call('read_table', { table: '매출표' })).result.row_count === 6);
  await call('sort_range', { table: '매출표', by: [{ column: '매출', ascending: false }] });
  ok('표 정렬은 값으로', (await call('read_table', { table: '매출표' })).result.rows[0][1] === 15100);
  await call('filter_table', { table: '매출표', column: '분기', values: ['1분기'] }); await call('filter_table', { table: '매출표', column: '분기' });
  await call('add_table', { sheet: '비용', address: 'A1:B3', name: '비용표' });
  const rm = await call('remove_table', { table: '비용표' });
  ok('표를 풀면 값은 남는다', rm.changed[0].includes('그대로') && (await call('read_range', { sheet: '비용', address: 'A1' })).result.values[0][0] !== '');
  const c = await call('add_chart', { sheet: '매출', source: 'A1:B6', chart_type: '막대', title: '분기 매출', name: '매출차트' });
  ok('한국어 차트 이름도 받는다', c.result.type === 'ColumnClustered' && c.result.type_ko === '세로 막대');
  ok('모르는 차트 종류는 목록과 함께 거절', (await threw(() => hand.run('add_chart', { source: 'A1:B6', chart_type: 'bubble3d' })))?.includes('ColumnClustered'));
  ok('머리글 없는 한 줄로는 차트를 못 만든다', (await threw(() => hand.run('add_chart', { source: 'A1:A1' })))?.includes('머리글'));
  const rc = await call('read_chart', { sheet: '매출', chart: '매출차트' });
  ok('차트 읽기: 계열 이름', rc.result.series[0] === '매출' && rc.result.title === '분기 매출');
  await call('format_chart', { sheet: '매출', chart: '매출차트', legend: 'none', y_title: '억원' }); ok('가짜는 픽셀을 지어내지 않는다', (await threw(() => call('render_chart', { sheet: '매출', chart: '매출차트' })))?.includes('진짜 Excel') && (await threw(() => call('render_range', { sheet: '매출', address: 'A1:C6' })))?.includes('진짜 Excel'));
  await call('delete_chart', { sheet: '매출', chart: '매출차트' });
  ok('없는 차트는 거절', (await threw(() => hand.run('read_chart', { sheet: '매출', chart: '매출차트' })))?.includes('없습니다'));
  await call('add_conditional_format', { sheet: '매출', address: 'B2:B6', cf_kind: 'color_scale' });
  await call('add_conditional_format', { sheet: '매출', address: 'B2:B6', cf_kind: 'cell_value', operator: 'GreaterThan', value: '13000', fill: '#FFC7CE' });
  ok('cell_value 에 value 가 없으면 거절', (await threw(() => hand.run('add_conditional_format', { address: 'B2', cf_kind: 'cell_value' })))?.includes('value'));
  ok('조건부 서식을 되읽는다', (await call('read_conditional_formats', { sheet: '매출', address: 'B2:B6' })).result.count === 2);
  ok('지우면 수를 말한다', (await call('clear_conditional_formats', { sheet: '매출' })).result.cleared === 2);
  await call('set_validation', { sheet: '매출', address: 'C2:C6', validation_kind: 'list', values: ['환율', '단가'] });
  ok('유효성이 되읽힌다', (await call('read_validation', { sheet: '매출', address: 'C2:C6' })).result.type === 'list');
  ok('list 에 값이 없으면 거절', (await threw(() => hand.run('set_validation', { address: 'C2', validation_kind: 'list' })))?.includes('values'));
  await call('set_validation', { sheet: '매출', address: 'C2:C6', clear: true });
  await call('set_name', { name: '합계', refers_to: '=매출!$B$6' });
  ok('이름이 목록에 선다', (await call('read_names', {})).result.names[0].name === '합계');
  await call('delete_name', { name: '합계' });
  ok('없는 이름은 거절', (await threw(() => hand.run('delete_name', { name: '합계' })))?.includes('없습니다'));
  await call('add_comment', { sheet: '매출', address: 'B2', text: '확인 요망' }); await call('add_comment', { sheet: '매출', address: 'B2', text: '답글' });
  const cm = await call('read_comments', { sheet: '매출' });
  ok('메모와 답글', cm.result.count === 1 && cm.result.comments[0].replies.length === 1);
  await call('resolve_comment', { sheet: '매출', address: 'B2' }); await call('resolve_comment', { sheet: '매출', address: 'B2', delete: true });
  ok('그림은 바이트 없이 거절', (await threw(() => hand.run('add_image', { path: '/x.png' })))?.includes('헬퍼'));
  await call('add_image', { sheet: '매출', path: '/x.png', image_base64: 'AAAA', alt: '로고' });
  await call('add_pivot', { sheet: '매출', source: '매출!A1:C6', destination: 'H2', rows: ['분기'], values: [{ field: '매출', function: 'Sum' }] }); await call('refresh_pivot', { sheet: '매출' });
  const fd = await call('find', { text: '분기' });
  ok('찾기: 시트와 주소', fd.result.matched >= 5 && fd.result.hits[0].sheet === '매출');
  ok('찾기: limit', (await call('find', { text: '분기', limit: 1 })).result.hits.length === 1);
  await call('describe_style'); await call('read_tags');
  await call('set_tag', { key: 'made.by', value: 'magi' });
  ok('기록이 남는다', (await call('read_tags')).result.tags.some((x) => x.key === 'made.by'));
  ok('제안 키는 기록으로 못 쓴다', (await threw(() => hand.run('set_tag', { key: 'MAGI.FIX.X', value: '1' })))?.includes('못 씁니다'));
  const sg = await call('suggest', { sheet: '매출', address: 'B2', what: '천 단위', fix: { tool: 'set_number_format', args: { format: '#,##0' } } });
  ok('제안은 안 고친 것이라고 말한다', sg.changed[0].includes('아직 안 고친'));
  ok('누를 수 없는 손은 거절', (await threw(() => hand.run('suggest', { what: 'x', fix: { tool: 'delete_sheet' } })))?.includes('누를 수 있는'));
  const rs = await call('read_suggestions', {});
  ok('제안 읽기: 픽스처 둘 + 새 것', rs.result.count === 3 && rs.result.suggestions.some((s) => s.key === sg.result.suggestion && s.appliable));
  await call('drop_suggestion', { key: sg.result.suggestion });
  ok('기록 키로는 제안을 못 뗀다', (await threw(() => hand.run('drop_suggestion', { key: 'made.by' })))?.includes('제안의 키가'));
  await call('advise', { items: [{ message: 'm', why: 'w' }] }); await call('clear_advice');
  const missing = ALL_OPS.filter((op) => !done.has(op));
  ok('도구 76개를 전부 한 번씩 돌렸다', missing.length === 0 && ALL_OPS.length === 76, `안 돈 것: ${missing.join(', ')} / ${ALL_OPS.length}`);
  ok('모르는 op 는 아는 것을 대고 던진다', (await threw(() => hand.run('fly', {})))?.includes('list_sheets'));
  ok('요구 집합이 모자라면 이름을 대고 거절한다', (await threw(async () => { const h = new FakeHand(structuredClone(fixture), { supports: () => false }); await h.run('find', { text: 'x' }); }))?.includes('ExcelApi 1.9'));
  ok('바꾼 호출마다 count 가 오른다', hand.count > 30, String(hand.count));
}

// ── A1 산수 ──────────────────────────────────────────────────────────────────
{
  ok('열 이름', colName(0) === 'A' && colName(25) === 'Z' && colName(26) === 'AA' && colName(701) === 'ZZ');
  const p = parseAddress('B2:E9');
  ok('범위 풀기', p.top === 1 && p.left === 1 && p.rows === 8 && p.cols === 4);
  ok('셀 하나', parseAddress('C3').rows === 1 && cellName(2, 2) === 'C3');
  ok('행 전체·열 전체', parseAddress('5:7').rows === 3 && parseAddress('A:C').cols === 3);
  ok('이름 짓기', rangeName(1, 1, 8, 4) === 'B2:E9' && rangeName(0, 0, 1, 1) === 'A1');
  ok('거꾸로는 던진다', (await threw(async () => parseAddress('E9:B2')))?.includes('거꾸로'));
  ok('차트 종류 별칭', chartTypeOf('꺾은선') === 'Line' && chartTypeOf('hbar') === 'BarClustered' && chartTypeOf(undefined) === 'ColumnClustered');
}

// ── 인용: 시트에서 잡은 범위 ──────────────────────────────────────────────────
{
  book.select('A1:B2');
  const r = await quote.run();
  ok('선택을 인용한다', r.empty === false && r.added.length === 1 && r.added[0].key === '매출!A1:B2', JSON.stringify(r));
  const q = r.added[0];
  ok('인용 글에 시트·범위·값이 실린다', q.toPrompt().startsWith('[인용] sheet="매출" range=A1:B2 size=2x2') && q.toPrompt().includes('분기\t매출'));
  ok('같은 범위는 두 번 안 붙는다', (await quote.run()).skipped === 1);
  book.select('D9');
  const empty = await quote.run();
  ok('빈 셀도 인용이다 — 사실이다', empty.added.length === 1 && quoteBody(empty.added[0]) === '(빈 범위)');
  book.reading = false;
  ok('못 읽으면 readFailed', (await quote.run()).reason === 'readFailed');
  book.reading = true;
  ok('사유가 말이 된다', quoteNote({ reason: 'readFailed' }).sticky === true && quoteNote({ reason: 'none' }).text.includes('범위') && quoteNote({ reason: '???' }).text.includes('고쳐야'));
  ok('표본은 12×12 로 자른다', SAMPLE_ROWS === 12 && new Quote({ sheet: 's', address: 'A1', rowCount: 30, columnCount: 2, values: [[1]], valuesTruncated: true }).toPrompt().includes('valuesTruncated=true'));
  ok('인용 미리보기', quoteMeta(q) === '2×2' && quoteBody(q).startsWith('"분기'));
  ok('프롬프트에 인용이 먼저 선다', promptOf('줄여줘', [q]).startsWith('[인용]') && promptOf('줄여줘', [q]).endsWith('줄여줘'));
  conv.clear();
}

// ── 안내: 시트와 범위를 가리킨다 ──────────────────────────────────────────────
{
  const rows = [
    { kind: 'tool', tool: 'mcp__xl__advise', callId: 'c1', args: { items: [{ message: '합계가 값이다', why: 'w', sheet: '매출', address: 'B6' }, { message: '어딘지 없음' }] } },
    { kind: 'tool', tool: 'mcp__ppt__advise', callId: 'c2', args: { items: [{ message: '남의 창' }] } },
  ];
  const { items, strays, dropped } = foldAdvice(rows);
  ok('xl 접두사만 내 안내다', items.length === 2 && strays[0] === 'mcp__ppt__advise' && dropped === 0);
  ok('가리킬 곳이 없으면 못 가리킨다', items[1].pointable === false && items[0].pointable === true);
  const idx = new SheetIndex();
  idx.note('매출'); idx.note('지워짐');
  ok('묻기 전에는 확인 중', targetLabel(items[0], idx.map, idx.answered('매출')).includes('확인 중'));
  const token = idx.ask(); idx.answer(token, await book.sheetNames());
  ok('답이 오면 시트 이름', targetLabel(items[0], idx.map, idx.answered('매출')) === '시트 매출 · B6');
  ok('없는 시트는 없다고', targetLabel(new Advice({ id: 'x', message: 'm', sheet: '지워짐' }), idx.map, idx.answered('지워짐')).includes('없습니다'));
  ok('목록을 못 주는 호스트', targetLabel(new Advice({ id: 'x', message: 'm', sheet: '매출' }), null, true).includes('못 줍니다'));
  const p1 = await point.run(items[0]);
  ok('안내를 누르면 그 자리로 간다', p1.ok === true && book.currentSheet === '매출' && book.selected === 'B6');
  ok('없는 시트는 사유가 온다', (await point.run(new Advice({ id: 'y', message: 'm', sheet: '없음', address: 'A1' }))).reason.includes('없는 시트'));
  ok('가리킬 곳 없는 안내', (await point.run(items[1])).ok === false);
  ok('안내 노트', adviceNote({ strays: ['mcp__ppt__advise'], dropped: 1 }).includes('mcp__ppt__advise') && adviceNote({}) === '');
}

// ── 제안 카드의 말 ────────────────────────────────────────────────────────────
{
  ok('쓰기 제안', fixLabel({ tool: 'write_range', args: { sheet: '매출', address: 'B6', formulas: [['=SUM(B2:B5)']] } }).text === '매출!B6 에 값을 씁니다 (1×1)');
  ok('표시 형식 제안', fixLabel({ tool: 'set_number_format', args: { address: 'B2:B6', format: '#,##0' } }).text.includes('#,##0'));
  ok('누를 수 없는 손', fixLabel({ tool: 'delete_sheet', args: {} }).can === false && fixLabel(null).can === false);
  ok('fixBoard 가 시트·범위를 적는다', fixBoard([{ key: 'k', what: 'w', sheet: '매출', address: 'B2', does: 'd', appliable: true }]).cards[0].whereText === '시트 매출 · B2');
}

// ── 어느 통합 문서에 붙는가 ─────────────────────────────────────────────────
{
  const none = await pickBook({ office: null });
  ok('Office 가 없으면 가짜', none.why === 'no-office' && none.book instanceof FakeWorkbook && pickNote(none) === null);
  const excel = { onReady: async () => ({ host: 'Excel' }), HostType: { Excel: 'Excel', PowerPoint: 'PowerPoint' } };
  const real = await pickBook({ office: excel });
  ok('Excel 이면 진짜', real.why === null && real.book instanceof OfficeWorkbook);
  const ppt = await pickBook({ office: { ...excel, onReady: async () => ({ host: 'PowerPoint' }) } });
  ok('다른 호스트면 가짜에 사유', ppt.why === 'not-excel' && pickNote(ppt).includes('PowerPoint'));
  const slow = await pickBook({ office: { ...excel, onReady: () => new Promise((r) => setTimeout(() => r({ host: 'Excel' }), 50)) }, waitMs: 1 });
  ok('늦으면 가짜로 가되 늦은 답을 남긴다', slow.why === 'timeout' && slow.late instanceof Promise && pickNote(slow).includes('1.5초'));
  ok('늦은 답의 말', lateNote('Excel', 'Excel').includes('새로고침') && lateNote('Word', 'Excel').includes('Word') && lateFailNote(new Error('x')).includes('x'));
  const boom = await pickBook({ office: { ...excel, onReady: () => { throw new Error('boom'); } } });
  ok('던지면 사유', boom.why === 'threw' && pickNote(boom).includes('boom'));
  ok('모르는 사유는 고치라고', pickNote({ why: 'weird' }).includes('고쳐야'));
}

// ── 요구 집합과 손/화면 역할 ──────────────────────────────────────────────────
{
  const stub = (answers) => ({ context: { requirements: { isSetSupported: (n, v) => { if (answers[`${n} ${v}`] === 'throw') throw new Error('x'); return answers[`${n} ${v}`] ?? false; } } } });
  globalThis.Office = stub({ 'ExcelApi 1.7': true, 'ExcelApi 1.8': true, 'ExcelApi 1.9': 'throw', 'SharedRuntime 1.1': true });
  const caps = new OfficeWorkbook({ run: async () => {} }).capabilities();
  ok('여덟+하나를 각각 잰다', caps.measured && caps.sets.length === 9 && caps.sets[0].name === 'ExcelApi');
  ok('던진 것은 모름이지 아니오가 아니다', caps.sets.find((s) => s.version === '1.9').ok === null && caps.sets.find((s) => s.version === '1.7').ok === true);
  ok('1.7 이 있으면 손', handRole({ isHost: true, caps }).role === 'hand' && HAND_FLOOR.name === 'ExcelApi');
  const low = { measured: true, sets: [{ name: 'ExcelApi', version: '1.7', ok: false }, { name: 'ExcelApi', version: '1.4', ok: true }] };
  ok('1.7 이 없으면 화면', handRole({ isHost: true, caps: low }).role === 'viewer' && handRole({ isHost: true, caps: low }).why.includes('1.4'));
  ok('안 쟀으면 손(모르는 것을 없다로 안 읽는다)', handRole({ isHost: true, caps: { measured: false } }).role === 'hand');
  delete globalThis.Office;
  ok('가짜 문서는 안 쟀다고 말한다', book.capabilities().measured === false && capsQuiet(book.capabilities()) === false);
}

// ── 통합 문서의 안정된 이름 ───────────────────────────────────────────────────
{
  const settings = new Map();
  const run = async (fn) => fn({ workbook: { settings: {
    getItemOrNullObject: (k) => ({ load() {}, get isNullObject() { return !settings.has(k); }, get value() { return settings.get(k); } }),
    add: (k, v) => settings.set(k, v),
  } }, sync: async () => {} });
  const a = await stableBookId(run); const b = await stableBookId(run);
  ok('처음 한 번 짓고 그 뒤로는 같은 이름', a.startsWith('book-') && a === b && settings.get(BOOK_SETTING) === a);
  ok('못 적으면 빈 이름(허브가 짓는다)', (await stableBookId(async () => { throw new Error('no'); }, () => {})) === '');
  ok('선택을 읽는다', (await new OfficeWorkbook({ run: async (fn) => fn({ workbook: { getSelectedRange: () => ({ load() {}, address: '매출!B2:C3', rowCount: 2, columnCount: 2, worksheet: { load() {}, name: '매출', position: 0 }, getCell: () => ({ getResizedRange: () => ({ load() {}, values: [[1, 2], [3, 4]] }) }) }) }, sync: async () => {} }) }).selection()).address === 'B2:C3');
}

// ── 화면이 정하는 것: 엑셀 라벨 ───────────────────────────────────────────────
{
  ok('도구는 사람 말로 뜬다(xl 접두사)', toolLabel('mcp__xl__write_range') === '값 쓰기' && toolLabel('mcp__xl__list_sheets') === '시트 목차 읽기');
  const unlabeled = ALL_OPS.filter((op) => !labelledTools().includes(op));
  ok('도구 61개 전부 라벨이 있다', unlabeled.length === 0, unlabeled.join(', '));
  ok('모르는 이름은 그대로', toolLabel('mcp__xl__nope') === 'mcp__xl__nope');
  ok('검토 부탁은 시트를 든다', reviewAsk({ sheet: '매출', address: 'B2:E9', rowCount: 8, columnCount: 4 }).text.startsWith('「매출」 시트를 검토해 주세요. 특히 B2:E9'));
  ok('시트를 모르면 못 적는다', reviewAsk({}).text === '' && reviewAsk({}).note.includes('시트'));
  ok('브랜드 줄은 문서 수를 센다', brandState({ companion: 'x', session: 's', streamLive: true, hands: 2 }).includes('문서 2'));
}
// ── 대화 스트림(§5.7). 문의 계약을 그대로 시험한다 — 여기는 PowerPoint 가 없어도 다 잰다.
{
  const ev = (seq, type, text) => ({ seq, sessionId: 'A', type, data: { text } });
  const port = new FakeTranscript({
    A: [ev(1, 'prompt.submitted', '제목 키워'), ev(2, 'part.appended', '키웠습니다')],
    B: [{ seq: 1, sessionId: 'B', type: 'prompt.submitted', data: { text: '다른 대화' } }],
  });
  const read = new ReadTranscript(port);

  ok('첫 접속은 전부를 청한다', read.attach('A') === -1);
  ok('받은 만큼 커서가 선다', read.cursor.seq === 2 && read.cursor.sessionId === 'A');
  ok('다시 붙으면 그 자리부터', read.attach('A') === 2);

  // **대화가 옮겨 가면 따라간다.**
  //
  // 「새 대화 시작」은 세션을 새로 만들고 `session.moved` 하나를 남긴다. 앞 판본은 그것을
  // 모르는 이벤트로 흘려보냈고, 그 뒤로 오는 것은 전부 다른 sessionId 라 걸름망에 걸려
  // 사라졌다 — 창은 「대화 스트림이 끊겼습니다」를 띄운 채 영영 아무것도 안 그렸다.
  // 실물에서 그 화면을 봤다(2026-09-03): 모델은 그동안 슬라이드 일곱 장을 만들고 있었는데
  // 사람은 빈 창을 보고 있었다.
  {
    const moved = { seq: 3, sessionId: 'A', type: 'session.moved', data: { to: 'B' } };
    const port2 = new FakeTranscript({
      A: [ev(1, 'prompt.submitted', '앞 대화'), moved],
      B: [{ seq: 1, sessionId: 'B', type: 'part.appended', data: { text: '옮겨 온 뒤의 말' } }],
    });
    const follow = new ReadTranscript(port2);
    follow.attach('A');
    ok('옮겨 간 대화를 따라간다', follow.sessionId === 'B', String(follow.sessionId));
    ok('옮겨 온 뒤의 말이 화면에 선다',
      follow.view.rows.some((r) => r.text === '옮겨 온 뒤의 말'),
      JSON.stringify(follow.view.rows.map((r) => r.text)));
    ok('앞 대화는 안 남는다', !follow.view.rows.some((r) => r.text === '앞 대화'));
  }

  // 대화가 바뀌면 커서를 버린다. **서버가 못 잡아 주는 자리**라 우리가 메운다.
  ok('대화가 바뀌면 커서를 버린다', read.attach('B') === -1);
  // **`every` 는 빈 것에 참이다.** 지운 쪽만 물면 「앞엣것을 지웠다」와 「아무것도 안 그렸다」가
  // 같은 초록이 되고, 통째로 비우는 구현이 만점을 받는다. 이 대화의 줄이 실제로 섰는지까지
  // 같이 문다 — 부재를 재는 단언은 무엇이 남아 있어야 하는지를 같이 적어야 잰다.
  ok('앞 대화가 화면에 안 남고 이 대화가 선다',
    everyOf(read.view.rows, (r) => r.text !== '키웠습니다')
      && read.view.rows.some((r) => r.text === '다른 대화'),
    read.view.rows.map((r) => r.text).join('|'));

  // **말한 것과 보낸 것.** 위 세 줄이 무는 것은 `attach` 의 **반환값**이고, 문이 실제로 받은
  // 값은 가짜의 `calls` 에 있다. 가짜는 그걸 보라고(「시험이 보는 것: 실제로 보낸 since」)
  // 들고 있는데 **여태 아무도 안 봤다** — `since` 를 통째로 안 실어도 스위트가 초록이었다
  // (필드 드롭 계측). 정직하게 적자면 셈을 상수로 바꾸는 뮤턴트는 옆의 거절·배우 시험들이
  // 이미 잡는다. 이 줄이 혼자 잡는 것은 **계측기 쪽이 조용히 죽는 경우**고, 그게 제일 나쁜
  // 종류다 — 계측기가 안 보면 나머지 단언들이 무엇을 봤는지도 못 믿게 된다.
  ok('문에 실제로 간 것이 말한 것과 같다',
    port.calls.map((c) => `${c.sessionId}:${c.since}`).join(' → ') === 'A:-1 → A:2 → B:-1',
    port.calls.map((c) => `${c.sessionId}:${c.since}`).join(' → '));

  // 거절 프레임. 안 읽으면 보던 대화 뒤에 같은 대화의 처음이 이어 붙는다.
  const port2 = new FakeTranscript({ A: [ev(1, 'prompt.submitted', '첫 줄')] });
  const read2 = new ReadTranscript(port2);
  read2.cursor = read2.cursor.advanced('A', 40);   // 어제 커서를 들고 왔다
  read2.sessionId = 'A';
  read2.attach('A');
  // **`!== null` 은 화면이 쓰는 물음이 아니다.** view 는 `if (v.refusal)` 로 읽으므로 빈
  // 문자열이면 아무 줄도 안 그린다 — 그런데 `!== null` 은 초록이다. 그 틈으로 「거절당했다」가
  // 시험에서만 참이고 사람에게는 아무 말도 안 하는 상태가 지나간다. 서버가 준 문장을 그대로
  // 실어 오는지까지 문다(인자 드롭 계측: `Transcript.restart` 의 `why` 를 비워도 초록이었다).
  ok('로그 끝을 넘은 커서는 거절당한다', Boolean(read2.view.refusal), read2.view.refusal);
  ok('거절에 서버가 댄 사유가 실려 온다',
    /40/.test(read2.view.refusal ?? '') && /past the end/.test(read2.view.refusal ?? ''),
    read2.view.refusal);
  ok('거절 뒤 화면은 한 벌뿐이다', read2.view.rows.length === 1, `${read2.view.rows.length}줄`);
  ok('거절당한 커서는 버려진다', read2.cursor.seq === 1);

  // 모르는 종류를 안 버린다. 버리면 화면이 "아무 일도 없었다"처럼 보인다.
  //
  // **예로 든 종류가 또 바뀌었다 — 두 번째다.** 처음엔 `council.verdict` 였고 그것을 그리게
  // 되면서 `todos.changed` 로 갈았는데, 이제 그것도 판으로 그린다. 예를 그리게 되면 그 시험은
  // **규칙이 아니라 예를 지키는 것**이 되므로 아직 진짜로 안 그리는 것으로 다시 갈았다.
  //
  // 두 번 겪었으니 적어 둔다: 이 시험이 무는 것은 「이 종류를 못 그린다」가 아니라 **「못 그린
  // 것을 세어서 말한다」**이고, 그래서 예는 **언젠가 반드시 낡는다.** 낡을 때 이 자리가 빨개지는
  // 것이 정상이고, 고치는 법은 예를 갈아 끼우는 것이지 시험을 지우는 것이 아니다.
  const port3 = new FakeTranscript({ A: [] });
  const read3 = new ReadTranscript(port3);
  read3.attach('A');
  port3.push({ seq: 1, sessionId: 'A', type: 'workflow.phase', data: {} });
  port3.push({ seq: 2, sessionId: 'A', type: 'labels.changed', data: {} });
  // `!== null` 은 화면이 쓰는 물음이 아니다 — `renderUnknown` 은 `el.hidden = !note` 로
  // 읽으므로 `undefined` 면 조용히 감춘다. 뷰 모델이 이 칸을 통째로 안 실어도 이 줄이
  // `!== null` 이던 동안은 초록이었다(필드 드롭 계측). **거절 사유와 같은 어긋남이다.**
  ok('모르는 종류는 안 그려도 안 사라진다', read3.view.rows.length === 0
    && Boolean(read3.view.unknownNote), read3.view.unknownNote ?? '(말이 없다)');
  ok('안 그린 것이 몇 건인지 그 말에 든다',
    /workflow\.phase/.test(read3.view.unknownNote ?? '')
    && /labels\.changed/.test(read3.view.unknownNote ?? ''), read3.view.unknownNote);
  ok('모르는 것도 커서는 민다', read3.cursor.seq === 2);

  // 위 두 사건은 `data` 가 비어 있어서 **못 그린 것과 안 실은 것이 같게 생긴다.** 알맹이를
  // 실은 모르는 사건을 하나 넣어 본다: 줄은 서되 **글은 안 옮겨 실려야** 한다. 못 알아본 모양의
  // 페이로드를 화면 글로 옮기는 것은 우리가 무슨 말을 그리는지 모르는 채로 그리는 일이다
  // (`textOf` 의 `kind === 'unknown'` 줄). 그 인자를 떨어뜨려도 스위트가 안 죽었다.
  {
    const t = new Transcript();
    t.append({ seq: 1, type: 'workflow.phase', data: { text: '워크플로가 뭐라고 했다' } });
    ok('모르는 종류도 줄은 선다', t.rows.length === 1 && t.rows[0].kind === 'unknown',
      `${t.rows.length} / ${t.rows[0]?.kind}`);
    ok('모르는 종류의 알맹이는 글로 안 옮겨진다', t.rows[0].text === '', t.rows[0].text);
    ok('모르는 줄은 안 그려진다', t.drawnRows.length === 0, t.drawnRows.length);
  }

  // 배우를 안 보면 정책이 한 일이 사용자가 한 말로 붙는다. §5.7 이 이름까지 대 놓은 결함이라
  // 여기서 못 박는다 — 버리지도 않고(그건 TUI 가 겪은 반대쪽 결함), 말풍선으로도 안 그린다.
  const port4 = new FakeTranscript({ A: [] });
  const read4 = new ReadTranscript(port4);
  read4.attach('A');
  port4.push({ seq: 1, sessionId: 'A', type: 'prompt.submitted',
    actor: { kind: 'user', id: 'u' }, data: { text: '제목 키워' } });
  port4.push({ seq: 2, sessionId: 'A', type: 'prompt.submitted',
    actor: { kind: 'system', id: 'policy' }, data: { text: 'allow-once (기본값)' } });
  const kinds4 = read4.view.rows.map((r) => r.kind);
  ok('정책이 낸 줄은 사용자 말풍선이 아니다',
    kinds4.length === 2 && kinds4[0] === 'user' && kinds4[1] === 'note', kinds4.join('/'));

  // 버스 전용 이벤트는 자리를 안 가진다(seq 0). 그대로 커서에 넣으면 자리가 **뒤로 가고**,
  // 0 은 계약상 "전부"라 다음 접속이 대화를 통째로 다시 받는다 — 거절 프레임도 없이 조용히.
  port4.push({ seq: 0, sessionId: 'A', type: 'part.delta',
    data: { messageId: 'm1', text: '키' } });
  ok('자리 없는 이벤트는 자리를 안 만든다',
    read4.transcript.rows.at(-1).positioned === false);
  ok('그래서 다시 붙어도 처음부터가 아니다', read4.attach('A') === 2);

  // 배우를 **안 밝힌** 줄. 코어의 `Actor` 는 구조체라 빈 `kind` 로도 오고, 그건 「사용자가
  // 넣었다」가 아니다. 「user 가 아니면」으로 물으면 이게 말풍선이 되고, 그 다음이 더 나쁘다 —
  // 낸 글을 지우는 신호가 사용자 줄의 **수**라 남의 줄 하나가 사람이 쓰던 글을 지운다.
  port4.push({ seq: 3, sessionId: 'A', type: 'prompt.submitted',
    actor: { kind: '', id: '' }, data: { text: '누가 넣었는지 안 실린 줄' } });
  port4.push({ seq: 4, sessionId: 'A', type: 'prompt.submitted',
    data: { text: '배우 자체가 없는 줄' } });
  const kindsAnon = read4.view.rows.slice(3).map((r) => r.kind);
  ok('안 밝힌 배우를 사용자로 세지 않는다',
    kindsAnon.length === 2 && everyOf(kindsAnon, (k) => k === 'note'), kindsAnon.join('/'));
  ok('안 밝힌 것과 밝힌 것을 줄이 구분해 든다',
    read4.view.rows[1].attributed === true && read4.view.rows[3].attributed === false
      && read4.view.rows[4].attributed === false);

  // 화면이 그 차이를 실제로 말하는가. 「사람이 아닌 배우가 넣었다」는 밝혔을 때만 할 수 있는
  // 말이고, 안 밝힌 줄에 그걸 적으면 모르는 것을 아는 것처럼 적는 것이다.
  ok('머리도 둘을 다르게 적는다',
    headOf(read4.view.rows[1]) !== headOf(read4.view.rows[3])
      && headOf(read4.view.rows[3]) === '⟳ 누가 넣었는지 안 밝힌 줄',
    String(headOf(read4.view.rows[3])));
  // 머리는 「누가 무엇을 끼웠는가」로 적는다 — 「사람이 아닌 배우」는 코어 필드의 직역이었다(2026-09-05).
  ok('중간 지시는 그렇게 읽힌다', noteHead({ kind: 'system', id: 'steer' }).includes('중간에 보낸 지시') && noteHead({ kind: 'system', id: 'steer' }).includes('사용자'));
  ok('카운슬·압축·플러그인은 제 이름으로', noteHead({ kind: 'system', id: 'council' }).includes('카운슬')
    && noteHead({ kind: 'system', id: 'compact' }).includes('압축') && noteHead({ kind: 'system', id: 'plugin' }).includes('플러그인'));
  ok('모르는 시스템 id 는 magi 와 id 로', noteHead({ kind: 'system', id: 'loop' }) === '⟳ magi 가 넣은 줄 (loop)');
  ok('다른 에이전트는 이름을 댄다', noteHead({ kind: 'agent', id: 'explorer' }).includes('explorer'));
  ok('「배우」라는 말은 화면에 없다', !noteHead({ kind: 'system', id: 'x' }).includes('배우'));

  // 그리고 이게 왜 화면 모양만의 문제가 아닌지 — 컴포저까지 내려가서 잰다.
  const anonComp = new Composer();
  anonComp.hold('사람이 쓰던 글', read4.view.rows.filter((r) => r.kind === 'user').length);
  port4.push({ seq: 5, sessionId: 'A', type: 'prompt.submitted',
    actor: { kind: '', id: '' }, data: { text: '또 안 밝힌 줄' } });
  ok('안 밝힌 줄은 사람이 쓰던 글을 안 지운다',
    anonComp.echoed(read4.view.rows.filter((r) => r.kind === 'user').length) === false);

  // **되풀이는 한 번만 그린다.** 스트림이 끊겼다 다시 붙으면 헬퍼가 그 대화의 앞을 다시 흘린다 —
  // 이미 그린 순번은 다시 안 그린다(실물 2026-09-05: 카운슬 판정 셋이 두 번 떴다).
  {
    const port5 = new FakeTranscript({ R: [] });
    const read5 = new ReadTranscript(port5);
    read5.attach('R');
    for (const seq of [1, 2, 3]) port5.push({ seq, sessionId: 'R', type: 'council.verdict', actor: { kind: 'system', id: 'council' }, data: { round: 1, member: 'M' + seq, decision: 'done', rationale: 'r' } });
    const before = read5.view.rows.length;
    for (const seq of [1, 2, 3]) port5.push({ seq, sessionId: 'R', type: 'council.verdict', actor: { kind: 'system', id: 'council' }, data: { round: 1, member: 'M' + seq, decision: 'done', rationale: 'r' } });
    ok('다시 온 순번은 다시 안 그린다', read5.view.rows.length === before && before === 3, `${before} → ${read5.view.rows.length}`);
    port5.push({ seq: 4, sessionId: 'R', type: 'council.verdict', actor: { kind: 'system', id: 'council' }, data: { round: 1, member: 'M4', decision: 'done', rationale: 'r' } });
    ok('새 순번은 그린다', read5.view.rows.length === 4);
  }

  // **말풍선의 상태.** 턴을 끄는 말은 처리 중, 턴 도는 중에 온 말은 대기 중, 끝나면 없음.
  // 큐의 말은 되살아나거나(resurfacedFrom) 모델이 지금 턴에 합치면(route_interjection append) 처리된 것.
  {
    const port6 = new FakeTranscript({ Q: [] });
    const read6 = new ReadTranscript(port6);
    read6.attach('Q');
    const u = (seq, id, extra = {}) => port6.push({ seq, sessionId: 'Q', type: 'prompt.submitted', actor: { kind: 'user', id: 'u' }, data: { messageId: id, parts: [{ kind: 'text', text: '말 ' + id }], ...extra } });
    u(1, 'm1');
    const rows = () => read6.view.rows.filter((r) => r.kind === 'user').map((r) => r.status).join(',');
    ok('첫 말은 처리 중', rows() === 'running', rows());
    u(2, 'm2');
    ok('턴 도는 중에 온 말은 대기 중', rows() === 'running,queued', rows());
    port6.push({ seq: 3, sessionId: 'Q', type: 'turn.finished', actor: { kind: 'system', id: 'loop' }, data: { usage: {} } });
    ok('턴이 끝나면 끈 말은 끝, 큐는 그대로', rows() === 'done,queued', rows());
    u(4, 'm2r', { resurfacedFrom: 'm2' });
    ok('되살아나면 원래 말은 끝, 되살아난 말이 처리 중', rows() === 'done,done,running', rows());
    u(5, 'm3');
    port6.push({ seq: 6, sessionId: 'Q', type: 'part.appended', actor: { kind: 'agent', id: 'a' }, data: { messageId: 'x', role: 'assistant', part: { kind: 'tool-call', toolCall: { callId: 'c', name: 'route_interjection', args: { action: 'append' } } } } });
    ok('모델이 지금 턴에 합치면 큐의 말은 끝', rows() === 'done,done,running,done', rows());
    ok('배지 문구', userBadge('running').kind === 'running' && userBadge('queued').text.includes('대기') && userBadge('done') === null);
    // 코어의 큐 이벤트가 오면 그것이 정본이다 — 줄로 안 그리고 상태에 얹는다.
    const before7 = read6.view.rows.length;
    u(7, 'm4');
    port6.push({ seq: 8, sessionId: 'Q', type: 'interjection.deferred', actor: { kind: 'system', id: 'loop' }, data: { messageId: 'm4' } });
    ok('interjection.deferred 는 그 말을 대기 중으로 만들고 줄은 안 는다', rows().endsWith(',queued') && read6.view.rows.length === before7 + 1 && !read6.view.unknownNote, String(read6.view.unknownNote));
    port6.push({ seq: 9, sessionId: 'Q', type: 'interjection.deferred', actor: { kind: 'system', id: 'loop' }, data: { messageId: 'm4', resolved: true } });
    ok('resolved 면 끝', rows().endsWith(',done'), rows());
    u(10, 'm5');
    port6.push({ seq: 11, sessionId: 'Q', type: 'interjection.answered', actor: { kind: 'system', id: 'loop' }, data: { messageId: 'm5' } });
    ok('interjection.answered 도 끝', rows().endsWith(',done'), rows());
    // 대기 중인 말풍선은 모양이 다르다 — 줄의 클래스로 갈린다(CSS 가 점선·흐린 글자를 건다).
    const urows = read6.view.rows.filter((r) => r.kind === 'user');
    ok('말풍선 클래스에 상태가 실린다', rowClass(urows[2]).includes('status-running') && rowClass(urows[1]).includes('status-done') && !rowClass({ kind: 'model' }).includes('status-'));
  }

  // **카운슬의 한 판정은 두 번 온다** — 심의 중 살아 있는 이벤트(seq 0)와 끝난 뒤의 기록(seq N).
  // 창은 한 줄로 접는다(실물 2026-09-05: 위원 셋이 두 번씩 떴다).
  {
    const port7 = new FakeTranscript({ V: [] });
    const read7 = new ReadTranscript(port7);
    read7.attach('V');
    const v = (seq, member) => port7.push({ seq, sessionId: 'V', type: 'council.verdict', actor: { kind: 'system', id: 'council' }, data: { round: 1, member, lens: 'x', decision: 'continue', rationale: '이유 ' + member } });
    v(0, 'Melchior'); v(0, 'Balthasar'); v(0, 'Casper');
    v(21, 'Melchior'); v(22, 'Balthasar'); v(23, 'Casper');
    const verdicts = read7.view.rows.filter((r) => r.kind === 'council' && r.council?.stage === 'verdict');
    ok('살아 있는 판정과 기록된 판정은 한 줄이다', verdicts.length === 3, String(verdicts.length));
    ok('자리는 기록 쪽 것을 갖는다', verdicts.map((r) => r.seq).join(',') === '21,22,23', verdicts.map((r) => r.seq).join(','));
    port7.push({ seq: 30, sessionId: 'V', type: 'council.verdict', actor: { kind: 'system', id: 'council' }, data: { round: 2, member: 'Melchior', lens: 'x', decision: 'done', rationale: 'r2' } });
    ok('다른 회차는 새 줄이다', read7.view.rows.filter((r) => r.kind === 'council' && r.council?.stage === 'verdict').length === 4);
  }

  // 델타와 완성본은 같은 말 두 번이다(같은 messageId). 둘 다 쌓으면 모델의 답이 두 번 뜨고,
  // 다시 붙은 창은 `appended` 만 받으므로 **붙어 있던 창과 화면이 갈린다.**
  const port5 = new FakeTranscript({ A: [] });
  const read5 = new ReadTranscript(port5);
  read5.attach('A');
  port5.push({ seq: 0, sessionId: 'A', type: 'part.delta', data: { messageId: 'm1', text: '키' } });
  port5.push({ seq: 0, sessionId: 'A', type: 'part.delta',
    data: { messageId: 'm1', text: '웠습니다' } });
  const live5 = read5.view.rows.map((r) => r.text);
  ok('조각은 한 줄로 이어진다', live5.length === 1 && live5[0] === '키웠습니다', live5.join('|'));
  port5.push({ seq: 1, sessionId: 'A', type: 'part.appended',
    data: { messageId: 'm1', part: { text: '키웠습니다' } } });
  const after5 = read5.view.rows;
  ok('완성본이 와도 줄이 늘지 않는다',
    after5.length === 1 && after5[0].text === '키웠습니다', `${after5.length}줄`);
  ok('완성본이 오면 자리가 생긴다', after5[0].positioned && read5.cursor.seq === 1);

  // 그리고 나중에 붙은 창(= replay 로 `appended` 만 받는 쪽)이 같은 화면을 봐야 한다.
  const read5b = new ReadTranscript(port5);
  read5b.attach('A');
  ok('나중에 붙은 창도 같은 화면이다',
    read5b.view.rows.length === 1 && read5b.view.rows[0].text === '키웠습니다',
    read5b.view.rows.map((r) => r.text).join('|'));

  // 빈 대화에 붙으면 이벤트가 한 장도 안 온다. 그때 알려 주지 않으면 화면에는 **붙기 전에
  // 그린 그림**이 그대로 서 있다 — 브라우저에서 「스트림이 끊겼습니다」가 그렇게 떠 있었다.
  {
    const empty = new FakeTranscript({ Z: [] });
    let drew = 0;
    const r = new ReadTranscript(empty);
    r.onChange = () => { drew += 1; };
    r.attach('Z');
    ok('빈 대화에 붙어도 한 번은 알린다', drew === 1, String(drew));
    ok('붙자마자 살아 있다고 말한다', r.view.live === true);
  }

  // 끊김. 문은 깨끗한 끝을 에러로 안 준다 — 그래서 조용한 대화와 죽은 스트림이 똑같이 생겼다.
  ok('붙어 있는 동안은 살아 있다', read3.view.live === true);
  port3.drop();
  ok('끊기면 화면이 그걸 안다', read3.view.live === false);

  // 연결이 둘이라는 사실(§5.7 — `transcript` 는 연결을 통째로 가져가므로 헬퍼는 두 번 붙는다).
  // 요청 쪽이 멀쩡히 도는 것이 스트림이 살아 있다는 증거가 아니다. 그래서 제출이 성공해도
  // `live` 가 되살아나면 안 된다 — 되살아나면 화면은 죽은 스트림을 살아 있다고 그린다.
  const chat = new FakeChat(new FakeTranscript(), { sessionId: 'sess-other', delay: -1 });
  const send = new SendTurn(chat, new Composer());
  const sent = await send.run('제목 줄여줘', { userRows: 0, live: read3.view.live });
  ok('스트림이 죽어도 제출은 간다', sent.sent === true && chat.sent[0] === '제목 줄여줘');
  ok('제출 성공이 스트림을 되살리지 않는다', read3.view.live === false);
  // 메아리가 올 곳이 없는데 잠그면 **영영 안 풀린다.** 그 대신 갔는지 모른다고 말한다.
  ok('메아리를 못 받을 땐 안 잠근다',
    sent.blind === true && send.composer.waiting === false);
}

// ── 조각의 종류(§5.7). 코어는 `messageId` 하나에 조각 **하나**를 싣는다(`PartAppendedData`).
// 그래서 「모델이 말하고 도구를 부른 턴」은 같은 messageId 로 이벤트가 둘 온다.
{
  const app = (mid, part) => ({ seq: 0, type: 'part.appended', data: { messageId: mid, part } });
  const dlt = (mid, kind, text) =>
    ({ seq: 0, type: 'part.delta', data: { messageId: mid, kind, text } });

  // 도구 호출이 답을 지우던 자리. 완성본은 통째라 덮어쓰는데, 조각 종류를 안 보면
  // **글 없는 도구 조각이 모델의 답을 덮는다.**
  const t1 = new Transcript();
  t1.append(app('m1', { kind: 'text', text: '키웠습니다' }));
  const call = { callId: 'c1', name: 'mcp__ppt__set_text' };
  t1.append(app('m1', { kind: 'tool-call', toolCall: call }));
  const said = t1.rows.find((r) => r.kind === 'model');
  ok('도구 호출이 모델의 답을 안 지운다', said?.text === '키웠습니다', said?.text ?? '(없음)');
  ok('도구 호출은 제 줄로 선다', t1.rows.length === 2 && t1.rows[1].kind === 'tool');
  ok('도구 줄은 이름을 들고 있다',
    t1.rows[1].tool === 'mcp__ppt__set_text', t1.rows[1].tool ?? '(없음)');

  // 추론은 모델의 혼잣말이지 사용자에게 한 말이 아니다. 델타도 종류를 싣는다(`PartDeltaData`).
  const t2 = new Transcript();
  t2.append(dlt('m2', 'reasoning', '음… 상자 폭 문제군'));
  t2.append(dlt('m2', 'text', '키웠습니다'));
  const answer = t2.rows.find((r) => r.kind === 'model');
  ok('추론이 답풍선에 안 섞인다', answer?.text === '키웠습니다', answer?.text ?? '(없음)');
  ok('그렇다고 추론을 버리지도 않는다', t2.rows.some((r) => r.kind === 'think'));

  // 델타로 쌓다 완성본이 오면 같은 말이라 덮어쓴다. 여기까지는 예전 그대로.
  const t3 = new Transcript();
  t3.append(dlt('m3', 'text', '키'));
  t3.append(dlt('m3', 'text', '웠습니다'));
  t3.append(app('m3', { kind: 'text', text: '키웠습니다' }));
  ok('델타 뒤 완성본은 되풀이가 아니다',
    t3.rows.length === 1 && t3.rows[0].text === '키웠습니다', t3.rows.map((r) => r.text).join('|'));

  // 플러그인이 넣은 사용자 메시지(⟦landing⟧ …)는 사람의 말풍선으로 안 선다 — 수만 센다.
  {
    const t = new Transcript();
    t.append({ seq: 1, type: 'prompt.submitted', actor: { kind: 'user' }, data: { text: PLUGIN_NUDGE_MARK + ' 이 턴은 land 없이 끝났습니다.' } });
    t.append({ seq: 2, type: 'prompt.submitted', actor: { kind: 'user' }, data: { text: '제목 키워' } });
    ok('플러그인 넛지는 말풍선이 아니다', t.rows.length === 1 && t.rows[0].text === '제목 키워', t.rows.map((r) => r.text).join('|'));
    ok('안 그린 수를 센다', t.skippedCounts.get('plugin.nudge') === 1);
    ok('표식은 첫머리만 본다', isPluginNudge('  ⟦landing⟧ x') && !isPluginNudge('x ⟦landing⟧'));
    const lua = readFileSync(new URL('../../../../plugins/landing/init.lua', import.meta.url), 'utf8');
    ok('플러그인과 창이 같은 표식을 쓴다', lua.includes('local NUDGE_MARK = "' + PLUGIN_NUDGE_MARK + '"'));
  }

  // 끝난 턴이 살아 있는 이벤트(seq 0)와 기록(seq>0)으로 두 번 오면 「응답 끝」은 한 줄이다(사용자 2026-09-06:
  // 「카운슬을 켜든 끄든 저거 두 번 와」). 사람 말이 사이에 있으면 정말 두 턴이다.
  {
    const t = new Transcript();
    t.append({ seq: 1, type: 'prompt.submitted', actor: { kind: 'user' }, data: { text: '하이' } });
    t.append({ seq: 0, type: 'turn.finished', data: {} });
    t.append({ seq: 4, type: 'turn.finished', data: {} });
    const ends = t.rows.filter((r) => r.kind === 'turn');
    ok('살아 있는 끝과 기록된 끝은 한 줄', ends.length === 1 && ends[0].seq === 4, String(ends.length));
    t.append({ seq: 5, type: 'prompt.submitted', actor: { kind: 'user' }, data: { text: '또' } });
    t.append({ seq: 6, type: 'turn.finished', data: {} });
    ok('사람 말 뒤의 끝은 새 끝', t.rows.filter((r) => r.kind === 'turn').length === 2);
  }

  // land 로 끝난 턴은 「응답 끝」 줄을 안 그린다 — 착지의 답이 이미 끝을 말했다(사용자 2026-09-06).
  {
    const user = { seq: 1, type: 'prompt.submitted', actor: { kind: 'user' }, data: { text: '정리해' } };
    const call = (mid, cid, name) => ({ seq: 2, type: 'part.appended', data: { messageId: mid, part: { kind: 'tool-call', toolCall: { callId: cid, name } } } });
    const res = (cid, isError) => ({ seq: 3, type: 'part.appended', data: { messageId: 'r', part: { kind: 'tool-result', toolResult: { callId: cid, content: '"x"', isError } } } });
    const end = { seq: 4, type: 'turn.finished', data: {} };
    const t1 = new Transcript();
    [user, call('m', 'c1', 'land'), res('c1', false), end].forEach((e) => t1.append(e));
    ok('land 가 받아들여진 턴은 landed', t1.rows.find((r) => r.kind === 'turn')?.landed === true);
    const t2 = new Transcript();
    [user, call('m', 'c2', 'land'), res('c2', true), end].forEach((e) => t2.append(e));
    ok('land 가 거절된 턴은 landed 가 아니다', t2.rows.find((r) => r.kind === 'turn')?.landed === false);
    const t3 = new Transcript();
    [user, call('m', 'c3', 'mcp__ppt__set_text'), res('c3', false), end].forEach((e) => t3.append(e));
    ok('land 없이 끝난 턴은 그대로 그린다', t3.rows.find((r) => r.kind === 'turn')?.landed === false);
    const t4 = new Transcript();
    [user, call('m', 'c4', 'land'), res('c4', false), { ...user, seq: 5 }, end].forEach((e) => t4.append(e));
    ok('그 뒤에 사람 말이 오면 새 턴이다', t4.rows.find((r) => r.kind === 'turn')?.landed === false);
    const view = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
    ok('그리는 쪽이 landed 턴의 끝 줄을 숨긴다', /shape === 'turn'[\s\S]{0,200}r\.landed && !r\.unverified[\s\S]{0,40}el\.hidden = true/.test(view));
  }

  // **같은 완성본이 두 번**(살아 있는 seq 0 + 기록된 seq)은 되풀이다 — 답의 끝이 두 번 서던 자리
  // (사용자 2026-09-06). 글이 같고 한쪽이 자리 없는 이벤트면 한 번으로 접는다.
  const tDup = new Transcript();
  tDup.append(dlt('m5', 'text', '키웠'));
  tDup.append(dlt('m5', 'text', '습니다.'));
  tDup.append(app('m5', { kind: 'text', text: '키웠습니다.' }));
  tDup.append({ seq: 9, type: 'part.appended', data: { messageId: 'm5', part: { kind: 'text', text: '키웠습니다.' } } });
  ok('살아 있는 완성본과 기록된 완성본은 한 번이다',
    tDup.rows.length === 1 && tDup.rows[0].text === '키웠습니다.' && tDup.rows[0].seq === 9, tDup.rows.map((r) => r.text).join('|'));
  // 둘 다 자리가 있고 자리가 다르면 정말 같은 말을 두 번 한 것이다 — 그대로 잇는다.
  const tDupB = new Transcript();
  tDupB.append({ seq: 3, type: 'part.appended', data: { messageId: 'm5b', part: { kind: 'text', text: '네.' } } });
  tDupB.append({ seq: 4, type: 'part.appended', data: { messageId: 'm5b', part: { kind: 'text', text: '네.' } } });
  ok('자리가 다른 같은 글 둘은 두 번이다', tDupB.rows[0].text === '네.네.', tDupB.rows[0].text);

  // 그런데 **완성본 둘**은 되풀이가 아니라 다음 조각이다(로그를 처음부터 읽으면 델타가 없다).
  const t4 = new Transcript();
  t4.append(app('m4', { kind: 'text', text: '먼저.' }));
  t4.append(app('m4', { kind: 'text', text: ' 그리고.' }));
  ok('완성본 둘은 이어 붙는다',
    t4.rows.length === 1 && t4.rows[0].text === '먼저. 그리고.', t4.rows[0].text);

  // 못 그리는 조각. 「part.appended 3건」은 무엇을 못 그렸는지 안 알려 준다.
  const t5 = new Transcript();
  t5.append(app('m5', { kind: 'image', image: { path: 'a.png' } }));
  ok('못 그린 조각은 조각 이름까지 적는다',
    /part\.appended \(image\)/.test(t5.unknownNote ?? ''), t5.unknownNote ?? '(없음)');
}

// ── 죽은 컴패니언은 헬퍼에 다시 마련해 달라고 한다(768aa9f8 뒤 헬퍼는 물어야 띄운다).
{
  let owns = 0;
  const dead = { async status() { return { reachable: false, why: 'a socket is at x.sock but nothing is listening — the daemon died', doing: '', pending: null }; }, async own() { owns++; return { phase: 'working' }; } };
  const w = new WatchPrompt(dead);
  await w.poll(); await w.poll(); await w.poll();
  await new Promise((r) => setTimeout(r, 0));
  ok('죽은 데몬을 보면 헬퍼에 다시 마련해 달라고 한다 — 15초에 한 번', owns === 1 && w.reasked === 1, `own ${owns}번`);
  w.askedOwnAt = Date.now() - 16000; await w.poll(); await new Promise((r) => setTimeout(r, 0));
  ok('15초가 지나면 다시 묻는다', owns === 2 && w.reasked === 2, `own ${owns}번`);
  const gone = new WatchPrompt({ async status() { return { reachable: false, why: 'no magi daemon at C:/x/daemon.sock', doing: '', pending: null }; }, async own() { owns += 1; } });
  await gone.poll(); await new Promise((r) => setTimeout(r, 0));
  ok('소켓 파일이 없어진 것도 죽음이다', owns === 3, `own ${owns}번`);
  const mute = new WatchPrompt({ async status() { return { reachable: false, why: 'helper unreachable', doing: '', pending: null }; }, async own() { owns += 10; } });
  await mute.poll(); await new Promise((r) => setTimeout(r, 0));
  ok('죽음이 아닌 불통에는 안 묻는다', owns === 3, `own ${owns}`);
  const fine = new WatchPrompt({ async status() { return { reachable: true, doing: '', pending: null }; }, async own() { owns += 100; } });
  await fine.poll(); await new Promise((r) => setTimeout(r, 0));
  ok('닿는 동안은 안 묻는다', owns === 3, `own ${owns}`);
  const noOwn = new WatchPrompt({ async status() { return { reachable: false, why: 'the daemon died', doing: '', pending: null }; } });
  await noOwn.poll();
  ok('own 이 없는 포트(가짜)에서는 조용히 넘어간다', noOwn.reasked === 0);
}

// ── 권한 확인 요청(§5.7). 스트림에 안 오는 것이라 따로 돈다.
{
  const st = new FakeStatus();
  let drew = 0;
  const w = new WatchPrompt(st, { onChange: () => { drew++; } });

  st.ask({ id: 'call_7', kind: 'permission', what: 'mcp__ppt__set_text',
    reason: '쓰기 도구는 허용 규칙에 없습니다' });
  await w.poll();
  ok('묻는 것이 서면 화면에 선다', w.view.pending?.id === 'call_7');
  // 이 물음에는 인자가 없다 — 소켓의 `Args` 는 `omitempty` 라 **진짜로 이렇게 온다.** 화면이
  // 이때 인자 칸을 통째로 안 만들면 사람은 무엇을 허가하는지 모른 채 누른다(`askArgs`).
  ok('인자 없이 온 권한 확인 요청은 그 사실이 칸의 내용이다',
    askArgs(w.view.pending)?.note != null);

  // 폴링이 같은 것을 계속 실어 온다. 매번 새로 그리면 고르던 것이 지워지고, 스크린 리더는
  // 대기가 이어지는 내내 같은 말을 되풀이한다.
  const before = drew;
  await w.poll(); await w.poll();
  ok('같은 물음을 다시 그리지 않는다', drew === before, `${drew - before}회 더 그림`);

  // 답을 보내는 것과 물음이 내려가는 것은 다른 일이다. 직접 내리면 답이 실패했는데도 사라진다.
  await w.answer('always');
  ok('답은 call id 로 간다',
    st.answers.length === 1 && st.answers[0].callId === 'call_7'
    && st.answers[0].decision === 'always');
  ok('보냈다고 화면에서 안 내린다', w.view.pending?.id === 'call_7');
  st.clear();
  await w.poll();
  ok('내려가는 것은 다음 status 가 말한다', w.view.pending === null);
  ok('우리가 답한 것으로 적힌다', w.view.clearedBy === CLEARED.answered);

  // 남이 답한 경우. 무엇으로 답했는지는 안 찍는다 — 찍으면 남의 입에 결정을 넣는 것이 된다.
  st.ask({ id: 'call_8', kind: 'permission', what: 'mcp__xl__delete_sheet' });
  await w.poll();
  st.clear();
  await w.poll();
  ok('남이 답하면 사유가 다르다', w.view.clearedBy === CLEARED.elsewhere);
  ok('무엇으로 답했는지는 안 찍는다',
    !['allow', 'deny', 'always', 'persist'].includes(w.view.clearedBy));

  // 못 닿음. 「묻는 게 없다」와 값이 같으면 안 된다 — 앞은 아는 것이고 뒤는 모르는 것이다.
  st.ask({ id: 'call_9', kind: 'permission', what: 'mcp__ppt__set_text' });
  await w.poll();
  st.reachable = false;
  await w.poll();
  ok('못 닿으면 세운 것을 내리되 사유가 다르다',
    w.view.pending === null && w.view.clearedBy === CLEARED.unreachable);
  // 여기도 `!== null` 이었다. 화면은 `lostEl(v.lostNote)` 의 `textContent` 에 그대로 꽂으므로
  // 이 칸이 비면 **「undefined」라는 글자가 사람에게 뜬다** — 안 뜨는 것보다 나쁘다.
  ok('못 닿으면 소리 내어 말한다', Boolean(w.view.lostNote), String(w.view.lostNote));
  ok('그 말이 마지막으로 읽은 것임을 밝힌다',
    /마지막으로 읽은/.test(w.view.lostNote ?? ''), w.view.lostNote);
  const said = drew;
  await w.poll(); await w.poll();
  ok('못 닿는다는 말은 한 번뿐이다', drew === said, `${drew - said}회 더 말함`);

  // 다시 닿음. **물음이 하나도 안 실려 오는 조용한 데몬**이라야 이 줄이 뭘 잡는지 보인다 —
  // 물음이 같이 오면 그 분기가 대신 그려 줘서, 고장 나 있어도 시험은 통과한다.
  st.clear();
  st.reachable = true;
  const lost = drew;
  await w.poll();
  ok('다시 닿으면 바꿔 그린다', drew > lost, '「안 닿습니다」가 그대로 서 있음');
  ok('다시 닿아도 왜 내려갔는지는 남는다', w.view.clearedBy === CLEARED.unreachable);
  const back = drew;
  await w.poll(); await w.poll();
  ok('다시 닿았다는 말도 한 번뿐이다', drew === back, `${drew - back}회 더 말함`);

  // 문이 아예 안 열리는 것도 못 닿은 것이다. 예외를 삼키되 사실은 남긴다.
  const st2 = new FakeStatus();
  st2.throwOnStatus = true;
  const w2 = new WatchPrompt(st2);
  await w2.poll();
  ok('dial 실패도 못 닿음이다',
    w2.view.reachable === false && Boolean(w2.view.lostNote), String(w2.view.lostNote));

  // 「…하는 중」은 **지금**에 대한 말이라 못 닿는 순간 근거가 없어진다. 로그 줄은 지나간
  // 일이라 못 닿아도 참인데 이건 아니다 — 그대로 두면 죽은 데몬이 영영 일하는 중으로 선다.
  // 지우지도 않는다(뭘 하다 놓쳤는지는 알아야 한다). 값과 **아직 유효한지**를 같이 싣는다.
  const stD = new FakeStatus();
  const wD = new WatchPrompt(stD);
  stD.doing = '도구를 실행하는 중';
  await wD.poll();
  ok('하는 일은 status 가 말한 그대로 온다',
    wD.view.doing === '도구를 실행하는 중' && wD.view.doingFresh === true);
  stD.reachable = false;
  await wD.poll();
  ok('못 닿으면 하는 일을 안 지우고', wD.view.doing === '도구를 실행하는 중', wD.view.doing);
  ok('지금 읽은 것이 아니라고 값에 싣는다', wD.view.doingFresh === false);
  // 다시 닿았는데 이제 아무것도 안 하면, 마지막 읽기가 그 자리를 계속 지키면 안 된다.
  stD.reachable = true;
  stD.doing = '';
  await wD.poll();
  ok('다시 닿아 조용해지면 그 말은 내려간다',
    wD.view.doing === '' && wD.view.doingFresh === true, wD.view.doing);

  // 단추 문구가 여는 폭을 말해야 한다.
  // 「허용」/「항상 허용」이면 세션 전체를 여는 줄 모르고 누른다.
  const widths = new Set(DECISIONS.map((d) => d.width));
  ok('넷이 다 있고 폭이 셋으로 갈린다',
    DECISIONS.length === 4 && widths.size === 3, [...widths].join('/'));
  ok('문구가 폭을 말한다',
    everyOf(DECISIONS, (d) => d.width === 'call' || /대화에서|계속|설정/.test(d.label)),
    DECISIONS.map((d) => d.label).join(' · '));

  // 모르는 종류. 코어의 `Waiting.Event` 는 `default:` 로 질문 아닌 것을 전부 권한 확인 요청으로
  // 되살린다 — 새 종류가 생기면 옛 창이 「허용/거절」 단추를 달고 그리고, 사람이 누른 결정은
  // 그 종류가 기다리는 답이 아니다. 이 창은 넘겨짚지 않는다.
  const st3 = new FakeStatus();
  const w3 = new WatchPrompt(st3);
  st3.ask({ id: 'call_10', kind: 'confirm', what: '무언가' });
  await w3.poll();
  ok('모르는 종류도 대기 중이라는 사실은 보여 준다', w3.view.pending?.id === 'call_10');
  ok('모르는 종류를 권한으로 넘겨짚지 않는다', w3.view.pending?.known === false);
  ok('모르는 종류는 사실만 적는다', /kind=confirm/.test(w3.view.unknownKindNote ?? ''));
  let refused = false;
  try { await w3.answer('allow'); } catch { refused = true; }
  ok('모르는 종류에 allow 를 안 보낸다', refused && st3.answers.length === 0);

  // 종류가 없는 것도 권한이 아니다 — 없는 것을 기본값으로 메우면 위와 같은 사고가 된다.
  st3.ask({ id: 'call_11', what: '종류 없음' });
  await w3.poll();
  ok('종류가 없으면 없는 대로 든다', w3.view.pending?.kind === '' && !w3.view.pending.known);

  // 질문은 손이 다르다. 권한은 정해진 낱말 넷이고 질문은 사람이 고른 글이다.
  st3.ask({ id: 'call_12', kind: 'question', what: '어느 장에 넣을까요?',
    options: ['3장', '새 장'] });
  await w3.poll();
  await w3.choose('새 장');
  ok('질문의 답은 글로 간다',
    st3.answers.length === 1 && st3.answers[0].callId === 'call_12'
    && st3.answers[0].text === '새 장');
  // 사유까지 본다. 이 물음은 **이미 답한** 것이기도 해서 거절 이유가 둘 겹치는데, 종류
  // 어긋남은 이 코드의 결함이고 「이미 보냄」은 사람이 두 번 누른 흔한 일이다. 결함이 흔한
  // 일에 가리면 안 되므로 나와야 하는 말은 종류 쪽이다.
  let wrongHand = '';
  try { await w3.answer('allow'); } catch (e) { wrongHand = e.message; }
  ok('질문에 권한의 낱말을 안 보낸다', wrongHand !== '' && st3.answers.length === 1);
  ok('겹치면 종류 어긋남을 먼저 말한다', /kind=question/.test(wrongHand), wrongHand);

  // 두 번 누르기. 답을 보내도 물음은 다음 `status` 까지 화면에 서 있으므로 단추도 서 있다.
  // 둘째 답은 코어까지 가면 어차피 떨어지지만, 돌아오는 말이 "이미 결정됐거나 만료됐다"라
  // 아무 잘못 없는 사람에게 오류로 뜬다. 여기서 막는다.
  const st4 = new FakeStatus();
  const w4 = new WatchPrompt(st4);
  st4.ask({ id: 'call_20', kind: 'permission', what: 'bash' });
  await w4.poll();
  ok('보내기 전에는 안 잠겨 있다', w4.view.answered === false);
  await w4.answer('allow');
  ok('보낸 뒤에는 잠긴다', w4.view.answered === true);
  let twice = false;
  try { await w4.answer('deny'); } catch { twice = true; }
  ok('같은 물음에 답이 두 번 안 간다',
    twice && st4.answers.length === 1 && st4.answers[0].decision === 'allow');
  // 폴이 계속 같은 것을 실어 와도 잠김이 안 풀린다 — 풀리면 두 번 누르기가 되살아난다.
  await w4.poll();
  ok('같은 물음이 계속 와도 잠김이 안 풀린다', w4.view.answered === true);
  // 다음 물음은 새 물음이다. 앞의 잠김을 물려받으면 답할 수 있는 것을 못 답한다.
  st4.ask({ id: 'call_21', kind: 'permission', what: 'write_file' });
  await w4.poll();
  ok('새 물음은 안 잠겨 있다', w4.view.answered === false);
  await w4.answer('deny');
  ok('새 물음에는 답이 간다', st4.answers.length === 2 && st4.answers[1].callId === 'call_21');

  // 두 폴 사이에 물음이 갈렸는데 id 와 종류가 같은 경우. call id 는 모델이 붙이는 것이라
  // 세션이 새로 세면 되풀이된다. 물은 시각을 안 보면 이게 「안 바뀜」으로 보이고, 그러면 앞
  // 물음의 잠김이 새 물음에 그대로 걸려 **답할 수 있는 것을 못 답한다.**
  const st6 = new FakeStatus();
  const w6 = new WatchPrompt(st6);
  st6.ask({ id: 'call_1', kind: 'permission', what: 'bash', since: '2026-08-29T01:00:00Z' });
  await w6.poll();
  await w6.answer('allow');
  ok('보낸 뒤 잠긴다 (id 되풀이 대비)', w6.view.answered === true);
  st6.ask({ id: 'call_1', kind: 'permission', what: 'bash', since: '2026-08-29T01:07:00Z' });
  await w6.poll();
  ok('id 가 같아도 물은 시각이 다르면 새 물음이다', w6.view.answered === false);
  await w6.answer('deny');
  ok('되풀이된 id 의 새 물음에도 답이 간다',
    st6.answers.length === 2 && st6.answers[1].decision === 'deny');
  // 같은 것이 계속 오는 것은 여전히 같은 것이다 — 이걸 새 것으로 보면 매 폴마다 다시 그린다.
  await w6.poll();
  ok('시각까지 같으면 같은 물음이다', w6.view.answered === true);

  // 같은 물음을 보는 **동안** 뒤에 물음이 더 쌓이는 경우. 신원(id·종류·시각)은 안 바뀌므로
  // 「같은 물음」인 것이 맞고, 판을 다시 세우면 사람이 적던 답이 지워지니 안 세우는 것도 맞다.
  // 틀린 것은 **값을 옛것으로 계속 쥐는 것**이다 — 「모두 2개」가 3개가 돼도 영영 2개로 선다.
  // 신원이 같다는 말과 보여 줄 것이 같다는 말은 다른 말이다.
  const st7 = new FakeStatus();
  let drew7 = 0;
  const w7 = new WatchPrompt(st7, { onChange: () => { drew7++; } });
  const q7 = { id: 'call_30', kind: 'question', what: '어느 쪽으로?',
               since: '2026-08-29T02:00:00Z', index: 1, total: 2 };
  st7.ask(q7);
  await w7.poll();
  ok('처음엔 실린 대로 선다', w7.view.pending?.placement === '1번째 · 모두 2개',
     String(w7.view.pending?.placement));
  const rang = drew7;
  st7.ask({ ...q7, total: 3 });
  await w7.poll();
  ok('같은 물음을 보는 동안 뒤가 늘면 그 수가 따라 온다',
     w7.view.pending?.placement === '1번째 · 모두 3개', String(w7.view.pending?.placement));
  ok('보여 줄 것이 달라졌을 때만 종이 울린다', drew7 === rang + 1, `${drew7 - rang}회`);
  // 값만 바뀌고 보일 것이 그대로면 종은 안 울린다 — 울리면 매 폴마다 판이 다시 선다.
  const rang2 = drew7;
  st7.ask({ ...q7, total: 3 });
  await w7.poll();
  ok('같은 것이 또 와도 종은 안 울린다', drew7 === rang2, `${drew7 - rang2}회`);

  // 물음이 **무엇을 근거로** 왔는지. 코어가 소켓으로 실어 보내는데 이 창이 버리면 화면에
  // 남는 것은 예/아니오뿐이고, 그건 판단이 아니라 클릭이다.
  const st5 = new FakeStatus();
  const w5 = new WatchPrompt(st5);
  st5.ask({ id: 'call_30#1', kind: 'question', what: '어느 쪽으로 맞출까요?',
    options: ['왼쪽', '가운데'],
    report: [{ key: 'tried', text: '2·5·9쪽은 왼쪽입니다' },
      { key: 'leaning', text: '왼쪽으로 기웁니다' }],
    index: 1, total: 2 });
  await w5.poll();
  ok('근거를 버리지 않는다', w5.view.pending?.report.length === 2);
  ok('근거의 차례를 안 바꾼다',
    w5.view.pending.report.map((r) => r.key).join(',') === 'tried,leaning');
  ok('몇 번째 물음인지 말한다', w5.view.pending.placement === '1번째 · 모두 2개');
  // 안 실린 것을 1/1 로 지어내지 않는다.
  st5.ask({ id: 'call_31', kind: 'permission', what: 'bash' });
  await w5.poll();
  ok('안 실린 자리는 비워 둔다', w5.view.pending.placement === null
    && w5.view.pending.report.length === 0);
}

{
  const port = new FakeTranscript({ live: [] });
  const read = new ReadTranscript(port);
  read.attach('live');
  const chat = new FakeChat(port, { sessionId: 'live', delay: -1 });
  const comp = new Composer();
  const send = new SendTurn(chat, comp);
  comp.attach(new Quote({ sheet: '매출', address: 'A1', rowCount: 1, columnCount: 1, values: [['3분기 매출 전망과 지역별 분해']] }));

  // **셈을 여기서 다시 짓지 않는다.** 앞 판본은 이 자리에 `filter(kind === 'user')` 를 손으로
  // 적어 뒀는데, 그러면 아래 블록 전체가 프로덕션의 셈이 아니라 **시험이 베낀 규칙**을 재게
  // 된다 — 화면 쪽 셈이 바뀌어도 여기는 초록이다. 지금은 화면이 부르는 그 함수를 그대로 쓴다.
  const rows = () => logShapeOf(read.view).userRows;
  const r1 = await send.run('제목 줄여줘', { userRows: rows(), live: true });
  ok('보내면 간다', r1.sent === true && chat.sent.length === 1);
  // 여기서 화면에 미리 붙이면 로그가 같은 말을 실어 올 때 두 벌이 된다.
  ok('낸 것을 화면이 미리 안 붙인다',
    read.view.rows.filter((r) => r.kind === 'user').length === 1
    && read.view.rows[0].text.includes('제목 줄여줘'));
  ok('메아리가 오면 컴포저가 빈다',
    send.settle(rows()) === true && comp.pending.length === 0 && comp.waiting === false);

  // **사람 줄만 센다.** 이 셈이 모든 줄을 세면 모델이 한 마디 하는 순간 수가 늘어 `settle` 이
  // 그걸 메아리로 읽고, **아직 안 돌아온 사람 글을 지운다** — 사람은 자기가 적은 것이 갔는지
  // 모른 채 빈 칸을 본다. 위 블록은 이 갈래를 못 가른다(사람 줄만 밀어 넣으므로 어느 셈이든
  // 같은 수가 나온다). 여기서 모델 줄을 하나 밀어 가른다.
  // 표시는 `run` 을 안 거치고 직접 찍는다. 가짜 문의 `submit` 은 사람 줄을 **그 자리에서**
  // 로그에 앉히므로 `run` 으로는 이 틈이 안 생기는데, 진짜 데몬에서는 submit 이 왕복이라
  // 사람 줄이 늦게 오고 그 사이에 **앞 턴의 모델 델타**가 먼저 도착한다. 재려는 것이 그 틈이다.
  const comp7 = new Composer();
  const send7 = new SendTurn(chat, comp7);
  const mark7 = rows();
  comp7.hold('세어 보자', mark7);
  port.push({ type: 'part.appended', data: { messageId: 'm7',
    part: { kind: 'text', text: '모델이 한 마디' } } });
  ok('모델 줄은 사람 줄 수를 안 올린다', rows() === mark7, `${mark7} → ${rows()}`);
  ok('모델이 말했다고 사람 글이 지워지지 않는다',
    send7.settle(rows()) === false && comp7.waiting === true);
  port.push({ type: 'prompt.submitted', actor: { kind: 'user', id: 'attach' },
    data: { messageId: 'u7', parts: [{ kind: 'text', text: '세어 보자' }] } });
  ok('사람 줄이 오면 그때 비운다', send7.settle(rows()) === true && comp7.waiting === false);

  // 읽는 유스케이스가 없으면 **읽는 중이 아니다.** `live` 를 여기서 참으로 지어내면 위 셋째
  // 갈래(눈감고 보냄)가 안 돌고, 사람은 안 올 메아리를 기다리며 잠긴다.
  ok('읽는 데가 없으면 눈감은 것이다',
    logShapeOf(null).live === false && logShapeOf(undefined).userRows === 0);
  ok('살아 있음은 지어내지 않고 그대로 나른다',
    logShapeOf({ rows: [], live: false }).live === false
    && logShapeOf({ rows: [], live: true }).live === true);

  // 낸 뒤 메아리 전에는 잠긴다 — 두 벌로 나가는 것을 막는 자리.
  const comp2 = new Composer();
  comp2.attach(new Quote({ sheet: '매출', address: 'A2', rowCount: 1, columnCount: 1, values: [['지역별 분해']] }));
  const send2 = new SendTurn(chat, comp2);
  const before = rows();
  await send2.run('한 번 더', { userRows: 999, live: true });   // 로그가 아직 안 따라왔다
  ok('메아리 전에는 잠긴다', comp2.waiting === true);
  const again = await send2.run('또', { userRows: 999, live: true });
  ok('잠긴 동안은 두 번 안 나간다', again.sent === false && again.why === 'waiting');
  ok('안 나간 것은 로그에도 없다', rows() === before + 1, String(rows()));
  // 로그가 움직였다고 다 메아리는 아니다. 도구 줄 하나에 컴포저를 비우면 사람이 낸 글이
  // **가지도 않은 채** 화면에서 사라진다.
  ok('메아리가 아니면 안 비운다',
    send2.settle(rows()) === false && comp2.waiting === true && comp2.pending.length === 1);
  // 데몬이 물음에 막혀 있으면 메아리가 한참 뒤에 오거나 안 온다. 나가는 문이 있어야 한다.
  comp2.release();
  ok('그만 기다리면 잠금이 풀린다', comp2.waiting === false);
  // 갔는지 모르는 채로 사람 글을 지우면 화면이 「갔다」를 말한 셈이 된다.
  ok('그만 기다려도 인용은 그대로다',
    comp2.pending.length === 1 && comp2.pending[0].key === '매출!A2');

  // 셋째 갈래 — **갔는데 로그를 못 읽는다.** 잠그느냐 마느냐는 이미 위에서 진짜 끊긴
  // 스트림으로 잰다(「메아리를 못 받을 땐 안 잠근다」). 여기서 더 재는 것은 **쥐고 있던
  // 인용**이다: 갔는지 모르는 채로 그걸 버리면 화면이 「갔다」를 말한 셈이 된다.
  const comp4 = new Composer();
  comp4.attach(new Quote({ sheet: '매출', address: 'A3', rowCount: 1, columnCount: 1, values: [['지역별']] }));
  const r4 = await new SendTurn(chat, comp4).run('안 보이는 채로', { userRows: 0, live: false });
  ok('끊겨도 인용은 그대로다', r4.blind === true && comp4.pending.length === 1);

  // `live` 를 **안 넘기면** 「살아 있다」가 아니라 「모른다」다. 모르는 채 잠그면 갇힌다.
  const comp5 = new Composer();
  const r5 = await new SendTurn(chat, comp5).run('안 알려주고 낸다', { userRows: 0 });
  ok('live 를 안 넘기면 살아 있다고 치지 않는다', r5.sent === true && r5.blind === true);
  ok('안 넘겼으면 안 잠근다', comp5.waiting === false);
  const comp6 = new Composer();
  const r6 = await new SendTurn(chat, comp6).run('두 번째 인자 자체가 없다');
  ok('둘째 인자가 통째로 없어도 마찬가지다',
    r6.blind === true && comp6.waiting === false);

  // **아직 빈 대화는 눈먼 보내기가 아니다.** 코어는 첫 말이 올 때 대화를 낳으므로 첫 보내기는 늘
  // 스트림이 아직 안 산 채로 나가고, 그 말이 스트림을 살린다. 실물 2026-09-05: 새 대화의 첫
  // 보내기마다 「갔는지 확인은 못 합니다 — 적은 글은 그대로 뒀습니다」가 떴다.
  const compE = new Composer();
  const rE = await new SendTurn(chat, compE).run('첫 말', { userRows: 0, live: false, empty: true });
  ok('빈 대화의 첫 보내기는 눈먼 보내기가 아니다', rE.sent === true && rE.blind === false && compE.waiting === true, JSON.stringify(rE));
  ok('빈 대화라도 끊긴 것이면 눈먼 보내기다', (await new SendTurn(chat, new Composer()).run('x', { live: false, empty: false })).blind === true);
  ok('logShapeOf 가 empty 를 나른다', logShapeOf({ rows: [], live: false, empty: true }).empty === true && logShapeOf(null).empty === false);

  // 문이 던지면 잠금을 푼다. 삼키면 사람은 간 줄 안다.
  const boom3 = new Error('문이 닫혔습니다');
  const bad = { async submit() { throw boom3; } };
  const comp3 = new Composer();
  const r3 = await new SendTurn(bad, comp3).run('안 갈 말', { userRows: 0, live: true });
  ok('못 가면 사유가 온다', r3.sent === false && r3.why === 'failed');
  ok('못 갔으면 안 잠긴다', comp3.waiting === false);
  // **`why` 만 재고 던진 물건을 안 쟀다.** 화면은 `r.error.message` 를 감싸는 것 없이 읽으므로
  // (`view.js` 의 `onSend`), 이 칸이 비면 「못 보냈습니다」를 적으려다 그 자리에서 또 던진다 —
  // 실패를 알리는 길이 실패한다. 값이 온다가 아니라 **던진 그 물건이** 와야 한다.
  ok('못 간 사유에 던진 물건이 실린다', r3.error === boom3, String(r3.error));
  // ⚠ 짝인 `blind: false` 는 **안 문다.** 소비자가 `if (r.blind)` 라 `undefined` 와 구별을
  // 못 하고, 못 하는 것을 못박으면 아무도 안 쓰는 값을 시험이 지키는 꼴이 된다.
}
// ── 낸 결과가 **무슨 말로 나가는가**. 앞 판본은 화면 안의 `if` 둘(`failed`·`waiting`)이라
// 거기 안 걸린 결과는 **아무 말 없이** 나갔다 — 그런데 이 자리는 못 보냈을 때 사람 글을 **그대로
// 남긴다.** 조용하면 사람은 남은 글을 보고 「아직 안 눌렀나」로 읽고 다시 누른다. `SendTurn` 의
// `run` 주석이 이 침묵을 이름 대어 걱정해 두고도 아무 데서도 소리가 안 나던 자리다.
//
// **손으로 지은 답으로 안 잰다.** 다섯 중 넷은 진짜 `run` 이 낸 것을 그대로 먹인다 — 손으로
// 적으면 생산자의 철자가 바뀌어도 여기는 초록이고, 그건 두 벌이 맞는 게 아니라 갈라진 것이다.
{
  const port = new FakeTranscript({ live: [] });
  const read = new ReadTranscript(port);
  read.attach('live');
  const chat = new FakeChat(port, { sessionId: 'live', delay: -1 });
  const rows = () => logShapeOf(read.view).userRows;

  const comp = new Composer();
  const send = new SendTurn(chat, comp);
  const rLive = await send.run('갔다', { userRows: rows(), live: true });
  ok('가고 로그도 읽는 중이면 할 말이 없다', sendNote(rLive) === null, JSON.stringify(rLive));

  // 위에서 안 비웠으니 아직 잠겨 있다 — 그 잠금이 그대로 둘째 갈래를 만든다.
  const rWait = await send.run('또', { userRows: rows(), live: true });
  const nWait = sendNote(rWait);
  ok('기다리는 중이면 왜 안 갔는지 말해 준다',
    rWait.why === 'waiting' && nWait !== null && nWait.text.includes('아직'));
  // 곧 메아리가 와서 스스로 풀리는 사정이라, 이 줄은 붙어 있을 필요가 없다.
  ok('기다리라는 말은 붙어 있지 않는다', nWait.sticky === false);

  // 눈감고 보낸 것. **글이 남는다는 사실까지 적어야** 사람이 다시 안 누른다.
  const rBlind = await new SendTurn(chat, new Composer()).run('안 보이는 채로',
    { userRows: 0, live: false });
  const nBlind = sendNote(rBlind);
  ok('눈감고 보낸 것은 확인 못 한다고 말한다',
    rBlind.blind === true && nBlind !== null && nBlind.text.includes('확인'));
  ok('남은 글이 왜 남았는지까지 적는다', nBlind.text.includes('그대로 뒀습니다'));
  // 스스로 사라지면 남은 글만 남고, 그 글은 「안 눌렀다」로 읽힌다.
  ok('눈감고 보낸 말은 붙어 있는다', nBlind.sticky === true);

  const boom = new Error('문이 닫혔습니다');
  const bad = { async submit() { throw boom; } };
  const rFail = await new SendTurn(bad, new Composer()).run('안 갈 말',
    { userRows: 0, live: true });
  const nFail = sendNote(rFail);
  // 던진 쪽의 말을 안 실으면 사람은 무엇을 고쳐야 할지 모른 채 같은 단추를 다시 누른다.
  ok('못 간 것은 던진 말을 그대로 싣는다',
    nFail !== null && nFail.text.includes('문이 닫혔습니다'), JSON.stringify(nFail));
  ok('못 갔다는 말은 붙어 있는다', nFail.sticky === true);

  // 빈 상자. **여기만 조용해도 된다** — 사람이 방금 빈 칸에서 누른 것을 안다.
  const rEmpty = await new SendTurn(chat, new Composer()).run('   ',
    { userRows: 0, live: true });
  ok('빈 상자에는 할 말이 없다', rEmpty.why === 'empty' && sendNote(rEmpty) === null);

  // 여섯째 결말. **이것만 손으로 짓는다** — 오늘 `run` 이 못 내는 값이고, 못 내는 값을 위해
  // 생산자에 갈래를 하나 심으면 시험이 프로덕션을 늘리는 꼴이 된다. 재려는 것도 생산자가
  // 아니라 **화면이 모르는 것을 만났을 때**다.
  const nUnknown = sendNote({ sent: false, why: 'quota' });
  ok('모르는 사유는 조용히 안 나간다',
    nUnknown !== null && nUnknown.text.includes('quota') && nUnknown.sticky === true,
    JSON.stringify(nUnknown));

  // 갈라 놓고 같은 말로 내보내면 갈라 놓은 값이 없는 것과 같다.
  const said = [nWait, nBlind, nFail, nUnknown].map((n) => n.text);
  ok('결말마다 다른 말이 나간다', new Set(said).size === 4, said.join(' | '));
}

// ── 내려간 물음의 **사유가 무슨 말로 나가는가**. 「없다」만 남기면 셋이 화면에서 똑같이
// 생긴다는 것이 `CLEARED` 를 둔 이유인데, 그 셋을 문장으로 바꾸는 자리가 화면 안이라 안 재고
// 있었다. 게다가 거기서는 셋에 안 맞는 사유가 **`null` 로 떨어져 줄이 통째로 사라졌다** —
// 「내려간 물음이 없다」와 같은 모양으로. 없애려던 뭉갬이 한 겹 위에서 되살아난 자리다.
{
  // 사유는 손으로 안 적고 `CLEARED` 를 그대로 쓴다. 값이 한쪽에서만 바뀌면 드리프트다.
  // `answered`(이 창이 답함)는 일부러 말이 없다(2026-09-05) — 나머지 둘은 제 말을 갖고 서로 다르다.
  // `via` 는 접두어라 결정·도구가 붙어야 말이 된다(위 시험).
  const spoken = [CLEARED.elsewhere, CLEARED.unreachable].map((c) => clearedNote(c));
  ok('말이 있는 사유는 다 제 말을 갖는다', everyOf(spoken, (t) => typeof t === 'string' && t.length > 0));
  ok('둘이 서로 다른 말이다', new Set(spoken).size === 2);
  // 「모르게 된 것」을 「답했다」로 읽으면 사람이 그 물음을 잊는다.
  ok('못 닿아 내려간 것은 끝난 것이 아니라고 적는다',
    clearedNote(CLEARED.unreachable).includes('끝난 것이 아닙니다'));
  // 무엇으로 답했는지는 이 창이 모른다. 찍으면 남의 입에 결정을 넣는 것이 된다.
  ok('남이 답한 것은 무엇으로 답했는지 안 적는다',
    !DECISIONS.some((d) => clearedNote(CLEARED.elsewhere).includes(d.value)),
    clearedNote(CLEARED.elsewhere));
  // **여기만 조용해도 된다.** 내려간 물음이 없다는 뜻이라 적을 말이 없다.
  ok('내려간 것이 없으면 할 말이 없다',
    clearedNote(null) === null && clearedNote(undefined) === null);
  // **넷째 사유는 조용히 숨지 않는다.** 숨으면 물음이 사라진 자리에서 화면이 아무 말도 안 하고,
  // 답을 기다리던 사람은 자기가 뭘 놓쳤는지도 모른다.
  const fourth = clearedNote('expired');
  ok('모르는 사유는 줄을 지우는 대신 제 말을 갖고 온다',
    typeof fourth === 'string' && fourth.includes('expired'), String(fourth));
  // 객체 조회는 프로토타입까지 뒤진다 — 사유가 그런 이름이면 함수가 문장 자리에 앉았다.
  ok('프로토타입의 이름도 사유로 안 샌다', typeof clearedNote('constructor') === 'string');
}

// ── 판을 **다시 세울지** 재는 서명. 이 한 줄에 사람이 적던 답과 포커스가 달려 있는데,
// 화면 안에 있는 동안은 DOM 이 있어야 돌아서 한 번도 안 재 봤다. 재는 쪽이 없으면 이 목록은
// 나중에 고치는 사람에게 그냥 다섯 칸짜리 배열로 보이고, 한 칸 더 넣는 것이 사람이 적던 답을
// 지우는 일이라는 것을 아무도 안 말해 준다.
{
  const st = new FakeStatus();
  const w = new WatchPrompt(st, {});
  const ask = { id: 'call_9', kind: 'permission', what: 'mcp__ppt__set_text',
    reason: '쓰기 도구는 허용 규칙에 없습니다', index: 1, total: 2 };
  st.ask({ ...ask });
  await w.poll();
  const base = askSig(w.view);

  // **뒤에 쌓인 수는 서명에 안 든다.** 들면 뒤가 늘 때마다 판이 다시 서고 적던 답이 지워진다.
  st.ask({ ...ask, total: 3 });
  await w.poll();
  ok('뒤가 늘어도 판을 다시 안 세운다',
    askSig(w.view) === base && w.view.pending.placement.includes('3개'),
    `${base} / ${w.view.pending.placement}`);

  // 답을 보내면 단추가 잠긴다 — 그건 다시 그려야 보인다.
  await w.answer('always');
  ok('답을 보낸 것은 판을 다시 세운다', askSig(w.view) !== base && w.view.answered === true);

  // 다른 물음이면 다른 판이다. 신원이 안 들면 새 물음이 옛 판 위에 그려진다.
  const w2 = new WatchPrompt(new FakeStatus(), {});
  const sigOf = async (p, f = () => {}) => {
    const s2 = new FakeStatus(); const ww = new WatchPrompt(s2, {});
    s2.ask(p); await ww.poll(); await f(ww, s2); return askSig(ww.view);
  };
  const a = await sigOf({ ...ask });
  ok('물음이 바뀌면 판이 바뀐다', await sigOf({ ...ask, id: 'call_10' }) !== a);
  ok('종류가 바뀌면 판이 바뀐다', await sigOf({ ...ask, kind: 'question' }) !== a);
  // 못 닿는 동안 세워 둔 판은 답할 수 있는 판이면 안 된다.
  ok('닿지 않게 되면 판이 바뀐다',
    await sigOf({ ...ask }, async (ww, s2) => { s2.reachable = false; await ww.poll(); }) !== a);
  // 내려간 사유가 화면에 뜬다(남이 답했다 / 정책이 답했다 / 못 닿는다). 사유가 안 들면 그 셋이
  // 화면에서 똑같이 생기던 자리로 돌아간다.
  ok('내려간 사유가 바뀌면 판이 바뀐다',
    await sigOf({ ...ask }, async (ww, s2) => { s2.clear(); await ww.poll(); }) !== a);
  ok('선 물음이 하나도 없던 처음과는 다르다', askSig(w2.view) !== a);

  // **조용한 데몬이 죽는 길.** 물음이 하나도 없는 채로 못 닿게 되면 위 갈래 어느 것도 값이
  // 안 바뀐다 — 내릴 물음이 없으니 사유도 안 적힌다(`poll` 의 `if (this.pending)` 밖이다).
  // `reachable` 이 서명에 없으면 판이 그대로 서고, 화면은 「안 닿습니다」를 **영영 안 그린다.**
  // 사람은 데몬이 죽은 줄 모르고 조용한 창을 본다. 위 다섯 칸 중 이 칸만 이 길을 잡는다.
  {
    const s3 = new FakeStatus(); const q = new WatchPrompt(s3, {});
    await q.poll();
    const quiet = askSig(q.view);
    s3.reachable = false;
    await q.poll();
    ok('물음 없이 죽어도 판이 바뀐다', askSig(q.view) !== quiet,
      `${quiet} / ${askSig(q.view)}`);
  }

  // **내려간 사유는 물음이 없는 자리에서 갈린다.** 둘 다 선 물음이 없고 닿는 중인데, 화면이
  // 적을 말이 다르다(「남이 답했습니다」 / 「답을 보냈습니다」). 사유가 서명에 없으면 한쪽에서
  // 다른 쪽으로 갈 때 판이 안 서고, 앞의 말이 그대로 남는다.
  {
    const down = async (answerFirst) => {
      const s4 = new FakeStatus(); const q = new WatchPrompt(s4, {});
      s4.ask({ ...ask }); await q.poll();
      if (answerFirst) await q.answer('always');
      s4.clear(); await q.poll();
      return { sig: askSig(q.view), why: q.view.clearedBy };
    };
    const bySelf = await down(true); const byOther = await down(false);
    ok('내려간 사유가 다르면 판도 다르다',
      bySelf.sig !== byOther.sig && bySelf.why !== byOther.why,
      `${bySelf.why} / ${byOther.why}`);
  }
}

// ── 훑는 단언은 빈 것에 초록을 안 준다 ─────────────────────────────────────────
// 위 스캔과 같은 수를 이 파일 자신에게 쓴다. `[].every(f)` 가 늘 참이라 훑을 것이 없는 단언은
// 술어가 무엇이든 통과하는데, 실제로 그런 줄이 하나 서 있었다 — 표가 빈 판에서 표의 값을
// 훑었고, 술어를 상수 거짓으로 바꿔도 스위트가 초록이었다. 여덟 자리를 다 재 보니 나머지
// 일곱은 오늘 안 비어 있다. **그건 규칙이 아니라 운이고, 여덟째가 올 때 아무 데서도 소리가
// 안 난다.** 그래서 `everyOf` 하나로 길을 좁히고, 안 거친 것이 있으면 여기서 이름을 부른다.
//
// 주석 줄은 뺀다. 예외 목록이 아니라 문법 갈래라 늘어날 자리가 없다 — 바로 위 `everyOf` 의
// 설명이 자기 이야기를 하느라 `.every(` 를 적고 있고, 그걸 예외로 적기 시작하면 목록이 산다.
//
// 두 겹인 이유도 같다. 「안 거친 것이 없다」는 훑을 것을 못 찾아도 참이라, 떠받치는 줄은
// 「훑는 자리를 실제로 찾았다」 쪽이다.
{
  // 찾는 글자를 통째로 안 적는다. 적으면 **이 줄이 스스로 걸린다** — 첫 판에서 실제로 그랬고,
  // 세는 자리도 하나 부풀어 「9 자리」라고 적었다(여덟이다). 스캐너가 제 바늘에 걸리는 것을
  // 예외로 빼면 그 예외가 진짜 위반도 같이 가려 준다.
  const CALL = `.${'every'}(`;
  const VIA = `${'every'}Of(`;
  const self = readFileSync(new URL(import.meta.url), 'utf8').split('\n')
    .map((l, i) => [i + 1, l]).filter(([, l]) => !/^\s*(\*|\/\/)/.test(l));
  const sweeps = self.filter(([, l]) => l.includes(VIA)).length;
  const bare = self.filter(([, l]) => l.includes(CALL)).map(([n]) => `smoke.mjs:${n}`);
  ok('훑는 단언 자리를 실제로 찾았다', sweeps > 1, `${sweeps} 자리`);
  ok('훑는 단언이 전부 빈 것을 거르는 길로 간다', bare.length === 0, bare.join(' '));
}

// ── 접힌 판이 접힌 채로 거짓말하지 않는가 ────────────────────────────────────
//
// 작업창은 PowerPoint 에서 348×391 이라(MS 애드인 디자인 지침의 크기 표) 세로가 귀하고, 요구
// 집합 여섯 줄은 뭔가 안 될 때만 읽는 값이라 접어 뒀다. 접는 순간 규칙이 하나 생긴다:
// **요약이 사실을 말해야 한다.** 「다 좋다」로 접어 두면 아무도 펴지 않고, 안 쟀다는 사실이
// 화면에서 사라진다.
{
  const all = { measured: true, sets: [{ ok: true }, { ok: true }] };
  const some = { measured: true, sets: [{ ok: true }, { ok: false }, { ok: null }] };
  ok('안 쟀으면 요약이 안 쟀다고 적는다',
    capsSummary({ measured: false, sets: [] }).includes('재지 못했'),
    capsSummary({ measured: false, sets: [] }));
  // 카운슬 단추: 글이 동작을 적고(끕니다/켭니다), 값(재기동·새 대화)을 미리 말한다.
  {
    const on = councilButton(true), off = councilButton(false), unk = councilButton(null);
    ok('켜져 있으면 「끕니다」와 눌림', on.pressed === true && on.title.includes('끕니다') && on.title.includes('지금 켜짐'), on.title);
    ok('꺼져 있으면 「켭니다」', off.pressed === false && off.title.includes('켭니다') && off.title.includes('지금 꺼짐'), off.title);
    ok('모르면 모른다고 적는다', unk.pressed === false && unk.title.includes('모름'), unk.title);
    ok('새 대화로 시작된다는 값을 미리 적는다', everyOf([on, off, unk], (b) => b.title.includes('새 대화')));
    const html = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
    ok('단추가 고급 줄에 있다', /id="advanced"[\s\S]*?id="council"[\s\S]*?id="repick"/.test(html));
    const src = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    ok('누르면 헬퍼의 카운슬 문을 두드린다', /#council'\)\?\.addEventListener[\s\S]*?api\.setCouncil\(want\)/.test(src));
    ok('상태가 바뀌면 단추를 다시 그린다', /view\.councilButton\(watchPrompt\.view\.council\)/.test(src));
    // 폴이 말한 값이 view 에 실린다 — 바뀔 때만 onChange.
    const port = new FakeStatus();
    port.reachable = true;
    const wp = new WatchPrompt(port);
    let changes = 0; wp.onChange = () => { changes += 1; };
    await wp.poll();
    ok('모르면 null', wp.view.council === null);
    port.council = true; const before = changes; await wp.poll();
    ok('데몬이 켜졌다고 하면 true 고 onChange', wp.view.council === true && changes === before + 1, String(changes - before));
    await wp.poll();
    ok('같은 값이면 onChange 안 한다', changes === before + 1, String(changes - before));
  }

  // 다 지원이면 줄이 숨는다 — 못 쟀거나 하나라도 빠지면 보인다(숨은 채로 거짓말 금지).
  ok('요구 집합 줄은 다 지원일 때만 숨는다',
    capsQuiet({ measured: true, sets: [{ ok: true }, { ok: true }] }) === true
      && capsQuiet({ measured: false, sets: [] }) === false
      && capsQuiet({ measured: true, sets: [{ ok: true }, { ok: false }] }) === false
      && capsQuiet({ measured: true, sets: [{ ok: true }, { ok: null }] }) === false,
    'capsQuiet');
  ok('다 되면 수를 적는다', capsSummary(all).includes('2종'), capsSummary(all));
  ok('빠진 것이 있으면 접힌 줄이 그것을 적는다',
    capsSummary(some).includes('1개 없음') && capsSummary(some).includes('1개 모름'),
    capsSummary(some));
}

// 브랜드 줄(MS 지침이 작업창 아래에 두라고 적은 자리)은 **늘 사실을 적는다.**
{
  ok('안 골랐으면 안 골랐다고 적는다',
    brandState({ companion: null, streamLive: false }) === '컴패니언 미선택');
  ok('붙었으면 어디에·어느 대화·살아 있는지·손이 몇인지',
    brandState({ companion: 'deck2', session: 's_1', streamLive: true, hands: 2 }) === 'deck2 · 대화 s_1 · 대화 연결됨 · 문서 2',
    brandState({ companion: 'deck2', session: 's_1', streamLive: true, hands: 2 }));
  ok('대화가 있는데 끊기면 그렇게 적는다',
    brandState({ companion: 'deck2', session: 's_1', streamLive: false }).includes('대화 끊김'));
}


// ── 붙기 전의 창은 「고장 났다」고 말하지 않는다 ──────────────────────────────
//
// 실물에서 본 화면이 근거다(2026-09-01): 컴패니언을 고르라는 카드 위에 「데몬에 안 닿습니다」와
// 「대화 스트림이 끊겼습니다」가 노란 배너 둘로 겹쳐 떴다. 둘 다 **붙어 있던 것에 대한 말**인데
// 아직 아무 데도 안 붙었으니 참이 아니고, 사람은 고르기도 전에 고장 난 줄 안다.
{
  const notBound = { bound: false, reachable: false, pending: null, live: false };
  ok('안 붙었으면 물음 칸이 아무것도 안 그린다', askKind(notBound) === 'none', askKind(notBound));
  ok('안 붙었으면 스트림 줄이 숨는다', streamLine(notBound).hidden === true);
  // 붙은 뒤에는 **같은 값이 말을 한다** — 조용해지는 것은 붙기 전뿐이다.
  const bound = { bound: true, reachable: false, pending: null, live: false };
  ok('붙은 뒤 못 닿으면 그때는 말한다', askKind(bound) === 'lost');
  ok('붙은 뒤 스트림이 죽으면 그때는 말한다',
    streamLine(bound).hidden === false && streamLine(bound).text.includes('끊겼'));
  // bound 를 안 실어 보내는 옛 호출자도 그대로 돈다 — 없으면 예전처럼 군다.
  ok('bound 를 안 실으면 예전 그대로', askKind({ reachable: false }) === 'lost');
}

// ── 대화 이름은 우리가 짓지 않는다 ────────────────────────────────────────────
// ── 아이콘 단추는 반드시 말을 단다 ──────────────────────────────────────────────
//
// 아이콘만 두면 무슨 단추인지 모른다. M3 가 그것을 거동으로 못 박았다 — 「hover 에 **동작을
// 설명하는** 툴팁을 띄운다, **아이콘의 이름이 아니라**」. 낭독기에는 `aria-label` 이 그 몫이다.
{
  const html = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
  const btns = [...html.matchAll(/<button[^>]*class="[^"]*icon-btn[^"]*"[^>]*>/g)].map((m) => m[0]);
  // **훑을 것을 실제로 찾았는가** — 0개를 훑고 초록인 것과 다 통과한 것은 글자가 같다(§9).
  ok('아이콘 단추를 찾았다', btns.length >= 8, String(btns.length));
  // `everyOf` 는 **빈 것에 참을 안 준다**(§4.1) — `every` 로 적으면 0개를 훑고도 초록이다.
  ok('전부 툴팁이 있다', everyOf(btns, (b) => /title="[^"]{4,}"/.test(b)),
    btns.find((b) => !/title="[^"]{4,}"/.test(b)) ?? '');
  ok('전부 낭독기 이름이 있다', everyOf(btns, (b) => /aria-label="[^"]{4,}"/.test(b)),
    btns.find((b) => !/aria-label="[^"]{4,}"/.test(b)) ?? '');
  // 스프라이트는 파일 안에 있다 — 남의 주소에서 아이콘을 부르면 LNA·혼합 콘텐츠가 다시
  // 걸린다(§5.5). **글자가 아니라 주소를 센다**: 이 파일에는 그 이유를 적은 주석도 있어서
  // 낱말로 재면 제 주석에 제가 걸린다(실제로 걸렸다).
  const outside = [...html.matchAll(/(?:src|href)="(https?:\/\/[^"]+)"/g)].map((m) => m[1]);
  ok('아이콘이 파일 안에 있다', /<svg width="0"/.test(html));
  ok('밖에서 받아오는 것은 office.js 뿐이다',
    everyOf(outside, (u) => u.startsWith('https://appsforoffice.microsoft.com/')), outside.join(' '));

  // **권한 단추는 아이콘으로 안 바꾼다.** 여는 폭을 문구에 적어야 하는 자리라(§8), 「이번
  // 호출만」과 「이 세션의 set_text 전부」가 아이콘으로는 안 갈린다.
  const view = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
  ok('권한 단추는 글자로 남는다', /askAction/.test(view) && !/askAction[\s\S]{0,200}icon\(/.test(view));
}

// ── 마크다운으로 온 글을 그린다 ────────────────────────────────────────────────
//
// 모델의 답·카운슬 판정·플러그인 줄이 `**굵게**`·`|---|`·백틱 그대로 찍혔다(2026-09-05).
// 파서는 순수 함수라 여기서 재고, 짓는 쪽은 가짜 document 로 노드 모양만 본다.
{
  const bold = inlines('이건 **굵게** 와 `코드` 와 [링크](https://x.y/z) 와 *기울임*');
  ok('인라인: 굵게·코드·링크·기울임을 가른다', bold.map((k) => k.t).join(',') === 'text,strong,text,code,text,link,text,em', bold.map((k) => k.t).join(','));
  ok('링크는 http(s) 만 받고 주소를 든다', bold.find((k) => k.t === 'link')?.href === 'https://x.y/z');
  ok('javascript: 링크는 링크가 아니다', everyOf(inlines('[x](javascript:alert(1))'), (k) => k.t !== 'link'));
  ok('숫자 사이의 별표 하나는 기울임이 아니다', everyOf(inlines('3*4*5'), (k) => k.t === 'text'));
  ok('<br> 은 줄바꿈이고 다른 태그는 글자다', inlines('a<br>b<br/>c<i>d').map((k) => k.t).join(',') === 'text,br,text,br,text', inlines('a<br>b<br/>c<i>d').map((k) => k.t).join(','));
  const md = parseMd('## 요약\n\n첫 문단\n둘째 줄\n\n- 하나\n- 둘 **굵게**\n\n1. 첫\n2. 둘\n\n| 장 | 제목 |\n|---|---|\n| 1 | 표지 |\n| 2 | 문제 |\n\n```json\n{"a":1}\n```\n\n---\n끝');
  ok('블록: 제목·문단·목록 둘·표·코드·가로줄·문단', md.map((b) => b.t).join(',') === 'heading,para,list,list,table,code,hr,para', md.map((b) => b.t).join(','));
  ok('제목 단계를 든다', md[0].level === 2);
  ok('문단 안 줄바꿈은 한 문단이다', md[1].kids.map((k) => k.text).join('') === '첫 문단\n둘째 줄');
  ok('목록은 순서 유무를 가른다', md[2].ordered === false && md[3].ordered === true && md[2].items.length === 2 && md[3].items.length === 2);
  ok('표는 머리와 행을 가른다', md[4].head.length === 2 && md[4].rows.length === 2 && md[4].rows[1][1][0].text === '문제');
  ok('코드 블록은 언어와 본문을 그대로 든다', md[5].lang === 'json' && md[5].text === '{"a":1}');
  ok('표식이 없으면 파서를 안 거친다', looksLikeMd('그냥 문장입니다.') === false && looksLikeMd('**굵게**') && looksLikeMd('| a | b |'));
  const mk = (tag) => ({ tag, kids: [], attrs: {}, append(...n) { this.kids.push(...n); }, set textContent(v) { this.kids = [{ tag: '#text', text: v }]; }, get textContent() { return this.kids.map((k) => k.text ?? k.textContent).join(''); }, set className(v) { this.attrs.class = v; }, set href(v) { this.attrs.href = v; }, set target(v) { this.attrs.target = v; }, set rel(v) { this.attrs.rel = v; } });
  const doc = { createElement: mk, createTextNode: (t) => ({ tag: '#text', text: t }) };
  const dom = mdToDom(doc, '## 제목\n\n**굵게** 글\n\n| a | b |\n|---|---|\n| 1 | 2 |');
  ok('짓는 쪽: div.md 아래 h4·p·table', dom.attrs.class === 'md' && dom.kids.map((k) => k.tag).join(',') === 'h4,p,table', dom.kids.map((k) => k.tag).join(','));
  ok('굵게는 strong 노드다', dom.kids[1].kids[0].tag === 'strong' && dom.kids[1].kids[0].textContent === '굵게');
  ok('표는 thead/tbody 를 갖는다', dom.kids[2].kids.map((k) => k.tag).join(',') === 'thead,tbody');
  const src = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
  const body = /  rowEl\(r\) \{([\s\S]*?)\n  \}\n/.exec(src)?.[1] ?? '';
  ok('rowEl 이 있다', body !== '');
  // 사람의 말도 마크다운으로 짓는다(사용자 2026-09-05) — 표·목록을 붙여 넣는 부탁이 흔하다. 표식이 없으면 문단 하나.
  ok('사람의 말도 마크다운 길로 짓는다', /r\.kind === 'user'[\s\S]*?el\.append\(this\.proseEl\(bodyText\(r\)\)\)/.test(body) && !/p\.textContent = bodyText\(r\)/.test(body));
  ok('모델·플러그인의 글과 카운슬 판정도 마크다운으로 짓는다', (body.match(/this\.proseEl\(/g) ?? []).length >= 3, String((body.match(/this\.proseEl\(/g) ?? []).length));
  ok('짓는 쪽은 md.js 의 mdToDom 을 쓴다', /mdToDom\(document, text\)/.test(src));
}

// ── 컨텍스트 띠와 모델 고르기 (2026-09-06) ─────────────────────────────────
{
  const st = { model: 'sonnet', window: 131072, used: 30861, estimated: false, messages: 12, compactions: 1, shed: 4000,
    parts: { system: 2404, tools: 5703, talk: 800, calls: 300, results: 900 } };
  const m = contextMeter(st);
  ok('띠는 퍼센트와 토큰을 k 단위로 적는다', m.hidden === false && m.pct === 24 && m.text.startsWith('31k / 131k 토큰 · 24%'), m.text);
  ok('다섯 조각이 요청에 실리는 순서로 선다', m.segments.map((s) => s.kind).join(',') === CONTEXT_PARTS.map(([k]) => k).join(','));
  ok('조각의 폭은 모델 창에 대한 몫이다 — 안 찬 자리는 빈다', Math.round(m.segments[1].pct) === 4 && Math.round(m.segments.reduce((a, s) => a + s.pct, 0)) === 8 && m.keys[1].text === '도구 목록 5.7k', JSON.stringify(m.segments.map((s) => s.pct)));
  ok('창을 모르면 합에 맞춘다 — 가득 찬 띠가 「모른다」의 모양', Math.round(contextMeter({ used: 100, window: 0, parts: { system: 25, tools: 75 } }).segments[1].pct) === 75);
  ok('접은 기록을 적는다', m.note === '접기 1회 · 4k 토큰 덜어냄' && m.title.includes('시스템 · 2.4k'), m.note + ' | ' + m.title);
  ok('추정치는 물결로', contextMeter({ ...st, estimated: true }).text.startsWith('~31k'));
  ok('창을 모르면 퍼센트를 안 짓는다', contextMeter({ used: 500, window: 0 }).pct === null && contextMeter({ used: 500, window: 0 }).text === '500 토큰');
  ok('다섯 조각이 없으면 띠 조각도 없다 — 모름은 0이 아니다', contextMeter({ used: 500, window: 1000 }).segments.length === 0);
  ok('아무것도 모르면 숨는다', contextMeter(null).hidden === true && contextMeter({}).hidden === true);
  ok('대화·호출·결과가 없으면 접기 단추가 잠긴다', contextMeter({ used: 8000, window: 1e5, parts: { system: 2000, tools: 6000 } }).compactDisabled === true && m.compactDisabled === false);

  const pick = modelPicker({ providers: [{ name: '클로드 심', base: 'http://127.0.0.1:58412/v1', models: ['opus', 'sonnet'] }, { name: 'Ollama', base: 'http://localhost:11434/v1', models: ['gpt-oss:20b'] }],
    models: ['ignored'], backend: 'http://127.0.0.1:58412/v1', model: 'sonnet' });
  ok('지금 프로바이더가 선택된다', pick.providers.length === 2 && pick.providers[0].selected === true && pick.providers[1].selected === false);
  ok('모델 목록은 고른 프로바이더의 것', pick.models.map((o) => o.value).join(',') === 'opus,sonnet' && pick.models[1].selected === true);
  ok('제목이 지금 것을 말한다', pick.title === '클로드 심 · sonnet' && pick.note === '');
  const off = modelPicker({ providers: [{ name: 'Ollama', base: 'http://localhost:11434/v1', models: ['gpt-oss:20b'] }], models: ['a', 'b'], backend: 'http://x/v1', model: 'zzz' });
  ok('명단 밖의 백엔드도 지금 것이면 선다', off.providers[0].text.includes('명단 밖') && off.providers[0].selected === true);
  ok('지금 모델이 목록에 없어도 선다', off.models[0].value === 'zzz' && off.models[0].selected === true && off.models.length === 3);
  const none = modelPicker({ providers: [], models: [], error: '아직 어느 컴패니언에도 안 붙었습니다' });
  ok('고를 것이 없으면 사유가 선다', none.empty === true && none.note.includes('안 붙었'));

  const html = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
  ok('띠·접기·프로바이더·모델이 마크업에 있다', ['id="ctx"', 'id="ctx-bar"', 'id="compact"', 'id="provider"', 'id="model"', 'id="provider-menu"', 'id="model-menu"'].filter((id) => !html.includes(id)).length === 0);
  ok('프로바이더·모델은 네이티브 select 가 아니라 직접 구현한 M3 exposed dropdown(필드 + listbox 메뉴)이다', !/<select id="(provider|model)"/.test(html) && html.includes('aria-haspopup="listbox"') && html.includes('role="listbox"') && html.includes('class="dd-label"'));
  ok('다섯 조각의 색이 CSS 에 있다', CONTEXT_PARTS.filter(([k]) => !readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8').includes(`--p-${k}`)).length === 0);
}

{
  const on = confirmAsk('council', 'on'); const off = confirmAsk('council', 'off');
  ok('카운슬 토글은 먼저 묻는다 — 데몬이 다시 뜨고 다른 창·플러그인도 끊긴다고', on && on.danger === true && on.head.includes('켭니다') && off.head.includes('끕니다') && on.body.includes('다시 뜹니다') && on.body.includes('플러그인'));
  ok('덜 위험한 쪽이 그만두기다', on.cancel === '그만둡니다' && on.ok === '다시 띄웁니다');
}
console.log(failed ? `\n${failed} 실패` : '\n전부 통과');

// ── 접기 이벤트 (2026-09-06) ─────────────────────────────────────────────
{
  const t = new Transcript();
  t.append({ type: 'compaction', seq: 9, data: { tokensBefore: 9000, tokensAfter: 1200 } });
  const row = t.rows[t.rows.length - 1];
  ok('compaction 은 접은 줄로 선다 — 모르는 이벤트가 아니다', row?.kind === 'fold' && row.fold.before === 9000 && t.unknownNote === null, JSON.stringify({ kind: row?.kind, un: t.unknownNote }));
  ok('접은 줄의 글은 전후와 덜어낸 양을 k 로', foldText(row) === '컨텍스트를 접었습니다 — 9k → 1.2k 토큰 · 7.8k 덜어냄', foldText(row));
  ok('전후를 모르면 접었다고만', foldText({ kind: 'fold', fold: { before: 0, after: 0 } }) === '컨텍스트를 접었습니다');
  // 덜어냄(2026-09-07) — 접기의 싼 층. 모르는 이벤트가 아니라 접은 줄이고, 진행 말은 세기만 한다.
  t.append({ type: 'result.elided', seq: 10, data: { callId: 'c1', bytes: 8000 } });
  const cut = t.rows[t.rows.length - 1];
  ok('result.elided 는 덜어낸 줄로 선다', cut?.kind === 'fold' && cut.fold.bytes === 8000 && t.unknownNote === null, JSON.stringify({ kind: cut?.kind, un: t.unknownNote }));
  ok('덜어낸 줄의 글은 토큰쯤과 되돌리는 길', foldText(cut) === '도구 결과 하나를 덜어냈습니다 — 2k 토큰쯤 · 다시 읽으면 돌아옵니다', foldText(cut));
  t.append({ type: 'tool.progress', seq: 11, data: { name: 'compact', text: 'freed the window' } });
  ok('tool.progress 는 세기만 한다 — 모르는 것도 그리는 것도 아니다', t.unknownNote === null && /tool\.progress/.test(t.skippedNote ?? ''), `${t.unknownNote} / ${t.skippedNote}`);
}
