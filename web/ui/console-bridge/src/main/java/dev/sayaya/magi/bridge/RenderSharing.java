package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * window 전역으로 GWT 모듈 간 Render를 공유하는 브리지 (handbook RenderSharing 이식).
 * 셸이 register()로 수신자를 걸고, 화면 모듈이 next()로 자기 Render를 민다.
 *
 * 화면 모듈 스크립트는 셸이 주입하므로 register가 항상 먼저다 — 그래도 next는
 * 수신자 부재 시 조용히 무시한다(스크립트를 단독 페이지에서 열어본 경우).
 */
public final class RenderSharing {
    private static final String KEY = "__magi_render";

    private RenderSharing() {}

    /** 셸 측: 화면 모듈의 렌더 요청을 받을 콜백 등록. */
    public static void register(NextFn observer) {
        Js.asPropertyMap(DomGlobal.window).set(KEY, observer);
    }

    /** 화면 모듈 측: 자기 Render를 셸로 전달. */
    public static void next(Object render) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(KEY)) return;
        Js.<NextFn>cast(win.get(KEY)).call(render);
    }

    @JsFunction
    public interface NextFn {
        void call(Object value);
    }
}
