package dev.sayaya.magi.demo;

import elemental2.core.Global;
import elemental2.dom.RequestInit;
import elemental2.dom.Response;
import elemental2.promise.Promise;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 회의 — 목록과 방 하나, 그리고 그 방의 전사.
 *
 * 방 하나는 목록의 그 방 그대로다: 목록과 방이 다른 사실을 말하지 않게 픽스처는 하나다.
 * 데모는 방을 <b>열지 않는다</b> — 이름 없는 주소로 사람을 보내느니 목록에 머문다.
 */
final class Meetings {
    private Meetings() {}

    static Promise<Response> answer(String path, String url, RequestInit init) {
        // 회의의 행동 넷은 <b>이름을 대고</b> 답한다 — 바닥을 잡는 것, 논의를 끝내고 각자에게
        // 무엇을 할 것인지 묻는 것, 결론을 일로 넘기는 것은 서로 다른 일이다. 셋 다
        // "would have sent: POST"로 적으면 데모는 그것들이 같은 짓이라고 가르친다(운영 목이
        // 이 넷만 따로 이름 댄 이유). 넘기기 하나가 이 기능에서 유일하게 워크스페이스에 닿는다.
        switch (path) {
            case "/meet-open":  return Mock.did("would have put the meeting back in session");
            case "/meet-say":   return Mock.did("would have taken the floor and said it");
            case "/meet-close": return Mock.did("would have ended the discussion and asked each of them "
                    + "what they will do");
            case "/meet-hand":  return Mock.did("would have sent that conclusion to the companion as work, "
                    + "in its own session");
            default: break;
        }
        if (!"/meet".equals(path)) return null;
        // 방을 여는 것은 받아만 둔다 — 빈 답에 JSON.parse가 걸려 콘솔이 null을 내고, 그것이
        // "이름 없는 주소로 사람을 보내지 않는다"이다(FetchMeetingSource.convene의 계약).
        if (Mock.wrote(init)) return Mock.took(path, init);
        String id = Mock.param(url, "id");
        if (id.isEmpty()) return Mock.json(ROOMS);
        elemental2.core.JsArray<Object> all = Js.uncheckedCast(Global.JSON.parse(ROOMS));
        for (int i = 0; i < all.length; i++) {
            JsPropertyMap<Object> one = Js.uncheckedCast(all.getAt(i));
            if (id.equals(String.valueOf(one.get("id")))) return Mock.json(Global.JSON.stringify(one));
        }
        // 없는 방은 없다고 답한다 — 빈 방과 사라진 방은 다른 화면이다.
        return Mock.json("null");
    }

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
