package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.FleetAgent;

import dev.sayaya.rx.subject.BehaviorSubject;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

import static dev.sayaya.rx.subject.BehaviorSubject.behavior;

/**
 * 명단의 저장소 — 화면은 여기만 본다. 회선이 셸의 것인지 제 것인지는 포트 뒤의 일이다.
 */
@Singleton
public class FleetStore {
    private final FleetRepository repo;
    private final BehaviorSubject<FleetAgent[]> _this = behavior(null);
    private boolean started = false;

    @Inject
    public FleetStore(FleetRepository repo) { this.repo = repo; }

    /** 구독. 이미 읽은 명단이 있으면 즉시 재생한다 — 늦게 온 구독자도 화면을 가진다. */
    public void subscribe(Consumer<FleetAgent[]> o) { _this.subscribe(o); }

    /** 구독 + 첫 읽기. 여러 번 불러도 구독은 하나다. */
    public void start() {
        if (!started) {
            started = true;
            repo.watch(this::push);
        }
        refresh();
    }

    /** 행동 뒤의 재조회 — 인터럽트/답의 결과를 다음 프레임보다 먼저 본다. */
    public void refresh() { repo.refresh(); }

    private void push(FleetAgent[] listOrNull) {
        // null은 "아직/못 읽음"이라 들고 있던 명단을 지우지 않는다 — 한 번 그린 목록이
        // 프레임 하나 빠졌다고 사라지면 안 된다.
        if (listOrNull != null) _this.next(listOrNull);
    }
}
