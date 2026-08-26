package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.RosterSource;
import elemental2.dom.DomGlobal;
import elemental2.dom.EventSource;
import elemental2.dom.MessageEvent;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * RosterSource의 회선: /fleet 읽기와 /events 스트림, 그리고 그 스트림의 건강.
 * 끊기면 1.5초 뒤 조용히 다시 잇는다 — 콘솔 재시작도 노트북이 깨는 것도 평범한 일이다.
 */
@Singleton
public class FetchRosterSource implements RosterSource {
    private Listener listener;
    private EventSource es;

    @Inject
    public FetchRosterSource() {}

    @Override
    public void start(Listener l) {
        listener = l;
        open();
    }

    @Override
    public void refresh() {
        if (listener == null) return;
        Console.fetchList("/fleet", parsed -> listener.roster(parsed == null ? null : Js.uncheckedCast(parsed)));
    }

    private void open() {
        es = new EventSource("/events");
        es.addEventListener("open", evt -> listener.link(true));
        es.addEventListener("fleet", evt -> {
            MessageEvent<String> me = Js.uncheckedCast(evt);
            try { listener.roster(Js.uncheckedCast(elemental2.core.Global.JSON.parse(me.data))); }
            catch (Exception ignore) { /* 깨진 프레임은 다음 프레임이 고친다 */ }
        });
        es.addEventListener("error", evt -> {
            listener.link(false);
            EventSource mine = es;
            if (mine != null) mine.close();
            es = null;
            DomGlobal.setTimeout(a -> { if (es == null) open(); }, 1500);
        });
    }
}
