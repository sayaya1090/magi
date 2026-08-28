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
  ['turn.finished', 'turn'],
  ['error', 'error'],
]);

/**
 * 조각의 종류로 무엇으로 그릴지 정한다. **`part.appended` 를 조각 종류 안 보고 그리면 안 된다.**
 *
 * 코어의 `PartAppendedData` 는 `messageId` 하나에 **조각 하나**를 싣는다(`payload.go`). 그래서
 * 모델이 글을 쓰고 도구를 부르면 **같은 `messageId` 로 이벤트가 둘** 온다 — 앞은 `text` 조각,
 * 뒤는 `tool-call` 조각. 조각 종류를 안 보면 뒤엣것도 「모델의 말」이 되고, 완성본은 통째라
 * 덮어쓰므로 **모델의 답이 자기 도구 호출에 지워진다.** 이 제품은 도구가 슬라이드를 고치는
 * 물건이라 거의 매 턴이 그 모양이 된다.
 *
 * `part.delta` 도 마찬가지로 `kind` 를 싣는다(`PartDeltaData`). 안 보면 **추론이 답풍선으로
 * 흘러 들어간다** — 모델이 혼잣말한 것이 사용자에게 답으로 붙는다.
 */
const PART_DRAWN = new Map([
  ['text', 'model'],
  ['reasoning', 'think'],
  ['tool-call', 'tool'],
]);

/** 같은 `messageId` 로 이어 붙는 줄. 도구 호출은 **한 줄에 하나**라 여기 없다. */
const FOLDED = new Set(['model', 'think']);

export class Row {
  constructor({ seq, kind, type, text, actor, ts, messageId, tool }) {
    this.seq = seq ?? 0;
    this.kind = kind;       // user | model | think | tool | note | turn | error | unknown
    this.type = type;       // 로그의 이름 그대로
    this.text = text ?? '';
    this.actor = actor ?? null;
    this.ts = ts ?? null;
    this.messageId = messageId ?? null;
    /** 도구 이름. `kind === 'tool'` 일 때만 있다 — 이 줄이 「모델이 한 일」이다. */
    this.tool = tool ?? null;
    /**
     * 완성본이 한 번이라도 앉았는가. **덮어쓸지 이어 붙일지가 여기 달렸다** — 델타로 쌓던 줄에
     * 오는 첫 완성본은 같은 말의 되풀이라 덮어쓰고, 그 뒤에 오는 완성본은 **다음 조각**이라
     * 이어 붙인다. 로그를 처음부터 다시 읽을 때는 델타가 아예 없어서 전부 뒤엣것이 된다.
     */
    this.settled = false;
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
    const partKind = partKindOf(ev, type);
    const kind = kindOf(type, ev?.actor, partKind);
    if (kind === 'unknown') {
      // 조각 종류까지 적는다. 「part.appended 3건」은 무엇을 못 그렸는지 안 알려 준다.
      const label = partKind ? `${type} (${partKind})` : type;
      this.unknownCounts.set(label, (this.unknownCounts.get(label) ?? 0) + 1);
    }
    const messageId = ev?.data?.messageId ?? null;

    // 델타와 완성본은 같은 말이다. 같은 messageId 의 **같은 종류** 줄이 있으면 거기 접는다.
    if (FOLDED.has(kind) && messageId) {
      const at = this.rows.findIndex((r) => r.kind === kind && r.messageId === messageId);
      if (at >= 0) {
        const row = this.rows[at];
        const t = textOf(ev, kind);
        if (type === 'part.appended') {
          row.text = row.settled ? row.text + t : t;
          row.settled = true;
        } else {
          row.text += t;
        }
        row.type = type;
        if (ev?.seq > 0) row.seq = ev.seq;   // 자리가 생겼다
        return row;
      }
    }

    const row = new Row({
      seq: ev?.seq, kind, type, actor: ev?.actor, ts: ev?.ts, messageId,
      text: textOf(ev, kind), tool: toolNameOf(ev),
    });
    row.settled = type === 'part.appended';
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
function kindOf(type, actor, partKind) {
  if (partKind !== null) return PART_DRAWN.get(partKind) ?? 'unknown';
  const base = DRAWN.get(type);
  if (!base) return 'unknown';
  // 정책·플래너·카운슬이 밀어 넣은 줄은 사람이 한 말이 아니다. 버리지도 않는다.
  if (type === 'prompt.submitted' && actor?.kind && actor.kind !== 'user') return 'note';
  return base;
}

/**
 * 이 이벤트가 실은 조각 하나인가, 그렇다면 무슨 조각인가. 조각이 아니면 `null`.
 *
 * 종류가 안 실렸으면 `text` 로 본다 — 코어가 늘 채우지만, 안 채워진 낡은 줄을 **못 그리는 것**으로
 * 떨어뜨리면 있던 대화가 화면에서 사라진다. 모르는 종류는 그대로 두어 `unknown` 으로 세게 한다.
 */
function partKindOf(ev, type) {
  if (type === 'part.appended') return String(ev?.data?.part?.kind ?? 'text');
  if (type === 'part.delta') return String(ev?.data?.kind ?? 'text');
  return null;
}

/**
 * 도구 호출 줄이 들고 있어야 하는 이름. **이 줄이 「모델이 한 일」**이고, 이 제품에서 그건
 * 사용자의 슬라이드가 바뀌었다는 뜻이라 답보다 중요할 때가 있다.
 *
 * ⚠ 나중에 `tool-result` 를 그리게 되면 `IsError` 만 보고 ✗ 를 찍으면 안 된다 — 코어가
 * `Advisory` 를 따로 둔 이유가 그 주석에 적혀 있다(한 일은 했는데 읽을 것이 붙은 호출도
 * `IsError` 를 세우므로, 두 창이 성공한 쓰기를 실패로 그린 적이 있다).
 */
function toolNameOf(ev) {
  const n = ev?.data?.part?.toolCall?.name;
  return typeof n === 'string' && n !== '' ? n : null;
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
