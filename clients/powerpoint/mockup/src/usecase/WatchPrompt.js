import { Pending, CLEARED } from '../domain/Pending.js';

/**
 * 데몬이 무엇을 묻고 있는지 화면에 세워 두고, 답을 보낸다.
 *
 * `ReadTranscript`와 **따로 도는 이유**가 계약이다 — 물음은 로그에 없으므로 스트림으로 안 온다.
 * 둘은 같은 화면의 두 영역이지 한 흐름이 아니다(DESIGN.md §5.7).
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
    /** 데몬이 지금 뭘 하는 중인지. 바뀔 때만 화면에 올린다. */
    this.doing = '';
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
      if (this.pending) { this.pending = null; this.clearedBy = CLEARED.unreachable; }
      const firstTime = wasReachable || !this.saidLost;
      this.saidLost = true;
      if (firstTime) this.onChange();
      return this.view;
    }

    const next = s.pending ? new Pending(s.pending) : null;
    if (next && this.pending?.same(next)) {
      // 같은 물음이다. 아무것도 안 한다 — 이 줄이 없으면 고르던 것이 매 폴마다 지워진다.
    } else if (next) {
      this.pending = next;
      this.clearedBy = null;
      this.onChange();
    } else if (this.pending) {
      // 답한 것이 우리면 `answer()`가 이미 사유를 적어 뒀다. 아니면 남이 답한 것이다.
      this.pending = null;
      this.clearedBy ??= CLEARED.elsewhere;
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
    if (!this.pending) throw new Error('묻고 있는 것이 없는데 답을 보내려 했습니다');
    const id = this.pending.id;
    await this.port.answerPermission(id, decision);
    this.clearedBy = CLEARED.answered;
    return id;
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
    };
  }
}
