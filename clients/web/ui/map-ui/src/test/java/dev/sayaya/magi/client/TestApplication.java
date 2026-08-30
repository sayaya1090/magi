package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

/** 셸 없이 화면만: 프레임을 만들고 가짜 포트가 물린 그래프로 mount한다. */
public class TestApplication implements EntryPoint {
    @Override
    public void onModuleLoad() {
        HTMLElement frame = Js.uncheckedCast(DomGlobal.document.createElement("main"));
        frame.id = "frame";
        DomGlobal.document.body.appendChild(frame);
        DaggerMapTestComponent.create().mapElement().mount(frame);
    }
}
