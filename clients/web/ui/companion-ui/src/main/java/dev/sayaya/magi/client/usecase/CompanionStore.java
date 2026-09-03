package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.rx.Observable;
import dev.sayaya.rx.subject.BehaviorSubject;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.BiConsumer;
import java.util.function.Consumer;

import static dev.sayaya.rx.subject.BehaviorSubject.behavior;

/**
 * 화면의 저장소 — 컨텍스트·전사·턴을 들고, 뷰는 여기서만 읽는다.
 * 구독은 현재값을 재생한다: 뷰가 소스보다 늦게 서도 첫 그림을 놓치지 않는다.
 */
@Singleton
public class CompanionStore implements CompanionSource.Listener {
    private final CompanionSource source;
    private final BehaviorSubject<CompanionContext> ctxOf = behavior(null);
    private final BehaviorSubject<Object> rowsOf = behavior(null);
    private final BehaviorSubject<dev.sayaya.magi.bridge.Turn> turnOf =
            dev.sayaya.rx.subject.BehaviorSubject.behavior(dev.sayaya.magi.bridge.Turn.NONE);
    private CompanionContext ctx = null;
    private boolean turnOpen = false;
    private double turnFor = 0;
    private boolean started = false;

    @Inject
    public CompanionStore(CompanionSource source) { this.source = source; }

    public void start() {
        if (started) return;
        started = true;
        source.start(this);
        startFacts();
    }

    public void onContext(Consumer<CompanionContext> o) { ctxOf.subscribe(o); }

    public void onRows(Consumer<Object> o) { rowsOf.subscribe(o); }

    public void onTurn(BiConsumer<Boolean, Double> o) {
        turnOf.distinctUntilChanged().subscribe(t -> o.accept(t.open, t.forSec));
    }

    public CompanionContext context() { return ctx; }

    public void submit(String text, Consumer<String> why) {
        if (ctx == null) { why.accept("no companion"); return; }
        source.submit(ctx, text, why);
    }

    // ── 지난 일 층위: 목록 또는 한 세션의 전사 — ctx.past가 정한다 ──────────
    private final BehaviorSubject<Object> pastOf = behavior(null);
    private Object pastData = null;      // 목록(past=="")이거나 전사 행들(past=id)
    private String pastFor = null;

    /** 구독자는 (ctx.past, 자료)를 함께 읽는다 — 자료 null은 "아직/못 읽음". */
    public void onPast(Consumer<Object> o) { pastOf.subscribe(o); }

    private void askPast() {
        if (ctx == null || ctx.past == null) { pastData = null; pastFor = null; emitPast(); return; }
        final String want = ctx.socket + "\u0000" + ctx.past;
        if (want.equals(pastFor)) return;
        pastFor = want;
        pastData = null;
        emitPast();
        java.util.function.Consumer<Object> land = d -> {
            if (!want.equals(pastFor)) return;   // 늦은 답이 새 층위에 앉지 않게
            pastData = d;
            emitPast();
        };
        if (ctx.past.isEmpty()) source.history(ctx, land);
        else source.pastTranscript(ctx, ctx.past, land);
    }

    private void emitPast() { pastOf.next(pastData); }

    // ── 사실판이 읽는 것들: 명단과 컨텍스트 창 ──────────────────────────────
    private final BehaviorSubject<Object> rosterOf = behavior(null);
    private final BehaviorSubject<Object> ctxInfoOf = behavior(null);
    private Object ctxInfo = null;
    private String ctxFor = null;

    public void onRoster(Consumer<Object> o) { rosterOf.subscribe(o); }

    public void onContextInfo(Consumer<Object> o) { ctxInfoOf.subscribe(o); }

