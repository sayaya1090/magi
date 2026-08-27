package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.client.usecase.FrameView;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 화면이 앉는 자리 — <main>. page.css의 main 규칙(가로 중앙·페이지 캡·레일 거터)이
 * 그대로 이 요소를 입는다.
 */
@Singleton
public class FrameElement implements FrameView {
    private final HTMLElement element;

    @Inject
    public FrameElement() {
        element = Js.uncheckedCast(DomGlobal.document.createElement("main"));
        element.id = "frame";
        // page.css의 main은 바닥 여백을 calc(var(--dock, 매우 큰 기본값) + …)로 잡는다:
        // 운영은 컴포저 도크를 실측해 --dock에 넣고(measureDock), 이 셸엔 그 도크가 없어
        // 기본값이 남아 160px이 깔렸다(실측: 운영 32px 대 여기 160px). 도크가 없다는 것을
        // 말해 둔다 — 컴패니언 화면이 제 도크를 갖게 되면 그때 그 화면이 실측해 덮는다.
        DomGlobal.document.documentElement.style.setProperty("--dock", "0px");
    }

    public HTMLElement element() { return element; }

    @Override
    public void mount(Object render) {
        Render r = Js.cast(render);
        r.onInvoke(element);
    }
}
