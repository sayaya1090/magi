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
                "[{\"name\":\"rule-cache\",\"description\":\"reuse the prompt cache window\",\"tier\":\"global\"," +
                  "\"kind\":\"skill\",\"observed\":4,\"firstSeen\":\"2026-08-01\",\"lastSeen\":\"2026-08-25\"," +
                  "\"body\":\"Keep the shared prefix byte-identical.\\n(source: retry postmortem)\"}," +
                 "{\"name\":\"rule-logs\",\"description\":\"read logs before guessing\",\"tier\":\"team\",\"team\":\"core\"," +
                  "\"kind\":\"skill\",\"body\":\"\"}," +
                 "{\"name\":\"mem-staging\",\"description\":\"staging db restores every Monday\",\"tier\":\"global\"," +
                  "\"kind\":\"memory\",\"body\":\"\"}]"));
        return skills;
    }

    private static JsArray<Object> wiki() {
        if (wiki == null) wiki = Js.uncheckedCast(Global.JSON.parse(
                "[{\"title\":\"release trains\",\"tier\":\"global\",\"updated\":\"2026-08-20T09:00:00Z\"," +
                  "\"editor\":\"docs\",\"summary\":\"web-v* ships the console alone\"," +
                  "\"body\":\"core on v*, console on web-v*.\"}," +
                 "{\"title\":\"old runbook\",\"tier\":\"global\",\"stale\":true,\"body\":\"superseded\"}]"));
        return wiki;
    }

    private static JsArray<Object> mcp() {
        if (mcp == null) mcp = Js.uncheckedCast(Global.JSON.parse(
                "[{\"name\":\"github\",\"tier\":\"global\",\"url\":\"https://api.example.com/mcp/\"," +
                  "\"file\":\"~/.config/magi/config.toml\"}," +
                 "{\"name\":\"repo-grep\",\"tier\":\"project\",\"companion\":\"build\",\"socket\":\"/tmp/a1.sock\"," +
                  "\"command\":\"rg-mcp\",\"args\":[\"--root\",\".\"],\"envNames\":[\"RG_TOKEN\"]," +
                  "\"file\":\".magi/config.toml\"}]"));
        return mcp;
    }
}
