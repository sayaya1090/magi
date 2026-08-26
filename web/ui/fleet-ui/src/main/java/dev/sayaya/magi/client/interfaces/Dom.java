package dev.sayaya.magi.client.interfaces;

import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

/** 이 화면의 마크업 관용구 세 개 — cell()은 기존 콘솔의 것과 같은 뜻(div 하나, 클래스가 계약). */
public final class Dom {
    private Dom() {}

    public static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }

    public static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    /** 듣고 있는 독자를 위한 문구 — 배지 숫자의 뜻은 위치에 있고, 위치는 낭독되지 않는다. */
    public static HTMLElement srOnly(String text) {
        HTMLElement s = el("span");
        s.className = "sr-only";
        s.textContent = text;
        return s;
    }
}
