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
    }

    public HTMLElement element() { return element; }

    @Override
    public void mount(Object render) {
        Render r = Js.cast(render);
        r.onInvoke(element);
    }
}
