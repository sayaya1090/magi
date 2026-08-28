/**
 * 덱에 닿는 유일한 구멍. **유스케이스는 Office.js 를 모른다.**
 *
 * 이 경계가 장식이 아닌 이유가 지금 하나 있다 — 이 저장소를 만든 머신에 PowerPoint 가 안 깔려
 * 있다. 그래서 `OfficeDeck` 은 오늘 못 돌리고 `FakeDeck` 은 맨 브라우저에서 돈다. 같은
 * 유스케이스가 둘 다에 물리므로 **화면과 흐름은 오늘 확인할 수 있고, 어댑터만 나중에 확인한다.**
 *
 * 구현이 지켜야 하는 것:
 * - `selection()` 은 **물어볼 때 아는 것**이다(§6.1). 알려 오는 이벤트는 프로덕션에 없다.
 * - `point()` 는 사용자가 항목을 눌렀을 때만 불린다(§6.1) — 남의 커서를 뺏는 일이다.
 * - 실패는 던진다. **못 찾은 것을 비슷한 것으로 갈음하지 않는다**(§5.8).
 */
export class DeckPort {
  /** @returns {Promise<{slideId:string, shapes:Array}>} 지금 잡혀 있는 것. 없으면 shapes:[] */
  async selection() { throw new Error('not implemented'); }

  /**
   * 슬라이드로 데려가고 그 도형을 잡는다. **순서가 계약이다**(S13 — 문서가 침묵하므로
   * 슬라이드를 먼저 고르고 도형을 잡는다).
   */
  async point(_slideId, _shapeIds) { throw new Error('not implemented'); }

  /** 이 어댑터가 무엇인지 화면이 정직하게 말할 수 있게. */
  get label() { return 'unknown'; }

  /**
   * 이 호스트가 무엇을 지원한다고 **자기 입으로 말하는가**. 화면에 한 줄로 남긴다.
   *
   * 여기 있는 이유는 계측이다. 이 문서(§3.3·§12 #4)가 기대는 값 — PowerPointApi 천장 —
   * 은 오늘 이 머신에서 못 잰다. 그런데 **사용자 머신에서 처음 도는 날 그 한 줄이 미검증
   * 항목을 실측으로 바꾼다.** 없는 검증을 있는 척하지 않는 것 다음으로 좋은 것은, 검증이
   * 가능해지는 순간 자동으로 남게 해 두는 것이다.
   *
   * 실패해도 던지지 않는다 — 계측이 본 작업을 못 막는다. 대신 `measured:false` 로 말한다.
   *
   * @returns {{measured:boolean, note:string, sets:Array<{name:string,version:string,ok:boolean}>}}
   */
  capabilities() { return { measured: false, note: '안 잼', sets: [] }; }
}
