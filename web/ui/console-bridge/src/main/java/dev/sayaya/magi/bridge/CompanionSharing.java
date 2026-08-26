package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * "지금 어느 컴패니언을 보고 있나"의 창 브리지 — handbook UriSharing의 자리.
 *
 * 셸이 host()로 문을 걸고 next()로 CompanionContext(socket·peer·type)를 민다. 화면 모듈은
 * subscribe()만 안다 — 구독은 현재값을 재생하므로, 모듈이 셸보다 늦게 로드돼도(늘 그렇다)
 * 첫 컨텍스트를 놓치지 않는다. null은 "컴패니언 화면이 아니다"이고, 그 화면의 렌더는
 * 셸이 다시 부르지 않으므로 모듈은 null이면 그리던 것을 세워 두기만 하면 된다.
 */
public final class CompanionSharing {
    private static final String SUB = "__magi_companion_subscribe";

    private CompanionSharing() {}

    @JsFunction
    public interface NextFn { void call(CompanionContext ctxOrNull); }

    @JsFunction
    public interface SubscribeFn { void call(NextFn cb); }

    /** 셸 측: 구독의 문을 건다. 구독자 명부와 현재값 재생은 넘긴 구현의 몫이다. */
    public static void host(SubscribeFn sub) {
        Js.asPropertyMap(DomGlobal.window).set(SUB, sub);
    }

    public static boolean hosted() { return Js.asPropertyMap(DomGlobal.window).has(SUB); }

    /** 화면 측: 컨텍스트를 듣는다. 호스트가 없으면 조용히 무시 — 폴백은 호출자의 몫. */
    public static void subscribe(NextFn cb) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(SUB)) return;
        Js.<SubscribeFn>cast(win.get(SUB)).call(cb);
    }
}
