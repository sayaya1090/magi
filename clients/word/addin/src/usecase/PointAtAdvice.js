/** 안내를 누르면 그 문단으로 간다. 자동으로는 절대 안 옮긴다 — 누를 때만. */
export class PointAtAdvice {
  constructor(doc) { this.doc = doc; }
  async run(advice) {
    if (!advice?.pointable) return { ok: false, reason: advice?.unpointableReason ?? '안내가 없습니다' };
    try {
      await this.doc.point(advice.paragraph);
      return { ok: true, reason: null };
    } catch (e) {
      const m = e?.message ?? String(e);
      return { ok: false, reason: m.includes('없습니다') ? `없는 문단입니다 — ${m}` : `못 옮겼습니다 — ${m}` };
    }
  }
}