    /**
     * 지금 보는 컴패니언의 <b>행</b>만 — 명단 전체가 아니라 그 조각이고, 그 행이 같은 말을
     * 다시 하면 흐르지 않는다. 사실판이 초당 여러 번 다시 서던 자리가 여기였다: 큰 스토어가
     * 전부를 내려보내고, 받는 판이 제 손으로 "내 것이 바뀌었나"를 따지고 있었다.
     */
    public Observable<FleetAgent> aimed() {
        return rosterOf.map(list -> {
                    FleetAgent row = rowOf(list);
                    refetchWhenATurnEnds(row);
                    return row;
                })
                .distinctUntilChanged((java.util.function.BiFunction<FleetAgent, FleetAgent, Boolean>)
                        CompanionStore::same);
    }

    /**
     * 이 컴패니언이 아직 답하는가 — 명단에 없거나, 있어도 live가 거짓이면 멈춘 것이다.
     * 명단을 아직 못 읽었으면 산 것으로 둔다("모른다"를 "죽었다"로 읽지 않는다).
     */
    public Observable<Boolean> alive() {
        return rosterOf.map(list -> list == null || ctx == null || ctx.socket == null
                        || ctx.socket.isEmpty()
                        || dev.sayaya.magi.bridge.AgentStates.answering(rowOf(list)))
                .distinctUntilChanged();
    }

    /**
     * 어디에 있나 — 그 행을 찾는 것뿐이고, <b>답하는지는 묻지 않는다</b>.
     *
     * 한때 여기서 둘을 한꺼번에 했다: 답하지 않는 행을 없는 행으로 돌려주었다. 그래서 멈춘
     * 컴패니언의 사실판이 통째로 숨었다 — 무엇을 하던 컴패니언이었는지, 어느 작업공간에
     * 있었는지가 멈추는 순간 사라졌고, 그것은 바로 그때 읽고 싶은 것들이다. 운영은 그 행으로
     * 판을 그리고 연결 줄에만 멈췄다고 적는다. 두 질문이니 답도 둘이다({@link #alive()}).
     */
    private FleetAgent rowOf(Object list) {
        if (list == null || ctx == null || ctx.socket == null) return null;
        jsinterop.base.JsArrayLike<Object> rows = jsinterop.base.Js.uncheckedCast(list);
        String peer = ctx.peer == null ? "" : ctx.peer;
        for (int i = 0; i < rows.getLength(); i++) {
            FleetAgent r = jsinterop.base.Js.uncheckedCast(rows.getAt(i));
            String had = r.peer == null ? "" : r.peer;
            if (ctx.socket.equals(r.socket) && peer.equals(had)) return r;
        }
        return null;
    }

    /** 같은 소식인가 — 도는 숫자(쉰 시간)는 빼고 본다: 매 초 달라지는 값이 섞이면 "바뀌었다"가
     *  늘 참이 되어 거른다는 말에 뜻이 없어진다. */
    private static boolean same(FleetAgent a, FleetAgent b) {
        if (a == null || b == null) return a == b;
        return sig(a).equals(sig(b));
    }

    private static String sig(FleetAgent a) {
        return a.state + "|" + a.steps + "|" + a.role + "|" + a.team + "|" + a.hub + "|" + a.host
                + "|" + a.instance + "|" + a.addr + "|" + a.pid + "|" + a.version + "|" + a.workdir
                + "|" + a.session + "|" + a.permission + "|" + a.model + "|" + a.backend
                + "|" + a.handling + "|" + a.waiting + "|" + a.live;
    }

    /** 지금 접기 — 끝나면 컨텍스트를 다시 읽는다(운영 규칙: 접기 전 숫자를 계속 보이지 않게). */
    // ── 사실판이 바꿀 수 있는 것들 ────────────────────────────────────────────
    // 그리는 것은 늘 <b>데몬이 말한 것</b>이다: 여기서는 청하고, 다음 명단 프레임이 답을 그린다.
    // 그래서 거부된 바꿈은 눈에 띄게 되돌아온다(운영이 이 세 컨트롤에 세운 규칙).

