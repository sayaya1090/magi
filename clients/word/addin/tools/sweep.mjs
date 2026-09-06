// 워드 66개 도구 전수 스윕 — 헬퍼(https://127.0.0.1:3000/word)에 붙은 **첫 문서**에 순서대로 다 불러 보고 표로 낸다.
//
//   node clients/word/addin/tools/sweep.mjs [--deck <wd-…>] [--docx <다른 문서.docx>] [--origin https://127.0.0.1:3000/word]
//
// 문서 **끝에** 문단 셋을 붙이고 그 아래에서만 논다(표·목록·그림·필드·내용 컨트롤). 끝에 그 문단부터 끝까지 지우고 바닥글·속성·
// 메모(태그)·제안을 되돌린다 — 앞에 있던 글은 안 건드린다. 토큰은 헬퍼 페이지에서, 문서는 /api/documents 에서 얻는다.
// insert_file 은 --docx 로 준 파일을 넣는다(없으면 건너뛰고 그렇게 적는다). 그림 답은 이 파일 옆 sweep_<도구>.png.
// 2019·2021(WordApi 1.3)에서는 메모·책갈피·변경 추적·각주·스타일 서식·도형·쪽 설정·제안(settings)이 **이름을 대고 거절**한다 —
// 그것은 판의 한계지 고장이 아니다(docs/MANUAL.ko.md §1). 365 에서는 전부 통과가 기대값이다. 아직 실물에 안 대 봤다(2026-09-07 —
// 이 머신에 Word 가 없다); 엑셀 판 sweep.mjs 와 같은 뼈대다.
import { writeFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
const here = dirname(fileURLToPath(import.meta.url));
const opt = { deck: '', docx: '', origin: 'https://127.0.0.1:3000/word' };
for (let i = 2; i < process.argv.length; i += 2) { const k = process.argv[i].replace(/^--/, ''); if (k in opt) opt[k] = process.argv[i + 1] ?? ''; }
const page = await (await fetch(opt.origin + '/taskpane.html')).text();
const m = page.match(/token[^a-zA-Z0-9]{1,6}([A-Za-z0-9_-]{16,})/);
if (!m) { console.log('헬퍼 페이지에서 토큰을 못 찾았다 — 헬퍼가 떠 있나?'); process.exit(2); }
const TOK = m[1];
const H = { authorization: 'Bearer ' + TOK, 'content-type': 'application/json' };
const docs = (await (await fetch(opt.origin + '/api/documents', { headers: H })).json()).documents ?? [];
const DECK = opt.deck || docs[0]?.document || '';
if (!DECK) { console.log('붙은 작업창이 없다 — Word 에서 magi 작업창을 열어라'); process.exit(2); }
console.log('document', DECK, 'of', docs.map((d) => d.document).join(','));
const IMG = join(here, 'sweep-image.png');
if (!existsSync(IMG)) writeFileSync(IMG, Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64'));
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
const count = async (note) => { const x = (await call('list_paragraphs', {}, note)) ?? {}; return Number(x.count ?? x.total ?? (x.paragraphs ?? []).length ?? 0); };
const tables = async () => { const x = (await call('read_document', {}, '표 수')) ?? {}; return Number(x.tables ?? x.counts?.tables ?? 0); };

const base = await count('처음');
await call('read_document', {}, '');
await call('describe_style', {}, '');
await call('read_paragraphs', { from: 1, to: Math.min(2, base) }, '1~2');
await call('read_html', { from: 1, to: 1 }, '1');
await call('insert_paragraphs', { lines: ['스윕 시작', '스윕 본문 요약입니다', '스윕 셋째'], at: 'end', style: 'Normal' }, '끝에 3');
const p1 = base + 1, p2 = base + 2, p3 = base + 3;
await call('find', { text: '요약' }, '');
await call('set_style', { from: p1, builtin: 'Heading2' }, `${p1}`);
await call('format_text', { from: p2, text: '요약', bold: true, color: '#C00000', highlight: 'Yellow' }, `${p2} 요약`);
await call('format_paragraph', { from: p2, align: 'Justified', space_after: 6 }, `${p2}`);
await call('replace_paragraph', { paragraph: p3, text: '스윕 셋째 — 바꿈' }, `${p3}`);
const snap = (await call('snapshot_paragraphs', { from: p2, to: p3 }, `${p2}~${p3}`)) ?? {};
const snapId = snap.snapshot ?? snap.id;
await call('replace_all', { find: '바꿈', replace: '되돌림', from: p1, to: p3 }, '바꿈→되돌림');
await call('set_hyperlink', { from: p3, text: '스윕', url: 'https://example.com' }, '걸기');
await call('set_hyperlink', { from: p3, text: '스윕' }, '떼기');
await call('insert_table', { values: [['구분', '값'], ['a', '1'], ['b', '2']], after: p3, table_style: 'GridTable4_Accent1' }, `${p3} 뒤`);
const T = await tables();
if (T) {
  await call('read_table', { table: T }, `${T}`);
  await call('set_table_cells', { table: T, cells: [{ row: 1, column: 1, value: 'x' }] }, '');
  await call('add_table_rows', { table: T, rows: [['c', '3']] }, '');
  await call('format_table', { table: T, header_row: true, align: 'Centered', widths: [100, 100] }, '');
  await call('format_table_cells', { table: T, rows: [0, 0], fill: '#DDDDDD', bold: true }, '머리행');
  await call('edit_table', { table: T, add_columns: { at: 'end', count: 1, values: [['합계', '1', '2', '3']] } }, '열 하나');
  await call('edit_table', { table: T, merge: { from_row: 0, from_column: 0, to_row: 0, to_column: 1 } }, '병합(1.4)');
  await call('delete_table', { table: T }, '');
} else skipped.push('read_table·set_table_cells·add_table_rows·format_table·format_table_cells·edit_table·delete_table(표를 못 만들었다)');
await call('insert_list', { items: ['하나', '둘'], after: p3, kind: 'numbered', levels: [0, 1] }, `${p3} 뒤`);
await call('set_list', { from: p3 + 1, to: p3 + 2, kind: 'bulleted', level: 0 }, '글머리');
await call('set_list', { from: p3 + 1, to: p3 + 2, detach: true }, '떼기');
await call('insert_image', { path: IMG, after: p3, width: 40, alt: '스윕 점' }, 'png');
const imgs = (await call('list_images', {}, '')) ?? {};
const n = (imgs.images ?? imgs.items ?? []).length;
if (n) { await call('format_image', { image: n, width: 60, align: 'Centered' }, `${n}`); await call('delete_image', { image: n }, `${n}`); }
else skipped.push('format_image·delete_image(그림을 못 넣었다)');
await call('insert_break', { paragraph: p3, kind: 'line' }, 'line');
await call('insert_field', { after: p3, field: 'date' }, 'date');
await call('insert_field', { which: 'footer', template: '{page} / {pages}', align: 'Centered' }, '바닥글 쪽수');
await call('set_header_footer', { which: 'footer', text: '스윕 바닥글', align: 'Centered' }, '');
await call('move_paragraphs', { from: p3, after: p1 }, `${p3}→${p1} 뒤`);
await call('move_paragraphs', { from: p1 + 1, after: p1 + 2 }, '되돌림');
await call('insert_content_control', { paragraph: p2, tag: 'sweep', title: '스윕' }, `${p2}`);
await call('read_content_controls', {}, '');
await call('set_content_control', { tag: 'sweep', title: '스윕2', placeholder: '여기에' }, '');
await call('delete_content_control', { tag: 'sweep' }, '');
if (opt.docx) await call('insert_file', { path: opt.docx, at: 'end' }, ''); else skipped.push('insert_file(--docx 없음)');
await call('set_page_setup', { orientation: 'Landscape' }, 'Desktop 1.1');
await call('set_page_setup', { orientation: 'Portrait' }, '되돌림');
await call('insert_shape', { shape: 'rectangle', text: '주의', fill: '#FFF2CC', name: 'sweepbox', paragraph: p1 }, 'Desktop 1.2');
await call('list_shapes', {}, '');
await call('format_shape', { name: 'sweepbox', left: 100 }, '');
await call('delete_shape', { name: 'sweepbox' }, '');
await call('set_style_format', { style: 'Heading2', color: '#1E3A8A' }, '1.5');
await call('insert_footnote', { paragraph: p2, note: '스윕 각주' }, '1.5');
await call('read_footnotes', {}, '');
await call('delete_footnote', { number: 1 }, '');
await call('add_comment', { from: p2, comment: '근거는?' }, '1.4');
const cm = (await call('read_comments', {}, '')) ?? {};
const cid = (cm.comments ?? cm.items ?? [])[0]?.id;
await call('reply_comment', { id: cid ?? 'c-none', text: '답' }, cid ? String(cid) : '메모 없음');
await call('resolve_comment', { id: cid ?? 'c-none', delete: true }, cid ? String(cid) : '메모 없음');
await call('add_bookmark', { from: p1, to: p2, name: 'sweep_mark' }, '1.4');
await call('delete_bookmark', { name: 'sweep_mark' }, '');
await call('set_track_changes', { mode: 'TrackAll' }, '1.4');
await call('replace_all', { find: '되돌림', replace: '추적', from: p1, to: p3 }, '추적 중 편집');
await call('read_tracked_changes', {}, '1.6');
await call('review_changes', { what: 'reject' }, '1.6');
await call('set_track_changes', { mode: 'Off' }, '');
await call('set_properties', { title: '스윕' }, '');
await call('set_properties', { title: '' }, '되돌림');
await call('set_tag', { key: 'sweep', value: '1' }, '');
await call('read_tags', {}, '');
await call('set_tag', { key: 'sweep', value: '' }, '삭제');
await call('suggest', { what: '제목을 굵게', paragraph: p1, why: '눈에 띄게', fix: { tool: 'format_text', args: { from: p1, bold: true } } }, '');
const sg = (await call('read_suggestions', {}, '')) ?? {};
let key = null;
for (const k of ['suggestions', 'items']) if (Array.isArray(sg[k]) && sg[k].length) { key = sg[k][0].key; break; }
await call('drop_suggestion', { key: key ?? 'MAGI.FIX.NONE' }, key ? String(key) : '제안 없음');
await call('advise', { items: [{ message: '표 머리행 확인', why: '굵기가 다르다', paragraph: p1 }] }, '');
await call('clear_advice', {}, '');
await call('render_page', { page: 1, max_width: 400 }, '');
if (snapId) await call('restore_paragraphs', { snapshot: snapId }, String(snapId));
const end = await count('정리 전');
if (end >= p1) await call('delete_paragraphs', { from: p1, to: end }, `${p1}~${end}`);
await call('set_header_footer', { which: 'footer', text: '' }, '바닥글 비움');
await count('정리 후');
const lst = await (await fetch(`${opt.origin}/mcp?deck=${DECK}`, { method: 'POST', headers: H, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list' }) })).json();
const all = lst.result.tools.map((t) => t.name);
const missing = all.filter((t) => !done.has(t));
console.log(`호출 ${rows.length} · 도구 ${done.size}/${all.length} · 안 부른 것: ${JSON.stringify(missing)}${skipped.length ? ' · 건너뜀: ' + skipped.join(', ') : ''}`);
const errs = rows.filter((r) => r[1] === 'ERR');
console.log(`오류 ${errs.length} · 총 ${(rows.reduce((s, r) => s + r[4], 0)).toFixed(1)}s`);
for (const r of rows) console.log(`${r[1].padEnd(3)} ${r[0].padEnd(24)} ${String(r[4].toFixed(1)).padStart(5)}s  ${String(r[3]).padEnd(12)} ${r[2]}`);
process.exit(errs.length || missing.length ? 1 : 0);
