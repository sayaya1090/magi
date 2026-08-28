// 안내 항목. **도형이 아니다**(DESIGN.md §6.1).
//
// 화면에 그리는 포스트잇이 아니라 작업창에 사는 텍스트 한 줄이고, "여기"는 진짜 선택이 진다.
// 그래서 이 객체가 갖는 것은 말(message)과 가리킬 곳(target)이지 좌표가 아니다.
export class Advice {
  constructor({ id, message, slideId, shapeIds = [] }) {
    this.id = id;
    this.message = message;
    this.slideId = slideId;
    this.shapeIds = Object.freeze([...shapeIds]);
    Object.freeze(this);
  }

  /** 가리킬 곳이 있는가. 없으면 항목은 읽히기만 하고 눌리지 않는다. */
  get pointable() {
    return Boolean(this.slideId);
  }
}
