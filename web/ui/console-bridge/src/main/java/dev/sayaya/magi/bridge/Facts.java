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
    private static final String CONSOLE_AGAIN = "__magi_console_again";

    private Facts() {}

    @JsFunction
    public interface NextFn { void call(Object value); }

    @JsFunction
    public interface AgainFn { void call(); }

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

    /**
     * 셸: 이 사실들을 <b>다시 읽는 법</b>을 창에 둔다.
     *
     * 한 번 읽어 올리는 것이 규칙이지만, 그 사실이 낡는 순간이 있다 — 누가 데몬을 갱신하면
     * 이 콘솔이 이고 있는 빌드 번호는 그 자리에서 거짓이 된다. 갱신을 시킨 화면은 그 사실을
     * 알지만 어떻게 읽는지는 모른다(회선의 주인은 셸이다). 그래서 <b>다시 읽어라</b>만 건넨다.
     */
    public static void reloader(AgainFn read) {
        Js.asPropertyMap(DomGlobal.window).set(CONSOLE_AGAIN, read);
    }

    /** 화면: 지금 이고 있는 답이 낡았다 — 셸에게 다시 읽어 달라고 한다(없으면 조용히 만다). */
    public static void reread() {
        Object again = Js.asPropertyMap(DomGlobal.window).get(CONSOLE_AGAIN);
        if (again != null) Js.<AgainFn>cast(again).call();
    }

    /** 셸: 이 사람이 무엇을 할 수 있는지 창에 올린다(May가 그것을 읽는다). */
    public static void putMay(Object caps) {
        if (caps != null) Js.asPropertyMap(DomGlobal.window).set("__magi_may", caps);
    }
}
