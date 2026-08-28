/**
 * 데몬이 지금 막혀서 기다리는 물음 하나. **스트림에 안 오는 것**이라 따로 든다.
 *
 * 왜 따로인가: 물음은 *무엇이 일어났는가*에 대한 사실이 아니라 *무엇을 해야 하는가*에 대한
 * 질문이라 로그에 안 쌓이고 막힌 데몬의 버스에만 나간다. 그래서 밖의 창은 **전량을 받고도 그
 * 물음만 못 본다.** 답은 `status`가 `Waiting`으로 실어 주는 것이고, 어태치 TUI도 웹 콘솔도
 * 같은 우회를 한다(clients/powerpoint/DESIGN.md §5.7).
 */

/**
 * 답으로 보낼 수 있는 넷과 **각각이 여는 폭**. 단추 문구가 이 폭을 말해야 한다 —
 * 「허용」/「항상 허용」으로 적으면 사람이 세션 전체를 여는 줄 모르고 누른다.
 */
export const DECISIONS = [
  { value: 'allow',   label: '이번 호출만 허용',        width: 'call' },
  { value: 'deny',    label: '거절',                    width: 'call' },
  { value: 'always',  label: '이 세션에서 이 도구 전부', width: 'session' },
  { value: 'persist', label: '앞으로 계속 (설정에 기록)', width: 'project' },
];

/**
 * 넓은 둘의 폭이 **줄어들 수 있다**. 다른 컴패니언이 넘긴 일이면(`handedFrom`) 코어가
 * `always`·`persist`를 **이번 호출 하나로** 받고, 그 사실을 대화에 한 줄로 적는다
 * (`internal/app/permission.go`의 `noteOneCallOnly`). 그러니 단추 문구는 폭을 **약속**하면
 * 안 된다 — 코어가 줄이면 문구가 거짓말이 된다. 대신 줄었다는 그 줄을 화면이 보여 준다.
 *
 * 다행히 그 줄은 `PromptSubmitted`에 배우가 `system`/`loop`로 온다 — 채팅창이 정책 줄을
 * 정보 줄로 그리기로 한 규칙(`Transcript`의 `kindOf`)이 이것도 같이 받는다. 규칙 하나가
 * 사례 둘을 덮는다는 것이 그 규칙이 맞다는 표시다.
 */
export const WIDTH_NOTE =
  '넓은 선택은 코어가 이번 호출 하나로 줄일 수 있습니다 — 줄이면 대화에 그렇게 적힙니다.';

/**
 * 이 창이 답할 줄 아는 종류 둘. **모르는 종류를 이 둘 중 하나로 넘겨짚지 않는다.**
 *
 * 넘겨짚으면 어떻게 되는지가 코어에 실물로 있다 — `daemon.go`의 `Waiting.Event`는 `switch
 * w.Kind`의 `default:`가 **질문이 아닌 것을 전부 권한 물음으로** 되살린다. 새 종류가 생기면
 * 옛 뷰어가 그것을 「허용/거절」 단추와 함께 그리고, 사람이 누른 결정은 그 종류가 기다리는
 * 답이 아니다. 이 파일의 첫 판에도 `kind ?? 'permission'`이 그대로 있었다(자백이다).
 *
 * 그래서 규칙: **모르면 모른다고 든다.** 무엇이 대기 중인지는 보여 주되(안 보여 주면 §6이
 * 말한 「아무도 안 보는 곳에서 대기」로 돌아간다), 단추는 안 준다.
 */
export const KINDS = Object.freeze({ permission: 'permission', question: 'question' });

