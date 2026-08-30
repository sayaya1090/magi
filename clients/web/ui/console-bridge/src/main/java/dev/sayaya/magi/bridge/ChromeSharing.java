package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 마스트헤드에 창의 손잡이를 놓을 자리 — 셸이 내주고, 화면이 채운다.
 *
 * 왜 화면이 아니라 창의 것인가: 손잡이가 여는 것은 그 화면의 기둥이지만, 손잡이 자체는
 * 이 창을 어떻게 배치할지에 대한 것이라 운영 콘솔도 그것을 열리는 기둥이 아니라 마스트헤드에
 * 둔다("the two pane handles, in the masthead rather than in the columns they open"). 그래서
 * 셸은 기둥이 무엇인지 모른 채 자리만 내주고, 그 자리를 아는 화면이 손잡이를 민다.
 *
 * 페더레이션이라 모듈끼리는 창 브리지로만 만난다({@link PaneSharing}과 같은 모양):
 * 셸이 {@code host}로 받는 이를 세우고, 화면이 {@code next}로 민다. 셸이 없으면(단독 테스트
 * 페이지) 미는 쪽은 조용히 무시된다 — 손잡이 없는 화면도 그 자체로 산다.
 */
public final class ChromeSharing {
    private ChromeSharing() {}

    /** 셸이 자리를 연다. render는 손잡이들이 앉을 상자를 받는다. */
    public static void host(Host host) {
        JsPropertyMap<Object> w = Js.asPropertyMap(DomGlobal.window);
        w.set("__magi_chrome", (Slot) render -> { host.mount(render); return true; });
    }

    /** 화면이 제 손잡이를 민다. 셸이 없으면 false. */
    public static boolean next(Object render) {
        JsPropertyMap<Object> w = Js.asPropertyMap(DomGlobal.window);
        Object slot = w.get("__magi_chrome");
        if (slot == null) return false;
        Js.<Slot>cast(slot).onInvoke(render);
        return true;
    }

    /** 자리가 비었는지 — 화면이 물러날 때 셸이 치울 수 있게 빈 렌더를 밀 수 있다. */
    public static void clear() {
        next((Render) (HTMLElement box) -> { box.replaceChildren(); return true; });
    }

    @FunctionalInterface
    @jsinterop.annotations.JsFunction
    public interface Slot { boolean onInvoke(Object render); }

    @FunctionalInterface
    public interface Host { void mount(Object render); }

    /**
     * 본문 위에 얹히는 것이 달라졌다 — 창 높이에 물린 기둥의 앵커를 다시 재 달라는 말.
     *
     * 폰의 탭 줄은 화면 모듈의 것이고, 그 앵커는 셸의 것이다. 화면이 그 값을 직접 쓰면 두 곳이
     * 같은 사실을 따로 계산하게 된다 — 여기서는 "바뀌었다"만 알린다.
     */
    private static final String REMEASURE = "__magi_chrome_remeasure";

    @jsinterop.annotations.JsFunction
    public interface Runner { void call(); }

    public static void hostRemeasure(Runner r) {
        Js.asPropertyMap(DomGlobal.window).set(REMEASURE, r);
    }

    public static void remeasure() {
        Object r = Js.asPropertyMap(DomGlobal.window).get(REMEASURE);
        if (r != null) Js.<Runner>cast(r).call();
    }
}