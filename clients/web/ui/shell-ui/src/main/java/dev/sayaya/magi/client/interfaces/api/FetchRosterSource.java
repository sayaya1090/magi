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
    private String meeting;
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

    /**
     * 컴패니언 조준과 독립이다 — 회의 화면에는 조준된 컴패니언이 없다.
     *
     * 그래서 q()가 소켓 없이도 쿼리를 낼 수 있어야 한다: 예전에는 `socket == null`이면 빈
     * 문자열을 냈고, 그것이 `?m=`이 서버에 한 번도 닿지 못한 이유였다.
     */
    @Override
    public void meet(String wantMeeting) {
        if (eq(meeting, wantMeeting)) return;
        meeting = wantMeeting;
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
        // 조준된 회의의 방 프레임 — 참가자 하나가 방금 한 일. 서버가 변한 것만 보내므로
        // (main.go의 roomFrames가 NewSince로 고른다) 받는 쪽은 이어 붙이기만 하면 된다.
        // 깨진 프레임에 null을 흘리지 않는다: 이 판의 침묵은 "못 읽었다"가 아니라 "아직
        // 아무 일도 없었다"이고, 다음 프레임이 고친다.
        es.addEventListener("room", evt -> {
            MessageEvent<String> me = Js.uncheckedCast(evt);
            try { listener.room(Global.JSON.parse(me.data)); }
            catch (Exception ignore) { /* 다음 프레임이 고친다 */ }
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
        StringBuilder q = new StringBuilder();
        if (socket != null) {
            q.append("?d=").append(Global.encodeURIComponent(socket));
            if (peer != null) q.append("&p=").append(Global.encodeURIComponent(peer));
        }
        // 회의는 컴패니언과 나란한 또 하나의 조준이라 혼자서도 회선을 뜻있게 만든다.
        if (meeting != null) q.append(q.length() == 0 ? "?" : "&")
                .append("m=").append(Global.encodeURIComponent(meeting));
        return q.toString();
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }
}
