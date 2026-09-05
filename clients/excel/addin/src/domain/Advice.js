/**
 * 안내 — 모델이 `advise` 로 창에 꽂은 한 줄. 가리킬 곳은 시트와 범위다.
 */
export class Advice {
  constructor({ id, message, sheet, address }) {
    this.id = id;
    this.message = message;
    this.sheet = sheet ?? null;
    this.address = address ?? null;
    Object.freeze(this);
  }
  get unpointableReason() {
    return this.sheet || this.address ? null : '가리킬 곳이 안 실린 안내입니다';
  }
  get pointable() { return this.unpointableReason === null; }
}

/**
 * 가리킬 곳의 글. 시트 목록(`sheetNames`)이 답했는지에 따라 「확인 중」「지금 통합 문서에 없습니다」를
 * 가른다 — 파워포인트 판의 targetLabel 과 같은 규칙.
 */
export function targetLabel(advice, names, answered) {
  const sheet = advice.sheet;
  let where;
  if (!sheet) where = '(시트 미지정)';
  else if (names?.has(sheet)) where = `시트 ${sheet}`;
  else if (!answered) where = `시트 ${sheet} (확인 중)`;
  else if (names === null) where = `시트 ${sheet} (이 호스트는 목록을 못 줍니다)`;
  else where = `시트 ${sheet} (지금 통합 문서에 없습니다)`;
  return [where, advice.address].filter(Boolean).join(' · ');
}

/** 시트 목록의 세대 관리 — 파워포인트 판의 SlideNumbers 와 같은 기계. */
export class SheetIndex {
  constructor() {
    this.asks = 0;
    this.at = 0;
    this.map = null;        // Map(sheetName → 1-based index). `null` 은 「못 준다」는 **답**이다
    this.seen = new Map();  // sheet → 그 이름을 처음 본 때의 세대
  }
  note(sheet) {
    if (sheet && !this.seen.has(sheet)) this.seen.set(sheet, this.asks);
  }
  ask() { this.asks += 1; return this.asks; }
  answer(token, map) {
    if (token <= this.at) return false;
    this.at = token;
    this.map = map ?? null;
    return true;
  }
  answered(sheet) {
    return this.at > (this.seen.get(sheet) ?? this.asks);
  }
}
