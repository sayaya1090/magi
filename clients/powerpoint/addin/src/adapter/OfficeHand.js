import { HandPort } from '../port/HandPort.js';
import { fromBase64, zipEntries, zipRead } from './zip.js';

/**
 * 손을 Office.js 로 구현한다. **이 파일과 `OfficeDeck` 만 Office 를 안다.**
 *
 * ⚠ **시험은 여전히 호스트를 안 잰다.** 2026-09-01 에 이 손이 실물 PowerPoint 에 붙어 덱을
 * 고쳤지만(그날 결함 열둘이 나왔다 — `docs/TESTING.ko.md` §5.1), `tools/officehand.mjs` 가
 * 도는 자리는 `PowerPoint.run` 을 흉내 낸 stub 위다. 거기서 무는 것은 **우리가 고른
 * 가지**(요구 집합을 먼저 묻는가 · 없는 것을 지어내지 않는가 · 쓰기가 바뀐 값을 싣는가)뿐이고,
 * 호스트가 실제로 어떻게 답하는지는 사람이 점검표로 잰다. 안 돌려 본 것을 "된다"고 세지 않는다.
 *
 * # 바닥이 1.8 이고, 그 위의 것은 도구로 안 낸다
 *
 * 이미 있는 표의 서식·구조(1.9), 페이지 크기(1.10), 덱 통째 내보내기(1.10)는 **여기 없다.**
 * 못 하는 것을 광고하면 §2.3 이 최악이라고 적은 실패가 난다 — 「고쳤습니다」 하고 안 바뀌는 것.
 * 그리고 1.6(하이퍼링크)처럼 **바닥과 천장 사이**에 있는 것은 호스트에게 먼저 묻고, 없으면
 * 그렇게 말한다(LTSC 2024 에는 선택이 있고 하이퍼링크가 없다 — 부록 A).
 */
export class OfficeHand extends HandPort {
  /**
   * @param {{run?:Function, supports?:(name:string,version:string)=>boolean,
   *          document?:string, label?:string}} deps
   */
  constructor({ run, supports, document = '', label = '' } = {}) {
    super();
    this.runner = run ?? ((fn) => PowerPoint.run(fn));
    this.supports = supports ?? ((name, version) => {
      const req = (typeof Office !== 'undefined') && Office.context && Office.context.requirements;
      if (!req || typeof req.isSetSupported !== 'function') return false;
      try { return req.isSetSupported(name, version) === true; } catch { return false; }
    });
    this.document = document;
    this.labelText = label;
    // 개정 쌍(§5.6). **epoch 는 이 손이 사는 동안의 신원**이고, 다시 뜨면 다른 값이 된다 —
    // 그 다름이 곧 「그 사이를 아무도 못 봤다」는 사실이다.
    this.epoch = Math.floor(Date.now() / 1000) % 2147483647;
    this.count = 0;
    this.snapshots = new Map();
    this.nextSnap = 1;
  }

  get label() { return this.labelText || 'PowerPoint (Office.js)'; }

  ops() {
    return ['list_slides', 'read_slide', 'find_shapes', 'render_slide', 'export_slide_ooxml',
      'set_text', 'format_shape', 'move_shape', 'add_shape', 'delete_shape', 'apply_layout',
      'reorder_slide', 'set_hyperlink', 'add_table', 'set_table_cells',
      'snapshot_slide', 'restore_slide', 'advise', 'clear_advice',
      'list_layouts', 'add_slide', 'delete_slide', 'duplicate_slide', 'replace_table'];
  }

