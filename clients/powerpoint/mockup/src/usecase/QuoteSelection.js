import { Quote } from '../domain/Quote.js';

/**
 * 선택을 인용으로 바꾼다 — §5.8 의 두 번째 걸음.
 *
 * **특수키+클릭은 못 본다.** 애드인 JS 는 작업창 웹뷰에서 돌고 캔버스의 입력이 거기 안 온다.
 * 그래서 사용자가 **인용을 누르는 순간**을 이벤트로 삼고 그때 덱에 물어본다. 없는 것은 푸시고
 * 있는 것은 풀이다.
 *
 * 이 유스케이스가 곧 **S14 의 측정기**이기도 하다: 버튼을 누르려고 포커스가 작업창으로 갔을 때
 * 선택이 남아 있는지를 반환값이 그대로 말한다. 비어 있으면 그건 "인용할 게 없다"가 아니라
 * **"포커스가 옮겨지면서 선택이 날아갔다"** 일 수 있고, 둘을 구분 못 하는 채로 조용히 넘어가면
 * 안 되므로 사유를 갈라 돌려준다.
 */
export class QuoteSelection {
  constructor(deck, conversation) {
    this.deck = deck;
    this.conversation = conversation;
  }

  /** @returns {Promise<{added:Quote[], skipped:number, empty:boolean}>} */
  async run() {
    const { slideId, slideNo, shapes } = await this.deck.selection();
    if (!shapes || shapes.length === 0) {
      return { added: [], skipped: 0, empty: true };
    }
    const added = [];
    let skipped = 0;
    for (const s of shapes) {
      const q = new Quote({
        slideId,
        slideNo,
        shapeId: s.id,
        name: s.name,
        type: s.type,
        text: s.text,
        width: s.width,
        height: s.height,
      });
      if (this.conversation.attach(q)) added.push(q);
      else skipped += 1;
    }
    return { added, skipped, empty: false };
  }
}
