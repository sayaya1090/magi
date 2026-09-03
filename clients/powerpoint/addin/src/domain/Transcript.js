/**
 * 화면에 보이는 대화. 문이 흘리는 **로그 자신의 이벤트**를 접어서 줄로 만든다.
 *
 * 문은 렌더를 안 준다 — `Response.Event` 가 `internal/core/event/event.go` 의 `Event` 를
 * 통째로, 이름도 안 바꾸고 싣는다. 코어가 그렇게 고른 이유도 적어 뒀다: 렌더만 받은 쪽은
 * 콘솔이 하는 일을 못 하고, 한 스트림을 두 가지로 적으면 한쪽을 고칠 때 갈라진다.
 * **그래서 그리는 것이 읽는 쪽 일이고, 이 파일이 그 자리다**(clients/powerpoint/DESIGN.md §5.7).
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
 * 그 물음은 **긍정으로** 물어야 한다. 「`user` 가 아니면 note」로 물으면 배우를 **안 밝힌**
 * 줄이 사용자 말풍선이 되는데, 그건 화면 모양만의 문제가 아니다 — 낸 글을 지우는 신호가
 * 사용자 줄의 **수**라서(`Composer` 의 `echoed`), 남의 줄 하나가 사람이 쓰던 글을 지운다.
 *
 * **셋 — `part.delta` 와 `part.appended` 는 같은 말 두 번이다.** 델타는 도는 중에 조각으로
 * 오고(버스 전용), `appended` 는 끝난 뒤 통째로 온다(로그). `messageId` 가 같다. 둘 다 새 줄로
 * 쌓으면 모델의 답이 화면에 두 번 뜬다. 그래서 **같은 messageId 는 한 줄**이고, `appended` 가
 * 오면 그 줄을 **덮어쓴다** — 붙어 있던 창과 나중에 붙은 창이 같은 화면을 보게 된다.
 *
 * **넷 — 끝난 턴에는 검증됐는지가 실려 온다.** `turn.finished` 는 검증 못 한 착지에도 오고
 * (`TurnFinishedData.Unverified`), 그 딱지를 안 실으면 「고쳤다」와 「고쳤다는데 아무도 못
 * 봤다」가 화면에서 똑같이 생긴다. 슬라이드를 고치는 물건에서 그 차이는 사람이 덱을 그대로
 * 발표하느냐 열어 보느냐를 가른다.
 */

/** 채팅창이 실제로 그릴 줄 아는 종류. 나머지는 버리지 않고 `unknown` 으로 남는다. */
const DRAWN = new Map([
  ['prompt.submitted', 'user'],
  ['turn.finished', 'turn'],
  ['error', 'error'],
  // **카운슬은 이 제품에서 특히 그려야 한다.** 종료 게이트가 「다 했다」를 거절하면 턴이 계속
  // 도는데, 그 사유가 화면에 없으면 사람은 **모델이 왜 같은 일을 또 하는지** 모른다. 실물에서
  // 그 화면을 봤다(2026-09-01): 제목은 이미 바뀌었는데 창에는 도구 호출만 줄줄이 섰고, 세
  // 번의 거절과 그 사유는 로그에만 있었다.
  ['council.convened', 'council'],
  ['council.verdict', 'council'],
  ['council.decided', 'council'],
  // 무엇을 허락했는가. 이 제품에서 그 답은 **덱을 고칠 권한을 줬는가**라, 감사 줄이 아니라
  // 사람이 읽는 줄이다. 짝이 되는 호출 줄에 접힌다(아래 `append`).
  ['permission.decided', 'permission'],
]);

/**
 * **안 그리기로 정한 것.** 모르는 것과 다르다 — 모르는 것은 아래 `unknownNote` 가 세어서
 * 「이 창을 고쳐야 한다」고 적고, 이것들은 고칠 것이 없다.
 *
 * `context.usage` 는 토큰 계량기라 턴마다 수십 건 오고, 이 판(348×391)에서 그 수는 대화를
 * 밀어낸다. `council.deliberating` 은 「지금 묻는 중」이라는 **살아 있는 패널용** 신호인데
 * (payload 주석이 그렇게 적어 뒀다) 곧 뒤따라 오는 `council.verdict` 가 같은 사실을 오래 가는
 * 형태로 말한다 — 둘 다 그리면 같은 말이 두 번 선다.
 *
 * 그래도 **몇 건인지는 적는다.** 조용히 버리는 것과 안 그리기로 한 것은 화면에서 같아 보이면
 * 안 된다(이 파일이 맨 위에 못 박은 것).
 */
