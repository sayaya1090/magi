/**
 * 모델에게 **내는** 쪽. 목업에서는 가짜가 물리고, 진짜는 데몬의 문(`dispatchNow` 의 submit)에
 * 붙는다(DESIGN.md §5.7).
 *
 * # 받는 쪽이 여기 없는 이유
 *
 * 답은 이 포트로 **안 온다.** `TranscriptPort` 로 온다. 편의가 아니라 문이 그렇게 생겼다 —
 * `transcript` 는 연결을 통째로 가져가므로 요청용 연결과 **다른 연결**이고, 둘은 서로의 생사
 * 증거가 아니다(`TranscriptPort` 4). 여기에 `subscribe` 를 두면 그 사실이 코드에서 지워지고,
 * 지워진 자리에서 화면은 「제출이 됐으니 스트림도 살아 있다」를 조용히 믿게 된다.
 *
 * 그리고 **아무것도 안 돌려준다.** 코어의 `Response` 에는 seq 도 messageId 도 없어서, 낸 것을
 * 로그에서 신원으로 찾을 방법이 없다(`Composer` 에 그 대가를 적어 뒀다).
 */
export class ChatPort {
  /**
   * 던지고 즉시 돌아온다. **답을 여기서 기다리지 않는다** — 기다리면 §5.7 의 교착이 난다.
   * @param {string} prompt 인용까지 접힌 완성된 글(`promptOf`)
   */
  async submit(_prompt) { throw new Error('not implemented'); }
}
