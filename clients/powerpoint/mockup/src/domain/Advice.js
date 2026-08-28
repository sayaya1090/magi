// 안내 항목. **도형이 아니다**(clients/powerpoint/DESIGN.md §6.1).
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

/**
 * 번호표 한 벌과 **그 답이 언제 것인가**.
 *
 * `targetLabel` 의 `answered` 를 화면이 `boolean` 하나로 들고 있었는데, 그 하나가 두 물음을
 * 겸했다 — 「물어본 적이 있는가」와 「**이 안내**에 대해 답을 받았는가」. 안내는 대화가 흐르면서
 * 뒤늦게 도착하므로 둘이 갈린다. 앞 판본은 첫 답이 온 뒤 영영 참이라, 그 뒤에 온 안내가 가리키는
 * 슬라이드가 낡은 스냅숏에 없으면 **「지금 덱에 없습니다」**라고 적었다. 덱에는 있고 우리가 안
 * 물어봤을 뿐인데, 없다고 단정한 것이다. 그래서 세대(`asks`)를 센다: 그 id 를 **처음 본 뒤에**
 * 던진 물음의 답이라야 그 id 에 대한 답이다.
 *
 * 늦게 온 옛 답도 안 앉힌다. 화면은 그릴 때마다 다시 묻는데(순서가 사용자 손에서 바뀐다) 앞의
 * 왕복이 뒤엣것보다 늦게 돌아올 수 있고, 그러면 새 번호 위에 옛 번호가 앉는다. **낡은 번호는
 * 없는 번호보다 나쁘다**는 것이 `OfficeDeck.slideNumbers` 가 캐시를 안 두는 이유고, 화면이
 * 여기서 뒤집으면 그 결정은 없는 것과 같다.
 */
export class SlideNumbers {
  constructor() {
    this.asks = 0;          // 지금까지 던진 물음 수
    this.at = 0;            // 지금 든 답이 몇 번째 물음의 답인가. 0 = 아직 아무 답도 없다
    this.map = null;        // 답. `null` 은 「번호를 못 준다」는 **답**이다
    this.seen = new Map();  // slideId → 그 id 를 처음 본 때의 세대
  }

  /** 이 id 를 언제 처음 봤는지 적어 둔다. 이미 적혀 있으면 안 건드린다. */
  note(slideId) {
    if (slideId && !this.seen.has(slideId)) this.seen.set(slideId, this.asks);
  }

  /** 물음 하나. 돌려주는 표를 답과 함께 다시 가져와야 한다. */
  ask() { this.asks += 1; return this.asks; }

  /**
   * 답을 앉힌다. 앉혔으면 `true`.
   *
   * `undefined` 를 `null` 로 눕힌다 — 포트가 아무것도 안 돌려주는 판이 생기면 `map` 이
   * `undefined` 가 되고, `targetLabel` 의 `nos === null` 이 빗나가 「지금 덱에 없습니다」로
   * 샌다. 안 준 것을 없다고 적는 그 자리다.
   */
  answer(token, map) {
    if (token <= this.at) return false;
    this.at = token;
    this.map = map ?? null;
    return true;
  }

  /**
   * 이 id 에 대해 답을 받았는가.
   *
   * 처음 본 때가 안 적힌 id 는 **지금 막 본 것**으로 친다(`this.asks`) — 안 적힌 것을
   * 「옛날부터 있었다」로 세면 그게 다시 거짓 단정이 된다.
   */
  answered(slideId) {
    return this.at > (this.seen.get(slideId) ?? this.asks);
  }
}
