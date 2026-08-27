package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.CompanionSharing;
import dev.sayaya.magi.bridge.TranscriptSharing;
import dev.sayaya.magi.client.usecase.CompanionSource;
import elemental2.core.Global;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** 이 화면이 데몬 없이 답하는 것 — 지난 일 층위와, 보낸 척하는 한 마디. */
@Singleton
public class DemoCompanionSource implements CompanionSource {
    @Inject
    public DemoCompanionSource() {}

    @Override
    public void start(Listener l) {
        // 전사도 컨텍스트도 셸의 목에서 브리지로 온다 — 데모에서도 길이 같다.
        CompanionSharing.subscribe(l::context);
        TranscriptSharing.subscribe(l::transcript);
        TranscriptSharing.subscribeTurn(l::turn);
    }

    @Override
    public void history(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[{\"id\":\"s_now\",\"title\":\"run the migration\",\"current\":true,"
                + "\"started\":\"2026-08-27T09:00:00\"},"
                + "{\"id\":\"s_old\",\"title\":\"fix the retry storm\",\"started\":\"2026-08-26T11:00:00\","
                + "\"ended\":\"2026-08-26T12:40:00\"}]"));
    }

    @Override
    public void pastTranscript(CompanionContext ctx, String session, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[{\"who\":\"user\",\"text\":\"why did the retries storm?\"},"
                + "{\"who\":\"assistant\",\"text\":\"the backoff had no ceiling — capped at 30s\"}]"));
    }

    @Override
    public void submit(CompanionContext ctx, String text, Consumer<String> why) { why.accept(""); }

    @Override
    public void interrupt(CompanionContext ctx, java.util.function.Consumer<String> why) {
        // 데모에는 멈출 턴이 없다 — 받아 주고 잊는다.
        why.accept("");
    }

    @Override
    public void suggest(CompanionContext ctx, String prefix, java.util.function.Consumer<String> text) {
        // 데모의 제안은 한 마디다 — 무엇이 일어나는지만 보이면 된다.
        text.accept(prefix.trim().isEmpty() ? "" : " and then run the tests");
    }
}