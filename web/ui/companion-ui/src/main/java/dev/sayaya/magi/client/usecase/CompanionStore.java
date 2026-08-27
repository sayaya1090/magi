package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.BiConsumer;
import java.util.function.Consumer;

/**
 * 화면의 저장소 — 컨텍스트·전사·턴을 들고, 뷰는 여기서만 읽는다.
 * 구독은 현재값을 재생한다: 뷰가 소스보다 늦게 서도 첫 그림을 놓치지 않는다.
 */
@Singleton
public class CompanionStore implements CompanionSource.Listener {
    private final CompanionSource source;
    private final List<Consumer<CompanionContext>> ctxObs = new ArrayList<>();
    private final List<Consumer<Object>> rowsObs = new ArrayList<>();
    private final List<BiConsumer<Boolean, Double>> turnObs = new ArrayList<>();
    private CompanionContext ctx = null;
    private Object rows = null;
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

    public void onContext(Consumer<CompanionContext> o) { ctxObs.add(o); o.accept(ctx); }

    public void onRows(Consumer<Object> o) { rowsObs.add(o); o.accept(rows); }

    public void onTurn(BiConsumer<Boolean, Double> o) { turnObs.add(o); o.accept(turnOpen, turnFor); }

    public CompanionContext context() { return ctx; }

    public void submit(String text, Consumer<String> why) {
        if (ctx == null) { why.accept("no companion"); return; }
        source.submit(ctx, text, why);
    }

    // ── 지난 일 층위: 목록 또는 한 세션의 전사 — ctx.past가 정한다 ──────────
    private final List<Consumer<Object>> pastObs = new ArrayList<>();
    private Object pastData = null;      // 목록(past=="")이거나 전사 행들(past=id)
    private String pastFor = null;

    /** 구독자는 (ctx.past, 자료)를 함께 읽는다 — 자료 null은 "아직/못 읽음". */
    public void onPast(Consumer<Object> o) { pastObs.add(o); o.accept(pastData); }

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

    private void emitPast() { for (Consumer<Object> o : pastObs) o.accept(pastData); }

    // ── 사실판이 읽는 것들: 명단과 컨텍스트 창 ──────────────────────────────
    private final List<Consumer<Object>> rosterObs = new ArrayList<>();
    private final List<Consumer<Object>> ctxInfoObs = new ArrayList<>();
    private Object rosterList = null;
    private Object ctxInfo = null;
    private String ctxFor = null;

    public void onRoster(Consumer<Object> o) { rosterObs.add(o); o.accept(rosterList); }

    public void onContextInfo(Consumer<Object> o) { ctxInfoObs.add(o); o.accept(ctxInfo); }

    /** 지금 접기 — 끝나면 컨텍스트를 다시 읽는다(운영 규칙: 접기 전 숫자를 계속 보이지 않게). */
    public void compact(Runnable after) {
        if (ctx == null) return;
        source.compact(ctx, () -> { ctxFor = null; askContextInfo(); after.run(); });
    }

    private void startFacts() {
        source.roster(list -> {
            if (list != null) rosterList = list;
            for (Consumer<Object> o : rosterObs) o.accept(rosterList);
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
            for (Consumer<Object> o : ctxInfoObs) o.accept(info);
        });
    }

    @Override
    public void context(CompanionContext c) {
        ctx = c;
        for (Consumer<CompanionContext> o : ctxObs) o.accept(c);
        ctxInfo = null;
        for (Consumer<Object> o : ctxInfoObs) o.accept(null);
        askContextInfo();
        askPast();
    }

    @Override
    public void transcript(Object rowsOrNull) {
        rows = rowsOrNull;
        for (Consumer<Object> o : rowsObs) o.accept(rowsOrNull);
    }

    @Override
    public void turn(boolean open, double forSec) {
        turnOpen = open;
        turnFor = forSec;
        for (BiConsumer<Boolean, Double> o : turnObs) o.accept(open, forSec);
    }
}
