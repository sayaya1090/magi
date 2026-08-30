package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.MapSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 두 머신 — 여기(you: core 팀 둘)와 buildbox(들인 것, 통째 침묵) — 와 오간 것 둘:
 * 같은 상자 안의 도는 중 하나, 침묵한 머신으로 간 것 하나(닿을 수 없음).
 *
 * 명단은 한 번 오고 마는 것이 아니라 <b>계속 흐른다</b>. 그 흐름이 무엇을 다시 세우고
 * 무엇을 고쳐 쓰는지가 이 화면의 계약이라, 테스트가 프레임을 하나 더 밀 문을 둔다:
 * {@code window.__magi_test_map_tick(socket, state)} — 그 노드의 상태 하나만 갈아 다시 흘린다.
 */
@Singleton
public class FakeMapSource implements MapSource {
    @JsFunction
    interface Tick { void run(String socket, String state); }

    private Consumer<Object> listening = null;

    @Inject
    public FakeMapSource() {}

    @Override
    public void fleet(Consumer<Object> cb) {
        listening = cb;
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_map_tick", (Tick) this::tick);
        cb.accept(fleetJson("working"));
    }

    /** 그 노드의 상태만 간 프레임을 다시 흘린다 — 명단이 실제로 하는 일이 이것이다. */
    private void tick(String socket, String state) {
        if (listening == null) return;
        listening.accept("/a".equals(socket) ? fleetJson(state) : fleetJson("working"));
    }

    private Object fleetJson(String first) {
        return Global.JSON.parse(
                "[{\"socket\":\"/a\",\"name\":\"build\",\"team\":\"core\",\"state\":\"" + first + "\",\"live\":true," +
                  "\"host\":\"mac\",\"addr\":\"10.0.0.7\",\"instance\":\"you@mac\",\"trust\":\"own\",\"hub\":true,\"idle\":3}," +
                 "{\"socket\":\"/b\",\"name\":\"docs\",\"team\":\"core\",\"state\":\"idle\",\"live\":true," +
                  "\"host\":\"mac\",\"instance\":\"you@mac\",\"trust\":\"own\",\"idle\":60}," +
                 // 쉰 시간이 <b>작다</b>(8초): 이 상자의 나이는 초 단위로 자라야 스펙이 늙는 것을 볼 수
                 // 있다 — 300이면 "5m"이 1분 내내 그대로다. 침묵을 만드는 것은 이 숫자가
                 // 아니라 live:false다.
                 "{\"socket\":\"/c\",\"name\":\"ci\",\"state\":\"remote\",\"live\":false,\"elsewhere\":true," +
                  "\"host\":\"buildbox\",\"instance\":\"agent@buildbox\",\"trust\":\"admitted\",\"idle\":8}]");
    }

    @Override
    public void handoffs(Consumer<Object> cb) {
        cb.accept(Global.JSON.parse(
                "[{\"from\":\"build\",\"to\":\"docs\",\"socket\":\"/b\",\"state\":\"working\"}," +
                 "{\"from\":\"build\",\"to\":\"ci\",\"socket\":\"/c\",\"state\":\"done\"}]"));
    }
}
