package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.FleetCommander;
import elemental2.dom.URLSearchParams;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * FleetCommander의 HTTP 구현. 대상은 ?d=<socket>&p=<peer>로 지목한다 — BFF의 회선 그대로.
 */
@Singleton
public class FetchFleetCommander implements FleetCommander {
    @Inject
    public FetchFleetCommander() {}

    @Override
    public void interrupt(FleetAgent a, java.util.function.Consumer<String> why) {
        Console.post("/interrupt", null, a.socket, a.peer, (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void answer(FleetAgent a, String text, java.util.function.Consumer<String> then) {
        URLSearchParams p = new URLSearchParams();
        p.append("call", a.askId == null ? "" : a.askId);
        p.append("kind", a.askKind == null ? "" : a.askKind);
        p.append("text", text);
        // post가 이미 재어 온 것을 그대로 넘긴다 — 성공이면 "", 거부면 서버가 적은 사유.
        // 여기서 버리면 그 사유를 다시 만들 수 있는 곳이 없다.
        Console.post("/answer", p, a.socket, a.peer, (ok, w) -> then.accept(Console.why(ok, w)));
    }
}
