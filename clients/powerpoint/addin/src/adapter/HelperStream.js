/**
 * 헬퍼로 난 **하나뿐인 연결**. 프레임 종류로 갈라 나눠 준다
 * (clients/powerpoint/DESIGN.md §5.5·§5.7).
 *
 * # 왜 하나인가
 *
 * 설계가 그렇게 적었다 — 애드인이 접속하고 도구 호출은 그 연결을 거슬러 내려오며, 대화는
 * **같은 연결을 반대 방향으로 한 번 더** 쓴다. 새 포트도, 새 연결도 없다.
 *
 * # 왜 WebSocket 이 아닌가
 *
 * 헬퍼 쪽 주석에 근거를 적어 뒀다(`handhttp.go`): Chrome 147 부터 Local Network Access 가
 * WebSocket 까지 가두고, 설계가 그것을 「우리가 고른 전송이 바로 그것」이라며 나쁜 소식으로
 * 적어 뒀다(§5.5). 지금 고를 수 있는 것 중 그 게이트에 **나중에** 들어가는 쪽이 fetch 계열이다.
 *
 * # 토큰이 쿼리에 실리는 이유
 *
 * `EventSource` 는 헤더를 못 싣는다. 페이지 자신이 그 토큰을 들고 있고 루프백이라 새로 새는
 * 것이 없다 — 그래도 **여기 한 자리뿐**이고, 나머지 왕복은 전부 헤더다.
 */
export class HelperStream {
  /**
   * @param {{token:string, presentation?:string, label?:string, role?:'hand'|'viewer',
   *          EventSourceImpl?:Function, origin?:string,
   *          fetchImpl?:Function, reload?:Function, wait?:(ms:number)=>Promise<void>}} opts
   *   `role` 은 손(기본)인가 화면인가 — 화면은 전사만 받고 호출은 안 받는다(HandRole.js).
   *   `fetchImpl`·`reload`·`wait` 는 되살리기의 세 문이고, 시험이 채운다.
   */
  constructor({
    token, presentation = '', label = '', EventSourceImpl, origin,
    fetchImpl, reload, wait, role = 'hand',
  } = {}) {
    this.token = token;
    // 되살리기에 쓰는 셋. **시험이 채워 넣을 수 있어야** 이 갈래를 잴 수 있고, 못 재는 갈래는
    // 안 만든 것과 같다.
    this.fetchImpl = fetchImpl ?? (typeof fetch === 'undefined' ? null : fetch.bind(globalThis));
    this.reload = reload
      ?? (typeof location === 'undefined' ? null : () => location.reload());
    this.wait = wait ?? ((ms) => new Promise((go) => setTimeout(go, ms)));
    /** 연이어 몇 번 헛걸음했는가. 물러서는 간격이 이 수로 자란다. */
    this.misses = 0;
    /** 되살리는 중인가. 두 번 겹쳐 돌면 연결이 둘 생긴다. */
    this.healing = false;
    this.presentation = presentation;
    this.label = label;
    /**
     * 손인가 화면인가. **화면(viewer)은 전사만 받고 호출은 안 받는다** — 바닥(PowerPointApi 1.8)
     * 아래 호스트에서 편집은 COM 손이 하고, 이 창이 손으로 붙으면 못 하는 호출을 받아 날 오류를
     * 낸다(HandRole.js). 헬퍼는 같은 문서 키의 연결을 하나로 보므로 역할은 붙을 때 말해야 한다.
     */
    this.role = role === 'viewer' ? 'viewer' : 'hand';
    // 주소는 **적지 않는다** — 페이지가 헬퍼에서 왔으므로 헬퍼의 주소가 곧 자기 오리진이다
    // (§5.5). 시험만 이 자리를 채운다.
    this.origin = origin ?? (typeof location === 'undefined' ? '' : location.origin);
    this.EventSourceImpl = EventSourceImpl
      ?? (typeof EventSource === 'undefined' ? null : EventSource);
    /** kind → Set<fn> */
    this.handlers = new Map();
    this.backlog = [];   // 청자가 없을 때 온 event 프레임 — 첫 청자에게 준다
    this.source = null;
    /** 애드인이 붙은 문서 키. `hello` 프레임이 준다 — 도구의 `document` 인자가 이 값이다. */
    this.document = null;
  }

