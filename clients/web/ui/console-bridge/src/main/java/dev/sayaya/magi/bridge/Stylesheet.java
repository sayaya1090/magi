package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLLinkElement;
import jsinterop.base.Js;

/**
 * 모듈이 제 스타일시트를 스스로 단다 — 셸의 console.html은 셸이 아는 것만 링크하고,
 * 화면 모듈은 셸보다 뒤에 오기 때문이다. 한 창에서 한 번만: id로 이미 걸렸는지 본다.
 */
public final class Stylesheet {
    private Stylesheet() {}

    /** /ui/<name>.css 를 문서 머리에 건다(이미 있으면 아무 일도 하지 않는다). */
    public static void ensure(String name) {
        String id = "css-" + name;
        if (DomGlobal.document.getElementById(id) != null) return;
        HTMLLinkElement link = Js.uncheckedCast(DomGlobal.document.createElement("link"));
        link.id = id;
        link.rel = "stylesheet";
        link.href = "/ui/" + name + ".css";
        DomGlobal.document.head.append(link);
    }
}
