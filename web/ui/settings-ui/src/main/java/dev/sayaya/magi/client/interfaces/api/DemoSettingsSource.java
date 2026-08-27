package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.client.usecase.SettingsSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** 데몬 대신 고정된 완성 설정 — 저장은 창에 적어 스펙이 무엇이 갔는지 본다. */
/**
 * 데몬 없이 이 화면이 답하는 것 — 이 모듈이 <b>제 목을 싣는다</b>.
 *
 * 목이 모듈 안에 있는 이유는 배포가 모듈 단위이기 때문이다: 화면은 저마다 컴파일돼 저마다의
 * 주기로 나가고 제 창에서 제 회선으로 말한다. 페이지가 남의 창에 목을 밀어 넣는 방식은 그
 * 구조를 거스르고, 창 하나만 갈아끼우면 iframe 안의 모듈에는 닿지도 않는다(실측).
 */
@Singleton
public class DemoSettingsSource implements SettingsSource {
    @Inject
    public DemoSettingsSource() {}

    @Override
    public void read(String socket, String peer, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("{\"file\":\"~/.config/magi/config.toml\"," +
                "\"ambient\":true,\"crossSession\":false," +
                "\"profiles\":[\"fast-local\",\"cloud-mini\"]," +
                "\"codeProfile\":\"fast-local\",\"composerProfile\":\"\"}"));
    }

    @Override
    public void profiles(String socket, Consumer<Object> list) {
        // 구 콘솔의 데모와 같은 셋 — 하나에는 키가 있어 "정해졌지만 보여 주지는 않는" 상태를
        // 보인다(값은 어느 화면에도 오지 않는다: 있다는 사실만 온다).
        list.accept(elemental2.core.Global.JSON.parse(
                "[{\"name\":\"balanced\",\"tier\":\"global\",\"baseUrl\":\"http://localhost:11434/v1\","
                        + "\"model\":\"qwen3-coder:30b\",\"hasKey\":false,\"file\":\"~/.config/magi/config.toml\"},"
                        + "{\"name\":\"fast\",\"tier\":\"global\",\"baseUrl\":\"http://localhost:11434/v1\","
                        + "\"model\":\"qwen2.5-coder:1.5b\",\"hasKey\":false,\"file\":\"~/.config/magi/config.toml\"},"
                        + "{\"name\":\"cloud\",\"tier\":\"project\",\"companion\":\"design\","
                        + "\"socket\":\"/demo/design.sock\",\"baseUrl\":\"https://api.example.com/v1\","
                        + "\"model\":\"big-model\",\"hasKey\":true,"
                        + "\"file\":\"/Users/you/work/design-system/.magi/config.toml\"}]"));
    }

    @Override
    public void providers(Consumer<Object> list) {
        // 구 콘솔의 데모와 같은 하나 — 짧은 카탈로그라 두 고르개가 하는 일이 보인다.
        list.accept(elemental2.core.Global.JSON.parse(
                "[{\"name\":\"gateway\",\"base\":\"http://127.0.0.1:47311/v1\","
                        + "\"models\":[\"fast\",\"balanced\",\"deep\"]}]"));
    }

    @Override
    public void saveProfile(String socket, String name, String baseUrl, String model, String key,
                            boolean delete, Consumer<String> why) {
        why.accept("");
    }

    /** 데모에는 뒤에 함대를 보는 것이 없다 — 키가 없다고 답하고, 화면이 그 사실을 적는다. */
    @Override
    public void pushKey(Consumer<String> key) { key.accept(""); }

    @Override
    public void push(String endpoint, String p256dh, String auth, boolean delete, Runnable then) {
        then.run();
    }

    @Override
    public void save(String socket, String peer, String field, String value, Runnable then) {
        then.run();
    }
}
