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
    public void interrupt(FleetAgent a, Runnable then) {
        Console.post("/interrupt", null, a.socket, a.peer).then(r -> { then.run(); return null; });
    }

    @Override
    public void answer(FleetAgent a, String text, Runnable then) {
        URLSearchParams p = new URLSearchParams();
        p.append("call", a.askId == null ? "" : a.askId);
        p.append("kind", a.askKind == null ? "" : a.askKind);
        p.append("text", text);
        Console.post("/answer", p, a.socket, a.peer).then(r -> { then.run(); return null; });
    }
}
