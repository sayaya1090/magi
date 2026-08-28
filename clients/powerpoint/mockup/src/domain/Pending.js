/**
 * 데몬이 지금 막혀서 기다리는 물음 하나. **스트림에 안 오는 것**이라 따로 든다.
 *
 * 왜 따로인가: 물음은 *무엇이 일어났는가*에 대한 사실이 아니라 *무엇을 해야 하는가*에 대한
 * 질문이라 로그에 안 쌓이고 막힌 데몬의 버스에만 나간다. 그래서 밖의 창은 **전량을 받고도 그
 * 물음만 못 본다.** 답은 `status`가 `Waiting`으로 실어 주는 것이고, 어태치 TUI도 웹 콘솔도
 * 같은 우회를 한다(DESIGN.md §5.7).
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
  constructor({ id, kind, what, args, reason, options, since }) {
    this.id = id;                       // 답이 실어야 하는 call id. **주소다.**
    this.kind = kind ?? '';             // 로그의 말 그대로. **기본값을 안 준다.**
    this.what = what ?? '';
    this.args = args ?? null;
    this.reason = reason ?? '';
    this.options = options ?? [];
    this.since = since ?? null;
    Object.freeze(this);
  }

  /** 이 창이 그릴 줄 아는 종류인가. 아니면 단추 없이 「모르는 물음이 대기 중」으로만 그린다. */
  get known() { return this.kind === KINDS.permission || this.kind === KINDS.question; }
  get isPermission() { return this.kind === KINDS.permission; }
  get isQuestion() { return this.kind === KINDS.question; }

  /** 같은 물음인가. 폴링이 같은 것을 계속 실어 오므로 **다시 그리지 않으려면** 이게 필요하다. */
  same(other) { return other != null && other.id === this.id && other.kind === this.kind; }
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
