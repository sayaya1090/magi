package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.KnowledgeSource;
import elemental2.core.Global;
import elemental2.core.JsArray;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 고정 목록 셋 — 규칙 둘·기억 하나(랭킹이 갈리게), 위키 둘(하나는 낡음), 서버 둘.
 * 행동은 window.__magi_test_* 에 적고, 잊기는 목록에서 실제로 빼서 재조회가 보이게 한다.
 */
@Singleton
public class FakeKnowledgeSource implements KnowledgeSource {
    private JsArray<Object> skills = Js.uncheckedCast(Global.JSON.parse(
            "[{\"name\":\"rule-cache\",\"description\":\"reuse the prompt cache window\",\"tier\":\"global\"," +
              "\"kind\":\"skill\",\"observed\":4,\"firstSeen\":\"2026-08-01\",\"lastSeen\":\"2026-08-25\"," +
              "\"body\":\"Keep the shared prefix byte-identical.\\n(source: retry postmortem)\"}," +
             "{\"name\":\"rule-logs\",\"description\":\"read logs before guessing\",\"tier\":\"team\",\"team\":\"core\"," +
              "\"kind\":\"skill\",\"body\":\"\"}," +
             "{\"name\":\"mem-staging\",\"description\":\"staging db restores every Monday\",\"tier\":\"global\"," +
              "\"kind\":\"memory\",\"body\":\"\"}]"));
    private final JsArray<Object> wiki = Js.uncheckedCast(Global.JSON.parse(
            "[{\"title\":\"release trains\",\"tier\":\"global\",\"updated\":\"2026-08-20T09:00:00Z\"," +
              "\"editor\":\"docs\",\"summary\":\"web-v* ships the console alone\",\"body\":\"core on v*, console on web-v*.\"}," +
             "{\"title\":\"old runbook\",\"tier\":\"global\",\"stale\":true,\"body\":\"superseded\"}]"));
    private JsArray<Object> mcp = Js.uncheckedCast(Global.JSON.parse(
            "[{\"name\":\"github\",\"tier\":\"global\",\"url\":\"https://api.example.com/mcp/\",\"file\":\"~/.config/magi/config.toml\"}," +
             "{\"name\":\"repo-grep\",\"tier\":\"project\",\"companion\":\"build\",\"socket\":\"/tmp/a1.sock\"," +
              "\"command\":\"rg-mcp\",\"args\":[\"--root\",\".\"],\"envNames\":[\"RG_TOKEN\"],\"file\":\".magi/config.toml\"}]"));

    @Inject
    public FakeKnowledgeSource() {}

    @Override
    public void skills(Consumer<Object> cb) { cb.accept(skills); }

    @Override
    public void wiki(Consumer<Object> cb) { cb.accept(wiki); }

    @Override
    public void mcp(Consumer<Object> cb) { cb.accept(mcp); }

    @Override
    public void forget(String name, String tier, String team, String socket, String peer, Runnable done) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_forgot", name + "@" + tier);
        skills = skills.filter((v, i) -> !name.equals(Js.asPropertyMap(v).get("name")));
        done.run();
    }

    @Override
    public void remember(String text, String tier, String team, Runnable done) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_remembered",
                text + "@" + tier + (team == null ? "" : ":" + team));
        done.run();
    }

    @Override
    public void saveServer(String socket, JsPropertyMap<String> fields, java.util.function.Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_saved",
                fields.get("name") + "@" + (socket == null ? "global" : socket));
        why.accept("");
    }

    @Override
    public void console(java.util.function.Consumer<String> embedModel) { embedModel.accept(""); }

    @Override
    public void removeServer(String name, String socket, Runnable done) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_removed",
                name + "@" + (socket == null ? "global" : socket));
        mcp = mcp.filter((v, i) -> !name.equals(Js.asPropertyMap(v).get("name")));
        done.run();
    }
}
