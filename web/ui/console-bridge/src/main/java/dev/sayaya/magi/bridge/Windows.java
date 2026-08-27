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

    /** 주소가 컴패니언을 대고 있는가 — ?d= 가 그 표시다(운영 콘솔과 같은 주소). */
    public static boolean companionAimed() {
        String d = new URLSearchParams(DomGlobal.window.location.search).get("d");
        return d != null && !d.isEmpty();
    }
}