  #envelope(result, changed = []) {
    return {
      document: this.document, label: this.labelText, result, changed,
      epoch: this.epoch, count: this.count,
    };
  }

  #mutated() { this.count += 1; }

  /**
   * 슬라이드 하나를 집는다. **위치는 1 부터**이고(CAPABILITIES.md §10.4), 받은 즉시 id 로
   * 옮긴다 — 그다음은 id 로 일한다. 생략하면 **사람이 보고 있는 장**이다: 문서가
   * `getSelectedSlides()` 의 첫 항목을 활성 슬라이드로 보장한다(부록 A —
   * `getActiveSlideOrNullObject` 는 프리뷰라 안 쓴다).
   */
  async #slide(context, args) {
    if (args.slide_id) {
      const s = context.presentation.slides.getItem(args.slide_id);
      s.load('id,index');
      await context.sync();
      return s;
    }
    if (args.slide !== undefined) {
      const s = context.presentation.slides.getItemAt(Number(args.slide) - 1);
      s.load('id,index');
      await context.sync();
      return s;
    }
    const sel = context.presentation.getSelectedSlides();
    sel.load('items/id,items/index');
    await context.sync();
    const first = sel.items[0];
    if (!first) {
      throw new Error('어느 슬라이드인지 알 수 없습니다 — slide 나 slide_id 를 주거나, 사람이 슬라이드를 하나 고르게 하세요');
    }
    return first;
  }

  /**
   * 도형들의 **자리표시자 역할**을 따로 읽어 온다. 자리표시자가 아닌 도형에 `placeholderFormat`
   * 을 걸면 호스트가 `GeneralException` 을 던지고 **그 묶음이 통째로** 죽으므로, 종류를 먼저
   * 보고 자리표시자인 것만 두 번째 왕복에서 묻는다.
   *
   * 그래도 던지면 **역할만 포기하고 나머지는 살린다** — 슬라이드를 통째로 못 읽는 것보다
   * 「이 도형의 역할은 모른다」가 훨씬 덜 나쁘다. 어느 쪽인지는 값이 말한다(`null`).
   *
   * @returns {Promise<Map<string,string>>} shape id → 역할
   */
  async #placeholderRoles(context, items) {
    // 종류 이름의 대소문자는 호스트가 정한다(실물은 `Placeholder`, 열거 문서는 `placeholder`).
    // 한쪽만 보면 다른 판에서 역할이 통째로 비고, 그건 조용한 실패다.
    const holders = items.filter((s) => String(s.type ?? '').toLowerCase() === 'placeholder');
    // 칸 자체가 없는 판(오래된 호스트·흉내 낸 객체)에서는 **물을 것이 없다.** 물을 수 없는
    // 것을 물어 터지는 것보다 「역할은 모른다」로 두는 편이 낫다.
    const askable = holders.filter((s) => typeof s.placeholderFormat?.load === 'function');
    if (askable.length === 0) return new Map();
    for (const s of askable) s.placeholderFormat.load('type');
    try {
      await context.sync();
    } catch {
      return new Map();
    }
    return new Map(askable.map((s) => [s.id, s.placeholderFormat?.type ?? null]));
  }

  async run(op, args = {}) {
    switch (op) {
      case 'list_slides': return this.#listSlides(args);
      case 'read_slide': return this.#readSlide(args);
      case 'find_shapes': return this.#findShapes(args);
      case 'render_slide': return this.#render(args);
      case 'export_slide_ooxml': return this.#ooxml(args);
      case 'set_text': return this.#setText(args);
      case 'format_shape': return this.#format(args);
      case 'move_shape': return this.#move(args);
      case 'add_shape': return this.#addShape(args);
      case 'delete_shape': return this.#deleteShape(args);
      case 'list_layouts': return this.#listLayouts();
      case 'add_slide': return this.#addSlide(args);
      case 'delete_slide': return this.#deleteSlide(args);
      case 'duplicate_slide': return this.#duplicateSlide(args);
      case 'apply_layout': return this.#applyLayout(args);
      case 'reorder_slide': return this.#reorder(args);
      case 'set_hyperlink': return this.#hyperlink(args);
      case 'add_table': return this.#addTable(args);
      case 'replace_table': return this.#replaceTable(args);
      case 'set_table_cells': return this.#setCells(args);
      case 'snapshot_slide': return this.#snapshot(args);
      case 'restore_slide': return this.#restore(args);
      case 'advise':
      case 'clear_advice':
        // 안내는 **덱을 안 고친다**(§6.1). 창이 로그의 도구 호출을 접어 그리므로 손이 할 일은
        // 「받았다」뿐이고, `changed` 를 안 싣는 것이 계약이다 — 안내는 한 일이 아니라 할 말이다.
        return this.#envelope({ pinned: op === 'advise' ? (args.items?.length ?? 0) : 0 });
      default:
        throw new Error(`이 손은 ${op} 을 모릅니다`);
    }
  }

  #listSlides(args) {
    return this.runner(async (context) => {
      const slides = context.presentation.slides;
      slides.load('items/id,items/index,items/layout/name');
      await context.sync();
      const from = Math.max(1, Number(args.from ?? 1));
      const count = args.count === undefined ? slides.items.length : Number(args.count);
      const want = slides.items.slice(from - 1, from - 1 + count);
      // 도형 수는 항목마다 세되 **왕복 하나에 몰아** 묻는다. 슬라이드 100 장이면 이 차이가
      // 그대로 S6 의 수가 된다(§9).
      const counts = want.map((s) => s.shapes.getCount());
      await context.sync();
      return this.#envelope({
        total: slides.items.length,
        slides: want.map((s, i) => ({
          // `index` 는 0 부터다(부록 A). 사람에게 보이는 번호는 +1 이고, 그 +1 을 여기서 한다.
          slide: (typeof s.index === 'number' ? s.index : from - 1 + i) + 1,
          slide_id: s.id,
          layout: s.layout?.name ?? null,
          shapes: counts[i].value,
        })),
      });
    });
  }

  #readSlide(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const shapes = slide.shapes;
      // ⚠ **`placeholderFormat` 을 여기서 같이 안 읽는다.** 자리표시자가 아닌 도형(표·그림·
      // 텍스트 상자)에 그 칸을 걸면 호스트가 `GeneralException` 을 던지고 **묶음 전체가**
      // 죽는다 — 그러면 도형이 하나라도 자리표시자가 아닌 슬라이드는 통째로 못 읽는다.
      // 실물에서 그 화면을 봤다(2026-09-02): 방금 만든 표가 있는 장에서 `read_slide` 가
      // GeneralException 으로 떨어졌고, 그건 「이 장에 뭐가 있나」를 묻는 유일한 도구다.
      shapes.load('items/id,items/name,items/type,items/left,items/top,items/width,items/height,' +
        'items/altTextDescription');
      slide.load('id,index,layout/name');
      await context.sync();

      const roles = await this.#placeholderRoles(context, shapes.items);

      // 글은 **두 번째 왕복**에서. 도형에 `textFrame` 이 없을 수 있어 통째로 실패할 수 있고,
      // 그때는 글을 포기하고 **신원은 살린다** — 다만 포기했다는 사실을 실어 보낸다.
      // ⚠ **글틀이 없는 도형에 글을 물으면 묶음 전체가 죽는다** — 그러면 `catch` 가 모든 글을
      // 빈 글로 만들고, 화면은 「글이 없는 슬라이드」를 그린다. 실물에서 그 답을 봤다
      // (2026-09-02): 표가 하나 있는 장을 읽었더니 **제목까지 포함해 글이 전부 빈 문자열**로
      // 왔다. 모델은 그 장에 뭐라고 쓰여 있는지 하나도 모르는 채로 답을 지어야 했다.
      // 같은 함정의 세 번째 발현이라(placeholderFormat · find_shapes), 답도 같다.
      const frames = shapes.items.map((s) => {
        if (TEXTLESS.has(String(s.type ?? '').toLowerCase())) return null;
        const tf = s.textFrame;
        tf.textRange.load('text');
        return tf;
      });
      let texts;
      let textUnavailable = false;
      try {
        await context.sync();
        texts = shapes.items.map((s, i) => (frames[i] ? (frames[i].textRange.text ?? '') : ''));
      } catch {
        texts = shapes.items.map(() => '');
        textUnavailable = true;
      }

      // 표는 **도형 하나가 아니라 격자**다. 칸의 글을 안 실으면 모델은 「여기 표가 하나 있다」
      // 까지만 알고 **무슨 내용인지는 모른다** — 「이 표 고쳐 줘」에 쓸 것이 하나도 없다.
      // 실물에서 그 화면을 봤다(2026-09-02): 방금 만든 표를 다시 읽었더니 종류와 크기만 왔다.
      const grids = new Map();
      for (const sh of shapes.items) {
        if (String(sh.type ?? '').toLowerCase() !== 'table') continue;
        const got = await this.#readTableText(context, sh);
        if (got.values?.length) grids.set(sh.id, got);
      }

      return this.#envelope({
        slide: (slide.index ?? 0) + 1,
        slide_id: slide.id,
        layout: slide.layout?.name ?? null,
        text_unavailable: textUnavailable,
        shapes: shapes.items.map((s, i) => ({
          shape_id: s.id,
          name: s.name,
          type: s.type,
          placeholder: roles.get(s.id) ?? null,
          alt: s.altTextDescription ?? null,
          left: s.left, top: s.top, width: s.width, height: s.height,
          text: texts[i],
          // 표면 격자를 그대로. **없으면 칸을 안 만든다** — 빈 격자는 「빈 표」로 읽힌다.
          ...(grids.has(s.id)
            ? { rows: grids.get(s.id).rows, columns: grids.get(s.id).columns, cells: grids.get(s.id).values }
            : {}),
        })),
        // **없는 것이 아니라 못 읽는 것이다**(CAPABILITIES.md §10.5). 모델에게 노트가 *없다*고
        // 말하면 노트가 없는 덱이라고 믿고, 필요할 때 다른 길을 안 쓴다.
        unreadable: ['notes', 'animation', 'transition', 'chart-data'],
      });
    });
  }

  #findShapes(args) {
    return this.runner(async (context) => {
      const slides = context.presentation.slides;
      slides.load('items/id,items/index');
      await context.sync();
      const pick = args.slide_id || args.slide !== undefined
        ? [await this.#slide(context, args)] : slides.items;
      for (const s of pick) {
        s.shapes.load('items/id,items/name,items/type');
      }
      await context.sync();

      const wantText = String(args.text ?? '').toLowerCase();
      const hits = [];
      for (const s of pick) {
        for (const sh of s.shapes.items) {
          if (args.type && sh.type !== args.type) continue;
          if (args.name && !String(sh.name ?? '').includes(args.name)) continue;
          hits.push({ slide: (s.index ?? 0) + 1, slide_id: s.id, shape_id: sh.id, name: sh.name, type: sh.type });
        }
      }
      if (!wantText) {
        return this.#envelope({ shapes: hits.slice(0, Number(args.limit ?? 50)) });
      }
      // 글로 거르려면 한 왕복이 더 든다. **거르는 조건이 없으면 안 든다** — 100 장 덱에서 이
      // 차이가 S6 이 재는 수다.
      //
      // ⚠ **글틀이 없는 도형에 글을 물으면 묶음 전체가 죽는다.** 표·그림·차트가 그렇고, 실물에서
      // 이 도구가 통째로 `InvalidArgument` 로 떨어졌다(2026-09-02 전수 점검). `read_slide` 의
      // `placeholderFormat` 과 같은 함정이고, 답도 같다 — **물을 수 있는 것에만 묻고, 그래도
      // 터지면 신원은 살린다.**
      const kept = [];
      for (const h of hits) {
        if (!TEXTLESS.has(String(h.type ?? '').toLowerCase())) {
          const shape = context.presentation.slides.getItem(h.slide_id).shapes.getItem(h.shape_id);
          shape.textFrame.textRange.load('text');
          kept.push({ hit: h, shape });
        }
      }
      let read = true;
      try {
        await context.sync();
      } catch {
        read = false;
      }
      if (!read) {
        // **글을 못 읽었으면 「글로 걸렀다」고 하지 않는다.** 못 거른 목록을 거른 것처럼 주면
        // 모델이 「그런 글은 없다」로 읽는다.
        return this.#envelope({
          shapes: hits.slice(0, Number(args.limit ?? 50)),
          text_unavailable: true,
          note: '이 호스트가 도형의 글을 안 내줘서 text 로 못 걸렀습니다 — 아래는 거르기 전 목록입니다',
        });
      }
      const out = [];
      for (const { hit, shape } of kept) {
        const text = shape.textFrame?.textRange?.text ?? '';
        if (text.toLowerCase().includes(wantText)) out.push({ ...hit, text });
      }
      return this.#envelope({ shapes: out.slice(0, Number(args.limit ?? 50)) });
    });
  }

  #render(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const image = slide.getImageAsBase64();
      await context.sync();
      // 헬퍼가 이 둘을 보고 **그림 블록**으로 실어 보낸다(§4.4 ①). 개정 3 에 따라 이 경로는
      // 아껴 쓴다 — 붙을 모델이 멀티모달이라는 보장이 없고, **카운슬은 어느 경우에도 그림을
      // 못 본다**(§7).
      return this.#envelope({
        slide_id: slide.id,
        image_base64: image.value,
        image_mime: 'image/png',
      });
    });
  }

  #ooxml(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const packed = slide.exportAsBase64();
      await context.sync();
      const bytes = fromBase64(packed.value);
      const { entries } = zipEntries(bytes);
      const part = String(args.part ?? 'slide');
      if (part === 'list') {
        // **좁히는 것은 우리 일이다**(§7). 목록만 주면 다음 호출이 정확히 하나를 집는다.
        return this.#envelope({
          slide_id: slide.id,
          parts: entries.map((e) => ({ name: e.name, bytes: e.size })),
        });
      }
      const name = pickPart(entries, part);
      const xml = await zipRead(bytes, name);
      return this.#envelope({ slide_id: slide.id, part: name, xml });
    });
  }

  #setText(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const shape = slide.shapes.getItem(args.shape_id);
      shape.textFrame.textRange.load('text');
      await context.sync();
      const before = shape.textFrame.textRange.text ?? '';
      shape.textFrame.textRange.text = String(args.text ?? '');
      await context.sync();
      this.#mutated();
      return this.#envelope(
        { slide_id: slide.id, shape_id: args.shape_id, text: args.text },
        [`슬라이드 ${slide.id} · 도형 ${args.shape_id}: "${before}" → "${args.text}"`]);
    });
  }

  #format(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const shape = slide.shapes.getItem(args.shape_id);
      const font = shape.textFrame.textRange.font;
      font.load('name,size,bold,italic,color');
      await context.sync();
      const before = { name: font.name, size: font.size, bold: font.bold, italic: font.italic, color: font.color };

      const changed = [];
      if (args.font !== undefined) { font.name = args.font; changed.push(`글꼴 ${before.name} → ${args.font}`); }
      if (args.size !== undefined) { font.size = Number(args.size); changed.push(`크기 ${before.size} → ${args.size}pt`); }
      if (args.bold !== undefined) { font.bold = Boolean(args.bold); changed.push(`굵게 ${before.bold} → ${args.bold}`); }
      if (args.italic !== undefined) { font.italic = Boolean(args.italic); changed.push(`기울임 ${before.italic} → ${args.italic}`); }
      if (args.color !== undefined) { font.color = args.color; changed.push(`글자색 ${before.color} → ${args.color}`); }
      if (args.fill !== undefined) {
        if (String(args.fill).toLowerCase() === 'none') shape.fill.clear();
        else shape.fill.setSolidColor(String(args.fill));
        changed.push(`채움 → ${args.fill}`);
      }
      if (args.align !== undefined) {
        shape.textFrame.textRange.paragraphFormat.horizontalAlignment = String(args.align);
        changed.push(`정렬 → ${args.align}`);
      }
      if (changed.length === 0) {
        // **아무것도 안 바꿨으면 바꿨다고 말하지 않는다.** 「wrote N bytes」가 변화로 읽히는
        // 자리를 magi 가 이미 한 번 겪었다(ARCHITECTURE §4 의 self-revert).
        throw new Error('무엇을 바꿀지가 하나도 안 왔습니다 — font·size·bold·italic·color·fill·align 중 하나는 주세요');
      }
      await context.sync();
      this.#mutated();
      return this.#envelope({ slide_id: slide.id, shape_id: args.shape_id },
        [`슬라이드 ${slide.id} · 도형 ${args.shape_id}: ${changed.join(', ')}`]);
    });
  }

  #move(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const shape = slide.shapes.getItem(args.shape_id);
      shape.load('left,top,width,height');
      await context.sync();
      const before = { left: shape.left, top: shape.top, width: shape.width, height: shape.height };
      for (const key of ['left', 'top', 'width', 'height']) {
        if (args[key] !== undefined) shape[key] = Number(args[key]);
      }
      await context.sync();
      this.#mutated();
      return this.#envelope(
        { slide_id: slide.id, shape_id: args.shape_id, left: shape.left, top: shape.top, width: shape.width, height: shape.height },
        [`슬라이드 ${slide.id} · 도형 ${args.shape_id}: ` +
          `(${before.left}, ${before.top}) ${before.width}×${before.height}pt → ` +
          `(${shape.left}, ${shape.top}) ${shape.width}×${shape.height}pt`]);
    });
  }

  #addShape(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const options = {
        left: Number(args.left ?? 100), top: Number(args.top ?? 100),
        width: Number(args.width ?? 200), height: Number(args.height ?? 60),
      };
      const kind = String(args.kind ?? 'textbox');
      const shape = kind === 'textbox'
        ? slide.shapes.addTextBox(String(args.text ?? ''), options)
        : slide.shapes.addGeometricShape(geometryOf(kind), options);
      shape.load('id');
      await context.sync();
      if (kind !== 'textbox' && args.text) {
        shape.textFrame.textRange.text = String(args.text);
        await context.sync();
      }
      this.#mutated();
      return this.#envelope({ slide_id: slide.id, shape_id: shape.id },
        [`슬라이드 ${slide.id}: ${kind} 도형 ${shape.id} 추가${args.text ? `("${args.text}")` : ''}`]);
    });
  }

  #deleteShape(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const shape = slide.shapes.getItem(args.shape_id);
      shape.load('name,type');
      await context.sync();
      const what = `${shape.name ?? args.shape_id}(${shape.type ?? '?'})`;
      shape.delete();
      await context.sync();
      this.#mutated();
      // **되돌릴 수 없다는 것을 결과가 말한다**(§2.1 — 태그 저널은 지운 것을 못 되살린다).
      return this.#envelope({ slide_id: slide.id, deleted: args.shape_id },
        [`슬라이드 ${slide.id}: 도형 ${args.shape_id} ${what} 삭제 — 스냅샷 없이는 못 되돌립니다`]);
    });
  }

  /**
   * 이 덱이 가진 레이아웃 전부. **장을 만들기 전에 읽는 자리다.**
   *
   * 레이아웃 이름은 덱마다 다르다(테마가 정한다). 목록 없이 만들게 하면 모델은 「제목 및 내용」
   * 같은 흔한 이름을 지어내고, 없는 이름은 `add_slide` 가 거절한다 — 왕복이 한 번 는다.
   * 자리표시자 역할까지 같이 싣는 이유도 같다: **무엇을 채울 수 있는 장인지**를 알아야
   * 「제목만 있는 장」과 「제목+본문 장」을 고를 수 있다.
   */
  #listLayouts() {
    return this.runner(async (context) => {
      const masters = context.presentation.slideMasters;
      masters.load('items/id,items/name,items/layouts/items/id,items/layouts/items/name');
      await context.sync();
      // 자리표시자는 레이아웃마다 한 번 더 물어야 안다. 왕복은 **하나에 몰아** 건다.
      // 레이아웃의 도형도 **자리표시자만 있는 것이 아니다** — 로고 그림 하나가 얹혀 있으면
      // `placeholderFormat` 을 건 묶음이 통째로 죽는다(`#placeholderRoles` 의 주석). 종류를
      // 먼저 보고, 역할은 그다음 왕복에서 자리표시자에만 묻는다.
      for (const m of masters.items) {
        for (const l of m.layouts.items) l.shapes.load('items/id,items/name,items/type');
      }
      await context.sync();
      const flat = masters.items.flatMap((m) => m.layouts.items.flatMap((l) => l.shapes?.items ?? []));
      const roles = await this.#placeholderRoles(context, flat);
      return this.#envelope({
        masters: masters.items.map((m) => ({
          master: m.name,
          master_id: m.id,
          layouts: m.layouts.items.map((l) => ({
            layout: l.name,
            layout_id: l.id,
            // 이 레이아웃이 무엇을 채울 자리를 갖고 있나. **빈 배열은 「없다」다** — 못 읽는
            // 경우는 여기 안 온다(`load` 가 실패하면 위 `sync` 가 통째로 던진다). 한동안
            // `null` 갈래를 적어 뒀는데 도달할 수 없는 줄이었고, 도달 못 하는 갈래는 있으나
            // 마나가 아니라 **읽는 사람에게 있는 척하는 구분**이다.
            placeholders: (l.shapes?.items ?? []).map((s) => roles.get(s.id)).filter(Boolean),
          })),
        })),
      });
    });
  }

  /**
   * 장을 새로 만든다. **이것이 없으면 「발표자료 만들어 줘」가 통째로 불가능하다.**
   *
   * 셋을 한 호출에 담는다 — 만들고, 자리를 옮기고, 자리표시자를 채운다. 셋으로 나누면 모델이
   * 왕복을 셋 쓰고, 그 사이에 실패하면 **제목 없는 빈 장**이 덱에 남는다. 사람이 보는 것은
   * 「장이 하나 늘었는데 비어 있다」이고, 그 상태는 아무도 원한 적이 없다.
   *
   * `slides.add` 는 **늘 맨 뒤에** 붙인다. 사람이 「2번 뒤에」라고 하면 붙인 뒤 옮긴다.
   * (이 사실은 2026-09-02 에 실물에서 재서 DESIGN.md 부록 A 에 적었다 — 그전에는 문서만 보고
   * 있었고, 이 파일이 부록 A 를 근거로 댔지만 거기 그 줄이 없었다.)
   */
  #addSlide(args) {
    return this.runner(async (context) => {
      const slides = context.presentation.slides;
      const options = {};
      let layoutName = null;
      if (args.layout) {
        const masters = context.presentation.slideMasters;
        masters.load('items/id,items/name,items/layouts/items/id,items/layouts/items/name');
        await context.sync();
        let found = null;
        for (const m of masters.items) {
          for (const l of m.layouts.items) {
            if (l.name === args.layout) { found = { layout: l, master: m }; break; }
          }
          if (found) break;
        }
        if (!found) {
          // **비슷한 것으로 갈음하지 않는다**(`apply_layout` 과 같은 규칙). 이름을 다 적어 주면
          // 모델이 다음 호출에서 맞힌다.
          const names = masters.items.flatMap((m) => m.layouts.items.map((l) => l.name));
          throw new Error(`${args.layout} 이라는 레이아웃이 없습니다 — 이 덱에는: ${names.join(', ')}`);
        }
        options.layoutId = found.layout.id;
        options.slideMasterId = found.master.id;
        layoutName = found.layout.name;
      }
      slides.load('items/id');
      await context.sync();
      const before = slides.items.map((s) => s.id);

      slides.add(options);
      await context.sync();

      slides.load('items/id,items/index');
      await context.sync();
      // **새로 생긴 장을 id 로 찾는다.** 「맨 뒤가 새것」이라고 세면, 같은 순간에 남이 장을
      // 하나 지운 판에서 엉뚱한 장을 고른다.
      const found = slides.items.find((s) => !before.includes(s.id));
      if (!found) throw new Error('장을 만들었는데 덱에서 그 장을 못 찾았습니다');
      const newId = found.id;
      // **여기서부터 덱은 이미 바뀌었다.** 아래에서 터져도 장은 남으므로, 개정 셈을 먼저
      // 올린다 — 나중에 올리면 「바뀐 덱」을 「안 바뀌었다」로 보고하는 창이 생긴다(§5.6).
      this.#mutated();

      const notes = [];
      if (args.at !== undefined) {
        // **덱 길이로 자른다.** 안 자르면 `add_slide{at:99}` 가 장을 만든 **뒤에** 던지고,
        // 사람은 오류와 함께 빈 장 하나를 얻는다.
        const to = Math.min(Math.max(1, Number(args.at)), slides.items.length);
        found.moveTo(to - 1);   // `moveTo` 는 0 부터다(CAPABILITIES.md §10.4)
        await context.sync();
        notes.push(`${to} 번 자리로`);
      }
      // **옮긴 뒤에는 id 로 다시 집는다.** 컬렉션에서 꺼낸 항목이 자리로 매인 것인지 id 로
      // 매인 것인지 우리가 못 정하는데, 자리로 매여 있으면 옮긴 다음의 모든 조작이 **그
      // 자리에 새로 온 남의 장**으로 간다 — 제목이 엉뚱한 장에 들어가고 성공으로 보고된다.
      const made = context.presentation.slides.getItem(newId);
      made.load('id,index');
      await context.sync();

      const { filled, unfilled } = await this.#fillPlaceholders(context, made, args);
      const at = (made.index ?? 0) + 1;
      // **못 넣은 글을 조용히 버리지 않는다.** 레이아웃에 그 자리가 없으면 글은 아무 데도 안
      // 들어가는데, 결과가 성공이면 사람은 「제목 있는 장」을 요청하고 빈 장을 받는다 — 이
      // 저장소가 제일 피하려는 실패다(§2.3). 무엇이 안 들어갔는지 이름을 대야 모델이 다른
      // 레이아웃으로 다시 걸 수 있다.
      const missed = unfilled.length
        ? ` · ⚠ ${unfilled.map((u) => `${u.role} 자리가 없어 "${clipText(u.text)}" 는 안 넣었습니다`).join(' · ')}`
        : '';
      return this.#envelope(
        {
          slide_id: newId, slide: at, layout: layoutName,
          filled: filled.map((f) => f.role),
          unfilled: unfilled.map((u) => u.role),
        },
        [`슬라이드 ${at}(id ${newId}) 를 만들었습니다` +
          (layoutName ? ` — 레이아웃 ${layoutName}` : '') +
          (notes.length ? ` · ${notes.join(' · ')}` : '') +
          (filled.length ? ` · ${filled.map((f) => `${f.role}="${clipText(f.text)}"`).join(' · ')}` : '') +
          missed]);
    });
  }

  /**
   * 새 장의 자리표시자를 채운다. **좌표로 텍스트 상자를 놓는 것과 결과가 다르다** — 자리표시자는
   * 테마를 따르고, 나중에 사람이 디자인을 바꾸면 같이 바뀐다(CAPABILITIES.md §4).
   *
   * 역할 이름은 호스트가 정한다(`title`·`centerTitle`·`body`·`subtitle`…). 그래서 **정확히
   * 일치**를 안 묻고 무리로 묻는다 — 레이아웃마다 제목의 역할 이름이 다르기 때문이다.
   */
  async #fillPlaceholders(context, slide, args) {
    const wants = [
      { role: 'title', text: args.title, match: (t) => /title/i.test(t) && !/sub/i.test(t) },
      { role: 'body', text: args.body, match: (t) => /body|content|subtitle|text/i.test(t) },
    ].filter((w) => typeof w.text === 'string' && w.text !== '');
    if (wants.length === 0) return { filled: [], unfilled: [] };

    // 역할은 **따로** 읽는다(`#placeholderRoles`) — 새 장이라도 레이아웃이 그림 하나를 얹어
    // 두면 그 도형에서 묶음이 죽고, 제목도 본문도 안 들어간 채 성공으로 보고된다.
    slide.shapes.load('items/id,items/name,items/type');
    await context.sync();
    const roles = await this.#placeholderRoles(context, slide.shapes.items);
    const taken = new Set();
    const filled = [];
    const unfilled = [];
    for (const w of wants) {
      const hit = slide.shapes.items.find((s) => {
        const t = String(roles.get(s.id) ?? '');
        return t !== '' && !taken.has(s.id) && w.match(t);
      });
      // **없는 자리를 지어내지 않고, 못 넣었다는 사실을 돌려준다.** 조용히 넘기면 부르는 쪽이
      // 성공으로 보고하고, 사람은 제목을 부탁한 자리에서 빈 장을 본다.
      if (!hit) { unfilled.push({ role: w.role, text: w.text }); continue; }
      taken.add(hit.id);
      hit.textFrame.textRange.text = w.text;
      filled.push({ role: w.role, text: w.text, shape_id: hit.id });
    }
    await context.sync();
    return { filled, unfilled };
  }

  /**
   * 장 하나를 지운다. **뒤 번호가 전부 밀린다**는 것을 결과가 말한다 — 그 말이 없으면 모델은
   * 지우기 전에 읽어 둔 번호로 다음 호출을 건다.
   */
  #deleteSlide(args) {
    return this.runner(async (context) => {
      // **지우는 것만은 「보고 있는 장」으로 안 떨어진다.** 다른 도구는 생략하면 앞에 있는
      // 장으로 가는 편이 편하지만(되돌릴 수 있으니까), 이건 스냅샷 없이는 못 되돌린다.
      // 사람이 어느 장인지 안 말했는데 골라 주면, 그 편의의 대가를 사람이 치른다.
      if (args.slide === undefined && !args.slide_id) {
        throw new Error('어느 장을 지울지 slide 나 slide_id 로 정확히 말해 주세요 — '
          + '지우기는 스냅샷 없이 못 되돌리므로 보고 있는 장으로 넘겨짚지 않습니다');
      }
      const slide = await this.#slide(context, args);
      slide.load('id,index');
      await context.sync();
      const at = (slide.index ?? 0) + 1;
      const id = slide.id;
      slide.delete();
      await context.sync();
      this.#mutated();
      return this.#envelope({ deleted: id, was: at },
        [`슬라이드 ${at}(id ${id}) 를 지웠습니다 — 스냅샷 없이는 못 되돌리고, ` +
          `${at} 번 뒤의 번호는 전부 하나씩 당겨졌습니다`]);
    });
  }

  /**
   * 장 하나를 그대로 하나 더. **서식을 원본 그대로 가져온다** — `formatting` 을 안 넘기는 것이
   * 계약이고(기본값이 KeepSourceFormatting), Learn 예제의 `useDestinationTheme` 을 베끼면
   * 복제본이 테마를 새로 입고 나온다. `restore_slide` 가 같은 이유로 같은 선택을 한다.
   */
  #duplicateSlide(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      slide.load('id,index');
      const packed = slide.exportAsBase64();
      await context.sync();
      const from = (slide.index ?? 0) + 1;

      const slides = context.presentation.slides;
      slides.load('items/id');
      await context.sync();
      const before = slides.items.map((s) => s.id);

      // `targetSlideId` 는 **그 장 바로 뒤**에 넣는다(부록 A).
      context.presentation.insertSlidesFromBase64(packed.value, { targetSlideId: slide.id });
      await context.sync();

      slides.load('items/id,items/index');
      await context.sync();
      const made = slides.items.find((s) => !before.includes(s.id));
      // **못 찾았으면 던진다.** 자리를 짐작해 「2 번에 넣었습니다」라고 답하면, 사람은 없는
      // 장을 있다고 듣는다 — `add_slide` 는 같은 자리에서 이미 던지고, 던지는 쪽이 맞다.
      if (!made) {
        throw new Error(`슬라이드 ${from} 을 복제했는데 덱에서 복제본을 못 찾았습니다 — `
          + '넣기가 안 먹었을 수 있으니 목차를 다시 읽어 확인하세요');
      }
      this.#mutated();
      const at = (made.index ?? 0) + 1;
      return this.#envelope({ slide_id: made.id, slide: at, from: slide.id },
        [`슬라이드 ${from} 을 복제해 ${at} 번에 넣었습니다 — ` +
          `복제본의 id 는 ${made.id} 이고 원본과 다릅니다`]);
    });
  }

  /**
   * 레이아웃을 갈아 끼운다.
   *
   * ⚠ **어느 모양으로 부르는지는 호스트가 정한다.** id 문자열을 넘겼더니 실물이
   * `InvalidArgument` 로 떨어졌다(2026-09-02 전수 점검). 문서로는 둘 다 그럴듯해서 **두 번
   * 시도한다** — 한 묶음이 실패하면 그 context 는 못 쓰므로, 두 번째는 새 묶음이다. 둘 다
   * 실패하면 **둘의 사유를 다 싣는다**: 「안 됐다」만으로는 다음에 뭘 고칠지 모른다.
   */
  async #applyLayout(args) {
    const tries = [];
    for (const how of ['object', 'id']) {
      try {
        return await this.#applyLayoutOnce(args, how);
      } catch (e) {
        tries.push(`${how}: ${e?.message ?? e}`);
        // 이름이 아예 없는 경우는 **다시 시도할 값이 없다** — 바로 올린다.
        if (String(e?.message ?? '').includes('레이아웃이 없습니다')) throw e;
      }
    }
    throw new Error(`레이아웃을 못 갈아 끼웠습니다 — ${tries.join(' / ')}`);
  }

  #applyLayoutOnce(args, how) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const masters = context.presentation.slideMasters;
      masters.load('items/id,items/name,items/layouts/items/id,items/layouts/items/name');
      slide.load('layout/name');
      await context.sync();
      const before = slide.layout?.name ?? null;
      let found = null;
      for (const m of masters.items) {
        for (const l of m.layouts.items) {
          if (l.name === args.layout) { found = l; break; }
        }
        if (found) break;
      }
      if (!found) {
        // **비슷한 것으로 갈음하지 않는다.** 이름을 다 적어 주면 모델이 다음 호출에서 맞힌다.
        const names = masters.items.flatMap((m) => m.layouts.items.map((l) => l.name));
        throw new Error(`${args.layout} 이라는 레이아웃이 없습니다 — 이 덱에는: ${names.join(', ')}`);
      }
      slide.applyLayout(how === 'id' ? found.id : found);
      await context.sync();
      this.#mutated();
      return this.#envelope({ slide_id: slide.id, layout: args.layout },
        [`슬라이드 ${slide.id}: 레이아웃 ${before ?? '?'} → ${args.layout}`]);
    });
  }

  #reorder(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const to = Number(args.to);
      const before = (slide.index ?? 0) + 1;
      // `moveTo` 는 1.8 이고 0 부터 센다(부록 A). 사람이 말하는 번호에서 1 을 뺀다.
      slide.moveTo(to - 1);
      await context.sync();
      this.#mutated();
      return this.#envelope({ slide_id: slide.id, from: before, to },
        [`슬라이드 ${slide.id}: ${before} 번 → ${to} 번 — 이 뒤의 번호는 전부 달라졌습니다`]);
    });
  }

  #hyperlink(args) {
    // **바닥과 천장 사이에 있는 것은 먼저 묻는다**(부록 A: LTSC 2024 에는 선택이 있고 하이퍼링크가
    // 없다). 없는 호스트에서 조용히 성공하면 그게 §2.3 의 최악이다.
    if (!this.supports('PowerPointApi', '1.6')) {
      return Promise.reject(new Error(
        '이 PowerPoint 는 하이퍼링크 API(1.6)가 없습니다 — 링크는 사람이 직접 걸어야 합니다'));
    }
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const shape = slide.shapes.getItem(args.shape_id);
      const url = String(args.url ?? '');
      shape.setHyperlink(url === '' ? null : { address: url });
      await context.sync();
      this.#mutated();
      return this.#envelope({ slide_id: slide.id, shape_id: args.shape_id, url },
        [`슬라이드 ${slide.id} · 도형 ${args.shape_id}: 링크 ${url === '' ? '제거' : `→ ${url}`}`]);
    });
  }

  /**
   * 표 만들기 옵션을 짓는다. `add_table` 과 `replace_table` 이 **같은 규칙**을 써야 한다 —
   * 갈라 두면 한쪽만 고쳐지고, 그 차이는 「고쳐 달랬더니 다르게 생긴 표가 나왔다」로 나온다.
   */
  #tableOptions(args, rows, columns, rect) {
    const options = {
      left: Number(args.left ?? rect?.left ?? 100),
      top: Number(args.top ?? rect?.top ?? 100),
      width: Number(args.width ?? rect?.width ?? 400),
      height: Number(args.height ?? rect?.height ?? 200),
    };
    if (args.values !== undefined) options.values = args.values;
    const uniform = {};
    if (args.font !== undefined) uniform.font = { ...(uniform.font ?? {}), name: args.font };
    if (args.size !== undefined) uniform.font = { ...(uniform.font ?? {}), size: Number(args.size) };
    // ⚠⚠ **칸 서식을 주는 순간 테마의 표 스타일이 사라진다.** 2026-09-02 에 실물에서 잰
    // 것이다: `uniformCellProperties` 를 아예 안 주면 PowerPoint 가 테마의 표 스타일을 입혀
    // 머리띠가 있는 **보기 좋은 표**가 서고, 글꼴 하나라도 주면 채움도 테두리도 없는 맨몸이
    // 선다(COM 으로 보면 네 변이 전부 `Visible=0`). 값이 안 든 맨몸 표는 화면에서
    // **아무것도 아니다** — 사람은 「표 만들어 줘」라고 하고 빈 슬라이드를 본다.
    //
    // 규칙 둘: 아무 서식도 안 청했으면 **아무것도 안 준다**(테마가 그린다). 서식을 청해서
    // 테마가 날아가면 **선을 우리가 그린다**(안 그리면 안 보인다).
    const wantsFormat = Object.keys(uniform).length > 0;
    const line = args.borders === undefined ? null : String(args.borders);
    const drawLines = line !== null ? line.toLowerCase() !== 'none' : wantsFormat;
    if (drawLines) {
      const color = line && line.toLowerCase() !== 'none' ? line : '#808080';
      const edge = { color, weight: 1, dashStyle: 'solid' };
      uniform.borders = { top: edge, bottom: edge, left: edge, right: edge };
    }
    if (Object.keys(uniform).length > 0) options.uniformCellProperties = uniform;
    if (args.header_bold) {
      options.specificCellProperties = Array.from({ length: rows }, (_, r) => (
        Array.from({ length: columns }, () => (r === 0 ? { font: { bold: true } } : {}))));
    }
    return options;
  }

  /**
   * 이 장에 이미 있는 표들. **경고 한 줄을 짓기 위해서** 센다 — 사람이 「표 고쳐 줘」라고 한
   * 것을 모델이 `add_table` 로 받으면 표가 둘이 되고, 사람 눈에는 「안 고쳐졌다」로 보인다.
   * 실제로 그렇게 신고가 들어왔다(2026-09-02).
   */
  async #tablesOn(context, slide) {
    slide.shapes.load('items/id,items/name,items/type,items/left,items/top,items/width,items/height');
    await context.sync();
    return slide.shapes.items.filter((s) => String(s.type ?? '').toLowerCase() === 'table');
  }

  #addTable(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const rows = Number(args.rows);
      const columns = Number(args.columns);
      // **이 장에 이미 표가 있으면 그 사실을 결과가 말한다.** 사람이 「표 고쳐 줘」라고 한 것을
      // 모델이 이 도구로 받으면 표가 둘이 되고, 사람 눈에는 「안 고쳐졌다」로 보인다 — 실제로
      // 그렇게 신고가 들어왔다(2026-09-02). 막지는 않는다(표를 둘 두는 장도 있다). 대신
      // **다음 수를 이름 대어 알려 준다.**
      const already = await this.#tablesOn(context, slide);
      const options = this.#tableOptions(args, rows, columns, null);
      const shape = slide.shapes.addTable(rows, columns, options);
      shape.load('id');
      await context.sync();
      this.#mutated();
      const warn = already.length
        ? ` · ⚠ 이 장에는 이미 표가 ${already.length}개 있습니다(${already.map((t) => t.id).join(', ')}) — `
          + '고치려던 것이면 그 표를 replace_table 로 바꾸거나 set_table_cells 로 글만 채우세요'
        : '';
      return this.#envelope(
        { slide_id: slide.id, shape_id: shape.id, rows, columns, tables_before: already.length },
        [`슬라이드 ${slide.id}: ${rows}×${columns} 표 ${shape.id} 추가`
          + `${args.header_bold ? ' (헤더 굵게)' : ''}` + warn]);
    });
  }

  /**
   * 있는 표를 **제자리에서** 다시 짓는다. 지우고 같은 자리에 새로 만든다.
   *
   * 이 도구가 없어서 생긴 일이 이것이다: 사람이 표를 만들게 하고 그것을 고쳐 달라고 했는데,
   * 모델에게는 고칠 길이 없어서(있는 표의 서식·행열은 1.9 라 이 바닥에 없다) **표를 하나 더
   * 만들었다.** 사람은 「기존 거 놔두고 새로 넣어 버렸다」고 신고했다(2026-09-02). 못 하는 것을
   * 광고하지 않는 것과, 할 수 있는 길을 **하나는 주는 것**은 다른 일이다.
   *
   * 값을 안 주면 **옛 표의 글을 옮겨 온다** — 「열 하나 더」에서 사람이 기대하는 것이 그것이다.
   */
  #replaceTable(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const tables = await this.#tablesOn(context, slide);
      if (tables.length === 0) {
        throw new Error(`슬라이드 ${slide.id} 에는 표가 없습니다 — 새로 만들려면 add_table 을 쓰세요`);
      }
      let old = tables[0];
      if (args.shape_id) {
        old = tables.find((t) => t.id === args.shape_id) ?? null;
        if (!old) {
          throw new Error(`도형 ${args.shape_id} 는 이 장의 표가 아닙니다 — 이 장의 표: `
            + tables.map((t) => t.id).join(', '));
        }
      } else if (tables.length > 1) {
        // **여럿이면 안 고른다.** 골라 주면 엉뚱한 표가 사라지고, 그건 못 되돌린다.
        throw new Error(`이 장에는 표가 ${tables.length}개 있습니다 — 어느 것인지 shape_id 로 말해 주세요: `
          + tables.map((t) => t.id).join(', '));
      }

      // 옛 표의 자리·크기·글을 읽어 둔다. **글까지 읽는 것이 이 도구의 값이다** — 「열 하나 더」에
      // 사람이 기대하는 것은 빈 표가 아니라 쓰던 표다.
      const rect = { left: old.left, top: old.top, width: old.width, height: old.height };
      const kept = await this.#readTableText(context, old);
      const rows = Number(args.rows ?? kept.rows ?? 1);
      const columns = Number(args.columns ?? kept.columns ?? 1);
      const values = args.values !== undefined ? args.values : regrid(kept.values, rows, columns);

      const options = this.#tableOptions({ ...args, values }, rows, columns, rect);
      const oldId = old.id;
      old.delete();
      const made = slide.shapes.addTable(rows, columns, options);
      made.load('id');
      await context.sync();
      this.#mutated();
      return this.#envelope(
        {
          slide_id: slide.id, shape_id: made.id, replaced: oldId, rows, columns,
          was: { rows: kept.rows, columns: kept.columns },
        },
        [`슬라이드 ${slide.id}: 표 ${oldId}(${kept.rows}×${kept.columns}) 를 지우고 `
          + `같은 자리에 ${rows}×${columns} 표 ${made.id} 를 놓았습니다 — 옛 id 는 이제 없습니다`]);
    });
  }

  /**
   * 표의 글을 격자로 읽는다. **못 읽으면 빈 격자를 주고 그렇게 적는다** — 지어낸 글로 표를
   * 다시 지으면 사람이 쓰던 것이 조용히 바뀐다.
   */
  async #readTableText(context, shape) {
    let table;
    try {
      table = shape.getTable();
      table.load('rowCount,columnCount');
      await context.sync();
    } catch {
      return { rows: null, columns: null, values: [] };
    }
    const rows = Number(table.rowCount ?? 0);
    const columns = Number(table.columnCount ?? 0);
    if (!rows || !columns) return { rows: rows || null, columns: columns || null, values: [] };
    const cells = [];
    for (let r = 0; r < rows; r++) {
      const line = [];
      for (let c = 0; c < columns; c++) {
        const cell = table.getCellOrNullObject(r, c);
        cell.load('isNullObject,text');
        line.push(cell);
      }
      cells.push(line);
    }
    try {
      await context.sync();
    } catch {
      return { rows, columns, values: [] };
    }
    return {
      rows,
      columns,
      values: cells.map((line) => line.map((cell) => (cell.isNullObject ? '' : (cell.text ?? '')))),
    };
  }

  #setCells(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const shape = slide.shapes.getItem(args.shape_id);
      const table = shape.getTable();
      const cells = Array.isArray(args.cells) ? args.cells : [];
      if (cells.length === 0) throw new Error('쓸 셀이 하나도 안 왔습니다');
      const handles = cells.map((c) => {
        const cell = table.getCellOrNullObject(Number(c.row), Number(c.column));
        cell.load('text,isNullObject');
        return { want: c, cell };
      });
      await context.sync();
      const changed = [];
      for (const { want, cell } of handles) {
        if (cell.isNullObject) {
          // **없는 셀은 지어내지 않는다.** 절반만 쓰고 성공으로 답하면 모델이 나머지를 쓴 줄 안다.
          throw new Error(`표에 (${want.row}, ${want.column}) 셀이 없습니다 — 아무것도 안 썼습니다`);
        }
        changed.push(`(${want.row}, ${want.column}): "${cell.text ?? ''}" → "${want.text ?? ''}"`);
        cell.text = String(want.text ?? '');
      }
      await context.sync();
      this.#mutated();
      return this.#envelope({ slide_id: slide.id, shape_id: args.shape_id, cells: cells.length },
        [`슬라이드 ${slide.id} · 표 ${args.shape_id}: ${changed.join(' / ')}`]);
    });
  }

  #snapshot(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const packed = slide.exportAsBase64();
      await context.sync();
      const id = `snap-${this.epoch}-${this.nextSnap++}`;
      this.snapshots.set(id, { slideId: slide.id, base64: packed.value });
      // 스냅샷은 **덱을 읽기만 한다** — 그래서 허용 규칙에 들어가고 사람의 클릭을 안 기다린다(§6).
      return this.#envelope({ snapshot: id, slide_id: slide.id, bytes: packed.value.length });
    });
  }

  #restore(args) {
    return this.runner(async (context) => {
      const kept = this.snapshots.get(args.snapshot);
      if (!kept) throw new Error(`스냅샷 ${args.snapshot} 이 없습니다 — 이 헬퍼가 뜬 뒤에 뜬 것만 있습니다`);
      const slides = context.presentation.slides;
      slides.load('items/id,items/index');
      await context.sync();
      const at = slides.items.find((s) => s.id === kept.slideId);
      if (!at) {
        // 낡은 id 로 넣으면 문서가 `SlideNotFound` 를 던지고 **아무것도 안 넣는다**(부록 A).
        // 그 전에 우리가 먼저 말한다.
        throw new Error(`되돌릴 자리(슬라이드 ${kept.slideId})가 지금 덱에 없습니다 — 그 사이 지워졌습니다`);
      }
      // **제자리 교체가 없다.** 넣고 원본을 지우는 두 걸음이고, 살아남는 장은 **새 id** 를 단다.
      // `formatting` 을 안 넘기는 것이 계약이다 — 기본값이 KeepSourceFormatting 이고, Learn 의
      // 예제가 넘기는 `useDestinationTheme` 을 베끼면 되돌린 장이 테마를 새로 입고 돌아온다.
      context.presentation.insertSlidesFromBase64(kept.base64, { targetSlideId: kept.slideId });
      await context.sync();

      slides.load('items/id,items/index');
      await context.sync();
      const after = slides.items.find((s) => s.id !== kept.slideId
        && (s.index ?? 0) === (at.index ?? 0) + 1);
      at.delete();
      await context.sync();
      this.#mutated();
      const newId = after?.id ?? null;
      return this.#envelope({ slide_id: newId, replaced: kept.slideId },
        [`슬라이드 ${kept.slideId} 를 스냅샷 ${args.snapshot} 으로 되돌렸습니다 — ` +
          `새 id 는 ${newId ?? '못 읽었습니다'} 이고, 옛 id 를 가리키던 것은 전부 낡았습니다`]);
    });
  }
}

