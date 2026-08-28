package dev.sayaya.magi.client.usecase;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

/** 맵의 저장소 — 명단과 오간 것. 둘 다 와야 그린다: 반쪽 지도는 가구 딸린 거짓말이다(운영). */
@Singleton
public class MapStore extends dev.sayaya.magi.bridge.Told {
    private final MapSource source;
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
        source.fleet(l -> { fleet = l; fleetAnswered = true; told(); });
        source.handoffs(l -> { handoffs = l; handsAnswered = true; told(); });
    }

    /**
     * 지도가 그리는 것 — 이름·자리·상태와 오간 것들. 걸음 수나 초 단위 쉰 시간은 여기 없다:
     * 지도는 그것을 그리지 않으므로 그 때문에 다시 그릴 이유도 없다.
     */
    public dev.sayaya.rx.Observable<String> drawn() {
        return when(this::sig);
    }

    private String sig() {
        StringBuilder b = new StringBuilder();
        b.append(fleetAnswered).append('|').append(handsAnswered).append('|');
        jsinterop.base.JsArrayLike<Object> all = jsinterop.base.Js.uncheckedCast(fleet);
        for (int i = 0; all != null && i < all.getLength(); i++) {
            jsinterop.base.JsPropertyMap<Object> a = jsinterop.base.Js.uncheckedCast(all.getAt(i));
            for (String k : new String[]{"socket", "name", "state", "team", "host", "instance",
                    "addr", "peer", "trust", "hub", "live", "elsewhere"}) {
                b.append(a.get(k)).append(',');
            }
            // 쉰 시간은 <b>그려지는 자리에서만</b>, 그리고 그려지는 낱말로 — 지도는 남의 기계의
            // 것에만 그 줄을 적는다(nodeage). 모든 행에 초를 넣으면 매 초 "달라졌다"가 되어
            // 거른다는 말에 뜻이 없어진다(실측: 그래도 10초에 70번이었다).
            Object idle = a.get("idle");
            if (jsinterop.base.Js.isTruthy(a.get("elsewhere")) && idle != null) {
                b.append(dev.sayaya.magi.component.Spans.dur((int) jsinterop.base.Js.coerceToDouble(idle)));
            }
            b.append(';');
        }
        b.append('|').append(handoffs == null ? "" : elemental2.core.Global.JSON.stringify(handoffs));
        return b.toString();
    }

    public Object fleet() { return fleet; }
    public Object handoffs() { return handoffs; }
    public boolean answered() { return fleetAnswered && handsAnswered; }


}
