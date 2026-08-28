import { TranscriptPort } from '../port/TranscriptPort.js';

/**
 * 문의 `transcript` 스트림을 흉내 낸다. **시험이 프레임을 손으로 밀어 넣는다** — 타이머로
 * 흘리면 시험이 시계를 재게 되고, 여기서 재려는 것은 시간이 아니라 순서와 상태다.
 *
 * 코어 계약을 그대로 흉내 내는 부분:
 * - `since <= 0` 이면 전부 보낸다.
 * - 로그 끝을 넘은 `since` 는 **거절하고 사유 프레임을 먼저 보낸 뒤** 처음부터 보낸다
 *   (`answerable`). 범위 안에 떨어지는 낡은 커서는 **안 잡는다** — 서버도 못 잡는다.
 * - 끊김에 에러가 없다.
 */
export class FakeTranscript extends TranscriptPort {
  constructor(logs = {}) {
    super();
    /** sessionId → 이벤트 배열(seq 오름차순) */
    this.logs = logs;
    this.calls = [];      // 시험이 보는 것: 실제로 보낸 since
    this._handlers = null;
    this._session = null;
  }

  get label() { return '가짜 대화 — 문에 안 붙었다'; }

  subscribe(sessionId, since, handlers) {
    this.calls.push({ sessionId, since });
    this._handlers = handlers;
    this._session = sessionId;
    const log = this.logs[sessionId] ?? [];
    const latest = log.length ? log[log.length - 1].seq : 0;

    let from = since;
    if (since > 0 && since > latest) {
      // 서버가 소리 내어 거절한다. **이벤트보다 먼저** 온다.
      handlers.onRestart(
        `since ${since} is past the end of this session's log, which ends at ${latest}`);
      from = 0;
    }
    for (const ev of log) {
      if (from > 0 && ev.seq <= from) continue;
      handlers.onEvent(ev);
    }
    return () => { this._handlers = null; };
  }

  /** 라이브 이벤트 하나. */
  push(ev) {
    (this.logs[this._session] ??= []).push(ev);
    this._handlers?.onEvent(ev);
  }

  /** 스트림이 끊겼다. **에러가 아니다** — 문이 그렇게 적어 뒀다. */
  drop() { this._handlers?.onEnd(); this._handlers = null; }
}
