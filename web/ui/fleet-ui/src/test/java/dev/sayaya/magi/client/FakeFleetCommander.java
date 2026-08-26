package dev.sayaya.magi.client;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.FleetCommander;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

/** 행동을 window.__magi_test_last 에 적는 가짜 — 테스트가 브라우저에서 읽는다. */
@Singleton
public class FakeFleetCommander implements FleetCommander {
    @Inject
    public FakeFleetCommander() {}

    @Override
    public void interrupt(FleetAgent a, Runnable then) { record("interrupt " + a.name); then.run(); }

    @Override
    public void answer(FleetAgent a, String text, Runnable then) { record("answer " + a.name + " " + text); then.run(); }

    private static void record(String what) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_last", what);
    }
}