export class Pending {
  constructor({ id, kind, what, args, reason, options, report, index, total, since }) {
    this.id = id;                       // 답이 실어야 하는 call id. **주소다.**
    this.kind = kind ?? '';             // 로그의 말 그대로. **기본값을 안 준다.**
    this.what = what ?? '';
    this.args = args ?? null;
    this.reason = reason ?? '';
    this.options = options ?? [];
    /**
     * 이 물음이 **무엇을 근거로** 사람에게 왔는가(`[{key, text}, …]`). 코어가 소켓으로
     * 실어 보내는 이유가 주석에 적혀 있다 — 「근거가 뒤에 남은 물음이야말로 이것이 막으려던
     * 그것」(`daemon.go`의 `Waiting.Report`). 이 창의 첫 판은 이 셋을 **받고도 버렸다.**
     * 버리면 화면에 남는 것은 「예/아니오」뿐이고, 그 화면은 사람을 **판단이 아니라 클릭**으로
     * 만든다.
     *
     * 순서를 그대로 든다. 코어의 `Contract.Fill`이 「무엇을 해 봤는가」를 「무엇을 고르겠는가」
     * 앞에 두는 것은 **읽는 차례를 정한 것**이지 우연이 아니다.
     */
    this.report = report ?? [];
    /**
     * 한 호출이 묻는 여러 물음 중 **몇 번째인가**(1부터). 하나를 답하면 다음이 온다는 사실을
     * 다른 창은 알 길이 없어 코어가 같이 싣는다. 0 은 「안 실렸다」로, 없는 것과 첫째를
     * 안 헷갈리려고 그대로 둔다.
     */
    this.index = index ?? 0;
    this.total = total ?? 0;
    this.since = since ?? null;
    Object.freeze(this);
  }

  /** 「2번째 · 모두 3개」. 안 실렸으면 `null` — **안 온 것을 1/1로 지어내지 않는다.** */
  get placement() {
    if (!this.index || !this.total || this.total <= 1) return null;
    return `${this.index}번째 · 모두 ${this.total}개`;
  }

  /** 이 창이 그릴 줄 아는 종류인가. 아니면 단추 없이 「모르는 물음이 대기 중」으로만 그린다. */
  get known() { return this.kind === KINDS.permission || this.kind === KINDS.question; }
  get isPermission() { return this.kind === KINDS.permission; }
  get isQuestion() { return this.kind === KINDS.question; }

  /**
   * 같은 물음인가. 폴링이 같은 것을 계속 실어 오므로 **다시 그리지 않으려면** 이게 필요하다.
   *
   * **물은 시각까지 본다.** id 와 종류만 보면, 두 폴 사이에 물음이 내려가고 같은 id·같은 종류의
   * 새 물음이 올라온 경우가 「안 바뀜」으로 보인다. 그러면 앞의 답이 보내진 표시가 안 풀려
   * **새 물음이 잠긴 채로 선다** — 답할 수 있는 것을 못 답한다. call id 는 모델이 붙이는 것이라
   * 세션이 새로 세면 `call_1` 이 다시 나오고, 그때 물은 시각은 다르다. 코어가 `Since` 를 싣는
   * 이유는 「얼마나 기다렸는지 말하라」였는데, 여기서는 그게 신원 노릇을 한다.
   *
   * 안 실렸으면 양쪽 다 `null` 이라 예전과 똑같이 군다 — 없는 것에 기대지는 않는다.
   */
  same(other) {
    return other != null && other.id === this.id && other.kind === this.kind
      && other.since === this.since;
  }
}

/**
 * 답이 왜 끝났는지. **끝난 사유를 값에 싣는다** — 「없다」만 남기면 화면이 답한 것과 남이 답한
 * 것과 데몬이 죽은 것을 못 가른다.
 *
 * - `answered` — 이 창이 답했다.
 * - `elsewhere` — 남이 답했다(여기·브라우저·TUI·정책). **무엇으로 답했는지는 모른다.** 코어의
 *   `answeredElsewhere`가 `allow`도 `deny`도 아닌 `elsewhere`를 싣는 이유가 그것이다 —
 *   찍으면 남의 입에 결정을 넣는 것이 된다. 화면은 묻기를 그만두면 되고, 진짜 결정은 로그로 온다.
 * - `unreachable` — 데몬이 안 잡힌다. 물음이 끝난 것이 아니라 **모르게 된 것**이다.
 */
export const CLEARED = Object.freeze({
  answered: 'answered', elsewhere: 'elsewhere', unreachable: 'unreachable',
});

