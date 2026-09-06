import { HandPort } from '../port/HandPort.js';
import {
  ALL_OPS, FIX_TOOLS, FIX_PREFIX, BOOK_SETTING_KEY, Refusal, refuse, str, num, int, bool, arr, need, grid, hex,
  chartTypeOf, CHART_KO, envelope, clip, isFormula, nowEpoch,
} from './handCore.js';

/**
 * 진짜 Excel 에 닿는 손. **이 파일과 `OfficeWorkbook` 만 Office 를 안다.**
 *
 * 헬퍼가 보낸 조작 하나(op, args)를 Excel.run 한 묶음으로 옮기고, 봉투(handCore.envelope)로 답한다. 규칙은
 * 파워포인트 판의 OfficeHand 와 같다:
 *  - 못 하는 것은 **던진다**(조용한 성공 금지). 사유는 사람이 읽는 한 문장, 가능하면 대안까지.
 *  - 쓰기는 `changed` 에 전후를 적는다 — 카운슬이 「이 턴에 바뀐 것」으로 읽는 유일한 자리다.
 *  - 요구 집합이 모자라면 op 마다 그 이름을 대고 거절한다(ExcelApi 1.7 바닥, 1.8 유효성·피벗, 1.9 찾기·그림,
 *    1.10 메모, 1.11 메모 해결).
 *  - Excel.run 은 겹쳐 돌면 거부하므로 호출을 한 줄로 세운다(#queue). 헬퍼는 45초에 포기한다 — 그보다 오래 줄에
 *    선 호출은 돌리지 않고 거절한다(staleAfter).
 */
export class ExcelHand extends HandPort {
  static staleAfter = 40000;
  static stuckAfter = 50000;

  constructor({ run, supports, document = '', label = '' } = {}) {
    super();
    this.runner = run ?? ((fn) => Excel.run(fn));
    this.supports = supports ?? ((name, version) => {
      try {
        const req = typeof Office !== 'undefined' && Office.context && Office.context.requirements;
        return Boolean(req && typeof req.isSetSupported === 'function' && req.isSetSupported(name, version) === true);
      } catch { return false; }
    });
    this.document = document;
    this.labelText = label;
    this.epoch = nowEpoch();
    this.count = 0;
    this.snapshots = new Map();
    this.#queue = Promise.resolve();
    this.#inside = false;
  }
  #queue; #inside;
  get label() { return this.labelText || 'Excel (Office.js)'; }
  ops() { return [...ALL_OPS]; }

  /** 호출을 한 줄로 세운다. 안에서 다시 부르는 것(applyFix → drop_suggestion)은 줄을 건너뛴다. */
  async run(op, args = {}) {
    if (this.#inside) return this.#dispatch(op, args);
    const joined = Date.now();
    const turn = this.#queue.then(async () => {
      if (Date.now() - joined > ExcelHand.staleAfter) {
        throw new Error(`${op}: 앞 호출을 ${Math.round((Date.now() - joined) / 1000)}초 기다리다 헬퍼가 포기했을 시각을 넘겼습니다 — 다시 부르세요`);
      }
      this.#inside = true;
      let timer;
      try {
        return await Promise.race([
          this.#dispatch(op, args),
          new Promise((_, rej) => { timer = setTimeout(() => rej(new Error(`${op}: Excel 이 ${ExcelHand.stuckAfter / 1000}초 안에 답하지 않았습니다`)), ExcelHand.stuckAfter); }),
        ]);
      } finally { clearTimeout(timer); this.#inside = false; }
    });
    this.#queue = turn.catch(() => {});
    return turn;
  }

