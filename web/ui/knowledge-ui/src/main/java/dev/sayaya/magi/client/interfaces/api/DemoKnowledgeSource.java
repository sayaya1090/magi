package dev.sayaya.magi.client.interfaces.api;

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
/**
 * 데몬 없이 이 화면이 답하는 것 — 이 모듈이 <b>제 목을 싣는다</b>.
 *
 * 목이 모듈 안에 있는 이유는 배포가 모듈 단위이기 때문이다: 화면은 저마다 컴파일돼 저마다의
 * 주기로 나가고 제 창에서 제 회선으로 말한다. 페이지가 남의 창에 목을 밀어 넣는 방식은 그
 * 구조를 거스르고, 창 하나만 갈아끼우면 iframe 안의 모듈에는 닿지도 않는다(실측).
 */
@Singleton
public class DemoKnowledgeSource implements KnowledgeSource {
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
    public DemoKnowledgeSource() {}

    @Override
    public void skills(Consumer<Object> cb) { cb.accept(skills); }

    @Override
    public void wiki(Consumer<Object> cb) { cb.accept(wiki); }

    @Override
    public void mcp(Consumer<Object> cb) { cb.accept(mcp); }

    @Override
    public void forget(String name, String tier, String team, String socket, String peer, Runnable done) {
        skills = skills.filter((v, i) -> !name.equals(Js.asPropertyMap(v).get("name")));
        done.run();
    }

    @Override
    public void remember(String text, String tier, String team, Runnable done) {
        done.run();
    }

    @Override
    public void saveServer(String socket, JsPropertyMap<String> fields, java.util.function.Consumer<String> why) {
        why.accept("");
    }

    @Override
    public void console(java.util.function.Consumer<String> embedModel) { embedModel.accept(""); }

    @Override
    public void removeServer(String name, String socket, Runnable done) {
        mcp = mcp.filter((v, i) -> !name.equals(Js.asPropertyMap(v).get("name")));
        done.run();
    }
}
