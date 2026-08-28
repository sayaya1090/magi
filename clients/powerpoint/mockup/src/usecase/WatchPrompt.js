import { Pending, CLEARED, KINDS } from '../domain/Pending.js';

/**
 * 데몬이 무엇을 묻고 있는지 화면에 세워 두고, 답을 보낸다.
 *
 * `ReadTranscript`와 **따로 도는 이유**가 계약이다 — 물음은 로그에 없으므로 스트림으로 안 온다.
 * 둘은 같은 화면의 두 영역이지 한 흐름이 아니다(clients/powerpoint/DESIGN.md §5.7).
 *
 * 여기서 정하는 것 넷:
 *
 * - **같은 물음을 다시 그리지 않는다.** 폴링이 같은 것을 계속 실어 온다. 매번 새로 그리면
 *   사용자가 고르던 것이 지워지고, 스크린 리더는 대기가 이어지는 내내 같은 말을 되풀이한다.
 * - **사라지면 사유를 실어 내린다.** 남이 답했을 수도, 정책이 답했을 수도, 데몬이 죽었을 수도
 *   있다. 「없다」만 남기면 셋이 화면에서 똑같이 생긴다.
 * - **무엇으로 답했는지는 안 찍는다.** 코어가 `elsewhere`를 싣는 이유와 같다.
 * - **못 닿음은 한 번만 말한다.** 매 폴마다 말하면 그 말이 배경이 되고, 배경이 된 말은 없는
 *   말과 같다(코어의 폴 루프도 `lost` 플래그로 같은 일을 한다).
 */
export class WatchPrompt {
  constructor(port, { onChange } = {}) {
    this.port = port;
    this.onChange = onChange ?? (() => {});
    this.pending = null;
    /** 물음이 내려간 사유. 내려간 **그 순간에만** 실린다. */
    this.clearedBy = null;
    this.reachable = true;
    /** 못 닿는다고 이미 말했나. 값이 아니라 **말했는지**를 기억한다. */
    this.saidLost = false;
    /**
     * 데몬이 뭘 하는 중인지. 바뀔 때만 화면에 올린다.
     *
     * **이건 「지금」에 대한 말이라 못 닿는 순간 근거가 없어진다.** 로그 줄은 지나간 일이라
     * 못 닿아도 그대로 참이지만, 「…하는 중」은 status 가 방금 그렇다고 해 줘야만 참이다.
     * 그래서 못 닿는 동안 이 값을 그대로 두면 죽은 데몬이 영영 일하는 중으로 서 있는다 —
     * 조건이 사라졌는데 보고가 살아남는 그 모양이다. 지우지도 않는다(마지막으로 뭘 하다
     * 놓쳤는지가 사람에게 필요한 정보다). **대신 지금 읽은 것인지를 값에 같이 싣는다**
     * (`view.doingFresh`).
     */
    this.doing = '';
    /**
     * 답을 이미 보낸 물음의 id. **보냈다고 물음이 내려가지는 않으므로** 화면에는 단추가
     * 그대로 서 있고, 두 번 누르면 같은 call id 로 답이 두 번 간다. 둘째는 코어가 거절한다
     * (등록부에서 이미 지워졌다) — 문제는 그 거절이 *"이미 결정됐거나 만료됐다"*로 와서
     * **아무 잘못도 안 한 사람에게 오류로 뜬다**는 것이다. 그러니 아예 안 보낸다.
     */
    this.sentFor = null;
  }

  /** 폴 한 번. 시험이 손으로 돌린다 — 여기서 재는 것은 시간이 아니라 상태 전이다. */
  async poll() {
    let s;
    try {
      s = await this.port.status();
    } catch {
      // 문이 안 열린 것도 못 닿은 것이다. 예외를 삼키되 **사실은 남긴다.**
      s = { reachable: false, pending: null, doing: '' };
    }
    const wasReachable = this.reachable;
    this.reachable = s.reachable !== false;
    if (this.reachable) this.saidLost = false;

    if (!this.reachable) {
      // 물음이 끝난 것이 아니라 **모르게 된 것**이다. 세워 둔 것을 내리되 사유를 그렇게 적는다.
      if (this.pending) {
        this.pending = null; this.clearedBy = CLEARED.unreachable; this.sentFor = null;
      }
      const firstTime = wasReachable || !this.saidLost;
      this.saidLost = true;
      if (firstTime) this.onChange();
      return this.view;
    }

    // 문이 다시 열린 것 자체가 바꿔 그려야 할 사실이다. 조용한 데몬에 다시 붙으면 아래
    // 어느 분기도 안 타는데(물음도 없고 하는 일도 그대로), 그러면 「안 닿습니다」가 **닿는
    // 동안 계속 서 있는다** — 화면이 아는 것보다 낡은 것을 사실로 말하는 그 모양이다.
    if (!wasReachable) this.onChange();

    const next = s.pending ? new Pending(s.pending) : null;
    if (next && this.pending?.same(next)) {
      // 같은 물음이다. **판은 다시 안 세운다** — 세우면 고르던 것과 적던 답이 매 폴마다
      // 지워진다. 그렇다고 옛 값을 계속 쥐면 안 된다: 뒤에 쌓인 물음의 수는 **같은 물음을
      // 보는 동안에도** 늘고, 그러면 「모두 2개」가 3개가 돼도 영영 2개로 선다. 신원이 같다는
      // 말과 보여 줄 것이 같다는 말은 다른 말인데, 이 자리는 앞엣말로 뒤엣말을 대신했었다.
      //
      // 그래서 값은 **늘** 새것을 쥐고, 종은 **보일 것이 달라졌을 때만** 친다. 매 폴마다
      // 치면 판을 다시 세우는 것과 같아지고, 아예 안 치면 새로 쥔 값을 아무도 안 읽는다.
      // 「보일 것」을 `placement` 하나로 재는 것은 지금 화면이 이 문 안에서 그 줄만 갈아
      // 끼우기 때문이다(`view.refreshPlace`). **갈아 끼우는 것이 늘면 여기도 같이 는다.**
      const was = this.pending.placement;
      this.pending = next;
      if (next.placement !== was) this.onChange();
    } else if (next) {
      this.pending = next;
      this.clearedBy = null;
      this.sentFor = null;
      this.onChange();
    } else if (this.pending) {
      // 답한 것이 우리면 `answer()`가 이미 사유를 적어 뒀다. 아니면 남이 답한 것이다.
      this.pending = null;
      this.clearedBy ??= CLEARED.elsewhere;
      this.sentFor = null;
      this.onChange();
    }

    if ((s.doing ?? '') !== this.doing) {
      this.doing = s.doing ?? '';
      this.onChange();
    }
    return this.view;
  }

