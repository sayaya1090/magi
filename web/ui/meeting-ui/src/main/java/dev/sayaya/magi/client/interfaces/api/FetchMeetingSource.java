package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.MeetingSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import elemental2.dom.Response;
import elemental2.dom.URLSearchParams;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** MeetingSource의 회선 — 운영 loadMeet/drawConvene/sayBox가 쓰던 그 경로들. */
@Singleton
public class FetchMeetingSource implements MeetingSource {
    @Inject
    public FetchMeetingSource() {}

    @Override
    public void rooms(Consumer<Object> cb) { Console.fetchList("/meet", cb::accept); }

    @Override
    public void fleet(Consumer<Object> cb) { Console.fetchList("/fleet", cb::accept); }

    /**
     * 회의 하나. 없는 방은 <b>null</b>로 답한다 — 빈 목록이 아니라: 사라진 방과 아직 아무도
     * 말하지 않은 방은 화면에서 다른 것이고, 그 둘을 같은 값으로 만들면 구별할 수 없다.
     */
    @Override
    public void room(String id, Consumer<Object> cb) {
        Console.raw("/meet?id=" + Global.encodeURIComponent(id), null)
                .then(r -> {
                    if (!r.ok) { cb.accept(null); return null; }
                    return r.text().then(body -> {
                        cb.accept(Global.JSON.parse(body));
                        return null;
                    });
                })
                .catch_(err -> { cb.accept(null); return null; });
    }

    @Override
    public void convene(String topic, String[] sockets, Consumer<Object> made, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("topic", topic);
        for (String s : sockets) body.append("who", s);
        postText("/meet", body, (ok, text) -> {
            if (!ok) { why.accept(text); return; }
            // 답이 곧 그 회의다. 만들지 않고 받아들이기만 한 콘솔(데모)은 이름 없는 주소로
            // 사람을 보내지 않도록 null을 낸다.
            Object m = null;
            try { m = Global.JSON.parse(text); } catch (Exception ignored) { }
            made.accept(m);
        });
    }

    @Override
    public void say(String id, String text, String call, boolean hold, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("id", id);
        if (text != null && !text.isEmpty()) body.set("text", text);
        if (call != null && !call.isEmpty()) body.set("call", call);
        if (hold) body.set("hold", "1");
        postText("/meet-say", body, (ok, t) -> why.accept(ok ? "" : t));
    }

    @Override
    public void close(String id, Runnable then) {
        URLSearchParams body = new URLSearchParams();
        body.set("id", id);
        postText("/meet-close", body, (ok, t) -> then.run());
    }

    @Override
    public void reopen(String id, String why, Runnable then) {
        URLSearchParams body = new URLSearchParams();
        body.set("id", id);
        if (why != null && !why.isEmpty()) body.set("why", why);
        postText("/meet-open", body, (ok, t) -> then.run());
    }

    @Override
    public void hand(String id, String who, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("id", id);
        body.set("who", who);
        postText("/meet-hand", body, (ok, t) -> why.accept(ok ? "" : t));
    }

    @Override
    public void roomRows(String socket, String room, Consumer<Object> cb) {
        Console.fetchList("/transcript?d=" + Global.encodeURIComponent(socket)
                + "&session=" + Global.encodeURIComponent(room), cb::accept);
    }

    /** 거절의 사유는 본문에 온다 — 실패를 조용히 삼키면 사람은 눌린 줄 알고 기다린다. */
    private interface Landed { void call(boolean ok, String text); }

    private static void postText(String path, URLSearchParams body, Landed then) {
        elemental2.dom.RequestInit init = elemental2.dom.RequestInit.create();
        init.setMethod("POST");
        init.setBody(body);
        Console.raw(path, init)
                .then((Response r) -> r.text().then(text -> {
                    then.call(r.ok, text == null ? "" : text.trim());
                    return null;
                }))
                .catch_(err -> { then.call(false, String.valueOf(err)); return null; });
    }
}
