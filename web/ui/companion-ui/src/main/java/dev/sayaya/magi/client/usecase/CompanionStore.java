package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
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
    private final List<Consumer<Boolean>> turnObs = new ArrayList<>();
    private CompanionContext ctx = null;
    private Object rows = null;
    private boolean turnOpen = false;
    private boolean started = false;

    @Inject
    public CompanionStore(CompanionSource source) { this.source = source; }

    public void start() {
        if (started) return;
        started = true;
        source.start(this);
    }

    public void onContext(Consumer<CompanionContext> o) { ctxObs.add(o); o.accept(ctx); }

    public void onRows(Consumer<Object> o) { rowsObs.add(o); o.accept(rows); }

    public void onTurn(Consumer<Boolean> o) { turnObs.add(o); o.accept(turnOpen); }

    public CompanionContext context() { return ctx; }

    public void submit(String text, Consumer<String> why) {
        if (ctx == null) { why.accept("no companion"); return; }
        source.submit(ctx, text, why);
    }

    @Override
    public void context(CompanionContext c) {
        ctx = c;
        for (Consumer<CompanionContext> o : ctxObs) o.accept(c);
    }

    @Override
    public void transcript(Object rowsOrNull) {
        rows = rowsOrNull;
        for (Consumer<Object> o : rowsObs) o.accept(rowsOrNull);
    }

    @Override
    public void turn(boolean open, double forSec) {
        turnOpen = open;
        for (Consumer<Boolean> o : turnObs) o.accept(open);
    }
}
