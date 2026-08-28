/**
 * 대화가 흘러 들어오는 구멍. **애드인에서 데몬까지 가는 길이 아니라 헬퍼까지다.**
 *
 * 브라우저 페이지는 magi 의 이벤트 저장소를 못 읽는다. 그래서 헬퍼가 대신 데몬의 `transcript`
 * 문에 붙고, §5.5 가 이미 연 애드인↔헬퍼 연결을 **반대 방향으로 한 번 더** 쓴다(DESIGN.md §5.7).
 *
 * # 구현이 지켜야 하는 것 — 넷 다 코어가 적어 둔 계약이다
 *
 * 1. **커서 없이 붙으면 전량이 온다.** 스토어의 `filterFrom` 은 `fromSeq > 0` 일 때만 자르고
 *    seq 는 1부터라, **0 도 -1 도 "전부"**다.
 * 2. **서버가 커서를 거절하면 이벤트보다 먼저 사유 프레임 하나가 온다** (`answerable`).
 *    이벤트가 없고 사유만 실린 진짜 프레임이다. `onRestart` 로 올린다.
 * 3. **깨끗한 끝은 에러가 아니다.** 문이 그렇게 적어 뒀다. 그래서 `onEnd` 는 에러 인자를 안 받고
 *    **끊겼다는 사실만** 알린다 — 사유 없는 종료를 에러로 위장하지 않는다.
 * 4. **이건 별도 연결이다.** `transcript` 는 `watch` 처럼 연결을 통째로 가져가고, 클라이언트
 *    쪽 뮤텍스를 읽는 내내 쥔다. 같은 연결로 `status` 를 부르면 거절도 에러도 아니고 **그냥 안
 *    돌아온다.** 그러니 요청용 연결과 이 연결은 **서로의 생사 증거가 아니다.**
 */
export class TranscriptPort {
  /**
   * 붙는다. 돌려주는 것은 끊는 함수.
   *
   * @param {string} sessionId
   * @param {number} since  0 이하는 전부
   * @param {{onRestart:(why:string)=>void, onEvent:(ev:object)=>void, onEnd:()=>void}} handlers
   * @returns {() => void}
   */
  subscribe(_sessionId, _since, _handlers) { throw new Error('not implemented'); }

  /** 이 어댑터가 무엇인지 화면이 정직하게 말할 수 있게. */
  get label() { return 'unknown'; }
}
