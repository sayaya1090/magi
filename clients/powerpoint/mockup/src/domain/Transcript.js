/**
 * 화면에 보이는 대화. 문이 흘리는 **로그 자신의 이벤트**를 접어서 줄로 만든다.
 *
 * 문은 렌더를 안 준다 — `Response.Event` 가 `internal/core/event/event.go` 의 `Event` 를
 * 통째로, 이름도 안 바꾸고 싣는다. 코어가 그렇게 고른 이유도 적어 뒀다: 렌더만 받은 쪽은
 * 콘솔이 하는 일을 못 하고, 한 스트림을 두 가지로 적으면 한쪽을 고칠 때 갈라진다.
 * **그래서 그리는 것이 읽는 쪽 일이고, 이 파일이 그 자리다**(DESIGN.md §5.7).
 *
 * # 못 박는 것 셋
 *
 * **하나 — 모르는 종류를 버리지 않는다.** 종류가 27가지인데 채팅창이 그릴 줄 아는 것은 몇 개뿐
 * 이다. 나머지를 조용히 버리면 화면은 **아무 일도 안 일어난 것처럼 보인다** — §5.7 이 피하려는
 * 「아무도 안 보는 곳에서 대기」와 같은 모양이다. 안 그리는 것과 없는 것은 다르다.
 *
 * **둘 — `prompt.submitted` 를 배우 안 보고 그리면 안 된다.** 정책이 대신 내린 기본값도
 * (`noteUnanswered`) 그 종류로 오는데 배우가 `system` 이다. 다 말풍선으로 그리면 **정책이 한
 * 일이 사용자가 한 말로 붙고**, 반대로 사람 아닌 배우를 버리면 아무것도 안 보인다 — 뒤엣것은
 * TUI 가 실제로 겪은 결함이다. 그래서 `⟳ … note:` 정보 줄로 그린다(§5.7).
 *
 * **셋 — `part.delta` 와 `part.appended` 는 같은 말 두 번이다.** 델타는 도는 중에 조각으로
 * 오고(버스 전용), `appended` 는 끝난 뒤 통째로 온다(로그). `messageId` 가 같다. 둘 다 새 줄로
 * 쌓으면 모델의 답이 화면에 두 번 뜬다. 그래서 **같은 messageId 는 한 줄**이고, `appended` 가
 * 오면 그 줄을 **덮어쓴다** — 붙어 있던 창과 나중에 붙은 창이 같은 화면을 보게 된다.
 */

/** 채팅창이 실제로 그릴 줄 아는 종류. 나머지는 버리지 않고 `unknown` 으로 남는다. */
const DRAWN = new Map([
  ['prompt.submitted', 'user'],
  ['part.appended', 'model'],
  ['part.delta', 'model'],
  ['turn.finished', 'turn'],
  ['error', 'error'],
]);

export class Row {
  constructor({ seq, kind, type, text, actor, ts, messageId }) {
    this.seq = seq ?? 0;
    this.kind = kind;       // user | model | note | turn | error | unknown
    this.type = type;       // 로그의 이름 그대로
    this.text = text ?? '';
    this.actor = actor ?? null;
    this.ts = ts ?? null;
    this.messageId = messageId ?? null;
  }
  get drawn() { return this.kind !== 'unknown'; }
  /** 로그에 자리가 있는가. 버스 전용 이벤트는 `seq == 0` 이라 자리가 없다. */
  get positioned() { return this.seq > 0; }
}

export class Transcript {
  constructor() {
    this.rows = [];
    /** 그릴 줄 몰라 남겨 둔 종류와 그 수. 화면 아래 한 줄로 정직하게 적는다. */
    this.unknownCounts = new Map();
    /** 스트림이 살아 있다고 **믿는가**. 끊김은 에러로 안 오므로 이 값은 밖에서 꺼 준다. */
    this.live = false;
    /** 서버가 커서를 거절하며 한 말. 있으면 화면이 그대로 보여 준다. */
    this.refusal = null;
  }

