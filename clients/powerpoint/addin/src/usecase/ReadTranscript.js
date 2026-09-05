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
        // **대화가 옮겨 갔으면 따라간다.**
        //
        // 「새 대화 시작」은 세션을 새로 만들고 `session.moved` 하나를 남긴다. 앞 판본은 그
        // 이벤트를 모르는 것으로 흘려보냈고, 그 뒤로 오는 것은 전부 **다른 sessionId** 라
        // 바로 아래 걸름망에 걸려 사라졌다 — 창은 「대화 스트림이 끊겼습니다」를 띄운 채
        // 영영 아무것도 안 그렸다. 실물에서 그 화면을 봤다(2026-09-03): 모델은 그동안
        // 슬라이드 일곱 장을 만들고 있었는데 사람은 빈 창을 보고 있었다.
        //
        // 「이 창이 아직 그릴 줄 모르는 이벤트」라고 적어 두는 것으로는 부족했다. 모르는
        // 이벤트가 **뒤따르는 모든 것을 못 보게 만드는** 종류일 수 있고, 이게 그랬다.
        const moved = ev?.type === 'session.moved' ? String(ev?.data?.to ?? '') : '';
        if (moved && moved !== this.sessionId) {
          this.attach(moved);
          this.onChange();
          return;
        }
        // 남의 대화 이벤트가 이 연결로 올 리는 없지만, 왔다면 그건 화면 문제가 아니라
        // 신원 문제다. 조용히 섞지 않는다.
        if (ev?.sessionId && ev.sessionId !== this.sessionId) return;
        this.transcript.append(ev);
        this.cursor = this.cursor.advanced(this.sessionId, ev?.seq);
        this.onChange();
      },
      // 끝난 이유가 둘이다: 스트림이 죽었거나, 아직 아무 요청도 안 보내 대화가 비어 있거나
      // (코어는 첫 말이 올 때 대화를 낳는다). 화면이 둘을 다르게 적어야 한다(사용자 지적 2026-09-05).
      onEnd: (empty) => {
        this.transcript.live = false;
        this.transcript.empty = empty === true;
        this.onChange();
      },
      /**
       * **돌아온 것도 사건이다.** 끊김만 값에 실으면 화면은 한 번 끊긴 뒤로 영영 끊긴 채다 —
       * 이 창은 붙기 전에 스트림을 열고 컴패니언을 나중에 고르므로, **정상 흐름에서** 죽은
       * 스트림으로 시작해서 살아난다. 실물에서 그 화면을 봤다(2026-09-01): 헬퍼는 `live:true`
       * 를 보내고 있는데 창은 「대화 스트림이 끊겼습니다」를 띄운 채였다.
       *
       * 같은 비대칭을 이 파일이 이미 한 번 고쳤다(바로 아래 `attach` 의 알림). 그때는 붙는
       * 것이었고 이번엔 되살아나는 것이다.
       */
      onLive: () => {
        this.transcript.live = true;
        this.transcript.empty = false;
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
      empty: this.transcript.empty,
      refusal: this.transcript.refusal,
      rows: this.transcript.drawnRows,
      unknownNote: this.transcript.unknownNote,
      // **모르는 것과 안 그리기로 한 것은 다른 칸이다.** 한 줄로 합치면 「고칠 것이 있다」와
      // 「이대로가 맞다」가 같은 문장이 되고, 그 줄은 곧 아무도 안 읽는다.
      skippedNote: this.transcript.skippedNote,
      // 계획. 대화 줄이 아니라 **판**이라 따로 나른다(`Transcript` 의 `PANEL`).
      todos: this.transcript.todos,
      // **커서는 안 싣는다.** 실어 뒀지만 화면도 시험도 이 칸을 한 번도 안 읽었다(필드 드롭
      // 계측 — 통째로 비워도 아무 소리가 안 났다). 읽는 이 없는 칸은 나중에 낡은 값을 담고
      // 앉아 있게 되고, 커서가 필요한 쪽은 유스케이스의 `this.cursor` 를 그대로 본다.
    };
  }
}
