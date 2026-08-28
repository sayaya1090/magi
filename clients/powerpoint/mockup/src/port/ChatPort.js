/**
 * 모델 쪽. 목업에서는 가짜가 물리고, 진짜는 데몬의 문(dispatchNow 의 submit/steer/…)에 붙는다
 * (DESIGN.md §5.7). **여기서 방향이 둘인 것이 중요하다** — 내는 것은 부르면 되고, 받는 것은
 * 구독이다(소켓이 락스텝이라 청하지 않은 프레임이 안 온다).
 */
export class ChatPort {
  /** 던지고 즉시 돌아온다. **답을 여기서 기다리지 않는다** — 기다리면 §5.7 의 교착이 난다. */
  async submit(_text, _quotes) { throw new Error('not implemented'); }

  /** 답과 안내가 흘러 오는 자리. @returns {() => void} 구독 해지 */
  subscribe(_onEvent) { throw new Error('not implemented'); }
}
