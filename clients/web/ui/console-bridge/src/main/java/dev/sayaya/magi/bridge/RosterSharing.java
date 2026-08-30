package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 명단 스트림의 창 브리지 — 창당 1스트림 규칙의 기전.
 *
 * 셸이 /events를 소유하고 host()로 두 문을 건다: 구독(현재값 재생 포함)과 재조회.
 * 화면 모듈은 subscribe/refresh만 안다 — 자기 EventSource를 열지 않는다. 셸 없이
 * 단독으로 뜬 모듈은 hosted()가 거짓이므로 제 회선으로 폴백한다(테스트 페이지가 그 경우).
 *
 * 구독자에게 가는 값은 파싱된 명단(JsArray) 또는 null("못 읽었다") — null도 흘린다:
 * 첫 로드의 실패는 화면이 말해야 한다.
 */
public final class RosterSharing {
    private static final String SUB = "__magi_roster_subscribe";
    private static final String REFRESH = "__magi_roster_refresh";

    private RosterSharing() {}

    @JsFunction
    public interface NextFn { void call(Object rosterOrNull); }

    @JsFunction
    public interface SubscribeFn { void call(NextFn cb); }

    @JsFunction
    public interface RefreshFn { void call(); }

    /** 셸 측: 스트림의 두 문을 건다. */
    public static void host(SubscribeFn sub, RefreshFn refresh) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(SUB, sub);
        win.set(REFRESH, refresh);
    }

    public static boolean hosted() { return Js.asPropertyMap(DomGlobal.window).has(SUB); }

    /** 화면 측: 명단을 듣는다. 호스트가 없으면 조용히 무시 — 폴백은 호출자의 몫. */
    public static void subscribe(NextFn cb) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(SUB)) return;
        Js.<SubscribeFn>cast(win.get(SUB)).call(cb);
    }

    /** 화면 측: 행동 뒤의 재조회 — 답은 구독으로 돌아온다. */
    public static void refresh() {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(REFRESH)) return;
        Js.<RefreshFn>cast(win.get(REFRESH)).call();
    }
}
