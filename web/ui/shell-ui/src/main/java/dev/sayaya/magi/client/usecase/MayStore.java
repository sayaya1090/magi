package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.May;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 내가 무엇을 해도 되는가 — /me를 한 번 물어 문을 거른다(운영 loadMe/applyMay의 이식).
 * 아무도 설정 안 된 콘솔은 "전부"라 답하고, 그때 이 저장소는 아무것도 바꾸지 않는다:
 * 1인 콘솔이 청하지 않은 권한 모델을 얻으면 안 된다(운영 규칙). 게이트는 늘 서버가 진다 —
 * 여기는 눌러서 거절에 닿는 문을 접는 것뿐이다.
 */
@Singleton
public class MayStore {
    private final List<Consumer<MayStore>> observers = new ArrayList<>();

    @Inject
    public MayStore() {}

    /** 창에 하나 — 화면 모듈들도 같은 답을 든다(bridge.May). */
    public void start() {
        May.load(() -> { for (Consumer<MayStore> o : observers) o.accept(this); });
    }

    public boolean may(String cap) { return May.can(cap); }

    public void subscribe(Consumer<MayStore> o) {
        observers.add(o);
        o.accept(this);
    }
}