  on(kind, fn) {
    if (!this.handlers.has(kind)) this.handlers.set(kind, new Set());
    this.handlers.get(kind).add(fn);
    // **먼저 온 것을 버리지 않는다.** 스트림은 창이 뜨자마자 열리고 전사 읽기는 대화 이름을 안 뒤에
    // 붙는다 — 그 사이 헬퍼가 되풀이해 준 앞부분이 듣는 이 없이 지나갔다(2026-09-05). 첫 청자에게 준다.
    if (kind === 'event' && this.backlog.length) {
      const held = this.backlog; this.backlog = [];
      for (const data of held) fn(data);
    }
    return () => this.handlers.get(kind)?.delete(fn);
  }

  #emit(kind, data) {
    const fns = this.handlers.get(kind);
    if (kind === 'event' && (!fns || fns.size === 0)) {
      if (this.backlog.length < 10000) this.backlog.push(data);
      return;
    }
    for (const fn of fns ?? []) fn(data);
  }

  /** 붙는다. **붙는 것이 곧 등록이고 끊기는 것이 곧 떠남이다** — 작별 프레임이 없다(§5.5). */
  open() {
    if (!this.EventSourceImpl) throw new Error('이 환경에는 EventSource 가 없다');
    const q = new URLSearchParams({ token: this.token ?? '' });
    if (this.presentation) q.set('presentation', this.presentation);
    if (this.label) q.set('label', this.label);
    if (this.role === 'viewer') q.set('role', 'viewer');
    const src = new this.EventSourceImpl(`${this.origin}/hand/stream?${q}`);
    this.source = src;

    // 종류마다 따로 듣는다. **모르는 종류를 조용히 버리지 않는다** — 그 규칙이 §5.7 의
    // 「모르는 것이 와도 안 버린다」이고, 여기서 버리면 화면이 아니라 이 층이 거짓말을 한다.
    for (const kind of ['hello', 'call', 'event', 'restart', 'stream', 'note']) {
      src.addEventListener(kind, (m) => {
        let data = null;
        try { data = JSON.parse(m.data); } catch { data = { raw: m.data }; }
        if (kind === 'hello') this.document = data?.document ?? null;
        this.#emit(kind, data);
      });
    }
    // **프로미스를 돌려준다.** 브라우저는 무시하지만 시험은 이것으로 기다린다 — 못 기다리면
    // 이 갈래는 잴 수가 없고, 못 재는 갈래는 안 만든 것과 같다.
    src.onerror = () => this.#heal(src);
    return this;
  }

  /**
   * 끊긴 연결을 되살린다. **왜 끊겼는지 물어보고 나서** 움직인다.
   *
   * 앞 판본은 「스트림이 끊겼습니다」 한 줄을 올리고 끝이었다. 그래서 헬퍼를 다시 띄우면
   * 판은 위쪽에 「붙었습니다 — 도구 28개」를 적어 둔 채 손이 죽어 있었고, 모델이 부르는 도구는
   * 전부 「연결된 작업창이 없습니다」로 떨어졌다. 살리는 길이 **파워포인트를 통째로 껐다 켜는
   * 것뿐**이었다 — 실물에서 그 화면을 봤다(2026-09-02). PC 를 잘 다루지 못하는 사람에게는
   * 고칠 방법이 없는 고장이다.
   *
   * 되살릴 수 없는 갈래가 하나 있다. **토큰은 헬퍼가 뜰 때마다 새로 난다.** 헬퍼가 다시
   * 뜨면 이 페이지가 들고 있는 토큰은 영영 거부되고, `EventSource` 는 200 이 아닌 답을 받으면
   * 규격대로 **재연결을 포기한다**. 아무리 다시 붙어도 안 된다 — 새 토큰을 실은 페이지를
   * 다시 받아 오는 수밖에 없다. 그래서 세 갈래를 가른다:
   *
   * - **토큰이 낡음**(401) → 페이지를 다시 불러온다. 사용자가 할 일이 없다.
   * - **헬퍼는 멀쩡, 연결만 끊김** → 그냥 다시 붙는다.
   * - **헬퍼가 안 뜸** → 물러서며 다시 묻고, **그 사이 사람에게는 사실대로 적는다.**
   */
  async #heal(dead) {
    if (this.healing || this.source !== dead) return;
    this.healing = true;
    try {
      const why = await this.#why();
      if (why === 'stale') {
        // 다시 불러오기 전에 한 번 적는다 — 화면이 깜빡이는 이유를 사람이 알아야 한다.
        this.#emit('stream', { live: false, why: '헬퍼가 다시 시작됐습니다 — 창을 새로 불러옵니다', reason: why });
        this.reload?.();
        return;
      }
      if (why === 'down') {
        this.misses += 1;
        this.#emit('stream', {
          live: false, reason: why,
          why: 'magi 헬퍼가 응답하지 않습니다 — 다시 연결해 보는 중입니다',
        });
        // 물러서되 천장을 둔다. 무한정 늘리면 헬퍼가 돌아와도 한참 뒤에야 붙는다.
        await this.wait(Math.min(1000 * 2 ** (this.misses - 1), 15000));
      } else if (this.role === 'viewer' && (await this.#anyHand()) === false) {
        // **볼 손이 없다.** 보는 연결은 손이 있어야 선다 — 헬퍼가 404 를 주는데 EventSource 는
        // 그 번호를 안 알려 주고 onerror 만 낸다. 그래서 따로 묻는다. 2021 에서 COM 손을 아직
        // 안 띄운 자리가 정확히 이것이고, 사람이 할 일이 있으므로 사실대로 적고 물러선다.
        this.misses += 1;
        this.#emit('stream', {
          live: false, reason: 'nohand',
          // **사람이 할 일이 있는 유일한 문장이다.** 손과 화면의 구분은 사람에게 안 말한다 — 이건
          // 구분이 아니라 「띄워라」다.
          why: '이 PowerPoint 판에서는 magi-ppt-hand 를 띄워야 편집이 됩니다 — 아직 안 떠 있습니다. 띄우면 이 창이 따라 붙습니다',
        });
        await this.wait(Math.min(1000 * 2 ** (this.misses - 1), 15000));
      } else {
        this.misses = 0;
      }
      if (this.source !== dead) return;   // 그 사이 누가 닫았거나 다시 열었다
      dead.close?.();
      this.source = null;
      this.open();
      this.#emit('stream', { live: true, why: '다시 연결됐습니다', reason: 'back' });
    } finally {
      this.healing = false;
    }
  }

  /**
   * 왜 끊겼는가 — `stale` · `down` · `dropped`.
   *
   * 토큰을 들려 보내 **헬퍼 자신에게 묻는다.** 401 은 「너는 지난 기동의 손님이다」이고,
   * 그것만이 다시 붙어서 안 풀리는 갈래다. 못 물어봤으면 `down` 이지 `stale` 이 아니다 —
   * 모르는 것을 「낡았다」로 적으면 헬퍼가 잠깐 바쁜 사이에 창을 다시 불러오고, 사람이 쓰던
   * 글이 날아간다.
   */
  async #why() {
    if (!this.fetchImpl) return 'dropped';
    try {
      const r = await this.fetchImpl(`${this.origin}/api/status`, {
        headers: { Authorization: `Bearer ${this.token ?? ''}` },
      });
      if (r?.status === 401 || r?.status === 403) return 'stale';
      return 'dropped';
    } catch {
      return 'down';
    }
  }

  /**
   * 볼 손이 하나라도 붙어 있는가 — `true` · `false` · `null`(못 물었다). 보는 연결만 묻는다.
   * 못 물은 것을 「없다」로 읽지 않는다 — 그러면 헬퍼가 잠깐 바쁜 사이에 사람에게 손을 띄우라고
   * 하게 된다.
   */
  async #anyHand() {
    if (!this.fetchImpl) return null;
    try {
      const r = await this.fetchImpl(`${this.origin}/api/documents`, {
        headers: { Authorization: `Bearer ${this.token ?? ''}` },
      });
      if (!r || r.status !== 200 || typeof r.json !== 'function') return null;
      const j = await r.json();
      if (j?.attached === true) return true;
      if (j?.attached === false) return false;
      return null;
    } catch {
      return null;
    }
  }

  close() {
    this.source?.close();
    this.source = null;
  }
}
