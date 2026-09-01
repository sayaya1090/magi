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
import { readFileSync } from 'node:fs';
import { OfficeHand, pickPart } from '../src/adapter/OfficeHand.js';
import { FakeHand } from '../src/adapter/FakeHand.js';
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
      // 같은 `run` 안에서 컬렉션이 자랄 수 있다(`slides.add`·`insertSlidesFromBase64`).
      // 처음에 만든 view 만 보면 **새로 생긴 장이 영영 안 보이고**, 그 장을 찾는 우리 코드가
      // 「만들었는데 못 찾았습니다」로 떨어진다 — 호스트가 아니라 스텁이 만든 실패다.
      let child = target.itemsView.find((v) => v.raw === item);
      if (!child) {
        child = target.makeView ? target.makeView(item) : new Loaded(item, target.pending);
        target.itemsView.push(child);
      }
      reveal(child, rest.join('/'));
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
    // **자리표시자가 아닌 도형에 이 칸을 걸면 호스트가 묶음 전체를 죽인다.** 실물에서 잰 것이라
    // (2026-09-02: 표가 있는 장에서 `read_slide` 가 GeneralException) 스텁도 그렇게 군다 —
    // 안 그러면 이 시험은 우리가 안 겪을 세상만 잰다.
    this.placeholderFormat = new Loaded(raw.placeholderFormat ?? {}, pending);
    this.placeholderFormat.load = (spec) => {
      if (String(raw.type ?? '').toLowerCase() !== 'placeholder') pending.push(['__throw__', 'GeneralException']);
      else pending.push([this.placeholderFormat, spec]);
      return this.placeholderFormat;
    };
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
  // 크기는 물어야 안다 — 실물의 `Table` 도 `load` 를 거친다. 이게 없으면 제자리 교체가
  // 옛 표의 크기를 `null` 로 읽고, 그 값으로 새 표를 짓는다.
  load(spec) { this.pending.push([this, spec]); return this; }
  reveal() {
    this.rowCount = this.raw.cells?.length ?? 0;
    this.columnCount = this.raw.cells?.[0]?.length ?? 0;
  }
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
    const slideView = (s) => {
      const view = new Loaded(s, pending);
      view.itemsView = null;
      view.shapes = makeShapes(s, pending, log);
      view.getImageAsBase64 = () => ({ value: 'PNGBASE64' });
      view.exportAsBase64 = () => ({ value: model.exported ?? 'PPTXBASE64' });
      view.applyLayout = (id) => log.push(`layout:${s.id}:${id}`);
      view.moveTo = (i) => { log.push(`moveTo:${s.id}:${i}`); move(s, i); };
      view.delete = () => { log.push(`slide-delete:${s.id}`); drop(s); };
      return view;
    };
    // 장이 실제로 옮겨지고 지워져야 그다음 `load` 가 사실을 말한다. 로그만 남기면 「옮겼다」와
    // 「옮겼다고 적기만 했다」가 스텁 안에서 같아진다.
    const renumber = () => model.slides.forEach((s, i) => { s.index = i; });
    const move = (s, i) => {
      const at = model.slides.indexOf(s);
      if (at < 0) return;
      model.slides.splice(at, 1);
      model.slides.splice(Math.max(0, Math.min(i, model.slides.length)), 0, s);
      renumber();
    };
    const drop = (s) => {
      const at = model.slides.indexOf(s);
      if (at >= 0) model.slides.splice(at, 1);
      renumber();
    };
    const slidesView = model.slides.map(slideView);
    const slides = new Loaded({ items: model.slides }, pending);
    slides.itemsView = slidesView;
    slides.makeView = slideView;
    slides.add = (options) => {
      log.push(`slides.add:${options?.layoutId ?? ''}:${options?.slideMasterId ?? ''}`);
      const layout = (model.masters ?? [])
        .flatMap((m) => m.layouts).find((l) => l.id === options?.layoutId);
      model.slides.push({
        id: `sl-new${model.slides.length}`,
        index: model.slides.length,
        layout: { name: layout?.name ?? '기본' },
        // 새 장은 레이아웃의 자리표시자를 물려받는다 — 그게 `add_slide` 가 채우는 자리다.
        shapes: (layout?.placeholders ?? ['title', 'body']).map((t, i) => ({
          id: `ph${model.slides.length}-${i}`, name: t, type: 'Placeholder', text: '',
          placeholderFormat: { type: t },
        })),
      });
      renumber();
    };
    slides.getItem = (id) => slidesView.find((v) => v.raw.id === id)
      ?? (() => { throw new Error(`no slide ${id}`); })();
    slides.getItemAt = (i) => slidesView[i] ?? (() => { throw new Error(`no slide at ${i}`); })();

    const masters = new Loaded({ items: model.masters ?? [] }, pending);
    masters.itemsView = (model.masters ?? []).map((m) => {
      const v = new Loaded(m, pending);
      v.layouts = new Loaded({ items: m.layouts }, pending);
      v.layouts.itemsView = m.layouts.map((l) => {
        const lv = new Loaded(l, pending);
        const holders = (l.placeholders ?? []).map((t, i) => ({
          id: `${l.id}-ph${i}`, name: t, type: 'Placeholder', placeholderFormat: { type: t },
        }));
        lv.shapes = new Loaded({ items: holders }, pending);
        lv.shapes.itemsView = holders.map((h) => new StubShape(h, pending, log));
        return lv;
      });
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
        insertSlidesFromBase64: (b64, options) => {
          log.push(`insert:${b64.slice(0, 6)}:${options?.targetSlideId}:${options?.formatting ?? ''}`);
          const at = model.slides.findIndex((s) => s.id === options?.targetSlideId);
          const src = at >= 0 ? model.slides[at] : null;
          const copy = {
            id: `sl-copy${model.slides.length}`,
            index: 0,
            layout: { name: src?.layout?.name ?? '기본' },
            shapes: (src?.shapes ?? []).map((sh) => ({ ...sh, id: `${sh.id}-copy` })),
          };
          model.slides.splice(at < 0 ? model.slides.length : at + 1, 0, copy);
          renumber();
        },
      },
      sync: async () => {
        while (pending.length) {
          const [target, path] = pending.shift();
          // 호스트가 묶음을 죽이는 자리. **남은 요청도 같이 버린다** — 실물의 배치가 그렇다.
          if (target === '__throw__') { pending.length = 0; throw new Error(path); }
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
  coll.addGeometricShape = (geo, opts) => {
    log.push(`addGeometricShape:${geo}:${opts.left},${opts.top}`);
    return new StubShape({ id: 'sh-geo', name: geo, type: 'GeometricShape', text: '' }, pending, log);
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
      shapes: [{ id: 'sh1', name: '제목 1', type: 'Placeholder', text: '전분기 요약', left: 10, top: 20, width: 300, height: 60, placeholderFormat: { type: 'title' }, altTextDescription: null }],
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

// ── 장을 만들고 지우고 복제한다 ──────────────────────────────────────────────
//
// **이것이 없으면 「발표자료 만들어 줘」가 통째로 불가능하다.** 도구 19 개로는 이미 있는 장만
// 고칠 수 있었고, 사람이 새 장을 손으로 만들어 주기 전에는 에이전트가 할 수 있는 일이 없었다.
{
  const withLayouts = () => {
    const m = model();
    m.masters = [{
      id: 'm1', name: '기본',
      layouts: [
        { id: 'l1', name: '제목 및 내용', placeholders: ['title', 'body'] },
        { id: 'l2', name: '제목만', placeholders: ['title'] },
        { id: 'l3', name: '빈 화면', placeholders: [] },
      ],
    }];
    return m;
  };
  const hand = (mm, log) => new OfficeHand({ run: stubRunner(mm, log), supports: () => true, document: 'doc-1' });

  // 레이아웃 이름은 **덱의 테마가 정한다.** 목록 없이 만들게 하면 모델은 흔한 이름을 지어내고,
  // 없는 이름은 거절당한다 — 왕복이 한 번 는다.
  {
    const out = await hand(withLayouts(), []).run('list_layouts', {});
    const layouts = out.result.masters[0].layouts;
    ok('레이아웃 목록이 이름을 싣는다',
      layouts.map((l) => l.layout).join('|') === '제목 및 내용|제목만|빈 화면',
      layouts.map((l) => l.layout).join('|'));
    ok('무엇을 채울 수 있는 장인지도 싣는다',
      layouts[0].placeholders.join(',') === 'title,body', JSON.stringify(layouts[0].placeholders));
    // **자리표시자가 없는 레이아웃**과 못 읽은 것은 다르다 — 빈 배열은 「없다」다.
    ok('빈 레이아웃은 빈 배열이지 null 이 아니다', Array.isArray(layouts[2].placeholders)
      && layouts[2].placeholders.length === 0);
    ok('레이아웃 목록은 덱을 안 고친다', out.count === 0, String(out.count));
  }

  // 만들기 — 레이아웃·자리·제목·본문이 **한 호출**이다. 넷으로 나누면 중간에 실패했을 때
  // 제목 없는 빈 장이 덱에 남고, 그 상태는 아무도 원한 적이 없다.
  {
    const mm = withLayouts(); const log = [];
    const out = await hand(mm, log).run('add_slide',
      { layout: '제목 및 내용', at: 2, title: '3분기 실적', body: '전년 대비 12% 성장' });
    ok('레이아웃을 이름으로 찾아 id 로 만든다',
      log.some((l) => l.startsWith('slides.add:l1:m1')), log.join(' / '));
    ok('사람이 말한 자리는 0-based 로 옮겨 부른다',
      log.some((l) => l.endsWith(':1') && l.startsWith('moveTo:')), log.join(' / '));
    ok('제목과 본문이 자리표시자에 들어간다',
      out.result.filled.join(',') === 'title,body', JSON.stringify(out.result.filled));
    ok('새 장의 id 와 사람 번호를 돌려준다',
      typeof out.result.slide_id === 'string' && out.result.slide === 2,
      `${out.result.slide_id} / ${out.result.slide}`);
    ok('무엇을 만들었는지 한 줄로 적는다',
      out.changed[0].includes('만들었습니다') && out.changed[0].includes('제목 및 내용'),
      out.changed[0]);
    ok('덱을 고친 것으로 센다', out.count === 1, String(out.count));
  }

  // **없는 자리를 지어내지 않는다.** 제목만 있는 레이아웃에 본문을 주면 제목만 들어가고,
  // 결과가 무엇이 들어갔는지 말한다 — 조용히 텍스트 상자를 놓으면 테마 밖의 글이 하나 생긴다.
  {
    const out = await hand(withLayouts(), []).run('add_slide',
      { layout: '제목만', title: '표지', body: '이 장에는 본문 자리가 없다' });
    ok('없는 자리표시자는 안 채우고 안 지어낸다',
      out.result.filled.join(',') === 'title', JSON.stringify(out.result.filled));
    ok('못 넣은 것은 이름을 대고 넘어간다',
      out.result.unfilled.join(',') === 'body', JSON.stringify(out.result.unfilled));
  }

  // 이름을 틀리면 **비슷한 것으로 갈음하지 않는다.** 있는 이름을 다 적어 주면 다음 호출에서 맞다.
  {
    let why = null;
    try { await hand(withLayouts(), []).run('add_slide', { layout: '없는 레이아웃' }); }
    catch (e) { why = e.message; }
    ok('없는 레이아웃은 거절하고 있는 것을 알려 준다',
      why?.includes('제목 및 내용') && why?.includes('빈 화면'), why);
  }

  // 레이아웃을 안 주면 덱의 기본으로 만든다 — **되묻지 않는다.**
  {
    const mm = withLayouts(); const log = [];
    const out = await hand(mm, log).run('add_slide', {});
    ok('레이아웃 없이도 장이 선다', typeof out.result.slide_id === 'string');
    ok('그때는 레이아웃 id 를 안 넘긴다',
      log.some((l) => l === 'slides.add::'), log.join(' / '));
  }

  // 지우기 — **뒤 번호가 전부 밀린다**는 것을 결과가 말해야 한다. 그 말이 없으면 모델은 지우기
  // 전에 읽어 둔 번호로 다음 호출을 건다.
  {
    const mm = withLayouts(); const log = [];
    const out = await hand(mm, log).run('delete_slide', { slide: 1 });
    ok('그 장을 지운다', log.includes('slide-delete:s1'), log.join(' / '));
    ok('지운 자리와 id 를 돌려준다', out.result.deleted === 's1' && out.result.was === 1,
      JSON.stringify(out.result));
    ok('번호가 밀린다는 것을 적는다',
      out.changed[0].includes('당겨졌습니다') && out.changed[0].includes('못 되돌'),
      out.changed[0]);
  }

  // 복제 — **서식을 원본 그대로** 가져온다. `formatting` 을 넘기면 복제본이 테마를 새로 입고
  // 나오고, 그건 「똑같은 장 하나 더」가 아니다(`restore_slide` 가 같은 이유로 같은 선택을 한다).
  {
    const mm = withLayouts(); const log = [];
    const out = await hand(mm, log).run('duplicate_slide', { slide: 1 });
    const insert = log.find((l) => l.startsWith('insert:'));
    ok('원본을 내보내 그 뒤에 넣는다', insert?.includes(':s1:'), insert);
    ok('서식 옵션을 안 넘긴다 — 기본이 원본 유지다', insert?.endsWith(':'), insert);
    ok('복제본은 새 id 를 단다',
      typeof out.result.slide_id === 'string' && out.result.slide_id !== 's1',
      String(out.result.slide_id));
    ok('복제본이 원본 바로 뒤에 선다', out.result.slide === 2, String(out.result.slide));
    ok('id 가 달라졌다는 것을 적는다', out.changed[0].includes('원본과 다릅니다'), out.changed[0]);
  }

  // 넷 다 손이 안다고 **광고**해야 헬퍼가 부를 수 있다.
  {
    const ops = new OfficeHand({}).ops();
    for (const op of ['list_layouts', 'add_slide', 'delete_slide', 'duplicate_slide']) {
      ok(`손이 ${op} 을 광고한다`, ops.includes(op));
    }
  }
}

// ── 광고한 도구는 손이 다 알아야 한다 ────────────────────────────────────────
//
// 도구 표는 Go(`helper/tools.go`)에 있고 그것을 수행하는 손은 여기(JS)에 있다. **두 벌이다.**
// 어긋나면 증상은 런타임의 「이 손은 X 을 모릅니다」 하나뿐이고, 그건 사람이 그 도구를 실제로
// 시켜 봐야 나온다 — 새 도구를 더하는 날이 정확히 그 날이다.
//
// 그래서 목록을 손으로 두 번 적지 않고 **원천에서 유도해 견준다.**
{
  const go = readFileSync(new URL('../../helper/tools.go', import.meta.url), 'utf8');
  const body = go.slice(go.indexOf('return []tool{'));
  const advertised = [...body.matchAll(/Name:\s+"([a-z_]+)",\n\s*Desc:/g)].map((m) => m[1]);
  const known = new OfficeHand({}).ops();

  ok('도구 표에서 이름을 뽑았다', advertised.length >= 20, `${advertised.length}개`);
  const missing = advertised.filter((n) => !known.includes(n));
  ok('광고한 도구를 손이 전부 안다', missing.length === 0, missing.join(', '));
  // 거울도 본다: 손만 알고 아무도 안 부르는 것은 **죽은 코드**다.
  const orphan = known.filter((n) => !advertised.includes(n));
  ok('손이 아는 것 중 안 광고된 것은 없다', orphan.length === 0, orphan.join(', '));

  // **목록을 견주는 것으로는 부족하다.** `ops()` 는 손이 손으로 적은 배열이라, 스위치에 없는
  // 이름도 얼마든지 들어 있을 수 있다 — 실제로 가짜 손이 여섯을 그렇게 광고하고 있었고
  // (format_shape·apply_layout·reorder_slide·set_hyperlink·add_table·set_table_cells),
  // 목록만 견주던 이 시험은 그동안 초록이었다(2026-09-02 리뷰가 짚었다). 그래서 **불러 본다.**
  //
  // 인자는 안 준다. 그래서 대부분 다른 사유로 실패하는데, 그게 맞다 — 여기서 무는 것은
  // 「일이 됐는가」가 아니라 **「그 이름을 아는가」**뿐이다. 모르는 이름의 사유는 하나뿐이다.
  const probe = async (hand, who) => {
    const deaf = [];
    for (const name of advertised) {
      try {
        await hand.run(name, {});
      } catch (e) {
        if (/이 손은 .* 을 모릅니다/.test(e?.message ?? '')) deaf.push(name);
      }
    }
    ok(`${who}이 광고한 도구를 하나도 안 흘린다`, deaf.length === 0, deaf.join(', '));
  };
  await probe(new FakeHand(model()), '가짜 손');
  await probe(new OfficeHand({ run: stubRunner(model(), []), supports: () => true }), '진짜 손');
}

// ── 리뷰가 짚은 갈래들(2026-09-02) ───────────────────────────────────────────
//
// 넷 다 **성공으로 보이는 실패**이거나 그 이웃이다. 이 저장소가 제일 피하려는 모양이라
// 하나씩 못 박는다.
{
  const withLayouts = () => {
    const m = model();
    m.masters = [{
      id: 'm1', name: '기본',
      layouts: [
        { id: 'l1', name: '제목 및 내용', placeholders: ['title', 'body'] },
        { id: 'l2', name: '제목만', placeholders: ['title'] },
        { id: 'l3', name: '빈 화면', placeholders: [] },
      ],
    }];
    return m;
  };
  const hand = (mm, log) => new OfficeHand({ run: stubRunner(mm, log), supports: () => true, document: 'doc-1' });

  // **못 넣은 글을 조용히 버리지 않는다.** 자리표시자가 없는 레이아웃에 제목을 주면 글은
  // 아무 데도 안 들어가는데, 결과가 성공이기만 하면 사람은 제목을 부탁하고 빈 장을 받는다.
  {
    const out = await hand(withLayouts(), []).run('add_slide',
      { layout: '빈 화면', title: '3분기 실적', body: '요약' });
    ok('못 넣은 글의 이름을 결과가 댄다',
      out.result.unfilled.join(',') === 'title,body', JSON.stringify(out.result.unfilled));
    ok('사람이 읽는 줄에도 그 사실이 선다',
      out.changed[0].includes('⚠') && out.changed[0].includes('안 넣었습니다'), out.changed[0]);
    ok('그래도 장은 만들어졌다고 적는다', out.changed[0].includes('만들었습니다'), out.changed[0]);
  }
  {
    const out = await hand(withLayouts(), []).run('add_slide',
      { layout: '제목만', title: '표지', body: '이 장에는 본문 자리가 없다' });
    ok('반만 들어간 것도 반만 들어갔다고 적는다',
      out.result.filled.join(',') === 'title' && out.result.unfilled.join(',') === 'body',
      JSON.stringify(out.result));
  }

  // **덱 길이 밖의 자리는 잘라 준다.** 안 자르면 장을 만든 뒤에 던져서, 사람은 오류와 함께
  // 빈 장 하나를 얻는다 — 「한 호출에 담는다」던 약속이 거기서 깨진다.
  {
    const mm = withLayouts(); const log = [];
    const out = await hand(mm, log).run('add_slide', { layout: '제목만', at: 99, title: '끝장' });
    ok('덱 밖의 자리는 마지막으로 자른다', out.result.slide === mm.slides.length,
      `${out.result.slide} / ${mm.slides.length}`);
    ok('그래도 장은 선다', typeof out.result.slide_id === 'string');
  }

  // **지우기는 넘겨짚지 않는다.** 다른 도구는 생략하면 보고 있는 장으로 가는 편이 편하지만,
  // 이건 스냅샷 없이 못 되돌린다.
  {
    let why = null;
    try { await hand(withLayouts(), []).run('delete_slide', {}); } catch (e) { why = e.message; }
    ok('어느 장인지 안 말하면 안 지운다', why?.includes('정확히 말해'), why);
    const mm = withLayouts();
    ok('안 지웠으므로 장 수도 그대로다', mm.slides.length === 2, String(mm.slides.length));
  }

  // id 로도 짚을 수 있어야 한다 — 번호는 장을 하나 넣고 빼면 전부 밀리므로, 읽어 둔 id 를
  // 되쓰는 쪽이 정확하다(`slideProps` 가 모델에게 그렇게 권한다).
  {
    const mm = withLayouts(); const log = [];
    const out = await hand(mm, log).run('delete_slide', { slide_id: 's2' });
    ok('id 로 지운다', out.result.deleted === 's2' && log.includes('slide-delete:s2'), log.join(' / '));
  }
  {
    const mm = withLayouts(); const log = [];
    const out = await hand(mm, log).run('duplicate_slide', { slide_id: 's2' });
    ok('id 로 복제한다', out.result.from === 's2', JSON.stringify(out.result));
  }

  // **복제본을 못 찾으면 던진다.** 자리를 짐작해 답하면 사람은 없는 장을 있다고 듣는다.
  {
    const mm = withLayouts(); const log = [];
    const h = new OfficeHand({
      // 넣기가 아무 일도 안 하는 호스트. 실물에서 `targetSlideId` 가 낡았을 때의 모양이다.
      run: (fn) => stubRunner(mm, log)(async (context) => {
        context.presentation.insertSlidesFromBase64 = () => log.push('insert:noop');
        return fn(context);
      }),
      supports: () => true,
    });
    let why = null;
    try { await h.run('duplicate_slide', { slide: 1 }); } catch (e) { why = e.message; }
    ok('복제본을 못 찾으면 성공이라고 안 한다', why?.includes('못 찾았습니다'), why);
  }
}

// ── 표가 한 장에 있으면 그 장을 통째로 못 읽던 것 ────────────────────────────
//
// 실물에서 잡았다(2026-09-02). 에이전트가 방금 만든 표가 있는 장에서 `read_slide` 가
// `GeneralException` 으로 떨어졌다 — **「이 장에 뭐가 있나」를 묻는 유일한 도구**가, 표나 그림이
// 하나라도 있는 장에서는 아무 답도 못 하고 있었다. 즉 거의 모든 진짜 슬라이드에서.
//
// 원인은 한 줄이었다: 도형 목록을 읽을 때 `placeholderFormat/type` 을 같이 걸었는데, 자리표시자가
// **아닌** 도형에 그 칸을 걸면 호스트가 묶음 전체를 죽인다. 스텁도 이제 그렇게 군다(`StubShape`).
{
  const mixed = () => ({
    slides: [{
      id: 's1', index: 0, layout: { name: '제목 및 내용' },
      shapes: [
        { id: 'sh1', name: '제목 1', type: 'Placeholder', text: '3분기', left: 10, top: 20, width: 300, height: 60, placeholderFormat: { type: 'title' }, altTextDescription: null },
        // 자리표시자가 아닌 것 셋 — 표·그림·글상자. 실제 덱에 흔한 조합이다.
        { id: 'sh2', name: '표 2', type: 'Table', text: '', left: 10, top: 100, width: 300, height: 120, altTextDescription: null },
        { id: 'sh3', name: '그림 3', type: 'Image', text: '', left: 10, top: 240, width: 100, height: 100, altTextDescription: '로고' },
        { id: 'sh4', name: '글상자 4', type: 'TextBox', text: '각주', left: 10, top: 350, width: 200, height: 30, altTextDescription: null },
      ],
    }],
    masters: [{ id: 'm1', name: '기본', layouts: [{ id: 'l1', name: '제목 및 내용', placeholders: ['title', 'body'] }] }],
  });

  const out = await new OfficeHand({ run: stubRunner(mixed(), []), supports: () => true, document: 'doc-1' })
    .run('read_slide', { slide: 1 });
  ok('표·그림이 섞인 장도 읽힌다', out.result.shapes.length === 4, `${out.result.shapes.length}개`);
  ok('자리표시자의 역할은 그대로 온다',
    out.result.shapes[0].placeholder === 'title', String(out.result.shapes[0].placeholder));
  // **자리표시자가 아닌 것은 `null` 이다** — 「역할이 없다」가 사실이고, 지어낸 역할보다 낫다.
  ok('자리표시자가 아닌 것은 역할이 없다고 적는다',
    out.result.shapes.slice(1).every((s) => s.placeholder === null),
    JSON.stringify(out.result.shapes.map((s) => s.placeholder)));
  ok('나머지 값은 다 살아 온다',
    out.result.shapes[1].name === '표 2' && out.result.shapes[2].alt === '로고'
      && out.result.shapes[3].text === '각주',
    JSON.stringify(out.result.shapes.map((s) => s.name)));

  // 같은 함정이 **새 장을 채울 때**도 있었다. 레이아웃이 로고 그림을 얹어 두면 그 도형에서
  // 묶음이 죽고, 제목도 본문도 안 들어간 채 성공으로 보고된다.
  {
    const mm = mixed();
    mm.masters[0].layouts = [{ id: 'l1', name: '표지', placeholders: ['title'] }];
    const log = [];
    const out2 = await new OfficeHand({ run: stubRunner(mm, log), supports: () => true })
      .run('add_slide', { layout: '표지', title: '표지 제목' });
    ok('레이아웃에서 만든 장도 제목이 들어간다',
      out2.result.filled.join(',') === 'title', JSON.stringify(out2.result));
  }
}

// ── 표를 고쳐 달랬더니 하나 더 만들던 것 ─────────────────────────────────────
//
// 사용자 신고다(2026-09-02): 표를 만들게 하고, 만들어진 걸 보고 **고쳐 달라고 했더니 기존 것을
// 놔두고 새로 넣었다.** 원인이 둘이었다.
//
// 하나 — **고칠 길이 없었다.** 있는 표의 서식·행열은 1.9 라 이 바닥에 없고, 우리가 준 것은
// 「글만 쓰는」 `set_table_cells` 뿐이었다. 「열 하나 더」에는 쓸 도구가 없었다.
// 둘 — **스키마가 그렇게 가르쳤다.** `add_table` 의 설명이 "이 호스트는 있는 표를 못 고치니
// 서식은 만들 때 주라"고 적혀 있었고, 모델은 그것을 「고치려면 새로 만들라」로 읽었다.
//
// 그래서 길을 하나 주고(`replace_table`), 설명을 고치고, **이미 표가 있는 장에 표를 더할 때는
// 결과가 그 사실을 말한다.**
{
  const deck = () => ({
    slides: [{
      id: 's1', index: 0, layout: { name: '제목 및 내용' },
      shapes: [
        { id: 'ph1', name: '제목 1', type: 'Placeholder', text: '분기', placeholderFormat: { type: 'title' }, left: 10, top: 10, width: 300, height: 50 },
        { id: 'tb1', name: '표 2', type: 'Table', text: '', left: 40, top: 120, width: 500, height: 160,
          cells: [['항목', '1월'], ['매출', '10']] },
      ],
    }],
    masters: [{ id: 'm1', name: '기본', layouts: [{ id: 'l1', name: '제목 및 내용', placeholders: ['title', 'body'] }] }],
  });
  const hand = (mm, log) => new OfficeHand({ run: stubRunner(mm, log), supports: () => true, document: 'doc-1' });

  // **더할 때 경고한다.** 막지는 않는다 — 표를 둘 두는 장도 있다. 대신 다음 수를 이름 대어 준다.
  {
    const out = await hand(deck(), []).run('add_table', { slide: 1, rows: 2, columns: 2 });
    ok('이미 있던 표의 수를 결과가 싣는다', out.result.tables_before === 1, String(out.result.tables_before));
    ok('사람이 읽는 줄에 「이미 있다」가 선다',
      out.changed[0].includes('이미 표가 1개') && out.changed[0].includes('replace_table'), out.changed[0]);
  }
  {
    const bare = deck();
    bare.slides[0].shapes = bare.slides[0].shapes.filter((s) => s.type !== 'Table');
    const out = await hand(bare, []).run('add_table', { slide: 1, rows: 2, columns: 2 });
    ok('표가 없던 장에는 경고를 안 붙인다', !out.changed[0].includes('⚠'), out.changed[0]);
  }

  // **제자리에서 다시 짓는다.** 자리·크기는 옛 표의 것을 물려받고, 글도 되도록 옮겨 온다.
  {
    const mm = deck(); const log = [];
    const out = await hand(mm, log).run('replace_table', { slide: 1, columns: 3 });
    const add = log.find((l) => l.startsWith('addTable:'));
    ok('행은 그대로, 열만 늘어난다', out.result.rows === 2 && out.result.columns === 3,
      JSON.stringify(out.result));
    ok('옛 표를 지운다', log.includes('delete:tb1'), log.join(' / '));
    ok('새 표를 만든다', Boolean(add), log.join(' / '));
    ok('옛 표의 크기를 결과가 같이 적는다',
      out.result.was.rows === 2 && out.result.was.columns === 2, JSON.stringify(out.result.was));
    ok('id 가 바뀐다는 것을 사람이 읽는 줄이 적는다',
      out.changed[0].includes('옛 id 는 이제 없습니다'), out.changed[0]);
  }

  // 표가 여럿이면 **안 고른다** — 골라 주면 엉뚱한 표가 사라지고 그건 못 되돌린다.
  {
    const mm = deck();
    mm.slides[0].shapes.push({ id: 'tb2', name: '표 3', type: 'Table', text: '', left: 40, top: 320, width: 300, height: 100, cells: [['x']] });
    let why = null;
    try { await hand(mm, []).run('replace_table', { slide: 1 }); } catch (e) { why = e.message; }
    ok('표가 여럿이면 어느 것인지 묻는다',
      why?.includes('tb1') && why?.includes('tb2'), why);
  }
  {
    const mm = deck();
    mm.slides[0].shapes = mm.slides[0].shapes.filter((s) => s.type !== 'Table');
    let why = null;
    try { await hand(mm, []).run('replace_table', { slide: 1 }); } catch (e) { why = e.message; }
    ok('표가 없으면 add_table 을 가리킨다', why?.includes('add_table'), why);
  }

  // 스키마가 **가르치는 말**도 시험한다 — 이 결함의 절반이 설명문이었다.
  {
    const go = readFileSync(new URL('../../helper/tools.go', import.meta.url), 'utf8');
    const desc = (name) => {
      const at = go.indexOf(`Name: "${name}"`);
      return at < 0 ? '' : go.slice(at, go.indexOf('\n', go.indexOf('Desc:', at)));
    };
    ok('add_table 이 「고치려면 다른 도구」라고 말한다',
      /replace_table/.test(desc('add_table')) && /set_table_cells/.test(desc('add_table')),
      desc('add_table').slice(0, 80));
    ok('set_table_cells 가 먼저 잡을 도구라고 말한다',
      /first thing to reach for/i.test(desc('set_table_cells')), desc('set_table_cells').slice(0, 80));
    ok('replace_table 이 「하나 더 만드는 게 아니다」라고 말한다',
      /not what was asked/i.test(desc('replace_table')), desc('replace_table').slice(0, 80));
  }
}

// ── 말로 부르는 도형 이름 ────────────────────────────────────────────────────
//
// 넷만 알던 자리다(사각형·둥근사각형·타원·선). API 한계가 아니라 처음에 좁게 잡은 것이었고,
// 사람이 「화살표 그려 줘」라고 하면 손이 모른다고 답했다. 사용자가 「테이블 말고 뭘 그릴 수
// 있냐」고 물어 넓혔다(2026-09-02).
{
  const hand = (log) => new OfficeHand({ run: stubRunner(model(), log), supports: () => true, document: 'doc-1' });
  const geoOf = async (kind) => {
    const log = [];
    // 모르는 이름은 던진다 — 여기서는 「못 알아봤다」로 접어 목록에 담는다.
    try { await hand(log).run('add_shape', { slide: 1, kind, text: 'ㄱ' }); } catch { return undefined; }
    return log.find((l) => l.startsWith('addGeometricShape:'))?.split(':')[1];
  };
  ok('영문 표준명을 그대로 받는다', await geoOf('rightArrow') === 'rightArrow');
  // **한국어로도 받는다.** 한국어 대화 중에 모델이 한국어 이름을 넘기는 것이 자연스럽고,
  // 거기서 거절하면 왕복이 한 번 는다.
  ok('한국어 이름도 받는다', await geoOf('삼각형') === 'triangle');
  ok('별은 star5 로 간다', await geoOf('별') === 'star5');
  ok('말풍선도 안다', await geoOf('말풍선') === 'wedgeRectCallout');
  ok('순서도 판단 기호도 안다', await geoOf('판단') === 'flowChartDecision');
  // 띄어쓰기·대소문자·밑줄은 같은 것으로 본다 — 모델이 어느 쪽으로 쓸지 우리가 못 정한다.
  ok('모양이 조금 다른 표기도 같은 것으로 본다',
    await geoOf('round rectangle') === 'roundRectangle'
      && await geoOf('ROUND_RECTANGLE') === 'roundRectangle');
  ok('글상자는 여전히 기본이다', await geoOf('textbox') === undefined);

  // **모르는 이름은 지어내지 않고, 아는 것을 알려 준다.**
  let why = null;
  try { await hand([]).run('add_shape', { slide: 1, kind: '우주선' }); } catch (e) { why = e.message; }
  ok('모르는 도형은 거절하고 목록을 준다',
    why?.includes('우주선') && why?.includes('rightArrow') && why?.includes('triangle'),
    why?.slice(0, 80));

  // 스키마가 광고하는 이름은 **손이 다 알아야 한다** — 광고와 실행이 어긋나면 모델은 광고된
  // 이름을 부르고 거절당한다.
  const go = readFileSync(new URL('../../helper/tools.go', import.meta.url), 'utf8');
  // 스키마의 `kind` 설명문에서 **쉼표로 나열된 이름들만** 뽑는다. 산문까지 긁으면 영어 낱말이
  // 도형 이름으로 세어져, 이 시험이 자기가 만든 헛것을 잡으려 든다.
  const at = go.indexOf(String.fromCharCode(34) + 'kind' + String.fromCharCode(34));
  const desc = at < 0 ? '' : go.slice(at, go.indexOf(String.fromCharCode(10), at));
  const listed = /geometric shape: ([^.]+)./.exec(desc)?.[1] ?? '';
  const advertised = listed.split(',').map((w) => w.trim())
    .filter((w) => /^[a-zA-Z][A-Za-z0-9]+$/.test(w));
  const unknown = [];
  for (const w of advertised) {
    const got = await geoOf(w);
    if (!got) unknown.push(w);
  }
  ok('광고한 도형 이름을 뽑았다', advertised.length > 20, advertised.length + '개');
  ok('광고한 도형을 손이 전부 안다', unknown.length === 0, unknown.join(', '));
}
// ── 「이 장에 뭐라고 쓰여 있나」에 답할 수 있는가 ─────────────────────────────
//
// 사용자가 물었다(2026-09-02): 「슬라이드를 모델이 이해할 수 있는 수준은 되나? 어떤 내용의
// 슬라이드인지」. 재 보니 **아니었다.** 표가 하나 있는 장을 읽었더니 도형 종류와 자리만 오고
// **제목까지 포함해 글이 전부 빈 문자열**이었다 — 글틀 없는 도형에 글을 물어 묶음이 죽고,
// `catch` 가 그것을 「글 없음」으로 삼켰기 때문이다. 모델은 그 장의 내용을 하나도 모르는 채
// 답을 지어야 했다.
{
  const mixed = () => ({
    slides: [{
      id: 's1', index: 0, layout: { name: '제목 및 내용' },
      shapes: [
        { id: 'ph1', name: '제목 1', type: 'Placeholder', text: '3분기 실적', placeholderFormat: { type: 'title' }, left: 10, top: 10, width: 300, height: 50, altTextDescription: null },
        { id: 'sh2', name: '화살표', type: 'GeometricShape', text: '흐름', left: 10, top: 80, width: 100, height: 40, altTextDescription: null },
        { id: 'tb1', name: '표 3', type: 'Table', text: '', left: 40, top: 140, width: 400, height: 120, altTextDescription: null,
          cells: [['항목', '1월'], ['매출', '10']] },
        { id: 'im4', name: '그림 4', type: 'Image', text: '', left: 500, top: 140, width: 120, height: 120, altTextDescription: '로고' },
      ],
    }],
    masters: [{ id: 'm1', name: '기본', layouts: [{ id: 'l1', name: '제목 및 내용', placeholders: ['title'] }] }],
  });

  const out = await new OfficeHand({ run: stubRunner(mixed(), []), supports: () => true, document: 'doc-1' })
    .run('read_slide', { slide: 1 });
  const by = Object.fromEntries(out.result.shapes.map((s) => [s.shape_id, s]));

  // **글이 온다.** 이게 안 되면 모델은 장의 내용을 모른다.
  ok('표가 있어도 제목 글이 온다', by.ph1.text === '3분기 실적', JSON.stringify(by.ph1.text));
  ok('도형 안의 글도 온다', by.sh2.text === '흐름', JSON.stringify(by.sh2.text));
  ok('글이 통째로 날아가지 않았다', out.result.text_unavailable !== true);

  // **표는 격자로 온다.** 「표가 하나 있다」까지만 알면 「이 표 고쳐 줘」에 쓸 것이 없다.
  ok('표의 칸이 격자로 실린다',
    by.tb1.rows === 2 && by.tb1.columns === 2
      && by.tb1.cells[0][0] === '항목' && by.tb1.cells[1][1] === '10',
    JSON.stringify(by.tb1.cells));
  // 표가 아닌 도형에는 격자 칸 자체를 안 만든다 — 빈 격자는 「빈 표」로 읽힌다.
  ok('표가 아닌 것에는 격자를 안 붙인다', by.ph1.cells === undefined && by.im4.cells === undefined);
  // 그림은 대체 텍스트가 유일한 단서다.
  ok('그림은 대체 텍스트로 말한다', by.im4.alt === '로고', String(by.im4.alt));
}

// ── 「이 제목 몇 pt 야?」 ─────────────────────────────────────────────────────
//
// 바꾸는 것은 되는데(`format_shape`) **지금 값을 읽는 길이 없었다.** 모델은 자기가 방금 바꾼
// 값도 확인 못 했고, 「좀 키워 줘」 같은 상대적인 부탁에 기준이 없었다. 글을 읽는 그 왕복에서
// 같이 읽으면 되는 자리라 왕복도 안 는다.
{
  const styled = () => ({
    slides: [{
      id: 's1', index: 0, layout: { name: '제목 및 내용' },
      shapes: [
        { id: 'ph1', name: '제목 1', type: 'Placeholder', text: '분기 실적',
          placeholderFormat: { type: 'title' }, left: 10, top: 10, width: 300, height: 50,
          altTextDescription: null, font: { name: '맑은 고딕', size: 40, bold: true, italic: false, color: '#242424' } },
        { id: 'sh2', name: '메모', type: 'GeometricShape', text: '초안', left: 10, top: 80,
          width: 100, height: 40, altTextDescription: null, font: { size: 12 } },
        { id: 'tb3', name: '표', type: 'Table', text: '', left: 10, top: 140, width: 200, height: 80,
          altTextDescription: null, cells: [['a']] },
      ],
    }],
    masters: [{ id: 'm1', name: '기본', layouts: [{ id: 'l1', name: '제목 및 내용', placeholders: ['title'] }] }],
  });

  const out = await new OfficeHand({ run: stubRunner(styled(), []), supports: () => true, document: 'doc-1' })
    .run('read_slide', { slide: 1 });
  const by = Object.fromEntries(out.result.shapes.map((s) => [s.shape_id, s]));
  ok('제목의 글꼴과 크기가 온다',
    by.ph1.font?.name === '맑은 고딕' && by.ph1.font?.size === 40, JSON.stringify(by.ph1.font));
  ok('굵게·색도 같이 온다',
    by.ph1.font?.bold === true && by.ph1.font?.color === '#242424', JSON.stringify(by.ph1.font));
  // **호스트가 안 준 칸은 안 싣는다** — `null` 로 채우면 「글꼴이 없다」로 읽히고, 한 상자에
  // 서식이 섞였을 때 호스트가 실제로 그렇게 답한다.
  ok('안 온 칸은 아예 없다',
    by.sh2.font?.size === 12 && by.sh2.font?.name === undefined, JSON.stringify(by.sh2.font));
  // 글틀이 없는 도형에는 글꼴 칸 자체가 없다.
  ok('표에는 글꼴 칸이 안 붙는다', by.tb3.font === undefined, JSON.stringify(by.tb3.font));
}

// **안 잰 것을 안 잰 것으로 적는다**(§9 「초록을 읽는 법」).
console.log('\n※ 이 파일은 PowerPoint 를 안 쓴다. 위 초록은 우리 가지를 잰 것이고, ' +
  '호스트가 실제로 어떻게 답하는지는 S1·S13·S14 가 열려 있는 채다.');
console.log(failed ? `${failed} 실패` : '전부 통과');
process.exit(failed ? 1 : 0);
