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

    @JsFunction
    interface SubHook { void call(String sub); }

    @JsFunction
    interface RowsHook { void call(String json); }

    @JsFunction
    interface Fire { void call(); }

    /** 붙들린 두 읽기 — 놓기 전까지가 「아직」이다. */
    private Consumer<Object> heldList = null;
    private Consumer<Object> heldRows = null;

    /** 붙들리는 아이 · 전사를 읽지 못하는 아이 — 갈래마다 제 아이디를 준다. */
    private static final String SLOW = "s_slow", UNREAD = "s_unread";

    @Override
    public void start(Listener l) {
        listener = l;
        // 층위 전환은 주소(셸)의 것 — 단독 테스트는 이 훅으로 컨텍스트를 갈아탄다.
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_past", (PastHook) past ->
                listener.context(CompanionContext.of("/tmp/a1.sock", null, "1", past)));
        // 자식 층위도 주소의 것이다 — past와 나란한 또 하나의 조각이라 훅도 나란히 둔다.
        // 둘을 한 훅으로 묶지 않는다: 지난 일과 자식은 <b>같이 서지 않는</b>다(셸의 Place도
        // 그렇게 만든다). 한 훅에 둘을 받게 하면 스펙이 설 수 없는 자리를 세울 수 있다.
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_sub", (SubHook) sub ->
                listener.context(CompanionContext.of("/tmp/a1.sock", null, "1", null, null, false, "", sub)));
        // 전사는 <b>자란다</b> — 한 번만 밀면 자라는 자리에서 무엇이 일어나는지를 잴 수 없다
        // (행 재사용도, 짧아진 전사의 꼬리 제거도, 가운데 행이 끝나는 프레임도 전부 두 번째
        // 프레임에서만 보인다). 그 두 번째를 여기서 준다.
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_transcript", (RowsHook) json ->
                listener.transcript(json == null ? null : Global.JSON.parse(json)));
        // 붙들었던 두 읽기를 한꺼번에 놓는다 — 「아직」과 「읽고 났다」가 갈리는 그 순간이
        // 실제 소켓에는 있고 동기 콜백에는 없다. 그 사이를 스펙이 볼 수 있게 여는 문.
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_sub_release", (Fire) () -> {
            Consumer<Object> list = heldList, rows = heldRows;
            heldList = heldRows = null;
            if (list != null) list.accept(Global.JSON.parse(
                    "[{\"id\":\"s_slow\",\"role\":\"judge\",\"task\":\"weigh it\","
                    + "\"model\":\"qwen3-coder-next\",\"running\":true}]"));
            if (rows != null) rows.accept(Global.JSON.parse(
                    "[{\"who\":\"user\",\"text\":\"weigh it\"}]"));
        });
        l.context(CompanionContext.of("/tmp/a1.sock", null, "1", null));
        l.transcript(Global.JSON.parse(
                "[{\"who\":\"user\",\"text\":\"fix the build\",\"at\":\"2026-08-27T04:00:00Z\"}," +
                "{\"who\":\"thinking\",\"text\":\"read the log first\\nthen build\"}," +
                // 모델이 쓴 글은 마크다운으로 도착한다 — 표·펜스·강조가 한 행에 다 들어 있는
                // 본문 하나면 그리는 규칙 전부를 한 번에 잰다.
                "{\"who\":\"assistant\",\"text\":\"looking at the **log**\\n\\n| a | b |\\n|---|---|\\n| 1 | 2 |\\n\\n- one\\n- two\\n\\n```go\\nfmt.Println()\\n```\\n\\n[x](https://e.com) [no](javascript:alert(1))\\n\\n<b>raw</b>\"}," +
                "{\"who\":\"tool\",\"tool\":\"bash\",\"args\":\"{\\\"command\\\":\\\"go build ./...\\\"}\"," +
                    "\"out\":\"\\\"ok: 12 packages\\\\nwarnings: 0\\\"\",\"ok\":true}," +
                "{\"who\":\"tool\",\"tool\":\"edit\",\"args\":\"{\\\"path\\\":\\\"main.go\\\"}\"," +
                    "\"diff\":\"--- a/main.go\\n+++ b/main.go\\n@@ -1 +1 @@\\n-old\\n+new\",\"ok\":false}," +
                // 한 자리의 표 — 전선에 실려 오는 그대로(결정·렌즈·확신·이유·다음·유지·근거).
                // 이 여섯이 없으면 표결 카드는 이름 하나와 "근거: 없음"뿐이다.
                "{\"who\":\"council\",\"round\":2,\"member\":\"Melchior\",\"decision\":\"continue\"," +
                    "\"lens\":\"correctness\",\"confidence\":0.9,\"text\":\"\\u2717 reject (correctness) \\u00b7 90%\"," +
                    "\"why\":\"the report summarises instead of quoting\"," +
                    "\"feedback\":\"paste the exact output\",\"keep\":\"the build fix already landed\"," +
                    "\"cite\":\"bash ls -la: exit 0\"}," +
                // 아무 것도 대지 않은 찬성 — 근거 없음이 그 자체로 읽을 사실인 쪽.
                "{\"who\":\"council\",\"round\":2,\"member\":\"Balthasar\",\"decision\":\"done\"," +
                    "\"text\":\"\\u2713 done\"}," +
                "{\"who\":\"assistant\",\"text\":\"one failure left\",\"pending\":true}]"));
        l.turn(true, 12);
    }


    @Override
    public void subagents(CompanionContext ctx, Consumer<Object> cb) {
        if (SLOW.equals(ctx.sub)) { heldList = cb; return; }
        // 하나면 족하다 — 스펙이 재는 것은 "그 아이의 자리가 서는가"이지 목록의 길이가 아니다.
        // 하나뿐이라는 것이 <b>명단에 없는 아이</b> 갈래도 연다(다른 아이디를 대면 못 찾는다).
        cb.accept(Global.JSON.parse(
                "[{\"id\":\"s_kid\",\"role\":\"scout\",\"task\":\"find the empty states\","
                + "\"model\":\"qwen3-coder-next\",\"running\":false}]"));
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
        if (SLOW.equals(session)) { heldRows = cb; return; }
        // 읽지 못한 전사는 null로 온다 — 거부도 불통도 깨진 본문도 한 값이다(Console.fetchList).
        if (UNREAD.equals(session)) { cb.accept(null); return; }
        // 끝난 일에도 표는 있다 — 그리고 그 표의 근거를 물을 때 어느 세션에 묻는가가
        // 운영이 되밟은 자리다("일이 끝나면 카운슬의 근거에 닿을 수 없다"). 그 갈래를
        // 여기서만 열 수 있으므로 지난 전사에도 카운슬 행 하나를 둔다.
        cb.accept(Global.JSON.parse(
                "[{\"who\":\"user\",\"text\":\"old prompt\"}," +
                "{\"who\":\"assistant\",\"text\":\"old answer\"}," +
                "{\"who\":\"council\",\"round\":5,\"member\":\"Casper\",\"decision\":\"done\"," +
                    "\"text\":\"\\u2713 done\"}]"));
    }




    @Override
    public void submit(CompanionContext ctx, String text, Consumer<String> why) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_sent", text + "@" + ctx.socket);
        why.accept("");
    }

    /**
     * 옮기기 — 옮긴 곳을 창에 적고, 창에 미리 놓인 사유가 있으면 그것으로 거부한다.
     * 거부된 옮기기 뒤에 보내기가 따라가지 않는다는 것이 이 포트로 재는 사실이다.
     */
    @Override
    public void resume(CompanionContext ctx, String session, Consumer<String> why) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_resumed", session);
        Object no = win.get("__magi_test_resume_refuses");
        why.accept(no == null ? "" : String.valueOf(no));
    }

    /** 답은 다른 곳에 적는다 — 같은 상자에 쓴 글이 어디로 갔는지가 이 스펙의 요점이라서. */

    @Override
    public void interrupt(CompanionContext ctx, java.util.function.Consumer<String> why) {
        jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window).set("__magi_test_interrupt", "yes");
        why.accept("");
    }

    @Override
    public void suggest(CompanionContext ctx, String prefix, java.util.function.Consumer<String> text) {
        jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window).set("__magi_test_suggest", prefix);
        text.accept(" and then some");
    }

    @Override
    public void councilEvidence(CompanionContext ctx, int round, java.util.function.Consumer<Object> cb) {
        // 라운드만이 아니라 <b>어느 세션에</b> 물었는지까지 적는다 — 지난 세션의 표를 열고
        // 지금 대화에 물으면 근거는 null로 오고, 화면에는 이름 하나만 남는다(운영의 그 결함).
        //
        // null을 ""로 접지 않는다. past는 세 뜻이고(null=지금 대화, ""=지난 일 목록, 값=그 세션)
        // 층위를 잃는 회귀의 실제 산출물이 ""이다(Moves.to가 null과 ""를 같이 ""로 돌려준다).
        // 접으면 잃은 날의 계기판이 건강한 날과 같은 글자를 읽는다 — 재는 자리가 재는 것을
        // 표현할 수 없으면 그 자리는 없는 것이다.
        jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window).set("__magi_test_council",
                round + "@" + (ctx.past == null ? "-" : ctx.past));
        cb.accept(elemental2.core.Global.JSON.parse(
                "{\"task\":\"the task it judged\",\"report\":\"what was reported\",\"actions\":\"read · edit\"}"));
    }
}