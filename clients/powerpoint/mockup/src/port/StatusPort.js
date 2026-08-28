/**
 * 데몬의 `status` 문과 답을 보내는 손.
 *
 * **왜 스트림이 아니라 폴링인가.** 물음은 로그에 안 쌓이고 막힌 데몬의 버스에만 나가므로,
 * `transcript`를 아무리 붙들고 있어도 안 온다. 어태치 TUI가 `pollInterval`로 `status`를
 * 두드리는 이유가 그것이고(`cmd/magi/attach.go`), 웹 콘솔은 명단이 실어 주는 `AskID`로 같은
 * 우회를 한다. **밖에서 그리는 창이면 다 하는 일**이라 이 포트가 따로 있다(DESIGN.md §5.7).
 *
 * 구현이 지켜야 하는 것:
 *
 * - `status()`는 **닿았는지 여부까지** 돌려준다. 못 닿은 것과 「묻는 게 없다」는 값이 같으면
 *   안 된다 — 앞엣것은 모르는 것이고 뒤엣것은 아는 것이다. 데몬이 죽으면 화면은 살아 있는
 *   것처럼 보인다(로그는 그대로 읽히니까). 그래서 못 닿음은 **한 번 소리 내어** 말한다.
 * - `answerPermission(callId, decision)`은 **대화 턴이 아니다.** 답은 호출에 붙고 턴이 도는
 *   중에 나가야 하므로 채팅 제출과 다른 손이다(`dispatchNow`의 `permission`은 `CallID`와
 *   `Decision`을 받지 텍스트를 안 받는다).
 * - 답한 뒤 그 물음이 사라졌는지는 **다음 `status`가 말해 준다.** 질문의 답은 채널로 곧장
 *   기다리던 도구에 가므로, 끝났다는 유일한 신호가 **데몬이 더는 그걸 안 알리는 것**이다.
 */
export class StatusPort {
  /** `{ reachable, pending, doing }` — `pending`은 `Pending` 또는 `null`. */
  async status() { throw new Error('StatusPort.status 미구현'); }
  async answerPermission(_callId, _decision) {
    throw new Error('StatusPort.answerPermission 미구현');
  }

  /**
   * 질문의 답. **권한과 손이 다르다** — 권한은 `allow`/`deny` 같은 정해진 낱말이고 질문은
   * 사람이 고른 글이다. 웹 콘솔도 `kind`와 `text`를 같이 실어 보내며 둘을 따로 검사한다
   * (`cmd/magi-web`의 답 처리기). 한 손으로 합치면 질문에 `allow`를 보낼 수 있게 된다.
   */
  async answerQuestion(_callId, _text) {
    throw new Error('StatusPort.answerQuestion 미구현');
  }
}