  /**
   * 답을 보낸다. **보내는 것과 내려가는 것은 다른 일이다** — 내려가는 것은 다음 `status`가
   * 말해 준다. 그래서 여기서는 사유만 미리 적어 두고 세운 것을 직접 안 내린다. 직접 내리면
   * 답이 실패했는데도 화면에서 물음이 사라진다.
   */
  async answer(decision) {
    const p = this.answering(KINDS.permission);
    await this.port.answerPermission(p.id, decision);
    this.sentFor = p.id;
    this.clearedBy = CLEARED.answered;
    this.onChange();
    return p.id;
  }

  /**
   * 질문의 답. **권한과 손이 다른 이유**는 답의 모양이 다르기 때문이다 — 권한은 정해진 낱말
   * 넷이고 질문은 사람이 고른 글이다. 종류를 안 보고 한 손으로 보내면 질문에 `allow`가 간다.
   */
  async choose(text) {
    const p = this.answering(KINDS.question);
    await this.port.answerQuestion(p.id, text);
    this.sentFor = p.id;
    this.clearedBy = CLEARED.answered;
    this.onChange();
    return p.id;
  }

  /**
   * 답을 보내도 되는 상태인지 본다. **종류가 다르면 거절한다.**
   *
   * 코어까지 가면 어차피 떨어진다 — 등록부가 종류별로 갈려 있어(`st.questions` / `st.perms`)
   * 어긋난 답은 채널을 못 찾는다. 여기서 먼저 막는 이유는 통과할까 봐가 아니라 **코어가 대는
   * 사유가 틀리기 때문**이다: *"이미 결정됐거나 만료됐다"*. 그 말을 받은 사람은 없던 경합을
   * 찾아 나서고, 물음은 그대로 서 있다. 사유를 아는 자리에서 사유를 대는 편이 낫다.
   */
  answering(kind) {
    if (!this.pending) throw new Error('묻고 있는 것이 없는데 답을 보내려 했습니다');
    // 종류를 먼저 본다. 「이미 보냈다」는 사람이 두 번 누른 흔한 일이고 종류 어긋남은 **이
    // 코드의 결함**이라, 둘이 겹치면 결함 쪽을 말해야 한다. 뒤에 두면 결함이 흔한 일에 가린다.
    if (this.pending.kind !== kind) {
      const got = this.pending.kind || '(없음)';
      throw new Error(`${kind} 이 아닌 물음에 ${kind} 의 답을 보내려 했습니다: kind=${got}`);
    }
    if (this.sentFor === this.pending.id) {
      throw new Error('이미 답을 보냈습니다 — 물음이 내려가기를 기다리는 중입니다');
    }
    return this.pending;
  }

  get view() {
    return {
      pending: this.pending,
      clearedBy: this.clearedBy,
      reachable: this.reachable,
      /** 못 닿는다는 말을 지금 화면에 올릴 것인가. **한 번뿐**이다. */
      lostNote: this.reachable
        ? null
        : '데몬에 안 닿습니다 — 이 화면이 보여 주는 것은 마지막으로 읽은 것입니다',
      doing: this.doing,
      /**
       * 그 「…하는 중」을 **지금** 읽었는가. 거짓이면 못 닿는 동안 들고 있는 마지막 읽기라,
       * 화면은 현재형으로 적으면 안 된다. 값과 그 값이 아직 유효한지를 같이 실어야 버릴지
       * 말지를 읽는 쪽이 **고를 수 있다** — 여기서 미리 지우면 그 선택이 사라진다.
       */
      doingFresh: this.reachable,
      /** 이 물음에 답을 이미 보냈나. 참이면 단추를 잠근다 — 값이 아니라 **보냈는지**다. */
      answered: this.pending != null && this.sentFor === this.pending.id,
      /**
       * 이 창이 모르는 종류가 대기 중이다. **단추는 안 주고 사실만 준다** — 넘겨짚어 그리면
       * 사람이 엉뚱한 답을 보내고, 안 그리면 §6이 말한 「아무도 안 보는 곳에서 대기」다.
       */
      unknownKindNote: this.pending && !this.pending.known
        ? '이 창이 모르는 종류의 물음이 대기 중입니다'
          + `(kind=${this.pending.kind || '(없음)'}, id=${this.pending.id})`
          + ' — 답할 수 있는 창에서 답해 주십시오.'
        : null,
    };
  }
}
