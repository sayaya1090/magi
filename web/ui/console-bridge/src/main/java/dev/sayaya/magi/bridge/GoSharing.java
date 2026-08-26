package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 화면이 셸에 이동을 청하는 문 — 플릿의 행과 레일 2단의 항목이 컴패니언 화면으로 가는 길.
 *
 * 주소(pushState)는 셸의 것이라 화면이 직접 만지지 않는다. 호스트가 없으면(단독 테스트
 * 페이지) go는 조용히 아무 일도 하지 않는다 — 링크가 죽는 게 아니라 문이 없는 것이고,
 * hosted()로 물어 링크 모양 자체를 접을 수 있다.
 */
public final class GoSharing {
    private static final String GO = "__magi_go";

    private GoSharing() {}

    @JsFunction
    public interface GoFn { void call(String socket, String peer); }

    /** 셸 측: 이동의 문을 건다. */
    public static void host(GoFn go) {
        Js.asPropertyMap(DomGlobal.window).set(GO, go);
    }

    public static boolean hosted() { return Js.asPropertyMap(DomGlobal.window).has(GO); }

    /** 화면 측: 컴패니언 화면으로. peer는 없으면 null. */
    public static void go(String socket, String peer) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(GO)) return;
        Js.<GoFn>cast(win.get(GO)).call(socket, peer);
    }
}
