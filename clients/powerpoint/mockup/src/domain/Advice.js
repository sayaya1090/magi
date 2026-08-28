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

  /**
   * 왜 못 가리키는지. 가리킬 수 있으면 `null`.
   *
   * **없다는 사실만이 아니라 사유가 값에 실린다** — 이 문장은 두 군데서 필요하다. 눌렀을 때
   * 돌려주는 사유(`PointAtAdvice`)와, 안 눌리는 항목 옆에 적어 두는 줄(목록)이다. 후자가 없으면
   * 사람이 보는 건 회색 항목 하나뿐이고, 그건 "모델이 어딘지 안 말했다"와 "이 창이 고장났다"가
   * 똑같이 생긴 화면이다. 그래서 한 자리에서 낸다.
   */
  get unpointableReason() {
    return this.slideId ? null : '가리킬 곳이 안 실린 안내입니다';
  }

  /** 가리킬 곳이 있는가. 없으면 항목은 읽히기만 하고 눌리지 않는다. */
  get pointable() {
    return this.unpointableReason === null;
  }
}

/**
 * 목록에 적을 「가리킬 곳」 한 줄.
 *
 * **번호를 못 얻은 것과 아직 안 물어본 것을 가른다.** `DeckPort.slideNumbers` 가 빈 Map 이 아니라
 * `null` 을 돌려주기로 한 이유가 정확히 이 갈림인데, 화면이 도로 뭉치면 그 계약은 없는 것과 같다.
 * 셋 다 적는 글은 같은 id 지만 사람이 할 일이 다르다 — 기다린다 / 이 호스트에선 원래 안 나온다 /
 * 그 슬라이드가 사라졌으니 이 안내는 낡았다.
 *
 * @param {Advice} advice
 * @param {?Map<string,number>} nos 덱이 준 번호표. 못 얻었으면 null.
 * @param {boolean} answered 덱에 **물어보고 답을 받았는가**. false 면 아직 도는 중이다.
 */
export function targetLabel(advice, nos, answered) {
  const no = nos?.get(advice.slideId);
  let slide;
  if (no != null) slide = `슬라이드 ${no}`;
  else if (!answered) slide = `슬라이드 ${advice.slideId} (번호 확인 중)`;
  else if (nos === null) slide = `슬라이드 ${advice.slideId} (이 호스트는 번호를 못 줍니다)`;
  // 번호표는 덱 전체를 담는다. 답이 왔는데 이 id 가 없으면 그 슬라이드가 지금 덱에 없는 것이다.
  else slide = `슬라이드 ${advice.slideId} (지금 덱에 없습니다)`;
  return [slide, ...advice.shapeIds].join(' · ');
}
