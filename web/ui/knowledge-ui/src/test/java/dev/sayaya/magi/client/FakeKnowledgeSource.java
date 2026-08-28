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
    public void skills(Consumer<Object> cb) { cb.accept(unreachable() ? null : skills); }

    /**
     * 회선이 끊긴 판 — 목록 읽기가 <b>null</b>로 온다({@code Console.fetchList}가 거부·불통·
     * 깨진 본문을 전부 null로 접으므로).
     *
     * <p>이것이 거절과 <b>겹치는</b> 판을 만들 수 있어야 한다: 쓰기가 못 닿아 우리가 지어낸
     * 말이 설 때는, 뒤따르는 읽기도 못 닿는다. 그 겹침이 페이크에 없어서 스펙이 「사유가 목록
     * 실패 갈래에서 버려진다」를 못 봤다.</p>
     */
    private static boolean unreachable() {
        Object v = Js.asPropertyMap(DomGlobal.window).get("__magi_test_unreachable");
        return v != null && !"false".equals(String.valueOf(v));
    }

    @Override
    public void wiki(Consumer<Object> cb) { cb.accept(wiki); }

    @Override
    public void mcp(Consumer<Object> cb) { cb.accept(unreachable() ? null : mcp); }

    @Override
    public void forget(String name, String tier, String team, String socket, String peer, Consumer<String> why) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_forgot", name + "@" + tier);
        String no = refuses();
        if (!no.isEmpty()) { why.accept(no); return; }
        skills = skills.filter((v, i) -> !name.equals(Js.asPropertyMap(v).get("name")));
        why.accept("");
    }

    @Override
    public void remember(String text, String tier, String team, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_remembered",
                text + "@" + tier + (team == null ? "" : ":" + team));
        why.accept(refuses());
    }

    @Override
    public void saveServer(String socket, JsPropertyMap<String> fields, java.util.function.Consumer<String> why) {
        // 거절도 답이다 — 스펙이 그 문장을 창에 적어 두면 그대로 돌려준다(운영 서버가 403·400·500
        // 본문으로 주는 그 자리). 없으면 받아들인다.
        Object refuse = Js.asPropertyMap(DomGlobal.window).get("__magi_test_refuse");
        if (refuse != null && !String.valueOf(refuse).isEmpty()) { why.accept(String.valueOf(refuse)); return; }
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_saved",
                fields.get("name") + "@" + (socket == null ? "global" : socket));
        why.accept("");
    }

    @Override
    public void console(java.util.function.Consumer<String> embedModel) { embedModel.accept(""); }

    @Override
    public void removeServer(String name, String socket, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_removed",
                name + "@" + (socket == null ? "global" : socket));
        String no = refuses();
        if (!no.isEmpty()) { why.accept(no); return; }
        mcp = mcp.filter((v, i) -> !name.equals(Js.asPropertyMap(v).get("name")));
        why.accept("");
    }

    /**
     * 스펙이 창에 적어 두면 그 다음 쓰기가 거절당한다 — 다이얼로그 저장이 이미 쓰던
     * {@code __magi_test_refuse}와 <b>다른</b> 칸이다: 그쪽은 폼 안에 사유가 서고 이쪽은
     * 판 위에 서서, 한 칸을 나눠 쓰면 어느 자리를 재고 있는지 스펙이 말하지 못한다.
     */
    private static String refuses() {
        Object v = Js.asPropertyMap(DomGlobal.window).get("__magi_test_press_refuses");
        return v == null ? "" : String.valueOf(v);
    }
}
