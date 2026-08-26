package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.ModuleLoader;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

/** 주입 대신 window.__magi_test_loads 에 적는 가짜 — 스크립트 404 없이 셸의 흐름을 잰다. */
@Singleton
public class FakeModuleLoader implements ModuleLoader {
    private final StringBuilder loads = new StringBuilder();

    @Inject
    public FakeModuleLoader() {}

    @Override
    public void ensure(String module) {
        // 진짜처럼 한 번만: ensure의 계약(중복 주입 없음)은 구현이 아니라 포트의 것이다.
        if (loads.indexOf("[" + module + "]") >= 0) return;
        loads.append("[").append(module).append("]");
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_loads", loads.toString());
    }
}
