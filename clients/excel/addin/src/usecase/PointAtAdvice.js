/** 안내를 누르면 그 시트의 그 범위로 간다. */
export class PointAtAdvice {
  constructor(book) { this.book = book; }
  async run(advice) {
    if (!advice.pointable) return { ok: false, reason: advice.unpointableReason };
    try {
      await this.book.point(advice.sheet, advice.address);
      return { ok: true };
    } catch (e) {
      return { ok: false, reason: e?.message ?? String(e) };
    }
  }
}
