/**
 * 대화의 읽은 자리. **숫자 혼자 다니지 않는다 — 어느 대화에서 센 숫자인지를 같이 든다.**
 *
 * 와이어에는 `since` 숫자만 실린다. 코어가 그 한계를 자기 입으로 적어 뒀다
 * (`internal/adapter/daemon/daemon.go` 의 `answerable`): 어제 대화의 seq 40 은 오늘 대화의
 * **진짜 위치**라서, 서버는 그것이 남의 대화에서 센 숫자인지 알 방법이 없다. 서버가 잡아 주는
 * 것은 **로그 끝을 넘은** 커서 하나뿐이고, 범위 안에 떨어지는 낡은 커서는 못 잡는다.
 *
 * 그래서 세션 id 를 seq 옆에 같이 드는 것이 **클라이언트 몫**이고, 이 클래스가 그 자리다.
 * 콘솔이 로컬에서 하는 일과 같은 것을 우리는 소켓 너머에서 한다(clients/powerpoint/DESIGN.md §5.7).
 *
 * 값이다 — 옮길 때마다 새것을 만든다.
 */
export class Cursor {
  constructor(sessionId = null, seq = -1) {
    this.sessionId = sessionId;
    this.seq = seq;
    Object.freeze(this);
  }

  /** 아무것도 안 읽은 자리. `-1` 은 계약상 "전부"다(0 과 같은 뜻). */
  static empty() { return new Cursor(null, -1); }

  /**
   * 이 세션에 붙을 때 실제로 보낼 `since`.
   *
   * **대화가 다르면 -1 이다.** 옛 커서를 새 대화에 들고 가면 그 대화의 앞을 못 본다 — 그리고
   * 그건 서버가 못 잡아 주는 쪽의 사고다.
   */
  sinceFor(sessionId) {
    return this.sessionId === sessionId ? this.seq : -1;
  }

  /** 이 커서가 저 세션에 쓸 수 있는 것인가. 화면이 "처음부터 받습니다"를 말할 때 쓴다. */
  usableFor(sessionId) {
    return this.sessionId === sessionId && this.seq > 0;
  }

  /**
   * 이벤트 하나를 읽었다. **뒤로는 안 간다** — 스트림이 순서대로 온다는 계약을 여기서 한 번 더
   * 지키는데, 어긋나면 그건 이벤트가 아니라 우리 상태가 틀어진 것이라 조용히 넘기면 안 된다.
   */
  advanced(sessionId, seq) {
    if (typeof seq !== 'number' || !Number.isFinite(seq)) return this;
    // **자리 없는 이벤트는 자리를 못 민다.** 버스 전용(transient) 이벤트는 로그에 안 앉으므로
    // `seq == 0` 으로 온다 — `part.delta`, `tool.progress`, `permission.requested`,
    // `question.requested`, `context.usage`, `workflow.phase`, `council.deliberating`,
    // `model.changed`, `user.label.changed` 가 그렇다(`internal/core/event/event.go`).
    // 그대로 커서에 넣으면 자리가 0 으로 **뒤로 간다**. 그리고 0 은 계약상 "전부"라서
    // 다음 접속이 대화를 통째로 다시 받는데, `answerable` 은 `since <= 0` 을 정상으로 보고
    // **거절 프레임도 안 보낸다** — 화면은 두 벌이 되고 아무도 왜인지 모른다.
    // ⚠ 오늘 이 줄은 **없어도 이 클라이언트는 안 다친다** — 돌연변이 시험으로 확인했다
    // (지우고 돌려도 아무 시험이 안 죽었다). 바로 아래 단조 규칙이 세션 안에서 이미 막고,
    // 세션이 다르면 `sinceFor` 가 -1 을 내며, 와이어에서 0 과 -1 이 같은 뜻이기 때문이다.
    // 그래도 남긴다 — **면역이 두 번의 우연에 기대고 있어서**다. 단조 규칙을 손대거나
    // `sinceFor` 의 -1 을 0 으로 바꾸는 순간 조용히 뚫린다. 규칙은 그때 적는 게 아니라 지금
    // 적는 것이고, 이 사실 자체는 clients/powerpoint/DESIGN.md §5.7 에도 없던 것이라
    // 거기에도 적었다.
    if (seq <= 0) return this;
    if (this.sessionId === sessionId && seq <= this.seq) return this;
    return new Cursor(sessionId, seq);
  }

  /** 커서를 버린다(서버가 거절했거나, 대화가 바뀌었거나). */
  reset() { return Cursor.empty(); }
}
