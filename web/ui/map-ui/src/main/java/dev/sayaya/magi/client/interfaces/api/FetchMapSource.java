package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.MapSource;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

@Singleton
public class FetchMapSource implements MapSource {
    @Inject
    public FetchMapSource() {}

    /**
     * 명단은 셸의 것이다 — 여기서 /fleet을 다시 읽지 않는다. 같은 질문을 두 모듈이 회선으로
     * 물으면 그것이 곧 단일 원천이 아니라는 증거이고, 창당 스트림 하나라는 규칙도 그렇게 샌다.
     */
    @Override
    public void fleet(Consumer<Object> cb) { dev.sayaya.magi.bridge.RosterSharing.subscribe(cb::accept); }

    @Override
    public void handoffs(Consumer<Object> cb) { Console.fetchList("/handoffs", cb::accept); }
}
