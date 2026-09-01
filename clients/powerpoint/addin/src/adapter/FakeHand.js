import { HandPort } from '../port/HandPort.js';
// 도형 이름표는 **한 벌만** 둔다 — 두 손이 다른 이름을 알면 브라우저에서 배운 것이 실물에서 틀린다.
import { geometryOf, placeShapes, ALIGNMENTS } from './OfficeHand.js';

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
      'set_text', 'format_shape', 'move_shape', 'align_shapes', 'add_shape', 'delete_shape',
      'apply_layout',
      'reorder_slide', 'set_hyperlink', 'add_table', 'set_table_cells',
      'snapshot_slide', 'restore_slide', 'advise', 'clear_advice',
      'list_layouts', 'describe_style', 'apply_style', 'add_slide', 'add_slides', 'delete_slide',
      'duplicate_slide', 'replace_table'];
  }

  /**
   * 가짜 덱이 가진 레이아웃. **테마가 없으므로 지어낸 것이고, 그렇게 적는다** — 진짜 덱의
   * 레이아웃 이름은 그 덱의 테마가 정한다(`OfficeHand.#listLayouts`).
   */
  static LAYOUTS = [
    { layout: '제목 및 내용', layout_id: 'fake-l1', placeholders: ['title', 'body'] },
    { layout: '제목만', layout_id: 'fake-l2', placeholders: ['title'] },
    { layout: '빈 화면', layout_id: 'fake-l3', placeholders: [] },
  ];

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
            // 표는 **격자로** 실린다(진짜 손과 같은 계약). 이게 없으면 이 화면은 「표가 하나
            // 있다」까지만 가르치고, 실물은 내용까지 준다 — 배운 것이 틀리게 된다.
            ...(Array.isArray(s.cells)
              ? { rows: s.rows ?? s.cells.length, columns: s.columns ?? (s.cells[0]?.length ?? 0), cells: s.cells }
              : {}),
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
        // **모르는 도형 이름은 여기서도 거절한다.** 이 화면이 아무 이름이나 받으면 사람은
        // 「`우주선` 도 되는구나」를 배우고, 실물에서 거절당한다.
        const kind = String(args.kind ?? 'textbox');
        if (kind.toLowerCase() !== 'textbox') geometryOf(kind);
        const shape = {
          id: `sh-new-${this.nextId++}`, name: kind,
          type: 'TextBox', text: String(args.text ?? ''),
          width: Number(args.width ?? 200), height: Number(args.height ?? 60),
        };
        slide.shapes.push(shape);
        this.#mutated();
        return this.#envelope({ slide_id: slide.id, shape_id: shape.id },
          [`슬라이드 ${slide.id}: 도형 ${shape.id} 추가("${shape.text}")`]);
      }
      // 아래 여섯은 **광고만 되고 없던 것들**이다(2026-09-02, 리뷰가 짚었다). 브라우저 갈래는
      // 도구를 눌러 보라고 있는 화면인데, 눌리는 도구의 3 분의 1 이 「이 손은 X 을 모릅니다」만
      // 내놓고 있었다 — 없는 것을 광고하는 것과 같은 결함이고, 방향만 반대다.
      case 'format_shape': {
        const slide = this.#slide(args);
        const shape = this.#shape(slide, args.shape_id);
        const changed = [];
        for (const [k, label] of [['font', '글꼴'], ['size', '크기'], ['bold', '굵게'],
          ['italic', '기울임'], ['color', '글자색'], ['fill', '채움'], ['align', '정렬']]) {
          if (args[k] === undefined) continue;
          shape[k] = args[k];
          changed.push(`${label} → ${args[k]}`);
        }
        if (changed.length === 0) {
          throw new Error('무엇을 바꿀지가 하나도 안 왔습니다 — font·size·bold·italic·color·fill·align 중 하나는 주세요');
        }
        this.#mutated();
        return this.#envelope({ slide_id: slide.id, shape_id: shape.id },
          [`슬라이드 ${slide.id} · 도형 ${shape.id}: ${changed.join(' / ')}`]);
      }
      case 'apply_layout': {
        const slide = this.#slide(args);
        const layout = FakeHand.LAYOUTS.find((l) => l.layout === args.layout);
        if (!layout) {
          throw new Error(`${args.layout} 이라는 레이아웃이 없습니다 — 이 덱에는: `
            + FakeHand.LAYOUTS.map((l) => l.layout).join(', '));
        }
        const before = slide.layout ?? '제목 및 내용';
        slide.layout = layout.layout;
        this.#mutated();
        return this.#envelope({ slide_id: slide.id, layout: slide.layout },
          [`슬라이드 ${slide.id}: 레이아웃 ${before} → ${slide.layout}`]);
      }
      case 'reorder_slide': {
        const slide = this.#slide(args);
        const from = this.model.slides.indexOf(slide) + 1;
        const to = Math.min(Math.max(1, Number(args.to)), this.model.slides.length);
        this.model.slides.splice(from - 1, 1);
        this.model.slides.splice(to - 1, 0, slide);
        this.#mutated();
        return this.#envelope({ slide_id: slide.id, from, to },
          [`슬라이드 ${slide.id}: ${from} 번 → ${to} 번 — 이 뒤의 번호는 전부 달라졌습니다`]);
      }
      case 'set_hyperlink': {
        const slide = this.#slide(args);
        const shape = this.#shape(slide, args.shape_id);
        const before = shape.url ?? null;
        shape.url = args.url ? String(args.url) : null;
        this.#mutated();
        return this.#envelope({ slide_id: slide.id, shape_id: shape.id, url: shape.url },
          [`슬라이드 ${slide.id} · 도형 ${shape.id}: 링크 ${before ?? '없음'} → ${shape.url ?? '없음'}`]);
      }
      case 'add_table': {
        const slide = this.#slide(args);
        const rows = Number(args.rows);
        const columns = Number(args.columns);
        // **이미 표가 있으면 말한다**(진짜 손과 같은 계약) — 「고쳐 줘」를 「더해 줘」로 받으면
        // 표가 둘이 되고, 사람 눈에는 아무 일도 안 일어난 것으로 보인다.
        const already = slide.shapes.filter((sh) => Array.isArray(sh.cells));
        const cells = Array.from({ length: rows }, (_, r) =>
          Array.from({ length: columns }, (_, c) => String(args.values?.[r]?.[c] ?? '')));
        const shape = {
          id: `tb-${this.nextId++}`, name: '표', type: 'Table', text: '',
          width: Number(args.width ?? 360), height: Number(args.height ?? 120),
          rows, columns, cells,
        };
        slide.shapes.push(shape);
        this.#mutated();
        return this.#envelope(
          { slide_id: slide.id, shape_id: shape.id, rows, columns, tables_before: already.length },
          [`슬라이드 ${slide.id}: ${rows}행 ${columns}열 표 ${shape.id} 추가`
            + (already.length
              ? ` · ⚠ 이 장에는 이미 표가 ${already.length}개 있습니다(${already.map((t) => t.id).join(', ')}) — `
                + '고치려던 것이면 그 표를 replace_table 로 바꾸거나 set_table_cells 로 글만 채우세요'
              : '')]);
      }
      case 'replace_table': {
        // 진짜 손과 같은 계약이라야 이 화면에서 배운 것이 실물에서도 맞다.
        const slide = this.#slide(args);
        const tables = slide.shapes.filter((sh) => Array.isArray(sh.cells));
        if (tables.length === 0) {
          throw new Error(`슬라이드 ${slide.id} 에는 표가 없습니다 — 새로 만들려면 add_table 을 쓰세요`);
        }
        let old = tables[0];
        if (args.shape_id) {
          old = tables.find((t) => t.id === args.shape_id);
          if (!old) {
            throw new Error(`도형 ${args.shape_id} 는 이 장의 표가 아닙니다 — 이 장의 표: `
              + tables.map((t) => t.id).join(', '));
          }
        } else if (tables.length > 1) {
          // **여럿이면 안 고른다** — 골라 주면 엉뚱한 표가 사라지고 그건 못 되돌린다.
          throw new Error(`이 장에는 표가 ${tables.length}개 있습니다 — 어느 것인지 shape_id 로 말해 주세요: `
            + tables.map((t) => t.id).join(', '));
        }
        const rows = Number(args.rows ?? old.rows);
        const columns = Number(args.columns ?? old.columns);
        const kept = old.cells ?? [];
        const cells = Array.from({ length: rows }, (_, r) =>
          Array.from({ length: columns }, (_, c) =>
            String(args.values?.[r]?.[c] ?? kept[r]?.[c] ?? '')));
        const made = {
          id: `tb-${this.nextId++}`, name: '표', type: 'Table', text: '',
          // 자리도 물려받는다 — **제자리 교체**라는 이름이 그 뜻이다(진짜 손과 같은 계약).
          left: Number(args.left ?? old.left ?? 0), top: Number(args.top ?? old.top ?? 0),
          width: Number(args.width ?? old.width), height: Number(args.height ?? old.height),
          rows, columns, cells,
        };
        slide.shapes.splice(slide.shapes.indexOf(old), 1, made);
        this.#mutated();
        return this.#envelope(
          { slide_id: slide.id, shape_id: made.id, replaced: old.id, rows, columns,
            was: { rows: old.rows, columns: old.columns } },
          [`슬라이드 ${slide.id}: 표 ${old.id}(${old.rows}×${old.columns}) 를 지우고 `
            + `같은 자리에 ${rows}×${columns} 표 ${made.id} 를 놓았습니다 — 옛 id 는 이제 없습니다`]);
      }
      case 'set_table_cells': {
        const slide = this.#slide(args);
        const shape = this.#shape(slide, args.shape_id);
        if (!shape.cells) throw new Error(`도형 ${shape.id} 는 표가 아닙니다`);
        const wrote = [];
        for (const cell of args.cells ?? []) {
          const r = Number(cell.row);
          const c = Number(cell.column);
          // **없는 칸은 만들지 않는다** — 표 바깥에 글을 쓴 것처럼 답하면 사람은 안 보이는
          // 글을 찾아 헤맨다. 진짜 손도 같은 자리에서 `isNullObject` 를 보고 거절한다.
          if (!shape.cells[r] || shape.cells[r][c] === undefined) {
            throw new Error(`표 ${shape.id} 에 ${r}행 ${c}열 칸이 없습니다 — `
              + `${shape.rows}행 ${shape.columns}열 표입니다`);
          }
          shape.cells[r][c] = String(cell.text ?? '');
          wrote.push(`(${r},${c})`);
        }
        this.#mutated();
        return this.#envelope({ slide_id: slide.id, shape_id: shape.id, wrote: wrote.length },
          [`슬라이드 ${slide.id} · 표 ${shape.id}: ${wrote.join(' ')} 칸을 채웠습니다`]);
      }
      case 'align_shapes': {
        // 셈은 **진짜 손과 같은 순수 함수**(`placeShapes`)가 한다 — 두 곳에서 따로 셈하면
        // 브라우저에서 맞춰 본 배치가 실물에서 다르게 선다.
        const how = String(args.how ?? '').toLowerCase().replace(/[\s-]/g, '_');
        if (!ALIGNMENTS.has(how)) {
          throw new Error(`${args.how} 는 이 손이 아는 정렬이 아닙니다 — 아는 것: `
            + [...ALIGNMENTS].join(', '));
        }
        const slide = this.#slide(args);
        const all = slide.shapes;
        let want = all;
        if (Array.isArray(args.shape_ids) && args.shape_ids.length) {
          // **진짜 손과 같은 엄격함.** 못 찾은 id 를 조용히 빼면 브라우저에서 통과한 호출이
          // 실물에서 엉뚱한 도형을 옮긴다 — 도형 id 는 한 장 안에서만 유일하다.
          const here = new Map(all.map((sh) => [String(sh.id), sh]));
          const missing = args.shape_ids.filter((id) => !here.has(String(id)));
          if (missing.length) {
            throw new Error(`이 장에 없는 도형 id 입니다: ${missing.join(', ')} — `
              + '도형 id 는 한 장 안에서만 유일하니 다른 장에서 읽은 id 일 수 있습니다. '
              + `이 장의 도형: ${all.map((sh) => sh.id).join(', ') || '없음'}`);
          }
          want = args.shape_ids.map((id) => here.get(String(id)));
        }
        if (want.length < 2) {
          throw new Error(`줄 세울 도형이 ${want.length}개뿐입니다 — 둘 이상 골라 주세요`
            + ` (이 장의 도형: ${all.map((sh) => sh.id).join(', ') || '없음'})`);
        }
        const box = want.map((sh) => ({
          sh,
          left: Number(sh.left ?? 0), top: Number(sh.top ?? 0),
          width: Number(sh.width ?? 0), height: Number(sh.height ?? 0),
        }));
        const moves = placeShapes(box, how);
        if (moves.length === 0) {
          return this.#envelope({ slide_id: slide.id, moved: 0, planned: 0, how, of: want.length },
            [`도형 ${want.length}개가 이미 그렇게 서 있어 옮긴 것이 없습니다`]);
        }
        for (const m of moves) {
          if (m.left !== undefined) m.sh.left = m.left;
          if (m.top !== undefined) m.sh.top = m.top;
        }
        this.#mutated();
        // 봉투의 칸도 같은 모양이어야 한다 — 창은 두 손을 구별하지 않고 그린다.
        return this.#envelope(
          { slide_id: slide.id, moved: moves.length, planned: moves.length, how, of: want.length },
          [`슬라이드 ${slide.id}: 도형 ${want.length}개 중 ${moves.length}개를 옮겼습니다 — `
            + '기준은 슬라이드가 아니라 고른 도형들 자신입니다']);
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
      case 'list_layouts':
        return this.#envelope({
          masters: [{ master: '가짜 마스터', master_id: 'fake-m1', layouts: FakeHand.LAYOUTS }],
        });
      case 'add_slide': {
        const layout = args.layout ? FakeHand.LAYOUTS.find((l) => l.layout === args.layout) : FakeHand.LAYOUTS[0];
        if (!layout) {
          // **비슷한 것으로 갈음하지 않는다** — 있는 이름을 다 적어 주면 다음 호출에서 맞다.
          throw new Error(`${args.layout} 이라는 레이아웃이 없습니다 — 이 덱에는: `
            + FakeHand.LAYOUTS.map((l) => l.layout).join(', '));
        }
        const filled = [];
        const shapes = layout.placeholders.map((role, i) => {
          const text = role === 'title' ? String(args.title ?? '') : String(args.body ?? '');
          if (text) filled.push(role);
          return {
            id: `ph-${this.nextId++}-${i}`, name: role, type: 'TextBox', text,
            width: 360, height: role === 'title' ? 60 : 120,
          };
        });
        // **못 넣은 글의 이름을 댄다** — 진짜 손과 같은 계약이라야 이 화면에서 배운 것이
        // 실물에서도 맞다(`OfficeHand.#fillPlaceholders`).
        const unfilled = [];
        if (args.title && !filled.includes('title')) unfilled.push('title');
        if (args.body && !filled.includes('body')) unfilled.push('body');
        const slide = { id: `sl-new-${this.nextId++}`, layout: layout.layout, shapes };
        const at = args.at === undefined
          ? this.model.slides.length
          : Math.max(0, Math.min(Number(args.at) - 1, this.model.slides.length));
        this.model.slides.splice(at, 0, slide);
        this.#mutated();
        return this.#envelope(
          // `styled` 는 **빈 배열이 계약**이다 — 가짜 덱에는 따라갈 버릇이 없다. 칸을 아예
          // 안 만들면 이 화면은 그 계약 자체를 안 가르친다.
          { slide_id: slide.id, slide: at + 1, layout: layout.layout, filled, unfilled, styled: [] },
          [`슬라이드 ${at + 1}(id ${slide.id}) 를 만들었습니다 — 레이아웃 ${layout.layout}`
            + (filled.length ? ` · ${filled.join(' · ')} 채움` : '')
            + (unfilled.length ? ` · ⚠ ${unfilled.join(' · ')} 자리가 없어 안 넣었습니다` : '')]);
      }
      case 'describe_style':
        // 가짜 덱에는 **따라갈 버릇이 없다.** 그렇게 적는다 — 지어낸 스타일을 주면 이 화면이
        // 실물과 다른 것을 가르친다.
        return this.#envelope({ title: null, body: null, seen: 0,
          note: '가짜 덱이라 잴 스타일이 없습니다 — PowerPoint 에 붙어야 나옵니다' });
      case 'apply_style': {
        // **진짜 손과 같은 계약이라야 이 화면에서 배운 것이 실물에서도 맞다.** 리뷰가 짚은
        // 어긋남 넷을 여기서 맞춘다(2026-09-02): 빈 서식을 성공으로 답하던 것, 모르는 칸을
        // 받아 주던 것, `slide_ids` 를 무시하던 것, 「다른 것만 바꾼다」를 안 지키던 것.
        const wantTitle = fakePickFont(args.title);
        const wantBody = fakePickFont(args.body);
        if (!wantTitle && !wantBody) {
          throw new Error('무엇을 바꿀지가 안 왔습니다 — title 이나 body 에 '
            + '{font, size, bold, italic, color} 중 하나는 주세요');
        }
        const all = this.model.slides;
        const want = Array.isArray(args.slide_ids) && args.slide_ids.length
          ? all.filter((sl) => args.slide_ids.includes(sl.id))
          : (Array.isArray(args.slides) && args.slides.length
            ? all.filter((sl, i) => args.slides.includes(i + 1))
            : all);
        if (want.length === 0) {
          throw new Error(`고른 장이 하나도 없습니다 — 이 덱은 ${all.length} 장입니다`);
        }
        this.#mutated();
        const lines = [];
        let touched = 0;
        let noTarget = 0;
        for (const sl of want) {
          const worn = [];
          let targets = 0;
          for (const sh of sl.shapes) {
            const role = /제목|title/i.test(String(sh.name ?? '')) ? 'title'
              : (/본문|body|subtitle/i.test(String(sh.name ?? '')) ? 'body' : null);
            const spec = role === 'title' ? wantTitle : (role === 'body' ? wantBody : null);
            if (!spec) continue;
            targets += 1;
            const diff = {};
            for (const [k, v] of Object.entries(spec)) {
              if (sh[k] !== v) diff[k] = v;
            }
            if (Object.keys(diff).length === 0) continue;   // **이미 그 값이면 안 건드린다**
            for (const [k, v] of Object.entries(diff)) sh[k] = v;
            worn.push(role);
          }
          if (targets === 0) noTarget += 1;
          if (worn.length) { touched += 1; lines.push(`슬라이드 ${sl.id}: ${worn.join(' · ')}`); }
        }
        const already = want.length - touched - noTarget;
        const why = [];
        if (noTarget) why.push(`${noTarget}개에는 제목·본문 자리표시자가 없습니다`);
        if (already) why.push(`${already}개는 이미 그 서식입니다`);
        const head = touched
          ? `장 ${want.length}개 중 ${touched}개를 바꿨습니다`
          : `장 ${want.length}개를 봤는데 바꾼 것이 없습니다`;
        return this.#envelope(
          { looked: want.length, changed: touched, unread: 0, no_target: noTarget, already },
          [head + (why.length ? ` — ${why.join(' · ')}` : '')].concat(lines.slice(0, 12)));
      }
      case 'add_slides': {
        // 진짜 손과 같은 계약 — 이름을 **먼저 다 확인하고**, 중간에 실패해도 앞의 장은 남는다.
        const plan = Array.isArray(args.slides) ? args.slides : [];
        if (plan.length === 0) {
          throw new Error('만들 장이 하나도 안 왔습니다 — slides 에 [{layout, title, body}] 를 주세요');
        }
        const missing = [...new Set(plan.map((x) => x.layout).filter(Boolean))]
          .filter((n) => !FakeHand.LAYOUTS.some((l) => l.layout === n));
        if (missing.length) {
          throw new Error(`${missing.join(', ')} 이라는 레이아웃이 없습니다 — 이 덱에는: `
            + FakeHand.LAYOUTS.map((l) => l.layout).join(', '));
        }
        const rows = [];
        for (const want of plan) {
          const layout = want.layout
            ? FakeHand.LAYOUTS.find((l) => l.layout === want.layout)
            : FakeHand.LAYOUTS[0];
          const filled = [];
          const shapes = layout.placeholders.map((role, i) => {
            const text = role === 'title' ? String(want.title ?? '') : String(want.body ?? '');
            if (text) filled.push(role);
            return { id: `ph-${this.nextId++}-${i}`, name: role, type: 'TextBox', text,
              width: 360, height: role === 'title' ? 60 : 120 };
          });
          const unfilled = [];
          if (want.title && !filled.includes('title')) unfilled.push('title');
          if (want.body && !filled.includes('body')) unfilled.push('body');
          const slide = { id: `sl-new-${this.nextId++}`, layout: layout.layout, shapes };
          this.model.slides.push(slide);
          rows.push({ slide: this.model.slides.length, slide_id: slide.id,
            layout: want.layout ?? null, filled, unfilled, styled: [] });
        }
        this.#mutated();
        const missed = rows.filter((r) => r.unfilled.length);
        return this.#envelope({ slides: rows, made: rows.length },
          [`장 ${rows.length}개를 만들었습니다`].concat(missed.length
            ? [`⚠ 넣을 자리가 없어 못 채운 것: `
              + missed.map((r) => `${r.slide}번의 ${r.unfilled.join(',')}`).join(' · ')]
            : []));
      }
      case 'delete_slide': {
        // 진짜 손과 같은 규칙 — 지우기는 보고 있는 장으로 넘겨짚지 않는다.
        if (args.slide === undefined && !args.slide_id) {
          throw new Error('어느 장을 지울지 slide 나 slide_id 로 정확히 말해 주세요 — '
            + '지우기는 스냅샷 없이 못 되돌리므로 보고 있는 장으로 넘겨짚지 않습니다');
        }
        const slide = this.#slide(args);
        const at = this.model.slides.indexOf(slide) + 1;
        this.model.slides.splice(at - 1, 1);
        this.#mutated();
        return this.#envelope({ deleted: slide.id, was: at },
          [`슬라이드 ${at}(id ${slide.id}) 를 지웠습니다 — 스냅샷 없이는 못 되돌리고, `
            + `${at} 번 뒤의 번호는 전부 하나씩 당겨졌습니다`]);
      }
      case 'duplicate_slide': {
        const slide = this.#slide(args);
        const from = this.model.slides.indexOf(slide) + 1;
        const copy = JSON.parse(JSON.stringify(slide));
        copy.id = `${slide.id}-c${this.nextId++}`;
        copy.shapes = copy.shapes.map((s) => ({ ...s, id: `${s.id}-c${this.nextId++}` }));
        this.model.slides.splice(from, 0, copy);
        this.#mutated();
        return this.#envelope({ slide_id: copy.id, slide: from + 1, from: slide.id },
          [`슬라이드 ${from} 을 복제해 ${from + 1} 번에 넣었습니다 — `
            + `복제본의 id 는 ${copy.id} 이고 원본과 다릅니다`]);
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

/**
 * 사람이 준 서식에서 **실제로 준 칸만** 뽑는다. `OfficeHand` 의 `pickFont` 와 같은 규칙이라야
 * 이 화면에서 배운 것이 실물에서도 맞다 — 빈 객체 `{}` 나 오타 난 칸(`colour`)을 여기서
 * 받아 주면, 사람은 그게 되는 줄 알고 실물에서 거절당한다.
 */
function fakePickFont(spec) {
  if (!spec || typeof spec !== 'object') return null;
  const out = {};
  if (spec.font !== undefined) out.name = String(spec.font);
  if (spec.name !== undefined) out.name = String(spec.name);
  if (spec.size !== undefined) out.size = Number(spec.size);
  if (spec.bold !== undefined) out.bold = Boolean(spec.bold);
  if (spec.italic !== undefined) out.italic = Boolean(spec.italic);
  if (spec.color !== undefined) out.color = String(spec.color);
  return Object.keys(out).length ? out : null;
}
