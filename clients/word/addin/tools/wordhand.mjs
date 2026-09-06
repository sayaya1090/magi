// 진짜 손(WordHand)을 **가짜 Office.js 위에서** 도구 44개 전부 한 번씩 돌린다. `node tools/wordhand.mjs`
//
// 이 stub 은 호스트가 아니다 — 어떤 속성을 읽어도 그럴듯한 값을 주고 어떤 메서드를 불러도 받아 준다. 재는 것은
// 「우리 코드가 Word.js 를 잘못 부르지는 않는가」(없는 메서드·틀린 인자·load 없이 읽기 같은 TypeError)뿐이다. 엑셀 판이
// 실물에서 배운 대로, 이 층은 **지어낸 메서드 이름을 못 잡는다** — 그 답은 Word 뿐이다(docs/TESTING.ko.md §5).
import { WordHand } from '../src/adapter/WordHand.js';
import { ALL_OPS, Refusal } from '../src/adapter/handCore.js';

const PARAS = ['제목', '1. 요약', '본문 하나', '본문 둘', '2. 매출', '표 앞', '주요 성과', '항목 하나', '항목 둘', '3. 계획', '마지막'];
const STYLES = ['Title', 'Heading 1', 'Normal', 'Normal', 'Heading 1', 'Normal', 'Heading 2', 'List Paragraph', 'List Paragraph', 'Heading 1', 'Normal'];
const seen = [];
const scalar = (prop, path, idx) => {
  switch (prop) {
    case 'text': return path.includes('search') ? '요약' : PARAS[idx ?? 0] ?? '글';
    case 'style': case 'nameLocal': return STYLES[idx ?? 0] ?? 'Normal'; case 'styleBuiltIn': return (STYLES[idx ?? 0] ?? 'Normal').replace(/\s+/g, '');
    case 'isListItem': return (STYLES[idx ?? 0] ?? '') === 'List Paragraph'; case 'tableNestingLevel': return 0; case 'alignment': return 'Left';
    case 'lineSpacing': return 13.8; case 'spaceAfter': return 8; case 'spaceBefore': return 0; case 'firstLineIndent': return 0; case 'leftIndent': return 0;
    case 'name': return '맑은 고딕'; case 'size': return 11; case 'bold': return false; case 'italic': return false; case 'color': return '#000000'; case 'highlightColor': return null; case 'underline': return 'None';
    case 'altTextDescription': return '그림'; case 'level': return 0; case 'listString': return '•'; case 'id': return 'c1'; case 'content': return '메모'; case 'authorName': return '사람'; case 'creationDate': return '2026-09-06'; case 'resolved': return false;
    case 'rowCount': return 3; case 'headerRowCount': return 1; case 'values': return [['분기', '매출'], ['1', '2'], ['3', '4']];
    case 'title': return '제목'; case 'subject': return ''; case 'author': return '기획팀'; case 'keywords': return ''; case 'comments': return ''; case 'category': return ''; case 'lastAuthor': return '';
    case 'changeTrackingMode': return 'Off'; case 'type': return 'Added'; case 'date': return '2026-09-06'; case 'key': return 'MAGI.FIX.K1'; case 'width': return 100; case 'height': return 60;
    case 'isNullObject': return false;
    case 'value': return path.includes('getHtml') ? '<p>x</p>' : path.includes('getOoxml') ? '<pkg/>' : path.includes('compareLocationWith') ? 'Equal' : JSON.stringify({ what: 'w', fix: { tool: 'set_style', args: { from: 1, builtin: 'Title' } } });
    default: return undefined;
  }
};
function thing(path, idx) {
  const fn = function () {};
  return new Proxy(fn, {
    get(_, prop) {
      if (typeof prop === 'symbol' || prop === 'then') return undefined;
      if (prop === 'toJSON') return () => ({ stub: path });
      if (prop === 'items') {
        const n = path.endsWith('paragraphs') ? PARAS.length : path.endsWith('tables') || path.endsWith('sections') || path.endsWith('inlinePictures') ? 1 : 2;
        return Array.from({ length: n }, (_, i) => thing(`${path}.items[${i}]`, path.endsWith('paragraphs') ? i : idx));
      }
      const v = scalar(prop, path, idx);
      if (v !== undefined) return v;
      if (prop === 'sync') return async () => {};
      return thing(`${path}.${prop}`, idx);
    },
    set(_, prop, v) { seen.push(`${path}.${String(prop)} = ${JSON.stringify(v)}`); return true; },
    apply(_, __, args) { seen.push(`${path}(${args.map((a) => JSON.stringify(a)).join(', ')})`); return thing(path, idx); },
  });
}
const context = () => ({ document: thing('document'), sync: async () => {} });

