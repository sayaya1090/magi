/**
 * 문서 하나에 대해 화면이 알아야 하는 것 — 어댑터만 Office 를 안다.
 *
 * 워드의 손잡이는 **문단 번호**(1부터, 본문 순서)다. Word.js 에는 문단의 안정된 id 가 없어 번호가 손잡이이고,
 * 고치면 아래 번호가 민다 — 그래서 유스케이스는 번호를 저장하지 않고 그때그때 읽는다.
 */
export class DocumentPort {
  /** 지금 선택 — { from, to, text, textTruncated, textUnavailable, approx }. 못 읽으면 던진다. */
  async selection() { throw new Error('not implemented'); }
  /** 그 문단으로 간다(선택을 옮긴다). 없는 번호면 던진다. */
  async point(_paragraph) { throw new Error('not implemented'); }
  /** 본문 문단 수. 못 주는 호스트는 null — 「모름」은 0 이 아니다. */
  async paragraphCount() { return null; }
  get label() { return 'unknown'; }
  get isHost() { return false; }
  capabilities() {
    return { measured: false, note: '이 어댑터는 요구 집합을 재지 않는다', sets: [] };
  }
}
