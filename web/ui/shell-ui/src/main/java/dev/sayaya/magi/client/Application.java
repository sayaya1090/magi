package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
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
        Labels.load(() -> {
            DomGlobal.document.body.append(
                    component.masthead().element(),
                    component.rail().scrim(),
                    component.rail().element(),
                    component.frame().element());
            component.masthead().paint();
            component.rail().paint();
            component.initializer().initialize();
        });
    }
}
