package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.client.usecase.MeetingSource;
import elemental2.core.Global;
import jsinterop.base.Js;

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
        cb.accept(Global.JSON.parse(ROOMS));
    }

    @Override
    public void fleet(Consumer<Object> cb) { RosterSharing.subscribe(cb::accept); }

    @Override
    public void room(String id, Consumer<Object> cb) {
        // 방 하나는 목록의 그 방 그대로다 — 목록과 방이 다른 사실을 말하지 않게(하나의 픽스처).
        elemental2.core.JsArray<Object> all = Js.uncheckedCast(Global.JSON.parse(ROOMS));
        for (int i = 0; i < all.length; i++) {
            jsinterop.base.JsPropertyMap<Object> one = Js.uncheckedCast(all.getAt(i));
            if (id.equals(String.valueOf(one.get("id")))) { cb.accept(one); return; }
        }
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

    /**
     * 구 콘솔의 데모와 같은 여섯 — 열려 있는 것, 아직 준비 중인 것, 닿지 못하는 참가자가 있는 것,
     * 두 번 쉰 참가자가 있는 것, 그리고 각자 할 일을 안고 끝난 것 둘. 잘 풀린 경우만 담은 픽스처는
     * 이 화면이 왜 있는지 한 번도 보여 주지 못한다.
     */
    private static final String ROOMS = "[{\"id\": \"m20260813-090400-0\", \"topic\": \"how long may the fleet table take to load, and who owns it\","
            + " \"opened\": true, \"round\": 2, \"max\": 5, \"holder\": \"you\", \"held\": true, \"trouble\": \"ops: no daemon at /demo/ops.sock\","
            + " \"speakers\": [{\"name\": \"design\", \"socket\": \"/demo/design.sock\"}, {\"name\": \"api\", \"socket\": \"/demo/api.sock\","
            + " \"next\": true}, {\"name\": \"ops\", \"socket\": \"/demo/ops.sock\", \"passes\": 2}, {\"name\": \"you\","
            + " \"person\": true}], \"said\": [{\"who\": \"design\", \"round\": 1, \"at\": \"2026-08-13T09:04:10Z\", \"text\": \"It is 900ms today and most of that is the roster,"
            + " not the render. Anything over 200ms and people stop trusting the state column — they refresh instead of reading it.\"},"
            + " {\"who\": \"api\", \"round\": 1, \"at\": \"2026-08-13T09:05:02Z\", \"text\": \"The roster is mine. 200ms is reachable if I stop resolving each workspace path on the way out; that is a lookup per companion and it is not what anybody is reading.\"},"
            + " {\"who\": \"ops\", \"round\": 1, \"pass\": true, \"at\": \"2026-08-13T09:05:40Z\", \"text\": \"there is no number to hold anybody to yet\"},"
            + " {\"who\": \"you\", \"round\": 1, \"at\": \"2026-08-13T09:06:11Z\", \"text\": \"Take 200ms as the budget. @ops,"
            + " what does it cost to watch it?\"}, {\"who\": \"design\", \"round\": 2, \"at\": \"2026-08-13T09:07:30Z\","
            + " \"text\": \"Then the table renders whatever has arrived and marks the rest as still coming,"
            + " rather than waiting for the slowest machine.\\n\\n**What that costs**\\n\\n- one more state per row,"
            + " which the colour already has room for\\n- the empty table says *waiting for the roster* rather than *no companions*\\n\\n> and the slow machine stops deciding when everybody else sees the list\"}]},"
            + " {\"id\": \"m20260814-081500-0\", \"topic\": \"should the console keep polling for the fleet, or stream it\","
            + " \"opened\": false, \"round\": 1, \"max\": 5, \"speakers\": [{\"name\": \"api\", \"socket\": \"/demo/api.sock\","
            + " \"ready\": true, \"brief\": \"The stream is already there for the transcript; the fleet is the only poll left on my side.\"},"
            + " {\"name\": \"design\", \"socket\": \"/demo/design.sock\"}, {\"name\": \"ops\", \"socket\": \"/demo/ops.sock\","
            + " \"trouble\": \"no daemon at /demo/ops.sock\"}, {\"name\": \"you\", \"person\": true}]}, {\"id\": \"m20260813-142000-0\","
            + " \"topic\": \"the retry budget: who owns it and what happens when it runs out\", \"opened\": true,"
            + " \"round\": 1, \"max\": 5, \"holder\": \"api\", \"speakers\": [{\"name\": \"api\", \"socket\": \"/demo/api.sock\"},"
            + " {\"name\": \"ops\", \"socket\": \"/demo/ops.sock\", \"next\": true}, {\"name\": \"you\", \"person\": true}],"
            + " \"said\": [{\"who\": \"api\", \"round\": 1, \"at\": \"2026-08-13T14:20:14Z\", \"text\": \"Two hundred milliseconds and three tries is what the client assumes today. Nothing enforces either number and both are written in two places.\"}]},"
            + " {\"id\": \"m20260813-101200-0\", \"topic\": \"do we keep the fleet table or make it a list on narrow screens\","
            + " \"opened\": true, \"round\": 3, \"max\": 5, \"holder\": \"design\", \"held\": true, \"speakers\": [{\"name\": \"design\","
            + " \"socket\": \"/demo/design.sock\"}, {\"name\": \"buttons\", \"socket\": \"/demo/buttons.sock\", \"passes\": 2},"
            + " {\"name\": \"palette\", \"socket\": \"/demo/design.sock2\", \"next\": true}, {\"name\": \"you\", \"person\": true}],"
            + " \"said\": [{\"who\": \"design\", \"round\": 1, \"at\": \"2026-08-13T10:12:30Z\", \"text\": \"At 390px the table is four columns of two characters. It is a list there,"
            + " and the columns come back when there is room for them.\"}, {\"who\": \"palette\", \"round\": 2,"
            + " \"at\": \"2026-08-13T10:14:02Z\", \"text\": \"Then the state colour has to survive the change of shape — in a list it is the only column left.\"},"
            + " {\"who\": \"buttons\", \"round\": 2, \"pass\": true, \"at\": \"2026-08-13T10:14:40Z\", \"text\": \"nothing here touches a control of mine\"}]},"
            + " {\"id\": \"m20260811-093000-0\", \"topic\": \"why the console asks twice before it stops a turn\","
            + " \"opened\": true, \"round\": 2, \"max\": 3, \"closed\": true, \"speakers\": [{\"name\": \"api\", \"socket\": \"/demo/api.sock\"},"
            + " {\"name\": \"ops\", \"socket\": \"/demo/ops.sock\"}, {\"name\": \"you\", \"person\": true}], \"said\": [{\"who\": \"ops\","
            + " \"round\": 1, \"at\": \"2026-08-11T09:30:20Z\", \"text\": \"The second question is the daemon asking,"
            + " and it arrives after the console has already asked. One of the two is redundant and it is ours.\"},"
            + " {\"who\": \"api\", \"round\": 1, \"at\": \"2026-08-11T09:31:05Z\", \"text\": \"Agreed — the daemon is the one that knows whether the turn is still running,"
            + " so the console should stop asking.\"}], \"tasks\": [{\"who\": \"ops\", \"what\": \"Drop the console-side confirmation and let the daemon be the one that asks.\"},"
            + " {\"who\": \"api\", \"what\": \"Make the stop path answer \\\"already finished\\\" instead of an error.\"}]},"
            + " {\"id\": \"m20260812-161500-0\", \"topic\": \"what to do about the empty state nobody specified\","
            + " \"opened\": true, \"round\": 2, \"max\": 2, \"closed\": true, \"speakers\": [{\"name\": \"design\", \"socket\": \"/demo/design.sock\"},"
            + " {\"name\": \"buttons\", \"socket\": \"/demo/buttons.sock\"}, {\"name\": \"you\", \"person\": true}], \"said\": [{\"who\": \"design\","
            + " \"round\": 1, \"at\": \"2026-08-12T16:15:20Z\", \"text\": \"Three components invent their own empty state and two of them invent a colour for it.\"},"
            + " {\"who\": \"buttons\", \"round\": 1, \"pass\": true, \"at\": \"2026-08-12T16:16:02Z\"}], \"tasks\": [{\"who\": \"design\","
            + " \"what\": \"Write the empty-state spec, naming the token each surface uses, and put it in docs/empty-states.md.\"},"
            + " {\"who\": \"buttons\", \"what\": \"\"}]}]";
}
