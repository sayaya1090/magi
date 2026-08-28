import { DeckPort } from '../port/DeckPort.js';

/**
 * PowerPoint 없이 도는 덱. **목업이 오늘 실제로 도는 유일한 길이다.**
 *
 * 가짜인 것을 숨기지 않는다 — 화면이 label 을 그대로 띄우고, 선택은 옆의 미니 캔버스에서
 * 사용자가 진짜로 클릭해 만든다. 클릭으로 선택을 만드는 것까지가 가짜이고, **그 뒤의 흐름은
 * 진짜와 같은 코드**다(유스케이스가 이 둘을 구분 못 하는 것이 요점이다).
 */
export class FakeDeck extends DeckPort {
  constructor(model) {
    super();
    this.model = model;     // {slides:[{id,title,shapes:[{id,name,type,text,width,height}]}]}
    this.currentSlide = model.slides[0].id;
    this.selected = new Set();
    this.listeners = new Set();
  }

  get label() { return '가짜 덱 (PowerPoint 없이)'; }

  onChange(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  #emit() { for (const fn of this.listeners) fn(); }

  slide(id) { return this.model.slides.find((s) => s.id === id); }

  /** 미니 캔버스의 클릭. shift 면 더한다 — 진짜 PowerPoint 의 손버릇과 같게. */
  click(shapeId, additive) {
    if (!additive) this.selected.clear();
    if (this.selected.has(shapeId)) this.selected.delete(shapeId);
    else this.selected.add(shapeId);
    this.#emit();
  }

  goTo(slideId) {
    if (this.currentSlide === slideId) return;
    this.currentSlide = slideId;
    this.selected.clear();   // 슬라이드를 옮기면 선택이 풀린다
    this.#emit();
  }

  async selection() {
    const slide = this.slide(this.currentSlide);
    const shapes = slide.shapes.filter((s) => this.selected.has(s.id));
    return { slideId: slide.id, shapes: shapes.map((s) => ({ ...s })) };
  }

  async point(slideId, shapeIds) {
    const slide = this.slide(slideId);
    if (!slide) throw new Error(`슬라이드 ${slideId} 가 없다`);
    const missing = (shapeIds ?? []).filter((id) => !slide.shapes.some((s) => s.id === id));
    // **비슷한 것으로 갈음하지 않는다.** 지워진 도형을 가리키면 그대로 실패한다(§5.8).
    if (missing.length) throw new Error(`도형 ${missing.join(', ')} 을 찾을 수 없다`);
    this.currentSlide = slideId;
    this.selected = new Set(shapeIds ?? []);
    this.#emit();
  }
}
