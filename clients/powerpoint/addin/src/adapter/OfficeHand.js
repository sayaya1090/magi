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
      'list_layouts', 'add_slide', 'delete_slide', 'duplicate_slide'];
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
      shapes.load('items/id,items/name,items/type,items/left,items/top,items/width,items/height,' +
        'items/placeholderFormat/type,items/altTextDescription');
      slide.load('id,index,layout/name');
      await context.sync();

      // 글은 **두 번째 왕복**에서. 도형에 `textFrame` 이 없을 수 있어 통째로 실패할 수 있고,
      // 그때는 글을 포기하고 **신원은 살린다** — 다만 포기했다는 사실을 실어 보낸다.
      const frames = shapes.items.map((s) => {
        const tf = s.textFrame;
        tf.textRange.load('text');
        return tf;
      });
      let texts;
      let textUnavailable = false;
      try {
        await context.sync();
        texts = frames.map((tf) => tf.textRange.text ?? '');
      } catch {
        texts = shapes.items.map(() => '');
        textUnavailable = true;
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
          placeholder: s.placeholderFormat?.type ?? null,
          alt: s.altTextDescription ?? null,
          left: s.left, top: s.top, width: s.width, height: s.height,
          text: texts[i],
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
      const kept = [];
      for (const h of hits) {
        const shape = context.presentation.slides.getItem(h.slide_id).shapes.getItem(h.shape_id);
        shape.textFrame.textRange.load('text');
        kept.push({ hit: h, shape });
      }
      await context.sync();
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
      const holders = [];
      for (const m of masters.items) {
        for (const l of m.layouts.items) {
          l.shapes.load('items/name,items/placeholderFormat/type');
          holders.push({ master: m, layout: l });
        }
      }
      await context.sync();
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
            placeholders: (l.shapes?.items ?? []).map((s) => s.placeholderFormat?.type).filter(Boolean),
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

    slide.shapes.load('items/id,items/name,items/placeholderFormat/type');
    await context.sync();
    const taken = new Set();
    const filled = [];
    const unfilled = [];
    for (const w of wants) {
      const hit = slide.shapes.items.find((s) => {
        const t = String(s.placeholderFormat?.type ?? '');
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

  #applyLayout(args) {
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
      slide.applyLayout(found.id);
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

  #addTable(args) {
    return this.runner(async (context) => {
      const slide = await this.#slide(context, args);
      const rows = Number(args.rows);
      const columns = Number(args.columns);
      const options = {
        left: Number(args.left ?? 100), top: Number(args.top ?? 100),
        width: Number(args.width ?? 400), height: Number(args.height ?? 200),
      };
      if (args.values !== undefined) options.values = args.values;
      // **서식은 만들 때 준다** — 만든 뒤에 고치는 것은 1.9 라 이 바닥에 없다(§2.3).
      const uniform = {};
      if (args.font !== undefined) uniform.font = { ...(uniform.font ?? {}), name: args.font };
      if (args.size !== undefined) uniform.font = { ...(uniform.font ?? {}), size: Number(args.size) };
      if (Object.keys(uniform).length > 0) options.uniformCellProperties = uniform;
      if (args.header_bold) {
        options.specificCellProperties = Array.from({ length: rows }, (_, r) => (
          Array.from({ length: columns }, () => (r === 0 ? { font: { bold: true } } : {}))));
      }
      const shape = slide.shapes.addTable(rows, columns, options);
      shape.load('id');
      await context.sync();
      this.#mutated();
      return this.#envelope({ slide_id: slide.id, shape_id: shape.id, rows, columns },
        [`슬라이드 ${slide.id}: ${rows}×${columns} 표 ${shape.id} 추가` +
          `${args.header_bold ? ' (헤더 굵게)' : ''}`]);
    });
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
 * 결과 문장에 남의 글을 실을 때 길이를 자른다. **자른 표시까지가 길이다** — 자르고도 길면
 * 자른 뜻이 없다. 덱의 본문이 그대로 흐르는 자리라, 한 문장이 결과 줄 전체를 밀어내면
 * 「무엇이 됐는지」가 화면 밖으로 나간다.
 */
function clipText(s, n = 60) {
  const t = String(s ?? '');
  return t.length > n ? `${t.slice(0, n - 1)}…` : t;
}

function geometryOf(kind) {
  const map = {
    rectangle: 'rectangle', ellipse: 'ellipse', line: 'line',
    roundRectangle: 'roundRectangle', roundrectangle: 'roundRectangle',
  };
  const got = map[kind];
  if (!got) throw new Error(`${kind} 는 이 손이 아는 도형이 아닙니다 — textbox·rectangle·ellipse·line·roundRectangle`);
  return got;
}

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
