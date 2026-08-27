package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.FleetAgent;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 명단의 저장소 — 화면은 여기만 본다. 회선이 셸의 것인지 제 것인지는 포트 뒤의 일이다.
 */
@Singleton
public class FleetStore {
    private final FleetRepository repo;
    private final List<Consumer<FleetAgent[]>> observers = new ArrayList<>();
    private FleetAgent[] current = null;
    private boolean started = false;

    @Inject
    public FleetStore(FleetRepository repo) { this.repo = repo; }

    /** 구독. 이미 읽은 명단이 있으면 즉시 재생한다 — 늦게 온 구독자도 화면을 가진다. */
    public void subscribe(Consumer<FleetAgent[]> o) {
        observers.add(o);
        if (current != null) o.accept(current);
    }

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
        if (listOrNull != null) current = listOrNull;
        for (Consumer<FleetAgent[]> o : observers) o.accept(listOrNull);
    }
}
