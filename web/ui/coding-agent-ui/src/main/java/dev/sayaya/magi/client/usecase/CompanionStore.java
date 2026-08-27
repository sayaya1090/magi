package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.rx.Observable;
import dev.sayaya.rx.subject.BehaviorSubject;

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
    private final BehaviorSubject<CompanionContext> ctxOf = dev.sayaya.rx.subject.BehaviorSubject.behavior(null);
    private final BehaviorSubject<Object> rowsOf = dev.sayaya.rx.subject.BehaviorSubject.behavior(null);
    private final BehaviorSubject<Object> rosterOf = dev.sayaya.rx.subject.BehaviorSubject.behavior(null);
    private final List<BiConsumer<Boolean, Double>> turnObs = new ArrayList<>();
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
        // 명단 구독은 하나다 — 이름을 묻는 자리마다 걸면 화면을 옮길 때마다 겹으로 쌓인다.
        RosterSharing.subscribe(list -> { if (list != null) rosterOf.next(list); });
    }

    public void onContext(Consumer<CompanionContext> o) { ctxOf.subscribe(o); }

    public void onRows(Consumer<Object> o) { rowsOf.subscribe(o); }

    /**
     * 지금 보는 컴패니언의 <b>행</b> — 명단 전체가 아니라 그 조각이고, 같은 말을 다시 하면
     * 흐르지 않는다. 이름을 물을 때도, 아직 답하는지 물을 때도 이 하나를 본다.
     */
    public Observable<FleetAgent> aimed() {
        return rosterOf.map(this::rowOf)
                .distinctUntilChanged((java.util.function.BiFunction<FleetAgent, FleetAgent, Boolean>)
                        CompanionStore::same);
    }

    /** 이 소켓의 데몬이 아직 답하는가 — 명단을 아직 못 받았으면 "산 것"으로 둔다. */
    public Observable<Boolean> alive() {
        return rosterOf.map(list -> list == null || ctx == null || ctx.socket == null
                        || ctx.socket.isEmpty() || rowOf(list) != null)
                .distinctUntilChanged();
    }

    private FleetAgent rowOf(Object list) {
        if (list == null || ctx == null || ctx.socket == null || ctx.socket.isEmpty()) return null;
        jsinterop.base.JsArrayLike<Object> rows = jsinterop.base.Js.uncheckedCast(list);
        String peer = ctx.peer == null ? "" : ctx.peer;
        for (int i = 0; i < rows.getLength(); i++) {
            FleetAgent r = jsinterop.base.Js.uncheckedCast(rows.getAt(i));
            String had = r.peer == null ? "" : r.peer;
            // 소켓과 거쳐 온 콘솔이 둘 다 맞아야 이 컴패니언이고, 명단에 있어도 답하지 않으면
            // 없는 것과 같다 — 다만 말하지 않은 것은 산 것이다(운영 live !== false).
            if (ctx.socket.equals(r.socket) && peer.equals(had)) {
                return !jsinterop.base.Js.asPropertyMap(r).has("live") || r.live ? r : null;
            }
        }
        return null;
    }

    private static boolean same(FleetAgent a, FleetAgent b) {
        if (a == null || b == null) return a == b;
        return (a.state + "|" + a.name + "|" + a.doing + "|" + a.user + "|" + a.waiting
                + "|" + a.handling + "|" + a.live)
                .equals(b.state + "|" + b.name + "|" + b.doing + "|" + b.user + "|" + b.waiting
                + "|" + b.handling + "|" + b.live);
    }

    public void onTurn(BiConsumer<Boolean, Double> o) { turnObs.add(o); o.accept(turnOpen, turnFor); }

    public CompanionContext context() { return ctx; }

    /**
     * 상자에 쓴 한 마디를 보낸다 — 지금 답을 기다리는 부름이 있으면 <b>답으로</b>, 아니면 부탁으로.
     *
     * 한 상자가 두 몫을 하는 이유는 운영과 같다: 질문에 답하는 상자와 새 부탁을 보내는 상자를
     * 위아래로 세우면, 무엇이 무엇인지 말해 주는 표도 없이 글 상자 둘이 겹친다. 지금 무엇을
     * 하는 자리인지는 부모가 알려 준다(AskSharing) — 컴패니언이 무엇에 걸려 있는지는 이
     * 모듈이 아니라 컴패니언 패널이 아는 사실이라서.
     */
    public void submit(String text, Consumer<String> why) {
        if (ctx == null) { why.accept("no companion"); return; }
        if (answering != null) {
            // 답은 부모가 보낸다 — 기다리는 질문을 아는 쪽이 부모이고, /answer의 주인도 하나다.
            dev.sayaya.magi.bridge.AskSharing.answer(text, why::accept);
            return;
        }
        source.submit(ctx, text, why);
    }

    /** 그 라운드가 본 것 — 카드 하나로 펼쳐진다(전사 행에는 담을 자리가 없다). */
    public void councilEvidence(int round, Consumer<Object> cb) {
        if (ctx == null) { cb.accept(null); return; }
        source.councilEvidence(ctx, round, cb);
    }

    /** 컴포저가 쓰다 만 말의 다음 — 답은 이어붙일 글이다(빈 답은 "할 말 없음"이고 정상이다). */
    public void suggest(String prefix, Consumer<String> text) {
        if (ctx == null) { text.accept(""); return; }
        source.suggest(ctx, prefix, text);
    }

    /** 이 컴패니언의 턴을 멈춘다 — 무엇을 물어보고 멈출지는 화면의 몫이다(되돌릴 수 없는 일). */
    public void interrupt(Consumer<String> why) {
        if (ctx == null) return;
        source.interrupt(ctx, why);
    }

    /** 지금 답을 기다리는 부름(없으면 null) — 부모가 알린 사실을 그대로 든다. */
    private Object answering = null;

    public boolean answering() { return answering != null; }

    public void listenForAsk(Runnable changed) {
        dev.sayaya.magi.bridge.AskSharing.subscribe(ask -> {
            boolean was = answering != null;
            answering = ask;
            if (was != (answering != null)) changed.run();
        });
    }

    private static String str(Object o, String key) {
        Object v = jsinterop.base.Js.asPropertyMap(o).get(key);
        return v == null ? "" : String.valueOf(v);
    }

    // ── 지난 일 층위: 목록 또는 한 세션의 전사 — ctx.past가 정한다 ──────────
    private final BehaviorSubject<Object> pastOf = dev.sayaya.rx.subject.BehaviorSubject.behavior(null);
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

    @Override
    public void context(CompanionContext c) {
        ctx = c;
        ctxOf.next(c);
        // 조각은 명단과 <b>지금 보는 컴패니언</b> 둘에서 잘린다. 명단은 제 박자로 흐르므로,
        // 컴패니언이 바뀐 순간 다시 자르지 않으면 다음 명단 프레임까지 빈 판이 서 있는다
        // (실측: 명단을 한 번만 내놓는 데모에서 사실판이 영영 비어 있었다).
        if (rosterOf.getValue() != null) rosterOf.next(rosterOf.getValue());
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
        for (BiConsumer<Boolean, Double> o : turnObs) o.accept(open, forSec);
    }
}
