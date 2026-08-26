package dev.sayaya.magi.client;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.RosterSource;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 고정 명단(둘, 하나는 기다림)과 살아 있는 회선 — 마스트헤드·배지가 읽을 사실들.
 * 조준은 window.__magi_test_aim 에 적는다 — 회선 재개설 없이 셸의 흐름만 잰다.
 */
@Singleton
public class FakeRosterSource implements RosterSource {
    private Listener listener;

    @Inject
    public FakeRosterSource() {}

    @Override
    public void start(Listener l) {
        listener = l;
        l.link(true);
    }

    @Override
    public void aim(String socket, String peer) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_aim", socket == null ? "" : socket + (peer == null ? "" : "|" + peer));
        // 조준되면 전사가 흐르기 시작한다 — 가짜는 한 프레임이면 족하다.
        if (listener != null && socket != null) {
            listener.transcript(Js.uncheckedCast(elemental2.core.Global.JSON.parse(
                    "[{\"who\":\"user\",\"text\":\"hello there\"}," +
                    "{\"who\":\"assistant\",\"text\":\"doing it\"}]")));
            listener.turn(true, 4);
        }
    }

    @Override
    public void refresh() { if (listener != null) listener.roster(fixture()); }

    static FleetAgent[] fixture() {
        FleetAgent waiting = new FleetAgent();
        waiting.name = "alpha-1";
        waiting.state = "waiting";
        waiting.socket = "/tmp/a1.sock";
        waiting.live = true;
        FleetAgent idle = new FleetAgent();
        idle.name = "solo";
        idle.state = "idle";
        idle.socket = "/tmp/solo.sock";
        idle.live = true;
        return new FleetAgent[]{waiting, idle};
    }
}
