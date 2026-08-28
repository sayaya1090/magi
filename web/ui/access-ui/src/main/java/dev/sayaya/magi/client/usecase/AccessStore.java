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

    /**
     * 명부를 다시 읽는다 — <b>늦게 온 옛 명부는 버린다.</b>
     *
     * <p>읽기가 둘 이상 떠 있을 수 있다(쓰기마다 하나씩 부르고, 사람은 답을 기다리지 않고 다음
     * 컨트롤을 누른다). 응답이 청한 순서로 온다는 보장이 없으므로, 나중에 청한 것이 이미
     * 칠해진 뒤에 먼저 청한 것이 도착하면 그 옛 명부가 판을 덮는다 — 지운 사람이 다시 명단에
     * 서 있는 그림이다. 그래서 청할 때 번호를 쥐고, 그보다 새 것이 이미 칠해졌으면 버린다.</p>
     */
    public void reload() {
        int mine = ++asked;
        source.roster(g -> {
            if (mine < shown) return;
            shown = mine;
            got = g;
            answered = true;
            told();
        });
    }

    private int asked = 0;
    private int shown = 0;

    public Object got() { return got; }
    public boolean answered() { return answered; }
    public String capFilter() { return capFilter; }
    public String refusal() { return refusal; }

    public void filter(String cap) {
        capFilter = cap != null && cap.equals(capFilter) ? null : cap;
        told();
    }

    public void setPerson(String who, String role, String companions) {
        int mine = ++pressed;
        source.setPerson(who, role, companions, why -> said(mine, why));
    }

    public void removePerson(String who) {
        int mine = ++pressed;
        source.removePerson(who, why -> said(mine, why));
    }

    private int pressed = 0;
    private int applied = 0;

    /**
     * 사유를 <b>먼저</b> 쥐고 나서 다시 읽는다 — 다시 읽기가 이 판을 칠하므로 순서가 곧 그림이다.
     *
     * <p>누를 때 미리 비우지 않는 것은 일부러다: 어느 쓰기든 답하면 여기를 지나므로, 됐을 때
     * 오는 빈 문자열이 앞의 사유를 덮는다. 누름과 답 사이의 눈 깜짝할 창을 위해 같은 일을 두
     * 곳에 적어 두면, 둘 중 하나만 고치는 날이 온다.</p>
     *
     * <p>다만 <b>덮는 것은 새 대답만</b>이다. 사람은 답을 기다리지 않고 다음 컨트롤을 누르므로
     * 쓰기가 둘 떠 있을 수 있고, 답이 청한 순서로 온다는 보장은 없다. 나중에 누른 것이 통해
     * 사유가 걷힌 뒤에 먼저 누른 것의 거절이 도착하면, 이미 지나간 누름의 말이 판에 선다 —
     * 사람 눈에는 방금 통한 그 누름이 거절당한 것으로 읽힌다. 그래서 누를 때 번호를 쥐고,
     * 그보다 새 대답이 이미 반영됐으면 <b>말만</b> 버린다.</p>
     *
     * <p>말만 버리고 명부는 다시 읽는다: 그 쓰기가 서버에서 통했다면 그 결과가 아직 이 판에
     * 없을 수 있고, 그것은 어느 누름이 먼저였는지와 상관없는 사실이다.</p>
     */
    private void said(int mine, String why) {
        if (mine > applied) {
            applied = mine;
            refusal = why == null ? "" : why;
        }
        reload();
    }


}
