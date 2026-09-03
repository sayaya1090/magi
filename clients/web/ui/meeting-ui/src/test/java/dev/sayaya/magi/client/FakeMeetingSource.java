package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.MeetingSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 회선 대신 고정된 답 — 그리고 스펙이 방의 단계를 바꿀 수 있게 창에 문을 하나 연다
 * (window.__magi_test_room). 회의는 남이 말해서 바뀌는 화면이라, 그 "바뀜"을 만들 수
 * 있어야 단계별 화면을 잴 수 있다.
 */
@Singleton
public class FakeMeetingSource implements MeetingSource {
    private String stage = "open";     // open | held | closed
    // 회의록. 한 번 고쳐진 판을 만들 수 있어야 「바뀐 줄에만 강조」를 잰다 — 그 주장은 두 판을
    // 견줘야만 참이 되고, 한 판만으로는 전부 강조해도 통과한다.
    private String minutes = "## Decided\n- postgres\n## Still open\n- the retry budget";
    private Consumer<Object> waiting = null;

    @Inject
    public FakeMeetingSource() { door(); }

    private void door() {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_room", (StageFn) s -> {
            if (s.startsWith("minutes:")) {
                minutes = s.substring("minutes:".length()).replace("|", "\n");
                if (waiting != null) waiting.accept(roomOf());
                return;
            }
            stage = s;
            if (waiting != null) waiting.accept(roomOf());
        });
    }

    @jsinterop.annotations.JsFunction
    public interface StageFn { void call(String stage); }

    @Override
    public void rooms(Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[{\"id\":\"m1\",\"topic\":\"which store for the queue?\"," +
                "\"round\":2,\"max\":5,\"speakers\":[{\"name\":\"alpha\"},{\"name\":\"beta\"}]}," +
                "{\"id\":\"m0\",\"topic\":\"the retry storm\",\"closed\":true,\"spent\":true," +
                "\"tasks\":[{\"who\":\"alpha\",\"what\":\"write the postmortem\"}]," +
                "\"speakers\":[{\"name\":\"alpha\"}]}]"));
    }

    @Override
    public void fleet(Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[" +
                "{\"socket\":\"/tmp/a1.sock\",\"name\":\"alpha\",\"team\":\"core\",\"role\":\"keeps the build green\",\"instance\":\"you@mac\"}," +
                "{\"socket\":\"/tmp/b1.sock\",\"name\":\"beta\",\"team\":\"docs\",\"instance\":\"you@mac\"}," +
                "{\"socket\":\"/tmp/c1.sock\",\"name\":\"gamma\",\"elsewhere\":true}]"));
    }

    private Object roomOf() {
        String common = "\"id\":\"m1\",\"topic\":\"which store for the queue?\",\"round\":2,\"max\":5," +
                "\"speakers\":[{\"name\":\"alpha\",\"socket\":\"/tmp/a1.sock\",\"room\":\"s_a\",\"next\":true}," +
                "{\"name\":\"beta\",\"socket\":\"/tmp/b1.sock\",\"room\":\"s_b\"}," +
                "{\"name\":\"you\",\"person\":true}]," +
                "\"said\":[{\"who\":\"alpha\",\"round\":1,\"text\":\"postgres, for the ordering\"}," +
                "{\"who\":\"beta\",\"round\":1,\"pass\":true,\"text\":\"nothing to add\"}]," +
                "\"minutes\":" + Global.JSON.stringify(minutes);
        switch (stage) {
            case "held":
                return Global.JSON.parse("{" + common + ",\"opened\":true,\"holder\":\"alpha\"}");
            case "closed":
                return Global.JSON.parse("{" + common + ",\"opened\":true,\"closed\":true," +
                        "\"tasks\":[{\"who\":\"alpha\",\"what\":\"write the migration\"}," +
                        "{\"who\":\"beta\",\"what\":\"\"}]}");
            default:
                return Global.JSON.parse("{" + common + ",\"opened\":true}");
        }
    }

    @Override
    public void room(String id, Consumer<Object> cb) {
        waiting = cb;
        // 몇 번 다시 읽었는지 — 「거절이면 다시 읽지 않는다」를 스펙이 <b>직접</b> 재기 위해서다.
        // 화면으로만 재면 시간에 기댄다: 다시 읽기가 사유를 지우기 전에 스펙이 그 사유를 보고
        // 지나가 버려, 지우는 코드를 넣어도 초록이 나온다(실측 — 되돌림 검사가 이걸 잡았다).
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        Object n = win.get("__magi_test_reads");
        win.set("__magi_test_reads", (n == null ? 0 : (int) Double.parseDouble(String.valueOf(n))) + 1);
        if ("gone".equals(id)) { cb.accept(null); return; }
        cb.accept(roomOf());
    }

    @Override
    public void convene(String topic, String[] sockets, Consumer<Object> made, Consumer<String> why) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_convened", topic + "|" + String.join(",", sockets));
        String no = refuses();
        if (!no.isEmpty()) { why.accept(no); return; }
        made.accept(Global.JSON.parse("{\"id\":\"m9\"}"));
    }

    @Override
    public void say(String id, String text, String call, boolean hold, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_said",
                id + "|" + (text == null ? "" : text) + "|" + (call == null ? "" : call) + "|" + hold);
        why.accept("");
    }

    @Override
    public void close(String id, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_closed", id);
        String no = refuses();
        // 거절하면 <b>아무것도 바꾸지 않는다</b> — 서버가 거절하고도 방을 닫아 두지는 않는다.
        if (!no.isEmpty()) { why.accept(no); return; }
        stage = "closed";
        why.accept("");
    }

    @Override
    public void reopen(String id, String text, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_reopened", id + "|" + (text == null ? "" : text));
        String no = refuses();
        if (!no.isEmpty()) { why.accept(no); return; }
        why.accept("");
    }

    /** 스펙이 창에 적어 두면 그 다음 쓰기가 거절당한다 — 서버가 사유를 실어 돌려보내듯. */
    private static String refuses() {
        Object v = Js.asPropertyMap(DomGlobal.window).get("__magi_test_press_refuses");
        return v == null ? "" : String.valueOf(v);
    }

    @Override
    public void hand(String id, String who, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_handed", id + "|" + who);
        why.accept("");
    }

    @Override
    public void roomRows(String socket, String room, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[{\"who\":\"user\",\"text\":\"round 1: which store?\"}," +
                "{\"who\":\"thinking\",\"text\":\"weigh ordering against ops\"}," +
                "{\"who\":\"tool\",\"tool\":\"bash\",\"out\":\"pg_isready: accepting\"}," +
                "{\"who\":\"assistant\",\"text\":\"postgres, for the ordering\"}]"));
    }
}