    // 컴패니언마다 하나씩 기억한다. 마지막 하나만 들고 있으면 A→B→A로 오갈 때마다 A의 목록을
    // 다시 물어야 했고, 명단이 흐를 때마다 상세가 다시 그려지므로 그 왕복이 화면 하나에서
    // 여러 번 났다. 목록은 그 데몬의 사실이고 자주 바뀌지 않는다.
    private final java.util.Map<String, Object> modelNamesBySocket = new java.util.HashMap<>();
    private final java.util.Set<String> modelsAsking = new java.util.HashSet<>();

    /** 이 컴패니언이 닿는 모델 이름들 — 화면당 한 번만 묻는다(컴패니언이 바뀌면 다시). */
    // ── 오른쪽 판이 읽는 나머지 ─────────────────────────────────────────────
    // 로그에 없는 사실들이라 데몬에게 묻는다. 명단이 흐를 때마다 다시 묻되(그 사이에 달라진다),
    // 답이 <b>같으면</b> 다시 그리지 않는 것은 판의 몫이다.

    public void jobs(Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        source.jobs(ctx, cb);
    }

    public void handoffs(Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        source.handoffs(ctx, cb);
    }

    public void cron(Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        source.cron(ctx, cb);
    }

    /** 이 컴패니언의 지난 일 목록 — 사실판의 세션 고르개가 읽는다(층위와 같은 답, 다른 쓰임). */
    public void history(Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        source.history(ctx, cb);
    }

    public void models(Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        final String who = ctx.socket;
        if (modelNamesBySocket.containsKey(who)) { cb.accept(modelNamesBySocket.get(who)); return; }
        // 같은 컴패니언에 대해 두 번 묻지 않는다: 답이 오기 전에 다시 그려지면 두 번째 요청이
        // 나가고, 그 답이 첫 답을 덮어쓰며 고르개가 다시 세워졌다.
        if (!modelsAsking.add(who)) { cb.accept(modelNamesBySocket.get(who)); return; }
        source.models(ctx, got -> {
            modelsAsking.remove(who);
            modelNamesBySocket.put(who, got);
            cb.accept(got);
        });
    }

    /** 이 컴패니언의 모델 목록을 잊는다 — 백엔드를 갈아탄 뒤 그 데몬이 답할 이름이 달라진다. */
    public void forgetModels() {
        if (ctx != null) modelNamesBySocket.remove(ctx.socket);
    }

    private Object providerList = null;

    /** 이 콘솔이 볼 수 있는 백엔드들 — 화면당 한 번 묻는다(콘솔의 사실이라 컴패니언과 무관). */
    public void providers(Consumer<Object> cb) {
        if (providerList != null) { cb.accept(providerList); return; }
        if (!providersAsking) {
            providersAsking = true;
            source.providers(got -> {
                providersAsking = false;
                providerList = got;
                for (Consumer<Object> w : providersWaiting) w.accept(got);
                providersWaiting.clear();
                cb.accept(got);
            });
            return;
        }
        // 이미 묻고 있으면 답을 같이 받는다. 화면이 여러 번 그려지는 동안 같은 목록을 여러 번
        // 물어 오던 자리다 — 콘솔 하나의 사실이라 한 번이면 된다.
        providersWaiting.add(cb);
    }

    private boolean providersAsking = false;
    private final java.util.List<Consumer<Object>> providersWaiting = new java.util.ArrayList<>();

    /** 이 컴패니언을 그 백엔드로 — 주소를 보낸다. */
    public void useProvider(String base, Consumer<String> why) {
        if (ctx != null) source.useProvider(ctx, base, why);
    }

    public void model(String name, Consumer<String> why) {
        if (ctx != null) source.model(ctx, name, why);
    }

    public void permission(String mode, Consumer<String> why) {
        if (ctx != null) source.permission(ctx, mode, why);
    }

    /** 그 데몬을 최신 릴리스로 — 들은 말을 그대로 돌려준다(빈 문자열은 "아무 말도 없음"). */
    public void update(Consumer<String> said) {
        if (ctx == null) { said.accept(""); return; }
        source.update(ctx, said);
    }

