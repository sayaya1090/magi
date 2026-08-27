package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.CompanionSharing;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.bridge.TranscriptSharing;
import dev.sayaya.magi.client.domain.CompanionType;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 셸의 스트림 저장소 — /events의 유일한 소유자이자 창 브리지들의 호스트.
 *
 * 명단은 늘 흐르고(마스트헤드·레일 배지·플릿 화면), 컴패니언에 조준(aim)되면 같은
 * 회선의 기본 프레임이 전사가 되어 TranscriptSharing으로, 지금 보는 컴패니언은
 * CompanionSharing으로 흐른다. 타입은 여기서 해석한다: 명단 행의 선언(없으면 1)을
 * 카탈로그에 물어, 화면 모듈이 받는 컨텍스트에는 해석된 키가 실린다.
 */
@Singleton
public class RosterStore implements RosterSource.Listener {
    private final RosterSource source;
    private final List<Consumer<FleetAgent[]>> rosterObs = new ArrayList<>();
    private final List<Consumer<Boolean>> linkObs = new ArrayList<>();
    private final List<CompanionSharing.NextFn> ctxObs = new ArrayList<>();
    private final List<TranscriptSharing.RowsFn> rowsObs = new ArrayList<>();
    private final List<TranscriptSharing.TurnFn> turnObs = new ArrayList<>();
    private FleetAgent[] current = null;
    private boolean up = false;
    private boolean started = false;
    private String aimedSocket = null;
    private String aimedPeer = null;
    private String aimedPast = null;
    private CompanionContext ctx = null;
    private Object lastRows = null;
    private boolean turnOpen = false;
    private double turnFor = 0;

    @Inject
    public RosterStore(RosterSource source) { this.source = source; }

    public void start() {
        if (started) return;
        started = true;
        // 화면 모듈이 로드되기 전에 문을 걸어 둔다 — 구독은 현재값을 재생한다.
        RosterSharing.host(cb -> {
            rosterObs.add(list -> cb.call(list));
            if (current != null) cb.call(current);
        }, this::refresh);
        CompanionSharing.host(cb -> {
            ctxObs.add(cb);
            cb.call(ctx);
        });
        TranscriptSharing.host(
                cb -> { rowsObs.add(cb); cb.call(lastRows); },
                cb -> { turnObs.add(cb); cb.call(turnOpen, turnFor); });
        source.start(this);
        source.refresh();
    }

    /** 창 전체의 두 사실 — 스토어는 나르기만 한다(무엇인지는 브리지가 안다). */
    public void facts(java.util.function.Consumer<Object> consoleInfo,
                      java.util.function.Consumer<Object> caps) {
        source.facts(consoleInfo, caps);
    }

    public void refresh() { source.refresh(); }

    /** 지난 일 층위 — 스트림은 그대로 두고 컨텍스트만 갈아탄다(과거는 fetch의 것이다). */
    public void past(String pastOrNull) {
        if (eq(aimedPast, pastOrNull)) return;
        aimedPast = pastOrNull;
        pushContext();
    }

    /** 어느 컴패니언을 보는가 — null이면 카탈로그 화면. 스트림 조준과 컨텍스트가 함께 돈다. */
    public void aim(String socket, String peer) {
        if (eq(aimedSocket, socket) && eq(aimedPeer, peer)) return;
        aimedSocket = socket;
        aimedPeer = peer;
        // 새 컴패니언의 전사는 아직 모른다 — "이전 것"이 새 화면에 비치면 안 된다.
        lastRows = null;
        for (TranscriptSharing.RowsFn o : rowsObs) o.call(null);
        turnOpen = false;
        turnFor = 0;
        for (TranscriptSharing.TurnFn o : turnObs) o.call(false, 0);
        pushContext();
        source.aim(socket, peer);
    }

    /** 명단이 대는 타입 선언을 카탈로그로 푼다 — 행이 아직 없으면 기본(코딩 에이전트). */
    public CompanionType typeOf(String socket) {
        if (current != null && socket != null) {
            for (FleetAgent a : current) {
                if (socket.equals(a.socket)) return CompanionType.byId(a.type);
            }
        }
        return CompanionType.byId(null);
    }

    public void subscribe(Consumer<FleetAgent[]> o) {
        rosterObs.add(o);
        if (current != null) o.accept(current);
    }

    public void subscribeLink(Consumer<Boolean> o) {
        linkObs.add(o);
        o.accept(up);
    }

    @Override
    public void roster(FleetAgent[] listOrNull) {
        if (listOrNull != null) current = listOrNull;
        for (Consumer<FleetAgent[]> o : rosterObs) o.accept(listOrNull);
        // 조준된 행의 타입 선언이 이제야 도착했을 수 있다 — 컨텍스트가 따라간다.
        if (aimedSocket != null && listOrNull != null) {
            String want = typeOf(aimedSocket).id;
            if (ctx == null || !want.equals(ctx.type)) pushContext();
        }
    }

    @Override
    public void link(boolean now) {
        up = now;
        for (Consumer<Boolean> o : linkObs) o.accept(now);
    }

    @Override
    public void transcript(Object rowsOrNull) {
        lastRows = rowsOrNull;
        for (TranscriptSharing.RowsFn o : rowsObs) o.call(rowsOrNull);
    }

    @Override
    public void turn(boolean open, double forSec) {
        turnOpen = open;
        turnFor = forSec;
        for (TranscriptSharing.TurnFn o : turnObs) o.call(open, forSec);
    }

    private void pushContext() {
        ctx = aimedSocket == null ? null
                : CompanionContext.of(aimedSocket, aimedPeer, typeOf(aimedSocket).id, aimedPast,
                        typeOf(aimedSocket).module, typeOf(aimedSocket).styles);
        for (CompanionSharing.NextFn o : ctxObs) o.call(ctx);
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }
}
