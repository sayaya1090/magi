import { StatusPort } from '../port/StatusPort.js';

/**
 * `status` 문을 흉내 낸다. 시험이 답을 손으로 정한다 — 여기서 재는 것은 폴 간격이 아니라
 * **상태 전이**이므로 타이머를 안 쓴다.
 */
export class FakeStatus extends StatusPort {
  constructor() {
    super();
    this.reachable = true;
    this.pending = null;
    this.doing = '';
    this.answers = [];   // 시험이 보는 것: 실제로 보낸 (callId, decision)
    this.throwOnStatus = false;
  }

  async status() {
    if (this.throwOnStatus) throw new Error('dial 실패');
    return { reachable: this.reachable, pending: this.pending, doing: this.doing };
  }

  async answerPermission(callId, decision) {
    this.answers.push({ callId, decision });
    // 진짜 데몬도 답을 받았다고 해서 그 자리에서 물음을 내리지 않는다 — 내려가는 것은
    // 다음 `status`가 말한다. 그래서 여기서도 안 내린다. 시험이 `clear()`로 내린다.
  }

  ask(p) { this.pending = p; }
  clear() { this.pending = null; }
}