/**
 * 그 사유를 사람에게 주는 한 줄. `null` 은 **할 말이 없다** — 내려간 물음이 없다는 뜻이다.
 *
 * **화면에서 여기로 내렸다.** 앞 판본은 `view.lastAskEl` 안의 객체 조회였고, 셋 중 아무것도
 * 안 맞으면 `null` 을 내서 화면이 **그 줄을 통째로 숨겼다.** 그러면 「내려간 물음이 없다」와
 * 「이 창이 모르는 사유로 내려갔다」가 화면에서 똑같이 생긴다 — 정확히 이 함수가 없애려던
 * 그 뭉갬(「없다」만 남기면 셋이 똑같이 생긴다)이 한 겹 위에서 되살아난 모양이다.
 *
 * 침묵이 틀린 답인 이유는 사람이 잃는 것이 크기 때문이다. 물음이 사라진 자리에서 화면이 아무
 * 말도 안 하면, 답을 기다리던 사람은 자기가 뭘 놓쳤는지도 모르고 창을 본다. 넷째 사유가
 * 생기면 여기서 소리가 난다 — 조용히 숨는 대신 「이 창이 모르는 사유」라고 적는다.
 *
 * 객체 조회를 `switch` 로 바꾼 것도 같은 값이다. `{...}[clearedBy]` 는 프로토타입까지 뒤져서
 * 사유가 `'constructor'` 같은 이름이면 함수를 문장 자리에 앉힌다.
 */
export function clearedNote(clearedBy) {
  switch (clearedBy) {
    case CLEARED.answered:
      return '직전 물음: 답을 보냈고 내려갔습니다.';
    // **무엇으로 답했는지는 안 적는다** — 남의 입에 결정을 넣는 것이 된다(위 주석).
    case CLEARED.elsewhere:
      return '직전 물음: 다른 곳에서 답했습니다 — 무엇으로 답했는지는 모릅니다.';
    // **끝난 것이 아니라 모르게 된 것이다.** 「답했다」로 읽히면 사람이 그 물음을 잊는다.
    case CLEARED.unreachable:
      return '직전 물음: 데몬에 못 닿아 내려갔습니다 — 끝난 것이 아닙니다.';
    // 내려간 물음이 없다. 여기만 조용해도 된다.
    case null: case undefined:
      return null;
    default:
      return `직전 물음이 이 창이 모르는 사유로 내려갔습니다(${clearedBy}). 이 창을 고쳐야 합니다.`;
  }
}

/**
 * 물음의 **인자 칸**이 무엇을 담는가. `null` 은 이 칸을 아예 안 만든다는 뜻이다.
 *
 * **화면에서 여기로 내렸다.** 앞 판본은 `askEl` 안의 `if (p.args != null)` 한 줄이었고, 안
 * 걸리면 **칸이 통째로 없었다.** 그러면 권한 물음이 「권한을 묻고 있습니다 · bash」와 허용/거절
 * 단추만으로 서고, 사람은 무엇을 허가하는지 모르는 채 누른다 — 이 파일 위쪽이 「정해진 것은
 * 도구 이름이 아니라 인자다」라고 적어 둔 바로 그 자리인데, 인자가 없을 때 그 사실을 아무도
 * 말하지 않았다. 같은 창의 로그 줄(`rowEl`)은 인자가 없으면 「(인자 없음)」이라고 적는다.
 * **아무것도 안 걸린 쪽이 더 위험한데 대접은 반대였다.**
 *
 * 안 실린 것을 「인자가 없다」로 적지도 않는다. 소켓의 `Args` 는 `omitempty` 라, 인자 없이
 * 부른 도구와 오다 빠진 인자가 **여기 도착할 때는 똑같이 생겼다**(`daemon.go` 의 `Waiting`).
 * 못 가르는 것을 가른 척하면 그게 이 창이 없애려는 뭉갬이다 — 모르는 것은 모른다고 적는다.
 *
 * 질문에는 이 말을 안 붙인다. 허가할 것이 없고, 보기와 적는 칸이 그 물음의 내용이다.
 *
 * @param {Pending} p
 * @returns {{args:*}|{note:string}|null}
 */
export function askArgs(p) {
  if (p.args == null) {
    if (!p.isPermission) return null;
    return { note: '무엇에 대한 허가인지 이 창에 안 실렸습니다 — 인자 없이 부르는 도구인지 '
      + '오다 빠진 것인지 못 가릅니다. 도구 이름만 보고 누르지 마세요.' };
  }
  // 빈 것을 **빈 상자로 그리지 않는다.** 빈 `<pre>` 는 「인자가 이렇다」도 「없다」도 아니고,
  // 화면이 고장 난 것처럼 보인다.
  if (isBlank(p.args)) return { note: '인자 없이 부릅니다.' };
  return { args: p.args };
}

/** 실린 것이 **아무것도 안 실은 것**인가. 빈 글·빈 객체·빈 배열. */
function isBlank(args) {
  if (typeof args === 'string') return args.trim() === '';
  if (Array.isArray(args)) return args.length === 0;
  return typeof args === 'object' && Object.keys(args).length === 0;
}
