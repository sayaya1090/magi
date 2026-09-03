import { DeckPort } from '../port/DeckPort.js';

/**
 * DeckPort 를 Office.js 로 구현한다. **이 파일만 Office 를 안다.**
 *
 * ⚠ 이 머신에는 PowerPoint 가 없다. `capabilities()` 는 Office.js 를 안 부르고
 * `isSetSupported` 가 답한 것을 나르기만 해서 stub 위에서 나르는 계약을 실제로 잰다
 * (`tools/smoke.mjs`). `selection()` 도 stub 위에서 돌지만 **그 stub 은 이 저장소가 문서를
 * 읽고 적은 흉내지 호스트가 아니다** — 거기서 잰 것은 우리가 고른 가지뿐이고(1.8 이 없으면
 * index 를 안 묻는다 / 빈 선택은 왕복 한 번 / 글을 잃어도 신원은 산다), 호스트가 실제로 어떻게
 * 답하는지는 안 재 봤다. `point()` 는 한 번도 안 돌았다. **S13·S14 는 둘 다 열려 있고**, 재는
 * 자리가 정확히 이 둘이다. 돌려 보기 전까지 "된다"고 적지 않는다.
 */
export class OfficeDeck extends DeckPort {
  get label() { return 'PowerPoint (Office.js)'; }

  /** 이건 그 호스트 자신이다 — 그래서 화면이 이름을 안 적는다(`DeckPort.isHost`). */
  get isHost() { return true; }

  /**
   * 호스트에게 요구 집합을 **직접 묻는다.** `isSetSupported` 는 동기고 `PowerPoint.run` 밖에서
   * 돈다 — 덱을 안 건드리므로 이 계측이 사용자의 선택을 흔들 일이 없다.
   *
   * 무엇을 묻는지가 임의가 아니다. **여섯 다** 설계가 어딘가에서 기대는 값이다(아래 네 번째
   * 줄이 둘을 겹쳐 적는다):
   * - **1.2** — LTSC **2021** 의 천장. 이게 없으면 2021 에서 아래 넷이 전부 ✗ 로 나와서
   *   「LTSC 다」와 「그보다 아래 아무거나」가 화면에서 구분이 안 된다.
   * - **1.5** — LTSC **2024** 의 천장(부록 A 의 표: 1.3 부터가 2024 다). 여기서 멈추면
   *   ~~§12 #4(LTSC 를 받을 것인가)가 실물로 답해진다.~~ **그 물음은 2026-08-29 에 닫혔다** —
   *   LTSC 는 별도 플러그인이다(`LTSC.md`). 이 줄이 지금 답하는 것은 설계가 아니라 **어느
   *   플러그인으로 보낼지**이고, 그래서 이 계측은 없애는 게 아니라 겨누는 곳이 바뀐다.
   * - **1.6** — 하이퍼링크. **1.7** — customXmlParts.
   * - **1.8** — §3.3 이 고른 바닥.
   * - **SharedRuntime 1.1** — §5.7 의 전제. 이게 거짓이면 작업창을 닫는 순간 대화가 죽는다.
   *
   * 최고 지원 버전을 따로 세지 않고 **여섯을 다 그대로 돌려준다.** 요약하면 어디서 끊겼는지가
   * 사라지는데, 알고 싶은 것이 정확히 그 지점이다.
   *
   * ⚠ **말하는 것은 천장이지 SKU 가 아니다.** 오래된 리테일 빌드도 같은 자리에서 끊긴다
   * (표에서 1.5 는 리테일 2208). 바닥을 어디에 둘지에는 천장이면 충분했지만, **보낼 곳을
   * 고르는 데는 안 충분하다** — 올리면 풀릴 리테일 사용자에게 플러그인을 깔라고 하게 된다.
   * 이 화면을 「이 사람은 LTSC 다」로 읽으면 틀리고, 사유 문구는 둘을 한 문구에 담는다(§3.3).
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
      //
      // 이 「안 적는다」는 **2026-08-29 에 페이지를 다시 읽어 확인한 것**이다. 바로 아래 빈
      // 배열 건이 「안 적는다」고 해 놓고 실은 적혀 있던 자리라 같은 페이지를 다시 봤고, 이쪽은
      // 진짜로 없었다. 대신 예제 하나가 **저장된 슬라이드 id 로 getItem 한 뒤 곧장** 도형을
      // 잡는 것이 나왔는데(부록 A), 한 번으로 될 가능성을 높일 뿐 결정하진 않는다.
      context.presentation.setSelectedSlides([slideId]);
      await context.sync();
      // **빈 목록도 여기까지 온다 — 그때는 잡은 것을 놓는다**(`DeckPort.point` 의 계약).
      // 조기 이탈이 아니라 빈 배열을 그대로 넘기는 것이 맞는데, 레퍼런스의 `shapeIds` 설명이
      // 그 경우를 적고 있기 때문이다: *"If the list is empty, the selection is cleared."*
      // (powerpoint.slide, `setSelectedShapes`, 1.8 모니커 · 2026-08-29 읽음). 놓아야 하는
      // 이유는 안 놓았을 때 서는 것이 **앞 안내의 도형**이라서다 — 캔버스가 「이 안내는 저
      // 도형에 대한 것」이라는 거짓을 말한다(§5.7 의 *남의 값이 부재의 자리에 앉는다*).
      //
      // 이 줄은 한 번 **사유 없는 조기 이탈**이었고, 그동안 이 문서 자신이 §6.1 에서는 「빈
      // 배열이면 선택 해제」라고 적고 §12 #9 에서는 「문서가 안 적는다」고 적고 있었다. 결정을
      // 산 쪽은 뒤엣말이었는데 틀린 쪽이 뒤엣말이었다 — **자기 문서 안에서 어긋난 두 문장 중
      // 결정을 사는 쪽부터 확인한다.**
      const slide = context.presentation.slides.getItem(slideId);
      slide.setSelectedShapes(shapeIds ?? []);
      await context.sync();
    });
  }
}