/**
 * **판으로 그리는 것.** 대화 줄도 아니고 안 그리기로 한 것도 아니다 — 자리가 다른 것이다.
 *
 * `todos.changed` 가 그렇다. 계약이 「바뀔 때마다 **계획 전량**」이라(`payload.go` 의
 * `TodosChangedData`) 줄로 쌓으면 같은 계획이 턴마다 여덟 번 선다 — 실물에서 한 턴에
 * `todowrite` 가 여덟 번 오는 것을 봤다. 그래서 **마지막 것으로 갈아 끼운다.**
 *
 * `unknown` 으로도 `skip` 으로도 세지 않는다. 앞엣것은 「이 창을 고쳐야 한다」는 뜻이고
 * 뒤엣것은 「고칠 것이 없다」는 뜻인데, 이건 **고쳐서 그리기로 한 것**이다.
 */
const PANEL = new Set(['todos.changed']);

const IGNORED = new Set([
  'context.usage', 'council.deliberating',
  // **대화의 생김과 옮김은 그릴 것이 아니다.** 앞 판본은 이 둘을 「그릴 줄 모르는 이벤트」로
  // 세어서, 새 대화를 시작할 때마다 화면 아래에 「이 창이 아직 그릴 줄 모르는 이벤트 1건을
  // 받았습니다 — session.moved」가 떴다. 그건 고칠 것이 있다는 뜻의 줄인데 여기서는 고칠
  // 것이 없다 — 옮김은 `ReadTranscript` 가 따라가서 이미 **처리한** 것이고, 생김은 그
  // 대화의 첫 줄일 뿐이다. 그런 줄이 늘 떠 있으면 사람은 그 자리를 안 읽게 되고, 진짜
  // 모르는 것이 왔을 때 그 줄이 아무 일도 못 한다.
  'session.created', 'session.moved',
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
  // 호출의 답. **호출한 줄에 접힌다**(아래 `append`) — 따로 세우면 「무엇을 불렀나」와
  // 「어떻게 됐나」가 화면에서 떨어지고, 도구가 줄줄이 도는 턴에서 짝이 안 맞는다.
  ['tool-result', 'result'],
]);

/** 같은 `messageId` 로 이어 붙는 줄. 도구 호출은 **한 줄에 하나**라 여기 없다. */
const FOLDED = new Set(['model', 'think']);

