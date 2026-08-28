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
        return rosterOf.map(list -> rowOf(list))
                .distinctUntilChanged((java.util.function.BiFunction<FleetAgent, FleetAgent, Boolean>)
                        CompanionStore::same);
    }

    private FleetAgent rowOf(Object list) {
        if (list == null || ctx == null || ctx.socket == null) return null;
        jsinterop.base.JsArrayLike<Object> rows = jsinterop.base.Js.uncheckedCast(list);
        String peer = ctx.peer == null ? "" : ctx.peer;
        for (int i = 0; i < rows.getLength(); i++) {
            FleetAgent r = jsinterop.base.Js.uncheckedCast(rows.getAt(i));
            String had = r.peer == null ? "" : r.peer;
            // 명단에 남아 있어도 답하지 않으면(live 거짓) 없는 것과 같다(운영 companionAlive).
            // 다만 <b>말하지 않은 것은 산 것</b>이다: 이 값을 안 싣는 자리가 있어서(오래된
            // 데몬·테스트 목) 없음을 죽음으로 읽으면 멀쩡한 판이 통째로 사라진다.
            if (ctx.socket.equals(r.socket) && peer.equals(had)) return answering(r) ? r : null;
        }
        return null;
    }

    private static boolean answering(FleetAgent r) {
        return !jsinterop.base.Js.asPropertyMap(r).has("live") || r.live;
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

    private Object modelNames = null;
    private String modelsFor = null;

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
        if (modelNames != null && ctx.socket.equals(modelsFor)) { cb.accept(modelNames); return; }
        modelsFor = ctx.socket;
        source.models(ctx, got -> { modelNames = got; cb.accept(got); });
    }

    private Object providerList = null;

    /** 이 콘솔이 볼 수 있는 백엔드들 — 화면당 한 번 묻는다(콘솔의 사실이라 컴패니언과 무관). */
    public void providers(Consumer<Object> cb) {
        if (providerList != null) { cb.accept(providerList); return; }
        source.providers(got -> { providerList = got; cb.accept(got); });
    }

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
