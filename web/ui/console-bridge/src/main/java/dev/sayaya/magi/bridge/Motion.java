package dev.sayaya.magi.bridge;

import elemental2.dom.Element;

/**
 * 화면이 들어올 때의 움직임 — 클래스 하나로 시작하고, 규칙은 console.css의 것이다
 * (.enter=fadeThrough 200ms, .rise=riseIn 250ms, .slideL/.slideR=옆에서 들어옴).
 *
 * ⚠ 클래스를 <b>떼고, 레이아웃을 한 번 읽고, 다시 붙인다</b>. 이미 돌고 있는 애니메이션은
 * 같은 클래스를 다시 붙여도 다시 시작하지 않는다 — 그래서 두 번째 방문은 아무 움직임 없이
 * 도착한다(운영 reveal의 그 주석: offsetWidth를 읽는 것이 그 강제다).
 *
 * 숨은 요소에는 아무 일도 하지 않는다: 보이지 않는 것의 등장은 등장이 아니고, 그때 붙인
 * 클래스는 나중에 진짜로 보일 때 다시 붙지 않아 그 등장을 삼킨다.
 */
public final class Motion {
    /** 어느 방향에서 들어오는가 — 옆 자리끼리의 이동에만 쓴다(위아래는 등장이 아니라 층이다). */
    public static final String ENTER = "enter";
    public static final String RISE = "rise";
    public static final String FROM_RIGHT = "slideL";
    public static final String FROM_LEFT = "slideR";

    private Motion() {}

    public static void enter(Element el) { play(el, ENTER); }

    public static void play(Element el, String how) {
        if (el == null || el.hasAttribute("hidden")) return;
        // 자리를 지키는 것이라고 화면이 말해 두었으면 움직이지 않는다: 요약 칩처럼 화면이
        // 바뀌어도 같은 자리에 같은 것이 서 있는 줄은, 함께 들어오면 화면 전체가 깜빡인 것처럼
        // 읽힌다(운영은 목적지의 몸 하나만 들인다).
        if (el.hasAttribute("data-still")) return;
        el.classList.remove(ENTER, RISE, FROM_RIGHT, FROM_LEFT);
        reflow(el);
        el.classList.add(how == null || how.isEmpty() ? ENTER : how);
    }

    /**
     * 레이아웃을 읽어 방금 뗀 클래스를 브라우저가 알아채게 한다(운영의 `void el.offsetWidth`).
     * 자바로 쓰면 쓰이지 않는 읽기라 컴파일러가 지울 수 있어, 그 한 줄만 JS로 둔다.
     */
    private static native void reflow(Element el) /*-{
        void el.offsetWidth;
    }-*/;
}
