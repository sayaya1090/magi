// 인용 블록. **그 순간의 사진이지 살아 있는 참조가 아니다**(DESIGN.md §5.8).
//
// 그래서 이 객체는 만들어진 뒤로 덱을 다시 안 본다. 사용자가 인용한 뒤 도형을 지우거나 고쳐도
// 여기 담긴 값은 안 바뀌고, 나중에 shapeId 가 안 맞으면 그때 **멈추는 것이 계약**이다 — 비슷한
// 것을 찾아 대신 고치면 모델이 엉뚱한 도형을 고치고도 성공했다고 말한다.
export class Quote {
  constructor({ slideId, shapeId, name, type, text, width, height }) {
    this.slideId = slideId;
    this.shapeId = shapeId;
    this.name = name ?? '';
    this.type = type ?? 'Unknown';
    this.text = text ?? '';
    this.width = width ?? null;   // pt
    this.height = height ?? null; // pt
    Object.freeze(this);
  }

  /** 사람이 창에서 읽는 한 줄. */
  get headline() {
    return this.name ? `${this.name}` : this.type;
  }

  /** 길면 자른다 — 인용은 요약이 아니라 지시 대상이다. */
  preview(limit = 60) {
    const t = this.text.replace(/\s+/g, ' ').trim();
    return t.length > limit ? t.slice(0, limit - 1) + '…' : t;
  }

  get sizeLabel() {
    if (this.width == null || this.height == null) return null;
    const cm = (pt) => (pt * 2.54 / 72).toFixed(1);
    return `${cm(this.width)}×${cm(this.height)}cm`;
  }

  /**
   * 모델에게 가는 모양. **텍스트다**(개정 3 — 이미지를 주로 쓰지 않는다).
   * 도구가 그대로 받을 수 있게 신원(slide/shape)이 먼저 온다.
   *
   * 긴 글은 자른다(§5.8). 자르면 **잘랐다고 적는다** — 안 적으면 모델은 그게 전문인 줄 알고
   * 뒤쪽을 없는 것으로 치고 고친다. 신원을 받았으니 전문이 필요하면 읽기 도구로 물으면 된다.
   */
  toPrompt(limit = 400) {
    const head = `[인용] slide=${this.slideId} shape=${this.shapeId} type=${this.type}` +
      (this.name ? ` name="${this.name}"` : '');
    if (!this.text) return head;
    const cut = this.text.length > limit;
    const body = cut ? this.text.slice(0, limit) : this.text;
    return `${head}${cut ? ` textTruncated=${this.text.length}` : ''}\n       text="${body}"`;
  }
}
