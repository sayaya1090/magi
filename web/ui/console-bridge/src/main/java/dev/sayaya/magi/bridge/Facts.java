package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 창 전체가 같은 답을 들어야 하는 사실들 — 어느 magi인가(/console), 나는 무엇을 할 수 있나(/me).
 *
 * 셸이 한 번 읽어 창에 올리고, 화면들은 그것을 든다. 화면마다 제 회선으로 물으면 같은 질문이
 * 여러 번 나갈 뿐 아니라(마스트헤드와 지식 화면이 실제로 둘 다 /console을 물었다), 데모에서는
 * 그 화면 수만큼 목이 필요해진다 — 한 API의 주인은 하나여야 한다.
 */
public final class Facts {
    private static final String CONSOLE = "__magi_console";
    private static final String CONSOLE_OBS = "__magi_console_obs";

    private Facts() {}

    @JsFunction
    public interface NextFn { void call(Object value); }

    /** 셸: 이 콘솔이 누구인지 창에 올린다(없으면 올리지 않는다 — 모른다고 말하는 편이 낫다). */
    public static void putConsole(Object info) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (info != null) win.set(CONSOLE, info);
        Object obs = win.get(CONSOLE_OBS);
        if (obs != null) Js.<NextFn>cast(obs).call(info);
    }

    /** 화면: 지금 값을 받고, 뒤에 도착하면 한 번 더 받는다. */
    public static void onConsole(NextFn fn) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(CONSOLE_OBS, fn);
        if (win.has(CONSOLE)) fn.call(win.get(CONSOLE));
    }

    /** 셸: 이 사람이 무엇을 할 수 있는지 창에 올린다(May가 그것을 읽는다). */
    public static void putMay(Object caps) {
        if (caps != null) Js.asPropertyMap(DomGlobal.window).set("__magi_may", caps);
    }
}
