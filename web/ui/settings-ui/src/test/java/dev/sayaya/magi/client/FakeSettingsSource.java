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

    @Override
    public void save(String socket, String peer, String field, String value, Runnable then) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_saved",
                (socket == null ? "global" : socket) + "|" + field + "=" + value);
        then.run();
    }
}
