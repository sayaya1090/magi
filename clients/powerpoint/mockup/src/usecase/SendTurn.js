/**
 * 사용자 차례를 낸다.
 *
 * **던지고 즉시 돌아온다.** 여기서 답을 기다리면 같은 헬퍼를 지나는 모델의 도구 호출과 서로
 * 기다리게 된다(§5.7). 답은 **다른 연결**인 전사 스트림으로 온다.
 *
 * 그래서 이 유스케이스가 정하는 것은 「보냈다」가 아니라 **「보낸 것을 언제 화면에서 지우는가」**
 * 다. 셋으로 갈린다:
 *
 * - **갔고 로그를 읽고 있다** — 잠그고 기다린다. 지우는 것은 메아리다(`settle`).
 * - **갔는데 로그를 못 읽는다**(스트림이 끊겼다) — 잠그지 않는다. 기다릴 메아리가 없으므로
 *   영영 안 풀릴 잠금이 된다. 대신 **갔는지 확인 못 한다고 말한다.** 글도 안 지운다.
 * - **못 갔다** — 잠금을 풀고 사유를 올린다. 삼키면 사람은 간 줄 안다.
 */
export class SendTurn {
  constructor(chat, composer) {
    this.chat = chat;
    this.composer = composer;
  }

  /**
   * `live` 에 **기본값을 안 준다.** 안 주면 없는 것이 `undefined` 라 아래 `!live` 가 잡아
   * 세 번째 갈래로 간다 — 「모른다」와 「끊겼다」가 같은 길로 가는 것이 맞기 때문이다. 한쪽만
   * 안전하지 않다: 모르는 채 잠그면 안 올 메아리를 영영 기다리고(위 두 번째 갈래가 있는 바로
   * 그 이유), 모르는 채 안 잠그면 「확인 못 한다」고 말하고 글을 남긴다. 뒤엣것은 사실이다.
   *
   * 예전엔 `live = true` 였다. 그건 **안 잰 것을 잰 것처럼 적는 것**이었다 — 안 넘긴 호출자에게
   * 「스트림이 살아 있다」를 대신 단언해 주고, 그 대가를 사람이 갇혀서 치른다. 오늘 프로덕션
   * 호출자는 하나뿐이고 늘 넘기므로 그 기본값은 아무도 안 쓰는 함정이었다.
   *
   * ⚠ **`userRows` 는 아직 기본값이 있고, 그건 같은 결함이다** — 미결로 적어 둔다. `live:true`
   * 인데 `userRows` 를 안 넘기면 `mark` 가 0 이라, 로그에 이미 있던 남의 줄 하나에 `echoed`
   * 가 참이 되어 **메아리가 오기 전에 사람 글이 지워진다.** `live` 쪽과 달리 안전한 기본값이
   * 없어서 안 고쳤다: 0 이면 이르게 지우고, `undefined` 면 `echoed` 가 영영 거짓이라 갇힌다.
   * 제대로 막으려면 값이 **필수**여야 하는데, 이 클래스는 거절을 던지지 않고 `{sent:false,
   * why}` 로 말하므로 새 `why` 가 하나 늘고 `View.onSend` 의 닫힌 집합도 같이 늘어야 한다
   * (거기 안 걸린 `why` 는 조용히 나간다 — 오늘 그런 것은 `empty` 하나뿐이고 빈 상자에는 할
   * 말이 없는 게 맞지만, 새 사유가 그 침묵을 물려받으면 안 된다). 오늘 호출자는 늘 넘긴다.
   *
   * @param {string} text
   * @param {{userRows:number, live:boolean}} log 지금 로그에서 보이는 것. `live` 는 필수다.
   * @returns {Promise<{sent:boolean, why?:string, blind?:boolean, error?:Error}>}
   */
  async run(text, { userRows = 0, live } = {}) {
    if (!this.composer.canSend(text)) {
      return { sent: false, why: this.composer.waiting ? 'waiting' : 'empty' };
    }
    const held = this.composer.hold(text, userRows);
    try {
      await this.chat.submit(held.prompt);
    } catch (e) {
      this.composer.release();
      return { sent: false, why: 'failed', error: e };
    }
    // 스트림이 죽어 있으면 메아리가 올 곳이 없다. 잠근 채 두면 사람이 갇힌다.
    if (!live) { this.composer.release(); return { sent: true, blind: true }; }
    return { sent: true, blind: false };
  }

  /** 로그가 여기까지 왔다. 메아리면 컴포저를 비운다. @returns 비웠는가 */
  settle(userRows) {
    if (!this.composer.echoed(userRows)) return false;
    this.composer.clear();
    return true;
  }
}

/**
 * 전사 뷰를 위 두 메서드가 먹는 모양으로 옮겨 싣는다.
 *
 * **화면에서 여기로 내렸다.** 위 갈래 셋은 전부 이 두 칸에서 갈리는데, 이 칸들을 **채우는
 * 쪽**은 화면 안에 있어서 한 번도 안 돌아 봤다 — 소비자는 손으로 지은 객체로 샅샅이 재고
 * 생산자는 아무도 안 재는, `OfficeDeck.selection` 에서 본 것과 같은 모양이다. 옮겨 싣는
 * 자리가 틀리면 위 주석이 지키려는 것이 통째로 무너지는데도.
 *
 * 두 칸이 각각 무엇을 지키는지:
 * - `userRows` 는 **사람 줄만** 센다. 모든 줄을 세면 모델이 한 마디 하는 순간 수가 늘어
 *   `settle` 이 메아리로 읽고, 아직 안 돌아온 사람 글을 지운다(`Composer.echoed`).
 * - `live` 는 **그대로 나른다.** 없는 것을 `true` 로 채우면 안 올 메아리를 기다리며 잠기고,
 *   그 함정은 위 `run` 주석이 이미 한 번 걷어 낸 것이다.
 *
 * @param {{rows:Array, live:boolean}|null|undefined} view `ReadTranscript.view`
 */
export function logShapeOf(view) {
  // 읽는 유스케이스가 아예 없는 판(문에 안 붙었다). **읽는 중이 아니다** — 눈감고 보내는
  // 쪽으로 가야 사람이 안 갇힌다.
  if (!view) return { userRows: 0, live: false };
  return { userRows: view.rows.filter((r) => r.kind === 'user').length, live: view.live };
}