  async #dispatch(op, args) {
    const before = this.count;
    const out = await this.#route(op, args ?? {});
    if (this.count !== before && out && typeof out === 'object' && out.result && !out.result.now) {
      // 바꾼 뒤의 사실 한 조각 — 어느 시트를 손댔는가. 모델이 다음 호출의 sheet 를 되묻지 않게.
      try { out.result.now = await this.#now(args); } catch { /* 계측이 본 작업을 막지 않는다 */ }
    }
    return out;
  }
  async #now(args) {
    return this.runner(async (context) => {
      const ws = this.#sheet(context, args);
      ws.load('name'); await context.sync();
      return { sheet: ws.name };
    });
  }
  #mutated() { this.count += 1; }
  #envelope(result, changed = []) { return envelope(this, result, changed); }
  #need(name, version, what) {
    if (!this.supports(name, version)) refuse(`${what} 은 ${name} ${version} 이 필요한데 이 호스트에는 없습니다`);
  }

  // ── 자리 고르기 ──────────────────────────────────────────────────────────────
  #sheet(context, args, key = 'sheet') {
    const s = str(args, key) ?? str(args, 'worksheet');
    if (s == null || s === '') return context.workbook.worksheets.getActiveWorksheet();
    if (/^\d+$/.test(s)) {
      const items = context.workbook.worksheets; items.load('items/name');
      // 번호는 동기적으로 못 푼다 — getItemAt 이 있다.
      return context.workbook.worksheets.getItemAt(Number(s) - 1);
    }
    return context.workbook.worksheets.getItem(s);
  }
  #range(context, args, { must = true } = {}) {
    const ws = this.#sheet(context, args);
    const address = str(args, 'address') ?? str(args, 'range');
    if (!address) {
      if (must) refuse('address 가 없습니다 — "B2:E9" 같은 A1 주소');
      return { ws, range: ws.getUsedRangeOrNullObject(true), used: true };
    }
    if (address.includes('!')) refuse(`address 에 시트 이름을 넣지 마세요(${address}) — sheet 인자로 주세요`);
    return { ws, range: ws.getRange(address), used: false };
  }
  static #sheetOf(addressWithSheet) {
    const at = String(addressWithSheet ?? '').indexOf('!');
    return at < 0 ? null : { sheet: addressWithSheet.slice(0, at).replace(/^'|'$/g, ''), address: addressWithSheet.slice(at + 1) };
  }
  static #bare(address) { const s = String(address ?? ''); const at = s.indexOf('!'); return at < 0 ? s : s.slice(at + 1); }

  async #route(op, a) {
    switch (op) {
      // ── 읽기 ──
      case 'list_sheets': return this.#listSheets();
      case 'describe_sheet': return this.#describeSheet(a);
      case 'read_range': return this.#readRange(a);
      case 'find': return this.#find(a);
      case 'read_table': return this.#readTable(a);
      case 'read_chart': return this.#readChart(a);
      case 'render_range': return this.#renderRange(a);
      case 'render_chart': return this.#renderChart(a);
      case 'read_comments': return this.#readComments(a);
      case 'read_names': return this.#readNames(a);
      case 'read_validation': return this.#readValidation(a);
      case 'read_conditional_formats': return this.#readConditionalFormats(a);
      case 'describe_style': return this.#describeStyle();
      case 'snapshot_range': return this.#snapshot(a);
      case 'read_tags': return this.#readTags();
      case 'read_suggestions': return this.#readSuggestions(a);
      case 'advise': case 'clear_advice':
        return this.#envelope({ pinned: op === 'advise' ? (a.items?.length ?? 0) : 0 });
      // ── 쓰기 ──
      case 'write_range': return this.#writeRange(a);
      case 'set_cell_style': return this.#setCellStyle(a);
      case 'edit_table': return this.#editTable(a);
      case 'set_page_setup': return this.#setPageSetup(a);
      case 'protect_workbook': return this.#protectWorkbook(a);
      case 'trace_cell': return this.#traceCell(a);
      case 'insert_sheets_from_file': return this.#insertSheetsFromFile(a);
      case 'import_csv': return this.#importCSV(a);
      case 'set_rows_columns': return this.#setRowsColumns(a);
      case 'set_tab_color': return this.#setTabColor(a);
      case 'set_sheet_view': return this.#setSheetView(a);
      case 'set_workbook_properties': return this.#setWorkbookProperties(a);
      case 'replace_all': return this.#replaceAll(a);
      case 'copy_range': return this.#copyRange(a);
      case 'fill_range': return this.#fillRange(a);
      case 'remove_duplicates': return this.#removeDuplicates(a);
      case 'set_number_format': return this.#setNumberFormat(a);
      case 'format_range': return this.#formatRange(a);
      case 'clear_range': return this.#clearRange(a);
      case 'merge_cells': return this.#merge(a, true);
      case 'unmerge_cells': return this.#merge(a, false);
      case 'insert_cells': return this.#insertDelete(a, true);
      case 'delete_cells': return this.#insertDelete(a, false);
      case 'autofit': return this.#autofit(a);
      case 'set_hyperlink': return this.#hyperlink(a);
      case 'add_sheet': return this.#addSheet(a);
      case 'delete_sheet': return this.#deleteSheet(a);
      case 'rename_sheet': return this.#renameSheet(a);
      case 'move_sheet': return this.#moveSheet(a);
      case 'copy_sheet': return this.#copySheet(a);
      case 'set_sheet_visibility': return this.#visibility(a);
      case 'activate_sheet': return this.#activate(a);
      case 'freeze_panes': return this.#freeze(a);
      case 'protect_sheet': return this.#protect(a, true);
      case 'unprotect_sheet': return this.#protect(a, false);
      case 'add_table': return this.#addTable(a);
      case 'set_table_cells': return this.#setTableCells(a);
      case 'add_table_rows': return this.#addTableRows(a);
      case 'remove_table': return this.#removeTable(a);
      case 'sort_range': return this.#sort(a);
      case 'filter_table': return this.#filter(a);
      case 'add_chart': return this.#addChart(a);
      case 'format_chart': return this.#formatChart(a);
      case 'delete_chart': return this.#deleteChart(a);
      case 'add_conditional_format': return this.#addConditionalFormat(a);
      case 'clear_conditional_formats': return this.#clearConditionalFormats(a);
      case 'set_validation': return this.#setValidation(a);
      case 'set_name': return this.#setName(a);
      case 'delete_name': return this.#deleteName(a);
      case 'add_comment': return this.#addComment(a);
      case 'resolve_comment': return this.#resolveComment(a);
      case 'add_image': return this.#addImage(a);
      case 'add_pivot': return this.#addPivot(a);
      case 'refresh_pivot': return this.#refreshPivot(a);
      case 'restore_range': return this.#restore(a);
      case 'set_tag': return this.#setTag(a);
      case 'suggest': return this.#suggest(a);
      case 'drop_suggestion': return this.#dropSuggestion(a);
      default: throw new Error(`이 손은 ${op} 을 모릅니다 — 아는 것: ${ALL_OPS.join(', ')}`);
    }
  }

  // ── 읽기 ────────────────────────────────────────────────────────────────────
  async #listSheets() {
    return this.runner(async (context) => {
      const sheets = context.workbook.worksheets;
      sheets.load('items/name,items/position,items/visibility');
      const active = context.workbook.worksheets.getActiveWorksheet(); active.load('name');
      // 통장 이름(1.7) — 헬퍼가 2021 의 메모를 COM 노트로 대신할 때 어느 통장인지 이것으로 잡는다(xl_notes.go).
      const book = this.supports('ExcelApi', '1.7') ? context.workbook : null; if (book) book.load('name');
      await context.sync();
      const probes = sheets.items.map((ws) => {
        const used = ws.getUsedRangeOrNullObject(true); used.load('address,rowCount,columnCount');
        const tables = ws.tables; tables.load('count');
        const charts = ws.charts; charts.load('count');
        const pivots = this.supports('ExcelApi', '1.3') ? ws.pivotTables : null; if (pivots) pivots.load('count');
        return { ws, used, tables, charts, pivots };
      });
      await context.sync();
      const rows = probes.map(({ ws, used, tables, charts, pivots }) => ({
        index: ws.position + 1, name: ws.name, visibility: ws.visibility, active: ws.name === active.name,
        used_range: used.isNullObject ? null : ExcelHand.#bare(used.address),
        rows: used.isNullObject ? 0 : used.rowCount, columns: used.isNullObject ? 0 : used.columnCount,
        tables: tables.count, charts: charts.count, pivots: pivots ? pivots.count : null,
      }));
      return this.#envelope({ sheets: rows, count: rows.length, active: active.name, workbook: book ? book.name : null });
    });
  }

  async #describeSheet(a) {
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a);
      ws.load('name,position,visibility');
      const used = ws.getUsedRangeOrNullObject(true); used.load('address,rowCount,columnCount');
      const tables = ws.tables; tables.load('items/name,items/id,items/showHeaders,items/showTotals,items/style');
      const charts = ws.charts; charts.load('items/name,items/chartType,items/left,items/top,items/width,items/height');
      const names = ws.names; names.load('items/name,items/formula,items/type');
      const has17 = this.supports('ExcelApi', '1.7');
      const frozen = has17 ? ws.freezePanes.getLocationOrNullObject() : null; if (frozen) frozen.load('address');
      const pivots = this.supports('ExcelApi', '1.3') ? ws.pivotTables : null; if (pivots) pivots.load('items/name');
      await context.sync();
      const tableRows = tables.items.map((t) => { const r = t.getRange(); r.load('address,rowCount'); const h = t.getHeaderRowRange(); h.load('values'); return { t, r, h }; });
      const chartRows = charts.items.map((c) => { c.title.load('text,visible'); const s = c.series; s.load('items/name'); return { c, s }; });
      let merged = null;
      if (has17 && !used.isNullObject) { merged = used.getMergedAreasOrNullObject(); merged.load('address,areaCount'); }
      let header = null;
      if (!used.isNullObject) {
        header = used.getRow(0); header.load('values'); header.format.font.load('bold,name,size,color'); header.format.fill.load('color');
      }
      await context.sync();
      const result = {
        sheet: ws.name, index: ws.position + 1, visibility: ws.visibility,
        used_range: used.isNullObject ? null : ExcelHand.#bare(used.address),
        rows: used.isNullObject ? 0 : used.rowCount, columns: used.isNullObject ? 0 : used.columnCount,
        frozen: frozen && !frozen.isNullObject ? ExcelHand.#bare(frozen.address) : null,
        merged: merged && !merged.isNullObject ? merged.address.split(',').map((x) => ExcelHand.#bare(x.trim())) : [],
        tables: tableRows.map(({ t, r, h }) => ({ name: t.name, address: ExcelHand.#bare(r.address), rows: r.rowCount - (t.showHeaders ? 1 : 0) - (t.showTotals ? 1 : 0), headers: h.values[0] ?? [], style: t.style })),
        charts: chartRows.map(({ c, s }) => ({ name: c.name, type: c.chartType, title: c.title.visible ? c.title.text : '', series: s.items.map((x) => x.name), left: c.left, top: c.top, width: c.width, height: c.height })),
        pivots: pivots ? pivots.items.map((p) => p.name) : [],
        names: names.items.map((n) => ({ name: n.name, refers_to: n.formula, type: n.type })),
        header: header ? { values: header.values[0], bold: header.format.font.bold, font: header.format.font.name, size: header.format.font.size, color: header.format.font.color, fill: header.format.fill.color } : null,
      };
      return this.#envelope(result);
    });
  }

  async #readRange(a) {
    const maxRows = int(a, 'max_rows') ?? 200; const maxCols = int(a, 'max_cols') ?? 30;
    const wantFormulas = bool(a, 'formulas') ?? true;
    return this.runner(async (context) => {
      const { ws, range, used } = this.#range(context, a, { must: false });
      ws.load('name'); range.load('address,rowCount,columnCount,isNullObject');
      await context.sync();
      if (used && range.isNullObject) return this.#envelope({ sheet: ws.name, address: null, rows: 0, columns: 0, values: [], note: '이 시트는 비어 있습니다' });
      const rows = Math.min(range.rowCount, maxRows); const cols = Math.min(range.columnCount, maxCols);
      const part = range.getCell(0, 0).getResizedRange(rows - 1, cols - 1);
      part.load('address,values,numberFormat' + (wantFormulas ? ',formulas' : ''));
      await context.sync();
      const formulas = {};
      if (wantFormulas) part.formulas.forEach((row, r) => row.forEach((f, c) => { if (isFormula(f)) formulas[cellAt(part.address, r, c)] = f; }));
      const formats = {};
      part.numberFormat.forEach((row, r) => row.forEach((f, c) => { if (f && f !== 'General') formats[cellAt(part.address, r, c)] = f; }));
      const truncated = rows < range.rowCount || cols < range.columnCount;
      return this.#envelope({
        sheet: ws.name, address: ExcelHand.#bare(part.address), rows, columns: cols,
        total_rows: range.rowCount, total_columns: range.columnCount, truncated,
        values: part.values, formulas, number_formats: formats,
        ...(truncated ? { note: `범위가 ${range.rowCount}×${range.columnCount} 인데 ${rows}×${cols} 만 읽었습니다 — max_rows/max_cols 를 올리거나 필요한 부분만 address 로 부르세요` } : {}),
      });
    });
  }

  async #find(a) {
    this.#need('ExcelApi', '1.9', 'find');
    const text = String(need(a, 'text')); const limit = int(a, 'limit') ?? 50;
    const matchCase = bool(a, 'match_case') ?? false; const whole = bool(a, 'whole_cell') ?? false; const inFormulas = bool(a, 'in_formulas') ?? false;
    return this.runner(async (context) => {
      const sheets = [];
      if (str(a, 'sheet')) { const ws = this.#sheet(context, a); ws.load('name'); sheets.push(ws); } else {
        const all = context.workbook.worksheets; all.load('items/name'); await context.sync(); sheets.push(...all.items);
      }
      const hits = [];
      if (inFormulas) {
        const probes = sheets.map((ws) => { const u = ws.getUsedRangeOrNullObject(true); u.load('address,formulas,isNullObject'); return { ws, u }; });
        await context.sync();
        for (const { ws, u } of probes) {
          if (u.isNullObject) continue;
          u.formulas.forEach((row, r) => row.forEach((f, c) => {
            if (typeof f === 'string' && (matchCase ? f.includes(text) : f.toLowerCase().includes(text.toLowerCase()))) hits.push({ sheet: ws.name, address: cellAt(u.address, r, c), formula: f });
          }));
        }
      } else {
        const probes = sheets.map((ws) => { const areas = ws.findAllOrNullObject(text, { completeMatch: whole, matchCase }); areas.load('address,isNullObject'); return { ws, areas }; });
        await context.sync();
        const cells = [];
        for (const { ws, areas } of probes) {
          if (areas.isNullObject) continue;
          for (const one of areas.address.split(',')) {
            const rng = ws.getRange(ExcelHand.#bare(one.trim())); rng.load('address,values'); cells.push({ ws, rng });
          }
        }
        await context.sync();
        for (const { ws, rng } of cells) rng.values.forEach((row, r) => row.forEach((v, c) => hits.push({ sheet: ws.name, address: cellAt(rng.address, r, c), value: v })));
      }
      return this.#envelope({ hits: hits.slice(0, limit), matched: hits.length, searched_sheets: sheets.map((s) => s.name), ...(hits.length > limit ? { note: `앞 ${limit}개만 실었습니다` } : {}) });
    });
  }

  async #readTable(a) {
    const name = String(need(a, 'table')); const maxRows = int(a, 'max_rows') ?? 200;
    return this.runner(async (context) => {
      const t = context.workbook.tables.getItemOrNullObject(name); t.load('name,isNullObject,showHeaders,showTotals,style');
      await context.sync();
      if (t.isNullObject) refuse(`'${name}' 이라는 표가 없습니다 — list_sheets/describe_sheet 가 이름을 줍니다`);
      const ws = t.worksheet; ws.load('name');
      const r = t.getRange(); r.load('address,rowCount');
      const h = t.getHeaderRowRange(); h.load('values');
      // getDataBodyRangeOrNullObject 는 없다(실물 2026-09-06: 「is not a function」). 표는 몸통 행을 늘 하나는 갖는다.
      const body = t.getDataBodyRange(); body.load('rowCount,columnCount');
      await context.sync();
      let rows = []; let truncated = false;
      {
        const n = Math.min(body.rowCount, maxRows); truncated = n < body.rowCount;
        const part = body.getCell(0, 0).getResizedRange(n - 1, body.columnCount - 1); part.load('values');
        await context.sync(); rows = part.values;
      }
      return this.#envelope({ table: t.name, sheet: ws.name, address: ExcelHand.#bare(r.address), headers: h.values[0] ?? [], rows, row_count: body.isNullObject ? 0 : body.rowCount, truncated, style: t.style });
    });
  }

  async #readChart(a) {
    const name = String(need(a, 'chart'));
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      const c = ws.charts.getItemOrNullObject(name); c.load('name,chartType,left,top,width,height,isNullObject');
      await context.sync();
      if (c.isNullObject) refuse(`'${name}' 이라는 차트가 이 시트에 없습니다 — describe_sheet 가 이름을 줍니다`);
      c.title.load('text,visible'); c.legend.load('position,visible');
      const series = c.series; series.load('items/name');
      const has17 = this.supports('ExcelApi', '1.7');
      const xa = c.axes.categoryAxis; const ya = c.axes.valueAxis;
      if (has17) { xa.title.load('text,visible'); ya.title.load('text,visible'); }
      await context.sync();
      return this.#envelope({
        chart: c.name, sheet: ws.name, type: c.chartType, type_ko: CHART_KO.get(c.chartType) ?? c.chartType,
        title: c.title.visible ? c.title.text : '', legend: c.legend.visible ? c.legend.position : 'none',
        x_title: has17 && xa.title.visible ? xa.title.text : '', y_title: has17 && ya.title.visible ? ya.title.text : '',
        series: series.items.map((s) => s.name), left: c.left, top: c.top, width: c.width, height: c.height,
      });
    });
  }

  async #renderRange(a) {
    this.#need('ExcelApi', '1.7', 'render_range');
    return this.runner(async (context) => {
      const { ws, range, used } = this.#range(context, a, { must: false });
      ws.load('name'); range.load('address,isNullObject');
      await context.sync();
      if (used && range.isNullObject) refuse('이 시트는 비어 있어 그릴 것이 없습니다');
      const img = range.getImage();
      await context.sync();
      const b64 = img.value ?? '';
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), image_base64: b64, image_mime: 'image/png', image_bytes: Math.floor(b64.length * 3 / 4) });
    });
  }

  async #renderChart(a) {
    const name = String(need(a, 'chart')); const w = int(a, 'max_width') ?? 800;
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      const c = ws.charts.getItemOrNullObject(name); c.load('name,isNullObject,width,height');
      await context.sync();
      if (c.isNullObject) refuse(`'${name}' 이라는 차트가 이 시트에 없습니다`);
      const h = Math.max(1, Math.round(w * (c.height / Math.max(1, c.width))));
      const img = c.getImage(w, h, 'Fit');
      await context.sync();
      const b64 = img.value ?? '';
      return this.#envelope({ chart: c.name, sheet: ws.name, image_base64: b64, image_mime: 'image/png', image_bytes: Math.floor(b64.length * 3 / 4), width: w, height: h });
    });
  }

  async #readComments(a) {
    this.#need('ExcelApi', '1.10', 'read_comments');
    const only = str(a, 'sheet');
    return this.runner(async (context) => {
      const comments = only ? this.#sheet(context, a).comments : context.workbook.comments;
      comments.load('items/id,items/authorName,items/content,items/creationDate' + (this.supports('ExcelApi', '1.11') ? ',items/resolved' : ''));
      await context.sync();
      const probes = comments.items.map((c) => { const loc = c.getLocation(); loc.load('address'); const rep = c.replies; rep.load('items/authorName,items/content'); return { c, loc, rep }; });
      await context.sync();
      const rows = probes.map(({ c, loc, rep }) => {
        const at = ExcelHand.#sheetOf(loc.address);
        return { id: c.id, sheet: at?.sheet ?? null, address: at?.address ?? loc.address, author: c.authorName, text: c.content, resolved: c.resolved ?? null, replies: rep.items.map((r) => ({ author: r.authorName, text: r.content })) };
      });
      return this.#envelope({ comments: rows, count: rows.length });
    });
  }

  async #readNames(a) {
    return this.runner(async (context) => {
      const wb = context.workbook.names; wb.load('items/name,items/formula,items/type,items/value,items/visible');
      let sheetNames = null; let wsName = null;
      if (str(a, 'sheet')) { const ws = this.#sheet(context, a); ws.load('name'); sheetNames = ws.names; sheetNames.load('items/name,items/formula,items/type,items/value'); await context.sync(); wsName = ws.name; }
      await context.sync();
      const row = (n, scope) => ({ name: n.name, refers_to: n.formula, type: n.type, value: n.value, scope });
      const items = [...wb.items.map((n) => row(n, 'workbook')), ...(sheetNames ? sheetNames.items.map((n) => row(n, wsName)) : [])];
      return this.#envelope({ names: items, count: items.length });
    });
  }

  async #readValidation(a) {
    this.#need('ExcelApi', '1.8', 'read_validation');
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      const dv = range.dataValidation; dv.load('type,rule,prompt,errorAlert,valid');
      await context.sync();
      // rule 은 종류별 칸이 다 실리고 하나만 채워진 객체다 — 채워진 것만 남긴다(실물 2026-09-06: OData 덩어리가 통째로 갔다).
      const strip = (o) => (o && typeof o === 'object' && !Array.isArray(o) ? Object.fromEntries(Object.entries(o).filter(([k, v]) => v != null && !k.startsWith('@')).map(([k, v]) => [k, strip(v)])) : o);
      const rule = dv.rule && typeof dv.rule === 'object' ? strip(dv.rule) : null;
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), type: dv.type, rule, prompt: dv.prompt ?? null, error: dv.errorAlert ?? null, valid: dv.valid });
    });
  }

  async #readConditionalFormats(a) {
    this.#need('ExcelApi', '1.6', 'read_conditional_formats');
    return this.runner(async (context) => {
      const { ws, range, used } = this.#range(context, a, { must: false }); ws.load('name'); range.load('address,isNullObject');
      await context.sync();
      if (used && range.isNullObject) return this.#envelope({ sheet: ws.name, formats: [], count: 0 });
      const cfs = range.conditionalFormats; cfs.load('items/type,items/priority,items/stopIfTrue,items/id');
      await context.sync();
      const probes = cfs.items.map((cf) => { const r = cf.getRange(); r.load('address'); return { cf, r }; });
      await context.sync();
      const rows = probes.map(({ cf, r }) => ({ id: cf.id, type: cf.type, priority: cf.priority, address: ExcelHand.#bare(r.address) }));
      return this.#envelope({ sheet: ws.name, formats: rows, count: rows.length });
    });
  }

  async #describeStyle() {
    return this.runner(async (context) => {
      const sheets = context.workbook.worksheets; sheets.load('items/name');
      await context.sync();
      const probes = sheets.items.map((ws) => { const u = ws.getUsedRangeOrNullObject(true); u.load('isNullObject,rowCount'); return { ws, u }; });
      await context.sync();
      const looks = [];
      for (const { ws, u } of probes) {
        if (u.isNullObject) continue;
        const head = u.getRow(0); head.format.font.load('name,size,bold,color'); head.format.fill.load('color');
        const body = u.rowCount > 1 ? u.getRow(1) : null; if (body) { body.format.font.load('name,size'); body.load('numberFormat'); }
        looks.push({ ws, head, body });
      }
      await context.sync();
      const rows = looks.map(({ ws, head, body }) => ({ sheet: ws.name, header: { font: head.format.font.name, size: head.format.font.size, bold: head.format.font.bold, color: head.format.font.color, fill: head.format.fill.color }, body: body ? { font: body.format.font.name, size: body.format.font.size, number_formats: [...new Set(body.numberFormat[0].filter((f) => f && f !== 'General'))] } : null }));
      const mode = (xs) => { const m = new Map(); for (const x of xs) m.set(x, (m.get(x) ?? 0) + 1); return [...m.entries()].sort((p, q) => q[1] - p[1])[0]?.[0] ?? null; };
      const summary = rows.length ? { header_fill: mode(rows.map((r) => r.header.fill)), header_bold: mode(rows.map((r) => r.header.bold)), font: mode(rows.map((r) => r.header.font)), size: mode(rows.map((r) => r.header.size)) } : null;
      return this.#envelope({ sheets: rows, seen: rows.length, summary, read: true, note: rows.length ? '새 시트는 이 버릇을 따르면 어울립니다' : '이 통합 문서에는 따라갈 서식이 없습니다 — 기본 서식으로 짓습니다' });
    });
  }

  async #snapshot(a) {
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address,values,formulas,numberFormat,rowCount,columnCount');
      await context.sync();
      const id = `snap-${this.snapshots.size + 1}-${Math.random().toString(36).slice(2, 6)}`;
      this.snapshots.set(id, { sheet: ws.name, address: ExcelHand.#bare(range.address), formulas: range.formulas, numberFormat: range.numberFormat });
      return this.#envelope({ snapshot: id, sheet: ws.name, address: ExcelHand.#bare(range.address), cells: range.rowCount * range.columnCount });
    });
  }

  async #readTags() {
    this.#need('ExcelApi', '1.4', 'read_tags');
    return this.runner(async (context) => {
      const items = context.workbook.settings; items.load('items/key,items/value');
      await context.sync();
      const tags = items.items.filter((s) => !s.key.startsWith(FIX_PREFIX) && s.key !== BOOK_SETTING_KEY).map((s) => ({ key: s.key, value: s.value }));
      return this.#envelope({ tags, count: tags.length });
    });
  }

  async #readSuggestions(a) {
    this.#need('ExcelApi', '1.4', 'read_suggestions');
    const only = str(a, 'sheet');
    return this.runner(async (context) => {
      const items = context.workbook.settings; items.load('items/key,items/value');
      await context.sync();
      const rows = [];
      for (const s of items.items) {
        if (!s.key.startsWith(FIX_PREFIX)) continue;
        const row = decodeSuggestion(s.key, s.value);
        if (only && row.sheet && row.sheet !== only) continue;
        rows.push(row);
      }
      return this.#envelope({ scope: only ?? null, count: rows.length, suggestions: rows });
    });
  }

  // ── 쓰기 ────────────────────────────────────────────────────────────────────
  async #writeRange(a) {
    const values = grid(a, 'values'); const formulas = grid(a, 'formulas');
    if (!values && !formulas) refuse('values 나 formulas 가 있어야 합니다 — 2차원 배열');
    if (values && formulas && (values.rows !== formulas.rows || values.cols !== formulas.cols)) refuse(`values(${values.rows}×${values.cols}) 와 formulas(${formulas.rows}×${formulas.cols}) 의 모양이 다릅니다`);
    const shape = values ?? formulas;
    const merged = [];
    for (let r = 0; r < shape.rows; r += 1) {
      const row = [];
      for (let c = 0; c < shape.cols; c += 1) {
        const f = formulas?.cells[r][c]; const v = values?.cells[r][c];
        row.push(f != null && f !== '' ? f : (v == null ? '' : v));
      }
      merged.push(row);
    }
    const nf = str(a, 'number_format');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      const address = String(need(a, 'address'));
      let range = ws.getRange(address); range.load('address,rowCount,columnCount');
      await context.sync();
      if (range.rowCount === 1 && range.columnCount === 1 && (shape.rows > 1 || shape.cols > 1)) {
        range = range.getResizedRange(shape.rows - 1, shape.cols - 1); range.load('address,rowCount,columnCount'); await context.sync();
      }
      if (range.rowCount !== shape.rows || range.columnCount !== shape.cols) refuse(`배열은 ${shape.rows}×${shape.cols} 인데 ${ExcelHand.#bare(range.address)} 는 ${range.rowCount}×${range.columnCount} 입니다 — 왼쪽 위 셀 하나만 주면 배열 크기로 잡습니다`);
      range.load('values');
      await context.sync();
      const overwrote = range.values.flat().filter((v) => v !== '' && v != null).length;
      range.formulas = merged;
      if (nf) range.numberFormat = merged.map((row) => row.map(() => nf));
      await context.sync();
      this.#mutated();
      const at = ExcelHand.#bare(range.address);
      const formulaCount = merged.flat().filter(isFormula).length;
      return this.#envelope(
        { sheet: ws.name, address: at, rows: shape.rows, columns: shape.cols, overwrote, formulas: formulaCount, number_format: nf ?? null },
        [`${ws.name}!${at} 에 ${shape.rows}×${shape.cols} 을 썼습니다` + (formulaCount ? ` (수식 ${formulaCount}개)` : '') + (overwrote ? ` — ⚠ 값이 있던 셀 ${overwrote}개를 덮어썼습니다` : '') + (nf ? ` · 표시 형식 ${nf}` : '')],
      );
    });
  }

  async #setNumberFormat(a) {
    const fmt = String(need(a, 'format', 'format'));
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address,rowCount,columnCount');
      await context.sync();
      range.numberFormat = Array.from({ length: range.rowCount }, () => Array.from({ length: range.columnCount }, () => fmt));
      await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), format: fmt }, [`${ws.name}!${ExcelHand.#bare(range.address)} 표시 형식 → ${fmt}`]);
    });
  }

  async #formatRange(a) {
    const font = str(a, 'font'); const size = num(a, 'size'); const b = bool(a, 'bold'); const i = bool(a, 'italic'); const u = bool(a, 'underline');
    const color = hex(a, 'color'); const fill = hex(a, 'fill', true); const align = str(a, 'align'); const valign = str(a, 'valign');
    const wrap = bool(a, 'wrap'); const indent = int(a, 'indent'); const borders = hex(a, 'borders', true); const borderStyle = str(a, 'border_style') ?? 'Continuous';
    const cw = num(a, 'column_width'); const rh = num(a, 'row_height');
    const said = [];
    if (font) said.push(`글꼴 ${font}`); if (size != null) said.push(`크기 ${size}`); if (b != null) said.push(b ? '굵게' : '굵게 해제'); if (i != null) said.push(i ? '기울임' : '기울임 해제');
    if (u != null) said.push(u ? '밑줄' : '밑줄 해제'); if (color) said.push(`글자색 ${color}`); if (fill) said.push(fill === 'none' ? '채우기 없음' : `채우기 ${fill}`);
    if (align) said.push(`정렬 ${align}`); if (valign) said.push(`세로 ${valign}`); if (wrap != null) said.push(wrap ? '줄바꿈' : '줄바꿈 해제'); if (indent != null) said.push(`들여쓰기 ${indent}`);
    if (borders) said.push(borders === 'none' ? '테두리 없음' : `테두리 ${borders}${borderStyle !== 'Continuous' ? ` ${borderStyle}` : ''}`);
    if (cw != null) said.push(`열 너비 ${cw}`); if (rh != null) said.push(`행 높이 ${rh}`);
    if (said.length === 0) refuse('바꿀 것이 하나도 안 왔습니다 — font, size, bold, color, fill, align, borders, column_width … 중 하나는 주세요');
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      const f = range.format;
      if (font) f.font.name = font; if (size != null) f.font.size = size; if (b != null) f.font.bold = b; if (i != null) f.font.italic = i;
      if (u != null) f.font.underline = u ? 'Single' : 'None'; if (color) f.font.color = color;
      if (fill) { if (fill === 'none') f.fill.clear(); else f.fill.color = fill; }
      if (align) f.horizontalAlignment = align; if (valign) f.verticalAlignment = valign; if (wrap != null) f.wrapText = wrap; if (indent != null) f.indentLevel = indent;
      if (borders) {
        for (const edge of ['EdgeTop', 'EdgeBottom', 'EdgeLeft', 'EdgeRight', 'InsideHorizontal', 'InsideVertical']) {
          const item = f.borders.getItem(edge);
          if (borders === 'none') item.style = 'None'; else { item.style = borderStyle; item.color = borders; }
        }
      }
      if (cw != null) f.columnWidth = cw; if (rh != null) f.rowHeight = rh;
      await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, changed: said.length }, [`${ws.name}!${at}: ${said.join(', ')}`]);
    });
  }

  async #traceCell(a) {
    const what = (str(a, 'what') ?? 'precedents').toLowerCase();
    if (what !== 'precedents' && what !== 'dependents') refuse(`what 는 precedents 나 dependents — '${what}'`);
    this.#need('ExcelApi', what === 'precedents' ? '1.12' : '1.13', `trace_cell{${what}}`);
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address,rowCount,columnCount,formulas'); await context.sync();
      if (range.rowCount * range.columnCount !== 1) refuse('셀 하나를 주세요 — address 를 셀 하나로');
      const areas = what === 'precedents' ? range.getDirectPrecedents() : range.getDirectDependents();
      areas.load('addresses'); await context.sync();
      const list = (areas.addresses ?? []).map((x) => String(x));
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), what, formula: range.formulas[0][0], count: list.length, cells: list });
    });
  }
  async #insertSheetsFromFile(a) {
    this.#need('ExcelApi', '1.13', 'insert_sheets_from_file');
    const b64 = str(a, 'file_base64'); if (!b64) refuse('파일 바이트가 안 왔습니다 — path 를 주면 헬퍼가 읽어 실어 줍니다');
    const name = str(a, 'file_name') ?? ''; const after = str(a, 'after');
    return this.runner(async (context) => {
      const opts = after ? { positionType: 'After', relativeTo: after } : { positionType: 'End' };
      // 답은 넣은 시트의 id 목록이다 — 이름은 그 id 로 다시 묻는다. 컬렉션을 앞뒤로 두 번 읽어 빼는 길은 같은 프록시가
      // 같은 스냅숏을 돌려줘 「0개」라고 답했다(실물 2026-09-06).
      const ids = context.workbook.insertWorksheetsFromBase64(b64, opts); await context.sync(); this.#mutated();
      const sheets = (Array.isArray(ids.value) ? ids.value : []).map((id) => { const w = context.workbook.worksheets.getItem(id); w.load('name'); return w; }); await context.sync();
      const added = sheets.map((w) => w.name);
      return this.#envelope({ file: name, sheets: added, count: added.length }, [`「${name}」 의 시트 ${added.length}개를 넣었습니다${added.length ? ` — ${added.join(', ')}` : ''}`]);
    });
  }
  async #importCSV(a) {
    const rows = arr(a, 'csv_rows'); if (!rows || !rows.length) refuse('CSV 줄이 안 왔습니다 — path 를 주면 헬퍼가 읽어 실어 줍니다');
    const name = str(a, 'file_name') ?? 'csv'; const address = str(a, 'address') ?? 'A1';
    const width = Math.max(...rows.map((r) => r.length));
    const values = rows.map((r) => Array.from({ length: width }, (_, i) => { const v = r[i] ?? ''; const t = String(v).trim(); return t !== '' && /^-?\d+(\.\d+)?$/.test(t) ? Number(t) : v; }));
    return this.runner(async (context) => {
      let ws; let made = false;
      if (str(a, 'sheet')) { ws = this.#sheet(context, a); } else {
        const base = name.replace(/\.csv$/i, '').slice(0, 28) || 'csv'; const all = context.workbook.worksheets; all.load('items/name'); await context.sync();
        let title = base; let k = 2; while (all.items.some((s) => s.name === title)) { title = `${base} ${k}`; k += 1; }
        ws = context.workbook.worksheets.add(title); made = true;
      }
      ws.load('name'); const target = ws.getRange(address).getCell(0, 0).getResizedRange(values.length - 1, width - 1); target.load('address'); await context.sync();
      target.values = values; await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(target.address), rows: values.length, columns: width, new_sheet: made }, [`「${name}」 ${values.length}×${width} → ${ws.name}!${ExcelHand.#bare(target.address)}${made ? ' (새 시트)' : ''}`]);
    });
  }
  async #setCellStyle(a) {
    this.#need('ExcelApi', '1.7', 'set_cell_style');
    const style = String(need(a, 'style'));
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address'); range.style = style; await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), style }, [`${ws.name}!${ExcelHand.#bare(range.address)} 에 셀 스타일 「${style}」`]);
    });
  }
  async #editTable(a) {
    const add = arr(a, 'add_columns'); const del = arr(a, 'delete_columns'); const resize = str(a, 'resize'); const totals = bool(a, 'show_totals');
    if (!add?.length && !del?.length && !resize && totals == null) refuse('할 일이 없습니다 — add_columns·delete_columns·resize·show_totals 중 하나');
    if (resize) this.#need('ExcelApi', '1.13', 'edit_table{resize}');
    return this.runner(async (context) => {
      const t = await this.#tableNamed(context, a); const done = [];
      if (del?.length) { for (const n of del) { const col = t.columns.getItemOrNullObject(String(n)); col.load('isNullObject,name'); await context.sync(); if (col.isNullObject) refuse(`표 '${t.name}' 에 「${n}」 열이 없습니다`); col.delete(); } await context.sync(); done.push(`열 ${del.length}개 삭제(${del.join('/')})`); }
      if (add?.length) { for (const n of add) t.columns.add(null, null, String(n)); await context.sync(); done.push(`열 ${add.length}개 추가(${add.join('/')})`); }
      if (resize) { const ws = t.worksheet; t.resize(ws.getRange(resize)); await context.sync(); done.push(`범위 ${resize}`); }
      if (totals != null) { t.showTotals = totals; await context.sync(); done.push(totals ? '요약 행 켬' : '요약 행 끔'); }
      const r = t.getRange(); r.load('address'); t.columns.load('items/name'); await context.sync(); this.#mutated();
      return this.#envelope({ table: t.name, address: ExcelHand.#bare(r.address), columns: t.columns.items.map((c) => c.name) }, [`표 '${t.name}': ${done.join(', ')} — 지금 ${ExcelHand.#bare(r.address)}`]);
    });
  }
  async #setPageSetup(a) {
    this.#need('ExcelApi', '1.9', 'set_page_setup');
    const area = str(a, 'print_area'); const orient = str(a, 'orientation'); const fw = int(a, 'fit_width'); const fh = int(a, 'fit_height'); const titles = str(a, 'title_rows'); const grid = bool(a, 'gridlines'); const center = bool(a, 'center');
    const margins = a.margins && typeof a.margins === 'object' ? a.margins : null;
    const words = [area && (area.toLowerCase() === 'none' ? '인쇄 영역 해제' : `인쇄 영역 ${area}`), orient && (orient === 'Landscape' ? '가로' : '세로'), (fw != null || fh != null) && `쪽 맞춤 ${fw ?? '자동'}×${fh ?? '자동'}`, titles && (titles.toLowerCase() === 'none' ? '반복 행 해제' : `반복 행 ${titles}`), grid != null && (grid ? '눈금선 인쇄' : '눈금선 인쇄 안 함'), center != null && (center ? '가운데' : '가운데 해제'), margins && '여백'].filter(Boolean);
    if (words.length === 0) refuse('바꿀 것이 없습니다 — print_area·orientation·fit_width·fit_height·title_rows·gridlines·center·margins 중 하나');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name'); const pl = ws.pageLayout;
      // 해제는 빈 글로 — null 은 InvalidArgument 다(실물 2026-09-06).
      if (area) { if (area.toLowerCase() === 'none') pl.setPrintArea(''); else pl.setPrintArea(ws.getRange(area)); }
      if (orient) pl.orientation = orient;
      if (fw != null || fh != null) pl.zoom = { horizontalFitToPages: fw ?? 0, verticalFitToPages: fh ?? 0 };
      if (titles) { if (titles.toLowerCase() === 'none') pl.setPrintTitleRows(''); else pl.setPrintTitleRows(titles); }
      if (grid != null) pl.printGridlines = grid; if (center != null) pl.centerHorizontally = center;
      if (margins) { for (const [k, prop] of [['left', 'leftMargin'], ['right', 'rightMargin'], ['top', 'topMargin'], ['bottom', 'bottomMargin']]) { const v = num(margins, k); if (v != null) pl[prop] = v; } }
      await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name }, [`시트 '${ws.name}' 인쇄: ${words.join(', ')}`]);
    });
  }
  async #protectWorkbook(a) {
    this.#need('ExcelApi', '1.7', 'protect_workbook');
    const on = bool(a, 'protected') ?? true; const pw = str(a, 'password');
    return this.runner(async (context) => {
      const p = context.workbook.protection; p.load('protected'); await context.sync();
      if (on) { if (p.protected) refuse('통합 문서 구조가 이미 보호되어 있습니다'); p.protect(pw ?? undefined); } else { if (!p.protected) refuse('통합 문서 구조가 보호되어 있지 않습니다'); p.unprotect(pw ?? undefined); }
      await context.sync(); this.#mutated();
      return this.#envelope({ protected: on, password: Boolean(pw) }, [on ? `통합 문서 구조를 보호했습니다${pw ? ' (암호)' : ''}` : '통합 문서 구조 보호를 풀었습니다']);
    });
  }
  async #setRowsColumns(a) {
    const rows = str(a, 'rows'); const cols = str(a, 'columns');
    if ((rows == null) === (cols == null)) refuse('rows("3:5") 나 columns("B:D") 중 하나를 주세요');
    const hidden = bool(a, 'hidden'); const group = bool(a, 'group'); const height = num(a, 'height'); const width = num(a, 'width');
    if (group != null) this.#need('ExcelApi', '1.10', 'set_rows_columns{group}');
    const words = [hidden != null && (hidden ? '숨김' : '보임'), group != null && (group ? '그룹' : '그룹 해제'), height != null && `높이 ${height}pt`, width != null && `너비 ${width}pt`].filter(Boolean);
    if (words.length === 0) refuse('바꿀 것이 없습니다 — hidden·group·height·width 중 하나');
    if (rows != null && width != null) refuse('width 는 columns 에만 — rows 에는 height');
    if (cols != null && height != null) refuse('height 는 rows 에만 — columns 에는 width');
    const span = rows != null ? (rows.includes(':') ? rows : `${rows}:${rows}`) : (cols.includes(':') ? cols : `${cols}:${cols}`);
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name'); const range = ws.getRange(span); range.load('address'); await context.sync();
      if (hidden != null) { if (rows != null) range.rowHidden = hidden; else range.columnHidden = hidden; }
      if (height != null) range.format.rowHeight = height; if (width != null) range.format.columnWidth = width;
      if (group != null) { if (group) range.group(rows != null ? 'ByRows' : 'ByColumns'); else range.ungroup(rows != null ? 'ByRows' : 'ByColumns'); }
      await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, span, kind: rows != null ? 'rows' : 'columns' }, [`${ws.name} ${rows != null ? '행' : '열'} ${span}: ${words.join(', ')}`]);
    });
  }
  async #setTabColor(a) {
    this.#need('ExcelApi', '1.7', 'set_tab_color');
    const color = hex(a, 'color', true) ?? refuse('color 가 없습니다 — #RRGGBB 또는 none');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name'); ws.tabColor = color === 'none' ? '' : color; await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, color }, [`시트 '${ws.name}' 탭 색 → ${color === 'none' ? '없음' : color}`]);
    });
  }
  async #setSheetView(a) {
    this.#need('ExcelApi', '1.8', 'set_sheet_view');
    const grid = bool(a, 'gridlines'); const head = bool(a, 'headings');
    if (grid == null && head == null) refuse('바꿀 것이 없습니다 — gridlines·headings 중 하나');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name'); if (grid != null) ws.showGridlines = grid; if (head != null) ws.showHeadings = head; await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, gridlines: grid, headings: head }, [`시트 '${ws.name}': ${[grid != null && `눈금선 ${grid ? '켬' : '끔'}`, head != null && `머리글 ${head ? '켬' : '끔'}`].filter(Boolean).join(', ')}`]);
    });
  }
  async #setWorkbookProperties(a) {
    this.#need('ExcelApi', '1.7', 'set_workbook_properties');
    const keys = ['title', 'subject', 'author', 'keywords', 'comments', 'category']; const set = keys.filter((k) => str(a, k) != null);
    if (set.length === 0) refuse('바꿀 것이 없습니다 — title·subject·author·keywords·comments·category 중 하나');
    return this.runner(async (context) => {
      const props = context.workbook.properties; for (const k of set) props[k] = str(a, k); await context.sync(); this.#mutated();
      return this.#envelope(Object.fromEntries(set.map((k) => [k, str(a, k)])), [`통합 문서 속성: ${set.map((k) => `${k}=「${clip(str(a, k), 30)}」`).join(', ')}`]);
    });
  }
  async #replaceAll(a) {
    this.#need('ExcelApi', '1.9', 'replace_all');
    const find = String(need(a, 'find')); const replace = String(a.replace ?? refuse('replace 가 없습니다(빈 문자열은 됩니다)'));
    const matchCase = bool(a, 'match_case') ?? false; const completeMatch = bool(a, 'whole_cell') ?? false;
    return this.runner(async (context) => {
      const sheets = [];
      if (str(a, 'sheet')) { const ws = this.#sheet(context, a); ws.load('name'); sheets.push(ws); } else { const all = context.workbook.worksheets; all.load('items/name'); await context.sync(); sheets.push(...all.items); }
      const counts = sheets.map((ws) => ws.replaceAll(find, replace, { matchCase, completeMatch }));
      await context.sync(); this.#mutated();
      const per = sheets.map((ws, i) => ({ sheet: ws.name, cells: counts[i].value })).filter((x) => x.cells > 0);
      const total = per.reduce((s, x) => s + x.cells, 0);
      if (total === 0) refuse(`「${clip(find, 40)}」 가 ${str(a, 'sheet') ? `시트 ${sheets[0].name}` : '통합 문서'} 에 없습니다 — 바꾼 것이 없습니다`);
      return this.#envelope({ find, replace, cells: total, sheets: per }, [`「${clip(find, 30)}」 → 「${clip(replace, 30)}」 셀 ${total}개 (${per.map((x) => `${x.sheet} ${x.cells}`).join(', ')})`]);
    });
  }
  async #copyRange(a) {
    this.#need('ExcelApi', '1.9', 'copy_range');
    const source = String(need(a, 'source')); const mode = (str(a, 'mode') ?? 'all').toLowerCase(); const transpose = bool(a, 'transpose') ?? false;
    const copyType = { all: 'All', values: 'Values', formulas: 'Formulas', formats: 'Formats' }[mode] ?? refuse(`mode 는 all, values, formulas, formats 중 하나 — '${mode}'`);
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name');
      const src = this.#range(context, { sheet: str(a, 'source_sheet') ?? str(a, 'sheet'), address: source }); src.ws.load('name'); src.range.load('address,rowCount,columnCount');
      await context.sync();
      const dest = range.getCell(0, 0).getResizedRange((transpose ? src.range.columnCount : src.range.rowCount) - 1, (transpose ? src.range.rowCount : src.range.columnCount) - 1);
      dest.copyFrom(src.range, copyType, false, transpose); dest.load('address'); await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, source: `${src.ws.name}!${ExcelHand.#bare(src.range.address)}`, address: ExcelHand.#bare(dest.address), mode, transpose }, [`${src.ws.name}!${ExcelHand.#bare(src.range.address)} → ${ws.name}!${ExcelHand.#bare(dest.address)} (${mode}${transpose ? ', 행열 바꿈' : ''})`]);
    });
  }
  async #fillRange(a) {
    this.#need('ExcelApi', '1.9', 'fill_range');
    const to = String(need(a, 'to')); const fill = (str(a, 'fill') ?? 'default').toLowerCase();
    const kind = { default: 'FillDefault', copy: 'FillCopy', series: 'FillSeries', formats: 'FillFormats', values: 'FillValues' }[fill] ?? refuse(`fill 은 default, copy, series, formats, values 중 하나 — '${fill}'`);
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      const dest = ws.getRange(to); dest.load('address');
      await context.sync();
      range.autoFill(dest, kind); await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), to: ExcelHand.#bare(dest.address), fill }, [`${ws.name}!${ExcelHand.#bare(range.address)} 를 ${ExcelHand.#bare(dest.address)} 까지 채웠습니다 (${fill})`]);
    });
  }
  async #removeDuplicates(a) {
    this.#need('ExcelApi', '1.9', 'remove_duplicates');
    const cols = arr(a, 'columns'); const header = bool(a, 'has_header') ?? true;
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address,columnCount'); await context.sync();
      const which = cols && cols.length ? cols.map((v) => int({ v }, 'v')) : Array.from({ length: range.columnCount }, (_, i) => i);
      for (const c of which) if (c == null || c < 0 || c >= range.columnCount) refuse(`columns 가 블록 밖입니다 — ${c} (0부터 ${range.columnCount - 1}까지)`);
      const res = range.removeDuplicates(which, header); res.load('removed,uniqueRemaining'); await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), removed: res.removed, remaining: res.uniqueRemaining }, [`${ws.name}!${ExcelHand.#bare(range.address)}: 중복 ${res.removed}행 제거, ${res.uniqueRemaining}행 남음`]);
    });
  }
  async #clearRange(a) {
    const what = (str(a, 'what') ?? 'all').toLowerCase();
    const applyTo = { all: 'All', contents: 'Contents', formats: 'Formats', hyperlinks: 'Hyperlinks' }[what] ?? refuse(`what 는 all, contents, formats, hyperlinks 중 하나입니다 — '${what}'`);
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address,values');
      await context.sync();
      const had = range.values.flat().filter((v) => v !== '' && v != null).length;
      range.clear(applyTo); await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, what, had_values: had }, [`${ws.name}!${at} 를 지웠습니다(${what})` + (had && what !== 'formats' && what !== 'hyperlinks' ? ` — 값이 있던 셀 ${had}개` : '')]);
    });
  }

  async #merge(a, merge) {
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      if (merge) range.merge(bool(a, 'across') ?? false); else range.unmerge();
      await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, merged: merge }, [merge ? `${ws.name}!${at} 를 병합했습니다` : `${ws.name}!${at} 의 병합을 풀었습니다`]);
    });
  }

  async #insertDelete(a, insert) {
    const shift = (str(a, 'shift') ?? (insert ? 'down' : 'up')).toLowerCase();
    const dir = insert ? { down: 'Down', right: 'Right' }[shift] : { up: 'Up', left: 'Left' }[shift];
    if (!dir) refuse(insert ? `shift 는 down 또는 right 입니다 — '${shift}'` : `shift 는 up 또는 left 입니다 — '${shift}'`);
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address,rowCount,columnCount');
      // 값은 **쓰인 부분만** 읽는다 — 행·열 전체("7:7", "C:D")의 values 는 2021 판이 null 로 주어 .flat() 에서 죽었다(실물 2026-09-07).
      const used = range.getUsedRangeOrNullObject(true); used.load('values,isNullObject');
      await context.sync();
      const had = used.isNullObject || !Array.isArray(used.values) ? 0 : used.values.flat().filter((v) => v !== '' && v != null).length;
      if (insert) range.insert(dir); else range.delete(dir);
      await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, shift, cells: range.rowCount * range.columnCount, had_values: insert ? null : had },
        [insert ? `${ws.name}!${at} 자리에 빈 셀을 끼워 넣었습니다(있던 셀은 ${shift === 'down' ? '아래' : '오른쪽'}으로)` : `${ws.name}!${at} 를 삭제했습니다(나머지는 ${shift === 'up' ? '위' : '왼쪽'}으로)` + (had ? ` — 값이 있던 셀 ${had}개` : '')]);
    });
  }

  async #autofit(a) {
    const what = (str(a, 'what') ?? 'columns').toLowerCase();
    if (!['columns', 'rows', 'both'].includes(what)) refuse(`what 는 columns, rows, both 중 하나입니다 — '${what}'`);
    return this.runner(async (context) => {
      const { ws, range, used } = this.#range(context, a, { must: false }); ws.load('name'); range.load('address,isNullObject');
      await context.sync();
      if (used && range.isNullObject) refuse('이 시트는 비어 있어 맞출 것이 없습니다');
      if (what !== 'rows') range.format.autofitColumns(); if (what !== 'columns') range.format.autofitRows();
      await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, what }, [`${ws.name}!${at} 의 ${what === 'rows' ? '행 높이' : what === 'both' ? '열 너비와 행 높이' : '열 너비'}를 맞췄습니다`]);
    });
  }

  async #hyperlink(a) {
    this.#need('ExcelApi', '1.7', 'set_hyperlink');
    const url = str(a, 'url'); const ts = str(a, 'target_sheet'); const ta = str(a, 'target_address'); const text = str(a, 'text'); const tip = str(a, 'screen_tip');
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      if (!url && !ts) {
        range.clear('Hyperlinks'); await context.sync(); this.#mutated();
        return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), removed: true }, [`${ws.name}!${ExcelHand.#bare(range.address)} 의 링크를 뗐습니다`]);
      }
      const link = {};
      if (url) link.address = url; else link.documentReference = `'${ts}'!${ta ?? 'A1'}`;
      if (text) link.textToDisplay = text; if (tip) link.screenTip = tip;
      range.hyperlink = link;
      await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, url: url ?? null, target: url ? null : link.documentReference }, [`${ws.name}!${at} 에 링크 → ${url ?? link.documentReference}`]);
    });
  }

  // ── 시트 ──
  async #addSheet(a) {
    const name = str(a, 'name'); const after = str(a, 'after'); const activate = bool(a, 'activate') ?? true;
    if (name != null && (name.length > 31 || /[:\\/?*\[\]]/.test(name))) refuse(`시트 이름은 31자 이하이고 : \\ / ? * [ ] 를 못 씁니다 — '${name}'`);
    return this.runner(async (context) => {
      const all = context.workbook.worksheets; all.load('items/name');
      await context.sync();
      if (name && all.items.some((s) => s.name === name)) refuse(`'${name}' 시트가 이미 있습니다 — 다른 이름을 주거나 그 시트를 쓰세요`);
      const ws = all.add(name ?? undefined); ws.load('name,position');
      if (after) { const ref = all.getItem(after); ref.load('position'); await context.sync(); ws.position = ref.position + 1; }
      if (activate) ws.activate();
      await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, index: ws.position + 1, activated: activate }, [`시트 '${ws.name}' 을 만들었습니다(탭 ${ws.position + 1}번)`]);
    });
  }
  async #deleteSheet(a) {
    need(a, 'sheet');
    return this.runner(async (context) => {
      const all = context.workbook.worksheets; all.load('count');
      const ws = this.#sheet(context, a); ws.load('name');
      await context.sync();
      if (all.count <= 1) refuse('마지막 시트는 지울 수 없습니다');
      const name = ws.name; ws.delete(); await context.sync(); this.#mutated();
      return this.#envelope({ deleted: name }, [`시트 '${name}' 을 지웠습니다 — 되돌릴 수 없습니다`]);
    });
  }
  async #renameSheet(a) {
    need(a, 'sheet'); const to = String(need(a, 'name'));
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name'); await context.sync();
      const was = ws.name; ws.name = to; await context.sync(); this.#mutated();
      return this.#envelope({ sheet: to, was }, [`시트 '${was}' → '${to}'`]);
    });
  }
  async #moveSheet(a) {
    need(a, 'sheet'); const to = int(a, 'to'); if (to == null || to < 1) refuse('to 는 1 이상의 탭 위치입니다');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name,position'); const all = context.workbook.worksheets; all.load('count');
      await context.sync();
      if (to > all.count) refuse(`탭이 ${all.count}개라 ${to}번 자리는 없습니다`);
      const was = ws.position + 1; ws.position = to - 1; await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, from: was, to }, [`시트 '${ws.name}' 을 ${was}번에서 ${to}번으로 옮겼습니다`]);
    });
  }
  async #copySheet(a) {
    this.#need('ExcelApi', '1.7', 'copy_sheet'); need(a, 'sheet');
    const name = str(a, 'name'); const after = str(a, 'after');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      const copy = after ? ws.copy('After', context.workbook.worksheets.getItem(after)) : ws.copy('After', ws);
      copy.load('name,position');
      await context.sync();
      if (name) { copy.name = name; await context.sync(); }
      this.#mutated();
      return this.#envelope({ sheet: name ?? copy.name, from: ws.name, index: copy.position + 1 }, [`시트 '${ws.name}' 을 복사해 '${name ?? copy.name}' 을 만들었습니다`]);
    });
  }
  async #visibility(a) {
    need(a, 'sheet'); const v = String(need(a, 'visibility'));
    if (!['Visible', 'Hidden', 'VeryHidden'].includes(v)) refuse(`visibility 는 Visible, Hidden, VeryHidden 중 하나입니다 — '${v}'`);
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name'); const all = context.workbook.worksheets; all.load('items/visibility');
      await context.sync();
      if (v !== 'Visible' && all.items.filter((s) => s.visibility === 'Visible').length <= 1) refuse('보이는 시트가 하나뿐이라 숨길 수 없습니다');
      ws.visibility = v; await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, visibility: v }, [`시트 '${ws.name}' → ${v}`]);
    });
  }
  async #activate(a) {
    need(a, 'sheet');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name'); ws.activate();
      const address = str(a, 'address');
      if (address) ws.getRange(address).select();
      await context.sync();
      return this.#envelope({ sheet: ws.name, address: address ?? null }, [`시트 '${ws.name}'${address ? ` ${address}` : ''} 로 갔습니다`]);
    });
  }
  async #freeze(a) {
    this.#need('ExcelApi', '1.7', 'freeze_panes');
    const rows = int(a, 'rows') ?? 0; const cols = int(a, 'columns') ?? 0;
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      if (rows === 0 && cols === 0) ws.freezePanes.unfreeze();
      else if (rows > 0 && cols > 0) ws.freezePanes.freezeAt(ws.getRangeByIndexes(0, 0, rows, cols));
      else if (rows > 0) ws.freezePanes.freezeRows(rows); else ws.freezePanes.freezeColumns(cols);
      await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, rows, columns: cols }, [rows === 0 && cols === 0 ? `시트 '${ws.name}' 의 틀 고정을 풀었습니다` : `시트 '${ws.name}': 위 ${rows}행·왼쪽 ${cols}열 고정`]);
    });
  }
  async #protect(a, on) {
    this.#need('ExcelApi', '1.7', on ? 'protect_sheet' : 'unprotect_sheet');
    const pw = str(a, 'password');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      if (on) ws.protection.protect({ allowFormatCells: bool(a, 'allow_formatting') ?? false, allowSort: bool(a, 'allow_sorting') ?? false, allowAutoFilter: bool(a, 'allow_filtering') ?? false }, pw ?? undefined);
      else ws.protection.unprotect(pw ?? undefined);
      await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, protected: on }, [on ? `시트 '${ws.name}' 을 보호했습니다${pw ? '(암호 있음)' : '(암호 없음)'}` : `시트 '${ws.name}' 의 보호를 풀었습니다`]);
    });
  }

  // ── 표 ──
  async #addTable(a) {
    const hasHeaders = bool(a, 'has_headers') ?? true; const name = str(a, 'name'); const style = str(a, 'table_style'); const totals = bool(a, 'show_totals');
    if (name && !/^[A-Za-z_가-힣][\w가-힣.]*$/.test(name)) refuse(`표 이름은 글자로 시작하고 빈칸이 없어야 합니다 — '${name}'`);
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      const existing = context.workbook.tables; existing.load('items/name');
      await context.sync();
      const at = ExcelHand.#bare(range.address);
      const t = ws.tables.add(`'${ws.name}'!${at}`, hasHeaders);
      t.load('name');
      if (name) t.name = name; if (style) t.style = style; if (totals != null) t.showTotals = totals;
      await context.sync(); t.load('name'); await context.sync(); this.#mutated(); // 이름은 sync 뒤에 다시 읽어야 지은 이름이다(실물: 표1 로 보고됐다)
      return this.#envelope({ table: t.name, sheet: ws.name, address: at, has_headers: hasHeaders, style: style ?? null }, [`${ws.name}!${at} 를 표 '${t.name}' 으로 만들었습니다${style ? ` (${style})` : ''}`]);
    });
  }
  async #tableNamed(context, a) {
    const name = String(need(a, 'table')); const t = context.workbook.tables.getItemOrNullObject(name); t.load('name,isNullObject');
    await context.sync();
    if (t.isNullObject) refuse(`'${name}' 이라는 표가 없습니다 — describe_sheet 가 이름을 줍니다`);
    return t;
  }
  async #setTableCells(a) {
    const cells = arr(a, 'cells'); if (!cells || cells.length === 0) refuse('cells 가 비었습니다 — [{row, column, value}]');
    return this.runner(async (context) => {
      const t = await this.#tableNamed(context, a);
      const h = t.getHeaderRowRange(); h.load('values'); const body = t.getDataBodyRange(); body.load('rowCount,columnCount');
      await context.sync();
      const headers = h.values[0].map(String); const rows = body.rowCount;
      let appended = 0;
      for (const c of cells) {
        const r = int(c, 'row'); const col = typeof c.column === 'number' ? c.column : headers.indexOf(String(c.column));
        if (r == null || r < 0) refuse(`row 는 0 이상입니다 — ${JSON.stringify(c)}`);
        if (col < 0 || col >= headers.length) refuse(`'${c.column}' 은 이 표의 열이 아닙니다 — ${headers.join(', ')}`);
        while (r >= rows + appended) { t.rows.add(null, [headers.map(() => '')]); appended += 1; }
      }
      if (appended) await context.sync();
      const b2 = t.getDataBodyRange();
      for (const c of cells) {
        const col = typeof c.column === 'number' ? c.column : headers.indexOf(String(c.column));
        b2.getCell(int(c, 'row'), col).formulas = [[c.value ?? '']];
      }
      await context.sync(); this.#mutated();
      return this.#envelope({ table: t.name, cells: cells.length, appended }, [`표 '${t.name}' 의 칸 ${cells.length}개를 적었습니다${appended ? ` (행 ${appended}개 추가)` : ''}`]);
    });
  }
  async #addTableRows(a) {
    const rows = grid(a, 'rows'); const at = int(a, 'at');
    return this.runner(async (context) => {
      const t = await this.#tableNamed(context, a);
      const h = t.getHeaderRowRange(); h.load('values'); await context.sync();
      const width = h.values[0].length;
      if (rows.cols !== width) refuse(`이 표는 ${width}열인데 줄마다 ${rows.cols}칸입니다`);
      t.rows.add(at ?? -1, rows.cells); await context.sync(); this.#mutated();
      return this.#envelope({ table: t.name, added: rows.rows, at: at ?? null }, [`표 '${t.name}' 에 행 ${rows.rows}개를 ${at != null ? `${at}번 앞에 끼워` : '끝에 붙여'} 넣었습니다`]);
    });
  }
  async #removeTable(a) {
    const del = bool(a, 'delete_data') ?? false;
    return this.runner(async (context) => {
      const t = await this.#tableNamed(context, a); const r = t.getRange(); r.load('address'); await context.sync();
      const at = ExcelHand.#bare(r.address);
      if (del) { t.delete(); } else { t.convertToRange(); }
      await context.sync(); this.#mutated();
      return this.#envelope({ table: t.name, address: at, deleted_data: del }, [del ? `표 '${t.name}' 과 그 셀(${at})을 지웠습니다` : `표 '${t.name}' 을 풀었습니다 — ${at} 의 값은 그대로입니다`]);
    });
  }
  async #sort(a) {
    const by = arr(a, 'by'); if (!by || by.length === 0) refuse('by 가 비었습니다 — [{column, ascending}]');
    const hasHeaders = bool(a, 'has_headers') ?? true;
    return this.runner(async (context) => {
      if (str(a, 'table')) {
        const t = await this.#tableNamed(context, a); const h = t.getHeaderRowRange(); h.load('values'); await context.sync();
        const headers = h.values[0].map(String);
        const fields = by.map((f) => { const k = typeof f.column === 'number' ? f.column : headers.indexOf(String(f.column)); if (k < 0) refuse(`'${f.column}' 은 이 표의 열이 아닙니다 — ${headers.join(', ')}`); return { key: k, ascending: bool(f, 'ascending') ?? true }; });
        t.sort.apply(fields, false); await context.sync(); this.#mutated();
        return this.#envelope({ table: t.name, by: fields }, [`표 '${t.name}' 을 ${fields.map((f) => `${headers[f.key]}${f.ascending ? '↑' : '↓'}`).join(', ')} 로 정렬했습니다`]);
      }
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      const fields = by.map((f) => { const k = int(f, 'column'); if (k == null || k < 0) refuse(`column 은 범위 안의 0-based 열 번호입니다 — ${JSON.stringify(f)}`); return { key: k, ascending: bool(f, 'ascending') ?? true }; });
      range.sort.apply(fields, false, hasHeaders); await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, by: fields, has_headers: hasHeaders }, [`${ws.name}!${at} 를 ${fields.map((f) => `${f.key}열${f.ascending ? '↑' : '↓'}`).join(', ')} 로 정렬했습니다`]);
    });
  }
  async #filter(a) {
    this.#need('ExcelApi', '1.9', 'filter_table');
    const column = String(need(a, 'column')); const values = arr(a, 'values'); const criterion = str(a, 'criterion');
    return this.runner(async (context) => {
      const t = await this.#tableNamed(context, a); const col = t.columns.getItemOrNullObject(column); col.load('isNullObject,name'); await context.sync();
      if (col.isNullObject) refuse(`'${column}' 은 표 '${t.name}' 의 열이 아닙니다`);
      let said;
      if (values && values.length) { col.filter.applyValuesFilter(values.map(String)); said = `값 ${values.join(', ')} 만`; }
      else if (criterion) {
        const m = /^(top|bottom)(\d+)$/i.exec(criterion);
        if (m) { if (m[1].toLowerCase() === 'top') col.filter.applyTopItemsFilter(Number(m[2])); else col.filter.applyBottomItemsFilter(Number(m[2])); }
        else col.filter.applyCustomFilter(criterion);
        said = `조건 ${criterion}`;
      } else { col.filter.clear(); said = '필터 해제'; }
      await context.sync(); this.#mutated();
      return this.#envelope({ table: t.name, column, values: values ?? null, criterion: criterion ?? null }, [`표 '${t.name}' 의 '${column}' 열: ${said}`]);
    });
  }

  // ── 차트 ──
  async #addChart(a) {
    const source = String(need(a, 'source')); const type = chartTypeOf(str(a, 'chart_type') ?? str(a, 'kind') ?? str(a, 'type'));
    const seriesBy = str(a, 'series_by') ?? 'Columns'; if (!['Columns', 'Rows', 'Auto'].includes(seriesBy)) refuse(`series_by 는 Columns 또는 Rows 입니다 — '${seriesBy}'`);
    const title = str(a, 'title'); const name = str(a, 'name');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      const at = ExcelHand.#sheetOf(source);
      const src = at ? context.workbook.worksheets.getItem(at.sheet).getRange(at.address) : ws.getRange(source);
      src.load('address,left,top,width,height,rowCount,columnCount');
      await context.sync();
      if (src.rowCount < 2 || src.columnCount < 2) refuse(`source ${ExcelHand.#bare(src.address)} 는 ${src.rowCount}×${src.columnCount} 라 차트를 못 만듭니다 — 머리글 행과 값 열이 있어야 합니다`);
      const chart = ws.charts.add(type, src, seriesBy);
      chart.load('name');
      chart.left = num(a, 'left') ?? (src.left + src.width + 20); chart.top = num(a, 'top') ?? src.top;
      chart.width = num(a, 'width') ?? 480; chart.height = num(a, 'height') ?? 300;
      if (title != null) { chart.title.text = title; chart.title.visible = title !== ''; }
      if (name) chart.name = name;
      await context.sync(); this.#mutated();
      const ko = CHART_KO.get(type) ?? type;
      return this.#envelope({ chart: name ?? chart.name, sheet: ws.name, type, type_ko: ko, source: ExcelHand.#bare(src.address), left: chart.left, top: chart.top, width: chart.width, height: chart.height },
        [`시트 '${ws.name}' 에 ${ko} 차트 '${name ?? chart.name}' 을 넣었습니다 — 원본 ${ExcelHand.#bare(src.address)}${title ? `, 제목 「${title}」` : ''}`]);
    });
  }
  async #chartNamed(context, a) {
    const name = String(need(a, 'chart')); const ws = this.#sheet(context, a); ws.load('name');
    const c = ws.charts.getItemOrNullObject(name); c.load('name,isNullObject'); await context.sync();
    if (c.isNullObject) refuse(`'${name}' 이라는 차트가 시트 '${ws.name}' 에 없습니다 — describe_sheet 가 이름을 줍니다`);
    return { ws, c };
  }
  async #formatChart(a) {
    const said = [];
    return this.runner(async (context) => {
      const { ws, c } = await this.#chartNamed(context, a);
      const title = str(a, 'title'); if (title != null) { c.title.text = title; c.title.visible = title !== ''; said.push(title ? `제목 「${title}」` : '제목 없음'); }
      const xt = str(a, 'x_title'); const yt = str(a, 'y_title');
      if (xt != null || yt != null) {
        this.#need('ExcelApi', '1.7', 'format_chart{x_title,y_title}');
        if (xt != null) { c.axes.categoryAxis.title.text = xt; c.axes.categoryAxis.title.visible = xt !== ''; said.push(`가로축 「${xt}」`); }
        if (yt != null) { c.axes.valueAxis.title.text = yt; c.axes.valueAxis.title.visible = yt !== ''; said.push(`세로축 「${yt}」`); }
      }
      const legend = str(a, 'legend'); if (legend) { if (legend.toLowerCase() === 'none') { c.legend.visible = false; said.push('범례 없음'); } else { c.legend.visible = true; c.legend.position = legend; said.push(`범례 ${legend}`); } }
      const labels = bool(a, 'data_labels'); if (labels != null) { c.dataLabels.showValue = labels; said.push(labels ? '값 표시' : '값 표시 해제'); }
      const type = str(a, 'chart_type'); if (type) { c.chartType = chartTypeOf(type); said.push(`종류 ${CHART_KO.get(c.chartType) ?? c.chartType}`); }
      const source = str(a, 'source'); if (source) { const at = ExcelHand.#sheetOf(source); c.setData(at ? context.workbook.worksheets.getItem(at.sheet).getRange(at.address) : ws.getRange(source), 'Auto'); said.push(`원본 ${source}`); }
      const ymin = num(a, 'y_min'); const ymax = num(a, 'y_max'); const yfmt = str(a, 'y_format');
      if (ymin != null || ymax != null) { this.#need('ExcelApi', '1.7', 'format_chart{y_min,y_max}'); if (ymin != null) { c.axes.valueAxis.minimum = ymin; said.push(`세로축 최소 ${ymin}`); } if (ymax != null) { c.axes.valueAxis.maximum = ymax; said.push(`세로축 최대 ${ymax}`); } }
      if (yfmt) { this.#need('ExcelApi', '1.8', 'format_chart{y_format}'); c.axes.valueAxis.numberFormat = yfmt; said.push(`세로축 서식 ${yfmt}`); }
      const series = arr(a, 'series');
      if (series && series.length) {
        c.series.load('items/name'); c.load('chartType'); await context.sync();
        // 선 차트(꺾은선·분산·방사형)의 계열 색은 선 색이다 — 채우기에 setSolidColor 는 InvalidOperation(실물 2026-09-06).
        const lineLike = /Line|Scatter|Radar/.test(String(c.chartType));
        for (const s of series) {
          const idx = int(s, 'index'); const byName = str(s, 'name');
          const item = idx != null ? c.series.items[idx] : c.series.items.find((x) => x.name === byName);
          if (!item) refuse(`계열이 없습니다 — ${JSON.stringify(s)} (계열 ${c.series.items.length}개: ${c.series.items.map((x) => x.name).join(', ')})`);
          const label = byName ?? `#${idx}`;
          const color = hex(s, 'color'); if (color) { if (lineLike) item.format.line.color = color; else item.format.fill.setSolidColor(color); said.push(`계열 ${label} 색 ${color}`); }
          const nn = str(s, 'new_name'); if (nn) { item.name = nn; said.push(`계열 ${label} 이름 「${nn}」`); }
          const tl = str(s, 'trendline'); if (tl) { this.#need('ExcelApi', '1.7', 'format_chart{series.trendline}'); if (tl === 'none') { item.trendlines.clear(); said.push(`계열 ${label} 추세선 없음`); } else { item.trendlines.add('Linear'); said.push(`계열 ${label} 추세선`); } }
          const mk = str(s, 'marker'); if (mk) { this.#need('ExcelApi', '1.7', 'format_chart{series.marker}'); item.markerStyle = mk; said.push(`계열 ${label} 표식 ${mk}`); }
        }
      }
      for (const k of ['left', 'top', 'width', 'height']) { const v = num(a, k); if (v != null) { c[k] = v; said.push(`${k} ${v}`); } }
      if (said.length === 0) refuse('바꿀 것이 하나도 안 왔습니다 — title, x_title, y_title, legend, data_labels, chart_type, series, y_min/y_max/y_format, source, left/top/width/height');
      await context.sync(); this.#mutated();
      return this.#envelope({ chart: c.name, sheet: ws.name, changed: said.length }, [`차트 '${c.name}': ${said.join(', ')}`]);
    });
  }
  async #deleteChart(a) {
    return this.runner(async (context) => {
      const { ws, c } = await this.#chartNamed(context, a); const name = c.name; c.delete(); await context.sync(); this.#mutated();
      return this.#envelope({ chart: name, sheet: ws.name }, [`차트 '${name}' 을 지웠습니다`]);
    });
  }

  // ── 조건부 서식·유효성·이름·메모 ──
  async #addConditionalFormat(a) {
    this.#need('ExcelApi', '1.6', 'add_conditional_format');
    const kind = (str(a, 'cf_kind') ?? str(a, 'kind') ?? refuse('cf_kind 가 없습니다')).toLowerCase();
    const fill = hex(a, 'fill'); const color = hex(a, 'color'); const bold = bool(a, 'bold');
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      let cf; let said;
      const paint = (fmt) => { if (fill) fmt.fill.color = fill; if (color) fmt.font.color = color; if (bold != null) fmt.font.bold = bold; };
      switch (kind) {
        case 'cell_value': {
          const op = str(a, 'operator') ?? 'GreaterThan'; const v1 = str(a, 'value') ?? refuse('cell_value 에는 value 가 있어야 합니다'); const v2 = str(a, 'value2');
          cf = range.conditionalFormats.add('CellValue'); cf.cellValue.rule = { formula1: v1, formula2: v2 ?? undefined, operator: op }; paint(cf.cellValue.format);
          said = `값이 ${op} ${v1}${v2 ? `~${v2}` : ''} 일 때`; break;
        }
        case 'contains_text': {
          const op = str(a, 'operator') ?? 'Contains'; const t = str(a, 'value') ?? refuse('contains_text 에는 value 가 있어야 합니다');
          cf = range.conditionalFormats.add('ContainsText'); cf.textComparison.rule = { operator: op, text: t }; paint(cf.textComparison.format);
          said = `글이 ${op} 「${t}」 일 때`; break;
        }
        case 'color_scale': {
          const colors = arr(a, 'colors') ?? ['#F8696B', '#FFEB84', '#63BE7B'];
          cf = range.conditionalFormats.add('ColorScale');
          const crit = colors.length >= 3 ? { minimum: { type: 'LowestValue', color: colors[0] }, midpoint: { type: 'Percentile', formula: '50', color: colors[1] }, maximum: { type: 'HighestValue', color: colors[2] } }
            : { minimum: { type: 'LowestValue', color: colors[0] }, maximum: { type: 'HighestValue', color: colors[1] ?? '#63BE7B' } };
          cf.colorScale.criteria = crit; said = `색조 ${colors.join('→')}`; break;
        }
        case 'data_bar': {
          const colors = arr(a, 'colors'); cf = range.conditionalFormats.add('DataBar'); if (colors?.[0]) cf.dataBar.positiveFormat.fillColor = colors[0]; said = '데이터 막대'; break;
        }
        case 'icon_set': {
          const set = str(a, 'icon_set') ?? 'ThreeTrafficLights1'; cf = range.conditionalFormats.add('IconSet'); cf.iconSet.style = set;
          cf.iconSet.criteria = [{}, { type: 'Number', formula: '=33', operator: 'GreaterThanOrEqual' }, { type: 'Number', formula: '=67', operator: 'GreaterThanOrEqual' }].map((c, i) => (i === 0 ? { type: 'Number', formula: '=0', operator: 'GreaterThanOrEqual' } : c));
          said = `아이콘 ${set}`; break;
        }
        case 'top_bottom': {
          const rank = int(a, 'rank') ?? 10; const bottom = bool(a, 'bottom') ?? false; const pct = bool(a, 'percent') ?? false;
          cf = range.conditionalFormats.add('TopBottom'); cf.topBottom.rule = { rank, type: `${bottom ? 'Bottom' : 'Top'}${pct ? 'Percent' : 'Items'}` }; paint(cf.topBottom.format);
          said = `${bottom ? '하위' : '상위'} ${rank}${pct ? '%' : '개'}`; break;
        }
        case 'custom': {
          const f = str(a, 'formula') ?? refuse('custom 에는 formula 가 있어야 합니다'); cf = range.conditionalFormats.add('Custom'); cf.custom.rule.formula = f; paint(cf.custom.format);
          said = `수식 ${f} 이 참일 때`; break;
        }
        default: refuse(`cf_kind 는 cell_value, color_scale, data_bar, icon_set, contains_text, top_bottom, custom 중 하나입니다 — '${kind}'`);
      }
      await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, cf_kind: kind }, [`${ws.name}!${at} 에 조건부 서식: ${said}${fill ? ` 채우기 ${fill}` : ''}${color ? ` 글자색 ${color}` : ''}`]);
    });
  }
  async #clearConditionalFormats(a) {
    return this.runner(async (context) => {
      const { ws, range, used } = this.#range(context, a, { must: false }); ws.load('name'); range.load('address,isNullObject');
      await context.sync();
      if (used && range.isNullObject) return this.#envelope({ sheet: ws.name, cleared: 0 }, [`시트 '${ws.name}' 은 비어 있습니다`]);
      // 컬렉션에 count 속성은 없다 — getCount() 가 답을 준다(실물 2026-09-06: 「undefined개를 지웠습니다」).
      const cfs = range.conditionalFormats; const cnt = cfs.getCount(); await context.sync();
      const n = cnt.value; cfs.clearAll(); await context.sync(); if (n) this.#mutated();
      return this.#envelope({ sheet: ws.name, address: ExcelHand.#bare(range.address), cleared: n }, [`${ws.name}!${ExcelHand.#bare(range.address)} 의 조건부 서식 ${n}개를 지웠습니다`]);
    });
  }
  async #setValidation(a) {
    this.#need('ExcelApi', '1.8', 'set_validation');
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address');
      const at = () => ExcelHand.#bare(range.address);
      if (bool(a, 'clear')) { range.dataValidation.clear(); await context.sync(); this.#mutated(); return this.#envelope({ sheet: ws.name, address: at(), cleared: true }, [`${ws.name}!${at()} 의 유효성을 지웠습니다`]); }
      const kind = (str(a, 'validation_kind') ?? str(a, 'kind') ?? refuse('validation_kind 가 없습니다')).toLowerCase();
      const op = str(a, 'operator') ?? 'Between'; const v1 = str(a, 'value'); const v2 = str(a, 'value2');
      const bounds = () => { if (v1 == null) refuse(`${kind} 에는 value 가 있어야 합니다`); return { formula1: v1, formula2: v2 ?? undefined, operator: op }; };
      let rule; let said;
      switch (kind) {
        case 'list': { const values = arr(a, 'values'); const src = str(a, 'source'); if (!values && !src) refuse('list 에는 values 나 source 가 있어야 합니다'); rule = { list: { inCellDropDown: true, source: src ?? values.map(String).join(',') } }; said = `목록 ${src ?? values.join('/')}`; break; }
        case 'whole_number': rule = { wholeNumber: bounds() }; said = `정수 ${op} ${v1}${v2 ? `~${v2}` : ''}`; break;
        case 'decimal': rule = { decimal: bounds() }; said = `소수 ${op} ${v1}${v2 ? `~${v2}` : ''}`; break;
        case 'date': rule = { date: bounds() }; said = `날짜 ${op} ${v1}${v2 ? `~${v2}` : ''}`; break;
        case 'time': rule = { time: bounds() }; said = `시각 ${op} ${v1}${v2 ? `~${v2}` : ''}`; break;
        case 'text_length': rule = { textLength: bounds() }; said = `글자 수 ${op} ${v1}${v2 ? `~${v2}` : ''}`; break;
        case 'custom': { const f = str(a, 'formula') ?? refuse('custom 에는 formula 가 있어야 합니다'); rule = { custom: { formula: f } }; said = `수식 ${f}`; break; }
        default: refuse(`validation_kind 는 list, whole_number, decimal, date, time, text_length, custom 중 하나입니다 — '${kind}'`);
      }
      range.dataValidation.rule = rule;
      const prompt = str(a, 'prompt'); if (prompt) range.dataValidation.prompt = { showPrompt: true, title: '', message: prompt };
      const error = str(a, 'error'); if (error) range.dataValidation.errorAlert = { showAlert: true, style: 'Stop', title: '입력 오류', message: error };
      await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, address: at(), validation_kind: kind }, [`${ws.name}!${at()} 에 유효성: ${said}`]);
    });
  }
  async #setName(a) {
    const name = String(need(a, 'name')); const refersTo = String(need(a, 'refers_to')); const comment = str(a, 'comment');
    return this.runner(async (context) => {
      const scope = str(a, 'sheet') ? this.#sheet(context, a) : null; if (scope) scope.load('name');
      const coll = scope ? scope.names : context.workbook.names;
      const had = coll.getItemOrNullObject(name); had.load('isNullObject,formula');
      await context.sync();
      let was = null;
      if (!had.isNullObject) { was = had.formula; had.formula = refersTo; if (comment) had.comment = comment; }
      else { const item = coll.add(name, refersTo, comment ?? undefined); item.load('name'); }
      await context.sync(); this.#mutated();
      return this.#envelope({ name, refers_to: refersTo, scope: scope ? scope.name : 'workbook', was }, [was ? `이름 '${name}' 을 ${was} 에서 ${refersTo} 로 바꿨습니다` : `이름 '${name}' = ${refersTo} 을 정의했습니다${scope ? ` (시트 '${scope.name}' 범위)` : ''}`]);
    });
  }
  async #deleteName(a) {
    const name = String(need(a, 'name'));
    return this.runner(async (context) => {
      const scope = str(a, 'sheet') ? this.#sheet(context, a) : null;
      const coll = scope ? scope.names : context.workbook.names;
      const item = coll.getItemOrNullObject(name); item.load('isNullObject'); await context.sync();
      if (item.isNullObject) refuse(`'${name}' 이라는 이름이 없습니다 — read_names 가 목록을 줍니다`);
      item.delete(); await context.sync(); this.#mutated();
      return this.#envelope({ name, deleted: true }, [`이름 '${name}' 을 지웠습니다 — 그것을 쓰던 수식은 #NAME? 이 됩니다`]);
    });
  }
  async #addComment(a) {
    this.#need('ExcelApi', '1.10', 'add_comment');
    const text = String(need(a, 'text'));
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address,rowCount,columnCount');
      await context.sync();
      if (range.rowCount * range.columnCount !== 1) refuse('메모는 셀 하나에 답니다 — address 를 셀 하나로');
      const cell = `'${ws.name}'!${ExcelHand.#bare(range.address)}`;
      const had = await this.#commentAt(context, cell);
      let replied = false;
      if (had) { had.replies.add(text); replied = true; } else { context.workbook.comments.add(cell, text); }
      await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, replied }, [replied ? `${ws.name}!${at} 의 메모에 답글을 달았습니다` : `${ws.name}!${at} 에 메모를 달았습니다 — 「${clip(text, 40)}」`]);
    });
  }
  /** 셀의 메모, 없으면 null. `getItemByCellOrNullObject` 는 없다(실물 2026-09-06) — `getItemByCell` 은 없으면 던지므로 받아 준다. */
  async #commentAt(context, cell) {
    const c = context.workbook.comments.getItemByCell(cell); c.load('id');
    try { await context.sync(); return c; } catch (e) { if (e?.code === 'ItemNotFound') return null; throw e; }
  }
  async #resolveComment(a) {
    this.#need('ExcelApi', '1.10', 'resolve_comment');
    const del = bool(a, 'delete') ?? false; const resolved = bool(a, 'resolved') ?? true;
    if (!del) this.#need('ExcelApi', '1.11', 'resolve_comment{resolved}');
    return this.runner(async (context) => {
      const { ws, range } = this.#range(context, a); ws.load('name'); range.load('address'); await context.sync();
      const cell = `'${ws.name}'!${ExcelHand.#bare(range.address)}`;
      const c = await this.#commentAt(context, cell);
      if (!c) refuse(`${ExcelHand.#bare(range.address)} 에는 메모가 없습니다`);
      if (del) c.delete(); else c.resolved = resolved;
      await context.sync(); this.#mutated();
      const at = ExcelHand.#bare(range.address);
      return this.#envelope({ sheet: ws.name, address: at, deleted: del, resolved: del ? null : resolved }, [del ? `${ws.name}!${at} 의 메모를 지웠습니다` : `${ws.name}!${at} 의 메모를 ${resolved ? '해결로' : '미해결로'} 표시했습니다`]);
    });
  }

  // ── 그림·피벗 ──
  async #addImage(a) {
    this.#need('ExcelApi', '1.9', 'add_image');
    const b64 = str(a, 'image_base64'); if (!b64) refuse('그림 바이트가 안 왔습니다 — path 를 주면 헬퍼가 읽어 실어 줍니다');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      const shape = ws.shapes.addImage(b64); shape.load('name,width,height');
      const anchor = str(a, 'address');
      if (anchor) { const at = ws.getRange(anchor); at.load('left,top'); await context.sync(); shape.left = at.left; shape.top = at.top; }
      else { shape.left = num(a, 'left') ?? 20; shape.top = num(a, 'top') ?? 20; }
      const w = num(a, 'width'); const h = num(a, 'height');
      await context.sync();
      if (w != null && h != null) { shape.lockAspectRatio = false; shape.width = w; shape.height = h; }
      else if (w != null) { shape.lockAspectRatio = true; shape.width = w; }
      else if (h != null) { shape.lockAspectRatio = true; shape.height = h; }
      const name = str(a, 'name'); if (name) shape.name = name;
      const alt = str(a, 'alt'); shape.altTextDescription = alt ?? String(str(a, 'path') ?? '').split(/[\\/]/).pop();
      await context.sync(); this.#mutated();
      return this.#envelope({ sheet: ws.name, shape: name ?? shape.name, path: str(a, 'path'), width: shape.width, height: shape.height }, [`시트 '${ws.name}' 에 그림 '${name ?? shape.name}' 을 넣었습니다(${Math.round(shape.width)}×${Math.round(shape.height)}pt)${alt ? '' : ' — 대체 텍스트가 없어 파일 이름을 씁니다'}`]);
    });
  }
  async #addPivot(a) {
    this.#need('ExcelApi', '1.8', 'add_pivot');
    const source = String(need(a, 'source')); const dest = String(need(a, 'destination'));
    const rows = arr(a, 'rows') ?? []; const cols = arr(a, 'columns') ?? []; const values = arr(a, 'values') ?? arr(a, 'data') ?? [];
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      const at = ExcelHand.#sheetOf(source);
      const src = at ? context.workbook.worksheets.getItem(at.sheet).getRange(at.address) : ws.getRange(source);
      const target = ws.getRange(dest); target.load('address,values');
      await context.sync();
      const name = str(a, 'name') ?? `Pivot${Date.now().toString(36).slice(-4)}`;
      const pivot = ws.pivotTables.add(name, src, target);
      for (const f of rows) pivot.rowHierarchies.add(pivot.hierarchies.getItem(String(f)));
      for (const f of cols) pivot.columnHierarchies.add(pivot.hierarchies.getItem(String(f)));
      for (const v of values) {
        const field = typeof v === 'string' ? v : String(need(v, 'field')); const fn = (typeof v === 'string' ? null : str(v, 'function')) ?? 'Sum';
        const dh = pivot.dataHierarchies.add(pivot.hierarchies.getItem(field)); dh.summarizeBy = fn;
        if (typeof v !== 'string') { const nf = str(v, 'number_format'); if (nf) dh.numberFormat = nf; const nm = str(v, 'name'); if (nm) dh.name = nm; }
      }
      try { await context.sync(); } catch (e) {
        // Excel 2021(볼륨 판) 실물(2026-09-07): 같은 시트의 표에 열을 붙이거나 지운(edit_table add_columns·delete_columns) 뒤에는
        // 그 시트의 범위로 피벗을 못 만든다 — GeneralException 한 단어. resize 로 늘린 표, 다른 시트의 원본, 피벗을 먼저
        // 만든 뒤의 열 고치기는 다 된다. 코드 한 단어면 모델이 인자를 바꿔 다시 부르므로 **길을 적어 준다.**
        if (e?.code === 'GeneralException') refuse(`피벗을 못 만들었습니다(GeneralException — PivotTableCollection.add). 원본 첫 줄이 머리글인지, 대상 ${dest} 가 원본이나 다른 것과 겹치지 않는지 보세요. 이 시트의 표에 열을 붙이거나 지운 뒤라면 Excel 2021 의 버릇입니다 — 원본을 다른 시트로 복사해(copy_range) 거기서 만들거나, 피벗을 먼저 만들고 표의 열을 고치세요`);
        throw e;
      }
      this.#mutated();
      return this.#envelope({ pivot: name, sheet: ws.name, destination: ExcelHand.#bare(target.address), rows, columns: cols, values: values.length }, [`시트 '${ws.name}' ${ExcelHand.#bare(target.address)} 에 피벗 '${name}' — 행 ${rows.join('/') || '없음'}, 열 ${cols.join('/') || '없음'}, 값 ${values.length}개`]);
    });
  }
  async #refreshPivot(a) {
    this.#need('ExcelApi', '1.3', 'refresh_pivot');
    const name = str(a, 'name');
    return this.runner(async (context) => {
      const ws = this.#sheet(context, a); ws.load('name');
      if (name) { const p = ws.pivotTables.getItemOrNullObject(name); p.load('isNullObject'); await context.sync(); if (p.isNullObject) refuse(`'${name}' 이라는 피벗이 없습니다`); p.refresh(); }
      else ws.pivotTables.refreshAll();
      await context.sync();
      return this.#envelope({ sheet: ws.name, pivot: name ?? 'all' }, [name ? `피벗 '${name}' 을 새로 고쳤습니다` : `시트 '${ws.name}' 의 피벗을 전부 새로 고쳤습니다`]);
    });
  }

  // ── 되돌리기·기록·제안 ──
  async #restore(a) {
    const id = String(need(a, 'snapshot')); const snap = this.snapshots.get(id);
    if (!snap) refuse(`그런 스냅숏이 없습니다: ${id} — snapshot_range 가 준 id 를 주세요(이 창이 뜬 뒤 찍은 것만 압니다)`);
    return this.runner(async (context) => {
      const ws = context.workbook.worksheets.getItem(snap.sheet); const range = ws.getRange(snap.address);
      range.formulas = snap.formulas; range.numberFormat = snap.numberFormat;
      await context.sync(); this.#mutated();
      return this.#envelope({ snapshot: id, sheet: snap.sheet, address: snap.address }, [`${snap.sheet}!${snap.address} 를 스냅숏 ${id} 로 되돌렸습니다`]);
    });
  }
  async #setTag(a) {
    this.#need('ExcelApi', '1.4', 'set_tag');
    const key = String(need(a, 'key')).trim(); const value = str(a, 'value');
    if (key.startsWith(FIX_PREFIX) || key === BOOK_SETTING_KEY) refuse(`'${key}' 는 이 창이 쓰는 이름이라 메모로 못 씁니다`);
    return this.runner(async (context) => {
      const settings = context.workbook.settings;
      if (value == null) { const item = settings.getItemOrNullObject(key); item.load('isNullObject'); await context.sync(); if (!item.isNullObject) item.delete(); await context.sync(); this.#mutated(); return this.#envelope({ key, removed: !item.isNullObject }, [item.isNullObject ? `메모 '${key}' 는 원래 없었습니다` : `메모 '${key}' 를 지웠습니다`]); }
      settings.add(key, value); await context.sync(); this.#mutated();
      return this.#envelope({ key, value }, [`메모 '${key}' 를 남겼습니다 — 「${clip(value, 40)}」`]);
    });
  }
  async #suggest(a) {
    this.#need('ExcelApi', '1.4', 'suggest');
    const what = String(need(a, 'what')).trim(); const why = str(a, 'why'); const fix = a.fix && typeof a.fix === 'object' ? a.fix : null;
    if (fix && !FIX_TOOLS.includes(String(fix.tool))) refuse(`제안으로 누를 수 있는 손이 아닙니다 — '${fix.tool}'. 누를 수 있는 것: ${FIX_TOOLS.join(', ')}`);
    const sheet = str(a, 'sheet'); const address = str(a, 'address');
    return this.runner(async (context) => {
      const settings = context.workbook.settings; settings.load('items/key'); await context.sync();
      const taken = new Set(settings.items.map((s) => s.key));
      const seed = FIX_PREFIX + `${Date.now().toString(36)}${Math.floor(Math.random() * 1e6).toString(36)}`.toUpperCase();
      let key = seed; for (let n = 1; taken.has(key); n += 1) key = `${seed}-${n}`;
      const body = { what, sheet, address }; if (why) body.why = why; if (fix) body.fix = { tool: String(fix.tool), args: fix.args ?? {} };
      settings.add(key, JSON.stringify(body)); await context.sync(); this.#mutated();
      return this.#envelope({ suggestion: key, sheet, address, fixable: Boolean(fix) }, [`${sheet ? `${sheet}${address ? `!${address}` : ''}` : '통합 문서'} 에 제안을 붙였습니다 — ${what}. **이건 아직 안 고친 것입니다** — 작업창의 「적용」을 누르기 전까지 통합 문서는 그대로입니다`]);
    });
  }
  async #dropSuggestion(a) {
    const key = String(need(a, 'key')).trim();
    if (!key.startsWith(FIX_PREFIX)) refuse(`제안의 키가 아닙니다 — '${key}'. set_tag 로 남긴 메모는 set_tag 로 지우세요`);
    return this.runner(async (context) => {
      const item = context.workbook.settings.getItemOrNullObject(key); item.load('isNullObject'); await context.sync();
      if (item.isNullObject) refuse(`그런 제안이 없습니다 — ${key}`);
      item.delete(); await context.sync(); this.#mutated();
      return this.#envelope({ dropped: key }, [`제안 ${key} 를 뗐습니다 — 고치지는 않았습니다`]);
    });
  }
}

