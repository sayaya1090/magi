package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.base.Js;

/**
 * 지금은 데모인가 — 페이지가 한 번 적어 두는 사실 하나(window.MAGI_DEMO).
 *
 * 데모는 정적 파일이라 뒤에 데몬이 없다. 그때 화면이 무엇을 답할지는 <b>그 화면의 몫</b>이다:
 * 모듈마다 제 목(Demo*Source)을 싣고, 데모 모드면 회선 대신 그것을 문다. 페이지가 fetch를
 * 갈아끼워 모듈에 밀어 넣는 방식이 아닌 이유는 배포가 그렇게 돌지 않기 때문이다 — 화면은
 * 저마다 컴파일돼 저마다의 주기로 배포되고, 제 창에서 제 회선으로 나간다. 목이 그 모듈과
 * 함께 실려야 그 배포에 따라붙는다.
 */
public final class Demo {
    private Demo() {}

    public static boolean on() {
        return Js.isTruthy(Js.asPropertyMap(DomGlobal.window).get("MAGI_DEMO"));
    }
}
