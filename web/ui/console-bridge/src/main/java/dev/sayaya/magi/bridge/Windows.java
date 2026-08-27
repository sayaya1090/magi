package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.URLSearchParams;

/**
 * 창이 이미 아는 사실 몇 가지 — 주소가 그 원본이다.
 *
 * 모듈은 저마다 컴파일되지만 주소는 창에 하나라, "지금 컴패니언을 보고 있나" 같은 질문은
 * 브리지를 기다릴 것 없이 여기서 답한다(셸이 흘리는 컨텍스트는 그 뒤 자세한 것을 준다).
 */
public final class Windows {
    private Windows() {}

    /**
     * 주소가 실은 한 조각 — 화면이 제 것을 읽는다(회의실의 ?m= 처럼).
     *
     * 셸을 거치지 않는 이유: 주소는 창에 하나뿐이고, 그 값이 무엇을 뜻하는지는 그 화면만
     * 안다. 셸은 어느 화면인지(v=)까지만 알면 된다.
     */
    public static String query(String name) {
        String v = new URLSearchParams(DomGlobal.window.location.search).get(name);
        return v == null ? "" : v;
    }

    /**
     * 지금 기둥이 <b>하나뿐인가</b>(폰) — 배치를 아는 쪽(부모)이 답하고, 화면은 묻기만 한다.
     *
     * 화면이 제 미디어 질의를 쓰면 기준 폭이 화면마다 흩어지고, 부모가 탭으로 판을 가르는 폭과
     * 어긋나는 순간 "한 기둥인 줄 알고 접은 판"이 넓은 화면에서 사라진다. 아무도 답하지 않으면
     * 거짓 — 부모 없이 뜬 테스트 페이지는 늘 넓은 화면이다.
     */
    public static boolean onePane() {
        Object v = jsinterop.base.Js.asPropertyMap(DomGlobal.window).get("__magi_one_pane");
        return v != null && jsinterop.base.Js.isTruthy(v);
    }

    /** 부모: 그 사실이 바뀌면 적어 둔다(구독은 화면이 제 렌더에서 다시 묻는 것으로 족하다). */
    public static void onePane(boolean now) {
        jsinterop.base.Js.asPropertyMap(DomGlobal.window).set("__magi_one_pane", now);
    }

    /** 주소가 컴패니언을 대고 있는가 — ?d= 가 그 표시다(운영 콘솔과 같은 주소). */
    public static boolean companionAimed() {
        String d = new URLSearchParams(DomGlobal.window.location.search).get("d");
        return d != null && !d.isEmpty();
    }
}
