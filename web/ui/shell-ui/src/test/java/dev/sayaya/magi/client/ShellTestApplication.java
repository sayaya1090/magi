package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.Tool;
import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;

import java.util.List;

/** 프로덕션 Application과 같은 조립, 로더·명단만 가짜 — 팩 없이(키 폴백) 셸의 뼈대를 세운다. */
public class ShellTestApplication implements EntryPoint {
    @JsFunction
    interface Hook { void run(); }

    @Override
    public void onModuleLoad() {
        ShellTestComponent component = DaggerShellTestComponent.create();
        DomGlobal.document.body.append(
                component.turnbar().element(),
                component.masthead().element(),
                component.rail().scrim(),
                component.rail().element(),
                component.frame().element(),
                component.palette().element());
        component.masthead().paint();
        component.rail().paint();
        component.initializer().initialize();
        // 도구는 아직 프로덕션에 없다(용례 대기) — 테스트가 이 문으로 등록해 기전을 잰다.
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_provide_tools", (Hook) () ->
                component.toolList().provide(Destination.FLEET.id, List.of(
                        new Tool("hammer", "tool.hammer", "M4 20l6-6M14 4l6 6-8 8-6-6z", 1, () ->
                                Js.asPropertyMap(DomGlobal.window).set("__magi_test_tool_ran", "hammer")),
                        new Tool("wrench", "tool.wrench", "M20 6a5 5 0 0 1-7 5l-7 7-2-2 7-7a5 5 0 0 1 5-7z", 2, null))));
    }
}
