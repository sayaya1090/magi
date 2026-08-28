package dev.sayaya.magi.client.usecase;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

/**
 * 화면의 저장소 — 세 목록(경험·위키·서버)과 세 검색어를 들고, 뷰는 여기서만 읽는다.
 * null은 "아직/못 읽음"이고 화면이 말한다. 행동(잊기·적기·제거)은 소스로 나가고,
 * 답이 오면 그 목록만 다시 읽는다.
 */
@Singleton
public class KnowledgeStore extends dev.sayaya.magi.bridge.Told {
    private final KnowledgeSource source;
    private Object skills = null;
    private Object wiki = null;
    private Object mcp = null;
    private boolean skillsAnswered = false;
    private boolean wikiAnswered = false;
    private boolean mcpAnswered = false;
    private String skillQuery = "";
    private String wikiQuery = "";
    private String mcpQuery = "";
    private String skillsRefusal = "";
    private String mcpRefusal = "";
    private boolean started = false;

    @Inject
    public KnowledgeStore(KnowledgeSource source) { this.source = source; }

    public void start() {
        if (started) return;
        started = true;
        reloadSkills();
        reloadWiki();
        reloadMcp();
    }

    public void reloadSkills() { source.skills(list -> { skills = list; skillsAnswered = true; told(); }); }

    public void reloadWiki() { source.wiki(list -> { wiki = list; wikiAnswered = true; told(); }); }

    public void reloadMcp() { source.mcp(list -> { mcp = list; mcpAnswered = true; told(); }); }

    public Object skills() { return skills; }
    public Object wiki() { return wiki; }
    public Object mcp() { return mcp; }
    public boolean skillsAnswered() { return skillsAnswered; }
    public boolean wikiAnswered() { return wikiAnswered; }
    public boolean mcpAnswered() { return mcpAnswered; }

    public String skillQuery() { return skillQuery; }
    public String wikiQuery() { return wikiQuery; }
    public String mcpQuery() { return mcpQuery; }
    public String skillsRefusal() { return skillsRefusal; }
    public String mcpRefusal() { return mcpRefusal; }
    public void skillQuery(String q) { skillQuery = q == null ? "" : q; told(); }
    public void wikiQuery(String q) { wikiQuery = q == null ? "" : q; told(); }
    public void mcpQuery(String q) { mcpQuery = q == null ? "" : q; told(); }

    public void forget(String name, String tier, String team, String socket, String peer) {
        source.forget(name, tier, team, socket, peer, this::skillsSaid);
    }

    public void remember(String text, String tier, String team) {
        source.remember(text, tier, team, this::skillsSaid);
    }

    public void removeServer(String name, String socket) {
        source.removeServer(name, socket, this::mcpSaid);
    }

    /**
     * 거절당한 사유는 <b>그 판의 것</b>이다 — 이 화면은 판이 셋이라, 하나로 모으면 서버를
     * 지우려다 들은 말이 규칙 목록 위에 서게 된다.
     *
     * <p>사유를 먼저 쥐고 나서 다시 읽는다: 다시 읽기가 판을 칠하므로 순서가 곧 그림이다.</p>
     */
    private void skillsSaid(String why) {
        skillsRefusal = why == null ? "" : why;
        reloadSkills();
    }

    private void mcpSaid(String why) {
        mcpRefusal = why == null ? "" : why;
        reloadMcp();
    }

    /** 저장이 거부되면 사유가 그대로 돌아온다 — 성공("")일 때만 목록을 다시 읽는다. */
    public void saveServer(String socket, jsinterop.base.JsPropertyMap<String> fields,
                           java.util.function.Consumer<String> why) {
        source.saveServer(socket, fields, w -> {
            // 이쪽 사유는 판이 아니라 <b>다이얼로그</b>가 세운다(사람이 그 폼을 아직 보고
            // 있으므로) — 그래서 mcpRefusal을 건드리지 않는다.
            if (w == null || w.isEmpty()) reloadMcp();
            why.accept(w == null ? "" : w);
        });
    }

    private String embedModel = null;

    /** 임베딩 모델 — 한 번 물어 두고(설정 파일은 보는 동안 안 바뀐다) 구독에 실어 준다. */
    public String embedModel() { return embedModel; }

    public void askConsole() {
        if (embedModel != null) return;
        source.console(m -> { embedModel = m == null ? "" : m; told(); });
    }


}
