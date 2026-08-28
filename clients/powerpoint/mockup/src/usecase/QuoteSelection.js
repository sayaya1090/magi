import { Quote } from '../domain/Quote.js';

/**
 * 선택을 인용으로 바꾼다 — §5.8 의 두 번째 걸음.
 *
 * **특수키+클릭은 못 본다.** 애드인 JS 는 작업창 웹뷰에서 돌고 캔버스의 입력이 거기 안 온다.
 * 그래서 사용자가 **인용을 누르는 순간**을 이벤트로 삼고 그때 덱에 물어본다. 없는 것은 푸시고
 * 있는 것은 풀이다.
 *
 * 이 유스케이스가 곧 **S14 의 측정기**다. 다만 누를 때 한 번 읽어서는 못 잰다 — 빈 선택은
 * "애초에 안 골랐다"와 "포커스가 작업창으로 오면서 날아갔다"가 **화면에서 똑같이 생겼고**,
 * 누른 뒤에는 이미 포커스가 옮겨진 뒤라 둘을 가를 근거가 남아 있지 않다. 그래서 읽기를 **둘**로
 * 나눈다:
 *
 * - `sampleBeforeFocus()` — 포인터가 단추에 **들어올 때**. 호버는 포커스를 안 옮기므로 이 값이
 *   「포커스가 가기 전」의 선택이다.
 * - `run()` — 눌린 뒤. 여기서 비었는데 앞의 읽기가 안 비었으면 **가져간 것은 포커스다.**
 *
 * ⚠ **전제가 하나 있고 그게 틀리면 이 계측이 눈을 감는다** — 호버가 포커스를 안 옮긴다는 것.
 * PowerPoint 작업창 웹뷰에서 그런지는 **안 재 봤다**(이 머신에 PowerPoint 가 없다). 틀리면
 * 앞 읽기도 비어서 `none` 이 나오는데, 그건 「안 골랐다」와 구분이 안 되는 옛 상태로 돌아가는
 * 것이지 없는 사실을 지어내지는 않는다.
 *
 * 앞 읽기가 아예 없는 길도 있다 — 단축키·키보드로 누르면 포인터가 단추에 들어온 적이 없다.
 * 그때는 `none` 이 아니라 **`unknown`** 이다. 모르는 것을 「안 골랐다」로 적으면 그게 바로
 * 이 계측이 없애려던 그 뭉갬이다.
 *
 * 앞 읽기는 **왕복**이라 누름보다 늦게 도착할 수 있다. 그래서 세대(`epoch`)를 센다 — 읽는 동안
 * 눌렸으면 그 읽기는 이미 「누르기 전」이 아니므로 **버린다.** 안 버리면 늦게 온 값이 다음
 * 누름의 앞 읽기 자리에 앉아 `lostFocus` 를 지어낸다(빨리 누르면 실제로 그랬다).
 */
export class QuoteSelection {
  constructor(deck, conversation) {
    this.deck = deck;
    this.conversation = conversation;
    this.beforeFocus = null;   // null = 「누르기 전」 읽기가 없다(모른다)
    this.epoch = 0;            // 누름 세대. 늦게 온 읽기가 자기 세대를 확인하는 데 쓴다.
  }

  /**
   * 누르기 **전**의 읽기. 포인터가 단추에 들어올 때마다 부른다.
   *
   * 매번 덮어쓰는 것이 곧 신선도 관리다 — 캔버스에 갔다가 돌아오면 포인터가 다시 들어오면서
   * 새 값이 앉는다. 덱을 안 건드리는 읽기라 이 계측이 사용자의 선택을 흔들지 않는다.
   */
  async sampleBeforeFocus() {
    const mine = this.epoch;
    try {
      const sel = await this.deck.selection();
      if (mine !== this.epoch) return;   // 읽는 사이에 눌렸다 — 이건 「누르기 전」이 아니다
      this.beforeFocus = { count: sel?.shapes?.length ?? 0 };
    } catch {
      if (mine === this.epoch) this.beforeFocus = null;   // 계측이 본 작업을 막지 않는다
    }
  }

  /**
   * @returns {Promise<{added:Quote[], skipped:number, empty:boolean, beforeCount:number,
   *                    reason:('none'|'lostFocus'|'unknown'|'readFailed'|null)}>}
   *
   * ⚠ 빈 답 두 갈래의 `added: []` 와 `skipped: 0` 은 **일부러 시험이 안 문다.** 부르는 쪽이
   * `if (empty) … return;` 으로 먼저 빠지므로 그 두 칸에는 읽는 이가 없고(`view.onQuote`),
   * 읽는 이 없는 칸에 세우는 단언은 계약이 아니라 구현 베끼기다. 그래도 **지우지는 않는다** —
   * 모양을 갈래마다 다르게 하면 `empty` 를 안 보고 부른 쪽이 0 대신 예외를 받는다. 대신
   * 문을 여는 `empty` 자체는 문다(위 갈래마다 하나씩).
   */
  async run() {
    this.epoch += 1;           // 아직 안 온 앞 읽기를 여기서 무효로 만든다
    const before = this.beforeFocus;
    this.beforeFocus = null;   // 한 번 쓰고 버린다 — 낡은 읽기가 다음 누름에 새면 거짓 진단이 된다
    const beforeCount = before?.count ?? 0;

    let sel;
    try {
      sel = await this.deck.selection();
    } catch {
      // **던진 것도 답이다** — "못 읽었다". 여기서 위로 새게 두면 단추는 조용히 죽고(누른
      // 사람에게는 아무 일도 안 일어난다), 사람은 자기가 도형을 안 골랐다고 생각한다.
      // 아래 세 사유가 서로 다른 말인 것과 같은 이유로, 이것도 제 이름으로 올라가야 한다.
      return { added: [], skipped: 0, empty: true, reason: 'readFailed', beforeCount };
    }
    const { slideId, slideNo, shapes } = sel;
    if (!shapes || shapes.length === 0) {
      const reason = before === null ? 'unknown' : (beforeCount > 0 ? 'lostFocus' : 'none');
      return { added: [], skipped: 0, empty: true, reason, beforeCount };
    }
    const added = [];
    let skipped = 0;
    for (const s of shapes) {
      const q = new Quote({
        slideId,
        slideNo,
        shapeId: s.id,
        name: s.name,
        type: s.type,
        text: s.text,
        textUnavailable: s.textUnavailable,
        width: s.width,
        height: s.height,
      });
      if (this.conversation.attach(q)) added.push(q);
      else skipped += 1;
    }
    return { added, skipped, empty: false, reason: null, beforeCount };
  }
}
