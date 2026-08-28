import { ChatPort } from '../port/ChatPort.js';

/**
 * 모델 없이 답을 흉내 낸다. **모델의 말솜씨를 흉내 내는 게 목적이 아니라, 방향이 둘이라는 것을
 * 화면이 실제로 겪게 하는 게 목적**이다(§5.7) — submit 은 즉시 돌아오고 답은 나중에 구독으로 온다.
 *
 * 그래서 여기서 흉내 내야 하는 것은 지연이지 지능이 아니다.
 */
export class FakeChat extends ChatPort {
  constructor() {
    super();
    this.listeners = new Set();
  }

  subscribe(onEvent) { this.listeners.add(onEvent); return () => this.listeners.delete(onEvent); }
  #emit(ev) { for (const fn of this.listeners) fn(ev); }

  async submit(text, quotes) {
    // 즉시 돌아온다. 응답은 "받았다"뿐이다.
    setTimeout(() => this.#emit({ kind: 'thinking' }), 120);
    setTimeout(() => {
      if (quotes.length === 0) {
        this.#emit({ kind: 'say', text: '어느 도형을 말씀하시는지 모르겠습니다. 도형을 잡고 「선택 인용」을 눌러 주세요.' });
        return;
      }
      const q = quotes[0];
      this.#emit({
        kind: 'say',
        text: `${q.headline} 의 글이 ${q.preview(20)} 입니다. 두 줄로 넘치는 것은 글자 수보다 상자 폭 문제로 보입니다.`,
      });
      // 안내는 답과 **따로** 온다 — 창의 다른 층에 산다(§6.1).
      this.#emit({
        kind: 'advise',
        advice: {
          id: 'a1',
          message: `제목을 "3분기 매출 전망" 으로 줄이면 한 줄에 듭니다`,
          slideId: q.slideId,
          shapeIds: [q.shapeId],
        },
      });
      this.#emit({
        kind: 'advise',
        advice: {
          id: 'a2',
          message: '같은 덱의 다른 제목도 두 줄입니다 — 슬라이드 7 을 같이 보시죠',
          slideId: 's7',
          shapeIds: ['sh7t'],
        },
      });
    }, 900);
  }
}
