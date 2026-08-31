import { HandPort } from '../port/HandPort.js';

/**
 * PowerPoint 없이 도는 손. 픽스처를 실제로 고친다.
 *
 * **이게 오늘 도구를 검증할 수 있는 유일한 길이다** — 이 머신에 PowerPoint 가 없다. 그래서
 * 여기서 재는 것은 「Office.js 가 그렇게 답하는가」가 **아니라** 「도구의 계약을 우리가
 * 지키는가」다: 위치가 1부터인가, 없는 것을 비슷한 것으로 갈음하지 않는가, 쓰기가 바뀐 값을
 * 스스로 싣는가, 못 하는 것을 조용히 성공으로 답하지 않는가.
 *
 * 그 넷은 호스트와 **무관하게** 참이어야 하는 것들이라 가짜 위에서 재는 것이 옳다. 호스트가
 * 실제로 어떻게 답하는지는 `OfficeHand` 가 붙는 날 재는 것이고, 그 전까지는 안 잰 것이다.
 */
export class FakeHand extends HandPort {
  constructor(model, { document = 'doc-fake', label = 'fake.pptx' } = {}) {
    super();
    this.model = model;
    this.document = document;
    this.label = label;
    /** 개정 쌍(§5.6). epoch 는 이 손의 신원, count 는 실제로 바뀐 횟수다. */
    this.epoch = 1;
    this.count = 0;
    this.snapshots = new Map();
    this.nextId = 1;
  }

  get label() { return this.labelText; }
  set label(v) { this.labelText = v; }

  ops() {
    return ['list_slides', 'read_slide', 'find_shapes', 'render_slide', 'export_slide_ooxml',
      'set_text', 'format_shape', 'move_shape', 'add_shape', 'delete_shape', 'apply_layout',
      'reorder_slide', 'set_hyperlink', 'add_table', 'set_table_cells',
      'snapshot_slide', 'restore_slide', 'advise', 'clear_advice'];
  }

