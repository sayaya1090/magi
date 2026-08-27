package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.client.usecase.MapSource;
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
        cb.accept(Global.JSON.parse(
                "[{\"socket\":\"/a\",\"name\":\"build\",\"team\":\"core\",\"state\":\"working\",\"live\":true," +
                  "\"host\":\"mac\",\"addr\":\"10.0.0.7\",\"instance\":\"you@mac\",\"trust\":\"own\",\"hub\":true,\"idle\":3}," +
                 "{\"socket\":\"/b\",\"name\":\"docs\",\"team\":\"core\",\"state\":\"idle\",\"live\":true," +
                  "\"host\":\"mac\",\"instance\":\"you@mac\",\"trust\":\"own\",\"idle\":60}," +
                 "{\"socket\":\"/c\",\"name\":\"ci\",\"state\":\"remote\",\"live\":false,\"elsewhere\":true," +
                  "\"host\":\"buildbox\",\"instance\":\"agent@buildbox\",\"trust\":\"admitted\",\"idle\":300}]"));
    }

    @Override
    public void handoffs(Consumer<Object> cb) {
        cb.accept(Global.JSON.parse(
                "[{\"from\":\"build\",\"to\":\"docs\",\"socket\":\"/b\",\"state\":\"working\"}," +
                 "{\"from\":\"build\",\"to\":\"ci\",\"socket\":\"/c\",\"state\":\"done\"}]"));
    }
}