export class Row {
  constructor({ seq, kind, text, actor, messageId, call, finish, result, permission, council }) {
    this.seq = seq ?? 0;
    this.kind = kind;       // user | model | think | tool | note | turn | error | unknown
    // **`type` 과 `ts` 는 안 싣는다.** 로그의 이름 그대로와 시각을 각각 실어 뒀지만, 이
    // 저장소 어디에도 그 둘을 읽는 곳이 없다(필드 드롭 계측 — 둘 다 통째로 비워도 아무
    // 소리가 안 났다). `type` 은 특히 접히는 줄에서 위험했다: 델타 여럿을 한 줄로 접으면서
    // 마지막 이벤트의 이름으로 덮어써서, **여러 이벤트가 만든 줄에 그중 하나의 이름만
    // 앉아 있었다.** 나중에 이 칸을 읽는 사람은 그것을 그 줄 전체의 종류로 읽는다.
    // 줄이 무엇으로 그려지는지는 `kind` 가 답하고, 그쪽은 읽는 데가 있다.
    this.text = text ?? '';
    this.actor = actor ?? null;
    this.messageId = messageId ?? null;
    /** 도구 이름. `kind === 'tool'` 일 때만 있다 — 이 줄이 「모델이 한 일」이다. */
    this.tool = call?.name ?? null;
    /** 그 호출의 인자와 신원. 안내 포스트잇이 여기서 나온다(§6.1). */
    this.args = call?.args ?? null;
    this.callId = call?.callId ?? null;
    /**
     * 끝난 턴이 **검증된 끝인가.** `turn.finished` 는 검증 못 한 착지에도 온다
     * (`TurnFinishedData.Unverified`) — 「했다」와 「했다는데 아무도 못 봤다」가 같은 종류로
     * 온다는 뜻이라, 이걸 안 실으면 화면은 둘을 똑같이 그린다.
     */
    this.unverified = finish?.unverified === true;
    this.reason = finish?.reason ?? '';
    /**
     * 이 호출이 **어떻게 됐는가.** 도구 줄에 접혀 들어온다.
     *
     * `isError` 하나로 ✗ 를 찍으면 안 된다 — 코어가 `Advisory` 를 따로 둔 이유가 그 필드
     * 주석에 적혀 있다: 한 일은 했는데 읽을 것이 붙은 호출도 `IsError` 를 세우고, 그래서 두
     * 창이 **성공한 쓰기를 실패로 그린 적이 있다.** 이 제품에서 그 오독은 「슬라이드가 안
     * 바뀌었다」로 읽힌다.
     */
    this.result = result ?? null;
    /** 이 호출을 허락했는가(`allow|deny|always`). 이 제품에서는 「덱을 고치게 뒀는가」다. */
    this.permission = permission ?? null;
    /** 종료 게이트가 한 말. `kind === 'council'` 일 때만 있다. */
    this.council = council ?? null;
    /**
     * 완성본이 한 번이라도 앉았는가. **덮어쓸지 이어 붙일지가 여기 달렸다** — 델타로 쌓던 줄에
     * 오는 첫 완성본은 같은 말의 되풀이라 덮어쓰고, 그 뒤에 오는 완성본은 **다음 조각**이라
     * 이어 붙인다. 로그를 처음부터 다시 읽을 때는 델타가 아예 없어서 전부 뒤엣것이 된다.
     */
    this.settled = false;
  }
  get drawn() { return this.kind !== 'unknown'; }
  /**
   * 배우를 **밝힌** 줄인가. 「사람이 아닌 배우가 넣었다」와 「누가 넣었는지 안 실렸다」는 다른
   * 말이라, 줄머리를 고르는 쪽이 둘을 가를 수 있어야 한다.
   */
  get attributed() { return typeof this.actor?.kind === 'string' && this.actor.kind !== ''; }
  /** 로그에 자리가 있는가. 버스 전용 이벤트는 `seq == 0` 이라 자리가 없다. */
  get positioned() { return this.seq > 0; }
}

export class Transcript {
  constructor() {
    this.rows = [];
    /** 그릴 줄 몰라 남겨 둔 종류와 그 수. 화면 아래 한 줄로 정직하게 적는다. */
    this.unknownCounts = new Map();
    /** **안 그리기로 정한** 종류와 그 수(`IGNORED`). 모르는 것과 한 칸에 섞지 않는다. */
    this.skippedCounts = new Map();
    /** 스트림이 살아 있다고 **믿는가**. 끊김은 에러로 안 오므로 이 값은 밖에서 꺼 준다. */
    this.live = false;
    /** 서버가 커서를 거절하며 한 말. 있으면 화면이 그대로 보여 준다. */
    this.refusal = null;
    /**
     * 지금 계획. **마지막 스냅숏 하나**다(`PANEL`).
     *
     * 빈 배열은 「계획이 없다」이지 「모른다」가 아니다 — `todos.changed` 는 로그에 자리를
     * 가지므로(전이가 아니다) 다시 붙어 되풀이를 받으면 그대로 되살아난다.
     */
    this.todos = [];
  }

  /** 서버가 「전부 다시 보낸다」고 했다. **가진 것을 버린다** — 안 버리면 앞뒤가 이어 붙는다. */
  restart(why) {
    this.rows = [];
    this.todos = [];
    this.unknownCounts = new Map();
    this.skippedCounts = new Map();
    this.refusal = why ?? null;
    return this;
  }

  /** 대화 자체가 바뀌었다. 거절과 달리 사유가 서버 말이 아니라 우리 판단이다. */
  switchTo() {
    this.rows = [];
    // 계획은 그 대화의 것이다 — 안 비우면 새 대화에 남의 계획이 서 있다.
    this.todos = [];
    this.unknownCounts = new Map();
    this.skippedCounts = new Map();
    this.refusal = null;
    return this;
  }

