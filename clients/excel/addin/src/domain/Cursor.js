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
    // **자리 없는 이벤트는 자리를 못 민다.** 로그에 안 앉은 이벤트는 `seq == 0` 으로 온다.
    // **어느 타입들인지는 여기 안 적는다** — 그리고 `transientTypes` 를 그 목록으로 가리키는
    // 것도 **틀렸다.** 그 집합이 답하는 물음은 「저장소가 **담을 수 있는** 타입이 무엇인가」고,
    // 여기서 묻는 것은 「이 프레임이 자리를 **갖고 왔는가**」다. 둘은 같은 물음처럼 생겼는데
    // 같지 않다 — 한 타입이 양쪽으로 오기 때문이고, 그 이유가 바로 아래에 있다.
    // 그대로 커서에 넣으면 자리가 0 으로 **뒤로 간다**. 그리고 0 은 계약상 "전부"라서
    // 다음 접속이 대화를 통째로 다시 받는데, `answerable` 은 `since <= 0` 을 정상으로 보고
    // **거절 프레임도 안 보낸다** — 화면은 두 벌이 되고 아무도 왜인지 모른다.
    //
    // 여기 한때 그 목록이 여덟 줄로 적혀 있었고, **두 판본이 잇달아 틀렸다.** 첫 판본은
    // `model.changed`·`user.label.changed` 를 전이로 세고 `question.answered` 를 뺐다 —
    // `event.go` 의 `// Transient events — bus only, not persisted.` 머리글 아래에 그 셋이
    // 어긋나게 앉아 있었고, 문서 셋이 그 머리글을 옮겼고 나는 문서를 옮겼다(§5.7 이 다섯을
    // 세어 적는다). 그래서 2026-08-29 에 지도를 직접 세어 여덟으로 고치고 **센 날짜까지
    // 적었다.** 그날 안에 그것도 낡았다 — 코어가 `model.changed` 를 사실 블록으로 옮기고
    // `user.label.changed` 를 지도에 넣어(`core/event: a constant under the wrong header
    // taught five documents the wrong set`) 지도가 아홉이 됐다.
    //
    // **날짜는 정직을 사지 정확을 안 산다.** 낡은 줄인 것을 읽는 사람이 알 수 있게 될 뿐,
    // 맞게 되지는 않는다. 그래서 이번엔 다시 안 센다 — 안 적는다.
    //
    // 안 적어도 되는 이유가 따로 있다. **목록을 다 고쳐도 목록으로는 못 맞힌다.**
    // `model.changed` 는 저장소가 있으면 자리를 갖고 오고 없으면 안 갖고 온다 — 같은 타입이
    // 양쪽으로 온다(`internal/app/routing.go` 의 `SetModel`). 그 없는 쪽 갈래는 한동안
    // 도달 불가능한 죽은 코드였는데(`New` 가 저장소를 늘 감쌌다), 코어가 그걸 고쳐서
    // **이제 진짜로 양쪽으로 온다**(`internal/app: the store-less guards could not fire,
    // because New always wrapped`). 그래서 요점은 목록이 아니다: **판단은 값에 건다.**
    // 아래 `seq <= 0` 은 목록을 한 번도 안 보므로, 주석이 두 번 틀려 있는 동안에도 그 줄은
    // 안 틀렸다.
    //
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
