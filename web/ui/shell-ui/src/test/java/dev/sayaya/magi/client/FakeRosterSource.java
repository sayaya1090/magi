package dev.sayaya.magi.client;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.RosterSource;

import javax.inject.Inject;
import javax.inject.Singleton;

/** 고정 명단(둘, 하나는 기다림)과 살아 있는 회선 — 마스트헤드·배지가 읽을 사실들. */
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
