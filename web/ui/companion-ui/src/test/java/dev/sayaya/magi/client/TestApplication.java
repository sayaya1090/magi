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
        HTMLElement frame = Js.uncheckedCast(DomGlobal.document.createElement("main"));
        frame.id = "frame";
        DomGlobal.document.body.appendChild(frame);
        CompanionTestComponent c = DaggerCompanionTestComponent.create();
        if (dev.sayaya.magi.bridge.Windows.companionAimed()) c.companionElement().mount(frame);
        else c.fleetElement().mount(frame);
    }
}
