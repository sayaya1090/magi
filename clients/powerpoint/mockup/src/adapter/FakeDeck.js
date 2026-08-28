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
    /**
     * 번호표를 줄 수 있는가. **목업 전용 손잡이다** — 1.8 아래 호스트(`OfficeDeck` 는 그때
     * `null` 을 준다)를 눌러 볼 길이 여기 말고는 없고, 안 눌러 보면 그 화면은 안 만든 것이다.
     */
    this.numbering = true;
  }

  /**
   * **가짜는 아무것도 안 잰다.** 여기서 "1.8 지원"을 돌려주면 화면이 실측처럼 보이고, 그게
   * 정확히 이 목업이 안 하기로 한 것이다. 그래서 잰 적 없다고 말한다.
   */
  capabilities() {
    return { measured: false, note: '가짜 덱 — 호스트가 없어 잰 것이 없다', sets: [] };
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
    return { slideId: slide.id, slideNo: slide.no ?? null, shapes: shapes.map((s) => ({ ...s })) };
  }

  async slideNumbers() {
    if (!this.numbering) return null;   // 1.8 아래 호스트 흉내. **지어내지 않는다.**
    return new Map(this.model.slides.map((s) => [s.id, s.no]));
  }

  async point(slideId, shapeIds) {
    const slide = this.slide(slideId);
    if (!slide) throw new Error(`덱에 없는 슬라이드입니다: ${slideId}`);
    const missing = (shapeIds ?? []).filter((id) => !slide.shapes.some((s) => s.id === id));
    // **비슷한 것으로 갈음하지 않는다.** 지워진 도형을 가리키면 그대로 실패한다(§5.8).
    if (missing.length) throw new Error(`찾을 수 없는 도형입니다: ${missing.join(', ')}`);
    this.currentSlide = slideId;
    this.selected = new Set(shapeIds ?? []);
    this.#emit();
  }
}
