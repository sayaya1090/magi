// `OfficeHand` 와 zip 읽개의 확인. `node tools/officehand.mjs`
//
// ⚠ **이 머신에는 PowerPoint 가 없다.** 그러므로 여기서 재는 것은 **호스트가 아니라 우리**다:
// 읽기 전에 `load` 를 했는가, 없는 것을 지어내지 않는가, 쓰기가 바뀐 값을 스스로 싣는가,
// 못 하는 것을 조용히 성공으로 답하지 않는가. 호스트가 실제로 어떻게 답하는지는 **여전히 안
// 잰 것이고**, 그 사실을 아래 마지막 줄이 매 런마다 소리 내어 적는다.
//
// stub 이 `load` 를 강제하는 이유가 그것이다. 값을 그냥 노출하면 **`load` 를 빠뜨린 코드가
// 시험에서 초록**이고 진짜 호스트에서만 죽는다 — 그게 이 목업이 못 잡는 결함의 대표 모양이라
// 여기서만이라도 문다.
import { OfficeHand, pickPart } from '../src/adapter/OfficeHand.js';
import { zipEntries, zipRead } from '../src/adapter/zip.js';

let failed = 0;
const ok = (name, cond, detail = '') => {
  console.log(`${cond ? '  ok  ' : '  FAIL'} ${name}${detail ? ' — ' + detail : ''}`);
  if (!cond) failed++;
};
const threw = async (fn) => {
  try { await fn(); return null; } catch (e) { return e?.message ?? String(e); }
};

// ── load 를 강제하는 흉내 ─────────────────────────────────────────────────────

/** 값을 감춰 두고 `load` 한 것만 `sync` 때 내준다. */
class Loaded {
  constructor(raw, pending) { this.raw = raw; this.pending = pending; }
  load(spec) {
    for (const path of String(spec).split(',').map((s) => s.trim()).filter(Boolean)) {
      this.pending.push([this, path]);
    }
    return this;
  }
}

function reveal(target, path) {
  // `items/id` 같은 경로는 컬렉션의 각 항목에 건다. `layout/name` 은 자식 객체다.
  const [head, ...rest] = path.split('/');
  if (head === 'items') {
    for (const item of target.raw.items ?? []) {
      const child = target.itemsView.find((v) => v.raw === item);
      if (child) reveal(child, rest.join('/'));
    }
    target.items = target.itemsView;
    return;
  }
  if (rest.length === 0) {
    target[head] = target.raw[head];
    return;
  }
  const childRaw = target.raw[head];
  if (childRaw === undefined) return;
  target[head] = target[head] ?? new Loaded(childRaw, target.pending);
  if (!target[head].itemsView && childRaw.items) {
    target[head].itemsView = childRaw.items.map((r) => new Loaded(r, target.pending));
  }
  reveal(target[head], rest.join('/'));
}

class StubShape extends Loaded {
  constructor(raw, pending, log) {
    super(raw, pending);
    this.log = log;
    this.textFrame = { textRange: new StubTextRange(raw, pending, log) };
    this.fill = { setSolidColor: (c) => log.push(`fill:${c}`), clear: () => log.push('fill:clear') };
  }
  delete() { this.log.push(`delete:${this.raw.id}`); }
  setHyperlink(v) { this.log.push(`link:${v?.address ?? 'none'}`); }
  getTable() { return this.table ?? (this.table = new StubTable(this.raw, this.pending, this.log)); }
}

class StubTextRange extends Loaded {
  constructor(raw, pending, log) {
    super(raw, pending);
    this.log = log;
    this.font = new StubFont(raw.font ?? {}, pending, log);
    this.paragraphFormat = {};
  }
  set text(v) { this.log.push(`text:${this.raw.id}:${v}`); this.raw.text = v; }
  get text() { return this.shown; }
  load(spec) { this.pending.push([this, spec]); return this; }
}

class StubFont extends Loaded {
  constructor(raw, pending, log) { super(raw, pending); this.log = log; }
}

