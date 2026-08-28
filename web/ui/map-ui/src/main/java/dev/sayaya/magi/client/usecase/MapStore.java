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
     * 지도의 <b>뼈대</b>가 되는 것 — 이름·자리·상태와 오간 것들. 걸음 수는 여기 없다:
     * 지도는 그것을 그리지 않으므로 그 때문에 다시 설 이유도 없다.
     *
     * <p>쉰 시간도 여기 없다. 지도는 남의 기계의 노드에 그 줄을 적으므로 <b>그리기는 한다</b> —
     * 그래서 한때 그려지는 낱말(dur)을 여기 넣었는데, 갓 쉰 노드의 그 낱말은 초 단위라 매 초
     * 달라졌다: 지도가 통째로 다시 서기를 10초에 70번(실측). 다시 세울 일과 낱말 하나를 고쳐
     * 쓸 일은 다른 일이라 갈랐다 — 뼈대는 이 서명이, 쉰 시간은 {@link #ticked()}가 나른다.
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
            b.append(';');
        }
        b.append('|').append(handoffs == null ? "" : elemental2.core.Global.JSON.stringify(handoffs));
        return b.toString();
    }

    /**
     * 명단이 다시 온 순간 — 값이 아니라 때. 지도는 이걸 듣고 이미 서 있는 줄의 낱말만 고쳐 쓴다:
     * 노드 하나의 쉰 시간이 1초 늘었다고 판을 다시 세우면 그 사이 클릭이 사라진다.
     */
    public dev.sayaya.rx.Observable<String> ticked() {
        return when(() -> {
            StringBuilder b = new StringBuilder();
            jsinterop.base.JsArrayLike<Object> all = jsinterop.base.Js.uncheckedCast(fleet);
            for (int i = 0; all != null && i < all.getLength(); i++) {
                jsinterop.base.JsPropertyMap<Object> a = jsinterop.base.Js.uncheckedCast(all.getAt(i));
                if (!jsinterop.base.Js.isTruthy(a.get("elsewhere"))) continue;
                Object idle = a.get("idle");
                b.append(idle == null ? "" : dev.sayaya.magi.component.Spans.dur(
                        (int) jsinterop.base.Js.coerceToDouble(idle))).append(';');
            }
            return b.toString();
        });
    }

    public Object fleet() { return fleet; }
    public Object handoffs() { return handoffs; }
    public boolean answered() { return fleetAnswered && handsAnswered; }


}
