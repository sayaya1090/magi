package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

/**
 * 셸 없이 이 모듈만: 주소가 컴패니언을 대면 상세(패널), 아니면 목록 — 프로덕션과 같은
 * 판단을 같은 자리에서 한다(Windows.companionAimed).
 */
public class TestApplication implements EntryPoint {
    @Override
    public void onModuleLoad() {
        // 셸이 하는 두 가지를 흉내 낸다: 화면이 앉을 <main>과, 화면이 제 손잡이를 놓을
        // 마스트헤드의 자리. 손잡이가 여는 것이 무엇인지는 셸도 이 하네스도 모른다.
        HTMLElement masthead = Js.uncheckedCast(DomGlobal.document.createElement("header"));
        masthead.id = "masthead";
        HTMLElement chrome = Js.uncheckedCast(DomGlobal.document.createElement("span"));
        chrome.id = "chrome";
        masthead.appendChild(chrome);
        DomGlobal.document.body.appendChild(masthead);
        dev.sayaya.magi.bridge.ChromeSharing.host(render ->
                Js.<dev.sayaya.magi.bridge.Render>cast(render).onInvoke(chrome));
        HTMLElement frame = Js.uncheckedCast(DomGlobal.document.createElement("main"));
        frame.id = "frame";
        DomGlobal.document.body.appendChild(frame);
        // 셸의 세 번째 몫: 지금 서 있는 곳을 body에 적는다. 손잡이가 컴패니언 페이지에서만
        // 보이는 것도(console.css), 폰에서 하단 바가 물러나는 것도 이 한 글자에 달려 있다.
        if (dev.sayaya.magi.bridge.Windows.companionAimed())
            DomGlobal.document.body.setAttribute("at", "agent");
        CompanionTestComponent c = DaggerCompanionTestComponent.create();
        if (dev.sayaya.magi.bridge.Windows.companionAimed()) c.companionElement().mount(frame);
        else c.fleetElement().mount(frame);
    }
}
