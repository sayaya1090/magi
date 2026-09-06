import { HandPort } from '../port/HandPort.js';
import {
  ALL_OPS, FIX_TOOLS, FIX_PREFIX, BOOK_SETTING_KEY, refuse, str, num, int, bool, arr, need, grid, hex,
  chartTypeOf, CHART_KO, envelope, clip, isFormula, nowEpoch,
} from './handCore.js';
import { parseAddress, cellName, rangeName } from './a1.js';
import { decodeSuggestion } from './ExcelHand.js';

/**
 * 가짜 손 — 메모리 통합 문서(ui/bookFixture.js 꼴) 위에서 도구 61개를 **정말로** 돌린다.
 *
 * 시험과 브라우저 목업이 쓴다. 진짜 손(ExcelHand)과 같은 봉투·같은 거절 규칙이라, 화면 코드와 헬퍼 왕복은
 * 여기로 잰다. 다만 셀 수식은 계산하지 않는다(값은 적힌 대로) — 그것은 Excel 의 일이다.
 * 진짜가 못 하는 것을 여기서 되게 만들지 않는다: 요구 집합 거절(`supports`)도 같은 문을 지난다.
 */
export class FakeHand extends HandPort {
  constructor(model, { document = 'book-fake', label = 'fake.xlsx', supports } = {}) {
    super();
    this.model = model;
    this.model.settings ??= {};
    for (const s of this.model.sheets) { s.cells ??= {}; s.tables ??= []; s.charts ??= []; s.names ??= []; s.comments ??= []; s.cf ??= []; s.validation ??= {}; s.frozen ??= null; s.merged ??= []; s.visibility ??= 'Visible'; s.pivots ??= []; s.images ??= []; }
    this.document = document;
    this.labelText = label;
    this.supports = supports ?? (() => true);
    this.epoch = nowEpoch();
    this.count = 0;
    this.snapshots = new Map();
    this.calls = [];
  }
  get label() { return this.labelText; }
  ops() { return [...ALL_OPS]; }
  #mutated() { this.count += 1; }
  #env(result, changed = []) { return envelope(this, result, changed); }
  #need(name, version, what) { if (!this.supports(name, version)) refuse(`${what} 은 ${name} ${version} 이 필요한데 이 호스트에는 없습니다`); }

