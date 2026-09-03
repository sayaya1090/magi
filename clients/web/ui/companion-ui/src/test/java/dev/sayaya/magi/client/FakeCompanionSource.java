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

    /**
     * 도는 턴인데 <b>아직 도구를 한 번도 안 부른</b> 상태. window.__magi_test_thinking().
     *
     * <p>명단은 열린 턴이 있을 때만 스텝을 채우므로, 이 0 은 "셀 것이 없다"가 아니라 "아직 아무
     * 것도 안 불렀다"다 — 라이브에서 43초째 이 상태였다.
     */
    /**
     * 쉬는 컴패니언의 <b>0</b>. window.__magi_test_idle_no_steps().
     *
     * <p>정지 문의 목은 스텝이 7이라, "쉴 때는 대시"를 그 목으로 재면 7과 대시를 견주게 되고
     * 「언제나 숫자로 그리기」 같은 변이가 통과한다 — 실제로 통과했다. 갈라야 하는 두 값은
     * 같은 0 이다.
     */
    private void openIdleNoStepsDoor() {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_idle_no_steps",
                (StopFn) () -> {
                    if (rosterCb == null) return;
                    rosterCb.accept(Global.JSON.parse(
                            "[{\"socket\":\"/tmp/a1.sock\",\"name\":\"alpha\",\"state\":\"idle\"," +
                            "\"live\":true,\"steps\":0,\"idle\":90,\"role\":\"keeps the build green\"," +
                            "\"team\":\"core\",\"host\":\"devbox\",\"version\":\"v0.28.0\",\"trust\":\"own\"," +
                            "\"workdir\":\"/Users/you/work/app\",\"session\":\"s_demo1\"," +
                            "\"permission\":\"ask\",\"model\":\"gpt-oss:120b\"}]"));
                });
    }

    private void openThinkingDoor() {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_thinking",
                (StopFn) () -> {
                    if (rosterCb == null) return;
                    rosterCb.accept(Global.JSON.parse(
                            "[{\"socket\":\"/tmp/a1.sock\",\"name\":\"alpha\",\"state\":\"working\"," +
                            "\"live\":true,\"steps\":0,\"idle\":0,\"role\":\"keeps the build green\"," +
                            "\"team\":\"core\",\"host\":\"devbox\",\"version\":\"v0.28.0\",\"trust\":\"own\"," +
                            "\"workdir\":\"/Users/you/work/app\",\"session\":\"s_demo1\"," +
                            "\"permission\":\"ask\",\"model\":\"gpt-oss:120b\"}]"));
                });
    }

    /**
     * 같은 문의 다른 쪽 — window.__magi_test_stopped()를 부르면 이 컴패니언이 답하기를
     * 멈춘 채로 명단이 다시 흐른다. 행은 <b>남는다</b>: 소켓 파일은 데몬보다 오래 살아서,
     * 명단이 실어 나르는 것은 "없다"가 아니라 "답하지 않는다"다.
     */
    private void openStopDoor() {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_stopped",
                (StopFn) () -> {
                    if (rosterCb == null) return;
                    rosterCb.accept(Global.JSON.parse(
                            "[{\"socket\":\"/tmp/a1.sock\",\"name\":\"alpha\",\"state\":\"stopped\"," +
                            "\"live\":false,\"steps\":7,\"idle\":42,\"role\":\"keeps the build green\"," +
                            "\"team\":\"core\",\"host\":\"devbox\",\"version\":\"v0.28.0\",\"trust\":\"own\"," +
                            "\"workdir\":\"/Users/you/work/app\",\"session\":\"s_demo1\"," +
                            "\"permission\":\"ask\",\"model\":\"gpt-oss:120b\"}]"));
                });
    }

    @jsinterop.annotations.JsFunction
    public interface StopFn { void call(); }

    /**
     * 한 시간을 아무도 안 보는 채로 일하고 나서 묻는 질문 — 긴 명령 하나와 산문 세 토막.
     * 운영이 딥 화면을 지은 이유가 이 모양이다("a strip at the bottom of a transcript is where
     * prose goes to be skipped"). 이 콘솔은 딥 화면 대신 도크가 이것을 인다: 잰다.
     */
    private void openLongAskDoor() {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_ask_long", (StopFn) () -> {
            if (rosterCb == null) return;
            StringBuilder cmd = new StringBuilder("psql -h prod-1 -c \\\"");
            for (int i = 0; i < 40; i++) {
                cmd.append("delete from staging_invoices_2026_0").append(i % 10)
                   .append(" where imported_at < now() - interval '90 days'; ");
            }
            cmd.append("\\\"");
            String prose = "we ran it against the replica first and it took 41 minutes there, "
                    + "which is longer than the window the nightly job leaves open, and the rows "
                    + "it touches are the ones the invoice export reads at 03:00. ";
            rosterCb.accept(Global.JSON.parse(
                    "[{\"socket\":\"/tmp/a1.sock\",\"name\":\"alpha\",\"state\":\"waiting\"," +
                    "\"steps\":7,\"idle\":42,\"workdir\":\"/Users/you/work/app\"," +
                    "\"session\":\"s_demo1\",\"permission\":\"ask\"," +
                    "\"asking\":\"" + cmd + "\",\"askId\":\"call_9\"," +
                    "\"askKind\":\"permission\",\"askIndex\":1,\"askTotal\":1," +
                    "\"report\":[{\"key\":\"tried\",\"text\":\"" + prose + prose + "\"}," +
                    "{\"key\":\"found\",\"text\":\"" + prose + prose + "\"}," +
                    "{\"key\":\"risk\",\"text\":\"" + prose + prose + "\"}]}]"));
        });
    }


    @Override
    public void roster(Consumer<Object> cb) {
        rosterCb = cb;
        openAskDoor();
        openStopDoor();
        openThinkingDoor();
        openIdleNoStepsDoor();
        openLongAskDoor();
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
                "{\"id\":\"s_old\",\"title\":\"fix the retry storm\",\"ago\":7200}," +
                // 제목 없는 지난 세션 — 제목은 첫 프롬프트에서 나므로 아무 말도 오가지 않은
                // 세션에는 없다. 고르개가 이것을 뭐라 적는지가 지금 이 세션과 갈린다.
                "{\"id\":\"s_bare\",\"ago\":9000}]"));
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

    /**
     * 가진 도구들. 스펙이 window.__magi_test_tools_says에 JSON을 놓아 두면 그것으로 답한다 —
     * 특히 <b>빈 배열</b>: 그것은 "도구가 없다"가 아니라 "물어볼 수 없을 만큼 낡은 데몬"이고,
     * 화면이 다른 말을 적으면 사실을 지어내는 것이 된다(운영 규칙). 그 갈래는 여기로만 온다.
     * <p>
     * `'null'`을 놓으면 <b>못 받은 답</b>이 된다(JSON.parse가 null을 돌려준다). 빈 배열과 같은
     * 사정이 아니다 — 그쪽은 데몬이 답한 것이고, 이쪽은 답이 오지 않은 것이다.
     */
    @Override
    public void tools(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(canned("__magi_test_tools_says",
                "[\"read\",\"edit\",\"bash\"]")));
    }

    /** 턴의 지도. __magi_test_loop_says로 갈라져 나온 세션(origin·diff 있음)도 세울 수 있다. */
    @Override
    public void loop(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(canned("__magi_test_loop_says",
                "{\"map\":\"1 plan\\n2 edit\",\"origin\":\"\",\"diff\":\"\"}")));
    }

    /** 스펙이 놓아 둔 답, 아니면 늘 하던 답. */
    private static String canned(String key, String fallback) {
        Object said = Js.asPropertyMap(DomGlobal.window).get(key);
        return said == null ? fallback : String.valueOf(said);
    }

    /**
     * 보고 양식. `__magi_test_format_says = 'null'`을 놓으면 <b>못 읽은 것</b>이 된다 —
     * 절이 없는 것과 같은 사정이 아니다(그쪽은 데몬이 답한 것이다).
     */
    @Override
    public void reportFormat(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(canned("__magi_test_format_says",
                "{\"from\":\"workspace\",\"sections\":[{\"key\":\"what\",\"prompt\":\"What changed\"}]}")));
    }

    @Override
    public void reportFormat(CompanionContext ctx, java.util.List<String> keys, java.util.List<String> prompts,
                             java.util.function.Consumer<String> why) {
        jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window)
                .set("__magi_test_format", String.join(",", keys) + "|" + String.join(",", prompts));
        // 서버가 거절하며 적어 보내는 사유를 스펙이 대신 놓아 둔다(reportfmt.go: "a report needs
        // at least one section", "two sections named …"). 빈 문자열은 이 문의 말로 <b>성공</b>이다.
        why.accept(canned("__magi_test_format_refuses", ""));
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