/** 제안 설정값(JSON)을 카드 한 줄로. 못 읽으면 broken. */
export function decodeSuggestion(key, value) {
  const base = { key, sheet: null, address: null };
  let body = null;
  try { body = JSON.parse(String(value)); } catch { body = null; }
  if (!body || typeof body !== 'object' || typeof body.what !== 'string' || !body.what.trim()) {
    return { ...base, what: '읽을 수 없는 제안입니다', why: '', fix: null, broken: true, does: '', appliable: false };
  }
  const fix = body.fix && typeof body.fix === 'object' && body.fix.tool ? { tool: String(body.fix.tool), args: body.fix.args ?? {} } : null;
  const does = fix ? (FIX_TOOLS.includes(fix.tool) ? `${fix.tool} 을 부릅니다` : `'${fix.tool}' 은 제안으로 누를 수 없습니다`) : '고칠 손이 안 달렸습니다 — 읽고 직접 고치세요';
  return { ...base, sheet: body.sheet ?? null, address: body.address ?? null, what: body.what.trim(), why: typeof body.why === 'string' ? body.why.trim() : '', fix, broken: false, does, appliable: Boolean(fix && FIX_TOOLS.includes(fix.tool)) };
}

/** A1 주소 안의 (r, c) 칸 이름 — 시트 이름이 붙어 있어도 뗀다. */
export function cellAt(address, r, c) {
  const bare = String(address).includes('!') ? String(address).split('!').pop() : String(address);
  const m = /^\$?([A-Za-z]+)\$?(\d+)/.exec(bare);
  if (!m) return `${bare}[${r},${c}]`;
  const col = (s) => { let n = 0; for (const ch of s.toUpperCase()) n = n * 26 + (ch.charCodeAt(0) - 64); return n - 1; };
  const name = (i) => { let n = i + 1; let s = ''; while (n > 0) { const x = (n - 1) % 26; s = String.fromCharCode(65 + x) + s; n = Math.floor((n - 1) / 26); } return s; };
  return `${name(col(m[1]) + c)}${Number(m[2]) + r}`;
}
