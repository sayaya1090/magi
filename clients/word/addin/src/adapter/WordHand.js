import { HandPort } from '../port/HandPort.js';
import {
  ALL_OPS, FIX_TOOLS, FIX_PREFIX, DOC_PROPERTY_KEY, Refusal, refuse, str, num, int, bool, arr, need, hex, span,
  envelope, clip, nowEpoch, BUILTIN_PARAGRAPH_STYLES, fieldPieces,
} from './handCore.js';

/** 문단 스타일 — 내장 이름(Heading2·"Heading 2"·normal)은 언어와 무관한 styleBuiltIn 으로, 나머지는 문서가 보여 주는 이름 그대로.
 *  한국어 Word 에는 "Heading 1" 이란 스타일이 없어 `style = "Heading 1"` 이 InvalidArgument 였다(실물 2026-09-06). */
export function applyStyle(p, style) {
  if (!style) return;
  const key = String(style).replace(/[\s_-]/g, '').toLowerCase();
  const builtin = BUILTIN_PARAGRAPH_STYLES.find((b) => b.toLowerCase() === key);
  if (builtin) p.styleBuiltIn = builtin; else p.style = style;
}
/** 목록 항목 뒤에 insertParagraph 한 문단은 Word 가 그 목록에 이어 붙인다 — 넣으라는 말은 문단이지 항목이 아니다. */
async function detachInherited(context, paras) {
  for (const p of paras) p.load('isListItem');
  await context.sync();
  let any = false;
  for (const p of paras) if (p.isListItem) { p.detachFromList(); any = true; }
  if (any) await context.sync();
}
/** 목록에 안 든 문단만 붙인다 — 든 문단에 attachToList 는 GeneralException. 항목이 된 문단에서 다시 읽는다. */
async function attachMissing(context, paras, listId) {
  for (const p of paras) p.load('isListItem');
  await context.sync();
  let any = false;
  for (const p of paras) if (!p.isListItem) { p.attachToList(listId, 0); any = true; }
  if (any) await context.sync();
}
/** 사용자 지정 속성은 형이 있다 — "2026-09-06" 을 문자열로 넣어도 Word 가 날짜로 굳힌다(실물). 자정 날짜는 날짜만 돌려준다. */
function tagText(p) {
  if (p.type !== 'Date') return String(p.value);
  const d = new Date(p.value);
  if (Number.isNaN(d.getTime())) return String(p.value);
  const iso = d.toISOString();
  return iso.endsWith('T00:00:00.000Z') ? iso.slice(0, 10) : iso;
}

/**
 * 진짜 Word 에 닿는 손. **이 파일과 `OfficeDocument` 만 Office 를 안다.**
 *
 * 헬퍼가 보낸 조작 하나(op, args)를 Word.run 한 묶음으로 옮기고, 봉투(handCore.envelope)로 답한다. 규칙은 엑셀·
 * 파워포인트 판과 같다: 못 하는 것은 던진다, 쓰기는 changed 에 전후를 적는다, 요구 집합이 모자라면 op 마다 그 이름을 대고
 * 거절한다(WordApi 1.3 바닥, 1.4 메모·책갈피·변경 추적 모드·settings, 1.6 변경 검토), 호출은 한 줄로 선다.
 *
 * 손잡이는 **문단 번호**(1부터, 본문 순서)다. 매 호출이 본문 문단 목록을 새로 읽는다 — 앞 호출이 끼워 넣었으면 번호가
 * 밀려 있고, 그 사실을 답의 `now`(문단 수) 가 말한다.
 */
export class WordHand extends HandPort {
  static staleAfter = 40000;
  static stuckAfter = 50000;

