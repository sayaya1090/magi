package dev.sayaya.magi.client;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.client.usecase.CompanionSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.annotations.JsFunction;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 고정 컨텍스트(타입 1)와 다섯 행의 전사, 열린 턴 — HTTP 없이 화면을 그린다.
 * submit은 window.__magi_test_sent 에 적는다.
 */
@Singleton
public class FakeCompanionSource implements CompanionSource {
    private Listener listener;

    @Inject
    public FakeCompanionSource() {}

    @JsFunction
    interface PastHook { void call(String past); }

    @Override
    public void start(Listener l) {
        listener = l;
        // 층위 전환은 주소(셸)의 것 — 단독 테스트는 이 훅으로 컨텍스트를 갈아탄다.
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_past", (PastHook) past ->
                listener.context(CompanionContext.of("/tmp/a1.sock", null, "1", past)));
        l.context(CompanionContext.of("/tmp/a1.sock", null, "1", null));
        l.transcript(Global.JSON.parse(
                "[{\"who\":\"user\",\"text\":\"fix the build\",\"at\":\"2026-08-27T04:00:00Z\"}," +
                "{\"who\":\"thinking\",\"text\":\"read the log first\\nthen build\"}," +
                "{\"who\":\"assistant\",\"text\":\"looking at the log\"}," +
                "{\"who\":\"tool\",\"tool\":\"bash\",\"args\":\"{\\\"command\\\":\\\"go build ./...\\\"}\"," +
                    "\"out\":\"\\\"ok: 12 packages\\\\nwarnings: 0\\\"\",\"ok\":true}," +
                "{\"who\":\"tool\",\"tool\":\"edit\",\"args\":\"{\\\"path\\\":\\\"main.go\\\"}\"," +
                    "\"diff\":\"--- a/main.go\\n+++ b/main.go\\n@@ -1 +1 @@\\n-old\\n+new\",\"ok\":false}," +
                "{\"who\":\"assistant\",\"text\":\"one failure left\",\"pending\":true}]"));
        l.turn(true, 12);
    }

    @Override
    public void roster(Consumer<Object> cb) {
        cb.accept(Global.JSON.parse(
                "[{\"socket\":\"/tmp/a1.sock\",\"name\":\"alpha\",\"state\":\"working\"," +
                "\"steps\":7,\"idle\":42,\"role\":\"keeps the build green\",\"team\":\"core\",\"hub\":true," +
                "\"host\":\"devbox\",\"addr\":\"10.0.0.7\",\"pid\":4242,\"version\":\"v0.28.0\"," +
                "\"workdir\":\"/Users/you/work/app\",\"session\":\"s_demo1\",\"permission\":\"ask\"," +
                "\"handling\":true,\"waiting\":2,\"model\":\"gpt-oss:120b\"}]"));
    }

    @Override
    public void history(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse(
                "[{\"id\":\"s_now\",\"title\":\"the open one\",\"ago\":0,\"current\":true}," +
                "{\"id\":\"s_old\",\"title\":\"fix the retry storm\",\"ago\":7200}]"));
    }

    @Override
    public void pastTranscript(CompanionContext ctx, String session, Consumer<Object> cb) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_past_read", session);
        cb.accept(Global.JSON.parse(
                "[{\"who\":\"user\",\"text\":\"old prompt\"}," +
                "{\"who\":\"assistant\",\"text\":\"old answer\"}]"));
    }

    @Override
    public void context(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse(
                "{\"used\":82000,\"window\":100000,\"messages\":41,\"estimated\":false," +
                "\"cacheReported\":true,\"cached\":41000,\"model\":\"gpt-oss:120b\"," +
                "\"compactions\":2,\"shed\":31000,\"lastBefore\":40000,\"lastAfter\":9000," +
                "\"lastAt\":\"2026-08-27T04:31:00\"}"));
    }

    @Override
    public void compact(CompanionContext ctx, Runnable done) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_compacted", ctx.socket);
        done.run();
    }

    @Override
    public void submit(CompanionContext ctx, String text, Consumer<String> why) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_sent", text + "@" + ctx.socket);
        why.accept("");
    }
}