  append(ev) {
    const type = String(ev?.type ?? '');
    if (PANEL.has(type)) {
      // **쌓지 않고 갈아 끼운다.** 계약이 매번 전량이므로 마지막 것이 곧 지금 계획이다.
      // 배열이 아니면 안 건드린다 — 모양이 달라졌을 때 빈 계획으로 덮으면 있던 것이 사라진다.
      const got = ev?.data?.todos;
      if (Array.isArray(got)) this.todos = got;
      return null;
    }
    const partKind = partKindOf(ev, type);
    const kind = kindOf(type, ev?.actor, partKind);
    if (kind === 'unknown') {
      // 조각 종류까지 적는다. 「part.appended 3건」은 무엇을 못 그렸는지 안 알려 준다.
      const label = partKind ? `${type} (${partKind})` : type;
      this.unknownCounts.set(label, (this.unknownCounts.get(label) ?? 0) + 1);
    }
    // 안 그리기로 정한 것. **세기는 센다** — 조용히 버리는 것과 같아 보이면 안 된다.
    if (kind === 'skip') {
      this.skippedCounts.set(type, (this.skippedCounts.get(type) ?? 0) + 1);
      return null;
    }
    const messageId = ev?.data?.messageId ?? null;

    // 호출의 답과 그 허락은 **호출한 줄에 접힌다.** 짝은 `callId` 로 짓는다 — 한 턴에 같은
    // 도구가 여러 번 도는 것이 이 제품의 보통이라(도형마다 한 번), 이름으로 지으면 세 번째
    // 호출의 답이 첫 번째 줄에 붙는다. **뒤에서부터** 찾는 것도 같은 이유다.
    if (kind === 'result' || kind === 'permission') {
      const id = kind === 'result' ? toolResultOf(ev)?.callId : callIdOf(ev);
      const at = lastIndex(this.rows, (r) => r.kind === 'tool' && r.callId && r.callId === id);
      if (at >= 0) {
        const row = this.rows[at];
        if (kind === 'result') row.result = toolResultOf(ev);
        else row.permission = decisionOf(ev);
        if (ev?.seq > 0 && ev.seq > row.seq) row.seq = ev.seq;
        return row;
      }
      // 짝을 못 찾았다. **버리지 않는다** — 호출 없이 답만 있는 화면이 사실이고, 그 사실은
      // 이 창이 로그 중간부터 읽기 시작했다는 뜻이라 사람이 알아야 한다.
    }

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
        if (ev?.seq > 0) row.seq = ev.seq;   // 자리가 생겼다
        return row;
      }
    }

    const row = new Row({
      seq: ev?.seq, kind, actor: ev?.actor, messageId,
      text: textOf(ev, kind), call: toolCallOf(ev), finish: finishOf(ev, type),
      result: kind === 'result' ? toolResultOf(ev) : null,
      permission: kind === 'permission' ? decisionOf(ev) : null,
      council: kind === 'council' ? councilOf(ev, type) : null,
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

  /**
   * 안 그리기로 정한 것의 셈. **`unknownNote` 와 다른 칸이다** — 한 줄로 합치면 「고칠 것이
   * 있다」와 「이대로가 맞다」가 같은 문장이 되고, 그 줄은 곧 아무도 안 읽는다.
   */
  get skippedNote() {
    if (this.skippedCounts.size === 0) return null;
    const n = [...this.skippedCounts.values()].reduce((a, b) => a + b, 0);
    const kinds = [...this.skippedCounts.keys()].sort().join(', ');
    return `일부러 안 그린 이벤트 ${n}건 — ${kinds}`;
  }
}

/**
 * 종류와 배우로 무엇으로 그릴지 정한다. **배우를 보는 자리가 여기 하나뿐**이라야 잊지 않는다.
 */
function kindOf(type, actor, partKind) {
  if (partKind !== null) return PART_DRAWN.get(partKind) ?? 'unknown';
  if (IGNORED.has(type)) return 'skip';
  const base = DRAWN.get(type);
  if (!base) return 'unknown';
  // 정책·플래너·카운슬이 밀어 넣은 줄은 사람이 한 말이 아니다. 버리지도 않는다.
  //
  // **「user 인가」를 묻지 「user 가 아닌가」를 묻지 않는다.** 코어의 `Actor` 는 포인터가 아니라
  // 구조체라 프레임에 늘 실리고, 아무도 안 채우면 `kind` 가 **빈 문자열**로 온다. 「안 밝혔다」를
  // 「사용자가 넣었다」로 세는 자리가 바로 거기고, 코어에서 이걸 읽는 쪽들은 전부 긍정으로
  // 묻는다(`internal/app/loop_helpers.go` 의 `lastUserPromptText`, `internal/app/fork.go` 의
  // `Replay`).
  if (type === 'prompt.submitted' && actor?.kind !== 'user') return 'note';
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
function toolCallOf(ev) {
  const c = ev?.data?.part?.toolCall;
  if (!c || typeof c.name !== 'string' || c.name === '') return null;
  // `Args` 는 `json.RawMessage` 라 **프레임 안에 JSON 그대로** 실린다 — 문자열 한 겹이 아니다.
  // 그래서 여기서 다시 파싱하지 않는다. 다시 파싱하면 진짜 객체를 받은 날 조용히 비게 된다.
  return { name: c.name, callId: typeof c.callId === 'string' ? c.callId : null,
    args: c.args ?? null };
}

/**
 * 호출의 답. **`isError` 와 `advisory` 를 둘 다 싣는다** — 하나로 접으면 「했는데 읽을 것이
 * 붙었다」가 「못 했다」로 그려진다(`session.ToolResult.Advisory` 의 주석이 그 사고를 적어 뒀다:
 * 파일은 디스크에 있었고 모델은 끝난 일로 다뤘는데 창 둘이 ✗ 를 찍었다).
 *
 * `content` 는 `json.RawMessage` 라 **프레임 안에 JSON 그대로** 온다. 글일 때도 있고 객체일
 * 때도 있어서, 여기서 모양을 정하지 않고 화면이 펴게 그대로 나른다.
 */
function toolResultOf(ev) {
  const r = ev?.data?.part?.toolResult;
  if (!r) return null;
  return {
    callId: typeof r.callId === 'string' ? r.callId : null,
    content: r.content ?? null,
    isError: r.isError === true,
    advisory: r.advisory === true,
    images: Array.isArray(r.images) ? r.images.length : 0,
  };
}

/** 허락 이벤트가 가리키는 호출. */
function callIdOf(ev) {
  const id = ev?.data?.callId;
  return typeof id === 'string' && id !== '' ? id : null;
}

/** 무엇으로 결정했는가(`allow|deny|always`). 모르면 빈 글이지 짐작이 아니다. */
function decisionOf(ev) {
  const d = ev?.data?.decision;
  return typeof d === 'string' ? d : '';
}

/**
 * 종료 게이트가 한 말. **셋을 한 모양으로 싣는다** — 화면이 종류마다 다른 칸을 뒤지게 하면
 * 새 종류가 늘 때 조용히 빈 줄이 선다.
 *
 * 표결 수(`tally`)는 코어의 `council.Breakdown` 을 그대로 나른다. 다시 세지 않는다 — 여기서
 * 세면 규칙(과반·만장일치·가중치)을 이 창이 한 벌 더 갖게 되고, 둘이 어긋나는 날 화면이
 * **로그와 다른 결론**을 적는다.
 */
function councilOf(ev, type) {
  const d = ev?.data ?? {};
  return {
    stage: type.slice('council.'.length),   // convened | verdict | decided
    round: Number(d.round ?? 0),
    members: Array.isArray(d.members) ? d.members : [],
    rule: typeof d.rule === 'string' ? d.rule : '',
    member: typeof d.member === 'string' ? d.member : '',
    lens: typeof d.lens === 'string' ? d.lens : '',
    decision: typeof d.decision === 'string' ? d.decision : '',
    rationale: typeof d.rationale === 'string' ? d.rationale : '',
    // **말 없는 표를 「기권했다」로 적지 않는다**(`CouncilVerdictData.Silent`) — 백엔드가
    // 죽었거나 답을 못 읽은 것이라, 판단한 기권과 다른 사실이다.
    silent: d.silent === true,
    note: typeof d.note === 'string' ? d.note : '',
    feedback: typeof d.feedback === 'string' ? d.feedback : '',
    tally: d.tally ?? null,
  };
}

/** 끝난 턴이 스스로 붙인 딱지. 다른 종류에는 없다. */
function finishOf(ev, type) {
  if (type !== 'turn.finished') return null;
  return { unverified: ev?.data?.unverified === true, reason: ev?.data?.reason ?? '' };
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

/**
 * 뒤에서부터 찾는다. `Array.prototype.findLastIndex` 를 안 쓰는 것은 이 창이 도는 WebView2 의
 * 나이를 우리가 못 고르기 때문이다 — 없는 메서드 하나가 **작업창 전체를 흰 화면으로** 만든다.
 */
function lastIndex(rows, pred) {
  for (let i = rows.length - 1; i >= 0; i--) {
    if (pred(rows[i])) return i;
  }
  return -1;
}