  constructor({ run, supports, document = '', label = '' } = {}) {
    super();
    this.runner = run ?? ((fn) => Word.run(fn));
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
  get label() { return this.labelText; }

  async run(op, args = {}) {
    if (this.#inside) return this.#dispatch(op, args);
    const joined = Date.now();
    const turn = this.#queue.then(async () => {
      if (Date.now() - joined > WordHand.staleAfter) {
        throw new Error(`${op}: 앞 호출을 ${Math.round((Date.now() - joined) / 1000)}초 기다리다 헬퍼가 포기했을 시각을 넘겼습니다 — 다시 부르세요`);
      }
      this.#inside = true;
      let timer;
      try {
        return await Promise.race([
          this.#dispatch(op, args),
          new Promise((_, rej) => { timer = setTimeout(() => rej(new Error(`${op}: Word 가 ${WordHand.stuckAfter / 1000}초 안에 답하지 않았습니다`)), WordHand.stuckAfter); }),
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
      try { out.result.now = await this.#now(); } catch { /* 계측이 본 작업을 막지 않는다 */ }
    }
    return out;
  }
  async #now() {
    return this.runner(async (context) => {
      const ps = context.document.body.paragraphs; ps.load('items'); await context.sync();
      return { paragraphs: ps.items.length };
    });
  }
  #mutated() { this.count += 1; }
  #envelope(result, changed = []) { return envelope(this, result, changed); }
  #need(name, version, what) {
    if (!this.supports(name, version)) refuse(`${what} 은 ${name} ${version} 이 필요한데 이 호스트에는 없습니다`);
  }

  // ── 자리 고르기 ──
  /** 본문 문단 목록. 번호는 1부터. */
  async #paras(context, fields = 'text') {
    const ps = context.document.body.paragraphs; ps.load(`items/${fields.split(',').join(',items/')}`); await context.sync();
    return ps.items;
  }
  /** from..to 의 문단들 — 없는 번호는 거절. */
  #pick(items, a, opts) {
    const s = span(a, items.length, opts);
    return { ...s, list: items.slice(s.from - 1, s.to) };
  }
  /** 문단 from..to 를 하나의 Range 로. */
  static #rangeOf(list) {
    const first = list[0].getRange('Whole');
    return list.length === 1 ? first : first.expandTo(list[list.length - 1].getRange('Whole'));
  }
  #anchor(items, a) {
    const after = int(a, 'after'); const before = int(a, 'before'); const at = str(a, 'at');
    if (after != null) { if (after < 1 || after > items.length) refuse(`문서에 ${after}번 문단이 없습니다 — 문단 ${items.length}개`); return { p: items[after - 1], where: 'After', said: `문단 ${after} 뒤에` }; }
    if (before != null) { if (before < 1 || before > items.length) refuse(`문서에 ${before}번 문단이 없습니다 — 문단 ${items.length}개`); return { p: items[before - 1], where: 'Before', said: `문단 ${before} 앞에` }; }
    if (at === 'start') return { p: null, where: 'Start', said: '본문 처음에' };
    return { p: null, where: 'End', said: '본문 끝에' };
  }
  async #table(context, a) {
    const n = int(a, 'table') ?? refuse('table 이 없습니다 — 표 번호(1부터)');
    const ts = context.document.body.tables; ts.load('items'); await context.sync();
    if (n < 1 || n > ts.items.length) refuse(`문서에 ${n}번 표가 없습니다 — 표 ${ts.items.length}개`);
    return ts.items[n - 1];
  }

  async #route(op, a) {
    if (!ALL_OPS.includes(op)) refuse(`모르는 조작입니다: ${op} — 아는 것: ${ALL_OPS.join(', ')}`);
    switch (op) {
      case 'list_paragraphs': return this.#listParagraphs(a);
      case 'read_paragraphs': return this.#readParagraphs(a);
      case 'read_document': return this.#readDocument(a);
      case 'find': return this.#find(a);
      case 'read_table': return this.#readTable(a);
      case 'read_html': return this.#readHtml(a);
      case 'read_comments': return this.#readComments(a);
      case 'read_tracked_changes': return this.#readTrackedChanges(a);
      case 'describe_style': return this.#describeStyle(a);
      case 'snapshot_paragraphs': return this.#snapshot(a);
      case 'read_tags': return this.#readTags(a);
      case 'read_suggestions': return this.#readSuggestions(a);
      case 'advise': return this.#envelope({ pinned: Array.isArray(a.items) ? a.items.length : 0 });
      case 'clear_advice': return this.#envelope({ pinned: 0 });
      case 'insert_paragraphs': return this.#insertParagraphs(a);
      case 'replace_paragraph': return this.#replaceParagraph(a);
      case 'delete_paragraphs': return this.#deleteParagraphs(a);
      case 'set_style': return this.#setStyle(a);
      case 'format_text': return this.#formatText(a);
      case 'format_paragraph': return this.#formatParagraph(a);
      case 'insert_table': return this.#insertTable(a);
      case 'set_table_cells': return this.#setTableCells(a);
      case 'add_table_rows': return this.#addTableRows(a);
      case 'delete_table': return this.#deleteTable(a);
      case 'format_table': return this.#formatTable(a);
      case 'format_table_cells': return this.#formatTableCells(a);
      case 'edit_table': return this.#editTable(a);
      case 'insert_list': return this.#insertList(a);
      case 'set_list': return this.#setList(a);
      case 'insert_image': return this.#insertImage(a);
      case 'list_images': return this.#listImages(a);
      case 'format_image': return this.#formatImage(a);
      case 'delete_image': return this.#deleteImage(a);
      case 'insert_break': return this.#insertBreak(a);
      case 'insert_field': return this.#insertField(a);
      case 'read_footnotes': return this.#readFootnotes(a);
      case 'insert_footnote': return this.#insertFootnote(a);
      case 'delete_footnote': return this.#deleteFootnote(a);
      case 'set_style_format': return this.#setStyleFormat(a);
      case 'move_paragraphs': return this.#moveParagraphs(a);
      case 'insert_file': return this.#insertFile(a);
      case 'set_header_footer': return this.#setHeaderFooter(a);
      case 'set_hyperlink': return this.#setHyperlink(a);
      case 'replace_all': return this.#replaceAll(a);
      case 'add_comment': return this.#addComment(a);
      case 'reply_comment': return this.#replyComment(a);
      case 'resolve_comment': return this.#resolveComment(a);
      case 'add_bookmark': return this.#addBookmark(a);
      case 'delete_bookmark': return this.#deleteBookmark(a);
      case 'set_track_changes': return this.#setTrackChanges(a);
      case 'review_changes': return this.#reviewChanges(a);
      case 'set_properties': return this.#setProperties(a);
      case 'restore_paragraphs': return this.#restore(a);
      case 'set_tag': return this.#setTag(a);
      case 'suggest': return this.#suggest(a);
      case 'drop_suggestion': return this.#dropSuggestion(a);
      default: refuse(`아직 손이 없는 조작입니다: ${op}`);
    }
  }

  // ── 읽기 ──
  async #listParagraphs(a) {
    const max = int(a, 'max') ?? 200;
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text,style,styleBuiltIn,isListItem,tableNestingLevel');
      const picked = this.#pick(items, a, { must: false }); const from = picked.from; const to = int(a, 'to') == null ? items.length : picked.to; // 목차는 from 만 주면 끝까지 넘긴다
      const rows = []; let tableNo = 0; let lastTable = null;
      const tables = context.document.body.tables; tables.load('items'); await context.sync();
      const tableRanges = tables.items.map((t) => t.getRange()); const cmp = [];
      for (let i = from - 1; i < to && rows.length < max; i += 1) {
        const p = items[i];
        let list = null;
        if (p.isListItem) { const li = p.listItem; li.load('level,listString'); cmp.push([i, li]); }
        rows.push({ paragraph: i + 1, style: p.style, builtin: p.styleBuiltIn, text: clip(p.text, 80), in_table: p.tableNestingLevel > 0 ? true : undefined, list });
      }
      try { await context.sync(); } catch { /* 목록 항목 읽기는 덤 */ }
      for (const [i, li] of cmp) { const r = rows.find((x) => x.paragraph === i + 1); if (r) r.list = { level: li.level, mark: li.listString }; }
      void tableRanges; void tableNo; void lastTable;
      return this.#envelope({ paragraphs: rows, from, to: Math.min(to, from - 1 + rows.length), total: items.length, tables: tables.items.length, truncated: rows.length < to - from + 1 });
    });
  }
  async #readParagraphs(a) {
    const maxChars = int(a, 'max_chars') ?? 4000;
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text,style,styleBuiltIn,alignment,isListItem,lineSpacing,spaceAfter,spaceBefore,firstLineIndent,leftIndent');
      const { from, to, list } = this.#pick(items, a, { must: false });
      for (const p of list) p.font.load('name,size,bold,italic,color,highlightColor,underline');
      await context.sync();
      const out = list.map((p, i) => ({
        paragraph: from + i, style: p.style, builtin: p.styleBuiltIn, align: p.alignment, list: p.isListItem || undefined,
        text: p.text.length > maxChars ? p.text.slice(0, maxChars) : p.text, truncated: p.text.length > maxChars || undefined,
        font: { name: p.font.name, size: p.font.size, bold: p.font.bold, italic: p.font.italic, color: p.font.color, highlight: p.font.highlightColor, underline: p.font.underline },
        spacing: { before: p.spaceBefore, after: p.spaceAfter, line: p.lineSpacing, first_line_indent: p.firstLineIndent, left_indent: p.leftIndent },
      }));
      return this.#envelope({ paragraphs: out, from, to, total: items.length });
    });
  }
  async #readDocument() {
    return this.runner(async (context) => {
      const doc = context.document; const props = doc.properties; props.load('title,subject,author,keywords,comments,category,lastAuthor');
      const ps = doc.body.paragraphs; ps.load('items'); const ts = doc.body.tables; ts.load('items');
      const secs = doc.sections; secs.load('items'); const pics = doc.body.inlinePictures; pics.load('items');
      await context.sync();
      const heads = secs.items.map((s) => { const h = s.getHeader('Primary'); const f = s.getFooter('Primary'); h.load('text'); f.load('text'); return { h, f }; });
      let tracking = null; let comments = null;
      if (this.supports('WordApi', '1.4')) { doc.load('changeTrackingMode'); }
      await context.sync();
      if (this.supports('WordApi', '1.4')) { tracking = doc.changeTrackingMode; try { const cs = doc.body.getComments(); cs.load('items'); await context.sync(); comments = cs.items.length; } catch { comments = null; } }
      const caps = ['1.3', '1.4', '1.5', '1.6', '1.7', '1.8', '1.9'].filter((v) => this.supports('WordApi', v));
      return this.#envelope({
        properties: { title: props.title, subject: props.subject, author: props.author, keywords: props.keywords, comments: props.comments, category: props.category, last_author: props.lastAuthor },
        paragraphs: ps.items.length, tables: ts.items.length, sections: secs.items.length, pictures: pics.items.length, comment_threads: comments,
        headers_footers: heads.map((x, i) => ({ section: i + 1, header: x.h.text, footer: x.f.text })),
        track_changes: tracking, word_api: caps.length ? `WordApi ${caps[caps.length - 1]}` : 'unknown',
      });
    });
  }
  async #find(a) {
    const text = String(need(a, 'text')); const limit = int(a, 'limit') ?? 50;
    const opts = { matchCase: bool(a, 'match_case') ?? false, matchWholeWord: bool(a, 'whole_word') ?? false, matchWildcards: false };
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const found = items.map((p) => p.search(text, opts)); for (const f of found) f.load('items/text');
      await context.sync();
      const hits = [];
      found.forEach((f, i) => { for (const r of f.items) { if (hits.length >= limit) break; const at = items[i].text.indexOf(r.text); hits.push({ paragraph: i + 1, text: r.text, context: clip(items[i].text.slice(Math.max(0, at - 30), at + r.text.length + 30), 100) }); } });
      const matched = found.reduce((n, f) => n + f.items.length, 0);
      return this.#envelope({ hits, matched, truncated: matched > hits.length });
    });
  }
  async #readTable(a) {
    const maxRows = int(a, 'max_rows') ?? 200;
    return this.runner(async (context) => {
      const t = await this.#table(context, a); t.load('rowCount,values,headerRowCount,style,styleBuiltIn,alignment'); await context.sync();
      const values = t.values.slice(0, maxRows);
      return this.#envelope({ table: int(a, 'table'), rows: t.rowCount, columns: values[0]?.length ?? 0, has_header: t.headerRowCount > 0, style: t.styleBuiltIn || t.style, align: t.alignment, values, truncated: t.rowCount > values.length });
    });
  }
  async #readHtml(a) {
    const maxChars = int(a, 'max_chars') ?? 20000;
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list } = this.#pick(items, a, { must: false });
      const html = WordHand.#rangeOf(list).getHtml(); await context.sync();
      const h = String(html.value ?? '');
      return this.#envelope({ from, to, html: h.length > maxChars ? h.slice(0, maxChars) : h, truncated: h.length > maxChars });
    });
  }
  async #readComments(a) {
    this.#need('WordApi', '1.4', 'read_comments');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list, whole } = this.#pick(items, a, { must: false });
      const cs = whole ? context.document.body.getComments() : WordHand.#rangeOf(list).getComments();
      cs.load('items/id,items/content,items/authorName,items/creationDate,items/resolved'); await context.sync();
      const detail = cs.items.map((c) => { const r = c.getRange(); r.load('text'); const rs = c.replies; rs.load('items/content,items/authorName,items/creationDate'); return { c, r, rs }; });
      await context.sync();
      return this.#envelope({ from, to, count: detail.length, comments: detail.map(({ c, r, rs }) => ({ id: c.id, author: c.authorName, date: c.creationDate, on: clip(r.text, 80), text: c.content, resolved: c.resolved, replies: rs.items.map((x) => ({ author: x.authorName, date: x.creationDate, text: x.content })) })) });
    });
  }
  async #readTrackedChanges(a) {
    this.#need('WordApi', '1.6', 'read_tracked_changes');
    const limit = int(a, 'limit') ?? 100;
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list, whole } = this.#pick(items, a, { must: false });
      const tc = whole ? context.document.body.getTrackedChanges() : WordHand.#rangeOf(list).getTrackedChanges();
      tc.load('items/type,items/author,items/date,items/text'); context.document.load('changeTrackingMode'); await context.sync();
      return this.#envelope({ from, to, mode: context.document.changeTrackingMode, count: tc.items.length, changes: tc.items.slice(0, limit).map((c) => ({ type: c.type, author: c.author, date: c.date, text: clip(c.text, 120) })) });
    });
  }
  async #describeStyle() {
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text,style,styleBuiltIn');
      const counts = {}; const headings = [];
      items.forEach((p, i) => { counts[p.style] = (counts[p.style] ?? 0) + 1; if (/^Heading|^Title/.test(p.styleBuiltIn ?? '') && headings.length < 12) headings.push({ paragraph: i + 1, style: p.style, text: clip(p.text, 60) }); });
      const body = items.find((p) => p.styleBuiltIn === 'Normal' && p.text.trim()) ?? items[0];
      const head = items.find((p) => /^Heading1|^Heading2/.test(p.styleBuiltIn ?? ''));
      if (body) body.font.load('name,size,color'); if (head) head.font.load('name,size,bold,color');
      await context.sync();
      return this.#envelope({ styles: counts, body_font: body ? { name: body.font.name, size: body.font.size, color: body.font.color } : null, heading_font: head ? { name: head.font.name, size: head.font.size, bold: head.font.bold, color: head.font.color } : null, headings, paragraphs: items.length });
    });
  }
  async #snapshot(a) {
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list } = this.#pick(items, a, { must: false });
      const ooxml = WordHand.#rangeOf(list).getOoxml(); await context.sync();
      const id = `snap-${this.snapshots.size + 1}-${Math.random().toString(36).slice(2, 6)}`;
      this.snapshots.set(id, { from, to, ooxml: ooxml.value, texts: list.map((p) => p.text) });
      return this.#envelope({ snapshot: id, from, to, paragraphs: list.length });
    });
  }
  async #readTags() {
    return this.runner(async (context) => {
      const props = context.document.properties.customProperties; props.load('items/key,items/value,items/type'); await context.sync();
      const tags = props.items.filter((p) => String(p.key).startsWith('MAGI.') && !String(p.key).startsWith(FIX_PREFIX) && p.key !== DOC_PROPERTY_KEY).map((p) => ({ key: p.key, value: tagText(p) }));
      return this.#envelope({ tags, count: tags.length });
    });
  }
  async #readSuggestions() {
    this.#need('WordApi', '1.4', 'read_suggestions');
    return this.runner(async (context) => {
      const st = context.document.settings; st.load('items/key,items/value'); await context.sync();
      const out = st.items.filter((s) => String(s.key).startsWith(FIX_PREFIX)).map((s) => WordHand.decodeSuggestion(s.key, s.value));
      return this.#envelope({ scope: 'document', count: out.length, suggestions: out });
    });
  }
  static decodeSuggestion(key, value) {
    let body = null; try { body = typeof value === 'string' ? JSON.parse(value) : value; } catch { body = null; }
    if (!body || typeof body !== 'object' || typeof body.what !== 'string') return { key, what: '읽을 수 없는 제안입니다', broken: true, appliable: false };
    const fix = body.fix && typeof body.fix === 'object' && body.fix.tool ? body.fix : null;
    return { key, what: body.what, why: body.why ?? '', paragraph: body.paragraph ?? null, fix, appliable: Boolean(fix && FIX_TOOLS.includes(fix.tool)) };
  }

  // ── 쓰기 ──
  async #insertParagraphs(a) {
    const lines = arr(a, 'lines'); if (!lines || lines.length === 0) refuse('lines 가 비었습니다 — 문단마다 한 줄인 문자열 배열');
    const style = str(a, 'style');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { p, where, said } = this.#anchor(items, a);
      const made = [];
      let cursor = p;
      for (const line of lines) {
        const text = String(line ?? '');
        let np;
        if (cursor) np = cursor.insertParagraph(text, where === 'Before' && made.length === 0 ? 'Before' : 'After');
        else np = context.document.body.insertParagraph(text, where === 'Start' && made.length === 0 ? 'Start' : made.length === 0 ? 'End' : 'After');
        applyStyle(np, style);
        made.push(np); cursor = np;
      }
      await context.sync();
      await detachInherited(context, made); this.#mutated();
      const after = await this.#paras(context, 'text');
      const firstText = String(lines[0] ?? '');
      let first = after.findIndex((q, i) => q.text === firstText && (p == null || i > 0)) + 1;
      if (first < 1) first = null;
      return this.#envelope({ inserted: lines.length, from: first, to: first ? first + lines.length - 1 : null, style: style ?? null }, [`${said} 문단 ${lines.length}개를 넣었습니다${style ? ` (${style})` : ''}${first ? ` — 문단 ${first}${lines.length > 1 ? `–${first + lines.length - 1}` : ''}` : ''}`]);
    });
  }
  async #replaceParagraph(a) {
    const n = int(a, 'paragraph') ?? int(a, 'para') ?? refuse('paragraph 가 없습니다'); const text = String(a.text ?? a.content ?? refuse('text 가 없습니다'));
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      if (n < 1 || n > items.length) refuse(`문서에 ${n}번 문단이 없습니다 — 문단 ${items.length}개`);
      const before = items[n - 1].text;
      items[n - 1].insertText(text, 'Replace'); await context.sync(); this.#mutated();
      return this.#envelope({ paragraph: n, before: clip(before, 80), after: clip(text, 80) }, [`문단 ${n}: 「${clip(before, 40)}」 → 「${clip(text, 40)}」`]);
    });
  }
  async #deleteParagraphs(a) {
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list } = this.#pick(items, a);
      if (list.length >= items.length) refuse('본문의 문단을 전부 지울 수는 없습니다 — 하나는 남겨야 합니다');
      for (const p of list) p.delete();
      await context.sync(); this.#mutated();
      return this.#envelope({ from, to, deleted: list.length }, [`문단 ${from}${to > from ? `–${to}` : ''} 을 지웠습니다 (${list.length}개) — 되돌릴 수 없습니다`]);
    });
  }
  async #setStyle(a) {
    const style = str(a, 'style'); const builtin = str(a, 'builtin');
    if (!style && !builtin) refuse('style 이나 builtin 이 있어야 합니다 — 예: builtin "Heading2"');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list } = this.#pick(items, a);
      for (const p of list) { if (builtin) p.styleBuiltIn = builtin; else applyStyle(p, style); }
      await context.sync(); this.#mutated();
      return this.#envelope({ from, to, style: builtin ?? style }, [`문단 ${from}${to > from ? `–${to}` : ''} 에 스타일 「${builtin ?? style}」`]);
    });
  }
  async #targets(context, a, items) {
    const { from, to, list } = this.#pick(items, a);
    const text = str(a, 'text');
    if (!text) return { from, to, ranges: list.map((p) => p.getRange('Whole')), said: `문단 ${from}${to > from ? `–${to}` : ''}` };
    const found = list.map((p) => p.search(text, { matchCase: true })); for (const f of found) f.load('items');
    await context.sync();
    const ranges = found.flatMap((f) => f.items);
    if (ranges.length === 0) refuse(`문단 ${from}${to > from ? `–${to}` : ''} 에 「${clip(text, 40)}」 가 없습니다`);
    return { from, to, ranges, said: `문단 ${from}${to > from ? `–${to}` : ''} 의 「${clip(text, 30)}」 ${ranges.length}곳` };
  }
  async #formatText(a) {
    const b = bool(a, 'bold'); const i = bool(a, 'italic'); const s = bool(a, 'strike'); const u = str(a, 'underline'); const size = num(a, 'size');
    const color = hex(a, 'color') ?? hex(a, 'font_color'); const hl = str(a, 'highlight'); const font = str(a, 'font'); const clear = bool(a, 'clear');
    const words = [b != null && (b ? '굵게' : '굵게 해제'), i != null && (i ? '기울임' : '기울임 해제'), s != null && (s ? '취소선' : '취소선 해제'), u && `밑줄 ${u}`, size != null && `크기 ${size}`, color && `색 ${color}`, hl && `형광 ${hl}`, font && `글꼴 ${font}`, clear && '직접 서식 지움'].filter(Boolean);
    if (words.length === 0) refuse('바꿀 것이 없습니다 — bold·italic·underline·strike·size·color·highlight·font·clear 중 하나');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { ranges, said } = await this.#targets(context, a, items);
      for (const r of ranges) {
        const f = r.font;
        if (clear) { f.bold = false; f.italic = false; f.underline = 'None'; f.strikeThrough = false; f.highlightColor = null; }
        if (b != null) f.bold = b; if (i != null) f.italic = i; if (s != null) f.strikeThrough = s; if (u) f.underline = u;
        if (size != null) f.size = size; if (color) f.color = color; if (hl) f.highlightColor = hl === 'none' ? null : hl; if (font) f.name = font;
      }
      await context.sync(); this.#mutated();
      return this.#envelope({ targets: ranges.length }, [`${said}: ${words.join(', ')}`]);
    });
  }
  async #formatParagraph(a) {
    const align = str(a, 'align'); const sb = num(a, 'space_before'); const sa = num(a, 'space_after'); const ls = num(a, 'line_spacing');
    const fi = num(a, 'first_line_indent'); const li = num(a, 'left_indent'); const ri = num(a, 'right_indent');
    const words = [align && `정렬 ${align}`, sb != null && `앞 ${sb}pt`, sa != null && `뒤 ${sa}pt`, ls != null && `줄 간격 ${ls}pt`, fi != null && `첫 줄 들여쓰기 ${fi}pt`, li != null && `왼쪽 ${li}pt`, ri != null && `오른쪽 ${ri}pt`].filter(Boolean);
    if (words.length === 0) refuse('바꿀 것이 없습니다 — align·space_before·space_after·line_spacing·first_line_indent·left_indent·right_indent 중 하나');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list } = this.#pick(items, a);
      for (const p of list) { if (align) p.alignment = align; if (sb != null) p.spaceBefore = sb; if (sa != null) p.spaceAfter = sa; if (ls != null) p.lineSpacing = ls; if (fi != null) p.firstLineIndent = fi; if (li != null) p.leftIndent = li; if (ri != null) p.rightIndent = ri; }
      await context.sync(); this.#mutated();
      return this.#envelope({ from, to }, [`문단 ${from}${to > from ? `–${to}` : ''}: ${words.join(', ')}`]);
    });
  }
  async #insertTable(a) {
    const values = arr(a, 'values'); if (!values || values.length === 0 || !values.every((r) => Array.isArray(r))) refuse('values 는 줄마다 배열인 2차원 배열이어야 합니다');
    const cols = Math.max(...values.map((r) => r.length)); const rows = values.map((r) => [...r.map((v) => String(v ?? '')), ...Array(cols - r.length).fill('')]);
    const hasHeader = bool(a, 'has_header') ?? true; const style = str(a, 'table_style');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { p, where, said } = this.#anchor(items, a);
      const t = p ? p.insertTable(rows.length, cols, where === 'Before' ? 'Before' : 'After', rows) : context.document.body.insertTable(rows.length, cols, where === 'Start' ? 'Start' : 'End', rows);
      if (hasHeader) t.headerRowCount = 1; if (style) t.styleBuiltIn = style;
      await context.sync(); this.#mutated();
      const ts = context.document.body.tables; ts.load('items'); await context.sync();
      const all = ts.items.map((x) => x.getRange()); const me = t.getRange(); const cmp = all.map((r) => r.compareLocationWith(me)); await context.sync();
      const no = cmp.findIndex((c) => c.value === 'Equal') + 1;
      return this.#envelope({ table: no || null, rows: rows.length, columns: cols, has_header: hasHeader, style: style ?? null }, [`${said} ${rows.length}×${cols} 표를 넣었습니다${no ? ` — 표 ${no}` : ''}${style ? ` (${style})` : ''}`]);
    });
  }
  async #setTableCells(a) {
    const cells = arr(a, 'cells'); if (!cells || cells.length === 0) refuse('cells 가 비었습니다 — [{row, column, value}]');
    return this.runner(async (context) => {
      const t = await this.#table(context, a); t.load('rowCount,values'); await context.sync();
      const cols = t.values[0]?.length ?? 0;
      for (const c of cells) {
        const r = int(c, 'row'); const col = int(c, 'column') ?? int(c, 'col');
        if (r == null || col == null || r < 0 || col < 0 || r >= t.rowCount || col >= cols) refuse(`표 밖의 칸입니다 — ${JSON.stringify(c)} (표는 ${t.rowCount}×${cols}, 0부터)`);
        t.getCell(r, col).value = String(c.value ?? '');
      }
      await context.sync(); this.#mutated();
      return this.#envelope({ table: int(a, 'table'), written: cells.length }, [`표 ${int(a, 'table')} 의 칸 ${cells.length}개를 적었습니다`]);
    });
  }
  async #addTableRows(a) {
    const rows = arr(a, 'rows'); if (!rows || rows.length === 0 || !rows.every((r) => Array.isArray(r))) refuse('rows 는 줄마다 배열인 2차원 배열이어야 합니다');
    const at = str(a, 'at') === 'start' ? 'Start' : 'End';
    return this.runner(async (context) => {
      const t = await this.#table(context, a); t.load('rowCount,values'); await context.sync();
      const cols = t.values[0]?.length ?? 0;
      const padded = rows.map((r) => [...r.map((v) => String(v ?? '')), ...Array(Math.max(0, cols - r.length)).fill('')].slice(0, cols));
      t.addRows(at, padded.length, padded); await context.sync(); this.#mutated();
      return this.#envelope({ table: int(a, 'table'), added: padded.length, rows: t.rowCount + padded.length }, [`표 ${int(a, 'table')} ${at === 'End' ? '끝' : '앞'}에 행 ${padded.length}개를 넣었습니다`]);
    });
  }
  async #deleteTable(a) {
    return this.runner(async (context) => {
      const t = await this.#table(context, a); t.load('rowCount'); await context.sync();
      const rows = t.rowCount; t.delete(); await context.sync(); this.#mutated();
      return this.#envelope({ table: int(a, 'table'), rows }, [`표 ${int(a, 'table')} (${rows}행)을 지웠습니다 — 되돌릴 수 없습니다`]);
    });
  }
  async #formatTable(a) {
    const style = str(a, 'table_style'); const header = bool(a, 'header_row'); const br = bool(a, 'banded_rows'); const bc = bool(a, 'banded_columns'); const align = str(a, 'align'); const widths = arr(a, 'widths');
    const words = [style && `스타일 ${style}`, header != null && (header ? '머리글 행' : '머리글 행 해제'), br != null && '줄무늬 행', bc != null && '줄무늬 열', align && `정렬 ${align}`, widths && `열 너비 ${widths.join('/')}`].filter(Boolean);
    if (words.length === 0) refuse('바꿀 것이 없습니다 — table_style·header_row·banded_rows·banded_columns·align·widths 중 하나');
    return this.runner(async (context) => {
      const t = await this.#table(context, a);
      if (style) t.styleBuiltIn = style; if (header != null) t.headerRowCount = header ? 1 : 0; if (br != null) t.styleBandedRows = br; if (bc != null) t.styleBandedColumns = bc; if (align) t.alignment = align;
      if (widths) { t.load('values'); await context.sync(); const cols = t.values[0]?.length ?? 0; for (let c = 0; c < Math.min(cols, widths.length); c += 1) { const w = Number(widths[c]); if (Number.isFinite(w)) t.getCell(0, c).columnWidth = w; } }
      await context.sync(); this.#mutated();
      return this.#envelope({ table: int(a, 'table') }, [`표 ${int(a, 'table')}: ${words.join(', ')}`]);
    });
  }
  /** 칸 고르기 — cells 목록이거나 rows/columns 사각형(0부터, 양끝 포함). 표 크기를 넘으면 거절. */
  static #cellTargets(a, rowCount, cols) {
    const span = (v, max, what) => {
      if (v == null) return [0, max - 1];
      const arr = Array.isArray(v) ? v : [v];
      const lo = int({ v: arr[0] }, 'v') ?? 0; const hi = int({ v: arr[1] ?? arr[0] }, 'v') ?? lo;
      if (lo < 0 || hi >= max || lo > hi) refuse(`${what} 범위가 표 밖입니다 — ${JSON.stringify(v)} (0부터 ${max - 1}까지)`);
      return [lo, hi];
    };
    const cells = arr(a, 'cells');
    if (cells && cells.length) {
      return cells.map((c) => { const r = int(c, 'row'); const col = int(c, 'column') ?? int(c, 'col'); if (r == null || col == null || r < 0 || col < 0 || r >= rowCount || col >= cols) refuse(`표 밖의 칸입니다 — ${JSON.stringify(c)} (표는 ${rowCount}×${cols}, 0부터)`); return [r, col]; });
    }
    const [r0, r1] = span(a.rows, rowCount, 'rows'); const [c0, c1] = span(a.columns, cols, 'columns');
    const out = []; for (let r = r0; r <= r1; r += 1) for (let c = c0; c <= c1; c += 1) out.push([r, c]);
    return out;
  }
  async #formatTableCells(a) {
    const fill = hex(a, 'fill', true); const color = hex(a, 'color'); const b = bool(a, 'bold'); const i = bool(a, 'italic'); const size = num(a, 'size');
    const align = str(a, 'align'); const valign = str(a, 'valign'); const width = num(a, 'width');
    const words = [fill && `채우기 ${fill}`, color && `글자색 ${color}`, b != null && (b ? '굵게' : '굵게 해제'), i != null && (i ? '기울임' : '기울임 해제'), size != null && `크기 ${size}`, align && `가로 ${align}`, valign && `세로 ${valign}`, width != null && `너비 ${width}pt`].filter(Boolean);
    if (words.length === 0) refuse('바꿀 것이 없습니다 — fill·color·bold·italic·size·align·valign·width 중 하나');
    return this.runner(async (context) => {
      const t = await this.#table(context, a); t.load('rowCount,values'); await context.sync();
      const cols = t.values[0]?.length ?? 0;
      const targets = WordHand.#cellTargets(a, t.rowCount, cols);
      for (const [r, c] of targets) {
        const cell = t.getCell(r, c);
        if (fill) cell.shadingColor = fill === 'none' ? '#FFFFFF' : fill;
        if (align) cell.horizontalAlignment = align; if (valign) cell.verticalAlignment = valign; if (width != null) cell.columnWidth = width;
        const f = cell.body.font; if (color) f.color = color; if (b != null) f.bold = b; if (i != null) f.italic = i; if (size != null) f.size = size;
      }
      await context.sync(); this.#mutated();
      return this.#envelope({ table: int(a, 'table'), cells: targets.length }, [`표 ${int(a, 'table')} 의 칸 ${targets.length}개: ${words.join(', ')}`]);
    });
  }
  async #editTable(a) {
    const delRows = arr(a, 'delete_rows'); const delCols = arr(a, 'delete_columns'); const add = a.add_columns && typeof a.add_columns === 'object' ? a.add_columns : null; const merge = a.merge && typeof a.merge === 'object' ? a.merge : null;
    if (!delRows?.length && !delCols?.length && !add && !merge) refuse('할 일이 없습니다 — delete_rows·delete_columns·add_columns·merge 중 하나');
    if (merge) this.#need('WordApi', '1.4', 'edit_table{merge}');
    return this.runner(async (context) => {
      const t = await this.#table(context, a); t.load('rowCount,values'); await context.sync();
      let rows = t.rowCount; let cols = t.values[0]?.length ?? 0; const done = [];
      if (merge) {
        const fr = int(merge, 'from_row') ?? 0; const fc = int(merge, 'from_column') ?? 0; const tr = int(merge, 'to_row') ?? fr; const tc = int(merge, 'to_column') ?? fc;
        if (fr < 0 || fc < 0 || tr >= rows || tc >= cols || fr > tr || fc > tc) refuse(`merge 가 표 밖입니다 — ${JSON.stringify(merge)} (표는 ${rows}×${cols}, 0부터)`);
        t.mergeCells(fr, fc, tr, tc); await context.sync(); done.push(`(${fr},${fc})–(${tr},${tc}) 병합`);
      }
      if (add) {
        const count = int(add, 'count') ?? (Array.isArray(add.values) ? add.values.length : 1); const values = Array.isArray(add.values) ? add.values.map((col) => (Array.isArray(col) ? col.map((v) => String(v ?? '')) : [String(col ?? '')])) : undefined;
        // Word 의 values 는 행×열이다 — 사람은 열마다 위→아래로 준다. 표의 행 수에 맞춰 뒤집는다.
        const byRow = values ? Array.from({ length: rows }, (_, r) => values.map((col) => col[r] ?? '')) : undefined;
        const at = add.at ?? 'end';
        if (at === 'end' || at === 'start') { t.addColumns(at === 'end' ? 'End' : 'Start', count, byRow); }
        else { const idx = int({ v: at }, 'v'); if (idx == null || idx < 0 || idx >= cols) refuse(`add_columns.at 이 표 밖입니다 — ${at} (0부터 ${cols - 1}까지, 또는 end·start)`); t.getCell(0, idx).insertColumns('After', count, byRow); }
        await context.sync(); cols += count; done.push(`열 ${count}개 추가(${at})`);
      }
      if (delCols?.length) {
        const idxs = [...new Set(delCols.map((v) => int({ v }, 'v')))].sort((x, y) => y - x);
        for (const c of idxs) { if (c == null || c < 0 || c >= cols) refuse(`delete_columns 가 표 밖입니다 — ${c} (0부터 ${cols - 1}까지)`); t.deleteColumns(c, 1); }
        await context.sync(); cols -= idxs.length; done.push(`열 ${idxs.length}개 삭제`);
      }
      if (delRows?.length) {
        const idxs = [...new Set(delRows.map((v) => int({ v }, 'v')))].sort((x, y) => y - x);
        for (const r of idxs) { if (r == null || r < 0 || r >= rows) refuse(`delete_rows 가 표 밖입니다 — ${r} (0부터 ${rows - 1}까지)`); t.deleteRows(r, 1); }
        await context.sync(); rows -= idxs.length; done.push(`행 ${idxs.length}개 삭제`);
      }
      this.#mutated();
      return this.#envelope({ table: int(a, 'table'), rows, columns: cols }, [`표 ${int(a, 'table')}: ${done.join(', ')} — 이제 ${rows}×${cols}`]);
    });
  }
  async #insertList(a) {
    const items0 = arr(a, 'items'); if (!items0 || items0.length === 0) refuse('items 가 비었습니다');
    const kind = str(a, 'kind') ?? 'bulleted'; const levels = arr(a, 'levels') ?? [];
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { p, where, said } = this.#anchor(items, a);
      const first = p ? p.insertParagraph(String(items0[0]), where === 'Before' ? 'Before' : 'After') : context.document.body.insertParagraph(String(items0[0]), where === 'Start' ? 'Start' : 'End');
      await context.sync();
      await detachInherited(context, [first]); // 목록 항목 뒤에 넣은 문단은 그 목록을 물려받고, 그 위에 startNewList 는 GeneralException 이다(실물 2026-09-06)
      const list = first.startNewList(); list.load('id');
      await context.sync();
      if (kind === 'numbered') list.setLevelNumbering(0, 'Arabic', [0, '.']); else list.setLevelBullet(0, 'Solid');
      let cursor = first; const made = [first];
      for (let i = 1; i < items0.length; i += 1) { const np = cursor.insertParagraph(String(items0[i]), 'After'); made.push(np); cursor = np; }
      await context.sync();
      // 목록 항목 뒤에 넣은 문단은 Word 가 그 목록에 이어 붙인다 — 이미 항목인 문단에 attachToList 는 GeneralException 이다(실물 2026-09-06).
      // 그래서 붙이는 것은 안 붙은 것만, 단계는 항목이 된 뒤에 listItem 으로.
      await attachMissing(context, made, list.id);
      made.forEach((np, i) => { const lv = Number(levels[i] ?? 0) || 0; if (lv) np.listItem.level = lv; });
      await context.sync(); this.#mutated();
      return this.#envelope({ inserted: items0.length, kind }, [`${said} ${kind === 'numbered' ? '번호' : '글머리 기호'} 목록 ${items0.length}개를 넣었습니다`]);
    });
  }
  async #setList(a) {
    const kind = str(a, 'kind'); const level = int(a, 'level'); const detach = bool(a, 'detach') ?? false;
    if (!kind && level == null && !detach) refuse('kind·level·detach 중 하나가 있어야 합니다');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text,isListItem');
      const { from, to, list } = this.#pick(items, a);
      if (detach) { for (const p of list) if (p.isListItem) p.detachFromList(); await context.sync(); this.#mutated(); return this.#envelope({ from, to, detached: true }, [`문단 ${from}${to > from ? `–${to}` : ''} 을 목록에서 뺐습니다`]); }
      let listId = null;
      if (kind || !list[0].isListItem) { const l = list[0].isListItem ? list[0].list : list[0].startNewList(); l.load('id'); await context.sync(); listId = l.id; if (kind === 'numbered') l.setLevelNumbering(0, 'Arabic', [0, '.']); else if (kind) l.setLevelBullet(0, 'Solid'); }
      else { const l = list[0].list; l.load('id'); await context.sync(); listId = l.id; }
      await attachMissing(context, list, listId);
      if (level != null) { for (const p of list) { p.listItem.level = level; } }
      await context.sync(); this.#mutated();
      return this.#envelope({ from, to, kind: kind ?? null, level: level ?? null }, [`문단 ${from}${to > from ? `–${to}` : ''} 을 ${kind === 'numbered' ? '번호 ' : kind === 'bulleted' ? '글머리 기호 ' : ''}목록으로${level != null ? ` (단계 ${level})` : ''}`]);
    });
  }
  /** 본문의 그림 — 문서 순서, 1부터. 문단 번호는 그림이 앉은 문단의 글로 찾는다. */
  async #pictures(context, items) {
    const pics = context.document.body.inlinePictures; pics.load('items/width,items/height,items/altTextDescription'); await context.sync();
    const paras = pics.items.map((p) => { const q = p.paragraph; q.load('text'); return q; }); await context.sync();
    return pics.items.map((p, i) => ({ pic: p, number: i + 1, paragraph: (items.findIndex((x) => x.text === paras[i].text) + 1) || null, width: p.width, height: p.height, alt: p.altTextDescription ?? '' }));
  }
  async #listImages(a) {
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to } = this.#pick(items, a, { must: false });
      const all = await this.#pictures(context, items);
      const images = all.filter((p) => p.paragraph == null || (p.paragraph >= from && p.paragraph <= to)).map(({ number, paragraph, width, height, alt }) => ({ number, paragraph, width: Math.round(width), height: Math.round(height), alt }));
      return this.#envelope({ from, to, count: images.length, images });
    });
  }
  async #formatImage(a) {
    const n = int(a, 'image') ?? refuse('image 가 없습니다 — list_images 의 번호'); const w = num(a, 'width'); const h = num(a, 'height'); const alt = str(a, 'alt'); const align = str(a, 'align');
    const words = [w != null && `너비 ${w}pt`, h != null && `높이 ${h}pt`, alt != null && `대체 텍스트 「${clip(alt, 30)}」`, align && `정렬 ${align}`].filter(Boolean);
    if (words.length === 0) refuse('바꿀 것이 없습니다 — width·height·alt·align 중 하나');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const all = await this.#pictures(context, items);
      if (n < 1 || n > all.length) refuse(`그림 ${n}번이 없습니다 — 그림 ${all.length}개`);
      const { pic } = all[n - 1];
      if (w != null && h != null) { pic.lockAspectRatio = false; pic.width = w; pic.height = h; }
      else if (w != null) { pic.lockAspectRatio = true; pic.width = w; }
      else if (h != null) { pic.lockAspectRatio = true; pic.height = h; }
      if (alt != null) pic.altTextDescription = alt;
      if (align) pic.paragraph.alignment = align;
      pic.load('width,height'); await context.sync(); this.#mutated();
      return this.#envelope({ image: n, width: Math.round(pic.width), height: Math.round(pic.height) }, [`그림 ${n}: ${words.join(', ')} (지금 ${Math.round(pic.width)}×${Math.round(pic.height)}pt)`]);
    });
  }
  async #deleteImage(a) {
    const n = int(a, 'image') ?? refuse('image 가 없습니다 — list_images 의 번호');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const all = await this.#pictures(context, items);
      if (n < 1 || n > all.length) refuse(`그림 ${n}번이 없습니다 — 그림 ${all.length}개`);
      all[n - 1].pic.delete(); await context.sync(); this.#mutated();
      return this.#envelope({ image: n, deleted: true }, [`그림 ${n}번을 지웠습니다 — 문단은 그대로입니다`]);
    });
  }
  async #insertImage(a) {
    const b64 = str(a, 'image_base64'); if (!b64) refuse('그림 바이트가 안 왔습니다 — path 를 주면 헬퍼가 읽어 실어 줍니다');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { p, where, said } = this.#anchor(items, a);
      // 문단의 insertInlinePictureFromBase64 는 Replace·Start·End 만 받는다(Before/After 는 InvalidArgument — 실물 2026-09-06).
      // 그래서 그림은 제 문단을 하나 얻는다: 앞/뒤에 빈 문단을 넣고 그 첫머리에.
      const host = p ? p.insertParagraph('', where === 'Before' ? 'Before' : 'After') : context.document.body.insertParagraph('', where === 'Start' ? 'Start' : 'End');
      const pic = host.insertInlinePictureFromBase64(b64, 'Start');
      const w = num(a, 'width'); if (w != null) { pic.lockAspectRatio = true; pic.width = w; }
      const alt = str(a, 'alt'); pic.altTextDescription = alt ?? String(str(a, 'path') ?? '').split(/[\\/]/).pop();
      pic.load('width,height'); await context.sync(); this.#mutated();
      return this.#envelope({ width: pic.width, height: pic.height }, [`${said} 그림을 넣었습니다 (${Math.round(pic.width)}×${Math.round(pic.height)}pt)`]);
    });
  }
  /** 다른 .docx 를 이 문서에 넣는다 — 바이트는 헬퍼가 읽어 실어 준다(그림과 같은 길). */
  async #insertFile(a) {
    const b64 = str(a, 'file_base64'); if (!b64) refuse('파일 바이트가 안 왔습니다 — path 를 주면 헬퍼가 읽어 실어 줍니다');
    const name = str(a, 'file_name') ?? String(str(a, 'path') ?? '').split(/[\\/]/).pop();
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { p, where, said } = this.#anchor(items, a);
      // Paragraph.insertFileFromBase64 도 Replace·Start·End 뿐이다 — 빈 문단을 세우고 통째로 바꾼다.
      // 본문 끝/처음도 빈 문단을 세워 바꾼다 — body 의 End 에 바로 넣으면 첫 문단이 마지막 문단에 합쳐지고 수가 하나 모자란다(실물 2026-09-06).
      const host = p ? p.insertParagraph('', where === 'Before' ? 'Before' : 'After') : context.document.body.insertParagraph('', where === 'Start' ? 'Start' : 'End');
      host.styleBuiltIn = 'Normal'; // 끝 문단의 제목 스타일을 물려받지 않게 — 넣는 문서의 문단 서식이 없으면 이 문단이 기준이다
      host.insertFileFromBase64(b64, 'Replace');
      await context.sync(); this.#mutated();
      const now = await this.#paras(context, 'text');
      return this.#envelope({ file: name, paragraphs_now: now.length, added: now.length - items.length }, [`${said} 「${name}」 을 넣었습니다 — 문단 ${now.length - items.length}개`]);
    });
  }
  /** 문단 덩어리를 옮긴다 — OOXML 로 떠서 새 자리에 넣고 원본을 지운다. 서식·표가 같이 간다. */
  async #moveParagraphs(a) {
    const after = int(a, 'after'); const before = int(a, 'before'); const at = str(a, 'at');
    if (after == null && before == null && !at) refuse('어디로 옮길지가 없습니다 — after·before·at(start/end) 중 하나');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list } = this.#pick(items, a);
      const target = after ?? before;
      if (target != null && target >= from && target <= to) refuse(`옮길 자리(문단 ${target})가 옮기는 덩어리(${from}–${to}) 안입니다`);
      const { p, where, said } = this.#anchor(items, { after, before, at });
      const texts = list.map((q) => q.text);
      const ooxml = WordHand.#rangeOf(list).getOoxml(); await context.sync();
      // Paragraph.insertOoxml 은 Replace·Start·End 만 받는다(Before/After 는 InvalidArgument — 실물 2026-09-06). 빈 문단을
      // 앞/뒤에 세우고 그것을 통째로 바꾼다.
      if (p) p.insertParagraph('', where === 'Before' ? 'Before' : 'After').insertOoxml(ooxml.value, 'Replace');
      else context.document.body.insertOoxml(ooxml.value, where === 'Start' ? 'Start' : 'End');
      await context.sync();
      for (const q of list) q.delete();
      await context.sync(); this.#mutated();
      // 새 번호: 옮긴 덩어리의 첫 글이 지금 어디 섰나. 같은 글이 둘이면 옮긴 방향 쪽 것.
      const now = await this.#paras(context, 'text');
      const hits = now.map((q, i) => (q.text === texts[0] ? i + 1 : 0)).filter(Boolean);
      const dir = where === 'Start' || (target != null && target < from) ? 'first' : 'last';
      const nf = hits.length ? (dir === 'first' ? hits[0] : hits[hits.length - 1]) : null;
      return this.#envelope({ from, to, moved: list.length, now_from: nf, now_to: nf ? nf + list.length - 1 : null, total: now.length },
        [`문단 ${from}${to > from ? `–${to}` : ''} 을 ${said} 옮겼습니다${nf ? ` — 지금은 문단 ${nf}${list.length > 1 ? `–${nf + list.length - 1}` : ''}` : ''}`]);
    });
  }
  // 스타일 자체를 고친다(1.5) — 그 스타일의 문단이 지금도, 나중에도 전부 바뀐다.
  async #setStyleFormat(a) {
    this.#need('WordApi', '1.5', 'set_style_format');
    const given = String(need(a, 'style')); const create = bool(a, 'create') ?? false;
    const f = { name: str(a, 'font'), size: num(a, 'size'), bold: bool(a, 'bold'), italic: bool(a, 'italic'), color: hex(a, 'color') };
    const pf = { alignment: str(a, 'align'), spaceBefore: num(a, 'space_before'), spaceAfter: num(a, 'space_after'), lineSpacing: num(a, 'line_spacing'), firstLineIndent: num(a, 'first_line_indent'), leftIndent: num(a, 'left_indent') };
    const words = [f.name && `글꼴 ${f.name}`, f.size != null && `크기 ${f.size}`, f.bold != null && (f.bold ? '굵게' : '굵게 해제'), f.italic != null && (f.italic ? '기울임' : '기울임 해제'), f.color && `색 ${f.color}`,
      pf.alignment && `정렬 ${pf.alignment}`, pf.spaceBefore != null && `앞 ${pf.spaceBefore}pt`, pf.spaceAfter != null && `뒤 ${pf.spaceAfter}pt`, pf.lineSpacing != null && `줄 간격 ${pf.lineSpacing}pt`, pf.firstLineIndent != null && `첫 줄 들여쓰기 ${pf.firstLineIndent}pt`, pf.leftIndent != null && `왼쪽 들여쓰기 ${pf.leftIndent}pt`].filter(Boolean);
    if (words.length === 0) refuse('바꿀 것이 없습니다 — font·size·bold·italic·color·align·space_before·space_after·line_spacing·first_line_indent·left_indent 중 하나');
    return this.runner(async (context) => {
      const styles = context.document.getStyles(); styles.load('items/nameLocal,items/type'); await context.sync();
      let target = styles.items.find((s) => s.nameLocal === given);
      const key = given.replace(/[\s_-]/g, '').toLowerCase();
      const builtin = BUILTIN_PARAGRAPH_STYLES.find((b) => b.toLowerCase() === key);
      const items = await this.#paras(context, 'text,style,styleBuiltIn');
      if (!target && builtin) {
        // 내장 이름의 **현지 이름**을 찾는다: 그 스타일인 문단이 있으면 거기서, 없으면 문단 하나를 잠깐 세워 읽는다.
        let local = items.find((p) => p.styleBuiltIn === builtin)?.style;
        if (!local) { const tmp = context.document.body.insertParagraph('', 'End'); tmp.styleBuiltIn = builtin; tmp.load('style'); await context.sync(); local = tmp.style; tmp.delete(); await context.sync(); }
        target = styles.items.find((s) => s.nameLocal === local);
      }
      if (!target && create) { target = context.document.addStyle(given, 'Paragraph'); target.load('nameLocal'); await context.sync(); }
      if (!target) refuse(`「${given}」 스타일이 없습니다 — 이 문서의 스타일: ${styles.items.slice(0, 24).map((s) => s.nameLocal).join(', ')}${styles.items.length > 24 ? ' …' : ''}. 새로 만들려면 create: true`);
      if (f.name) target.font.name = f.name; if (f.size != null) target.font.size = f.size; if (f.bold != null) target.font.bold = f.bold; if (f.italic != null) target.font.italic = f.italic; if (f.color) target.font.color = f.color;
      const pfmt = target.paragraphFormat;
      if (pf.alignment) pfmt.alignment = pf.alignment; if (pf.spaceBefore != null) pfmt.spaceBefore = pf.spaceBefore; if (pf.spaceAfter != null) pfmt.spaceAfter = pf.spaceAfter;
      if (pf.lineSpacing != null) pfmt.lineSpacing = pf.lineSpacing; if (pf.firstLineIndent != null) pfmt.firstLineIndent = pf.firstLineIndent; if (pf.leftIndent != null) pfmt.leftIndent = pf.leftIndent;
      await context.sync(); this.#mutated();
      const local = target.nameLocal; const affected = items.filter((p) => p.style === local).length;
      return this.#envelope({ style: local, builtin: builtin ?? null, affected, created: Boolean(create && !styles.items.some((s) => s.nameLocal === local)) }, [`스타일 「${local}」: ${words.join(', ')} — 문단 ${affected}개에 걸립니다`]);
    });
  }
  // 각주·미주(1.5). 번호는 종류 안에서 문서 순서(1부터) — Word 가 매기는 그 번호다.
  async #notes(context, kind) {
    const col = kind === 'endnote' ? context.document.body.endnotes : context.document.body.footnotes;
    col.load('items'); await context.sync();
    return col.items;
  }
  async #readFootnotes(a) {
    this.#need('WordApi', '1.5', 'read_footnotes');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to } = this.#pick(items, a, { must: false });
      const out = [];
      for (const kind of ['footnote', 'endnote']) {
        const notes = await this.#notes(context, kind);
        const paras = notes.map((n) => { const p = n.reference.paragraphs.getFirst(); p.load('text'); n.body.load('text'); n.reference.load('text'); return p; });
        await context.sync();
        // 실물: reference 의 text 는 표식 문자(\u0002) 하나고, 본문도 그 문자로 시작한다(2026-09-06). 걸린 글은 그 문단에서
        // 표식 바로 앞 글로 보인다 — 같은 문단에 여럿이면 순서대로 n 번째 표식.
        const seen = new Map();
        notes.forEach((n, i) => {
          const at = items.findIndex((p) => p.text === paras[i].text) + 1;
          const t = paras[i].text; const nth = (seen.get(at) ?? 0) + 1; seen.set(at, nth);
          let k = -1; for (let j = 0; j < nth; j += 1) k = t.indexOf('\u0002', k + 1);
          const on = k > 0 ? t.slice(Math.max(0, k - 30), k).replace(/\u0002/g, '').trim() : '';
          if (at >= from && at <= to) out.push({ number: i + 1, kind, paragraph: at || null, on: clip(on, 40), text: clip(n.body.text.replace(/[\u0002\s]+/, ''), 200) });
        });
      }
      return this.#envelope({ from, to, count: out.length, notes: out });
    });
  }
  async #insertFootnote(a) {
    this.#need('WordApi', '1.5', 'insert_footnote');
    const note = String(need(a, 'note')); const kind = str(a, 'kind') ?? 'footnote';
    const n = int(a, 'paragraph') ?? int(a, 'from') ?? refuse('paragraph 가 없습니다 — 각주가 걸릴 문단 번호');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, ranges, said } = await this.#targets(context, { ...a, from: n, to: n }, items);
      const at = str(a, 'text') ? ranges[0] : items[from - 1].getRange('End');
      const made = kind === 'endnote' ? at.insertEndnote(note) : at.insertFootnote(note);
      void made; await context.sync(); this.#mutated();
      const all = await this.#notes(context, kind);
      return this.#envelope({ paragraph: from, kind, number: all.length }, [`${said} 에 ${kind === 'endnote' ? '미주' : '각주'}를 달았습니다 — 「${clip(note, 40)}」`]);
    });
  }
  async #deleteFootnote(a) {
    this.#need('WordApi', '1.5', 'delete_footnote');
    const number = int(a, 'number') ?? refuse('number 가 없습니다 — read_footnotes 의 번호'); const kind = str(a, 'kind') ?? 'footnote';
    return this.runner(async (context) => {
      const notes = await this.#notes(context, kind);
      if (number < 1 || number > notes.length) refuse(`${kind === 'endnote' ? '미주' : '각주'} ${number}번이 없습니다 — ${notes.length}개`);
      notes[number - 1].delete(); await context.sync(); this.#mutated();
      return this.#envelope({ number, kind, deleted: true }, [`${kind === 'endnote' ? '미주' : '각주'} ${number}번을 지웠습니다 — 글은 그대로입니다`]);
    });
  }
  async #insertField(a) {
    this.#need('WordApi', '1.5', 'insert_field');
    const { pieces, said: what } = fieldPieces(a);
    const which = str(a, 'which'); const align = str(a, 'align');
    return this.runner(async (context) => {
      let host; let said;
      if (which) {
        const section = int(a, 'section') ?? 1; const kind = str(a, 'kind') ?? 'Primary';
        const secs = context.document.sections; secs.load('items'); await context.sync();
        if (section < 1 || section > secs.items.length) refuse(`문서에 ${section}번 구역이 없습니다 — 구역 ${secs.items.length}개`);
        const s = secs.items[section - 1]; const body = which === 'header' ? s.getHeader(kind) : s.getFooter(kind);
        host = body.insertParagraph('', 'End'); said = `구역 ${section} ${which === 'header' ? '머리글' : '바닥글'}(${kind})에`;
      } else {
        const items = await this.#paras(context, 'text');
        const { p, where, said: at } = this.#anchor(items, a);
        host = p ? p.insertParagraph('', where === 'Before' ? 'Before' : 'After') : context.document.body.insertParagraph('', where === 'Start' ? 'Start' : 'End');
        said = at;
      }
      if (align) host.alignment = align;
      if (!which) host.styleBuiltIn = 'Normal'; // 끝 문단의 제목 스타일을 물려받지 않게 — 필드 줄은 본문이다
      // 실물 Word(16.x Mac, WordApi 1.9)에 Paragraph.insertField 가 없다 — Range 에만 있다. 그리고 필드의 result
      // 범위에 이어 붙이면 첫 필드 뒤의 조각이 전부 사라졌다(2026-09-06 실물). 그래서 글을 통째로 먼저 적고, 자리
      // 표시 글자(`{page}` 같은)를 찾아 그 범위를 필드로 **바꾼다** — 순서는 글이 지키고, 필드는 제자리에 선다.
      const marks = pieces.map((piece, i) => (piece.text != null ? piece.text : `\u2063F${i}\u2063`)).join('');
      host.insertText(marks, 'Start');
      await context.sync();
      const found = pieces.map((piece, i) => (piece.text != null ? null : host.search(`\u2063F${i}\u2063`, { matchCase: true })));
      for (const r of found) if (r) r.load('items');
      await context.sync();
      const fields = [];
      pieces.forEach((piece, i) => {
        if (piece.text != null) return;
        const hit = found[i].items[0];
        if (!hit) refuse(`필드 자리를 못 찾았습니다 — ${piece.name}`);
        fields.push(hit.insertField('Replace', piece.type, piece.code, false));
      });
      await context.sync();
      for (const f of fields) f.updateResult(); // 목차는 갱신해야 채워진다
      await context.sync(); this.#mutated();
      return this.#envelope({ fields: fields.length, which: which ?? null }, [`${said} ${what}`]);
    });
  }
  async #insertBreak(a) {
    const n = int(a, 'paragraph') ?? int(a, 'para') ?? refuse('paragraph 가 없습니다'); const kind = str(a, 'kind') ?? 'page';
    const type = { page: 'Page', section: 'SectionNext', line: 'Line' }[kind] ?? refuse(`kind 는 page·section·line 중 하나 — ${kind}`);
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      if (n < 1 || n > items.length) refuse(`문서에 ${n}번 문단이 없습니다 — 문단 ${items.length}개`);
      items[n - 1].insertBreak(type, 'After'); await context.sync(); this.#mutated();
      return this.#envelope({ paragraph: n, kind }, [`문단 ${n} 뒤에 ${{ page: '쪽', section: '구역', line: '줄' }[kind]} 나누기`]);
    });
  }
  async #setHeaderFooter(a) {
    const which = str(a, 'which') ?? refuse('which 가 없습니다 — header 나 footer'); const text = String(a.text ?? refuse('text 가 없습니다'));
    const section = int(a, 'section') ?? 1; const kind = str(a, 'kind') ?? 'Primary'; const align = str(a, 'align');
    return this.runner(async (context) => {
      const secs = context.document.sections; secs.load('items'); await context.sync();
      if (section < 1 || section > secs.items.length) refuse(`문서에 ${section}번 구역이 없습니다 — 구역 ${secs.items.length}개`);
      const s = secs.items[section - 1]; const body = which === 'header' ? s.getHeader(kind) : s.getFooter(kind);
      body.clear(); const p = body.insertParagraph(text, 'Start'); if (align) p.alignment = align;
      await context.sync(); this.#mutated();
      return this.#envelope({ which, section, kind, text }, [`구역 ${section} ${which === 'header' ? '머리글' : '바닥글'}(${kind}) → 「${clip(text, 40)}」`]);
    });
  }
  async #setHyperlink(a) {
    const url = str(a, 'url');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { ranges, said } = await this.#targets(context, a, items);
      for (const r of ranges) r.hyperlink = url ?? '';
      await context.sync(); this.#mutated();
      return this.#envelope({ targets: ranges.length, url: url ?? null }, [url ? `${said} 에 링크 → ${url}` : `${said} 의 링크를 뗐습니다`]);
    });
  }
  async #replaceAll(a) {
    const find = String(need(a, 'find')); const replace = String(a.replace ?? refuse('replace 가 없습니다(빈 문자열은 됩니다)')); const limit = int(a, 'limit');
    const opts = { matchCase: bool(a, 'match_case') ?? false, matchWholeWord: bool(a, 'whole_word') ?? false, matchWildcards: false };
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list } = this.#pick(items, a, { must: false });
      const found = list.map((p) => p.search(find, opts)); for (const f of found) f.load('items'); await context.sync();
      let n = 0; const where = new Set();
      found.forEach((f, i) => { for (const r of f.items) { if (limit != null && n >= limit) return; r.insertText(replace, 'Replace'); n += 1; where.add(from + i); } });
      if (n === 0) refuse(`「${clip(find, 40)}」 가 문단 ${from}–${to} 에 없습니다`);
      await context.sync(); this.#mutated();
      return this.#envelope({ replaced: n, paragraphs: [...where] }, [`「${clip(find, 30)}」 → 「${clip(replace, 30)}」 ${n}곳 (문단 ${[...where].slice(0, 8).join(', ')}${where.size > 8 ? ' …' : ''})`]);
    });
  }
  async #addComment(a) {
    this.#need('WordApi', '1.4', 'add_comment');
    const comment = String(need(a, 'comment'));
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { ranges, said } = await this.#targets(context, a, items);
      const c = ranges[0].insertComment(comment); c.load('id'); await context.sync(); this.#mutated();
      return this.#envelope({ id: c.id, on: said }, [`${said} 에 메모를 달았습니다 — 「${clip(comment, 40)}」`]);
    });
  }
  async #comment(context, id) {
    const cs = context.document.body.getComments(); cs.load('items/id'); await context.sync();
    return cs.items.find((c) => String(c.id) === String(id)) ?? refuse(`id ${id} 인 메모가 없습니다 — read_comments 가 id 를 줍니다`);
  }
  async #replyComment(a) {
    this.#need('WordApi', '1.4', 'reply_comment');
    const id = String(need(a, 'id')); const text = String(need(a, 'text'));
    return this.runner(async (context) => { const c = await this.#comment(context, id); c.reply(text); await context.sync(); this.#mutated(); return this.#envelope({ id }, [`메모 ${id} 에 답글 — 「${clip(text, 40)}」`]); });
  }
  async #resolveComment(a) {
    this.#need('WordApi', '1.4', 'resolve_comment');
    const id = String(need(a, 'id')); const del = bool(a, 'delete') ?? false; const resolved = bool(a, 'resolved') ?? true;
    return this.runner(async (context) => { const c = await this.#comment(context, id); if (del) c.delete(); else c.resolved = resolved; await context.sync(); this.#mutated(); return this.#envelope({ id, deleted: del, resolved: del ? null : resolved }, [del ? `메모 ${id} 를 지웠습니다` : `메모 ${id} 를 ${resolved ? '해결로' : '미해결로'} 표시했습니다`]); });
  }
  async #addBookmark(a) {
    this.#need('WordApi', '1.4', 'add_bookmark');
    const name = String(need(a, 'name')); if (!/^[A-Za-z][A-Za-z0-9_]{0,39}$/.test(name)) refuse(`책갈피 이름은 영문자로 시작하는 영문·숫자·밑줄 40자 이내 — ${name}`);
    return this.runner(async (context) => { const items = await this.#paras(context, 'text'); const { from, to, list } = this.#pick(items, a); WordHand.#rangeOf(list).insertBookmark(name); await context.sync(); this.#mutated(); return this.#envelope({ name, from, to }, [`문단 ${from}${to > from ? `–${to}` : ''} 에 책갈피 「${name}」`]); });
  }
  async #deleteBookmark(a) {
    this.#need('WordApi', '1.4', 'delete_bookmark');
    const name = String(need(a, 'name'));
    return this.runner(async (context) => { const r = context.document.getBookmarkRangeOrNullObject(name); r.load('isNullObject'); await context.sync(); if (r.isNullObject) refuse(`책갈피 「${name}」 가 없습니다`); context.document.deleteBookmark(name); await context.sync(); this.#mutated(); return this.#envelope({ name }, [`책갈피 「${name}」 를 지웠습니다 — 글은 그대로입니다`]); });
  }
  async #setTrackChanges(a) {
    this.#need('WordApi', '1.4', 'set_track_changes');
    const mode = str(a, 'mode') ?? refuse('mode 가 없습니다 — Off, TrackAll, TrackMineOnly');
    return this.runner(async (context) => { context.document.changeTrackingMode = mode; await context.sync(); this.#mutated(); return this.#envelope({ mode }, [`변경 추적 → ${mode}`]); });
  }
  async #reviewChanges(a) {
    this.#need('WordApi', '1.6', 'review_changes');
    const what = str(a, 'what') ?? refuse('what 이 없습니다 — accept 나 reject');
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      const { from, to, list, whole } = this.#pick(items, a, { must: false });
      const tc = whole ? context.document.body.getTrackedChanges() : WordHand.#rangeOf(list).getTrackedChanges(); tc.load('items'); await context.sync();
      const n = tc.items.length; if (what === 'accept') tc.acceptAll(); else tc.rejectAll(); await context.sync(); this.#mutated();
      return this.#envelope({ what, count: n, from, to }, [`변경 ${n}건을 ${what === 'accept' ? '수락' : '거부'}했습니다${whole ? '' : ` (문단 ${from}–${to})`}`]);
    });
  }
  async #setProperties(a) {
    const keys = ['title', 'subject', 'author', 'keywords', 'comments', 'category'].filter((k) => a[k] != null);
    if (keys.length === 0) refuse('바꿀 속성이 없습니다 — title·subject·author·keywords·comments·category');
    return this.runner(async (context) => { const props = context.document.properties; for (const k of keys) props[k] = String(a[k]); await context.sync(); this.#mutated(); return this.#envelope({ set: keys }, [`문서 속성: ${keys.map((k) => `${k}=「${clip(String(a[k]), 30)}」`).join(', ')}`]); });
  }
  async #restore(a) {
    const id = String(need(a, 'snapshot')); const snap = this.snapshots.get(id);
    if (!snap) refuse(`그런 스냅숏이 없습니다: ${id} — snapshot_paragraphs 가 준 id 를 주세요(이 창이 뜬 뒤 찍은 것만 압니다)`);
    return this.runner(async (context) => {
      const items = await this.#paras(context, 'text');
      if (snap.from > items.length) refuse(`스냅숏의 자리(문단 ${snap.from})가 지금 문서에 없습니다 — 문단 ${items.length}개`);
      const list = items.slice(snap.from - 1, Math.min(snap.to, items.length));
      WordHand.#rangeOf(list).insertOoxml(snap.ooxml, 'Replace'); await context.sync(); this.#mutated();
      return this.#envelope({ snapshot: id, from: snap.from, to: snap.to }, [`문단 ${snap.from}–${snap.to} 을 스냅숏 ${id} 로 되돌렸습니다`]);
    });
  }
  async #setTag(a) {
    const key = String(need(a, 'key')); const value = String(a.value ?? '');
    if (key.startsWith(FIX_PREFIX) || key === DOC_PROPERTY_KEY) refuse('그 키는 제안·문서 이름의 것이라 기록으로 못 씁니다');
    if (value.length > 255) refuse(`Word 의 사용자 지정 속성은 255자까지입니다 — ${value.length}자`);
    const k = key.startsWith('MAGI.') ? key : `MAGI.${key}`;
    return this.runner(async (context) => {
      const props = context.document.properties.customProperties;
      if (value === '') { const had = props.getItemOrNullObject(k); had.load('isNullObject'); await context.sync(); if (!had.isNullObject) had.delete(); }
      else props.add(k, value);
      await context.sync(); this.#mutated();
      return this.#envelope({ key: k, value }, [value === '' ? `기록 '${k}' 를 지웠습니다` : `기록 '${k}' 를 남겼습니다 — 「${clip(value, 40)}」`]);
    });
  }
  async #suggest(a) {
    this.#need('WordApi', '1.4', 'suggest');
    const what = String(need(a, 'what')); const fix = a.fix && typeof a.fix === 'object' ? a.fix : null;
    if (fix && !FIX_TOOLS.includes(String(fix.tool))) refuse(`제안으로 누를 수 있는 손은 ${FIX_TOOLS.join(', ')} 뿐입니다 — ${fix.tool}`);
    const key = `${FIX_PREFIX}${Date.now().toString(36).toUpperCase()}${Math.random().toString(36).slice(2, 6).toUpperCase()}`;
    const body = { what, why: str(a, 'why') ?? '', paragraph: int(a, 'paragraph'), fix };
    return this.runner(async (context) => { context.document.settings.add(key, JSON.stringify(body)); await context.sync(); this.#mutated(); return this.#envelope({ suggestion: key, paragraph: body.paragraph }, [`${body.paragraph ? `문단 ${body.paragraph} 에` : '문서에'} 제안을 붙였습니다 — ${clip(what, 60)}. **이건 아직 안 고친 것입니다** — 작업창의 「적용」을 누르기 전까지 문서는 그대로입니다`]); });
  }
  async #dropSuggestion(a) {
    this.#need('WordApi', '1.4', 'drop_suggestion');
    const key = String(need(a, 'key')); if (!key.startsWith(FIX_PREFIX)) refuse(`제안의 키가 아닙니다 — ${key}`);
    return this.runner(async (context) => { const had = context.document.settings.getItemOrNullObject(key); had.load('isNullObject'); await context.sync(); if (had.isNullObject) refuse(`그런 제안이 없습니다: ${key}`); had.delete(); await context.sync(); this.#mutated(); return this.#envelope({ key }, [`제안 ${key} 를 뗐습니다 — 고치지는 않았습니다`]); });
  }
}
export { Refusal };
