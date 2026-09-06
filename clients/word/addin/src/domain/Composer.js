/**
 * **아직 안 보낸 것.** 보낸 것은 여기 안 산다 — 화면의 대화는 로그가 그린다
 * (clients/powerpoint/DESIGN.md §5.7).
 *
 * 예전엔 이 자리에 `turns[]` 가 있었고, 보낸 말을 그 자리에서 화면에 붙였다. 그 낙관적 메아리를
 * 버린 이유가 코어에 적혀 있다: `submit` 은 **아무 식별자도 안 돌려주고**(`Response` 에 seq 도
 * messageId 도 없다), 밖에서 붙은 창은 전부 `actor.id = "attach"` 로 찍힌다(`dispatchNow`).
 * 그러니 로그에 올라온 `prompt.submitted` 가 **내가 낸 것인지 옆 창이 낸 것인지 못 가린다.**
 * 두 벌로 그리면 내 말이 화면에 두 번 뜨고, 신원으로 맞추려 들면 못 맞춘다.
 *
 * 그래서 **로그 하나만 그리고**, 이 클래스는 「보내고 아직 안 돌아온 것」만 든다.
 *
 * # 잠그되 안 지운다
 *
 * 낸 뒤 적은 글과 인용을 **쥔 채** 잠근다. 지우는 것은 로그의 메아리다. 안 그러면 제출이 문
 * 너머에서 떨어졌을 때 사람이 쓴 글이 조용히 사라진다 — 그리고 데몬은 물음에 막혀 있을 수
 * 있으므로(§5.7) 메아리가 **한참 뒤에 오거나 안 올 수도** 있다. 그래서 기다림에는 늘
 * 나가는 문이 있다(`release`): 잠금만 풀고 글은 그대로 둔다. 「갔는지 모른다」가 사실이라면
 * 화면도 그렇게 말해야 하고, 사람 글을 지우는 것은 그 말과 안 맞는다.
 */
export class Composer {
  constructor() {
    /** 아직 안 보낸 인용. */
    this.pending = [];
    /** 낸 뒤 메아리를 기다리는 것. `null` 이면 안 기다린다. */
    this.sent = null;
  }

  /** 인용을 담는다. 같은 도형을 두 번 담지 않는다. */
  attach(quote) {
    if (this.pending.some((q) => q.key === quote.key)) return false;
    this.pending.push(quote);
    return true;
  }

  detach(key) {
    const n = this.pending.length;
    this.pending = this.pending.filter((q) => q.key !== key);
    return this.pending.length !== n;
  }

  /** 보낼 것이 있는가 — 인용만 있고 말이 없어도 보낼 수 있다. 기다리는 중엔 못 보낸다. */
  canSend(text) {
    if (this.sent) return false;
    return String(text ?? '').trim().length > 0 || this.pending.length > 0;
  }

  /**
   * 낸다. **비우지 않는다.**
   *
   * `mark` 는 낼 때 로그에 이미 있던 사용자 줄 수다. 메아리를 **자리로** 맞추기 때문인데,
   * 신원으로는 못 맞춘다(위 주석). 그 대가도 적어 둔다: 같은 대화에 붙은 **다른 창이 먼저
   * 내면 그 줄이 내 메아리 자리에 앉는다.** 그러면 이 창은 한 줄 이르게 풀린다 — 잘못
   * 그리는 게 아니라 잠금이 일찍 풀리는 것이고, 로그는 그대로 맞다.
   */
  hold(text, mark = 0) {
    const t = String(text ?? '').trim();
    // **`prompt` 와 `mark` 만 싣는다.** 앞 판본은 `text` 와 `quotes` 도 같이 실었는데, 이
    // 저장소 어디에도 그 둘을 읽는 곳이 없다(필드 드롭 계측 — 통째로 비워도 아무 소리가 안
    // 났다). 위 주석이 말하는 「쥔 채 잠근다」를 지키는 것은 이 칸이 아니라 **`this.pending`
    // 과 화면의 입력칸**이다: `release()` 는 `sent` 만 비우고 `pending` 은 그대로 두고,
    // 사람이 적은 글은 애초에 DOM 에 산다. 읽는 이 없는 칸은 나중에 낡은 값을 담고 앉아
    // 있게 되고, 여기서는 특히 `quotes` 가 그렇다 — 낸 뒤 인용을 하나 떼면 `pending` 만
    // 줄고 이 사본은 안 줄어서, 둘 중 어느 쪽이 진짜인지가 그 순간부터 안 정해진다.
    this.sent = { prompt: promptOf(t, this.pending), mark };
    return this.sent;
  }

  /** 메아리가 왔다. 이제 비운다. */
  clear() {
    this.pending = [];
    this.sent = null;
  }

  /** 그만 기다린다. **안 비운다** — 갔는지 모르는 채로 사람 글을 지우면 안 된다. */
  release() { this.sent = null; }

  get waiting() { return this.sent !== null; }

  /** 이 줄 수면 메아리가 온 것인가. */
  echoed(userRows) { return this.sent != null && userRows > this.sent.mark; }
}

/**
 * 문에 실제로 나가는 글. **인용은 여기서 글로 접혀 들어간다.**
 *
 * 문의 `submit` 은 글 하나만 받는다 — 인용을 객체로 들고 갈 자리가 없다. 그래서 접는 자리가
 * 어디든 하나는 있어야 하고, 여기가 그 자리다. 인용을 먼저 두는 것은 지시가 무엇을 가리키는지
 * 모델이 **읽기 전에** 알게 하려는 것이다.
 */
export function promptOf(text, quotes) {
  const head = [...quotes].map((q) => q.toPrompt()).join('\n');
  return [head, text].filter((s) => s.length > 0).join('\n\n');
}