    public void tools(Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        source.tools(ctx, cb);
    }

    public void loop(Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        source.loop(ctx, cb);
    }

    public void reportFormat(Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        source.reportFormat(ctx, cb);
    }

    public void reportFormat(java.util.List<String> keys, java.util.List<String> prompts, Consumer<String> why) {
        if (ctx != null) source.reportFormat(ctx, keys, prompts, why);
    }

    public void compact(Runnable after) {
        if (ctx == null) return;
        source.compact(ctx, () -> { ctxFor = null; askContextInfo(); after.run(); });
    }

    private void startFacts() {
        source.roster(list -> { if (list != null) rosterOf.next(list); });
    }

    private final BehaviorSubject<Object> planOf = behavior(null);
    private Object planData = null;
    private String planFor = null;

    public void onPlan(Consumer<Object> o) { planOf.subscribe(o); }

    private void askPlan() {
        if (ctx == null) return;
        final String want = ctx.socket;
        if (want.equals(planFor)) return;
        planFor = want;
        source.plan(ctx, list -> {
            if (ctx == null || !want.equals(ctx.socket)) return;
            planData = list;
            planOf.next(list);
        });
    }

    /**
     * 턴이 끝나면 컨텍스트를 다시 묻는다.
     *
     * <p>이 판은 컴패니언을 <b>바꿀 때만</b> 물었다. 그런데 컨텍스트는 자라는 값이고, 이 화면에서
     * 사람이 보러 오는 것이 바로 그 값이다 — 실측: 서버가 49,589 토큰을 답하는 동안 화면은
     * 페이지를 열 때의 「~0 tokens」를 계속 보이고 있었고, 구성 띠는 아예 서지 않았다.
     *
     * <p>매 프레임마다 묻지는 않는다. 이 답은 세션 로그를 처음부터 다시 재생하는 값이고(그래서
     * 명단의 한 칸이 아니라 제 문이다), 사람이 이 수를 보고 판단하는 시점은 턴 <b>사이</b>다 —
     * 압축 단추가 「턴 사이에 접고 싶은 사람의 것」인 것과 같은 이유다.
     */
    private void refetchWhenATurnEnds(FleetAgent a) {
        boolean turning = a != null && ("working".equals(a.state) || "waiting".equals(a.state));
        if (wasTurning && !turning) {
            ctxFor = null;
            askContextInfo();
        }
        wasTurning = turning;
    }

    private boolean wasTurning = false;

    private void askContextInfo() {
        if (ctx == null) return;
        final String want = ctx.socket;
        if (want.equals(ctxFor)) return;
        ctxFor = want;
        source.context(ctx, info -> {
            if (ctx == null || !want.equals(ctx.socket)) return;   // 늦게 온 답이 새 화면에 앉지 않게
            ctxInfo = info;
            ctxInfoOf.next(info);
        });
    }

    @Override
    public void context(CompanionContext c) {
        ctx = c;
        ctxOf.next(c);
        // 조각은 명단과 <b>지금 보는 컴패니언</b> 둘에서 잘린다. 명단은 제 박자로 흐르므로,
        // 컴패니언이 바뀐 순간 다시 자르지 않으면 다음 명단 프레임까지 빈 판이 서 있는다
        // (실측: 명단을 한 번만 내놓는 데모에서 사실판이 영영 비어 있었다).
        if (rosterOf.getValue() != null) rosterOf.next(rosterOf.getValue());
        ctxInfo = null;
        ctxInfoOf.next(null);
        askContextInfo();
        askPlan();
        askPast();
    }

    @Override
    public void transcript(Object rowsOrNull) {
        rowsOf.next(rowsOrNull);
    }

    @Override
    public void turn(boolean open, double forSec) {
        turnOpen = open;
        turnFor = forSec;
        turnOf.next(new dev.sayaya.magi.bridge.Turn(open, forSec));
    }
}
