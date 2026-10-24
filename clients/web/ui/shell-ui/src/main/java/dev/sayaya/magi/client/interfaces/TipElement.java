package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Tips;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.Event;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 창에 하나뿐인 툴팁 — 화면들은 말만 적고(<code>data-tip</code>), 그리는 것은 sayaya-ui의
 * <code>md-tooltip</code>이다(단독 feedback 번들, <code>/vendor/sayaya-feedback.js</code>).
 *
 * <p>열고 닫는 규칙은 이제 그 컴포넌트의 것이다: 포인터·포커스로 열고(500ms), 떠나고 1.5초 뒤
 * 걷고, 손가락은 길게 누르기(500ms)로 열고, 창 안에 하나만 서고, top layer(popover)에 떠서
 * 숨겨진 부모(<code>visibility:hidden</code>인 .gitacts 같은)에도 가리지 않는다. 그 규칙들은 이
 * 콘솔이 손으로 들고 있던 것을 그대로 sayaya-ui에 옮긴 것이다(2.5.0.5).
 *
 * <p>여기 남은 일은 둘뿐이다. <b>어느 컨트롤을 가리키나</b> — md-tooltip은 앵커 하나에 묶이는데
 * 이 콘솔의 계약은 "말만 적으면 된다"라서, 창 캡처 리스너로 [data-tip] 요소에 들어오는 순간 그것을
 * 앵커로 넘긴다(캡처라 앵커 자신의 pointerenter·focusin·pointerdown보다 먼저 묶인다). 그리고
 * <b>주인이 사라지면 걷기</b> — 판은 폴마다 다시 그려지고, 치워진 노드는 pointerleave를 내지 않는다.
 */
@Singleton
public class TipElement {
    private final HTMLElement tip;
    private Element host = null;

    @Inject
    public TipElement() {
        tip = Js.uncheckedCast(DomGlobal.document.createElement("md-tooltip"));
        tip.classList.add("tip");
        // 읽어 주기는 컨트롤의 aria-label이 한다 — 이것은 그 설명의 그림이라 접근성 트리에서 뺀다.
        // id를 주지 않는 것도 같은 이유다: id가 있으면 md-tooltip이 앵커마다 aria-describedby를
        // 달아, 같은 말이 두 번 읽힌다.
        tip.setAttribute("aria-hidden", "true");
        listen();
    }

    public HTMLElement element() { return tip; }

    private void listen() {
        on("pointerover", e -> aim(hostOf(e)));
        on("focusin", e -> aim(hostOf(e)));
        on("pointerdown", e -> {
            aim(hostOf(e));
            // 누르는 순간은 설명이 아니라 행동이다 — 손가락은 예외(길게 누르기가 곧 여는 길).
            if (!"touch".equals(String.valueOf(Js.asPropertyMap(e).get("pointerType")))) hideTip(tip);
        });
        // 주인이 문서에서 빠졌으면 걷는다 — 사라진 노드는 아무에게도 나간다고 말하지 않는다.
        on("pointermove", e -> { if (host != null && !host.isConnected) unaim(); });
    }

    private void on(String type, elemental2.dom.EventListener fn) {
        DomGlobal.window.addEventListener(type, fn, true);
    }

    private void aim(Element h) {
        if (h == null) return;
        String text = h.getAttribute(Tips.ATTR);
        if (text == null || text.isEmpty()) { if (h == host) unaim(); return; }
        Js.asPropertyMap(tip).set("text", text);   // 같은 주인이어도 말은 바뀌었을 수 있다
        if (h == host) return;
        hideTip(tip);                              // 다른 컨트롤로 옮겼다 — 앞의 말은 그 컨트롤의 것
        host = h;
        Js.asPropertyMap(tip).set("anchor", h);
    }

    private void unaim() {
        hideTip(tip);
        host = null;
        Js.asPropertyMap(tip).set("anchor", null);
    }

    /** 이벤트가 난 자리에서 위로 올라가며 말이 붙은 것을 찾는다(없으면 null). */
    private Element hostOf(Event e) {
        Object t = Js.asPropertyMap(e).get("target");
        // 텍스트 노드가 목표인 이벤트도 있다 — closest가 없는 것에는 묻지 않는다.
        if (t == null || !Js.asPropertyMap(t).has("closest")) return null;
        return Js.<Element>uncheckedCast(t).closest("[" + Tips.ATTR + "]");
    }

    /** 번들이 아직(또는 끝내) 정의하지 않은 요소에도 안전하게. */
    private static native void hideTip(HTMLElement t) /*-{
        if (t && typeof t.hide === 'function') t.hide();
    }-*/;
}
