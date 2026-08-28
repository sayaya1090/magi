import { DeckPort } from '../port/DeckPort.js';

/**
 * DeckPort 를 Office.js 로 구현한다. **이 파일만 Office 를 안다.**
 *
 * ⚠ 이 머신에는 PowerPoint 가 없다. **`capabilities()` 만 돈다** — Office.js 를 안 부르고
 * `isSetSupported` 가 답한 것을 나르기만 해서 stub 위에서 나르는 계약을 실제로 잰다
 * (`tools/smoke.mjs`). `selection()` 과 `point()` 는 `PowerPoint.run` 이 필요해 **한 번도
 * 안 돌았다** — 여기 적힌 것은 문서를 읽고 쓴 것이고, S13·S14 를 재는 자리가 정확히 그 둘이다.
 * 돌려 보기 전까지 "된다"고 적지 않는다.
 */
export class OfficeDeck extends DeckPort {
  get label() { return 'PowerPoint (Office.js)'; }

  /**
   * 호스트에게 요구 집합을 **직접 묻는다.** `isSetSupported` 는 동기고 `PowerPoint.run` 밖에서
   * 돈다 — 덱을 안 건드리므로 이 계측이 사용자의 선택을 흔들 일이 없다.
   *
   * 무엇을 묻는지가 임의가 아니다. **여섯 다** 설계가 어딘가에서 기대는 값이다(아래 네 번째
   * 줄이 둘을 겹쳐 적는다):
   * - **1.2** — LTSC **2021** 의 천장. 이게 없으면 2021 에서 아래 넷이 전부 ✗ 로 나와서
   *   「LTSC 다」와 「그보다 아래 아무거나」가 화면에서 구분이 안 된다.
   * - **1.5** — LTSC **2024** 의 천장(부록 A 의 표: 1.3 부터가 2024 다). 여기서 멈추면
   *   §12 #4(LTSC 를 받을 것인가)가 실물로 답해진다.
   * - **1.6** — 하이퍼링크. **1.7** — customXmlParts.
   * - **1.8** — §3.3 이 고른 바닥.
   * - **SharedRuntime 1.1** — §5.7 의 전제. 이게 거짓이면 작업창을 닫는 순간 대화가 죽는다.
   *
   * 최고 지원 버전을 따로 세지 않고 **여섯을 다 그대로 돌려준다.** 요약하면 어디서 끊겼는지가
   * 사라지는데, 알고 싶은 것이 정확히 그 지점이다.
   *
   * ⚠ **말하는 것은 천장이지 SKU 가 아니다.** 오래된 리테일 빌드도 같은 자리에서 끊긴다
   * (표에서 1.5 는 리테일 2208). 바닥을 어디에 둘지에는 천장이면 충분하지만, 이 화면을
   * 「이 사람은 LTSC 다」로 읽으면 틀린다.
   */
  capabilities() {
    const req = (typeof Office !== 'undefined') && Office.context && Office.context.requirements;
    if (!req || typeof req.isSetSupported !== 'function') {
      return { measured: false, note: 'Office.context.requirements 가 없다', sets: [] };
    }
    const want = [
      ['PowerPointApi', '1.2'],
      ['PowerPointApi', '1.5'], ['PowerPointApi', '1.6'],
      ['PowerPointApi', '1.7'], ['PowerPointApi', '1.8'],
      ['SharedRuntime', '1.1'],
    ];
    const sets = want.map(([name, version]) => {
      // 낱개로 감싼다. 하나가 던져서 나머지를 잃으면 계측이 계측을 못 하는 꼴이 된다.
      let ok = null;
      try { ok = req.isSetSupported(name, version); } catch { ok = null; }
      return { name, version, ok };
    });
    return { measured: true, note: '', sets };
  }