  /** 위치(1부터) 또는 id 로 슬라이드를 집는다. **못 찾으면 던진다.** */
  #slide(args) {
    if (args.slide_id) {
      const byId = this.model.slides.find((s) => s.id === args.slide_id);
      if (!byId) throw new Error(`슬라이드 ${args.slide_id} 는 이 덱에 없습니다`);
      return byId;
    }
    if (args.slide !== undefined) {
      const at = this.model.slides[Number(args.slide) - 1];
      if (!at) {
        throw new Error(
          `슬라이드 ${args.slide} 이 없습니다 — 이 덱은 ${this.model.slides.length} 장입니다`);
      }
      return at;
    }
    // 생략하면 첫 장이다. **지어내지 않고 그렇게 적는다** — 결과가 어느 슬라이드였는지를 싣는다.
    return this.model.slides[0];
  }

  #shape(slide, id) {
    const shape = slide.shapes.find((s) => s.id === id);
    if (!shape) {
      // **비슷한 것을 찾아 대신 고치지 않는다**(§5.8·§6.1). 틀린 채로 그럴듯한 것이 제일 나쁘다.
      throw new Error(`도형 ${id} 는 슬라이드 ${slide.id} 에 없습니다`);
    }
    return shape;
  }

  #mutated() { this.count += 1; }

  #envelope(result, changed = []) {
    return {
      document: this.document, label: this.labelText, result, changed,
      epoch: this.epoch, count: this.count,
    };
  }

  async run(op, args = {}) {
    switch (op) {
      case 'list_slides': {
        const from = Math.max(1, Number(args.from ?? 1));
        const count = args.count === undefined ? this.model.slides.length : Number(args.count);
        const slides = this.model.slides.slice(from - 1, from - 1 + count).map((s, i) => ({
          slide: from + i, slide_id: s.id, layout: s.layout ?? '제목 및 내용',
          shapes: s.shapes.length,
        }));
        return this.#envelope({ slides, total: this.model.slides.length });
      }
      case 'read_slide': {
        const slide = this.#slide(args);
        return this.#envelope({
          slide: this.model.slides.indexOf(slide) + 1,
          slide_id: slide.id,
          layout: slide.layout ?? '제목 및 내용',
          shapes: slide.shapes.map((s) => ({
            shape_id: s.id, name: s.name, type: s.type, text: s.text,
            left: s.width == null ? null : 0, top: null,
            width: s.width, height: s.height,
          })),
          // **못 읽는 것을 없는 것으로 적지 않는다**(CAPABILITIES.md §10.5).
          unreadable: ['notes', 'animation', 'chart-data'],
        });
      }
      case 'find_shapes': {
        const want = String(args.text ?? '').toLowerCase();
        const hits = [];
        this.model.slides.forEach((s, i) => {
          for (const sh of s.shapes) {
            if (args.type && sh.type !== args.type) continue;
            if (want && !String(sh.text ?? '').toLowerCase().includes(want)) continue;
            hits.push({ slide: i + 1, slide_id: s.id, shape_id: sh.id, name: sh.name, type: sh.type, text: sh.text });
          }
        });
        return this.#envelope({ shapes: hits.slice(0, Number(args.limit ?? 50)) });
      }
      case 'render_slide': {
        const slide = this.#slide(args);
        // 가짜는 픽셀을 지어내지 않는다. **못 한다고 말한다** — 없는 증거를 있는 척하는 것이
        // 이 제품이 제일 피하려는 것이다(§7).
        throw new Error(
          `이 손은 가짜라 슬라이드 ${slide.id} 를 렌더할 수 없습니다 — PowerPoint 에 붙어야 나옵니다`);
      }
      case 'export_slide_ooxml': {
        const slide = this.#slide(args);
        throw new Error(`이 손은 가짜라 슬라이드 ${slide.id} 의 OOXML 을 못 냅니다`);
      }
      case 'set_text': {
        const slide = this.#slide(args);
        const shape = this.#shape(slide, args.shape_id);
        const before = shape.text;
        shape.text = String(args.text ?? '');
        this.#mutated();
        return this.#envelope(
          { slide_id: slide.id, shape_id: shape.id, text: shape.text },
          [`슬라이드 ${slide.id} · 도형 ${shape.id}: "${before}" → "${shape.text}"`]);
      }
      case 'move_shape': {
        const slide = this.#slide(args);
        const shape = this.#shape(slide, args.shape_id);
        const before = { width: shape.width, height: shape.height };
        if (args.width !== undefined) shape.width = Number(args.width);
        if (args.height !== undefined) shape.height = Number(args.height);
        this.#mutated();
        return this.#envelope(
          { slide_id: slide.id, shape_id: shape.id, width: shape.width, height: shape.height },
          [`슬라이드 ${slide.id} · 도형 ${shape.id}: ${before.width}×${before.height}pt → ${shape.width}×${shape.height}pt`]);
      }
      case 'delete_shape': {
        const slide = this.#slide(args);
        const shape = this.#shape(slide, args.shape_id);
        slide.shapes = slide.shapes.filter((s) => s.id !== shape.id);
        this.#mutated();
        return this.#envelope({ slide_id: slide.id, deleted: shape.id },
          [`슬라이드 ${slide.id}: 도형 ${shape.id}("${shape.name}") 삭제 — 되돌릴 수 없습니다`]);
      }
      case 'add_shape': {
        const slide = this.#slide(args);
        const shape = {
          id: `sh-new-${this.nextId++}`, name: args.kind ?? 'textbox',
          type: 'TextBox', text: String(args.text ?? ''),
          width: Number(args.width ?? 200), height: Number(args.height ?? 60),
        };
        slide.shapes.push(shape);
        this.#mutated();
        return this.#envelope({ slide_id: slide.id, shape_id: shape.id },
          [`슬라이드 ${slide.id}: 도형 ${shape.id} 추가("${shape.text}")`]);
      }
      case 'snapshot_slide': {
        const slide = this.#slide(args);
        const id = `snap-${this.nextId++}`;
        this.snapshots.set(id, JSON.parse(JSON.stringify(slide)));
        return this.#envelope({ snapshot: id, slide_id: slide.id });
      }
      case 'restore_slide': {
        const kept = this.snapshots.get(args.snapshot);
        if (!kept) throw new Error(`스냅샷 ${args.snapshot} 이 없습니다`);
        const at = this.model.slides.findIndex((s) => s.id === kept.id);
        // **되돌린 슬라이드는 id 가 바뀐다**(§2.1) — 넣는 문이 제자리 교체가 아니라 삽입이다.
        // 그 사실을 결과가 실어야 다음 호출이 낡은 id 로 안 간다.
        const restored = { ...JSON.parse(JSON.stringify(kept)), id: `${kept.id}-r${this.nextId++}` };
        if (at >= 0) this.model.slides.splice(at, 1, restored);
        else this.model.slides.push(restored);
        this.#mutated();
        return this.#envelope({ slide_id: restored.id, replaced: kept.id },
          [`슬라이드 ${kept.id} 를 스냅샷 ${args.snapshot} 으로 되돌렸습니다 — 새 id 는 ${restored.id} 입니다`]);
      }
      case 'advise':
      case 'clear_advice':
        // 안내는 **덱을 안 고친다**(§6.1). 창이 로그의 도구 호출을 접어서 그리므로 손이 할 일은
        // 「받았다」뿐이다. `changed` 를 안 싣는 것이 계약이다 — 안내는 한 일이 아니라 할 말이다.
        return this.#envelope({ pinned: op === 'advise' ? (args.items?.length ?? 0) : 0 });
      default:
        // 헬퍼 목록에 있는데 손이 모르는 조작. **던진다** — 광고와 실행이 어긋난 것을
        // 조용히 성공으로 답하면 그게 §2.3 의 최악이다.
        throw new Error(`이 손은 ${op} 을 모릅니다`);
    }
  }
}
