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
    public void save(String socket, String peer, String field, String value, Runnable then) {
        then.run();
    }
}
