package dev.sayaya.magi.demo;

import elemental2.dom.DomGlobal;
import elemental2.dom.Element;

/**
 * 창 맨 위의 띠에 한 줄 적는다 — 데모에서 <b>행동이 자기를 보고하는</b> 자리.
 *
 * 띠 자체는 페이지가 세운다(web/server/demo.go의 shim, `.demo-banner`). 목은 그 자리에
 * 글자만 갈아 끼운다. 계약은 그 클래스 이름 하나이고 양쪽 주석에 적혀 있다 — 띠가 없는
 * 페이지(테스트 하네스)에서는 조용히 아무 일도 하지 않는다.
 *
 * 왜 필요한가: 띠의 첫 줄이 "and every action reports what it would have sent"라고 약속한다.
 * 그 약속을 지키는 것이 여기다. 지키지 않으면 지우기·보내기 같은 버튼이 <b>조용히 성공한
 * 것처럼</b> 보이고, 그것은 진짜 콘솔에 대해 틀린 것을 가르친다(운영 목이 이 갈래를 둔 이유).
 */
final class Banner {
    private Banner() {}

    static void say(String line) {
        Element band = DomGlobal.document.querySelector(".demo-banner");
        if (band != null) band.textContent = "demo — " + line;
    }
}