/** 도형 종류 이름을 Office 의 열거로. 모르는 것은 **던진다** — 지어내면 엉뚱한 도형이 선다. */
/**
 * 글틀이 없는 도형 종류. 여기에 글을 물으면 **묶음 전체가 죽는다**(2026-09-02 실물).
 * 좁게 잡는다 — 넓게 잡으면 글이 있는 도형을 조용히 건너뛰고, 그건 「없다」로 보고된다.
 */
const TEXTLESS = new Set(['table', 'image', 'picture', 'chart', 'group', 'media', 'ole',
  'smartart', 'model3d', 'ink', 'diagram', 'contentapp']);

/**
 * 옛 표의 글을 새 크기의 격자에 맞춘다. **넘치는 것은 버리고 모자라는 것은 빈 칸이다** —
 * 「열 하나 더」에 사람이 기대하는 것이 그것이고, 줄이는 쪽에서 글이 사라지는 것은 사실이라
 * 결과가 크기 둘을 다 적는다.
 */
function regrid(values, rows, columns) {
  if (!Array.isArray(values) || values.length === 0) return undefined;
  return Array.from({ length: rows }, (_, r) =>
    Array.from({ length: columns }, (_, c) => String(values[r]?.[c] ?? '')));
}

/**
 * 결과 문장에 남의 글을 실을 때 길이를 자른다. **자른 표시까지가 길이다** — 자르고도 길면
 * 자른 뜻이 없다. 덱의 본문이 그대로 흐르는 자리라, 한 문장이 결과 줄 전체를 밀어내면
 * 「무엇이 됐는지」가 화면 밖으로 나간다.
 */
