package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.RenderSharing;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.HashMap;
import java.util.Map;
import java.util.function.Consumer;

/**
 * 화면 모듈이 창 브리지로 민 렌더의 저장소 — handbook RenderStore의 자리.
 *
 * 렌더는 주인이 없이 도착하므로(브리지는 값 하나만 나른다), 지금 로드 중인 목적지
 * (expect)가 그 주인이다. 목적지별로 캐시해 재방문이 스크립트 재주입 없이 다시 그린다.
 */
@Singleton
public class RenderStore {
    private final Map<String, Object> renders = new HashMap<>();
    private String pending = null;
    private Consumer<Object> onRender = r -> {};

    @Inject
    public RenderStore() {
        RenderSharing.register(this::take);
    }

    /** 다음에 도착하는 렌더의 주인을 지목한다 — 모듈 주입 직전에 부른다. */
    public void expect(String destId) { pending = destId; }

    public Object renderOf(String destId) { return renders.get(destId); }

    public void onRender(Consumer<Object> cb) { this.onRender = cb; }

    private void take(Object render) {
        if (pending != null) renders.put(pending, render);
        onRender.accept(render);
    }
}
