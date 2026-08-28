import { ChatPort } from '../port/ChatPort.js';

/**
 * 모델 없이 한 턴을 흉내 낸다. **말솜씨를 흉내 내는 게 목적이 아니라, 화면이 진짜와 같은
 * 모양의 이벤트를 겪게 하는 게 목적**이다(§5.7).
 *
 * 그래서 예전처럼 `{kind:'say'}` 같은 **다 익힌 이벤트를 안 만든다.** 그건 문이 안 주는
 * 모양이고, 그걸로 그린 화면은 진짜에 붙는 날 전부 다시 짜야 한다. 여기서 미는 것은 로그
 * 이벤트다 — `prompt.submitted` → `part.delta`(추론·글) → `part.appended`(글·도구 호출) →
 * `turn.finished`.
 *
 * 방향이 둘인 것도 그대로 남는다: `submit` 은 즉시 돌아오고, 답은 **다른 연결**인 전사
 * 스트림으로 온다.
 */
export class FakeChat extends ChatPort {
  constructor(transcript, { sessionId = 'sess-1', delay = 500 } = {}) {
    super();
    this.transcript = transcript;
    this.sessionId = sessionId;
    this.delay = delay;
    this.sent = [];    // 시험이 보는 것: 실제로 나간 글
    this.turn = 0;
  }

  async submit(prompt) {
    this.sent.push(prompt);
    const n = (this.turn += 1);
    const mid = `m${n}`;
    this.#push({
      type: 'prompt.submitted',
      actor: { kind: 'user', id: 'attach' },
      data: { messageId: `u${n}`, parts: [{ kind: 'text', text: prompt }] },
    });
    if (this.delay < 0) return;   // 시험이 손으로 민다
    setTimeout(() => this.reply(mid, prompt), this.delay);
  }

  /** 한 턴의 답. 시험이 시계 없이 부를 수 있게 갈라 뒀다. */
  reply(mid, prompt) {
    const quoted = /\[인용\] slide=(\S+) shape=(\S+)/.exec(prompt ?? '');
    this.#push({ seq: 0, type: 'part.delta',
      data: { messageId: mid, kind: 'reasoning', text: '상자 폭 문제로 보인다…' } });
    if (!quoted) {
      const ask = '어느 도형을 말씀하시는지 모르겠습니다. 도형을 잡고 「선택 인용」을 눌러 주세요.';
      this.#push({ type: 'part.appended',
        data: { messageId: mid, part: { kind: 'text', text: ask } } });
      this.#push({ type: 'turn.finished', data: { usage: {} } });
      return;
    }
    const [, slideId, shapeId] = quoted;
    this.#push({ type: 'part.appended', data: { messageId: mid, part: { kind: 'text',
      text: '제목이 두 줄로 넘칩니다. 글자 수보다 상자 폭 문제로 보입니다.' } } });
    // 안내는 답이 아니라 **도구 호출**로 온다. 창이 붙이는 포스트잇은 모델이 이 도구를
    // 부른 흔적이지 모델이 한 말이 아니다(§6.1).
    this.#push({ type: 'part.appended', data: { messageId: mid, part: { kind: 'tool-call',
      toolCall: { callId: `c${this.turn}`, name: 'mcp__ppt__advise', args: { items: [
        { message: '제목을 "3분기 매출 전망" 으로 줄이면 한 줄에 듭니다',
          slideId, shapeIds: [shapeId] },
        { message: '같은 덱의 다른 제목도 두 줄입니다 — 슬라이드 7 을 같이 보시죠',
          slideId: 's7', shapeIds: ['sh7t'] },
      ] } } } } });
    this.#push({ type: 'turn.finished', data: { usage: {} } });
  }

  #push(ev) { this.transcript.push({ sessionId: this.sessionId, ...ev }); }
}
