package dev.sayaya.magi.demo;

import elemental2.dom.RequestInit;
import elemental2.dom.Response;
import elemental2.promise.Promise;

/**
 * 사람과 무리 — 무리 하나(범위 딸림)와 사람 둘(나=admin, 한 컴패니언으로 좁혀진 사람).
 *
 * 디렉터리에 물린 콘솔에서는 <b>무리가 곧 명부</b>이고(사람은 고용되고 떠나는 자리에서
 * 관리된다) 개인은 그 예외다. 그래서 예외 둘을 담는다.
 *
 * 그리고 손길 — 지도가 읽는 그 둘: 하나는 끝났고 하나는 아직 기다린다.
 */
final class People {
    private static String role = "responder";
    private static String scope = "api";
    private static boolean gone = false;

    private People() {}

    static Promise<Response> answer(String path, RequestInit init) {
        switch (path) {
            case "/access":
                if (Mock.wrote(init)) {
                    String who = Mock.field(init, "who");
                    if (who.startsWith("sam")) {
                        if (!Mock.field(init, "delete").isEmpty()) gone = true;
                        else { role = Mock.field(init, "role"); scope = Mock.field(init, "companions"); }
                    }
                    return Mock.took(path, init);
                }
                return Mock.json(roster());
            // 손길은 지도의 것이다 — 컴패니언 곁의 판도 같은 길로 묻는다(경로 하나, 주인 하나).
            case "/handoffs": return Mock.json(
                    "[{\"from\":\"design\",\"to\":\"buttons\",\"socket\":\"/demo/buttons.sock\",\"state\":\"idle\"},"
                  + "{\"from\":\"design\",\"to\":\"api\",\"socket\":\"/demo/api.sock\",\"state\":\"waiting\"}]");
            default: return null;
        }
    }

    private static String roster() {
        String sam = gone ? "" :
                ",{\"who\":\"sam@example.com\",\"role\":\"" + role + "\",\"can\":[\"read\",\"answer\"]" +
                (scope.isEmpty() ? "" : ",\"companions\":[\"" + scope.replace(",", "\",\"") + "\"]") + "}";
        return "{\"configured\":true,\"named\":true," +
                "\"instance\":{\"who\":\"you@studio\",\"configDir\":\"/Users/you/.config/magi\"}," +
                "\"roles\":[{\"name\":\"operator\",\"can\":[\"read\",\"answer\",\"prompt\",\"curate\"," +
                          "\"configure\",\"admin\",\"shell\"]}," +
                          "{\"name\":\"responder\",\"can\":[\"read\",\"answer\"]}," +
                          "{\"name\":\"viewer\",\"can\":[\"read\"]}]," +
                "\"groups\":[{\"who\":\"platform-team\",\"role\":\"operator\",\"can\":[\"read\",\"answer\"," +
                          "\"prompt\",\"curate\",\"configure\",\"admin\",\"shell\"]}," +
                          "{\"who\":\"design-guild\",\"role\":\"responder\",\"can\":[\"read\",\"answer\"]," +
                          "\"companions\":[\"design\",\"palette\"]}," +
                          "{\"who\":\"everyone\",\"role\":\"viewer\",\"can\":[\"read\"]}]," +
                "\"people\":[{\"who\":\"you@studio\",\"role\":\"operator\",\"can\":[\"read\",\"answer\"," +
                          "\"prompt\",\"curate\",\"configure\",\"admin\",\"shell\"],\"me\":true}" +
                sam +
                ",{\"who\":\"contractor@example.com\",\"role\":\"viewer\",\"can\":[\"read\"]}]}";
    }
}