class StubTable {
  constructor(raw, pending, log) { this.raw = raw; this.pending = pending; this.log = log; }
  getCellOrNullObject(r, c) {
    const cell = { isNullObject: !(this.raw.cells?.[r]?.[c] !== undefined), text: this.raw.cells?.[r]?.[c] };
    const view = new Loaded(cell, this.pending);
    view.setText = (v) => this.log.push(`cell:${r},${c}:${v}`);
    Object.defineProperty(view, 'text', {
      get() { return this.shownText; },
      set(v) { view.setText(v); },
      configurable: true,
    });
    view.reveal = () => { view.isNullObject = cell.isNullObject; view.shownText = cell.text; };
    return view;
  }
}

/** 아주 작은 프레젠테이션 모델 위에 `PowerPoint.run` 을 흉내 낸다. */
function stubRunner(model, log = []) {
  return async (fn) => {
    const pending = [];
    const slidesView = model.slides.map((s) => {
      const view = new Loaded(s, pending);
      view.itemsView = null;
      view.shapes = makeShapes(s, pending, log);
      view.getImageAsBase64 = () => ({ value: 'PNGBASE64' });
      view.exportAsBase64 = () => ({ value: model.exported ?? 'PPTXBASE64' });
      view.applyLayout = (id) => log.push(`layout:${s.id}:${id}`);
      view.moveTo = (i) => log.push(`moveTo:${s.id}:${i}`);
      view.delete = () => log.push(`slide-delete:${s.id}`);
      return view;
    });
    const slides = new Loaded({ items: model.slides }, pending);
    slides.itemsView = slidesView;
    slides.getItem = (id) => slidesView.find((v) => v.raw.id === id)
      ?? (() => { throw new Error(`no slide ${id}`); })();
    slides.getItemAt = (i) => slidesView[i] ?? (() => { throw new Error(`no slide at ${i}`); })();

    const masters = new Loaded({ items: model.masters ?? [] }, pending);
    masters.itemsView = (model.masters ?? []).map((m) => {
      const v = new Loaded(m, pending);
      v.layouts = new Loaded({ items: m.layouts }, pending);
      v.layouts.itemsView = m.layouts.map((l) => new Loaded(l, pending));
      return v;
    });

    const context = {
      presentation: {
        slides,
        slideMasters: masters,
        getSelectedSlides: () => {
          const sel = new Loaded({ items: model.selected ?? [model.slides[0]] }, pending);
          sel.itemsView = (model.selected ?? [model.slides[0]]).map((s) =>
            slidesView.find((v) => v.raw === s) ?? new Loaded(s, pending));
          return sel;
        },
        insertSlidesFromBase64: (b64, options) => log.push(`insert:${b64.slice(0, 6)}:${options?.targetSlideId}:${options?.formatting ?? ''}`),
      },
      sync: async () => {
        while (pending.length) {
          const [target, path] = pending.shift();
          if (target instanceof StubTextRange) { target.shown = target.raw.text; continue; }
          if (target.reveal) { target.reveal(); continue; }
          reveal(target, path);
        }
      },
    };
    return fn(context);
  };
}

function makeShapes(slide, pending, log) {
  const views = slide.shapes.map((sh) => new StubShape(sh, pending, log));
  const coll = new Loaded({ items: slide.shapes }, pending);
  coll.itemsView = views;
  coll.getItem = (id) => views.find((v) => v.raw.id === id)
    ?? (() => { throw new Error(`no shape ${id}`); })();
  coll.getCount = () => ({ value: slide.shapes.length });
  coll.addTextBox = (text, opts) => {
    log.push(`addTextBox:${text}:${opts.left},${opts.top}`);
    const raw = { id: 'sh-new', name: 'TextBox', type: 'TextBox', text };
    return new StubShape(raw, pending, log);
  };
  coll.addTable = (r, c, opts) => {
    log.push(`addTable:${r}x${c}:${JSON.stringify(opts.uniformCellProperties ?? null)}:${opts.specificCellProperties ? 'specific' : 'none'}`);
    return new StubShape({ id: 'sh-table' }, pending, log);
  };
  return coll;
}

