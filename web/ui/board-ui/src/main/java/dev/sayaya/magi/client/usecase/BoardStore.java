package dev.sayaya.magi.client.usecase;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Consumer;

/**
 * 보드의 저장소 — 명단과 컴패니언별 지난 일, 그리고 보는 날·검색어.
 * 명단이 오면 각 컴패니언의 /history를 한 번씩 걷어 온다(운영과 같은 두 요청, 새 엔드포인트 없음).
 */
@Singleton
public class BoardStore {
    private final BoardSource source;
    private final List<Runnable> observers = new ArrayList<>();
    private Object fleet = null;
    private boolean fleetAnswered = false;
    private final Map<String, Object> histories = new HashMap<>();
    private String day = "";
    private String query = "";
    private boolean started = false;

    @Inject
    public BoardStore(BoardSource source) { this.source = source; }

    public void start(String todayISO) {
        if (started) return;
        started = true;
        if (day.isEmpty()) day = todayISO;
        reload();
    }

    public void reload() {
        source.fleet(list -> {
            fleet = list;
            fleetAnswered = true;
            emit();
        });
    }

    /** 명단의 한 줄에 대한 지난 일들 — 처음 물을 때만 회선을 탄다. 답은 구독으로. */
    public void wantHistory(String socket, String peer) {
        String key = (peer == null ? "" : peer) + "|" + socket;
        if (histories.containsKey(key)) return;
        histories.put(key, null);
        source.history(socket, peer, h -> { histories.put(key, h); emit(); });
    }

    public Object historyOf(String socket, String peer) {
        return histories.get((peer == null ? "" : peer) + "|" + socket);
    }

    public Object fleet() { return fleet; }
    public boolean fleetAnswered() { return fleetAnswered; }
    public String day() { return day; }
    public String query() { return query; }

    public void day(String d) { if (d != null && !d.isEmpty()) { day = d; emit(); } }
    public void query(String q) { query = q == null ? "" : q; emit(); }

    public void subscribe(Runnable o) { observers.add(o); o.run(); }

    private void emit() { for (Runnable o : observers) o.run(); }
}
