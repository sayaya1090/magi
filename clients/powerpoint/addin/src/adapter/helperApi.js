/**
 * 헬퍼의 왕복들. **토큰은 헤더로 간다** — 쿼리에 실리는 자리는 `EventSource` 하나뿐이다
 * (clients/powerpoint/DESIGN.md §5.5, `HelperStream`).
 *
 * 주소를 안 적는 이유도 거기 있다: 페이지가 헬퍼에서 왔으므로 헬퍼의 주소가 곧 자기 오리진이다.
 * 소스에 주소를 박으면 §5.5 가 「같아야 한다」고 적은 이름 넷이 다섯이 되고, 다섯째는 아무도
 * 안 본다(헬퍼의 `TestTheAddinDoesNotWriteTheOriginDown` 이 그것을 매 빌드에서 막는다).
 */
export class HelperApi {
  constructor({ token, origin, fetchImpl } = {}) {
    this.token = token ?? '';
    this.origin = origin ?? (typeof location === 'undefined' ? '' : location.origin);
    this.fetch = fetchImpl ?? (typeof fetch === 'undefined' ? null : fetch.bind(globalThis));
  }

  async #send(path, { method = 'POST', body } = {}) {
    if (!this.fetch) throw new Error('이 환경에는 fetch 가 없다');
    const res = await this.fetch(`${this.origin}${path}`, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) {
      // **사유를 그대로 나른다.** 여기서 문장을 지어내면 헬퍼가 적은 것(어느 컴패니언이,
      // 무엇을 못 했는지)이 사라진다.
      const text = await res.text().catch(() => '');
      throw new Error(text.trim() || `헬퍼가 ${res.status} 로 답했습니다`);
    }
    if (res.status === 204 || res.status === 202) return null;
    const ct = res.headers?.get?.('Content-Type') ?? '';
    if (!ct.includes('json')) return null;
    return res.json();
  }

  companions() { return this.#send('/api/companions', { method: 'GET' }); }
  documents() { return this.#send('/api/documents', { method: 'GET' }); }
  status() { return this.#send('/api/status', { method: 'GET' }); }

  /**
   * **파워포인트 몫의 컴패니언**을 마련해 달라고 한다 — 고르는 화면을 안 거치는 길.
   *
   * 답은 **즉시** 온다. 데몬 냉시동은 오래 걸리는데 그걸 요청 안에서 기다리면 판이 멎고,
   * 멎은 화면은 사람에게 고장으로 읽힌다. 그래서 이 자리는 지금 상태(phase)를 주고 일은 뒤에서
   * 돈다 — 다시 불러 진행을 본다. 두 번 불러도 데몬이 둘 뜨지 않는다.
   */
  own() { return this.#send('/api/own', { body: {} }); }

  /**
   * **새 대화를 연다** — 「얘가 이상해요」의 탈출구.
   *
   * 대화 하나가 영원히 쌓이면 앞의 혼란이 뒤를 오염시킨다. 덱은 안 건드린다.
   */
  fresh() { return this.#send('/api/fresh', { body: {} }); }

  /** 늘 지킬 것을 읽는다. 아직 아무것도 안 적은 것은 **실패가 아니다.** */
  rules() { return this.#send('/api/instructions', { method: 'GET' }); }

  /** 늘 지킬 것을 적는다. 빈 글이면 지운다. */
  setRules(text) { return this.#send('/api/instructions', { body: { text } }); }

  /**
   * 가이드 — 여러 벌의, 껐다 켤 수 있는 규칙.
   *
   * `rules` 와 문이 갈린 이유는 실리는 방식이 달라서다: 저건 매 턴 통째로, 이건 모델이 부를 때만.
   */
  guides() { return this.#send('/api/guides', { method: 'GET' }); }
  guide(op, name, body) { return this.#send('/api/guide', { body: { op, name, body } }); }

  choose(socket, session) { return this.#send('/api/choose', { body: { socket, session } }); }
  submit(text) { return this.#send('/api/submit', { body: { text } }); }
  steer(text) { return this.#send('/api/steer', { body: { text } }); }
  interrupt() { return this.#send('/api/interrupt', { body: {} }); }
  permission(callId, decision) { return this.#send('/api/permission', { body: { callId, decision } }); }
  question(callId, text) { return this.#send('/api/question', { body: { callId, text } }); }

  /** 조작의 답을 올려 보낸다. 기다리는 사람이 없으면 헬퍼가 **410 으로 말한다**. */
  reply(payload) { return this.#send('/hand/reply', { body: payload }); }
}
