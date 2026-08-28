package dev.sayaya.magi.demo;

import elemental2.core.Global;
import elemental2.core.JsArray;
import elemental2.dom.RequestInit;
import elemental2.dom.Response;
import elemental2.promise.Promise;
import jsinterop.base.Js;

/**
 * 배운 것·위키·서버 — 규칙 둘과 기억 하나(랭킹이 갈리게), 위키 둘(하나는 낡음), 서버 둘.
 *
 * 쓰기는 목록을 <b>실제로</b> 고친다: 잊은 것이 다음 조회에서 사라지는 것까지 보여야 이
 * 화면이 무엇을 하는 화면인지 알 수 있다.
 */
final class Knowledge {
    private static JsArray<Object> skills = null;
    private static JsArray<Object> mcp = null;
    private static JsArray<Object> wiki = null;

    private Knowledge() {}

    static Promise<Response> answer(String path, RequestInit init) {
        switch (path) {
            case "/skills": return Mock.json(Global.JSON.stringify(skills()));
            case "/wiki": return Mock.json(Global.JSON.stringify(wiki()));
            case "/mcp":
                // 같은 길이 읽기이자 쓰기다 — 무엇인지는 몸이 있느냐가 가른다(운영 계약).
                if (Mock.wrote(init)) {
                    String gone = Mock.field(init, "delete");
                    if (!gone.isEmpty()) {
                        String name = Mock.field(init, "name");
                        mcp = mcp().filter((v, i) -> !name.equals(Js.asPropertyMap(v).get("name")));
                    }
                    return Mock.json("");
                }
                return Mock.json(Global.JSON.stringify(mcp()));
            case "/forget": {
                String name = Mock.field(init, "name");
                skills = skills().filter((v, i) -> !name.equals(Js.asPropertyMap(v).get("name")));
                return Mock.json("");
            }
            case "/remember": return Mock.json("");
            default: return null;
        }
    }

    private static JsArray<Object> skills() {
        if (skills == null) skills = Js.uncheckedCast(Global.JSON.parse(
                "[ {\"name\": \"skill-tests-before-done\",\"kind\": \"skill\",\"tier\": "
                + "\"global\",\"observed\": 6,\"firstSeen\": \"2026-06-30\",\"lastSeen\": "
                + "\"2026-08-07\",\"description\": \"run the tests before saying it is done\",\"body\": \"Run "
                + "the project's own test command and read the output before reporting a task finished. A "
                + "build that compiles is not a test that passed, and \\\"it should work\\\" is the sentence "
                + "that precedes every regression in this repository.\\n\\nIf the tests cannot be run, say so "
                + "and say why, rather than landing the work quietly.\\n\\n(source: agent)\"}, {\"name\": "
                + "\"skill-tokens\",\"kind\": \"skill\",\"tier\": \"project\",\"companion\": "
                + "\"design\",\"socket\": \"/demo/design.sock\",\"observed\": 3,\"firstSeen\": "
                + "\"2026-07-14\",\"lastSeen\": \"2026-08-06\",\"description\": \"spacing comes from the "
                + "scale, never hand-written\",\"body\": \"Every margin and padding is a token from the "
                + "spacing scale. A hand-written value is one more thing to keep in step with the rest, and "
                + "it will not be.\\n\\n(source: agent)\"}, {\"name\": \"skill-empty-states\",\"kind\": "
                + "\"skill\",\"tier\": \"team\",\"team\": \"frontend\",\"observed\": 4,\"firstSeen\": "
                + "\"2026-07-20\",\"lastSeen\": \"2026-08-08\",\"description\": \"an empty state names the "
                + "thing that is absent and how it stops being absent\",\"body\": \"Two lines. The first says "
                + "what is not there; the second says the one action that would put something there. No "
                + "illustrations, no apologies.\\n\\n(source: agent \\u00b7 spec the empty state for the "
                + "fleet table)\"}, {\"name\": \"mem-staging\",\"kind\": \"memory\",\"tier\": "
                + "\"project\",\"companion\": \"api\",\"socket\": \"/demo/api.sock\",\"observed\": "
                + "1,\"lastSeen\": \"2026-08-05\",\"tags\": [\"ops\"],\"description\": \"the staging database "
                + "is restored from prod every Monday\"}]"));
        return skills;
    }

    private static JsArray<Object> wiki() {
        if (wiki == null) wiki = Js.uncheckedCast(Global.JSON.parse(
                "[ {\"title\": \"auth flow\",\"tier\": \"team\",\"team\": \"frontend\",\"editor\": "
                + "\"melchior\",\"updated\": \"2026-08-14T02:11:09Z\",\"summary\": \"corrected the refresh "
                + "owner\",\"links\": [\"service map\"],\"body\": \"The gateway fronts every request, but "
                + "token refresh is the sidecar's: the gateway only forwards the 401 and the sidecar replays "
                + "with a fresh token. Timeout on the refresh path is 8s, set in the sidecar's own config, "
                + "not the gateway's.\"}, {\"title\": \"legacy queue\",\"tier\": \"team\",\"team\": "
                + "\"frontend\",\"editor\": \"gardener\",\"stale\": true,\"updated\": "
                + "\"2026-07-30T10:00:00Z\",\"summary\": \"replaced by kafka\",\"body\": \"no longer true: "
                + "jobs moved to kafka. The rabbitmq broker still answers on 5672 but nothing enqueues to it; "
                + "see the event bus page for the current path.\"}]"));
        return wiki;
    }

    private static JsArray<Object> mcp() {
        if (mcp == null) mcp = Js.uncheckedCast(Global.JSON.parse(
                "[ {\"name\": \"docs\",\"tier\": \"global\",\"url\": "
                + "\"http://localhost:3000/mcp\",\"file\": \"~/.config/magi/config.toml\"}, {\"name\": "
                + "\"figma\",\"tier\": \"project\",\"companion\": \"design\",\"socket\": "
                + "\"/demo/design.sock\",\"command\": \"npx\",\"args\": [\"-y\", \"figma-mcp\"],\"envNames\": "
                + "[\"FIGMA_TOKEN\"],\"file\": \"/Users/you/work/design-system/.magi/config.toml\"}]"));
        return mcp;
    }
}
