import { Cursor } from '../domain/Cursor.js';
import { Transcript } from '../domain/Transcript.js';

/**
 * 대화를 읽어 화면 상태로 만든다. **커서를 드는 자리이기도 하다**
 * (clients/powerpoint/DESIGN.md §5.7).
 *
 * 여기서 정하는 것 셋:
 *
 * - **대화가 바뀌면 커서를 버린다.** 서버는 이걸 못 잡아 준다 — 어제 대화의 seq 40 은 오늘
 *   대화의 진짜 위치라서 로그 끝 검사를 그냥 통과한다(`answerable` 이 그 한계를 스스로 적어
 *   뒀다). 그러니 이건 예의가 아니라 **문이 못 해 주겠다고 한 자리를 우리가 메우는 것**이다.
 * - **거절 프레임을 받으면 화면을 비우고 다시 쌓는다.** 안 비우면 보고 있던 대화 뒤에 같은
 *   대화의 처음이 이어 붙는다. 작업창은 PowerPoint 를 껐다 켤 때마다 새로 붙으니 **이 프레임을
 *   제일 자주 받는 쪽이 우리다.**
 * - **끊기면 끊겼다고 화면에 남긴다.** 조용한 대화와 죽은 스트림은 화면에서 똑같이 생겼고,
 *   문은 깨끗한 끝을 에러로 안 준다. 그 구분을 값에 실어 두지 않으면 사용자는 답을 영원히
 *   기다린다.
 */
export class ReadTranscript {
  constructor(port, { onChange } = {}) {
    this.port = port;
    this.onChange = onChange ?? (() => {});
    this.cursor = Cursor.empty();
    this.transcript = new Transcript();
    this.sessionId = null;
    this._detach = null;
  }

  /**
   * 이 대화에 붙는다. 이미 다른 대화에 붙어 있었으면 **끊고 화면을 비운다.**
   *
   * 돌려주는 `since` 는 실제로 보낸 값이다 — 시험이 그걸 봐야 "커서를 버렸는가"를 잰다.
   */
  attach(sessionId) {
    if (this._detach) { this._detach(); this._detach = null; }
    if (this.sessionId !== null && this.sessionId !== sessionId) {
      this.transcript.switchTo();
      this.cursor = this.cursor.reset();
    }
    this.sessionId = sessionId;

    const since = this.cursor.sinceFor(sessionId);
    this.transcript.live = true;
    this._detach = this.port.subscribe(sessionId, since, {
      onRestart: (why) => {
        // 서버가 커서를 안 받았다. 뒤따라올 것은 **이 대화의 처음부터**다.
        this.transcript.restart(why);
        this.cursor = this.cursor.reset();
        this.onChange();
      },
      onEvent: (ev) => {
        // 남의 대화 이벤트가 이 연결로 올 리는 없지만, 왔다면 그건 화면 문제가 아니라
        // 신원 문제다. 조용히 섞지 않는다.
        if (ev?.sessionId && ev.sessionId !== this.sessionId) return;
        this.transcript.append(ev);
        this.cursor = this.cursor.advanced(this.sessionId, ev?.seq);
        this.onChange();
      },
      onEnd: () => {
        this.transcript.live = false;
        this.onChange();
      },
    });
    // **붙었다는 것도 사건이다.** 로그가 빈 대화에서는 첫 이벤트가 영영 안 올 수 있고, 그동안
    // 화면에는 붙기 전에 그린 「스트림이 끊겼습니다」가 그대로 서 있다. `detach` 는 알리는데
    // `attach` 는 안 알리던 비대칭이 그 거짓말의 자리였다.
    this.onChange();
    return since;
  }

  detach() {
    if (this._detach) { this._detach(); this._detach = null; }
    this.transcript.live = false;
    this.onChange();
  }

  /** 화면이 읽는 것. */
  get view() {
    return {
      live: this.transcript.live,
      refusal: this.transcript.refusal,
      rows: this.transcript.drawnRows,
      unknownNote: this.transcript.unknownNote,
      // **커서는 안 싣는다.** 실어 뒀지만 화면도 시험도 이 칸을 한 번도 안 읽었다(필드 드롭
      // 계측 — 통째로 비워도 아무 소리가 안 났다). 읽는 이 없는 칸은 나중에 낡은 값을 담고
      // 앉아 있게 되고, 커서가 필요한 쪽은 유스케이스의 `this.cursor` 를 그대로 본다.
    };
  }
}
