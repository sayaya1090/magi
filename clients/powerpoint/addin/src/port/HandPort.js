/**
 * 덱을 **고치는** 구멍. 읽고 가리키는 `DeckPort` 와 갈라 둔 것은 취향이 아니다
 * (clients/powerpoint/DESIGN.md §2.3 — 읽기 천장과 쓰기 천장이 다르다, §10.3 의
 * 「읽는 모델과 쓰는 모델은 같은 물건이 아니다」).
 *
 * 여기 오는 것은 **모델이 부른 도구**다. `DeckPort` 로 가는 것은 사람이 누른 제스처다.
 * 둘은 주어가 다르고, 실패했을 때 할 말이 다르다 — 사람에게는 창이 말하고, 모델에게는
 * 도구 결과가 말한다.
 *
 * # 구현이 지켜야 하는 것
 *
 * - **못 하는 것은 던진다.** 조용히 성공하면 §2.3 이 최악이라고 적은 실패가 난다 —
 *   「고쳤습니다」 하고 아무것도 안 바뀌는 것.
 * - **쓰기는 바뀐 값을 실어 돌려준다**(`changed`). 우리 도구는 파일을 안 쓰므로 카운슬이
 *   보는 「이번 턴의 편집」 칸이 늘 비고, 그 자리를 메우는 것이 이 배열뿐이다(§4.4 ⑤·§7).
 * - **위치는 1부터**다(CAPABILITIES.md §10.4). 받은 즉시 id 로 옮기고, 그 다음은 id 로 일한다.
 * - **못 읽은 것은 없는 것이 아니다.** 읽기 결과의 `unreadable` 에 이름을 적는다 — 모델에게
 *   노트가 *없다*고 말하면 노트가 없는 덱이라고 믿는다(§10.5).
 */
export class HandPort {
  /**
   * 조작 하나.
   * @param {string} _op   도구 이름 그대로(`list_slides` · `set_text` …)
   * @param {object} _args 헬퍼가 이미 검사한 인자
   * @returns {Promise<{result?:object, changed?:string[], label?:string,
   *                    document?:string, epoch?:number, count?:number}>}
   */
  async run(_op, _args) { throw new Error('HandPort.run 미구현'); }

  /** 이 손이 무엇인지 화면이 정직하게 말할 수 있게. */
  get label() { return 'unknown'; }

  /** 이 손이 아는 조작들. 헬퍼의 목록과 어긋나면 **모르는 조작**으로 던진다. */
  ops() { return []; }
}
