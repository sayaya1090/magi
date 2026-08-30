package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Tips;
import elemental2.dom.DOMRect;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.Event;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 창에 하나뿐인 툴팁 — 화면들은 말만 적고(<code>data-tip</code>), 그리는 것은 셸이다.
 *
 * <p>둘이 아니라 하나인 이유: 툴팁 둘은 한 물음에 두 답이다. 그래서 판은 하나고, 다른 것을
 * 보이는 일이 곧 앞의 것을 닫는 일이다(운영 page.js와 같은 판·같은 규칙).
 *
 * <p>여는 길이 셋이다. 포인터(호버), <b>포커스</b>, 그리고 손가락의 <b>길게 누르기</b>.
 * 가운데 것이 <code>title=</code>이 한 번도 하지 못하던 절반이다 — 아이콘만 있는 버튼으로
 * 탭해 온 사람에게는 브라우저의 툴팁이 뜨지 않는다. 마지막 것도 마찬가지다: 손가락에는
 * 호버가 없어서, 줄임표로 잘린 줄의 나머지 말을 볼 방법이 아예 없었다.
 */
@Singleton
public class TipElement {
    /** 나가는 데 주는 시간(운영과 같은 수, 가이드의 그 1.5초). */
    private static final int LEAVE_MS = 1500;
    /** 손가락이 "누르고 있다"가 되는 데 드는 시간 — 이보다 짧으면 탭이고 스크롤이다. */
    private static final int HOLD_MS = 500;
    /** 컨트롤의 모서리에서 띄우는 거리(가이드의 4dp). */
    private static final int GAP = 4;

    private final HTMLElement tip;
    private double leaving = -1;
    private Element host = null;
    private double holding = -1;
    private Element holdFor = null;

    @Inject
    public TipElement() {
        tip = Js.uncheckedCast(DomGlobal.document.createElement("span"));
        tip.id = "tip";
        // 읽어 주기는 하되 초점이 되지는 않는다 — 이것은 다른 것의 설명이지 그 자체가
        // 무엇인 적은 없다. hidden이면 접근성 트리에서도 빠지므로 aria-hidden은 겹이 아니다:
        // 뜬 동안에도 스크린리더는 이 판이 아니라 컨트롤의 aria-label을 읽는다.
        tip.setAttribute("role", "tooltip");
        tip.setAttribute("aria-hidden", "true");
        tip.hidden = true;
        listen();
    }

    public HTMLElement element() { return tip; }

    private void listen() {
        // 캡처로 듣는다 — 말이 붙은 컨트롤은 어느 화면의 것이든 되고, 그 화면이 이벤트를
        // 삼키더라도(md-* 컴포넌트가 종종 그런다) 창은 먼저 본다.
        on("pointerover", e -> { Element h = hostOf(e); if (h != null) show(h); });
        on("focusin", e -> { Element h = hostOf(e); if (h != null) show(h); });
        on("pointerout", e -> leave());
        on("focusout", e -> leave());
        // 툴팁이 제 버튼보다 오래 살았다. 판은 폴마다 다시 그려지므로 하필 그때 호버 중이던
        // 컨트롤은 <b>치워지는</b> 것이지 떠나는 것이 아니고, 사라진 노드는 pointerout을
        // 내지 않는다 — 아무도 나가라고 하지 않는다. 그래서 둘로 막는다: 주인이 문서에서
        // 빠졌으면 걷고, 어떤 이벤트가 왔든 포인터가 주인 밖이면 걷는다.
        on("pointermove", e -> {
            if (host == null) return;
            if (!host.isConnected || !host.contains(Js.uncheckedCast(target(e)))) leave();
        });
        // 누르는 순간은 설명이 아니라 행동이다 — 기다리지 않고 걷는다.
        on("pointerdown", e -> hide());
        // 손가락: 0.5초를 가만히 누르고 있으면 뜬다. 그 전에 움직이거나 떼면 취소라,
        // 스크롤이나 탭과 싸우지 않는다.
        on("touchstart", e -> {
            Element h = hostOf(e);
            if (h == null) return;
            holdFor = h;
            holding = DomGlobal.setTimeout(a -> {
                if (holdFor != null) show(holdFor);
                holding = -1;
                // 그리고 스스로 나간다. 손가락은 pointerout을 내지 않아서, 이렇게 뜬 판은
                // 다른 데를 탭할 때까지 페이지에 남아 있었다(실측: 제가 속한 상자가 닫힌
                // 뒤에도). 나갈 포인터가 없을 뿐 나가는 시간은 같다.
                leave();
            }, HOLD_MS);
        });
        on("touchmove", e -> drop());
        on("touchend", e -> drop());
        on("touchcancel", e -> drop());
    }

    private void on(String type, elemental2.dom.EventListener fn) {
        DomGlobal.window.addEventListener(type, fn, true);
    }

    /** 이벤트가 난 자리에서 위로 올라가며 말이 붙은 것을 찾는다(없으면 null). */
    private Element hostOf(Event e) {
        Element t = target(e);
        return t == null ? null : t.closest("[" + Tips.ATTR + "]");
    }

    private Element target(Event e) {
        Object t = Js.asPropertyMap(e).get("target");
        // 텍스트 노드가 목표인 이벤트도 있다 — closest가 없는 것에는 묻지 않는다.
        return t != null && Js.asPropertyMap(t).has("closest") ? Js.uncheckedCast(t) : null;
    }

    private void show(Element h) {
        String text = h.getAttribute(Tips.ATTR);
        if (text == null || text.isEmpty()) return;
        if (leaving >= 0) { DomGlobal.clearTimeout(leaving); leaving = -1; }
        host = h;
        tip.textContent = text;
        tip.hidden = false;
        place(h);
    }

    /**
     * 위가 기본이고, 자리가 없으면 아래로 뒤집는다. 가로는 컨트롤의 왼쪽에 맞추되 창 밖으로
     * 나가지 않게 민다 — 창 끝의 버튼에 붙은 긴 말이 잘리던 자리다.
     */
    private void place(Element h) {
        DOMRect r = h.getBoundingClientRect(), t = tip.getBoundingClientRect();
        double above = r.top - t.height - GAP;
        tip.style.setProperty("top", (above >= 0 ? above : r.bottom + GAP) + "px");
        double left = Math.max(GAP, Math.min(r.left, DomGlobal.window.innerWidth - t.width - GAP));
        tip.style.setProperty("left", left + "px");
    }

    /**
     * 나가는 셈을 시작한다 — 이미 세고 있으면 그대로 둔다.
     *
     * <p>이 한 줄이 없으면 마우스를 움직일 때마다 셈이 처음으로 돌아간다: 이 함수는
     * pointerout뿐 아니라 주인 밖에 떨어진 <b>모든</b> pointermove에서 불리기 때문이다.
     * 운영이 밟은 자리고(그때는 툴팁이 영영 안 나갔다), 같은 이유로 여기서도 멱등이다.
     */
    private void leave() {
        if (leaving >= 0) return;
        leaving = DomGlobal.setTimeout(a -> hide(), LEAVE_MS);
    }

    private void hide() {
        if (leaving >= 0) { DomGlobal.clearTimeout(leaving); leaving = -1; }
        tip.hidden = true;
        host = null;
    }

    private void drop() {
        if (holding >= 0) { DomGlobal.clearTimeout(holding); holding = -1; }
        holdFor = null;
    }
}
