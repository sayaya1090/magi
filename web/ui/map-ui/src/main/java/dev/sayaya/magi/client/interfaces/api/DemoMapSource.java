package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.client.usecase.MapSource;
import dev.sayaya.magi.bridge.RosterSharing;
import elemental2.core.Global;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 두 머신 — 여기(you: core 팀 둘)와 buildbox(들인 것, 통째 침묵) — 와 오간 것 둘:
 * 같은 상자 안의 도는 중 하나, 침묵한 머신으로 간 것 하나(닿을 수 없음).
 */
/**
 * 데몬 없이 이 화면이 답하는 것 — 이 모듈이 <b>제 목을 싣는다</b>.
 *
 * 목이 모듈 안에 있는 이유는 배포가 모듈 단위이기 때문이다: 화면은 저마다 컴파일돼 저마다의
 * 주기로 나가고 제 창에서 제 회선으로 말한다. 페이지가 남의 창에 목을 밀어 넣는 방식은 그
 * 구조를 거스르고, 창 하나만 갈아끼우면 iframe 안의 모듈에는 닿지도 않는다(실측).
 */
@Singleton
public class DemoMapSource implements MapSource {
    @Inject
    public DemoMapSource() {}

    @Override
    public void fleet(Consumer<Object> cb) {
        // 이 화면의 명단은 <b>셸의 명단</b>이다 — 데모라고 해서 제 함대를 따로 지어 두면, 같은
        // 창의 두 화면이 서로 다른 기계와 다른 이름을 보여 준다(실측: 지도만 mac/buildbox였다).
        // 진짜 콘솔에서 이 화면이 하는 것과 같은 일이기도 하다: 명단의 주인은 셸 하나다.
        RosterSharing.subscribe(cb::accept);
    }

    @Override
    public void handoffs(Consumer<Object> cb) {
        // 구 콘솔의 데모와 같은 둘 — 하나는 끝났고 하나는 아직 기다린다.
        cb.accept(Global.JSON.parse(
                "[{\"from\":\"design\",\"to\":\"buttons\",\"socket\":\"/demo/buttons.sock\",\"state\":\"idle\"}," +
                 "{\"from\":\"design\",\"to\":\"api\",\"socket\":\"/demo/api.sock\",\"state\":\"waiting\"}]"));
    }
}
