package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

/** 부모 없이 가운데만: 프레임을 만들고 가짜 포트가 물린 그래프로 mount한다. */
public class TestApplication implements EntryPoint {
    @Override
    public void onModuleLoad() {
        HTMLElement frame = Js.uncheckedCast(DomGlobal.document.createElement("main"));
        frame.id = "frame";
        DomGlobal.document.body.appendChild(frame);
        CodingTestComponent c = DaggerCodingTestComponent.create();
        HTMLElement left = Js.uncheckedCast(DomGlobal.document.createElement("div"));
        left.id = "leftslot";
        DomGlobal.document.body.appendChild(left);
        c.conversation().mount(frame);
        c.workspace().mount(left);
    }
}