const model = () => ({
  slides: [
    {
      id: 's1', index: 0, layout: { name: '제목 및 내용' },
      shapes: [{ id: 'sh1', name: '제목 1', type: 'TextBox', text: '전분기 요약', left: 10, top: 20, width: 300, height: 60, placeholderFormat: { type: 'title' }, altTextDescription: null }],
    },
    { id: 's2', index: 1, layout: { name: '빈 화면' }, shapes: [] },
  ],
  masters: [{ id: 'm1', name: '기본', layouts: [{ id: 'l1', name: '제목 및 내용' }, { id: 'l2', name: '빈 화면' }] }],
});

// ── 읽기 ──────────────────────────────────────────────────────────────────────

{
  const log = [];
  const hand = new OfficeHand({ run: stubRunner(model(), log), supports: () => true, document: 'doc-1' });
  const out = await hand.run('list_slides', {});
  ok('목차가 0-based index 를 사람 번호로 옮긴다',
    out.result.slides[0].slide === 1 && out.result.slides[1].slide === 2,
    JSON.stringify(out.result.slides.map((s) => s.slide)));
  ok('목차가 레이아웃 이름을 싣는다', out.result.slides[0].layout === '제목 및 내용',
    String(out.result.slides[0].layout));
  ok('목차가 도형 수를 센다', out.result.slides[0].shapes === 1, String(out.result.slides[0].shapes));
}

{
  const hand = new OfficeHand({ run: stubRunner(model()), supports: () => true });
  const out = await hand.run('read_slide', { slide: 1 });
  const shape = out.result.shapes[0];
  ok('도형의 좌표와 크기가 포인트로 온다',
    shape.left === 10 && shape.width === 300, JSON.stringify([shape.left, shape.width]));
  ok('자리표시자 역할이 온다', shape.placeholder === 'title', String(shape.placeholder));
  ok('글이 온다', shape.text === '전분기 요약', shape.text);
  ok('못 읽는 것을 이름으로 적는다', out.result.unreadable.includes('notes'),
    JSON.stringify(out.result.unreadable));
}

{
  // 생략하면 **사람이 보고 있는 장**이다(부록 A — `getSelectedSlides` 의 첫 항목).
  const m = model();
  m.selected = [m.slides[1]];
  const hand = new OfficeHand({ run: stubRunner(m), supports: () => true });
  const out = await hand.run('read_slide', {});
  ok('슬라이드를 생략하면 보고 있는 장이다', out.result.slide_id === 's2', out.result.slide_id);
}

// ── 쓰기 ──────────────────────────────────────────────────────────────────────

{
  const log = [];
  const hand = new OfficeHand({ run: stubRunner(model(), log), supports: () => true });
  const out = await hand.run('set_text', { slide: 1, shape_id: 'sh1', text: 'Q3 실적' });
  ok('쓰기가 before→after 를 싣는다',
    out.changed[0].includes('전분기 요약') && out.changed[0].includes('Q3 실적'), out.changed[0]);
  ok('실제로 쓴다', log.includes('text:sh1:Q3 실적'), log.join(' '));
  ok('쓰기가 개정 쌍을 올린다', out.count === 1, String(out.count));
}

{
  const hand = new OfficeHand({ run: stubRunner(model()), supports: () => true });
  // **아무것도 안 바꿨으면 바꿨다고 말하지 않는다.**
  const why = await threw(() => hand.run('format_shape', { slide: 1, shape_id: 'sh1' }));
  ok('바꿀 것이 없으면 던진다', why?.includes('하나는 주세요'), why);
}

{
  const log = [];
  const hand = new OfficeHand({ run: stubRunner(model(), log), supports: () => true });
  const out = await hand.run('move_shape', { slide: 1, shape_id: 'sh1', width: 420 });
  ok('이동은 전후 치수를 싣는다',
    out.changed[0].includes('300×60pt') && out.changed[0].includes('420×60pt'), out.changed[0]);
}

{
  const log = [];
  const hand = new OfficeHand({ run: stubRunner(model(), log), supports: () => true });
  const out = await hand.run('delete_shape', { slide: 1, shape_id: 'sh1' });
  ok('삭제는 되돌릴 수 없다고 말한다', out.changed[0].includes('못 되돌립니다'), out.changed[0]);
  ok('실제로 지운다', log.includes('delete:sh1'), log.join(' '));
}

