/**
 * 안내 항목을 눌렀을 때 캔버스가 따라간다 — §6.1 의 "여기".
 *
 * 도형을 그리지 않는다. 진짜 선택을 옮기므로 좌표 환산도 축소판도 필요 없다. 대신 **사용자가
 * 잡고 있던 것을 뺏는** 일이라 자동으로는 절대 안 하고, 누른 그 순간에만 한다.
 */
export class PointAtAdvice {
  constructor(deck) { this.deck = deck; }

  /** @returns {Promise<{ok:boolean, reason?:string}>} */
  async run(advice) {
    // 사유는 여기서 짓지 않는다 — 목록에도 같은 문장이 필요해서 `Advice` 한 자리에 있다.
    if (!advice.pointable) return { ok: false, reason: advice.unpointableReason };
    try {
      await this.deck.point(advice.slideId, advice.shapeIds);
      return { ok: true };
    } catch (e) {
      // 지워진 도형을 가리키는 안내가 있을 수 있다. **찾아 헤매지 않고 그대로 말한다.**
      return { ok: false, reason: e?.message ?? String(e) };
    }
  }
}