  /** 서버가 「전부 다시 보낸다」고 했다. **가진 것을 버린다** — 안 버리면 앞뒤가 이어 붙는다. */
  restart(why) {
    this.rows = [];
    this.unknownCounts = new Map();
    this.refusal = why ?? null;
    return this;
  }

  /** 대화 자체가 바뀌었다. 거절과 달리 사유가 서버 말이 아니라 우리 판단이다. */
  switchTo() {
    this.rows = [];
    this.unknownCounts = new Map();
    this.refusal = null;
    return this;
  }

  append(ev) {
    const type = String(ev?.type ?? '');
    const kind = kindOf(type, ev?.actor);
    if (kind === 'unknown') {
      this.unknownCounts.set(type, (this.unknownCounts.get(type) ?? 0) + 1);
    }
    const messageId = ev?.data?.messageId ?? null;

    // 델타와 완성본은 같은 말이다. 같은 messageId 의 모델 줄이 이미 있으면 거기 접는다.
    if (kind === 'model' && messageId) {
      const at = this.rows.findIndex((r) => r.kind === 'model' && r.messageId === messageId);
      if (at >= 0) {
        const row = this.rows[at];
        // 완성본은 통째라 덮어쓰고, 델타는 조각이라 잇는다.
        row.text = type === 'part.appended' ? textOf(ev, kind) : row.text + textOf(ev, kind);
        row.type = type;
        if (ev?.seq > 0) row.seq = ev.seq;   // 자리가 생겼다
        return row;
      }
    }

    const row = new Row({
      seq: ev?.seq, kind, type, actor: ev?.actor, ts: ev?.ts, messageId,
      text: textOf(ev, kind),
    });
    this.rows.push(row);
    return row;
  }

  get drawnRows() { return this.rows.filter((r) => r.drawn); }

  /**
   * 화면 아래에 적을 한 줄. **없으면 `null`** — "0건"을 굳이 적으면 사람이 그 줄을 무시하게 되고,
   * 무시하게 된 줄은 없는 줄과 같다.
   */
  get unknownNote() {
    if (this.unknownCounts.size === 0) return null;
    const n = [...this.unknownCounts.values()].reduce((a, b) => a + b, 0);
    const kinds = [...this.unknownCounts.keys()].sort().join(', ');
    return `이 창이 아직 그릴 줄 모르는 이벤트 ${n}건을 받았습니다 — ${kinds}`;
  }
}

/**
 * 종류와 배우로 무엇으로 그릴지 정한다. **배우를 보는 자리가 여기 하나뿐**이라야 잊지 않는다.
 */
function kindOf(type, actor) {
  const base = DRAWN.get(type);
  if (!base) return 'unknown';
  // 정책·플래너·카운슬이 밀어 넣은 줄은 사람이 한 말이 아니다. 버리지도 않는다.
  if (type === 'prompt.submitted' && actor?.kind && actor.kind !== 'user') return 'note';
  return base;
}

/**
 * 이벤트에서 사람이 읽을 글을 뽑는다. **못 뽑으면 빈 글이지 추측이 아니다.**
 *
 * `Data` 는 종류마다 다른 payload 라 여기서 아는 것만 연다. 모양이 예상과 다르면 그냥 비운다 —
 * 지어낸 글을 모델의 말인 것처럼 화면에 올리는 것이 이 목업이 제일 피하려는 것이다.
 */
function textOf(ev, kind) {
  if (kind === 'unknown') return '';
  const d = ev?.data;
  if (d == null) return '';
  if (typeof d === 'string') return d;
  // 코어의 payload 모양 그대로: prompt 는 parts[], appended 는 part{}, delta 는 text.
  if (Array.isArray(d.parts)) return d.parts.map(partText).join('');
  if (d.part) return partText(d.part);
  for (const k of ['text', 'content', 'message']) {
    if (typeof d[k] === 'string') return d[k];
  }
  return '';
}

function partText(p) {
  if (typeof p === 'string') return p;
  return typeof p?.text === 'string' ? p.text : '';
}