{
  const hand = new OfficeHand({ run: stubRunner(model()), supports: () => true });
  const why = await threw(() => hand.run('apply_layout', { slide: 1, layout: '없는 레이아웃' }));
  ok('없는 레이아웃은 있는 이름을 알려 준다',
    why?.includes('제목 및 내용') && why?.includes('빈 화면'), why);
}

{
  const log = [];
  const hand = new OfficeHand({ run: stubRunner(model(), log), supports: () => true });
  const out = await hand.run('reorder_slide', { slide: 1, to: 2 });
  // `moveTo` 는 0 부터다(부록 A). 사람 번호 2 는 인덱스 1 이다.
  ok('옮기기는 0-based 로 부른다', log.includes('moveTo:s1:1'), log.join(' '));
  ok('옮기고 나면 뒤 번호가 달라졌다고 말한다', out.changed[0].includes('전부 달라졌습니다'), out.changed[0]);
}

{
  // **바닥과 천장 사이의 것은 먼저 묻는다** — LTSC 2024 에는 선택이 있고 하이퍼링크가 없다.
  const hand = new OfficeHand({ run: stubRunner(model()), supports: (n, v) => v !== '1.6' });
  const why = await threw(() => hand.run('set_hyperlink', { slide: 1, shape_id: 'sh1', url: 'https://x' }));
  ok('1.6 이 없으면 링크를 조용히 성공시키지 않는다', why?.includes('1.6'), why);

  const log = [];
  const okHand = new OfficeHand({ run: stubRunner(model(), log), supports: () => true });
  await okHand.run('set_hyperlink', { slide: 1, shape_id: 'sh1', url: 'https://x' });
  ok('1.6 이 있으면 실제로 건다', log.includes('link:https://x'), log.join(' '));
}

{
  const log = [];
  const hand = new OfficeHand({ run: stubRunner(model(), log), supports: () => true });
  await hand.run('add_table', { slide: 1, rows: 2, columns: 2, font: '맑은 고딕', header_bold: true });
  const line = log.find((l) => l.startsWith('addTable'));
  // 서식은 **만들 때** 준다 — 만든 뒤에 고치는 것은 1.9 라 이 바닥에 없다(§2.3).
  ok('표는 서식을 만들면서 받는다',
    line.includes('맑은 고딕') && line.includes('specific'), line);
}

{
  const m = model();
  m.slides[0].shapes.push({ id: 'sh-t', name: '표', type: 'Table', cells: [['a', 'b']] });
  const log = [];
  const hand = new OfficeHand({ run: stubRunner(m, log), supports: () => true });
  const out = await hand.run('set_table_cells', { slide: 1, shape_id: 'sh-t', cells: [{ row: 0, column: 1, text: '새 값' }] });
  ok('셀 쓰기는 전후를 싣는다', out.changed[0].includes('"b" → "새 값"'), out.changed[0]);

  const why = await threw(() => hand.run('set_table_cells', {
    slide: 1, shape_id: 'sh-t', cells: [{ row: 9, column: 9, text: 'x' }],
  }));
  // **절반만 쓰고 성공으로 답하지 않는다.**
  ok('없는 셀은 아무것도 안 쓰고 던진다', why?.includes('아무것도 안 썼습니다'), why);
}

{
  const log = [];
  const m = model();
  const hand = new OfficeHand({ run: stubRunner(m, log), supports: () => true });
  const snap = await hand.run('snapshot_slide', { slide: 1 });
  ok('스냅샷은 덱을 안 고친다', snap.count === 0 && snap.changed.length === 0, String(snap.count));

  const why = await threw(() => hand.run('restore_slide', { snapshot: 'snap-없음' }));
  ok('없는 스냅샷은 던진다', why?.includes('snap-없음'), why);

  await hand.run('restore_slide', { snapshot: snap.result.snapshot });
  const insert = log.find((l) => l.startsWith('insert:'));
  // **`formatting` 을 안 넘기는 것이 계약이다**(부록 A) — Learn 의 예제를 베끼면 되돌린 장이
  // 테마를 새로 입고 돌아온다.
  ok('되돌리기는 formatting 을 안 넘긴다', insert.endsWith(':'), insert);
  ok('되돌리기는 원본을 지운다', log.includes('slide-delete:s1'), log.join(' '));
}

