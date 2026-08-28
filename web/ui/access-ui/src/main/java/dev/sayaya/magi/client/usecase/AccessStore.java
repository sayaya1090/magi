package dev.sayaya.magi.client.usecase;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

/** 접근 화면의 저장소 — 명부와 능력 필터(화면의 보는 방식이지 명부의 사실이 아니다). */
@Singleton
public class AccessStore extends dev.sayaya.magi.bridge.Told {
    private final AccessSource source;
    private Object got = null;
    private boolean answered = false;
    private String capFilter = null;
    // 마지막으로 누른 것이 거절당한 사유. 다음에 누른 것이 답할 때 덮인다 — 그 전까지는
    // 명단이 여전히 청한 대로가 아니므로, 그 말이 서 있는 것이 맞다.
    private String refusal = "";
    private boolean started = false;

    @Inject
    public AccessStore(AccessSource source) { this.source = source; }

    public void start() {
        if (started) return;
        started = true;
        reload();
    }

    public void reload() { source.roster(g -> { got = g; answered = true; told(); }); }

    public Object got() { return got; }
    public boolean answered() { return answered; }
    public String capFilter() { return capFilter; }
    public String refusal() { return refusal; }

    public void filter(String cap) {
        capFilter = cap != null && cap.equals(capFilter) ? null : cap;
        told();
    }

    public void setPerson(String who, String role, String companions) {
        source.setPerson(who, role, companions, this::said);
    }

    public void removePerson(String who) { source.removePerson(who, this::said); }

    /**
     * 사유를 <b>먼저</b> 쥐고 나서 다시 읽는다 — 다시 읽기가 이 판을 칠하므로 순서가 곧 그림이다.
     *
     * <p>누를 때 미리 비우지 않는 것은 일부러다: 어느 쓰기든 답하면 여기를 지나므로, 됐을 때
     * 오는 빈 문자열이 앞의 사유를 덮는다. 누름과 답 사이의 눈 깜짝할 창을 위해 같은 일을 두
     * 곳에 적어 두면, 둘 중 하나만 고치는 날이 온다.</p>
     */
    private void said(String why) {
        refusal = why == null ? "" : why;
        reload();
    }


}
