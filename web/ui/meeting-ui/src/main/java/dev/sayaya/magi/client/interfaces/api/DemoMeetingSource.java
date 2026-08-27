package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.client.usecase.MeetingSource;
import elemental2.core.Global;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 데몬 없이 도는 회의실 — 이 모듈이 제 목을 싣는다.
 *
 * 부를 수 있는 이들은 여기서 지어내지 않는다: 명단은 셸의 것이라 데모에서도 브리지로 온다.
 * 그래야 데모의 회의실이 데모의 플릿과 같은 이름을 말한다 — 화면마다 다른 세상을 지으면
 * 그 데모는 아무것도 증명하지 못한다.
 */
@Singleton
public class DemoMeetingSource implements MeetingSource {
    @Inject
    public DemoMeetingSource() {}

    @Override
    public void rooms(Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[" + GOING + "," + DONE + "]"));
    }

    @Override
    public void fleet(Consumer<Object> cb) { RosterSharing.subscribe(cb::accept); }

    @Override
    public void room(String id, Consumer<Object> cb) {
        if ("m0".equals(id)) { cb.accept(Global.JSON.parse(room(DONE, true))); return; }
        if ("m1".equals(id)) { cb.accept(Global.JSON.parse(room(GOING, false))); return; }
        cb.accept(null);   // 없는 방은 없다고 답한다 — 빈 방과 사라진 방은 다른 화면이다
    }

    @Override
    public void convene(String topic, String[] sockets, Consumer<Object> made, Consumer<String> why) {
        // 데모는 방을 열지 않는다: 이름 없는 주소로 사람을 보내느니 목록에 머문다.
        made.accept(null);
    }

    @Override
    public void say(String id, String text, String call, boolean hold, Consumer<String> why) { why.accept(""); }

    @Override
    public void close(String id, Runnable then) { then.run(); }

    @Override
    public void reopen(String id, String why, Runnable then) { then.run(); }

    @Override
    public void hand(String id, String who, Consumer<String> why) { why.accept(""); }

    @Override
    public void roomRows(String socket, String room, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[{\"who\":\"user\",\"text\":\"round 1: which store?\"},"
                + "{\"who\":\"thinking\",\"text\":\"weigh ordering against ops\"},"
                + "{\"who\":\"tool\",\"tool\":\"bash\",\"out\":\"pg_isready: accepting connections\",\"ok\":true},"
                + "{\"who\":\"assistant\",\"text\":\"postgres, for the ordering\"}]"));
    }

    private static final String GOING = "{\"id\":\"m1\",\"topic\":\"which store should the queue use?\","
            + "\"round\":2,\"max\":5,\"opened\":true,"
            + "\"speakers\":[{\"name\":\"build\"},{\"name\":\"docs\"}]}";
    private static final String DONE = "{\"id\":\"m0\",\"topic\":\"why the retries stormed\","
            + "\"closed\":true,\"spent\":true,\"speakers\":[{\"name\":\"build\"},{\"name\":\"review\"}],"
            + "\"tasks\":[{\"who\":\"build\",\"what\":\"cap the backoff and write it down\"},"
            + "{\"who\":\"review\",\"what\":\"\"}]}";

    /** 방 하나는 목록의 그 방에 명단과 오간 말을 더한 것이다 — 둘이 다른 사실을 말하지 않게. */
    private static String room(String base, boolean closed) {
        String speakers = "\"speakers\":[{\"name\":\"build\",\"socket\":\"/demo/build.sock\",\"room\":\"s_demo1\""
                + (closed ? "" : ",\"next\":true") + "},"
                + "{\"name\":\"" + (closed ? "review" : "docs") + "\",\"socket\":\"/demo/docs.sock\","
                + "\"room\":\"s_demo2\",\"passes\":1},{\"name\":\"you\",\"person\":true}]";
        String said = closed
                ? "\"said\":[{\"who\":\"build\",\"round\":1,\"text\":\"the retries had no ceiling\"},"
                  + "{\"who\":\"review\",\"round\":1,\"pass\":true,\"text\":\"nothing to add\"}]"
                : "\"said\":[{\"who\":\"build\",\"round\":1,\"text\":\"postgres — the ordering is the point\"},"
                  + "{\"who\":\"docs\",\"round\":1,\"text\":\"sqlite keeps the ops story small\"}]";
        return base.substring(0, base.length() - 1) + "," + speakers + "," + said + "}";
    }
}
