package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.May;
import dev.sayaya.rx.subject.BehaviorSubject;
import lombok.experimental.Delegate;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.rx.subject.BehaviorSubject.behavior;

/**
 * 내가 무엇을 해도 되는가 — /me를 한 번 물어 문을 거른다(운영 loadMe/applyMay의 이식).
 * 아무도 설정 안 된 콘솔은 "전부"라 답하고, 그때 이 저장소는 아무것도 바꾸지 않는다:
 * 1인 콘솔이 청하지 않은 권한 모델을 얻으면 안 된다(운영 규칙). 게이트는 늘 서버가 진다 —
 * 여기는 눌러서 거절에 닿는 문을 접는 것뿐이다.
 *
 * 흐르는 것은 <b>답이 도착했다</b>는 사실 하나다(내용은 창의 것이고 May가 안다). 그래서
 * 값은 그 답의 나이: 구독한 판은 지금 상태로 한 번, 답이 오면 한 번 더 그린다.
 */
@Singleton
public class MayStore {
    @Delegate private final BehaviorSubject<Integer> _this = behavior(0);

    @Inject
    public MayStore() {}

    /** 창에 하나 — 화면 모듈들도 같은 답을 든다(bridge.May). */
    public void start() {
        May.load(() -> _this.next(getValue() + 1));
    }

    public boolean may(String cap) { return May.can(cap); }
}
