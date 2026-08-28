package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.CompanionSharing;
import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.bridge.TranscriptSharing;
import dev.sayaya.magi.client.usecase.CompanionSource;
import elemental2.core.Global;
import elemental2.dom.URLSearchParams;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 이 화면의 회선 — 그리고 <b>회선을 열지 않는 자리</b>가 어디인지가 이 클래스의 요점이다.
 *
 * 지금 무엇을 보고 있는지(컨텍스트), 오간 말(전사), 턴이 열렸는지는 전부 셸이 창에 흘리는
 * 것을 <b>구독</b>한다: 스트림은 창당 하나이고 그 하나를 셸이 갖는다. 예전에는 셸이 없으면
 * 제 스트림을 여는 폴백이 있었는데, 그것이 곧 /events를 읽는 두 번째 모듈이었다 — 규칙을
 * 어기는 경로가 규칙 안에 숨어 있던 셈이다. 셸 없는 자리(단독 테스트 페이지·데모)는 그래프가
 * 다른 구현을 물면 된다.
 */
@Singleton
public class BridgeCompanionSource implements CompanionSource {
    @Inject
    public BridgeCompanionSource() {}

    @Override
    public void start(Listener l) {
        CompanionSharing.subscribe(l::context);
        TranscriptSharing.subscribe(l::transcript);
        TranscriptSharing.subscribeTurn(l::turn);
    }

    @Override
    public void history(CompanionContext ctx, Consumer<Object> cb) {
        Console.fetchList("/history" + q(ctx), cb::accept);
    }

    @Override
    public void subagents(CompanionContext ctx, Consumer<Object> cb) {
        Console.fetchList("/subagents" + q(ctx), cb::accept);
    }

    @Override
    public void pastTranscript(CompanionContext ctx, String session, Consumer<Object> cb) {
        Console.fetchList("/transcript" + q(ctx) + "&session=" + Global.encodeURIComponent(session),
                cb::accept);
    }

    @Override
    public void submit(CompanionContext ctx, String text, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("text", text);
        Console.post("/submit", body, ctx.socket, ctx.peer, (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void resume(CompanionContext ctx, String session, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("session", session);
        Console.post("/resume", body, ctx.socket, ctx.peer, (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void interrupt(CompanionContext ctx, Consumer<String> why) {
        Console.post("/interrupt", new URLSearchParams(), ctx.socket, ctx.peer, (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void councilEvidence(CompanionContext ctx, int round, Consumer<Object> cb) {
        Console.fetchList("/council" + q(ctx) + "&round=" + round
                + (ctx.past == null || ctx.past.isEmpty() ? ""
                   : "&session=" + Global.encodeURIComponent(ctx.past)), cb::accept);
    }

    @Override
    public void suggest(CompanionContext ctx, String prefix, Consumer<String> text) {
        URLSearchParams body = new URLSearchParams();
        body.set("prefix", prefix);
        // 사유를 <b>일부러</b> 버린다 — 컴포저가 멈춘 것을 보고 부르는 도움이라, 사람은
        // 아무것도 청한 적이 없다. 거절도 침묵으로 지나간다.
        Console.post("/suggest", body, ctx.socket, ctx.peer, (ok, said) -> text.accept(ok ? said : ""));
    }

    private static String q(CompanionContext ctx) {
        return "?d=" + Global.encodeURIComponent(ctx.socket)
                + (ctx.peer != null && !ctx.peer.isEmpty() ? "&p=" + Global.encodeURIComponent(ctx.peer) : "");
    }
}
