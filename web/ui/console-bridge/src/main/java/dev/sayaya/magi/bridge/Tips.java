package dev.sayaya.magi.bridge;

import elemental2.dom.Element;

/**
 * 아이콘만 있는 것에 붙이는 한 줄 — 셸이 그린다.
 *
 * <p>이 콘솔의 컨트롤 상당수는 그림 하나뿐이다(⌘K·톱니·다시 읽기·커밋·나가기). 그림이
 * 무엇인지 아는 사람에게는 그것으로 족하지만, 처음 보는 사람에게는 아무 말도 아니다.
 * 지금까지 그 자리에 쓰던 <code>title=</code>은 절반만 하던 답이다: 브라우저의 것이라 시점도
 * 자리도 정할 수 없고, <b>키보드로 탭해 온 사람에게는 아무것도 뜨지 않으며</b>, 손가락에게도
 * 여는 법이 없다(운영 page.js의 그 주석).
 *
 * <p>그래서 창에 판 하나를 두고(셸의 <code>#tip</code>), 화면들은 <b>말만</b> 적는다. 두
 * 가지를 같이 적지 않는 것이 규칙이다 — <code>title=</code>과 나란히 적으면 같은 컨트롤에
 * 툴팁이 <b>둘</b> 그려진다(운영이 밟은 자리).
 *
 * <p>셸 없이 뜬 화면(모듈 단독 테스트 페이지)에서는 속성만 남고 아무것도 그려지지 않는다 —
 * 다른 문들과 같은 규칙이고, 그래서 화면 코드에 "셸이 있느냐"는 분기가 없다.
 */
public final class Tips {
    /** 셸이 읽는 이름. 스타일도 이 이름으로 걸린다(운영 page.css의 <code>#tip</code>). */
    public static final String ATTR = "data-tip";

    private Tips() {}

    /** 이 컨트롤의 한 줄. 빈 말은 붙이지 않는다 — 빈 상자가 뜨는 것이 답이 아니다. */
    public static void on(Element el, String text) {
        if (el == null) return;
        if (text == null || text.isEmpty()) el.removeAttribute(ATTR);
        else el.setAttribute(ATTR, text);
    }

    /** 말을 거둔다(그 컨트롤이 더는 설명할 것이 없어졌을 때). */
    public static void off(Element el) {
        if (el != null) el.removeAttribute(ATTR);
    }
}
