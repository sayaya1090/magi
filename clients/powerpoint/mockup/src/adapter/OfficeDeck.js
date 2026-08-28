import { DeckPort } from '../port/DeckPort.js';

/**
 * DeckPort 를 Office.js 로 구현한다. **이 파일만 Office 를 안다.**
 *
 * ⚠ 이 머신에는 PowerPoint 가 없어 **한 번도 안 돌려 봤다.** 목업의 나머지는 FakeDeck 으로
 * 확인했고 이쪽은 못 했다. 그러니 여기 적힌 것은 문서를 읽고 쓴 것이고, S13·S14 를 재는 자리가
 * 정확히 이 파일이다. 돌려 보기 전까지 "된다"고 적지 않는다.
 */
export class OfficeDeck extends DeckPort {
  get label() { return 'PowerPoint (Office.js)'; }

  async selection() {
    return PowerPoint.run(async (context) => {
      // 슬라이드 신원부터. getSelectedSlides 는 **첫 항목이 활성 슬라이드**라고 문서가 보장한다.
      const slides = context.presentation.getSelectedSlides();
      slides.load('items/id');
      const shapes = context.presentation.getSelectedShapes();
      shapes.load('items/id,items/name,items/type,items/width,items/height');
      await context.sync();

      const slideId = slides.items[0]?.id ?? null;
      if (shapes.items.length === 0) return { slideId, shapes: [] };

      // 텍스트는 두 번째 왕복에서. 도형에 textFrame 이 없을 수 있어 낱개로 묻고, 없으면 빈 문자열이다.
      const frames = shapes.items.map((s) => {
        const tf = s.textFrame;
        tf.textRange.load('text');
        return tf;
      });
      let texts;
      try {
        await context.sync();
        texts = frames.map((tf) => tf.textRange.text ?? '');
      } catch {
        // 텍스트가 없는 도형이 섞이면 통째로 실패할 수 있다. 그때는 텍스트를 포기하고
        // **신원은 살린다** — 인용의 몸은 신원이지 텍스트가 아니다.
        texts = shapes.items.map(() => '');
      }

      return {
        slideId,
        shapes: shapes.items.map((s, i) => ({
          id: s.id, name: s.name, type: s.type,
          width: s.width, height: s.height, text: texts[i],
        })),
      };
    });
  }

  async point(slideId, shapeIds) {
    return PowerPoint.run(async (context) => {
      // **슬라이드를 먼저 고르고 도형을 잡는다.** 안 보고 있는 슬라이드에서 setSelectedShapes 가
      // 도는지를 문서가 안 적으므로(S13) 두 번 부르는 쪽을 계약으로 둔다 — 한 번으로 되더라도
      // 두 번이 틀리지는 않는다.
      context.presentation.setSelectedSlides([slideId]);
      await context.sync();
      if (!shapeIds || shapeIds.length === 0) return;
      const slide = context.presentation.slides.getItem(slideId);
      slide.setSelectedShapes(shapeIds);
      await context.sync();
    });
  }
}
