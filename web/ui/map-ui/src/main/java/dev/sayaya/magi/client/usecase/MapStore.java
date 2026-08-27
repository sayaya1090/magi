package dev.sayaya.magi.client.usecase;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

/** 맵의 저장소 — 명단과 오간 것. 둘 다 와야 그린다: 반쪽 지도는 가구 딸린 거짓말이다(운영). */
@Singleton
public class MapStore {
    private final MapSource source;
    private final List<Runnable> observers = new ArrayList<>();
    private Object fleet = null;
    private Object handoffs = null;
    private boolean fleetAnswered = false;
    private boolean handsAnswered = false;
    private boolean started = false;

    @Inject
    public MapStore(MapSource source) { this.source = source; }

    public void start() {
        if (started) return;
        started = true;
        source.fleet(l -> { fleet = l; fleetAnswered = true; emit(); });
        source.handoffs(l -> { handoffs = l; handsAnswered = true; emit(); });
    }

    public Object fleet() { return fleet; }
    public Object handoffs() { return handoffs; }
    public boolean answered() { return fleetAnswered && handsAnswered; }

    public void subscribe(Runnable o) { observers.add(o); o.run(); }

    private void emit() { for (Runnable o : observers) o.run(); }
}