const ARGS = {
  list_paragraphs: {}, read_paragraphs: { from: 1, to: 3 }, read_document: {}, find: { text: '요약' }, read_table: { table: 1 }, read_html: { from: 1, to: 2 },
  read_comments: {}, read_footnotes: {}, list_images: {}, format_image: { image: 1, width: 120, alt: '차트' }, delete_image: { image: 1 }, insert_footnote: { paragraph: 3, text: '매출', note: '내부 집계 기준' }, set_style_format: { style: 'Title', size: 14, bold: true, space_before: 12 }, move_paragraphs: { from: 3, to: 4, after: 6 }, insert_file: { file_base64: 'UEsDBA==', file_name: 'x.docx', at: 'end' }, delete_footnote: { number: 1 }, read_tracked_changes: {}, describe_style: {}, snapshot_paragraphs: { from: 2, to: 3 }, read_tags: {}, read_suggestions: {},
  advise: { items: [{ message: 'm' }] }, clear_advice: {},
  insert_paragraphs: { lines: ['a', 'b'], after: 3, style: 'Normal' }, replace_paragraph: { paragraph: 3, text: '새 글' }, delete_paragraphs: { from: 4, to: 4 },
  set_style: { from: 2, builtin: 'Heading1' }, format_text: { from: 3, text: '요약', bold: true, color: '#C00000', highlight: 'Yellow' },
  format_paragraph: { from: 3, align: 'Justified', space_after: 6 }, insert_table: { values: [['a', 'b'], ['1', '2']], after: 5, table_style: 'GridTable4_Accent1' },
  set_table_cells: { table: 1, cells: [{ row: 1, column: 0, value: 'x' }] }, add_table_rows: { table: 1, rows: [['c', 'd']] }, delete_table: { table: 1 },
  format_table: { table: 1, header_row: true, align: 'Centered', widths: [100, 200] }, format_table_cells: { table: 1, rows: [0, 0], fill: '#DDDDDD', bold: true }, edit_table: { table: 1, add_columns: { at: 'end', count: 1, values: [['합계', '3', '7']] }, delete_rows: [2] }, insert_list: { items: ['하나', '둘'], after: 6, kind: 'numbered', levels: [0, 1] },
  set_list: { from: 8, to: 9, kind: 'bulleted', level: 1 }, insert_image: { path: '/x.png', image_base64: 'AAAA', after: 3, width: 120, alt: '점' },
  insert_break: { paragraph: 2, kind: 'page' }, insert_field: { which: 'footer', template: '{page} / {pages}', align: 'Centered' }, set_header_footer: { which: 'footer', text: '기획팀', align: 'Centered' }, set_hyperlink: { from: 3, text: '요약', url: 'https://x' },
  replace_all: { find: '요약', replace: '개요' }, add_comment: { from: 3, comment: '근거는?' }, reply_comment: { id: 'c1', text: '답' }, resolve_comment: { id: 'c1' },
  add_bookmark: { from: 2, to: 3, name: 'intro' }, delete_bookmark: { name: 'intro' }, set_track_changes: { mode: 'TrackAll' }, review_changes: { what: 'accept' },
  set_properties: { title: 't' }, restore_paragraphs: null, set_tag: { key: 'k', value: 'v' },
  suggest: { what: 'w', paragraph: 2, fix: { tool: 'set_style', args: { builtin: 'Heading2' } } }, drop_suggestion: { key: 'MAGI.FIX.K1' },
};
let failed = 0; let refused = 0;
const hand = new WordHand({ run: async (fn) => fn(context()), supports: () => true, document: 'doc-stub', label: 'stub.docx' });
for (const op of ALL_OPS) {
  let args = ARGS[op];
  if (!(op in ARGS)) { console.log(`  FAIL ${op} — 인자 표에 없다`); failed += 1; continue; }
  if (op === 'restore_paragraphs') { const snap = await hand.run('snapshot_paragraphs', { from: 2, to: 3 }); args = { snapshot: snap.result.snapshot }; }
  seen.length = 0;
  try {
    const out = await hand.run(op, args);
    const bad = !out || typeof out !== 'object' || !('result' in out) || !Array.isArray(out.changed) || out.document !== 'doc-stub';
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
