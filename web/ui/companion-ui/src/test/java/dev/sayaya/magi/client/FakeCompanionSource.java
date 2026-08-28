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

    private Consumer<Object> rosterCb = null;

    /**
     * 스펙이 "지금 이 컴패니언이 무엇을 묻는다"를 만들 수 있게 하는 문 —
     * window.__magi_test_ask(kind, options)를 부르면 명단이 그 사실을 안고 다시 흐른다.
     * 실제로도 명단은 계속 흐르며 이 사실을 나른다(스트림).
     */
    private void openAskDoor() {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_ask",
                (AskFn) (kind, options) -> {
                    if (rosterCb == null) return;
                    String opts = options == null || options.isEmpty() ? ""
                            : ",\"askOptions\":" + options;
                    rosterCb.accept(Global.JSON.parse(
                            "[{\"socket\":\"/tmp/a1.sock\",\"name\":\"alpha\",\"state\":" +
                            (kind == null ? "\"working\"" : "\"waiting\"") +
                            ",\"steps\":7,\"idle\":42,\"workdir\":\"/Users/you/work/app\"," +
                            "\"session\":\"s_demo1\",\"permission\":\"ask\"" +
                            (kind == null ? "" :
                             ",\"asking\":\"may I drop the table?\",\"askId\":\"call_7\"," +
                             "\"askKind\":\"" + kind + "\",\"askIndex\":1,\"askTotal\":2," +
                             "\"report\":[{\"key\":\"why\",\"text\":\"the migration needs it\"}]" + opts) +
                            "}]"));
                });
    }

    @jsinterop.annotations.JsFunction
    public interface AskFn { void call(String kind, String optionsJson); }

    @Override
    public void roster(Consumer<Object> cb) {
        rosterCb = cb;
        openAskDoor();
        cb.accept(Global.JSON.parse(
                "[{\"socket\":\"/tmp/a1.sock\",\"name\":\"alpha\",\"state\":\"working\"," +
                "\"steps\":7,\"idle\":42,\"role\":\"keeps the build green\",\"team\":\"core\",\"hub\":true," +
                "\"host\":\"devbox\",\"addr\":\"10.0.0.7\",\"pid\":4242,\"version\":\"v0.28.0\"," +
                "\"trust\":\"own\"," +
                "\"workdir\":\"/Users/you/work/app\",\"session\":\"s_demo1\",\"permission\":\"ask\"," +
                "\"handling\":true,\"waiting\":2,\"model\":\"gpt-oss:120b\"}," +
                // 둘째 행은 그리지 않는다 — 여기 있는 이유는 하나다: 뒤처졌다는 것은 <b>명단이</b>
                // 아는 사실이고, 견줄 상대가 없으면 갱신 버튼이 설 근거도 없다.
                "{\"socket\":\"/tmp/a2.sock\",\"name\":\"beta\",\"state\":\"idle\"," +
                "\"steps\":0,\"idle\":90,\"version\":\"v0.29.0\",\"trust\":\"own\"}]"));
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
    public void plan(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse(
                "[{\"content\":\"read the failing test\",\"status\":\"completed\"}," +
                "{\"content\":\"fix the retry window\",\"status\":\"in_progress\"}," +
                "{\"content\":\"write it down\",\"status\":\"pending\"}]"));
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

    // ── 사실판이 바꿀 수 있는 것들 ────────────────────────────────────────────
    @Override
    public void models(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse("[\"fast-model\",\"deep-model\"]"));
    }

    @Override
    public void providers(java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "[{\"name\":\"gateway\",\"base\":\"http://127.0.0.1:47311/v1\",\"models\":[\"fast\",\"deep\"]},"
                        + "{\"name\":\"studio\",\"base\":\"http://127.0.0.1:11434/v1\",\"models\":[\"local-8b\"]}]"));
    }

    @Override
    public void useProvider(CompanionContext ctx, String base, java.util.function.Consumer<String> why) {
        jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window).set("__magi_test_provider", base);
        why.accept("");
    }

    @Override
    public void model(CompanionContext ctx, String name, java.util.function.Consumer<String> why) {
        jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window).set("__magi_test_model", name);
        why.accept("");
    }

    @Override
    public void permission(CompanionContext ctx, String mode, java.util.function.Consumer<String> why) {
        jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window).set("__magi_test_perm", mode);
        why.accept("");
    }

    @Override
    public void update(CompanionContext ctx, Consumer<String> said) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_update", ctx.socket);
        // 데몬이 뭐라고 답할지는 스펙이 정한다 — 아무것도 정하지 않았으면 회선이 끊긴 셈이다.
        Object canned = Js.asPropertyMap(DomGlobal.window).get("__magi_test_update_says");
        said.accept(canned == null ? "" : String.valueOf(canned));
    }

    @Override
    public void tools(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse("[\"read\",\"edit\",\"bash\"]"));
    }

    @Override
    public void loop(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse("{\"map\":\"1 plan\\n2 edit\",\"origin\":\"\",\"diff\":\"\"}"));
    }

    @Override
    public void reportFormat(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "{\"from\":\"workspace\",\"sections\":[{\"key\":\"what\",\"prompt\":\"What changed\"}]}"));
    }

    @Override
    public void reportFormat(CompanionContext ctx, java.util.List<String> keys, java.util.List<String> prompts,
                             java.util.function.Consumer<String> why) {
        jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window)
                .set("__magi_test_format", String.join(",", keys) + "|" + String.join(",", prompts));
        why.accept("");
    }

    @Override
    public void jobs(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "{\"children\":[{\"id\":\"c1\",\"tool\":\"spawn\",\"task\":\"look at the log\",\"running\":true}]," +
                "\"background\":[{\"command\":\"go build ./...\",\"tail\":\"building\",\"running\":true}]," +
                "\"queued\":[{\"kind\":\"person\",\"text\":\"then push\"},{\"from\":\"alpha-1\",\"text\":\"and tell me\"}]}"));
    }

    @Override
    public void handoffs(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "[{\"to\":\"docs-1\",\"state\":\"working\",\"request\":\"write it up\"}]"));
    }

    @Override
    public void cron(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "[{\"name\":\"nightly\",\"schedule\":\"0 3 * * *\",\"enabled\":false,\"prompt\":\"run it\",\"file\":\".magi/cron.yaml\"}]"));
    }
}