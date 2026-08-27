package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.RosterSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import elemental2.dom.EventSource;
import elemental2.dom.MessageEvent;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * RosterSource의 회선: /fleet 읽기와 /events 스트림, 그리고 그 스트림의 건강.
 *
 * 조준(aim)이 곧 주소다: ?d= 없는 스트림은 명단(fleet 프레임)만, ?d=가 붙으면 같은
 * 회선의 기본 프레임이 전사 전체가 되고 turn 프레임이 함께 온다 — BFF의 events 핸들러
 * 계약. 끊기면 1.5초 뒤 현재 조준으로 다시 잇는다.
 */
@Singleton
public class FetchRosterSource implements RosterSource {
    private Listener listener;
    private EventSource es;
    private String socket;
    private String peer;
    private int generation = 0;   // 조준 변경이 옛 재접속 타이머를 이긴다

    @Inject
    public FetchRosterSource() {}

    @Override
    public void start(Listener l) {
        listener = l;
        open();
        watchTab();
    }

    /**
     * 보이지 않는 탭은 회선을 <b>반납한다</b>.
     *
     * 스트림은 창당 하나이고, 그 하나는 데몬의 자원이기도 하다: 열어 둔 채 잊힌 탭 다섯이
     * 컴패니언 다섯을 붙잡고 있으면 그것은 아무도 보지 않는 일을 위해 데몬이 계속 말하는
     * 것이다. 돌아오면 다시 열고, 그 사이의 일은 한 번의 명단 읽기가 따라잡는다 — 스트림은
     * 지금을 나르지 과거를 나르지 않으므로, 반납의 값은 재접속 한 번이다.
     */
    private void watchTab() {
        DomGlobal.document.addEventListener("visibilitychange", evt -> {
            if (Js.isTruthy(Js.asPropertyMap(DomGlobal.document).get("hidden"))) {
                generation++;
                if (es != null) { es.close(); es = null; }
                if (listener != null) listener.link(false);
                return;
            }
            if (listener != null && es == null) { open(); refresh(); }
        });
    }

    @Override
    public void aim(String wantSocket, String wantPeer) {
        if (eq(socket, wantSocket) && eq(peer, wantPeer)) return;
        socket = wantSocket;
        peer = wantPeer;
        reopen();
    }

    @Override
    public void facts(java.util.function.Consumer<Object> consoleInfo, java.util.function.Consumer<Object> caps) {
        Console.fetchList("/console", consoleInfo::accept);
        Console.fetchList("/me", parsed ->
                caps.accept(parsed == null ? null : Js.asPropertyMap(parsed).get("can")));
    }

    @Override
    public void refresh() {
        if (listener == null) return;
        Console.fetchList("/fleet", parsed -> listener.roster(parsed == null ? null : Js.uncheckedCast(parsed)));
    }

    private void reopen() {
        generation++;
        if (es != null) { es.close(); es = null; }
        if (listener != null) open();
    }

    private void open() {
        final int mine = ++generation;
        es = dev.sayaya.magi.bridge.Console.stream("/events" + q());
        es.addEventListener("open", evt -> listener.link(true));
        es.addEventListener("fleet", evt -> {
            MessageEvent<String> me = Js.uncheckedCast(evt);
            try { listener.roster(Js.uncheckedCast(Global.JSON.parse(me.data))); }
            catch (Exception ignore) { /* 깨진 프레임은 다음 프레임이 고친다 */ }
        });
        // 조준된 회선의 기본 프레임 = 전사 행 전체. 조준 없는 회선의 기본 프레임은 없다.
        es.addEventListener("message", evt -> {
            if (socket == null) return;
            MessageEvent<String> me = Js.uncheckedCast(evt);
            try { listener.transcript(Global.JSON.parse(me.data)); }
            catch (Exception ignore) { listener.transcript(null); }
        });
        es.addEventListener("turn", evt -> {
            MessageEvent<String> me = Js.uncheckedCast(evt);
            try {
                JsPropertyMap<Object> d = Js.uncheckedCast(Global.JSON.parse(me.data));
                boolean on = Js.isTruthy(d.get("open"));
                double sec = d.has("forSec") ? Js.coerceToDouble(d.get("forSec")) : 0;
                listener.turn(on, sec);
            } catch (Exception ignore) { listener.turn(false, 0); }
        });
        es.addEventListener("error", evt -> {
            listener.link(false);
            EventSource gone = es;
            if (gone != null) gone.close();
            es = null;
            DomGlobal.setTimeout(a -> { if (es == null && generation == mine) open(); }, 1500);
        });
    }

    private String q() {
        if (socket == null) return "";
        return "?d=" + Global.encodeURIComponent(socket)
                + (peer != null ? "&p=" + Global.encodeURIComponent(peer) : "");
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }
}
