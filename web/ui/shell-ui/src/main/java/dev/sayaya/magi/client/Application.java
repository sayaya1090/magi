package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.Labels;
import elemental2.dom.DomGlobal;

/**
 * 셸: 마스트헤드·드로어(레일)·프레임을 세우고, 명단 스트림을 소유하며, 주소가 대는
 * 목적지의 화면 모듈을 들인다. 언어 팩이 먼저다 — 문과 바의 말이 팩에서 온다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        ShellComponent component = DaggerShellComponent.create();
        // 그림은 구 콘솔이 굽는다 — 한 번 빌려 심고 나서 그린다(Icons.borrow). 없어도 정상:
        // 그때는 각 화면이 늘 그리던 제 도형을 그린다.
        Icons.borrow(() -> Labels.load(() -> {
            DomGlobal.document.body.append(
                    component.masthead().element(),
                    component.rail().scrim(),
                    component.rail().element(),
                    component.frame().element());
            component.masthead().paint();
            component.rail().paint();
            component.initializer().initialize();
            Icons.dress(DomGlobal.document.body);
        }));
    }
}
