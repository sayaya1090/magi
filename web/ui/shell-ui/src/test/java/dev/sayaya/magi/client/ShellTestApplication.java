package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import elemental2.dom.DomGlobal;

/** 프로덕션 Application과 같은 조립, 로더·명단만 가짜 — 팩 없이(키 폴백) 셸의 뼈대를 세운다. */
public class ShellTestApplication implements EntryPoint {
    @Override
    public void onModuleLoad() {
        ShellTestComponent component = DaggerShellTestComponent.create();
        DomGlobal.document.body.append(
                component.masthead().element(),
                component.rail().scrim(),
                component.rail().element(),
                component.frame().element());
        component.masthead().paint();
        component.rail().paint();
        component.initializer().initialize();
    }
}
