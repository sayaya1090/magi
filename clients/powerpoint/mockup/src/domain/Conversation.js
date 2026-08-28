// 대화. 순수 상태다 — 전송도 구독도 모른다.
export class Conversation {
  constructor() {
    this.turns = [];        // {role:'user'|'model', text, quotes:Quote[]}
    this.pending = [];      // 아직 안 보낸 인용
  }

  /** 인용을 담는다. 같은 도형을 두 번 담지 않는다. */
  attach(quote) {
    if (this.pending.some((q) => q.shapeId === quote.shapeId)) return false;
    this.pending.push(quote);
    return true;
  }

  detach(shapeId) {
    const n = this.pending.length;
    this.pending = this.pending.filter((q) => q.shapeId !== shapeId);
    return this.pending.length !== n;
  }

  /** 사용자 차례를 닫는다. 담아 둔 인용이 그 말에 붙어 나가고 대기열은 빈다. */
  say(text) {
    const turn = { role: 'user', text, quotes: this.pending };
    this.pending = [];
    this.turns.push(turn);
    return turn;
  }

  hear(text) {
    const turn = { role: 'model', text, quotes: [] };
    this.turns.push(turn);
    return turn;
  }

  /** 보낼 것이 있는가 — 인용만 있고 말이 없어도 보낼 수 있다. */
  canSend(text) {
    return text.trim().length > 0 || this.pending.length > 0;
  }
}
