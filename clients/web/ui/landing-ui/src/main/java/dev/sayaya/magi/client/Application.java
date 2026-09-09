package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.client.interfaces.LandingElement;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

/**
 * 랜딩 페이지의 진입점.
 *
 * 콘솔의 화면들은 셸이 건네는 프레임에 마운트하지만(RenderSharing), 여기에는 셸이 없다 —
 * 이 모듈이 그 페이지의 전부다. 그래서 자리를 창에서 직접 찾고, 없으면 만든다: 시험 페이지든
 * 사이트의 index.html 이든 같은 코드가 서게.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        LandingComponent component = DaggerLandingComponent.create();
        component.landing().mount(frame());
    }

    private static HTMLElement frame() {
        HTMLElement app = Js.uncheckedCast(DomGlobal.document.getElementById("app"));
        if (app != null) return app;
        HTMLElement made = Js.uncheckedCast(DomGlobal.document.createElement("div"));
        made.id = "app";
        DomGlobal.document.body.append(made);
        return made;
    }
}
