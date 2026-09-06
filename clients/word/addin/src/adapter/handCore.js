/**
 * 두 손(WordHand·FakeHand)이 같이 쓰는 뼈대 — 도구 이름표, 인자 읽기, 거절, 봉투, 열거형 옮기기.
 *
 * 헬퍼의 catalogue(clients/word/helper/tools.go)가 광고하는 이름과 여기 ALL_OPS 는 같은 집합이어야 한다 —
 * smoke 가 헬퍼 소스를 읽어 대조한다. 광고한 것을 손이 모르면 「고쳤습니다」 없이 「모릅니다」로 끝나고, 손이
 * 아는 것을 광고 안 하면 아무도 못 부른다.
 */

export const READ_OPS = Object.freeze([
  'list_paragraphs', 'read_paragraphs', 'read_document', 'find', 'read_table', 'read_html', 'read_comments', 'read_footnotes', 'list_images', 'render_page', 'read_tracked_changes',
  'describe_style', 'snapshot_paragraphs', 'read_tags', 'read_suggestions', 'advise', 'clear_advice',
]);
export const WRITE_OPS = Object.freeze([
  'insert_paragraphs', 'replace_paragraph', 'delete_paragraphs', 'set_style', 'format_text', 'format_paragraph',
  'insert_table', 'set_table_cells', 'add_table_rows', 'delete_table', 'format_table', 'format_table_cells', 'edit_table', 'insert_list', 'set_list',
  'insert_image', 'format_image', 'delete_image', 'insert_break', 'insert_field', 'insert_footnote', 'delete_footnote', 'set_style_format', 'move_paragraphs', 'insert_file', 'set_header_footer', 'set_hyperlink', 'replace_all',
  'add_comment', 'reply_comment', 'resolve_comment', 'add_bookmark', 'delete_bookmark', 'set_track_changes', 'review_changes',
  'set_properties', 'restore_paragraphs', 'set_tag', 'suggest', 'drop_suggestion',
]);
export const ALL_OPS = Object.freeze([...READ_OPS, ...WRITE_OPS]);

/** 제안으로 누를 수 있는 손 — helper/tools.go 의 suggest 설명과 domain/Suggestion.js 의 FIXABLE 과 같은 목록. */
export const FIX_TOOLS = Object.freeze(['replace_paragraph', 'format_text', 'format_paragraph', 'set_style', 'replace_all', 'insert_paragraphs']);
/** Word.BuiltInStyleName 의 문단 스타일 — helper/enums.go 의 builtinStyles 와 같은 목록(smoke 가 대조한다). */
export const BUILTIN_PARAGRAPH_STYLES = Object.freeze([
  'Normal', 'Title', 'Subtitle', 'Heading1', 'Heading2', 'Heading3', 'Heading4', 'Heading5', 'Heading6', 'Heading7', 'Heading8', 'Heading9',
  'Quote', 'IntenseQuote', 'ListParagraph', 'Caption', 'NoSpacing', 'TocHeading', 'Toc1', 'Toc2', 'Toc3',
  'Emphasis', 'Strong', 'SubtleEmphasis', 'IntenseEmphasis', 'SubtleReference', 'IntenseReference', 'BookTitle',
]);
/** insert_field 의 조각 — 글과 필드가 번갈아 온다. `template` 의 {page}·{pages}… 자리가 필드고, 없으면 `field` 하나. */
export const FIELD_TYPES = Object.freeze({
  toc: 'TOC', page: 'Page', num_pages: 'NumPages', pages: 'NumPages', date: 'Date', time: 'Time',
  title: 'Title', author: 'Author', file_name: 'FileName', file: 'FileName',
});
export function fieldPieces(a) {
  const field = str(a, 'field'); const template = str(a, 'template');
  if (!field && !template) refuse('field 나 template 이 있어야 합니다 — 예: field "toc", template "{page} / {pages}"');
  const codeOf = (name) => (name === 'toc' ? ` \\o "${str(a, 'levels') ?? '1-3'}" \\h \\z \\u ` : '');
  const pieces = [];
  if (template) {
    const re = /\{(page|pages|num_pages|date|time|title|author|file|file_name|toc)\}/g;
    let last = 0; let m;
    while ((m = re.exec(template)) !== null) {
      if (m.index > last) pieces.push({ text: template.slice(last, m.index) });
      pieces.push({ type: FIELD_TYPES[m[1]], code: codeOf(m[1]), name: m[1] });
      last = m.index + m[0].length;
    }
    if (last < template.length) pieces.push({ text: template.slice(last) });
    if (!pieces.some((p) => p.type)) refuse(`template 에 필드 자리가 없습니다 — {page} {pages} {date} {time} {title} {author} {file} 중 하나를 넣으세요: ${template}`);
  } else {
    const type = FIELD_TYPES[field] ?? refuse(`모르는 필드입니다: ${field} — toc, page, num_pages, date, time, title, author, file_name`);
    pieces.push({ type, code: codeOf(field), name: field });
  }
  const names = pieces.filter((p) => p.type).map((p) => ({ toc: '목차', page: '쪽 번호', pages: '전체 쪽수', num_pages: '전체 쪽수', date: '날짜', time: '시각', title: '제목', author: '작성자', file: '파일 이름', file_name: '파일 이름' }[p.name]));
  return { pieces, said: `${names.join('·')} 필드를 넣었습니다${template ? ` — 「${template}」` : ''}` };
}
export const FIX_PREFIX = 'MAGI.FIX.';
export const DOC_PROPERTY_KEY = 'MAGI.DOC';

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

/** 문단 범위 — from/to 를 1-based 로 재고 {from,to} 로 돌려준다. count 가 있으면 넘는 번호를 거절한다. */
export function span(a, count = null, { must = true } = {}) {
  const from = int(a, 'from'); const to = int(a, 'to');
  if (from == null) {
    if (must) refuse('from 이 없습니다 — list_paragraphs 가 준 문단 번호(1부터)');
    return { from: 1, to: count ?? Number.MAX_SAFE_INTEGER, whole: true };
  }
  if (from < 1) refuse(`from 은 1부터입니다 — ${from}`);
  const end = to == null ? from : to;
  if (end < from) refuse(`to(${end}) 가 from(${from}) 보다 앞입니다`);
  if (count != null && from > count) refuse(`문서에 ${from}번 문단이 없습니다 — 문단 ${count}개`);
  return { from, to: count != null ? Math.min(end, count) : end, whole: false };
}

/** 봉투 — 헬퍼의 HandResult 와 같은 모양. `changed` 는 사람이 읽는 한국어 한 줄씩. */
export function envelope(hand, result, changed = []) {
  return { document: hand.document, label: hand.labelText, result, changed, epoch: hand.epoch, count: hand.count };
}

export const clip = (s, n = 40) => { const t = String(s ?? '').replace(/\s+/g, ' '); return t.length > n ? t.slice(0, n - 1) + '…' : t; };
export const isFormula = (v) => typeof v === 'string' && v.startsWith('=');
export const nowEpoch = () => Math.floor(Date.now() / 1000) % 2147483647;
