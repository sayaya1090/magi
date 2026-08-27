package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 내가 무엇을 해도 되는가 — 창에 하나로 둔다.
 *
 * 모듈마다 static이 따로라(페더레이션) 각자 /me를 받으면 창 하나에서 여러 번이다. 그래서
 * 먼저 읽은 쪽이 창에 올리고(`__magi_may`), 뒤에 오는 모듈은 그것을 든다 — 언어 팩과 같은
 * 규칙이다.
 *
 * 아무도 설정되지 않은 콘솔은 "전부"라고 답하고, 그때 이 클래스는 아무것도 바꾸지 않는다:
 * 1인 콘솔이 청하지 않은 권한 모델을 얻으면 안 된다. **게이트는 언제나 서버가 진다** —
 * 여기서 하는 일은 눌러서 거절에 닿는 컨트롤을 그리지 않는 것뿐이다.
 */
public final class May {
    private static final String SHARED = "__magi_may";

    private May() {}

    /** 능력 목록을 한 번 읽어 창에 올린다 — 이미 있으면 회선을 타지 않는다. */
    public static void load(Runnable done) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (win.has(SHARED)) { done.run(); return; }
        Console.fetchList("/me", parsed -> {
            if (parsed != null) {
                Object caps = Js.asPropertyMap(parsed).get("can");
                if (caps != null) win.set(SHARED, caps);
            }
            done.run();
        });
    }

    /**
     * 그 능력이 있는가. 아직 못 읽었으면 참이다 — 그려진 대로 두고 서버가 거절하게 한다
     * (읽는 중이라는 이유로 컨트롤이 깜빡이며 사라지는 편이 나쁘다).
     */
    public static boolean can(String cap) {
        if (cap == null || cap.isEmpty()) return true;
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(SHARED)) return true;
        JsArrayLike<Object> caps = Js.uncheckedCast(win.get(SHARED));
        for (int i = 0; i < caps.getLength(); i++) {
            if (cap.equals(String.valueOf(caps.getAt(i)))) return true;
        }
        return false;
    }

    /** 읽힌 뒤에 다시 그리고 싶을 때 — 지금 값으로 한 번, 도착하면 한 번 더 부른다. */
    public static void observe(Consumer<Boolean> ready) {
        ready.accept(Js.asPropertyMap(DomGlobal.window).has(SHARED));
        load(() -> ready.accept(true));
    }
}
