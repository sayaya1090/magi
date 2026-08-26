package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.RosterSharing;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 셸의 명단 저장소 — 스트림의 유일한 소유자이자 창 브리지의 호스트.
 * 마스트헤드와 레일 배지가 여기서 읽고, 화면 모듈들은 RosterSharing으로 같은 물을 마신다.
 */
@Singleton
public class RosterStore implements RosterSource.Listener {
    private final RosterSource source;
    private final List<Consumer<FleetAgent[]>> rosterObs = new ArrayList<>();
    private final List<Consumer<Boolean>> linkObs = new ArrayList<>();
    private FleetAgent[] current = null;
    private boolean up = false;
    private boolean started = false;

    @Inject
    public RosterStore(RosterSource source) { this.source = source; }

    public void start() {
        if (started) return;
        started = true;
        // 화면 모듈이 로드되기 전에 문을 걸어 둔다 — 구독은 현재값을 재생한다.
        RosterSharing.host(cb -> {
            rosterObs.add(list -> cb.call(list));
            if (current != null) cb.call(current);
        }, this::refresh);
        source.start(this);
        source.refresh();
    }

    public void refresh() { source.refresh(); }

    public void subscribe(Consumer<FleetAgent[]> o) {
        rosterObs.add(o);
        if (current != null) o.accept(current);
    }

    public void subscribeLink(Consumer<Boolean> o) {
        linkObs.add(o);
        o.accept(up);
    }

    @Override
    public void roster(FleetAgent[] listOrNull) {
        if (listOrNull != null) current = listOrNull;
        for (Consumer<FleetAgent[]> o : rosterObs) o.accept(listOrNull);
    }

    @Override
    public void link(boolean now) {
        up = now;
        for (Consumer<Boolean> o : linkObs) o.accept(now);
    }
}
