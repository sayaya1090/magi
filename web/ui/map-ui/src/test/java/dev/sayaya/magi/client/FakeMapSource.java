package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.MapSource;
import elemental2.core.Global;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 두 머신 — 여기(you: core 팀 둘)와 buildbox(들인 것, 통째 침묵) — 와 오간 것 둘:
 * 같은 상자 안의 도는 중 하나, 침묵한 머신으로 간 것 하나(닿을 수 없음).
 */
@Singleton
public class FakeMapSource implements MapSource {
    @Inject
    public FakeMapSource() {}

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
