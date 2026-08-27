package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.CompanionSharing;
import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.bridge.TranscriptSharing;
import dev.sayaya.magi.client.usecase.CompanionSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import elemental2.dom.EventSource;
import elemental2.dom.MessageEvent;
import elemental2.dom.URLSearchParams;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * CompanionSource의 회선. 셸이 스트림을 호스팅하면(TranscriptSharing) 그 구독으로 —
 * 창당 1스트림 규칙. 셸 없이 단독으로 떴을 때만 주소(?d=)를 제 눈으로 읽고 제 회선을 연다.
 */
@Singleton
public class BridgeCompanionSource implements CompanionSource {
    private Listener listener;
    private EventSource es;
    private String socket;
    private String peer;

    @Inject
    public BridgeCompanionSource() {}

    @Override
    public void start(Listener l) {
        listener = l;
        if (CompanionSharing.hosted()) {
            CompanionSharing.subscribe(l::context);
            TranscriptSharing.subscribe(l::transcript);
            TranscriptSharing.subscribeTurn(l::turn);
            return;
        }
        own();
    }

    @Override
    public void roster(Consumer<Object> cb) {
        if (RosterSharing.hosted()) { RosterSharing.subscribe(cb::accept); return; }
        Console.fetchList("/fleet", cb::accept);
    }

    @Override
    public void history(CompanionContext ctx, Consumer<Object> cb) {
        Console.fetchList("/history" + q(ctx), cb::accept);
    }

    @Override
    public void pastTranscript(CompanionContext ctx, String session, Consumer<Object> cb) {
        Console.fetchList("/transcript" + q(ctx) + "&session=" + Global.encodeURIComponent(session), cb::accept);
    }

    private static String q(CompanionContext ctx) {
        return "?d=" + Global.encodeURIComponent(ctx.socket)
                + (ctx.peer != null && !ctx.peer.isEmpty() ? "&p=" + Global.encodeURIComponent(ctx.peer) : "");
    }

    @Override
    public void context(CompanionContext ctx, Consumer<Object> cb) {
        Console.fetchList("/context" + q(ctx), cb::accept);
    }

    @Override
    public void compact(CompanionContext ctx, Runnable done) {
        Console.post("/compact", null, ctx.socket, ctx.peer).then(w -> { done.run(); return null; });
    }

    @Override
    public void submit(CompanionContext ctx, String text, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("text", text);
        Console.post("/submit", body, ctx.socket, ctx.peer).then(w -> { why.accept(w); return null; });
    }

    /** 단독 모드: 주소가 컨텍스트다(타입 해석해 줄 셸이 없으니 ?type=, 없으면 기본). */
    private void own() {
        URLSearchParams q = new URLSearchParams(DomGlobal.window.location.search);
        socket = q.get("d");
        peer = q.get("p");
        listener.context(socket == null ? null
                : CompanionContext.of(socket, peer, q.get("type"),
                        q.has("past") ? (q.get("past") == null ? "" : q.get("past")) : null));
        if (socket != null) openStream();
    }

    private void openStream() {
        String query = "?d=" + Global.encodeURIComponent(socket)
                + (peer != null ? "&p=" + Global.encodeURIComponent(peer) : "");
        es = new EventSource("/events" + query);
        es.addEventListener("message", evt -> {
            MessageEvent<String> me = Js.uncheckedCast(evt);
            try { listener.transcript(Global.JSON.parse(me.data)); }
            catch (Exception ignore) { listener.transcript(null); }
        });
        es.addEventListener("turn", evt -> {
            MessageEvent<String> me = Js.uncheckedCast(evt);
            try {
                JsPropertyMap<Object> d = Js.uncheckedCast(Global.JSON.parse(me.data));
                listener.turn(Js.isTruthy(d.get("open")),
                        d.has("forSec") ? Js.coerceToDouble(d.get("forSec")) : 0);
            } catch (Exception ignore) { listener.turn(false, 0); }
        });
        es.addEventListener("error", evt -> {
            EventSource gone = es;
            if (gone != null) gone.close();
            es = null;
            DomGlobal.setTimeout(a -> { if (es == null) openStream(); }, 1500);
        });
    }
}