function clipText(s, n = 60) {
  const t = String(s ?? '');
  return t.length > n ? `${t.slice(0, n - 1)}…` : t;
}

/**
 * 도형 이름을 Office 의 열거로. **모르는 것은 던진다** — 지어내면 엉뚱한 도형이 선다.
 *
 * 넷만 알던 자리다(사각형·둥근사각형·타원·선). API 한계가 아니라 처음에 좁게 잡은 것이었고,
 * 사람이 「화살표 그려 줘」라고 하면 손이 모른다고 답했다. `addGeometricShape` 는 백여 가지를
 * 받으므로, **사람이 말로 부르는 것**을 위주로 넓힌다 — 한국어 이름도 같이 받는다. 모델이
 * 한국어 대화 중에 한국어 이름을 그대로 넘기는 것이 자연스럽고, 그때 거절하면 왕복이 는다.
 */
function geometryOf(kind) {
  const raw = String(kind ?? '').trim();
  const key = raw.toLowerCase().replace(/[\s_-]/g, '');
  // 이름표에 있는 별명이거나, **Office 의 표준명 그대로**거나. 뒤엣것을 안 받으면 스키마가
  // 광고한 이름을 손이 거절하는 자리가 생긴다 — `flowChartInputOutput` 이 실제로 그랬다
  // (별명은 `flowchartdata` 였다). 광고와 실행이 어긋나면 모델은 광고된 이름을 부르고 튕긴다.
  const got = GEOMETRY.get(key) ?? CANON.get(key);
  if (!got) {
    // **있는 이름을 알려 준다** — 「모른다」만으로는 다음에 무엇을 부를지 모른다.
    throw new Error(`${raw} 는 이 손이 아는 도형이 아닙니다 — 아는 것: ` + geometryNames().join(', '));
  }
  return got;
}

