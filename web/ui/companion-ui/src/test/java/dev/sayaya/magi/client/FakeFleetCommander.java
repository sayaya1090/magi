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

    /**
     * 답 — 그리고 스펙이 <b>거부</b>를 시킬 수 있다. window.__magi_test_refuse에 사유를 놓아
     * 두면 그 사유로 답한다: 진짜 BFF가 403·409와 함께 본문에 적어 보내는 그 자리다.
     */
    @Override
    public void answer(FleetAgent a, String text, java.util.function.Consumer<String> then) {
        record("answer " + a.name + " " + text);
        Object why = Js.asPropertyMap(DomGlobal.window).get("__magi_test_refuse");
        then.accept(why == null ? "" : String.valueOf(why));
    }

    private static void record(String what) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_last", what);
    }
}
