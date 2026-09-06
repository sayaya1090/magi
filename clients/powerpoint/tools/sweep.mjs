// 48개 도구 전수 스윕 — sweep.py 와 같은 일을 **Node 로** 한다(python3 이 없는 Windows 판을 위해; 2021 실물이 그랬다).
//
//   node clients/powerpoint/tools/sweep.mjs [--deck <pid-…>] [--image <png>] [--origin https://127.0.0.1:3000/ppt]
//   (--origin 은 앱의 뿌리 — /ppt 까지. sweep.py 의 --origin 은 헬퍼 뿌리라 뜻이 다르다.)
//
// 헬퍼에 붙은 첫 덱(또는 --deck)에 읽기·쓰기를 전부 실제로 부른다. 장 3~4개를 만들고 끝에 지운다 — 1장만 남는다.
// 토큰은 헬퍼 페이지에서, 덱은 /api/documents 에서 얻는다. 그림 답은 이 파일 옆 sweep_<도구>.png 로 떨어진다.
// 실측 2026-09-07(Office LTSC 2021 · COM 손): 48/48 · 57호출 · 오류 0 · 약 10초.
import { writeFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
const here = dirname(fileURLToPath(import.meta.url));
const opt = { deck: '', image: '', origin: 'https://127.0.0.1:3000/ppt' };
for (let i = 2; i < process.argv.length; i += 2) { const k = process.argv[i].replace(/^--/, ''); if (k in opt) opt[k] = process.argv[i + 1] ?? ''; }
const page = await (await fetch(opt.origin + '/taskpane.html')).text();
const m = page.match(/token[^a-zA-Z0-9]{1,6}([A-Za-z0-9_-]{16,})/);
if (!m) { console.log('헬퍼 페이지에서 토큰을 못 찾았다 — 헬퍼가 떠 있나?'); process.exit(2); }
const TOK = m[1];
const H = { authorization: 'Bearer ' + TOK, 'content-type': 'application/json' };
const docs = (await (await fetch(opt.origin + '/api/documents', { headers: H })).json()).documents ?? [];
const DECK = opt.deck || docs[0]?.document || '';
if (!DECK) { console.log('붙은 작업창이 없다 — PowerPoint 에서 magi 작업창을 열거나 COM 손을 띄워라'); process.exit(2); }
console.log('deck', DECK, 'of', docs.map((d) => d.document).join(','));
const IMG = opt.image || join(here, 'sweep-image.png');
if (!existsSync(IMG)) writeFileSync(IMG, Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64'));
const rows = []; const done = new Set();
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
  let summary = (x && typeof x === 'object' && Array.isArray(x.changed) && x.changed.length ? x.changed.join(' | ') : txt).slice(0, 110).replace(/\n/g, ' ');
  if (img.length) summary = `image ${Math.floor(img[0].data.length / 1024)}KB ` + summary;
  rows.push([name, err ? 'ERR' : 'ok', summary, note, (Date.now() - t0) / 1000]); done.add(name);
  return err ? null : x;
}
async function shapes(slide) { const x = (await call('read_slide', { slide }, '되읽기')) ?? {}; return x.shapes ?? []; }
const first = (shs, pred) => shs.find(pred) ?? null;
const ph = (s) => String(s.placeholder ?? '').toLowerCase();

const lay = (await call('list_layouts', {}, '')) ?? {};
const names = (lay.masters ?? []).flatMap((mm) => (mm.layouts ?? []).map((l) => l.layout));
const titleLayout = names.find((n) => n.includes('제목 슬라이드') || n.includes('Title Slide')) ?? names[0] ?? null;
await call('describe_style', {}, '');
await call('read_theme_colors', { scope: 'master' }, 'master');
await call('add_slides', { slides: [{ title: '스윕 2', body: '첫째\n둘째', bullet: true }, { title: '스윕 3', body: '차트와 그림', bullet: false }] }, '2장');
await call('add_slide', { title: '스윕 4', body: '레이아웃 바꿀 장' }, '');
await call('list_slides', {}, '');
let sh = await shapes(2);
const title = first(sh, (s) => ph(s).includes('title')); let body = first(sh, (s) => ['content', 'body'].includes(ph(s)));
await call('find_shapes', { text: '스윕' }, 'text=스윕');
await call('export_slide_ooxml', { slide: 2, part: 'list' }, 'part=list');
await call('set_text', { slide: 2, placeholder: 'title', text: '스윕 2 — 제목' }, 'placeholder=title');
if (title) await call('format_shape', { slide: 2, shape_id: title.shape_id, bold: true, color: '#1E3A8A', valign: 'Middle', underline: 'Single' }, '제목');
if (title) await call('format_text', { slide: 2, shape_id: title.shape_id, find: '스윕', color: '#DC2626', bold: true }, 'find=스윕');
const A = (await call('add_shape', { slide: 2, kind: 'textbox', text: '상자 A', left: 60, top: 330, width: 150, height: 50, fill: '#EEF2FF', line: '#3B82F6' }, 'textbox')) ?? {};
const B = (await call('add_shape', { slide: 2, kind: 'rectangle', text: 'B', left: 260, top: 340, width: 150, height: 50, fill: '#FECACA', transparency: 0.3 }, 'rectangle')) ?? {};
const a = A.shape_id, b = B.shape_id;
if (b) await call('move_shape', { slide: 2, shape_id: b, top: 330, z_order: 'SendToBack' }, 'top+z_order');
if (a && b) await call('align_shapes', { slide: 2, shape_ids: [a, b], how: 'top' }, 'how=top');
const G = a && b ? await call('group_shapes', { slide: 2, shape_ids: [a, b] }, '') : null;
if (G) await call('ungroup_shapes', { slide: 2, shape_id: G.shape_id }, '');
sh = await shapes(2); let tb = first(sh, (s) => s.type === 'TextBox');
if (tb) await call('set_hyperlink', { slide: 2, shape_id: tb.shape_id, url: 'https://example.com', screen_tip: '예' }, 'textbox');
if (tb) await call('render_shape', { slide: 2, shape_id: tb.shape_id, max_width: 300 }, '');
const T = (await call('add_table', { slide: 3, rows: 3, columns: 3, values: [['구분', '전', '후'], ['a', '1', '2'], ['b', '3', '4']], left: 60, top: 300, width: 400, height: 110, table_style: 'MediumStyle2Accent1', header_row: true, column_widths: [160, 120, 120] }, 'style+widths')) ?? {};
const tid = T.shape_id;
if (tid) {
  await call('set_table_cells', { slide: 3, shape_id: tid, cells: [{ row: 1, column: 1, text: '값' }] }, '');
  await call('format_table_cells', { slide: 3, shape_id: tid, row: 0, bold: true, fill: '#1E3A8A', color: '#FFFFFF', valign: 'Middle' }, '머리행');
  await call('edit_table', { slide: 3, shape_id: tid, add_rows: 1, merge: [{ row: 0, column: 1, columns: 2 }] }, 'add_rows+merge');
  await call('replace_table', { slide: 3, shape_id: tid, rows: 2, columns: 2 }, '3x3→2x2');
}
await call('add_chart', { slide: 3, kind: 'column', title: '스윕', categories: ['a', 'b'], series: [{ name: 's', values: [1, 2] }], left: 480, top: 120, width: 400, height: 160 }, '');
await call('add_image', { slide: 3, path: IMG, left: 60, top: 120, width: 200, alt: '스윕 그림' }, 'png');
await call('set_notes', { slide: 2, text: '스윕 노트' }, ''); await call('read_notes', { slide: 2 }, '');
await call('set_tag', { slide: 2, key: 'sweep', value: '1' }, ''); await call('read_tags', { slide: 2 }, '');
sh = await shapes(2); body = first(sh, (s) => ['content', 'body'].includes(ph(s)));
if (body) await call('animate_slide', { slide: 2, steps: [{ shape_id: body.shape_id, effect: 'fade' }] }, 'body fade');
await call('read_animation', { slide: 2 }, '');
await call('suggest', { slide: 2, what: '제목을 짧게', why: '두 줄이다' }, ''); const sg = (await call('read_suggestions', { slide: 2 }, '')) ?? {};
let key = null;
for (const k of ['suggestions', 'items']) if (Array.isArray(sg[k]) && sg[k].length) { key = sg[k][0].key; break; }
if (key) await call('drop_suggestion', { slide: 2, key }, '');
await call('advise', { items: [{ message: '표지 대비 확인', why: '배경이 어둡다', slide_id: null }] }, ''); await call('clear_advice', {}, '');
await call('set_background', { slide: 2, color: '#0F172A' }, 'dark → 대비 경고?');
await call('set_theme_colors', { colors: { accent1: '#0284C7' }, scope: 'master' }, 'master accent1');
await call('apply_style', { title: { bold: true }, slides: [2] }, 'slide 2 title bold');
if (titleLayout) await call('apply_layout', { slide: 4, layout: titleLayout }, titleLayout);
await call('reorder_slide', { slide: 4, to: 2 }, '4→2');
await call('duplicate_slide', { slide: 3 }, '');
const snap = (await call('snapshot_slide', { slide: 3 }, '')) ?? {};
if (snap.snapshot) await call('restore_slide', { slide: 3, snapshot: snap.snapshot }, '');
await call('render_slide', { slide: 3, max_width: 640 }, '');
sh = await shapes(3); tb = first(sh, (s) => ['TextBox', 'GeometricShape'].includes(s.type));
if (tb) await call('delete_shape', { slide: 3, shape_id: tb.shape_id }, '');
else {
  const x = (await call('add_shape', { slide: 3, kind: 'rectangle', left: 10, top: 10, width: 20, height: 20 }, '삭제용')) ?? {};
  if (x.shape_id) await call('delete_shape', { slide: 3, shape_id: x.shape_id }, '');
}
let ls = (await call('list_slides', {}, '정리 전')) ?? {};
const n = (ls.slides ?? []).length;
for (let i = n; i > 1; i--) await call('delete_slide', { slide: i }, String(i));
ls = (await call('list_slides', {}, '정리 후')) ?? {};
const lst = await (await fetch(`${opt.origin}/mcp?deck=${DECK}`, { method: 'POST', headers: H, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list' }) })).json();
const all = lst.result.tools.map((t) => t.name);
const missing = all.filter((t) => !done.has(t));
console.log(`호출 ${rows.length} · 도구 ${done.size}/${all.length} · 안 부른 것: ${JSON.stringify(missing)}`);
const errs = rows.filter((r) => r[1] === 'ERR');
console.log(`오류 ${errs.length} · 총 ${(rows.reduce((s, r) => s + r[4], 0)).toFixed(1)}s`);
for (const r of rows) console.log(`${r[1].padEnd(3)} ${r[0].padEnd(20)} ${String(r[4].toFixed(1)).padStart(5)}s  ${String(r[3]).padEnd(14)} ${r[2]}`);
process.exit(errs.length || missing.length ? 1 : 0);
