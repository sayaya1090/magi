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
public class BoardStore extends dev.sayaya.magi.bridge.Told {
    private final BoardSource source;
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
            told();
        });
    }

    /** 명단의 한 줄에 대한 지난 일들 — 처음 물을 때만 회선을 탄다. 답은 구독으로. */
    public void wantHistory(String socket, String peer) {
        String key = (peer == null ? "" : peer) + "|" + socket;
        if (histories.containsKey(key)) return;
        histories.put(key, null);
        source.history(socket, peer, h -> { histories.put(key, h); told(); });
    }

    public Object historyOf(String socket, String peer) {
        return histories.get((peer == null ? "" : peer) + "|" + socket);
    }

    /**
     * 보드가 그리는 것 — 어느 컴패니언들이 있고(이름·소켓), 그들의 지난 일이 무엇이며,
     * 보는 날과 좁히는 말이 무엇인가. 걸음 수나 지금 무엇을 하는지는 이 화면에 없다.
     */
    public dev.sayaya.rx.Observable<String> drawn() { return when(this::sig); }

    private String sig() {
        StringBuilder b = new StringBuilder(day).append('|').append(query).append('|').append(fleetAnswered);
        jsinterop.base.JsArrayLike<Object> all = jsinterop.base.Js.uncheckedCast(fleet);
        for (int i = 0; all != null && i < all.getLength(); i++) {
            jsinterop.base.JsPropertyMap<Object> a = jsinterop.base.Js.uncheckedCast(all.getAt(i));
            b.append(a.get("socket")).append(',').append(a.get("name")).append(',')
             .append(a.get("peer")).append(',').append(a.get("team")).append(';');
        }
        for (Map.Entry<String, Object> e : histories.entrySet()) {
            b.append('|').append(e.getKey()).append('=')
             .append(e.getValue() == null ? "" : elemental2.core.Global.JSON.stringify(e.getValue()));
        }
        return b.toString();
    }

    public Object fleet() { return fleet; }
    public boolean fleetAnswered() { return fleetAnswered; }
    public String day() { return day; }
    public String query() { return query; }

    public void day(String d) { if (d != null && !d.isEmpty()) { day = d; told(); } }
    public void query(String q) { query = q == null ? "" : q; told(); }


}
