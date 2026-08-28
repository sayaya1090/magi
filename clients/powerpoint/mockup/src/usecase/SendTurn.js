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
   * @param {string} text
   * @param {{userRows:number, live:boolean}} log 지금 로그에서 보이는 것
   * @returns {Promise<{sent:boolean, why?:string, blind?:boolean, error?:Error}>}
   */
  async run(text, { userRows = 0, live = true } = {}) {
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