/** 표준명 자체로 찾는 길. 소문자로 눌러 둔다. */
const CANON = new Map();

/** 사람에게 보여 줄 이름들(영문 표준명만, 중복 없이). */
function geometryNames() {
  return [...new Set(GEOMETRY.values())].sort();
}

/**
 * 이름표. 왼쪽이 사람·모델이 부르는 말, 오른쪽이 Office 의 `GeometricShapeType`.
 * 한 도형에 여러 이름이 붙는다 — 「별」과 `star5` 와 `star` 는 같은 것을 가리킨다.
 */
const GEOMETRY = new Map(Object.entries({
  // 기본
  rectangle: 'rectangle', 사각형: 'rectangle', 네모: 'rectangle',
  roundrectangle: 'roundRectangle', 둥근사각형: 'roundRectangle', 라운드사각형: 'roundRectangle',
  ellipse: 'ellipse', oval: 'ellipse', circle: 'ellipse', 원: 'ellipse', 타원: 'ellipse',
  line: 'line', 선: 'line', 직선: 'line',
  triangle: 'triangle', 삼각형: 'triangle',
  righttriangle: 'rightTriangle', 직각삼각형: 'rightTriangle',
  diamond: 'diamond', 마름모: 'diamond',
  parallelogram: 'parallelogram', 평행사변형: 'parallelogram',
  trapezoid: 'trapezoid', 사다리꼴: 'trapezoid',
  pentagon: 'pentagon', 오각형: 'pentagon',
  hexagon: 'hexagon', 육각형: 'hexagon',
  heptagon: 'heptagon', 칠각형: 'heptagon',
  octagon: 'octagon', 팔각형: 'octagon',
  // 눈에 띄는 것들
  star4: 'star4', star5: 'star5', star: 'star5', 별: 'star5', 별5: 'star5',
  star6: 'star6', star8: 'star8', star10: 'star10', star12: 'star12',
  heart: 'heart', 하트: 'heart',
  sun: 'sun', 해: 'sun',
  moon: 'moon', 달: 'moon',
  cloud: 'cloud', 구름: 'cloud',
  smileyface: 'smileyFace', 스마일: 'smileyFace',
  lightningbolt: 'lightningBolt', 번개: 'lightningBolt',
  // 화살표 — 흐름을 그리는 데 제일 자주 쓴다
  rightarrow: 'rightArrow', 오른쪽화살표: 'rightArrow', 화살표: 'rightArrow', arrow: 'rightArrow',
  leftarrow: 'leftArrow', 왼쪽화살표: 'leftArrow',
  uparrow: 'upArrow', 위화살표: 'upArrow',
  downarrow: 'downArrow', 아래화살표: 'downArrow',
  leftrightarrow: 'leftRightArrow', 양쪽화살표: 'leftRightArrow',
  updownarrow: 'upDownArrow',
  bentarrow: 'bentArrow', 꺾인화살표: 'bentArrow',
  curvedrightarrow: 'curvedRightArrow', 곡선화살표: 'curvedRightArrow',
  chevron: 'chevron', 갈매기: 'chevron',
  homeplate: 'homePlate', 오각화살표: 'homePlate',
  // 말풍선
  wedgerectcallout: 'wedgeRectCallout', 말풍선: 'wedgeRectCallout',
  wedgeroundrectcallout: 'wedgeRoundRectCallout', 둥근말풍선: 'wedgeRoundRectCallout',
  wedgeellipsecallout: 'wedgeEllipseCallout', 타원말풍선: 'wedgeEllipseCallout',
  cloudcallout: 'cloudCallout', 구름말풍선: 'cloudCallout',
  // 순서도에 쓰는 것들
  flowchartprocess: 'flowChartProcess', 처리: 'flowChartProcess',
  flowchartdecision: 'flowChartDecision', 판단: 'flowChartDecision',
  flowchartterminator: 'flowChartTerminator', 시작끝: 'flowChartTerminator',
  flowchartdocument: 'flowChartDocument', 문서: 'flowChartDocument',
  flowchartdata: 'flowChartInputOutput', 입출력: 'flowChartInputOutput',
  // 기타 자주 쓰는 것
  can: 'can', 원기둥: 'can', cube: 'cube', 정육면체: 'cube',
  donut: 'donut', 도넛: 'donut',
  plaque: 'plaque', bevel: 'bevel',
  frame: 'frame', 액자: 'frame',
  plus: 'mathPlus', 더하기: 'mathPlus',
  minus: 'mathMinus', 빼기: 'mathMinus',
  multiply: 'mathMultiply', 곱하기: 'mathMultiply',
  equal: 'mathEqual', 등호: 'mathEqual',
  noSmoking: 'noSmoking', 금지: 'noSmoking',
  blockarc: 'blockArc', arc: 'arc', 호: 'arc',
  chord: 'chord', pie: 'pie', 부채꼴: 'pie',
  teardrop: 'teardrop', 물방울: 'teardrop',
}));

for (const v of GEOMETRY.values()) CANON.set(v.toLowerCase(), v);



/**
 * 조각 이름을 고른다. 슬라이드 하나짜리 `.pptx` 에서 `slide` 는 `ppt/slides/slide1.xml` 이고
 * 노트는 `ppt/notesSlides/notesSlide1.xml` 이다. **못 찾으면 무엇이 들어 있는지 말한다.**
 */
export function pickPart(entries, part) {
  const names = entries.map((e) => e.name);
  const first = (re) => names.find((n) => re.test(n));
  const found = {
    slide: first(/^ppt\/slides\/slide\d+\.xml$/),
    notes: first(/^ppt\/notesSlides\/notesSlide\d+\.xml$/),
    chart: first(/^ppt\/charts\/chart\d+\.xml$/),
  }[part] ?? (names.includes(part) ? part : null);
  if (!found) {
    throw new Error(`${part} 조각이 이 슬라이드에 없습니다 — 들어 있는 것: ${names.join(', ')}`);
  }
  return found;
}
