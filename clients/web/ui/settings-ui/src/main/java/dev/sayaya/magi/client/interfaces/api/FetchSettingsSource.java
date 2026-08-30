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

    /**
     * 어느 설정을 묻는가 — 컴패니언 하나면 그 소켓, 아니면 <b>전역</b>이다.
     *
     * 소켓 없이 그냥 묻는 것은 "이 디렉토리의 데몬"을 묻는 뜻이라, 콘솔이 서 있는 자리에
     * 데몬이 없으면 거절이 돌아온다("no daemon in this directory") — 그러면 이 판은 답을
     * 못 읽어 완성 설정 묶음이 통째로 사라졌다(실측: 운영 11줄, 이식 7줄). 쓸 때는 이미
     * tier=global을 실어 보내고 있었으니, 읽지 못하는 곳에 쓰고 있던 셈이다.
     */
    private static String scope(String socket, String peer) {
        if (socket == null || socket.isEmpty()) return "?tier=global";
        String q = "?d=" + elemental2.core.Global.encodeURIComponent(socket);
        return peer == null || peer.isEmpty() ? q : q + "&p=" + elemental2.core.Global.encodeURIComponent(peer);
    }

    @Override
    public void read(String socket, String peer, Consumer<Object> cb) {
        Console.fetchList("/autocomplete" + scope(socket, peer), cb::accept);
    }

    @Override
    public void profiles(String socket, Consumer<Object> list) {
        Console.fetchList("/profiles" + scope(socket, null), list::accept);
    }

    @Override
    public void providers(Consumer<Object> list) {
        Console.fetchList("/providers", list::accept);
    }

    @Override
    public void saveProfile(String socket, String name, String baseUrl, String model, String key,
                            boolean delete, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("name", name);
        if (delete) {
            body.set("delete", "1");
        } else {
            body.set("baseUrl", baseUrl == null ? "" : baseUrl);
            body.set("model", model == null ? "" : model);
            // 키는 <b>적었을 때만</b> 보낸다: 빈 칸을 보내면 이미 있는 키를 지우는 뜻이 된다.
            if (key != null && !key.isEmpty()) body.set("apiKey", key);
        }
        Console.post("/profiles", body, socket == null || socket.isEmpty() ? null : socket, null, (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void pushKey(Consumer<String> key) {
        Console.fetchList("/push", got -> key.accept(got == null ? ""
                : String.valueOf(jsinterop.base.Js.asPropertyMap(got).get("key"))));
    }

    @Override
    public void push(String endpoint, String p256dh, String auth, boolean delete, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("endpoint", endpoint);
        body.set("p256dh", p256dh);
        body.set("auth", auth);
        if (delete) body.set("delete", "1");
        Console.post("/push", body, null, null, (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void save(String socket, String peer, String field, String value, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set(field, value);
        // 전역 config는 소켓으로 지목하지 않는다 — 어느 컴패니언의 것도 아니라서.
        if (socket == null || socket.isEmpty()) body.set("tier", "global");
        Console.post("/autocomplete", body, socket, peer, (ok, w) -> why.accept(Console.why(ok, w)));
    }
}
