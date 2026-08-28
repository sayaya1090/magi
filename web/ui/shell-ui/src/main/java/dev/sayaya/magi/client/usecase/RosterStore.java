package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.CompanionSharing;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.bridge.TranscriptSharing;
import dev.sayaya.magi.client.domain.CompanionType;

import dev.sayaya.rx.Observable;
import dev.sayaya.rx.subject.BehaviorSubject;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

import static dev.sayaya.rx.subject.BehaviorSubject.behavior;

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
    // 명단과 회선은 흐름이다. 브리지로 나가는 것도, 셸 제 판들이 보는 것도 같은 이 흐름
    // 하나다 — 한 API에 주인은 하나라는 규칙이 스트림에도 걸린다.
    private final BehaviorSubject<FleetAgent[]> roster = behavior(null);
    private final BehaviorSubject<Boolean> link = behavior(false);
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
        RosterSharing.host(cb -> roster.subscribe(list -> cb.call(list)), this::refresh);
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

    /** 셸 자신도 턴을 본다(턴바) — 브리지를 걸기 전에도 구독할 수 있게 바로 문을 낸다. */
    public void onTurn(TranscriptSharing.TurnFn cb) {
        turnObs.add(cb);
        cb.call(turnOpen, turnFor);
    }

    private String aimedSub = null;

    /** 자식 층위 — 지난 일과 같은 규칙이다: 스트림은 그대로, 컨텍스트만 갈아탄다. */
    public void sub(String idOrNull) {
        if (eq(aimedSub, idOrNull)) return;
        aimedSub = idOrNull;
        pushContext();
    }

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

    /** 명단 — 아직 못 읽었으면 null이 흐른다(그것도 사실이다: "모른다"). */
    public void subscribe(Consumer<FleetAgent[]> o) { roster.subscribe(o); }

    /** 회선이 서 있는가. */
    public void subscribeLink(Consumer<Boolean> o) { link.subscribe(o); }

    /**
     * 한 컴패니언만 — 명단 전체가 아니라 <b>그 행</b>의 흐름이다.
     *
     * 전체를 내려보내면 받는 판마다 "내 것이 바뀌었나"를 제 손으로 판별해야 하고, 그 판별을
     * 한 곳이라도 빠뜨리면 초당 한 번씩 애먼 판이 다시 선다. 그래서 큰 스토어가 조각을 잘라
     * 내려보내고, 바뀌었는지는 그 조각 위에서 본다(같은 행이면 흐르지 않는다).
     */
    public Observable<FleetAgent> of(String socket, String peer) {
        String want = peer == null ? "" : peer;
        return roster.map(list -> rowOf(list, socket, want))
                .distinctUntilChanged((java.util.function.BiFunction<FleetAgent, FleetAgent, Boolean>)
                        RosterStore::same);
    }

    private static FleetAgent rowOf(FleetAgent[] list, String socket, String peer) {
        if (list == null || socket == null) return null;
        for (FleetAgent a : list) {
            if (socket.equals(a.socket) && peer.equals(a.peer == null ? "" : a.peer)) return a;
        }
        return null;
    }

    /** 두 행이 같은 소식인가 — 도는 숫자(쉰 시간)는 빼고 본다: 매 초 달라지는 값을 넣으면
     *  "바뀌었다"가 매 초 참이 되어 거르는 뜻이 없어진다. */
    private static boolean same(FleetAgent a, FleetAgent b) {
        if (a == null || b == null) return a == b;
        return sig(a).equals(sig(b));
    }

    private static String sig(FleetAgent a) {
        return a.state + "|" + a.steps + "|" + a.role + "|" + a.team + "|" + a.hub + "|" + a.host
                + "|" + a.instance + "|" + a.addr + "|" + a.pid + "|" + a.version + "|" + a.workdir
                + "|" + a.session + "|" + a.permission + "|" + a.model + "|" + a.backend
                + "|" + a.handling + "|" + a.waiting + "|" + a.live + "|" + a.name + "|" + a.doing;
    }

    @Override
    public void roster(FleetAgent[] listOrNull) {
        // 못 읽은 프레임(null)은 흘리지 않는다. 흐름은 마지막 값을 기억했다가 늦게 온
        // 구독자에게 재생하는데, 그 기억이 null이면 나중에 붙은 화면이 "명단을 모른다"는
        // 상태로 서고 다음 프레임까지 빈 판이 된다(실측: 명단을 한 번만 내놓는 데모에서
        // 사실판이 영영 비었다). 읽기가 실패했다는 소식은 회선(link)이 따로 나른다.
        if (listOrNull == null) return;
        current = listOrNull;
        roster.next(listOrNull);
        // 조준된 행의 타입 선언이 이제야 도착했을 수 있다 — 컨텍스트가 따라간다.
        if (aimedSocket != null && listOrNull != null) {
            String want = typeOf(aimedSocket).id;
            String dir = workdirOf(aimedSocket);
            if (ctx == null || !want.equals(ctx.type) || !dir.equals(ctx.workdir)) pushContext();
        }
    }

    @Override
    public void link(boolean now) {
        up = now;
        link.next(now);
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
                        typeOf(aimedSocket).module, typeOf(aimedSocket).styles, workdirOf(aimedSocket),
                        aimedSub);
        for (CompanionSharing.NextFn o : ctxObs) o.call(ctx);
    }

    /** 그 컴패니언의 작업공간 — 명단이 아는 사실이고, 명단의 주인은 셸이다. */
    private String workdirOf(String socket) {
        if (current == null || socket == null) return "";
        for (FleetAgent a : current) if (socket.equals(a.socket)) return a.workdir == null ? "" : a.workdir;
        return "";
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }
}
