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
    private static final String VIEW = "__magi_go_view";
    private static final String PAST = "__magi_go_past";

    private GoSharing() {}

    @JsFunction
    public interface GoFn { void call(String socket, String peer); }

    @JsFunction
    public interface ViewFn { void call(String view); }

    /** 셸 측: 이동의 문을 건다. */
    public static void host(GoFn go) {
        Js.asPropertyMap(DomGlobal.window).set(GO, go);
    }

    /** 셸 측: 카탈로그 화면(v=…)으로의 문 — 보드처럼 문 없는 주소도 화면이 청할 수 있게. */
    public static void hostView(ViewFn view) {
        Js.asPropertyMap(DomGlobal.window).set(VIEW, view);
    }

    /** 셸 측: 지난 일 층위(?past=)의 문 — null=지금 대화로, ""=목록, 값=그 세션. */
    public static void hostPast(ViewFn past) {
        Js.asPropertyMap(DomGlobal.window).set(PAST, past);
    }

    /** 화면 측: 지난 일 층위로. 주소는 셸의 것이라 여기로 청한다. */
    public static void past(String pastOrNull) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(PAST)) return;
        Js.<ViewFn>cast(win.get(PAST)).call(pastOrNull);
    }

    /** 화면 측: 카탈로그 화면으로. 호스트가 없으면 조용히 무시 — 앵커의 href가 폴백이다. */
    public static void view(String v) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(VIEW)) return;
        Js.<ViewFn>cast(win.get(VIEW)).call(v);
    }

    public static boolean hosted() { return Js.asPropertyMap(DomGlobal.window).has(GO); }

    /** 화면 측: 컴패니언 화면으로. peer는 없으면 null. */
    public static void go(String socket, String peer) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(GO)) return;
        Js.<GoFn>cast(win.get(GO)).call(socket, peer);
    }
}
