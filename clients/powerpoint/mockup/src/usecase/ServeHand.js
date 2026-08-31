/**
 * 손 노릇 — 헬퍼가 내려보낸 조작을 덱에서 수행하고 답을 올려 보낸다
 * (clients/powerpoint/DESIGN.md §5.1·§6).
 *
 * 이 유스케이스는 **Office.js 를 모른다.** 아는 것은 손(`HandPort`)이고, 그래서 같은 흐름이
 * PowerPoint 밖에서도 돈다 — 이 머신에 PowerPoint 가 없는 오늘 검증할 수 있는 유일한 길이다.
 */
export class ServeHand {
  /**
   * @param {{stream: object, api: object, hand: object, onNote?: (s:string)=>void}} deps
   */
  constructor({ stream, api, hand, onNote }) {
    this.stream = stream;
    this.api = api;
    this.hand = hand;
    this.onNote = onNote ?? (() => {});
    /** 시험이 보는 것: 실제로 수행한 조작 */
    this.served = [];
    this.off = null;
  }

  /** 스트림에 귀를 붙인다. 돌려주는 것은 떼는 함수. */
  start() {
    // **손에게 자기 문서 키를 알려 준다.** 손이 그것을 알아야 결과가 「실제로 손댄 문서」를
    // 스스로 실을 수 있다(§6). 헬퍼가 `hello` 로 주는 그 값이고, 여기서 넘기지 않으면 손은
    // 빈 문자열을 싣는다 — 그 빈 값이 위 `||` 가 막는 바로 그것이다.
    const hello = this.stream.on('hello', (d) => {
      if (this.hand && d?.document) this.hand.document = d.document;
      if (this.hand && d?.label) this.hand.labelText = d.label;
    });
    if (this.stream.document && this.hand) this.hand.document = this.stream.document;
    const call = this.stream.on('call', (req) => { void this.#run(req); });
    this.off = () => { hello(); call(); };
    return () => this.stop();
  }

  stop() { this.off?.(); this.off = null; }

  async #run(req) {
    if (!req || !req.id) return;
    const { id, op, args } = req;
    try {
      const out = await this.hand.run(op, args ?? {});
      this.served.push({ op, args: args ?? {} });
      await this.api.reply({
        id,
        // **실제로 손댄 문서**를 싣는다(§6). 스트림이 `hello` 로 준 그 키다.
        //
        // `??` 가 아니라 `||` 인 것이 이 줄의 전부다. 손이 문서를 **빈 문자열**로 돌려주면
        // `??` 는 그것을 값으로 치고 그대로 싣는데, 그러면 헬퍼가 라우팅할 키가 없어 답이
        // 404 로 떨어지고 **모델은 45초를 기다린 뒤 타임아웃을 받는다.** 실제로 PowerPoint 에
        // 처음 붙인 날 그 모양이 났다(2026-09-01): 조작은 내려갔고, 수행됐고, 답이 길을 잃었다.
        document: out?.document || this.stream.document || '',
        label: out?.label || '',
        result: out?.result ?? {},
        // 쓰기 결과가 **before→after 를 스스로 싣는다**(§4.4 ⑤·§7). 카운슬이 「이번 턴의
        // 편집」으로 받는 칸은 우리 턴에서 늘 비므로, 이 줄이 없으면 판정에 도달하는 것이
        // 아무것도 없다.
        changed: out?.changed ?? [],
        epoch: out?.epoch ?? 0,
        count: out?.count ?? 0,
      });
    } catch (e) {
      // **사유를 애드인 말로 올려 보낸다.** 헬퍼가 문장을 지어내면 Office.js 가 실제로 뭐라고
      // 했는지가 사라진다 — 그쪽 `HandReply.Error` 주석이 그 규칙이다.
      const why = e?.message ?? String(e);
      this.onNote(`조작 ${op} 이 실패했습니다: ${why}`);
      try {
        await this.api.reply({ id, document: this.stream.document ?? '', error: why });
      } catch (postErr) {
        // 답도 못 올렸다. **조용히 넘어가지 않는다** — 그러면 모델은 60초를 기다린다.
        this.onNote(`실패를 알리지도 못했습니다: ${postErr?.message ?? postErr}`);
      }
    }
  }
}
