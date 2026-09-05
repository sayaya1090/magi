import { ChatPort } from '../port/ChatPort.js';
import { StatusPort } from '../port/StatusPort.js';
import { TranscriptPort } from '../port/TranscriptPort.js';
import { Pending } from '../domain/Pending.js';

/**
 * 진짜 문에 붙는 어댑터 셋. 가짜들과 **같은 포트**를 만족하므로 유스케이스와 화면은
 * 안 바뀐다 — 목업이 층을 나눈 값이 여기서 나온다(clients/powerpoint/DESIGN.md §5.7).
 */

/** 내는 쪽. **던지고 즉시 돌아온다** — 답은 이 포트로 안 온다(§5.7). */
export class HelperChat extends ChatPort {
  constructor(api) { super(); this.api = api; }
  async submit(prompt) { await this.api.submit(prompt); }
}

/**
 * 물음을 폴하는 쪽.
 *
 * **스트림이 아니라 폴인 것이 계약이다**(§5.7): 물음은 로그에 안 쌓이고 막힌 데몬의 버스에만
 * 나가므로 대화 스트림으로는 영영 안 온다. 어태치 TUI 도 웹 콘솔도 같은 우회를 한다.
 */
export class HelperStatus extends StatusPort {
  constructor(api) { super(); this.api = api; }

  async status() {
    const st = await this.api.status();
    // **못 닿은 것과 「묻는 게 없다」를 안 뭉갠다**(§5.7). 헬퍼가 그 둘을 갈라 실어 보낸다.
    return {
      reachable: st?.reachable === true,
      doing: st?.doing ?? '',
      why: st?.why ?? '',
      streamLive: st?.streamLive === true,
      // **붙어 있던 컴패니언이 다시 뜬 것과 「닿는다」를 안 뭉갠다.** 소켓 경로는 그대로라
      // dial 은 성공하는데, 우리 등록도 이 창이 든 대화 이름도 남의 생애의 것이다.
      stale: st?.stale === true,
      answered: st?.answered ?? null,
      pending: st?.asking ? pendingOf(st.asking) : null,
    };
  }

  async answerPermission(callId, decision) { await this.api.permission(callId, decision); }
  async answerQuestion(callId, text) { await this.api.question(callId, text); }
}

/**
 * 데몬의 `Waiting` 을 이 창의 값으로. **`kind` 에 기본값을 안 준다** — 모르는 종류를
 * 권한 확인 요청으로 넘겨짚으면 사람이 누른 답이 그 종류가 기다리는 답이 아니고, 거절은
 * **틀린 사유**로 온다("이미 결정됐거나 만료됐다"). 코어의 `default:` 가 그 실물이다(§5.7).
 */
export function pendingOf(w) {
  return new Pending({
    id: w.id ?? w.ID ?? '',
    kind: w.kind ?? w.Kind ?? '',
    what: w.what ?? w.What ?? '',
    args: w.args ?? w.Args ?? null,
    reason: w.reason ?? w.Reason ?? '',
    options: w.options ?? w.Options ?? [],
    report: w.report ?? w.Report ?? [],
    index: w.index ?? w.Index ?? 0,
    total: w.total ?? w.Total ?? 0,
    since: w.since ?? w.Since ?? null,
  });
}

/**
 * 대화가 흘러 들어오는 쪽.
 *
 * # 커서는 여기 없다 — 헬퍼가 든다
 *
 * 포트의 `since` 인자를 이 어댑터는 **안 쓴다.** 소켓을 쥐고 있는 것이 헬퍼이고, 문이 커서를
 * 검사해 거절할 때 오는 사유 프레임도 헬퍼가 먼저 받는다(§5.7). 여기서 두 번째 커서를 들면
 * **한 사실에 두 자리**가 생기고, 둘이 어긋나는 날 화면이 대화를 두 벌 그린다 —
 * `docs/ARCHITECTURE.md` §3 의 「한 사실에 한 자리」가 이 자리에서 걸린다.
 *
 * 그래서 이 클래스가 지는 것은 나르는 것뿐이다: `restart` 는 사유 그대로, `event` 는 그대로,
 * 그리고 **끊김은 에러가 아니다**(문이 그렇게 적어 뒀다).
 */
export class HelperTranscript extends TranscriptPort {
  constructor(stream) { super(); this.stream = stream; this.off = []; }

  get label() { return '헬퍼가 중계하는 대화'; }

  subscribe(_sessionId, _since, { onRestart, onEvent, onEnd, onLive }) {
    const offs = [
      this.stream.on('restart', (d) => onRestart?.(d?.why ?? '이어 읽기 위치가 거절됐습니다')),
      this.stream.on('event', (ev) => onEvent?.(ev)),
      // **양쪽을 다 알린다.** 죽음만 알리면 화면은 한 번 끊긴 뒤로 영영 끊긴 채이고, 이 창은
      // 스트림을 먼저 열고 컴패니언을 나중에 고르므로 **정상 흐름이 죽은 채로 시작한다.**
      // 실물에서 그 화면을 봤다(2026-09-01): 헬퍼는 `live:true` 를 보내는데 창은 「끊겼습니다」
      // 를 띄우고 있었다. **비대칭 통지는 거짓말 생성기다.**
      this.stream.on('stream', (d) => {
        if (d?.live === false) onEnd?.(d?.empty === true);
        else if (d?.live === true) onLive?.();
      }),
    ];
    return () => { for (const off of offs) off(); };
  }
}
