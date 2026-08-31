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
   * @param {{token:string, presentation?:string, label?:string,
   *          EventSourceImpl?:Function, origin?:string}} opts
   */
  constructor({ token, presentation = '', label = '', EventSourceImpl, origin } = {}) {
    this.token = token;
    this.presentation = presentation;
    this.label = label;
    // 주소는 **적지 않는다** — 페이지가 헬퍼에서 왔으므로 헬퍼의 주소가 곧 자기 오리진이다
    // (§5.5). 시험만 이 자리를 채운다.
    this.origin = origin ?? (typeof location === 'undefined' ? '' : location.origin);
    this.EventSourceImpl = EventSourceImpl
      ?? (typeof EventSource === 'undefined' ? null : EventSource);
    /** kind → Set<fn> */
    this.handlers = new Map();
    this.source = null;
    /** 애드인이 붙은 문서 키. `hello` 프레임이 준다 — 도구의 `document` 인자가 이 값이다. */
    this.document = null;
  }

  on(kind, fn) {
    if (!this.handlers.has(kind)) this.handlers.set(kind, new Set());
    this.handlers.get(kind).add(fn);
    return () => this.handlers.get(kind)?.delete(fn);
  }

  #emit(kind, data) {
    for (const fn of this.handlers.get(kind) ?? []) fn(data);
  }

  /** 붙는다. **붙는 것이 곧 등록이고 끊기는 것이 곧 떠남이다** — 작별 프레임이 없다(§5.5). */
  open() {
    if (!this.EventSourceImpl) throw new Error('이 환경에는 EventSource 가 없다');
    const q = new URLSearchParams({ token: this.token ?? '' });
    if (this.presentation) q.set('presentation', this.presentation);
    if (this.label) q.set('label', this.label);
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
    src.onerror = () => this.#emit('stream', { live: false, why: '스트림이 끊겼습니다' });
    return this;
  }

  close() {
    this.source?.close();
    this.source = null;
  }
}
