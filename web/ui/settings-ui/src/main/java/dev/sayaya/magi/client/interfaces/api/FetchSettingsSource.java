package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.SettingsSource;
import elemental2.dom.URLSearchParams;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** 운영 loadAutocomplete/acSave가 쓰던 그 한 경로(/autocomplete). */
@Singleton
public class FetchSettingsSource implements SettingsSource {
    @Inject
    public FetchSettingsSource() {}

    @Override
    public void read(String socket, String peer, Consumer<Object> cb) {
        String q = socket == null || socket.isEmpty() ? "" : "?d=" + elemental2.core.Global.encodeURIComponent(socket);
        Console.fetchList("/autocomplete" + q, cb::accept);
    }

    @Override
    public void save(String socket, String peer, String field, String value, Runnable then) {
        URLSearchParams body = new URLSearchParams();
        body.set(field, value);
        // 전역 config는 소켓으로 지목하지 않는다 — 어느 컴패니언의 것도 아니라서.
        if (socket == null || socket.isEmpty()) body.set("tier", "global");
        Console.post("/autocomplete", body, socket, peer).then(w -> { then.run(); return null; });
    }
}