  /** 이 호스트가 그 집합을 지원한다고 말하는가. 왕복이 없다 — `isSetSupported` 는 동기다. */
  #supports(name, version) {
    const req = (typeof Office !== 'undefined') && Office.context && Office.context.requirements;
    if (!req || typeof req.isSetSupported !== 'function') return false;
    try { return req.isSetSupported(name, version) === true; } catch { return false; }
  }

  async selection() {
    // 번호(`Slide.index`)는 **1.8**이라 §3.3 의 바닥과 같은 높이다. 바닥 아래 호스트에서도 창은
    // 열리므로(§3.3 — 매니페스트로 막지 않는다) 그런 데서 이 속성을 load 하면 **sync 가 통째로
    // 실패해 선택까지 잃는다.** 그래서 물어보고 넣는다 — 여기 왕복이 안 붙는 이유는 위 주석.
    const wantsNo = this.#supports('PowerPointApi', '1.8');
    return PowerPoint.run(async (context) => {
      // 슬라이드 신원부터. getSelectedSlides 는 **첫 항목이 활성 슬라이드**라고 문서가 보장한다.
      const slides = context.presentation.getSelectedSlides();
      slides.load(wantsNo ? 'items/id,items/index' : 'items/id');
      const shapes = context.presentation.getSelectedShapes();
      shapes.load('items/id,items/name,items/type,items/width,items/height');
      await context.sync();

      const slideId = slides.items[0]?.id ?? null;
      // 문서가 **zero-based** 라고 적는다. 사람에게 보여 줄 번호는 그래서 +1 이다.
      const idx = slides.items[0]?.index;
      const slideNo = (wantsNo && typeof idx === 'number') ? idx + 1 : null;
      if (shapes.items.length === 0) return { slideId, slideNo, shapes: [] };

      // 텍스트는 두 번째 왕복에서. 도형에 textFrame 이 없을 수 있어 낱개로 묻고,
      // 없으면 빈 문자열이다.
      const frames = shapes.items.map((s) => {
        const tf = s.textFrame;
        tf.textRange.load('text');
        return tf;
      });
      let texts;
      let textUnavailable = false;
      try {
        await context.sync();
        texts = frames.map((tf) => tf.textRange.text ?? '');
      } catch {
        // 텍스트가 없는 도형이 섞이면 통째로 실패할 수 있다. 그때는 텍스트를 포기하고
        // **신원은 살린다** — 인용의 몸은 신원이지 텍스트가 아니다.
        //
        // 다만 **포기했다는 사실을 실어 보낸다.** 빈 문자열로만 두면 「글이 없는 도형」과 값이
        // 같아지고, 그 인용은 모델에게 "이 상자는 비었다"고 말하는 셈이 된다.
        texts = shapes.items.map(() => '');
        textUnavailable = true;
      }

      return {
        slideId,
        slideNo,
        shapes: shapes.items.map((s, i) => ({
          id: s.id, name: s.name, type: s.type,
          width: s.width, height: s.height, text: texts[i], textUnavailable,
        })),
      };
    });
  }

  /**
   * 덱 전체의 번호표. 1.8 아래면 **null 이다** — 지어내지 않는다.
   *
   * 한 번 왕복한다. 안내가 도착할 때만 부르므로 누를 때마다 드는 값이 아니고, 여기서 캐시하지
   * 않는 이유는 **슬라이드 순서가 사용자 손에서 바뀌기 때문**이다. 낡은 번호는 없는 번호보다
   * 나쁘다.
   */
  async slideNumbers() {
    if (!this.#supports('PowerPointApi', '1.8')) return null;
    try {
      return await PowerPoint.run(async (context) => {
        const slides = context.presentation.slides;
        slides.load('items/id,items/index');
        await context.sync();
        return new Map(slides.items.map((s) => [s.id, s.index + 1]));
      });
    } catch {
      return null;   // 계측이 본 작업을 못 막는다 — 번호가 없으면 화면이 id 로 적는다
    }
  }

  async point(slideId, shapeIds) {
    return PowerPoint.run(async (context) => {
      // **슬라이드를 먼저 고르고 도형을 잡는다.** 안 보고 있는 슬라이드에서 setSelectedShapes 가
      // 도는지를 문서가 안 적으므로(S13) 두 번 부르는 쪽을 계약으로 둔다 — 한 번으로 되더라도
      // 두 번이 틀리지는 않는다.
      context.presentation.setSelectedSlides([slideId]);
      await context.sync();
      // ⚠ **여기가 `FakeDeck` 과 갈린다 — 안 고치고 적어 둔다.** 문은 빈 목록이면 잡은 것을
      // 놓으라고 하고(`DeckPort.point`) 가짜 덱은 그렇게 하는데, 이 줄은 놓지 않고 그냥 나간다.
      // 그러면 앞 안내의 도형이 그대로 잡힌 채 새 안내가 뜬다. 사유 없는 조기 이탈이라 아무도
      // 안 물었고, 이 함수는 오늘 한 번도 안 돌아서 아무도 못 봤다.
      //
      // 고치려면 `setSelectedShapes([])` 를 부르면 되는데, **그게 놓는지·던지는지·아무 일도
      // 안 하는지를 문서가 안 적는다**(§12 #9). 던지는 쪽이면 도형 없는 안내가 전부 실패로
      // 바뀐다 — 안 재고 바꾸면 도는 길을 못 도는 길로 만들 수 있어서, 재고 나서 고친다.
      if (!shapeIds || shapeIds.length === 0) return;
      const slide = context.presentation.slides.getItem(slideId);
      slide.setSelectedShapes(shapeIds);
      await context.sync();
    });
  }
}
