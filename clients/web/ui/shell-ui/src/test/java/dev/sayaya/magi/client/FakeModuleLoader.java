package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.ModuleLoader;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 주입 대신 window.__magi_test_loads 에 적는 가짜 — 스크립트 404 없이 셸의 흐름을 잰다.
 * 시트 선언도 함께 적는다(`[companion+css]`): 화면이 옷을 입고 도착하는지가 셸의 계약이다.
 */
@Singleton
public class FakeModuleLoader implements ModuleLoader {
    private final StringBuilder loads = new StringBuilder();

    @Inject
    public FakeModuleLoader() {}

    @Override
    public void ensure(String module, boolean styles) {
        // 진짜처럼 한 번만: ensure의 계약(중복 주입 없음)은 구현이 아니라 포트의 것이다.
        String mark = "[" + module + (styles ? "+css" : "") + "]";
        if (loads.indexOf("[" + module) >= 0) return;
        loads.append(mark);
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_loads", loads.toString());
    }
}