  // ── 자리 ──
  #sheet(a, key = 'sheet') {
    const s = str(a, key) ?? str(a, 'worksheet');
    if (s == null || s === '') return this.model.sheets.find((x) => x.name === (this.model.active ?? this.model.sheets[0].name)) ?? this.model.sheets[0];
    if (/^\d+$/.test(s)) { const i = Number(s) - 1; if (!this.model.sheets[i]) refuse(`탭이 ${this.model.sheets.length}개라 ${s}번 시트는 없습니다`); return this.model.sheets[i]; }
    const got = this.model.sheets.find((x) => x.name === s);
    return got ?? refuse(`'${s}' 이라는 시트가 없습니다 — 있는 것: ${this.model.sheets.map((x) => x.name).join(', ')}`);
  }
  #used(sheet) {
    const keys = Object.keys(sheet.cells).filter((k) => sheet.cells[k] && (sheet.cells[k].v !== '' && sheet.cells[k].v != null || sheet.cells[k].f));
    if (keys.length === 0) return null;
    let top = 1e9; let left = 1e9; let bottom = -1; let right = -1;
    for (const k of keys) { const { top: r, left: c } = parseAddress(k); top = Math.min(top, r); left = Math.min(left, c); bottom = Math.max(bottom, r); right = Math.max(right, c); }
    return { top, left, rows: bottom - top + 1, cols: right - left + 1, address: rangeName(top, left, bottom - top + 1, right - left + 1) };
  }
  #rangeOf(a, { must = true } = {}) {
    const sheet = this.#sheet(a);
    const address = str(a, 'address') ?? str(a, 'range');
    if (!address) {
      if (must) refuse('address 가 없습니다 — "B2:E9" 같은 A1 주소');
      const u = this.#used(sheet);
      return { sheet, box: u, address: u?.address ?? null, used: true };
    }
    if (address.includes('!')) refuse(`address 에 시트 이름을 넣지 마세요(${address}) — sheet 인자로 주세요`);
    const box = parseAddress(address);
    return { sheet, box, address: rangeName(box.top, box.left, box.rows, box.cols), used: false };
  }
  #cell(sheet, r, c) { return sheet.cells[cellName(r, c)] ?? null; }
  #put(sheet, r, c, patch) { const k = cellName(r, c); sheet.cells[k] = { ...(sheet.cells[k] ?? {}), ...patch }; }
  #values(sheet, box) {
    const out = [];
    for (let r = 0; r < box.rows; r += 1) { const row = []; for (let c = 0; c < box.cols; c += 1) row.push(this.#cell(sheet, box.top + r, box.left + c)?.v ?? ''); out.push(row); }
    return out;
  }

  async run(op, args = {}) {
    this.calls.push({ op, args });
    switch (op) {
      case 'list_sheets': {
        const active = this.model.active ?? this.model.sheets[0].name;
        const sheets = this.model.sheets.map((s, i) => { const u = this.#used(s); return { index: i + 1, name: s.name, visibility: s.visibility, active: s.name === active, used_range: u?.address ?? null, rows: u?.rows ?? 0, columns: u?.cols ?? 0, tables: s.tables.length, charts: s.charts.length, pivots: s.pivots.length }; });
        return this.#env({ sheets, count: sheets.length, active });
      }
      case 'describe_sheet': {
        const s = this.#sheet(a(args)); const u = this.#used(s);
        const header = u ? { values: this.#values(s, { ...u, rows: 1 })[0], bold: Boolean(this.#cell(s, u.top, u.left)?.bold), font: this.#cell(s, u.top, u.left)?.font ?? 'Calibri', size: 11, color: '#000000', fill: this.#cell(s, u.top, u.left)?.fill ?? '#FFFFFF' } : null;
        return this.#env({ sheet: s.name, index: this.model.sheets.indexOf(s) + 1, visibility: s.visibility, used_range: u?.address ?? null, rows: u?.rows ?? 0, columns: u?.cols ?? 0, frozen: s.frozen, merged: s.merged, tables: s.tables.map((t) => ({ name: t.name, address: t.address, rows: t.rows, headers: t.headers, style: t.style ?? null })), charts: s.charts.map((c) => ({ name: c.name, type: c.type, title: c.title ?? '', series: c.series ?? [], left: c.left, top: c.top, width: c.width, height: c.height })), pivots: s.pivots.map((p) => p.name), names: [...this.model.names ?? [], ...s.names].map((n) => ({ name: n.name, refers_to: n.refers_to, type: 'Range' })), header });
      }
      case 'read_range': {
        const { sheet, box, address, used } = this.#rangeOf(args, { must: false });
        if (used && !box) return this.#env({ sheet: sheet.name, address: null, rows: 0, columns: 0, values: [], note: '이 시트는 비어 있습니다' });
        const maxRows = int(args, 'max_rows') ?? 200; const maxCols = int(args, 'max_cols') ?? 30;
        const rows = Math.min(box.rows, maxRows); const cols = Math.min(box.cols, maxCols);
        const part = { top: box.top, left: box.left, rows, cols };
        const values = this.#values(sheet, part); const formulas = {}; const formats = {};
        for (let r = 0; r < rows; r += 1) for (let c = 0; c < cols; c += 1) { const cell = this.#cell(sheet, box.top + r, box.left + c); if (cell?.f) formulas[cellName(box.top + r, box.left + c)] = cell.f; if (cell?.nf && cell.nf !== 'General') formats[cellName(box.top + r, box.left + c)] = cell.nf; }
        const truncated = rows < box.rows || cols < box.cols;
        return this.#env({ sheet: sheet.name, address: rangeName(box.top, box.left, rows, cols), rows, columns: cols, total_rows: box.rows, total_columns: box.cols, truncated, values, formulas: (bool(args, 'formulas') ?? true) ? formulas : {}, number_formats: formats, ...(truncated ? { note: `범위가 ${box.rows}×${box.cols} 인데 ${rows}×${cols} 만 읽었습니다 — max_rows/max_cols 를 올리거나 필요한 부분만 address 로 부르세요` } : {}) });
      }
      case 'find': {
        this.#need('ExcelApi', '1.9', 'find');
        const text = String(need(args, 'text')); const limit = int(args, 'limit') ?? 50; const mc = bool(args, 'match_case') ?? false; const whole = bool(args, 'whole_cell') ?? false; const inF = bool(args, 'in_formulas') ?? false;
        const sheets = str(args, 'sheet') ? [this.#sheet(args)] : this.model.sheets;
        const hits = [];
        for (const s of sheets) for (const [k, cell] of Object.entries(s.cells)) {
          const hay = inF ? (cell.f ?? '') : String(cell.v ?? '');
          const ok = whole ? (mc ? hay === text : hay.toLowerCase() === text.toLowerCase()) : (mc ? hay.includes(text) : hay.toLowerCase().includes(text.toLowerCase()));
          if (ok) hits.push(inF ? { sheet: s.name, address: k, formula: cell.f } : { sheet: s.name, address: k, value: cell.v });
        }
        return this.#env({ hits: hits.slice(0, limit), matched: hits.length, searched_sheets: sheets.map((s) => s.name), ...(hits.length > limit ? { note: `앞 ${limit}개만 실었습니다` } : {}) });
      }
      case 'read_table': {
        const { sheet, t } = this.#table(args); const maxRows = int(args, 'max_rows') ?? 200;
        const box = parseAddress(t.address); const body = { top: box.top + 1, left: box.left, rows: box.rows - 1, cols: box.cols };
        const all = body.rows > 0 ? this.#values(sheet, body) : [];
        return this.#env({ table: t.name, sheet: sheet.name, address: t.address, headers: t.headers, rows: all.slice(0, maxRows), row_count: all.length, truncated: all.length > maxRows, style: t.style ?? null });
      }
      case 'read_chart': { const { sheet, c } = this.#chart(args); return this.#env({ chart: c.name, sheet: sheet.name, type: c.type, type_ko: CHART_KO.get(c.type) ?? c.type, title: c.title ?? '', legend: c.legend ?? 'Right', x_title: c.x_title ?? '', y_title: c.y_title ?? '', series: c.series ?? [], left: c.left, top: c.top, width: c.width, height: c.height }); }
      // **가짜는 픽셀을 지어내지 않는다.** 없는 증거를 있는 척하는 것이 이 제품이 제일 피하는 것이다 —
      // 그림은 진짜 Excel(Range.getImage·Chart.getImage)만 준다.
      case 'render_range': { this.#need('ExcelApi', '1.7', 'render_range'); this.#rangeOf(args, { must: false }); refuse('가짜 손은 그림을 못 그립니다 — 진짜 Excel 에서만 render_range 가 됩니다'); }
      case 'render_chart': { this.#chart(args); refuse('가짜 손은 그림을 못 그립니다 — 진짜 Excel 에서만 render_chart 가 됩니다'); }
      case 'read_comments': {
        this.#need('ExcelApi', '1.10', 'read_comments');
        const sheets = str(args, 'sheet') ? [this.#sheet(args)] : this.model.sheets;
        const rows = sheets.flatMap((s) => s.comments.map((c) => ({ id: c.id, sheet: s.name, address: c.address, author: c.author ?? 'me', text: c.text, resolved: c.resolved ?? false, replies: c.replies ?? [] })));
        return this.#env({ comments: rows, count: rows.length });
      }
      case 'read_names': {
        const items = (this.model.names ?? []).map((n) => ({ ...n, scope: 'workbook' }));
        if (str(args, 'sheet')) { const s = this.#sheet(args); items.push(...s.names.map((n) => ({ ...n, scope: s.name }))); }
        return this.#env({ names: items.map((n) => ({ name: n.name, refers_to: n.refers_to, type: 'Range', value: n.value ?? null, scope: n.scope })), count: items.length });
      }
      case 'read_validation': { this.#need('ExcelApi', '1.8', 'read_validation'); const { sheet, address } = this.#rangeOf(args); const v = sheet.validation[address] ?? null; return this.#env({ sheet: sheet.name, address, type: v?.kind ?? 'None', rule: v?.rule ?? null, prompt: v?.prompt ?? null, error: v?.error ?? null, valid: true }); }
      case 'read_conditional_formats': { const { sheet, address, used, box } = this.#rangeOf(args, { must: false }); const list = used ? sheet.cf : sheet.cf.filter((f) => overlaps(parseAddress(f.address), box)); return this.#env({ sheet: sheet.name, formats: list.map((f, i) => ({ id: f.id, type: f.kind, priority: i, address: f.address })), count: list.length }); }
      case 'describe_style': {
        const rows = this.model.sheets.map((s) => { const u = this.#used(s); if (!u) return null; const h = this.#cell(s, u.top, u.left); return { sheet: s.name, header: { font: h?.font ?? 'Calibri', size: 11, bold: Boolean(h?.bold), color: h?.color ?? '#000000', fill: h?.fill ?? '#FFFFFF' }, body: null }; }).filter(Boolean);
        return this.#env({ sheets: rows, seen: rows.length, summary: rows.length ? { header_fill: rows[0].header.fill, header_bold: rows[0].header.bold, font: rows[0].header.font, size: 11 } : null, read: true, note: rows.length ? '새 시트는 이 버릇을 따르면 어울립니다' : '이 통합 문서에는 따라갈 서식이 없습니다 — 기본 서식으로 짓습니다' });
      }
      case 'snapshot_range': {
        const { sheet, box, address } = this.#rangeOf(args);
        const id = `snap-${this.snapshots.size + 1}`; const cells = {};
        for (let r = 0; r < box.rows; r += 1) for (let c = 0; c < box.cols; c += 1) { const k = cellName(box.top + r, box.left + c); cells[k] = sheet.cells[k] ? { ...sheet.cells[k] } : null; }
        this.snapshots.set(id, { sheet: sheet.name, address, cells });
        return this.#env({ snapshot: id, sheet: sheet.name, address, cells: box.rows * box.cols });
      }
      case 'read_tags': { const tags = Object.entries(this.model.settings).filter(([k]) => !k.startsWith(FIX_PREFIX) && k !== BOOK_SETTING_KEY).map(([key, value]) => ({ key, value })); return this.#env({ tags, count: tags.length }); }
      case 'read_suggestions': { const only = str(args, 'sheet'); const rows = Object.entries(this.model.settings).filter(([k]) => k.startsWith(FIX_PREFIX)).map(([k, v]) => decodeSuggestion(k, v)).filter((r) => !only || !r.sheet || r.sheet === only); return this.#env({ scope: only ?? null, count: rows.length, suggestions: rows }); }
      case 'advise': case 'clear_advice': return this.#env({ pinned: op === 'advise' ? (args.items?.length ?? 0) : 0 });

      // ── 쓰기 ──
      case 'write_range': {
        const values = grid(args, 'values'); const formulas = grid(args, 'formulas');
        if (!values && !formulas) refuse('values 나 formulas 가 있어야 합니다 — 2차원 배열');
        if (values && formulas && (values.rows !== formulas.rows || values.cols !== formulas.cols)) refuse(`values(${values.rows}×${values.cols}) 와 formulas(${formulas.rows}×${formulas.cols}) 의 모양이 다릅니다`);
        const shape = values ?? formulas; const sheet = this.#sheet(args); const address = String(need(args, 'address'));
        let box = parseAddress(address);
        if (box.rows === 1 && box.cols === 1 && (shape.rows > 1 || shape.cols > 1)) box = { ...box, rows: shape.rows, cols: shape.cols };
        if (box.rows !== shape.rows || box.cols !== shape.cols) refuse(`배열은 ${shape.rows}×${shape.cols} 인데 ${address} 는 ${box.rows}×${box.cols} 입니다 — 왼쪽 위 셀 하나만 주면 배열 크기로 잡습니다`);
        let overwrote = 0; let fcount = 0; const nf = str(args, 'number_format');
        for (let r = 0; r < box.rows; r += 1) for (let c = 0; c < box.cols; c += 1) {
          const had = this.#cell(sheet, box.top + r, box.left + c); if (had && had.v !== '' && had.v != null) overwrote += 1;
          const f = formulas?.cells[r][c]; const v = values?.cells[r][c];
          const raw = f != null && f !== '' ? f : (v == null ? '' : v);
          const patch = isFormula(raw) ? { f: raw, v: raw } : { v: raw, f: undefined };
          if (isFormula(raw)) fcount += 1;
          if (nf) patch.nf = nf;
          this.#put(sheet, box.top + r, box.left + c, patch);
        }
        this.#mutated();
        const at = rangeName(box.top, box.left, box.rows, box.cols);
        return this.#env({ sheet: sheet.name, address: at, rows: box.rows, columns: box.cols, overwrote, formulas: fcount, number_format: nf ?? null }, [`${sheet.name}!${at} 에 ${box.rows}×${box.cols} 을 썼습니다` + (fcount ? ` (수식 ${fcount}개)` : '') + (overwrote ? ` — ⚠ 값이 있던 셀 ${overwrote}개를 덮어썼습니다` : '') + (nf ? ` · 표시 형식 ${nf}` : '')]);
      }
      case 'set_number_format': { const fmt = String(need(args, 'format', 'format')); const { sheet, box, address } = this.#rangeOf(args); this.#each(sheet, box, (r, c) => this.#put(sheet, r, c, { nf: fmt })); this.#mutated(); return this.#env({ sheet: sheet.name, address, format: fmt }, [`${sheet.name}!${address} 표시 형식 → ${fmt}`]); }
      case 'format_range': {
        const said = []; const patch = {};
        const font = str(args, 'font'); if (font) { patch.font = font; said.push(`글꼴 ${font}`); }
        const size = num(args, 'size'); if (size != null) { patch.size = size; said.push(`크기 ${size}`); }
        const b = bool(args, 'bold'); if (b != null) { patch.bold = b; said.push(b ? '굵게' : '굵게 해제'); }
        const i = bool(args, 'italic'); if (i != null) { patch.italic = i; said.push(i ? '기울임' : '기울임 해제'); }
        const color = hex(args, 'color'); if (color) { patch.color = color; said.push(`글자색 ${color}`); }
        const fill = hex(args, 'fill', true); if (fill) { patch.fill = fill === 'none' ? null : fill; said.push(fill === 'none' ? '채우기 없음' : `채우기 ${fill}`); }
        const align = str(args, 'align'); if (align) { patch.align = align; said.push(`정렬 ${align}`); }
        const borders = hex(args, 'borders', true); if (borders) { patch.borders = borders; said.push(borders === 'none' ? '테두리 없음' : `테두리 ${borders}`); }
        const cw = num(args, 'column_width'); if (cw != null) said.push(`열 너비 ${cw}`);
        if (bool(args, 'wrap') != null) said.push('줄바꿈');
        if (said.length === 0) refuse('바꿀 것이 하나도 안 왔습니다 — font, size, bold, color, fill, align, borders, column_width … 중 하나는 주세요');
        const { sheet, box, address } = this.#rangeOf(args); this.#each(sheet, box, (r, c) => this.#put(sheet, r, c, patch)); this.#mutated();
        return this.#env({ sheet: sheet.name, address, changed: said.length }, [`${sheet.name}!${address}: ${said.join(', ')}`]);
      }
      case 'replace_all': {
        this.#need('ExcelApi', '1.9', 'replace_all');
        const find = String(need(args, 'find')); const replace = String(args.replace ?? refuse('replace 가 없습니다(빈 문자열은 됩니다)')); const mc = bool(args, 'match_case') ?? false; const whole = bool(args, 'whole_cell') ?? false;
        const sheets = str(args, 'sheet') ? [this.#sheet(args)] : this.model.sheets; const per = [];
        for (const s of sheets) { let n = 0; for (const cell of Object.values(s.cells)) { if (cell?.f || typeof cell?.v !== 'string') continue; const hit = whole ? (mc ? cell.v === find : cell.v.toLowerCase() === find.toLowerCase()) : (mc ? cell.v.includes(find) : cell.v.toLowerCase().includes(find.toLowerCase())); if (!hit) continue; cell.v = whole ? replace : cell.v.replace(new RegExp(find.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), mc ? 'g' : 'gi'), replace); n += 1; } if (n) per.push({ sheet: s.name, cells: n }); }
        const total = per.reduce((x, y) => x + y.cells, 0);
        if (total === 0) refuse(`「${clip(find, 40)}」 가 ${str(args, 'sheet') ? `시트 ${sheets[0].name}` : '통합 문서'} 에 없습니다 — 바꾼 것이 없습니다`);
        this.#mutated(); return this.#env({ find, replace, cells: total, sheets: per }, [`「${clip(find, 30)}」 → 「${clip(replace, 30)}」 셀 ${total}개 (${per.map((x) => `${x.sheet} ${x.cells}`).join(', ')})`]);
      }
      case 'copy_range': {
        this.#need('ExcelApi', '1.9', 'copy_range');
        const source = String(need(args, 'source')); const mode = (str(args, 'mode') ?? 'all').toLowerCase(); const transpose = bool(args, 'transpose') ?? false;
        if (!['all', 'values', 'formulas', 'formats'].includes(mode)) refuse(`mode 는 all, values, formulas, formats 중 하나 — '${mode}'`);
        const dest = this.#sheet(args); const srcSheet = this.#sheet(args, str(args, 'source_sheet') ? 'source_sheet' : 'sheet'); const sb = parseAddress(source); const db = parseAddress(String(need(args, 'address')));
        const rows = transpose ? sb.cols : sb.rows; const cols = transpose ? sb.rows : sb.cols;
        for (let r = 0; r < sb.rows; r += 1) for (let c = 0; c < sb.cols; c += 1) { const from = this.#cell(srcSheet, sb.top + r, sb.left + c) ?? {}; const tr = transpose ? c : r; const tc = transpose ? r : c; const k = cellName(db.top + tr, db.left + tc); const had = dest.cells[k] ?? {}; const next = mode === 'formats' ? { ...had, nf: from.nf, fmt: from.fmt } : mode === 'values' ? { ...had, v: from.v ?? '', f: undefined } : mode === 'formulas' ? { ...had, v: from.f ?? from.v ?? '', f: from.f } : { ...from }; dest.cells[k] = next; }
        const at = rangeName(db.top, db.left, rows, cols); this.#mutated();
        return this.#env({ sheet: dest.name, source: `${srcSheet.name}!${source}`, address: at, mode, transpose }, [`${srcSheet.name}!${source} → ${dest.name}!${at} (${mode}${transpose ? ', 행열 바꿈' : ''})`]);
      }
      case 'fill_range': {
        this.#need('ExcelApi', '1.9', 'fill_range');
        const to = String(need(args, 'to')); const fill = (str(args, 'fill') ?? 'default').toLowerCase(); if (!['default', 'copy', 'series', 'formats', 'values'].includes(fill)) refuse(`fill 은 default, copy, series, formats, values 중 하나 — '${fill}'`);
        const { sheet, box, address } = this.#rangeOf(args); const tb = parseAddress(to);
        if (tb.top > box.top || tb.left > box.left || tb.top + tb.rows < box.top + box.rows || tb.left + tb.cols < box.left + box.cols) refuse(`to(${to}) 는 씨앗(${address})을 포함해야 합니다`);
        const seeds = []; for (let r = 0; r < box.rows; r += 1) for (let c = 0; c < box.cols; c += 1) seeds.push(this.#cell(sheet, box.top + r, box.left + c) ?? { v: '' });
        const down = tb.rows > box.rows; const n = down ? box.rows : box.cols; const numeric = seeds.every((s) => !s.f && typeof s.v === 'number');
        const step = numeric && seeds.length >= 2 ? seeds[1].v - seeds[0].v : (numeric ? 1 : 0);
        let filled = 0;
        for (let r = 0; r < tb.rows; r += 1) for (let c = 0; c < tb.cols; c += 1) {
          const rr = tb.top + r; const cc = tb.left + c; if (rr >= box.top && rr < box.top + box.rows && cc >= box.left && cc < box.left + box.cols) continue;
          const i = down ? (r % n) : (c % n); const seed = down ? seeds[i * box.cols + (cc - box.left)] : seeds[(rr - box.top) * box.cols + i]; const dist = down ? r : c; const series = numeric && fill !== 'copy' && fill !== 'formats';
          const v = series ? seed.v + step * Math.floor(dist / n) * n + (fill === 'series' || fill === 'default' ? 0 : 0) : seed.v;
          sheet.cells[cellName(rr, cc)] = fill === 'formats' ? { ...(sheet.cells[cellName(rr, cc)] ?? {}), nf: seed.nf, fmt: seed.fmt } : { ...seed, v: series ? seed.v + step * dist : v }; filled += 1;
        }
        this.#mutated(); return this.#env({ sheet: sheet.name, address, to, fill, filled }, [`${sheet.name}!${address} 를 ${to} 까지 채웠습니다 (${fill})`]);
      }
      case 'remove_duplicates': {
        this.#need('ExcelApi', '1.9', 'remove_duplicates');
        const cols = arr(args, 'columns'); const header = bool(args, 'has_header') ?? true; const { sheet, box, address } = this.#rangeOf(args);
        const which = cols && cols.length ? cols.map((v) => int({ v }, 'v')) : Array.from({ length: box.cols }, (_, i) => i);
        for (const c of which) if (c == null || c < 0 || c >= box.cols) refuse(`columns 가 블록 밖입니다 — ${c} (0부터 ${box.cols - 1}까지)`);
        const rows = []; for (let r = 0; r < box.rows; r += 1) rows.push(Array.from({ length: box.cols }, (_, c) => this.#cell(sheet, box.top + r, box.left + c) ?? { v: '' }));
        const start = header ? 1 : 0; const seen = new Set(); const kept = rows.slice(0, start); let removed = 0;
        for (const row of rows.slice(start)) { const key = JSON.stringify(which.map((c) => row[c].v)); if (seen.has(key)) { removed += 1; continue; } seen.add(key); kept.push(row); }
        for (let r = 0; r < box.rows; r += 1) for (let c = 0; c < box.cols; c += 1) { const k = cellName(box.top + r, box.left + c); if (r < kept.length) sheet.cells[k] = { ...kept[r][c] }; else delete sheet.cells[k]; }
        this.#mutated(); return this.#env({ sheet: sheet.name, address, removed, remaining: kept.length - start }, [`${sheet.name}!${address}: 중복 ${removed}행 제거, ${kept.length - start}행 남음`]);
      }
      case 'clear_range': {
        const what = (str(args, 'what') ?? 'all').toLowerCase(); if (!['all', 'contents', 'formats', 'hyperlinks'].includes(what)) refuse(`what 는 all, contents, formats, hyperlinks 중 하나입니다 — '${what}'`);
        const { sheet, box, address } = this.#rangeOf(args); let had = 0;
        this.#each(sheet, box, (r, c) => { const k = cellName(r, c); const cell = sheet.cells[k]; if (!cell) return; if (cell.v !== '' && cell.v != null) had += 1; if (what === 'all') delete sheet.cells[k]; else if (what === 'contents') { delete cell.v; delete cell.f; } else if (what === 'formats') { delete cell.nf; delete cell.bold; delete cell.fill; delete cell.color; } else delete cell.link; });
        this.#mutated();
        return this.#env({ sheet: sheet.name, address, what, had_values: had }, [`${sheet.name}!${address} 를 지웠습니다(${what})` + (had && what !== 'formats' && what !== 'hyperlinks' ? ` — 값이 있던 셀 ${had}개` : '')]);
      }
      case 'merge_cells': { const { sheet, address } = this.#rangeOf(args); sheet.merged.push(address); this.#mutated(); return this.#env({ sheet: sheet.name, address, merged: true }, [`${sheet.name}!${address} 를 병합했습니다`]); }
      case 'unmerge_cells': { const { sheet, address, box } = this.#rangeOf(args); sheet.merged = sheet.merged.filter((m) => !overlaps(parseAddress(m), box)); this.#mutated(); return this.#env({ sheet: sheet.name, address, merged: false }, [`${sheet.name}!${address} 의 병합을 풀었습니다`]); }
      case 'insert_cells': case 'delete_cells': {
        const insert = op === 'insert_cells'; const shift = (str(args, 'shift') ?? (insert ? 'down' : 'up')).toLowerCase();
        if (insert ? !['down', 'right'].includes(shift) : !['up', 'left'].includes(shift)) refuse(insert ? `shift 는 down 또는 right 입니다 — '${shift}'` : `shift 는 up 또는 left 입니다 — '${shift}'`);
        const { sheet, box, address } = this.#rangeOf(args);
        const had = this.#values(sheet, box).flat().filter((v) => v !== '' && v != null).length;
        const vertical = shift === 'down' || shift === 'up';
        const moved = {};
        for (const [k, cell] of Object.entries(sheet.cells)) {
          const p = parseAddress(k);
          const inBand = vertical ? (p.left >= box.left && p.left < box.left + box.cols) : (p.top >= box.top && p.top < box.top + box.rows);
          if (!inBand) { moved[k] = cell; continue; }
          const pos = vertical ? p.top : p.left; const start = vertical ? box.top : box.left; const n = vertical ? box.rows : box.cols;
          if (insert) { if (pos >= start) moved[vertical ? cellName(p.top + n, p.left) : cellName(p.top, p.left + n)] = cell; else moved[k] = cell; }
          else { if (pos >= start + n) moved[vertical ? cellName(p.top - n, p.left) : cellName(p.top, p.left - n)] = cell; else if (pos < start) moved[k] = cell; }
        }
        sheet.cells = moved; this.#mutated();
        return this.#env({ sheet: sheet.name, address, shift, cells: box.rows * box.cols, had_values: insert ? null : had }, [insert ? `${sheet.name}!${address} 자리에 빈 셀을 끼워 넣었습니다(있던 셀은 ${shift === 'down' ? '아래' : '오른쪽'}으로)` : `${sheet.name}!${address} 를 삭제했습니다(나머지는 ${shift === 'up' ? '위' : '왼쪽'}으로)` + (had ? ` — 값이 있던 셀 ${had}개` : '')]);
      }
      case 'autofit': { const what = (str(args, 'what') ?? 'columns').toLowerCase(); if (!['columns', 'rows', 'both'].includes(what)) refuse(`what 는 columns, rows, both 중 하나입니다 — '${what}'`); const { sheet, box, address, used } = this.#rangeOf(args, { must: false }); if (used && !box) refuse('이 시트는 비어 있어 맞출 것이 없습니다'); this.#mutated(); return this.#env({ sheet: sheet.name, address, what }, [`${sheet.name}!${address} 의 ${what === 'rows' ? '행 높이' : what === 'both' ? '열 너비와 행 높이' : '열 너비'}를 맞췄습니다`]); }
      case 'set_hyperlink': {
        this.#need('ExcelApi', '1.7', 'set_hyperlink'); const { sheet, box, address } = this.#rangeOf(args); const url = str(args, 'url'); const ts = str(args, 'target_sheet');
        if (!url && !ts) { this.#each(sheet, box, (r, c) => { const cell = this.#cell(sheet, r, c); if (cell) delete cell.link; }); this.#mutated(); return this.#env({ sheet: sheet.name, address, removed: true }, [`${sheet.name}!${address} 의 링크를 뗐습니다`]); }
        const target = url ?? `'${ts}'!${str(args, 'target_address') ?? 'A1'}`; const text = str(args, 'text');
        this.#each(sheet, box, (r, c) => this.#put(sheet, r, c, { link: target, ...(text ? { v: text } : {}) })); this.#mutated();
        return this.#env({ sheet: sheet.name, address, url: url ?? null, target: url ? null : target }, [`${sheet.name}!${address} 에 링크 → ${target}`]);
      }
      case 'add_sheet': {
        const name = str(args, 'name'); if (name != null && (name.length > 31 || /[:\\/?*\[\]]/.test(name))) refuse(`시트 이름은 31자 이하이고 : \\ / ? * [ ] 를 못 씁니다 — '${name}'`);
        if (name && this.model.sheets.some((s) => s.name === name)) refuse(`'${name}' 시트가 이미 있습니다 — 다른 이름을 주거나 그 시트를 쓰세요`);
        const made = { name: name ?? `Sheet${this.model.sheets.length + 1}`, cells: {}, tables: [], charts: [], names: [], comments: [], cf: [], validation: {}, frozen: null, merged: [], visibility: 'Visible', pivots: [], images: [] };
        const after = str(args, 'after'); let idx = this.model.sheets.length;
        if (after) { const i = this.model.sheets.findIndex((s) => s.name === after); if (i < 0) refuse(`'${after}' 이라는 시트가 없습니다`); idx = i + 1; }
        this.model.sheets.splice(idx, 0, made); if (bool(args, 'activate') ?? true) this.model.active = made.name; this.#mutated();
        return this.#env({ sheet: made.name, index: idx + 1, activated: bool(args, 'activate') ?? true }, [`시트 '${made.name}' 을 만들었습니다(탭 ${idx + 1}번)`]);
      }
      case 'delete_sheet': { need(args, 'sheet'); const s = this.#sheet(args); if (this.model.sheets.length <= 1) refuse('마지막 시트는 지울 수 없습니다'); this.model.sheets.splice(this.model.sheets.indexOf(s), 1); if (this.model.active === s.name) this.model.active = this.model.sheets[0].name; this.#mutated(); return this.#env({ deleted: s.name }, [`시트 '${s.name}' 을 지웠습니다 — 되돌릴 수 없습니다`]); }
      case 'rename_sheet': { need(args, 'sheet'); const s = this.#sheet(args); const to = String(need(args, 'name')); const was = s.name; s.name = to; if (this.model.active === was) this.model.active = to; this.#mutated(); return this.#env({ sheet: to, was }, [`시트 '${was}' → '${to}'`]); }
      case 'move_sheet': { need(args, 'sheet'); const to = int(args, 'to'); if (to == null || to < 1) refuse('to 는 1 이상의 탭 위치입니다'); const s = this.#sheet(args); if (to > this.model.sheets.length) refuse(`탭이 ${this.model.sheets.length}개라 ${to}번 자리는 없습니다`); const was = this.model.sheets.indexOf(s) + 1; this.model.sheets.splice(was - 1, 1); this.model.sheets.splice(to - 1, 0, s); this.#mutated(); return this.#env({ sheet: s.name, from: was, to }, [`시트 '${s.name}' 을 ${was}번에서 ${to}번으로 옮겼습니다`]); }
      case 'copy_sheet': { this.#need('ExcelApi', '1.7', 'copy_sheet'); need(args, 'sheet'); const s = this.#sheet(args); const name = str(args, 'name') ?? `${s.name} (2)`; const copy = structuredClone(s); copy.name = name; const at = str(args, 'after') ? this.model.sheets.findIndex((x) => x.name === str(args, 'after')) + 1 : this.model.sheets.indexOf(s) + 1; this.model.sheets.splice(at, 0, copy); this.#mutated(); return this.#env({ sheet: name, from: s.name, index: at + 1 }, [`시트 '${s.name}' 을 복사해 '${name}' 을 만들었습니다`]); }
      case 'set_sheet_visibility': { need(args, 'sheet'); const v = String(need(args, 'visibility')); if (!['Visible', 'Hidden', 'VeryHidden'].includes(v)) refuse(`visibility 는 Visible, Hidden, VeryHidden 중 하나입니다 — '${v}'`); const s = this.#sheet(args); if (v !== 'Visible' && this.model.sheets.filter((x) => x.visibility === 'Visible').length <= 1) refuse('보이는 시트가 하나뿐이라 숨길 수 없습니다'); s.visibility = v; this.#mutated(); return this.#env({ sheet: s.name, visibility: v }, [`시트 '${s.name}' → ${v}`]); }
      case 'activate_sheet': { need(args, 'sheet'); const s = this.#sheet(args); this.model.active = s.name; const address = str(args, 'address'); if (address) parseAddress(address); return this.#env({ sheet: s.name, address: address ?? null }, [`시트 '${s.name}'${address ? ` ${address}` : ''} 로 갔습니다`]); }
      case 'set_rows_columns': { const rows = str(args, 'rows'); const cols = str(args, 'columns'); if ((rows == null) === (cols == null)) refuse('rows("3:5") 나 columns("B:D") 중 하나를 주세요'); const hidden = bool(args, 'hidden'); const group = bool(args, 'group'); const height = num(args, 'height'); const width = num(args, 'width'); if (group != null) this.#need('ExcelApi', '1.10', 'set_rows_columns{group}'); const words = [hidden != null && (hidden ? '숨김' : '보임'), group != null && (group ? '그룹' : '그룹 해제'), height != null && `높이 ${height}pt`, width != null && `너비 ${width}pt`].filter(Boolean); if (!words.length) refuse('바꿀 것이 없습니다 — hidden·group·height·width 중 하나'); if (rows != null && width != null) refuse('width 는 columns 에만 — rows 에는 height'); if (cols != null && height != null) refuse('height 는 rows 에만 — columns 에는 width'); const s = this.#sheet(args); const span = rows != null ? (rows.includes(':') ? rows : `${rows}:${rows}`) : (cols.includes(':') ? cols : `${cols}:${cols}`); (s.spans ??= {})[span] = { ...(s.spans[span] ?? {}), hidden, group, height, width }; this.#mutated(); return this.#env({ sheet: s.name, span, kind: rows != null ? 'rows' : 'columns' }, [`${s.name} ${rows != null ? '행' : '열'} ${span}: ${words.join(', ')}`]); }
      case 'set_tab_color': { this.#need('ExcelApi', '1.7', 'set_tab_color'); const color = hex(args, 'color', true) ?? refuse('color 가 없습니다 — #RRGGBB 또는 none'); const s = this.#sheet(args); s.tabColor = color === 'none' ? null : color; this.#mutated(); return this.#env({ sheet: s.name, color }, [`시트 '${s.name}' 탭 색 → ${color === 'none' ? '없음' : color}`]); }
      case 'set_sheet_view': { this.#need('ExcelApi', '1.8', 'set_sheet_view'); const grid = bool(args, 'gridlines'); const head = bool(args, 'headings'); if (grid == null && head == null) refuse('바꿀 것이 없습니다 — gridlines·headings 중 하나'); const s = this.#sheet(args); if (grid != null) s.gridlines = grid; if (head != null) s.headings = head; this.#mutated(); return this.#env({ sheet: s.name, gridlines: grid, headings: head }, [`시트 '${s.name}': ${[grid != null && `눈금선 ${grid ? '켬' : '끔'}`, head != null && `머리글 ${head ? '켬' : '끔'}`].filter(Boolean).join(', ')}`]); }
      case 'set_workbook_properties': { this.#need('ExcelApi', '1.7', 'set_workbook_properties'); const keys = ['title', 'subject', 'author', 'keywords', 'comments', 'category']; const set = keys.filter((k) => str(args, k) != null); if (!set.length) refuse('바꿀 것이 없습니다 — title·subject·author·keywords·comments·category 중 하나'); this.model.properties ??= {}; for (const k of set) this.model.properties[k] = str(args, k); this.#mutated(); return this.#env(Object.fromEntries(set.map((k) => [k, str(args, k)])), [`통합 문서 속성: ${set.map((k) => `${k}=「${clip(str(args, k), 30)}」`).join(', ')}`]); }
      case 'freeze_panes': { this.#need('ExcelApi', '1.7', 'freeze_panes'); const s = this.#sheet(args); const rows = int(args, 'rows') ?? 0; const cols = int(args, 'columns') ?? 0; s.frozen = rows === 0 && cols === 0 ? null : rangeName(0, 0, Math.max(rows, 1), Math.max(cols, 1)); this.#mutated(); return this.#env({ sheet: s.name, rows, columns: cols }, [rows === 0 && cols === 0 ? `시트 '${s.name}' 의 틀 고정을 풀었습니다` : `시트 '${s.name}': 위 ${rows}행·왼쪽 ${cols}열 고정`]); }
      case 'protect_sheet': case 'unprotect_sheet': { this.#need('ExcelApi', '1.7', op); const s = this.#sheet(args); const on = op === 'protect_sheet'; s.protected = on; this.#mutated(); return this.#env({ sheet: s.name, protected: on }, [on ? `시트 '${s.name}' 을 보호했습니다${str(args, 'password') ? '(암호 있음)' : '(암호 없음)'}` : `시트 '${s.name}' 의 보호를 풀었습니다`]); }
      case 'add_table': {
        const { sheet, box, address } = this.#rangeOf(args); const hasHeaders = bool(args, 'has_headers') ?? true; const name = str(args, 'name'); const style = str(args, 'table_style');
        if (name && !/^[A-Za-z_가-힣][\w가-힣.]*$/.test(name)) refuse(`표 이름은 글자로 시작하고 빈칸이 없어야 합니다 — '${name}'`);
        const clash = this.model.sheets.flatMap((s) => s.tables).find((t) => t.name === name || (t.sheet === sheet.name && overlaps(parseAddress(t.address), box)));
        if (clash) refuse(`${clash.name === name ? `'${name}' 이라는 표가 이미 있습니다` : `${address} 는 이미 표 '${clash.name}' 에 속합니다 — set_table_cells 로 쓰세요`}`);
        const headers = hasHeaders ? this.#values(sheet, { ...box, rows: 1 })[0].map(String) : Array.from({ length: box.cols }, (_, i) => `Column${i + 1}`);
        const t = { name: name ?? `Table${this.model.sheets.flatMap((s) => s.tables).length + 1}`, sheet: sheet.name, address, headers, rows: box.rows - (hasHeaders ? 1 : 0), style: style ?? 'TableStyleMedium2' };
        sheet.tables.push(t); this.#mutated();
        return this.#env({ table: t.name, sheet: sheet.name, address, has_headers: hasHeaders, style: style ?? null }, [`${sheet.name}!${address} 를 표 '${t.name}' 으로 만들었습니다${style ? ` (${style})` : ''}`]);
      }
      case 'set_table_cells': {
        const { sheet, t } = this.#table(args); const cells = arr(args, 'cells'); if (!cells || cells.length === 0) refuse('cells 가 비었습니다 — [{row, column, value}]');
        const box = parseAddress(t.address); let appended = 0;
        for (const c of cells) { const r = int(c, 'row'); const col = typeof c.column === 'number' ? c.column : t.headers.indexOf(String(c.column)); if (r == null || r < 0) refuse(`row 는 0 이상입니다 — ${JSON.stringify(c)}`); if (col < 0 || col >= t.headers.length) refuse(`'${c.column}' 은 이 표의 열이 아닙니다 — ${t.headers.join(', ')}`); while (r >= t.rows) { t.rows += 1; appended += 1; } this.#put(sheet, box.top + 1 + r, box.left + col, isFormula(c.value) ? { f: c.value, v: c.value } : { v: c.value ?? '' }); }
        t.address = rangeName(box.top, box.left, t.rows + 1, box.cols); this.#mutated();
        return this.#env({ table: t.name, cells: cells.length, appended }, [`표 '${t.name}' 의 칸 ${cells.length}개를 적었습니다${appended ? ` (행 ${appended}개 추가)` : ''}`]);
      }
      case 'add_table_rows': { const { sheet, t } = this.#table(args); const rows = grid(args, 'rows'); if (rows.cols !== t.headers.length) refuse(`이 표는 ${t.headers.length}열인데 줄마다 ${rows.cols}칸입니다`); const box = parseAddress(t.address); const at = int(args, 'at') ?? t.rows; for (let i = 0; i < rows.rows; i += 1) for (let c = 0; c < rows.cols; c += 1) this.#put(sheet, box.top + 1 + at + i, box.left + c, { v: rows.cells[i][c] }); t.rows += rows.rows; t.address = rangeName(box.top, box.left, t.rows + 1, box.cols); this.#mutated(); return this.#env({ table: t.name, added: rows.rows, at: int(args, 'at') ?? null }, [`표 '${t.name}' 에 행 ${rows.rows}개를 ${int(args, 'at') != null ? `${at}번 앞에 끼워` : '끝에 붙여'} 넣었습니다`]); }
      case 'remove_table': { const { sheet, t } = this.#table(args); const del = bool(args, 'delete_data') ?? false; sheet.tables.splice(sheet.tables.indexOf(t), 1); if (del) { const box = parseAddress(t.address); this.#each(sheet, box, (r, c) => { delete sheet.cells[cellName(r, c)]; }); } this.#mutated(); return this.#env({ table: t.name, address: t.address, deleted_data: del }, [del ? `표 '${t.name}' 과 그 셀(${t.address})을 지웠습니다` : `표 '${t.name}' 을 풀었습니다 — ${t.address} 의 값은 그대로입니다`]); }
      case 'sort_range': {
        const by = arr(args, 'by'); if (!by || by.length === 0) refuse('by 가 비었습니다 — [{column, ascending}]');
        let sheet; let box; let hasHeaders; let label; let fields;
        if (str(args, 'table')) { const got = this.#table(args); sheet = got.sheet; box = parseAddress(got.t.address); hasHeaders = true; fields = by.map((f) => { const k = typeof f.column === 'number' ? f.column : got.t.headers.indexOf(String(f.column)); if (k < 0) refuse(`'${f.column}' 은 이 표의 열이 아닙니다 — ${got.t.headers.join(', ')}`); return { key: k, ascending: bool(f, 'ascending') ?? true }; }); label = `표 '${got.t.name}'`; }
        else { const got = this.#rangeOf(args); sheet = got.sheet; box = got.box; hasHeaders = bool(args, 'has_headers') ?? true; fields = by.map((f) => { const k = int(f, 'column'); if (k == null || k < 0) refuse(`column 은 범위 안의 0-based 열 번호입니다 — ${JSON.stringify(f)}`); return { key: k, ascending: bool(f, 'ascending') ?? true }; }); label = `${sheet.name}!${got.address}`; }
        const start = hasHeaders ? 1 : 0; const rows = [];
        for (let r = start; r < box.rows; r += 1) { const row = []; for (let c = 0; c < box.cols; c += 1) row.push(sheet.cells[cellName(box.top + r, box.left + c)] ?? null); rows.push(row); }
        rows.sort((p, q) => { for (const f of fields) { const x = p[f.key]?.v ?? ''; const y = q[f.key]?.v ?? ''; if (x === y) continue; const cmp = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y)); return f.ascending ? cmp : -cmp; } return 0; });
        rows.forEach((row, i) => row.forEach((cell, c) => { const k = cellName(box.top + start + i, box.left + c); if (cell) sheet.cells[k] = cell; else delete sheet.cells[k]; }));
        this.#mutated();
        return this.#env({ sheet: sheet.name, by: fields, has_headers: hasHeaders }, [`${label} 를 ${fields.map((f) => `${f.key}열${f.ascending ? '↑' : '↓'}`).join(', ')} 로 정렬했습니다`]);
      }
      case 'filter_table': { this.#need('ExcelApi', '1.9', 'filter_table'); const { t } = this.#table(args); const column = String(need(args, 'column')); if (!t.headers.includes(column)) refuse(`'${column}' 은 표 '${t.name}' 의 열이 아닙니다`); const values = arr(args, 'values'); const criterion = str(args, 'criterion'); t.filters ??= {}; if (values?.length) t.filters[column] = { values }; else if (criterion) t.filters[column] = { criterion }; else delete t.filters[column]; this.#mutated(); return this.#env({ table: t.name, column, values: values ?? null, criterion: criterion ?? null }, [`표 '${t.name}' 의 '${column}' 열: ${values?.length ? `값 ${values.join(', ')} 만` : criterion ? `조건 ${criterion}` : '필터 해제'}`]); }
      case 'add_chart': {
        const sheet = this.#sheet(args); const source = String(need(args, 'source')); const type = chartTypeOf(str(args, 'chart_type') ?? str(args, 'kind') ?? str(args, 'type'));
        const seriesBy = str(args, 'series_by') ?? 'Columns'; if (!['Columns', 'Rows', 'Auto'].includes(seriesBy)) refuse(`series_by 는 Columns 또는 Rows 입니다 — '${seriesBy}'`);
        const src = source.includes('!') ? this.#sheet({ sheet: source.split('!')[0].replace(/^'|'$/g, '') }) : sheet; const box = parseAddress(source.includes('!') ? source.split('!')[1] : source);
        if (box.rows < 2 || box.cols < 2) refuse(`source ${rangeName(box.top, box.left, box.rows, box.cols)} 는 ${box.rows}×${box.cols} 라 차트를 못 만듭니다 — 머리글 행과 값 열이 있어야 합니다`);
        const head = this.#values(src, { ...box, rows: 1 })[0]; const series = seriesBy === 'Rows' ? this.#values(src, box).slice(1).map((r) => String(r[0])) : head.slice(1).map(String);
        const c = { name: str(args, 'name') ?? `Chart ${sheet.charts.length + 1}`, type, title: str(args, 'title') ?? '', series, source: rangeName(box.top, box.left, box.rows, box.cols), left: num(args, 'left') ?? 300, top: num(args, 'top') ?? 20, width: num(args, 'width') ?? 480, height: num(args, 'height') ?? 300 };
        sheet.charts.push(c); this.#mutated();
        return this.#env({ chart: c.name, sheet: sheet.name, type, type_ko: CHART_KO.get(type) ?? type, source: c.source, left: c.left, top: c.top, width: c.width, height: c.height }, [`시트 '${sheet.name}' 에 ${CHART_KO.get(type) ?? type} 차트 '${c.name}' 을 넣었습니다 — 원본 ${c.source}${c.title ? `, 제목 「${c.title}」` : ''}`]);
      }
      case 'format_chart': {
        const { c } = this.#chart(args); const said = [];
        const title = str(args, 'title'); if (title != null) { c.title = title; said.push(title ? `제목 「${title}」` : '제목 없음'); }
        const xt = str(args, 'x_title'); if (xt != null) { c.x_title = xt; said.push(`가로축 「${xt}」`); } const yt = str(args, 'y_title'); if (yt != null) { c.y_title = yt; said.push(`세로축 「${yt}」`); }
        const legend = str(args, 'legend'); if (legend) { c.legend = legend; said.push(legend.toLowerCase() === 'none' ? '범례 없음' : `범례 ${legend}`); }
        const labels = bool(args, 'data_labels'); if (labels != null) { c.data_labels = labels; said.push(labels ? '값 표시' : '값 표시 해제'); }
        const type = str(args, 'chart_type'); if (type) { c.type = chartTypeOf(type); said.push(`종류 ${CHART_KO.get(c.type) ?? c.type}`); }
        for (const k of ['left', 'top', 'width', 'height']) { const v = num(args, k); if (v != null) { c[k] = v; said.push(`${k} ${v}`); } }
        if (said.length === 0) refuse('바꿀 것이 하나도 안 왔습니다 — title, x_title, y_title, legend, data_labels, chart_type, left/top/width/height');
        this.#mutated(); return this.#env({ chart: c.name, changed: said.length }, [`차트 '${c.name}': ${said.join(', ')}`]);
      }
      case 'delete_chart': { const { sheet, c } = this.#chart(args); sheet.charts.splice(sheet.charts.indexOf(c), 1); this.#mutated(); return this.#env({ chart: c.name, sheet: sheet.name }, [`차트 '${c.name}' 을 지웠습니다`]); }
      case 'add_conditional_format': {
        this.#need('ExcelApi', '1.6', 'add_conditional_format'); const kind = (str(args, 'cf_kind') ?? str(args, 'kind') ?? refuse('cf_kind 가 없습니다')).toLowerCase();
        if (!['cell_value', 'color_scale', 'data_bar', 'icon_set', 'contains_text', 'top_bottom', 'custom'].includes(kind)) refuse(`cf_kind 는 cell_value, color_scale, data_bar, icon_set, contains_text, top_bottom, custom 중 하나입니다 — '${kind}'`);
        if (kind === 'cell_value' && str(args, 'value') == null) refuse('cell_value 에는 value 가 있어야 합니다');
        if (kind === 'custom' && str(args, 'formula') == null) refuse('custom 에는 formula 가 있어야 합니다');
        const { sheet, address } = this.#rangeOf(args); const f = { id: `cf${sheet.cf.length + 1}`, kind, address, args: { ...args } }; sheet.cf.push(f); this.#mutated();
        return this.#env({ sheet: sheet.name, address, cf_kind: kind }, [`${sheet.name}!${address} 에 조건부 서식: ${kind}${hex(args, 'fill') ? ` 채우기 ${hex(args, 'fill')}` : ''}`]);
      }
      case 'clear_conditional_formats': { const { sheet, address, box, used } = this.#rangeOf(args, { must: false }); const before = sheet.cf.length; sheet.cf = used ? [] : sheet.cf.filter((f) => !overlaps(parseAddress(f.address), box)); const n = before - sheet.cf.length; if (n) this.#mutated(); return this.#env({ sheet: sheet.name, address, cleared: n }, [`${sheet.name}!${address ?? ''} 의 조건부 서식 ${n}개를 지웠습니다`]); }
      case 'set_validation': {
        this.#need('ExcelApi', '1.8', 'set_validation'); const { sheet, address } = this.#rangeOf(args);
        if (bool(args, 'clear')) { delete sheet.validation[address]; this.#mutated(); return this.#env({ sheet: sheet.name, address, cleared: true }, [`${sheet.name}!${address} 의 유효성을 지웠습니다`]); }
        const kind = (str(args, 'validation_kind') ?? str(args, 'kind') ?? refuse('validation_kind 가 없습니다')).toLowerCase();
        if (!['list', 'whole_number', 'decimal', 'date', 'time', 'text_length', 'custom'].includes(kind)) refuse(`validation_kind 는 list, whole_number, decimal, date, time, text_length, custom 중 하나입니다 — '${kind}'`);
        if (kind === 'list' && !arr(args, 'values') && !str(args, 'source')) refuse('list 에는 values 나 source 가 있어야 합니다');
        if (['whole_number', 'decimal', 'date', 'time', 'text_length'].includes(kind) && str(args, 'value') == null) refuse(`${kind} 에는 value 가 있어야 합니다`);
        sheet.validation[address] = { kind, rule: { ...args }, prompt: str(args, 'prompt'), error: str(args, 'error') }; this.#mutated();
        return this.#env({ sheet: sheet.name, address, validation_kind: kind }, [`${sheet.name}!${address} 에 유효성: ${kind}`]);
      }
      case 'set_name': { const name = String(need(args, 'name')); const refersTo = String(need(args, 'refers_to')); const coll = str(args, 'sheet') ? this.#sheet(args).names : (this.model.names ??= []); const had = coll.find((n) => n.name === name); const was = had?.refers_to ?? null; if (had) had.refers_to = refersTo; else coll.push({ name, refers_to: refersTo, comment: str(args, 'comment') }); this.#mutated(); return this.#env({ name, refers_to: refersTo, scope: str(args, 'sheet') ?? 'workbook', was }, [was ? `이름 '${name}' 을 ${was} 에서 ${refersTo} 로 바꿨습니다` : `이름 '${name}' = ${refersTo} 을 정의했습니다`]); }
      case 'delete_name': { const name = String(need(args, 'name')); const coll = str(args, 'sheet') ? this.#sheet(args).names : (this.model.names ??= []); const i = coll.findIndex((n) => n.name === name); if (i < 0) refuse(`'${name}' 이라는 이름이 없습니다 — read_names 가 목록을 줍니다`); coll.splice(i, 1); this.#mutated(); return this.#env({ name, deleted: true }, [`이름 '${name}' 을 지웠습니다 — 그것을 쓰던 수식은 #NAME? 이 됩니다`]); }
      case 'add_comment': { this.#need('ExcelApi', '1.10', 'add_comment'); const text = String(need(args, 'text')); const { sheet, box, address } = this.#rangeOf(args); if (box.rows * box.cols !== 1) refuse('메모는 셀 하나에 답니다 — address 를 셀 하나로'); const had = sheet.comments.find((c) => c.address === address); if (had) { (had.replies ??= []).push({ author: 'me', text }); } else sheet.comments.push({ id: `c${sheet.comments.length + 1}`, address, author: 'me', text, replies: [], resolved: false }); this.#mutated(); return this.#env({ sheet: sheet.name, address, replied: Boolean(had) }, [had ? `${sheet.name}!${address} 의 메모에 답글을 달았습니다` : `${sheet.name}!${address} 에 메모를 달았습니다 — 「${clip(text, 40)}」`]); }
      case 'resolve_comment': { this.#need('ExcelApi', '1.10', 'resolve_comment'); const del = bool(args, 'delete') ?? false; const resolved = bool(args, 'resolved') ?? true; if (!del) this.#need('ExcelApi', '1.11', 'resolve_comment{resolved}'); const { sheet, address } = this.#rangeOf(args); const c = sheet.comments.find((x) => x.address === address); if (!c) refuse(`${address} 에는 메모가 없습니다`); if (del) sheet.comments.splice(sheet.comments.indexOf(c), 1); else c.resolved = resolved; this.#mutated(); return this.#env({ sheet: sheet.name, address, deleted: del, resolved: del ? null : resolved }, [del ? `${sheet.name}!${address} 의 메모를 지웠습니다` : `${sheet.name}!${address} 의 메모를 ${resolved ? '해결로' : '미해결로'} 표시했습니다`]); }
      case 'add_image': { this.#need('ExcelApi', '1.9', 'add_image'); if (!str(args, 'image_base64')) refuse('그림 바이트가 안 왔습니다 — path 를 주면 헬퍼가 읽어 실어 줍니다'); const sheet = this.#sheet(args); const img = { name: str(args, 'name') ?? '그림', left: num(args, 'left') ?? 20, top: num(args, 'top') ?? 20, width: num(args, 'width') ?? num(args, 'image_width') ?? 200, height: num(args, 'height') ?? num(args, 'image_height') ?? 150, alt: str(args, 'alt') }; sheet.images.push(img); this.#mutated(); return this.#env({ sheet: sheet.name, shape: img.name, path: str(args, 'path'), width: img.width, height: img.height }, [`시트 '${sheet.name}' 에 그림 '${img.name}' 을 넣었습니다(${Math.round(img.width)}×${Math.round(img.height)}pt)${img.alt ? '' : ' — 대체 텍스트가 없어 파일 이름을 씁니다'}`]); }
      case 'add_pivot': { this.#need('ExcelApi', '1.8', 'add_pivot'); const sheet = this.#sheet(args); const source = String(need(args, 'source')); const dest = String(need(args, 'destination')); parseAddress(dest); const rows = arr(args, 'rows') ?? []; const cols = arr(args, 'columns') ?? []; const values = arr(args, 'values') ?? arr(args, 'data') ?? []; const name = str(args, 'name') ?? `Pivot${sheet.pivots.length + 1}`; sheet.pivots.push({ name, source, destination: dest, rows, columns: cols, values }); this.#mutated(); return this.#env({ pivot: name, sheet: sheet.name, destination: dest, rows, columns: cols, values: values.length }, [`시트 '${sheet.name}' ${dest} 에 피벗 '${name}' — 행 ${rows.join('/') || '없음'}, 열 ${cols.join('/') || '없음'}, 값 ${values.length}개`]); }
      case 'refresh_pivot': { const sheet = this.#sheet(args); const name = str(args, 'name'); if (name && !sheet.pivots.some((p) => p.name === name)) refuse(`'${name}' 이라는 피벗이 없습니다`); return this.#env({ sheet: sheet.name, pivot: name ?? 'all' }, [name ? `피벗 '${name}' 을 새로 고쳤습니다` : `시트 '${sheet.name}' 의 피벗을 전부 새로 고쳤습니다`]); }
      case 'restore_range': { const id = String(need(args, 'snapshot')); const snap = this.snapshots.get(id); if (!snap) refuse(`그런 스냅숏이 없습니다: ${id} — snapshot_range 가 준 id 를 주세요(이 창이 뜬 뒤 찍은 것만 압니다)`); const sheet = this.#sheet({ sheet: snap.sheet }); for (const [k, cell] of Object.entries(snap.cells)) { if (cell) sheet.cells[k] = { ...cell }; else delete sheet.cells[k]; } this.#mutated(); return this.#env({ snapshot: id, sheet: snap.sheet, address: snap.address }, [`${snap.sheet}!${snap.address} 를 스냅숏 ${id} 로 되돌렸습니다`]); }
      case 'set_tag': { const key = String(need(args, 'key')).trim(); const value = str(args, 'value'); if (key.startsWith(FIX_PREFIX) || key === BOOK_SETTING_KEY) refuse(`'${key}' 는 이 창이 쓰는 이름이라 메모로 못 씁니다`); if (value == null) { const had = key in this.model.settings; delete this.model.settings[key]; this.#mutated(); return this.#env({ key, removed: had }, [had ? `메모 '${key}' 를 지웠습니다` : `메모 '${key}' 는 원래 없었습니다`]); } this.model.settings[key] = value; this.#mutated(); return this.#env({ key, value }, [`메모 '${key}' 를 남겼습니다 — 「${clip(value, 40)}」`]); }
      case 'suggest': { const what = String(need(args, 'what')).trim(); const fix = args.fix && typeof args.fix === 'object' ? args.fix : null; if (fix && !FIX_TOOLS.includes(String(fix.tool))) refuse(`제안으로 누를 수 있는 손이 아닙니다 — '${fix.tool}'. 누를 수 있는 것: ${FIX_TOOLS.join(', ')}`); const sheet = str(args, 'sheet'); const address = str(args, 'address'); let key = `${FIX_PREFIX}${Date.now().toString(36).toUpperCase()}${Object.keys(this.model.settings).length}`; while (key in this.model.settings) key += '-1'; const body = { what, sheet, address }; if (str(args, 'why')) body.why = str(args, 'why'); if (fix) body.fix = { tool: String(fix.tool), args: fix.args ?? {} }; this.model.settings[key] = JSON.stringify(body); this.#mutated(); return this.#env({ suggestion: key, sheet, address, fixable: Boolean(fix) }, [`${sheet ? `${sheet}${address ? `!${address}` : ''}` : '통합 문서'} 에 제안을 붙였습니다 — ${what}. **이건 아직 안 고친 것입니다** — 작업창의 「적용」을 누르기 전까지 통합 문서는 그대로입니다`]); }
      case 'drop_suggestion': { const key = String(need(args, 'key')).trim(); if (!key.startsWith(FIX_PREFIX)) refuse(`제안의 키가 아닙니다 — '${key}'. set_tag 로 남긴 메모는 set_tag 로 지우세요`); if (!(key in this.model.settings)) refuse(`그런 제안이 없습니다 — ${key}`); delete this.model.settings[key]; this.#mutated(); return this.#env({ dropped: key }, [`제안 ${key} 를 뗐습니다 — 고치지는 않았습니다`]); }
      default: throw new Error(`이 손은 ${op} 을 모릅니다 — 아는 것: ${ALL_OPS.join(', ')}`);
    }
    function a(x) { return x; }
  }

  #each(sheet, box, fn) { for (let r = 0; r < box.rows; r += 1) for (let c = 0; c < box.cols; c += 1) fn(box.top + r, box.left + c); }
  #table(args) { const name = String(need(args, 'table')); for (const s of this.model.sheets) { const t = s.tables.find((x) => x.name === name); if (t) return { sheet: s, t }; } return refuse(`'${name}' 이라는 표가 없습니다 — describe_sheet 가 이름을 줍니다`); }
  #chart(args) { const sheet = this.#sheet(args); const name = String(need(args, 'chart')); const c = sheet.charts.find((x) => x.name === name); return c ? { sheet, c } : refuse(`'${name}' 이라는 차트가 시트 '${sheet.name}' 에 없습니다 — describe_sheet 가 이름을 줍니다`); }
}

function overlaps(p, q) {
  return p.top < q.top + q.rows && q.top < p.top + p.rows && p.left < q.left + q.cols && q.left < p.left + p.cols;
}
