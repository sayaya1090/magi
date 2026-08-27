package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.SettingsSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** 데몬 대신 고정된 완성 설정 — 저장은 창에 적어 스펙이 무엇이 갔는지 본다. */
@Singleton
public class FakeSettingsSource implements SettingsSource {
    @Inject
    public FakeSettingsSource() {}

    @Override
    public void read(String socket, String peer, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("{\"file\":\"~/.config/magi/config.toml\"," +
                "\"ambient\":true,\"crossSession\":false," +
                "\"profiles\":[\"fast-local\",\"cloud-mini\"]," +
                "\"codeProfile\":\"fast-local\",\"composerProfile\":\"\"}"));
    }

    /** 키가 있는 콘솔로 답한다 — 스펙은 "켤 수 있는 자리"를 재고, 브라우저 쪽은 못 켠다. */
    @Override
    public void pushKey(Consumer<String> key) { key.accept("BM9-demo-key"); }

    @Override
    public void push(String endpoint, String p256dh, String auth, boolean delete, Runnable then) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_push",
                (delete ? "off|" : "on|") + endpoint);
        then.run();
    }

    @Override
    public void save(String socket, String peer, String field, String value, Runnable then) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_saved",
                (socket == null ? "global" : socket) + "|" + field + "=" + value);
        then.run();
    }
}
