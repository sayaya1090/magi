/** 안내 한 장 — 무엇을, 어느 문단에. 가리킬 곳이 없으면 읽기만 된다. */
export class Advice {
  constructor({ id, message, paragraph }) {
    this.id = id;
    this.message = message;
    const n = Number(paragraph);
    this.paragraph = Number.isInteger(n) && n > 0 ? n : null;
    Object.freeze(this);
  }
  get unpointableReason() {
    return this.paragraph ? null : '가리킬 곳이 안 실린 안내입니다';
  }
  get pointable() { return this.unpointableReason === null; }
}

/**
 * 「어느 문단」을 사람 말로. 문단 수는 뒤에 온다 — 묻기 전엔 「확인 중」, 못 주는 호스트는 그렇다고, 번호가 수를 넘으면
 * 「지금 문서에 없습니다」. 모름과 없음을 같은 말로 적지 않는다.
 */
export function targetLabel(advice, count, answered) {
  const p = advice.paragraph;
  if (!p) return '(문단 미지정)';
  if (typeof count === 'number' && p <= count) return `문단 ${p}`;
  if (!answered) return `문단 ${p} (확인 중)`;
  if (count === null) return `문단 ${p} (이 호스트는 문단 수를 못 줍니다)`;
  return `문단 ${p} (지금 문서에 없습니다)`;
}

/** 문단 수를 물은 세대 — 엑셀 판 SheetIndex 와 같은 기전, 답이 이름 목록이 아니라 수다. */
export class ParagraphIndex {
  constructor() {
    this.asks = 0;
    this.at = 0;
    this.count = null;      // 본문 문단 수. `null` 은 「못 준다」는 **답**이다
    this.seen = new Map();  // paragraph → 처음 본 세대
  }
  note(paragraph) {
    if (paragraph && !this.seen.has(paragraph)) this.seen.set(paragraph, this.asks);
  }
  ask() { this.asks += 1; return this.asks; }
  answer(token, count) {
    if (token <= this.at) return false;
    this.at = token;
    this.count = typeof count === 'number' ? count : null;
    return true;
  }
  answered(paragraph) {
    return this.at > (this.seen.get(paragraph) ?? this.asks);
  }
}
