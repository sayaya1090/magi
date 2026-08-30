package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

/**
 * 셸 없이 화면만. 능력(/me)은 셸이 창에 올려 두는 사실이라, 하네스가 그 자리에 "전부"를
 * 올려 둔다 — 1인 콘솔이 실제로 그렇게 답한다.
 */
public class TestApplication implements EntryPoint {
    @Override
    public void onModuleLoad() {
        Js.asPropertyMap(DomGlobal.window).set("__magi_may",
                elemental2.core.Global.JSON.parse("[\"read\",\"answer\",\"prompt\",\"configure\",\"shell\",\"admin\"]"));
        HTMLElement frame = Js.uncheckedCast(DomGlobal.document.createElement("main"));
        frame.id = "frame";
        DomGlobal.document.body.appendChild(frame);
        DaggerSettingsTestComponent.create().settingsElement().mount(frame);
    }
}