// ── zip ───────────────────────────────────────────────────────────────────────

/** 진짜 zip 을 하나 만든다 — 압축까지 해서, 읽개가 실제 deflate 를 지나게 한다. */
async function makeZip(files) {
  const enc = new TextEncoder();
  const chunks = [];
  const central = [];
  let offset = 0;
  for (const [name, text] of Object.entries(files)) {
    const raw = enc.encode(text);
    const packed = new Uint8Array(await new Response(
      new Blob([raw]).stream().pipeThrough(new CompressionStream('deflate-raw'))).arrayBuffer());
    const nameBytes = enc.encode(name);
    const local = new Uint8Array(30 + nameBytes.length);
    const lv = new DataView(local.buffer);
    lv.setUint32(0, 0x04034b50, true);
    lv.setUint16(8, 8, true);          // deflate
    lv.setUint32(18, packed.length, true);
    lv.setUint32(22, raw.length, true);
    lv.setUint16(26, nameBytes.length, true);
    local.set(nameBytes, 30);
    chunks.push(local, packed);

    const cen = new Uint8Array(46 + nameBytes.length);
    const cv = new DataView(cen.buffer);
    cv.setUint32(0, 0x02014b50, true);
    cv.setUint16(10, 8, true);
    cv.setUint32(20, packed.length, true);
    cv.setUint32(24, raw.length, true);
    cv.setUint16(28, nameBytes.length, true);
    cv.setUint32(42, offset, true);
    cen.set(nameBytes, 46);
    central.push(cen);
    offset += local.length + packed.length;
  }
  const cenBytes = central.reduce((n, c) => n + c.length, 0);
  const eocd = new Uint8Array(22);
  const ev = new DataView(eocd.buffer);
  ev.setUint32(0, 0x06054b50, true);
  ev.setUint16(8, central.length, true);
  ev.setUint16(10, central.length, true);
  ev.setUint32(12, cenBytes, true);
  ev.setUint32(16, offset, true);
  const all = [...chunks, ...central, eocd];
  const total = all.reduce((n, c) => n + c.length, 0);
  const out = new Uint8Array(total);
  let at = 0;
  for (const c of all) { out.set(c, at); at += c.length; }
  return out;
}

{
  const bytes = await makeZip({
    'ppt/slides/slide1.xml': '<p:sld>제목</p:sld>',
    'ppt/notesSlides/notesSlide1.xml': '<p:notes>메모</p:notes>',
  });
  const { entries } = zipEntries(bytes);
  ok('zip 항목을 센다', entries.length === 2, String(entries.length));
  ok('슬라이드 조각을 이름으로 고른다',
    pickPart(entries, 'slide') === 'ppt/slides/slide1.xml', pickPart(entries, 'slide'));
  ok('노트 조각도 고른다',
    pickPart(entries, 'notes') === 'ppt/notesSlides/notesSlide1.xml');
  const xml = await zipRead(bytes, 'ppt/slides/slide1.xml');
  ok('풀면 글이 나온다', xml === '<p:sld>제목</p:sld>', xml);

  let why = null;
  try { pickPart(entries, 'chart'); } catch (e) { why = e.message; }
  // **없으면 무엇이 들어 있는지 말한다** — 「없다」만으로는 다음에 무엇을 물어야 할지 모른다.
  ok('없는 조각은 있는 것을 알려 준다', why?.includes('ppt/slides/slide1.xml'), why);
}

// **안 잰 것을 안 잰 것으로 적는다**(§9 「초록을 읽는 법」).
console.log('\n※ 이 파일은 PowerPoint 를 안 쓴다. 위 초록은 우리 가지를 잰 것이고, ' +
  '호스트가 실제로 어떻게 답하는지는 S1·S13·S14 가 열려 있는 채다.');
console.log(failed ? `${failed} 실패` : '전부 통과');
process.exit(failed ? 1 : 0);
