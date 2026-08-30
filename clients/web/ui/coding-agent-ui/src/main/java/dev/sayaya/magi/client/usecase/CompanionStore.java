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
                        || ctx.socket.isEmpty()
                        || dev.sayaya.magi.bridge.AgentStates.answering(rowOf(list)))
                .distinctUntilChanged();
    }

    /**
     * 어디에 있나 — 소켓과 거쳐 온 콘솔이 둘 다 맞는 행이고, <b>답하는지는 묻지 않는다</b>.
     * 멈춘 컴패니언도 이름이 있고 어느 세션에 있었는지가 있다: 그것을 없는 것으로 돌려주면
     * 멈추는 순간 읽을 것이 사라진다. 답하는지는 {@link #alive()}가 따로 답한다.
     */
    private FleetAgent rowOf(Object list) {
        if (list == null || ctx == null || ctx.socket == null || ctx.socket.isEmpty()) return null;
        jsinterop.base.JsArrayLike<Object> rows = jsinterop.base.Js.uncheckedCast(list);
        String peer = ctx.peer == null ? "" : ctx.peer;
        for (int i = 0; i < rows.getLength(); i++) {
            FleetAgent r = jsinterop.base.Js.uncheckedCast(rows.getAt(i));
            String had = r.peer == null ? "" : r.peer;
            if (ctx.socket.equals(r.socket) && peer.equals(had)) return r;
        }
        return null;
    }

    private static boolean same(FleetAgent a, FleetAgent b) {
        if (a == null || b == null) return a == b;
        // 세션도 이 목록에 있어야 한다: 옮기고 나면 <b>세션만</b> 바뀌는 명단이 온다(상태도
        // 이름도 그대로다). 빠뜨리면 그 조각이 흐르지 않아, 컴포저는 이미 들어와 있는 대화로
        // "옮겨서 이어가겠다"고 계속 말한다.
        return (a.state + "|" + a.name + "|" + a.doing + "|" + a.user + "|" + a.waiting
                + "|" + a.handling + "|" + a.live + "|" + a.session)
                .equals(b.state + "|" + b.name + "|" + b.doing + "|" + b.user + "|" + b.waiting
                + "|" + b.handling + "|" + b.live + "|" + b.session);
    }

    public void onTurn(BiConsumer<Boolean, Double> o) {
        turnOf.distinctUntilChanged().subscribe(t -> o.accept(t.open, t.forSec));
    }

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

    /**
     * 컴패니언을 그 세션으로 옮긴다 — 보내기 <b>전에</b>, 그리고 옮기지 못하면 보내지 않는다.
     *
     * 순서를 여기서 강제하지 않는 이유: 무엇을 물어보고 옮길지는 화면의 몫이고(되돌릴 수 없는
     * 일이라 한 번 묻는다), 스토어는 두 문을 따로 열어 둔다.
     */
    public void resume(String session, Consumer<String> why) {
        if (ctx == null) { why.accept("no companion"); return; }
        source.resume(ctx, session, why);
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

    // ── 자식 층위: 그 아이가 무엇이었나 + 그 아이의 전사 ────────────────────
    private final BehaviorSubject<Object> subOf = dev.sayaya.rx.subject.BehaviorSubject.behavior(null);
    private boolean subMetaRead = false;
    private boolean subRowsRead = false;
    private Object subMeta = null;    // /subagents의 그 행
    private Object subRows = null;    // 그 아이디로 읽은 전사
    private String subFor = null;

    /** 구독자는 (그 아이, 그 전사)를 함께 받는다 — 둘 다 null이면 아직 읽는 중이다. */
    public void onSub(Consumer<Object> o) { subOf.subscribe(o); }

    public Object subMeta() { return subMeta; }

    /**
     * 두 읽기가 <b>돌아왔는가</b> — 값만으로는 「아직」과 「없음」이 같은 null이다.
     * 이 둘을 섞으면 아직 아무것도 안 읽은 화면이 읽기가 끝난 것처럼 선다(자매 함수
     * paintPast는 그래서 null을 그리지 않는다).
     */
    public boolean subMetaRead() { return subMetaRead; }

    public boolean subRowsRead() { return subRowsRead; }

    private void askSub() {
        if (ctx == null || ctx.sub == null || ctx.sub.isEmpty()) {
            subMeta = subRows = null;
            subMetaRead = subRowsRead = false;
            subFor = null;
            emitSub();
            return;
        }
        final String want = ctx.socket + "\u0000" + ctx.sub;
        if (want.equals(subFor)) return;
        subFor = want;
        subMeta = subRows = null;
        subMetaRead = subRowsRead = false;
        emitSub();
        final String id = ctx.sub;
        source.subagents(ctx, list -> {
            if (!want.equals(subFor)) return;   // 늦은 답이 새 층위에 앉지 않게
            subMeta = rowWithId(list, id);
            // 답은 왔다 — 그 안에 이 아이가 없었다는 것까지가 읽은 것이다.
            subMetaRead = true;
            emitSub();
        });
        source.pastTranscript(ctx, id, rows -> {
            if (!want.equals(subFor)) return;
            subRows = rows;
            subRowsRead = true;
            emitSub();
        });
    }

    private static Object rowWithId(Object list, String id) {
        if (list == null) return null;
        jsinterop.base.JsArrayLike<Object> all = jsinterop.base.Js.uncheckedCast(list);
        for (int i = 0; i < all.getLength(); i++) {
            jsinterop.base.JsPropertyMap<Object> one = jsinterop.base.Js.uncheckedCast(all.getAt(i));
            if (id.equals(String.valueOf(one.get("id")))) return one;
        }
        return null;
    }

    private void emitSub() { subOf.next(subRows); }

    @Override
    public void context(CompanionContext c) {
        ctx = c;
        ctxOf.next(c);
        // 조각은 명단과 <b>지금 보는 컴패니언</b> 둘에서 잘린다. 명단은 제 박자로 흐르므로,
        // 컴패니언이 바뀐 순간 다시 자르지 않으면 다음 명단 프레임까지 빈 판이 서 있는다
        // (실측: 명단을 한 번만 내놓는 데모에서 사실판이 영영 비어 있었다).
        if (rosterOf.getValue() != null) rosterOf.next(rosterOf.getValue());
        askPast();
        askSub();
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
