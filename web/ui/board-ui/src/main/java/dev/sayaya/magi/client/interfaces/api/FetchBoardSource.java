package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.BoardSource;
import elemental2.core.Global;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** BoardSource의 회선 — 운영 loadBoard가 쓰던 그 두 경로(/fleet, /history). */
@Singleton
public class FetchBoardSource implements BoardSource {
    @Inject
    public FetchBoardSource() {}

    @Override
    public void fleet(Consumer<Object> cb) { Console.fetchList("/fleet", cb::accept); }

    @Override
    public void history(String socket, String peer, Consumer<Object> cb) {
        String q = "/history?d=" + Global.encodeURIComponent(socket)
                + (peer != null && !peer.isEmpty() ? "&p=" + Global.encodeURIComponent(peer) : "");
        Console.fetchList(q, cb::accept);
    }
}
