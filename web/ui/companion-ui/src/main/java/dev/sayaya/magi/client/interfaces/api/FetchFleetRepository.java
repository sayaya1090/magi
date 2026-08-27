package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.client.usecase.FleetRepository;
import elemental2.dom.DomGlobal;
import elemental2.dom.EventSource;
import elemental2.dom.MessageEvent;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * FleetRepository의 회선. 셸이 스트림을 호스팅하면(RosterSharing) 그 구독으로 —
 * 창당 1스트림 규칙. 셸 없이 단독으로 떴을 때만 제 /fleet + /events를 연다.
 */
@Singleton
public class FetchFleetRepository implements FleetRepository {
    private RosterHandler handler;
    private EventSource es;

    @Inject
    public FetchFleetRepository() {}

    @Override
    public void watch(RosterHandler h) {
        handler = h;
        if (RosterSharing.hosted()) {
            RosterSharing.subscribe(o -> h.roster(o == null ? null : Js.uncheckedCast(o)));
            return;
        }
        own();
    }

    @Override
    public void refresh() {
        if (RosterSharing.hosted()) { RosterSharing.refresh(); return; }
        if (handler == null) return;
        Console.fetchList("/fleet", parsed -> handler.roster(parsed == null ? null : Js.uncheckedCast(parsed)));
    }

    /** 단독 모드의 제 회선 — 끊기면 1.5초 뒤 조용히 다시 잇는다. */
    private void own() {
        es = dev.sayaya.magi.bridge.Console.stream("/events");
        es.addEventListener("fleet", evt -> {
            MessageEvent<String> me = Js.uncheckedCast(evt);
            try { handler.roster(Js.uncheckedCast(elemental2.core.Global.JSON.parse(me.data))); }
            catch (Exception ignore) { /* 깨진 프레임은 다음 프레임이 고친다 */ }
        });
        es.addEventListener("error", evt -> {
            EventSource mine = es;
            if (mine != null) mine.close();
            es = null;
            DomGlobal.setTimeout(a -> { if (es == null && handler != null) own(); }, 1500);
        });
    }
}
