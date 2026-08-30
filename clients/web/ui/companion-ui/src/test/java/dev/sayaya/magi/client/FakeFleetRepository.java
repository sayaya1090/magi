package dev.sayaya.magi.client;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.FleetRepository;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 고정 명단: 두 팀 + 무명, 다섯 상태 전부 — 화면 테스트가 도달해야 하는 가지들.
 * 클린 아키텍처의 보상: HTTP 목 없이 포트에 가짜를 물린다.
 */
@Singleton
public class FakeFleetRepository implements FleetRepository {
    private RosterHandler handler;

    @Inject
    public FakeFleetRepository() {}

    @Override public void watch(RosterHandler h) { handler = h; }
    @Override public void refresh() { if (handler != null) handler.roster(fixture()); }

    static FleetAgent[] fixture() {
        FleetAgent waiting = agent("alpha-1", "waiting", "alpha");
        waiting.asking = "rm -rf /tmp/x 해도 됩니까?";
        waiting.askId = "c1";
        waiting.askKind = "permission";
        waiting.version = "v1.0.0";        // 최신(v1.1.0)보다 뒤 → behind 힌트
        FleetAgent working = agent("alpha-hub", "working", "alpha");
        working.task = "build the thing";
        working.doing = "go test ./...";
        working.planDone = 2;
        working.planTotal = 5;
        working.hub = true;
        working.version = "v1.1.0";
        FleetAgent idle = agent("solo", "idle", null);
        idle.version = "v1.1.0";
        FleetAgent stopped = agent("deadone", "stopped", null);
        stopped.live = false;
        FleetAgent far = agent("faraway", "remote", null);
        far.elsewhere = true;
        return new FleetAgent[]{idle, stopped, waiting, working, far};   // 정렬은 화면의 몫
    }

    private static FleetAgent agent(String name, String state, String team) {
        FleetAgent a = new FleetAgent();
        a.name = name;
        a.state = state;
        if (team != null) a.team = team;
        a.socket = "/tmp/" + name + ".sock";
        a.workdir = "/w/" + name;
        a.host = "mac";
        a.live = true;
        a.steps = 3;
        a.idle = 5;
        return a;
    }
}
