package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.CompanionSharing;
import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.bridge.TranscriptSharing;
import dev.sayaya.magi.client.usecase.CompanionSource;
import elemental2.core.Global;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 이 화면이 데몬 없이 답하는 것들 — 계획·컨텍스트·접기, 그리고 지난 일.
 *
 * 컨텍스트와 전사와 명단은 여기 없다: 그것들은 셸의 것이고, 데모에서도 셸의 목에서 브리지로
 * 온다(진짜 콘솔과 같은 길). 목이 모듈마다인 것은 배포가 모듈마다이기 때문이지, 모듈마다
 * 세상을 다시 지어야 한다는 뜻이 아니다.
 */
@Singleton
public class DemoCompanionSource implements CompanionSource {
    @Inject
    public DemoCompanionSource() {}

    @Override
    public void start(Listener l) {
        CompanionSharing.subscribe(l::context);
        TranscriptSharing.subscribe(l::transcript);
        TranscriptSharing.subscribeTurn(l::turn);
    }

    @Override
    public void roster(Consumer<Object> cb) { RosterSharing.subscribe(cb::accept); }

    @Override
    public void history(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[{\"id\":\"s_now\",\"title\":\"run the migration\",\"current\":true,"
                + "\"started\":\"2026-08-27T09:00:00\",\"model\":\"gpt-oss:120b\",\"labels\":[\"migration\"]},"
                + "{\"id\":\"s_old\",\"title\":\"fix the retry storm\",\"started\":\"2026-08-26T11:00:00\","
                + "\"ended\":\"2026-08-26T12:40:00\",\"model\":\"gpt-oss:120b\"}]"));
    }

    @Override
    public void pastTranscript(CompanionContext ctx, String session, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[{\"who\":\"user\",\"text\":\"why did the retries storm?\"},"
                + "{\"who\":\"assistant\",\"text\":\"the backoff had no ceiling — capped at 30s\"}]"));
    }

    @Override
    public void plan(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("[{\"content\":\"read the migration\",\"status\":\"completed\"},"
                + "{\"content\":\"run it on staging\",\"status\":\"in_progress\"},"
                + "{\"content\":\"write down what changed\",\"status\":\"pending\"}]"));
    }

    @Override
    public void context(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("{\"used\":8587,\"max\":131072,\"measured\":true,\"messages\":54}"));
    }

    @Override
    public void compact(CompanionContext ctx, Runnable done) { done.run(); }

    @Override
    public void submit(CompanionContext ctx, String text, Consumer<String> why) { why.accept(""); }


    // ── 사실판이 바꿀 수 있는 것들 — 데모는 답하고 잊는다 ──────────────────────
    @Override
    public void models(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "[\"gpt-oss:120b-cloud\",\"qwen3-coder-next\",\"claude-sonnet-5\"]"));
    }

    @Override
    public void providers(java.util.function.Consumer<Object> cb) {
        // 구 콘솔의 데모와 같은 하나 — 짧은 카탈로그라 두 고르개가 하는 일이 보인다.
        cb.accept(elemental2.core.Global.JSON.parse(
                "[{\"name\":\"gateway\",\"base\":\"http://127.0.0.1:47311/v1\","
                        + "\"models\":[\"fast\",\"balanced\",\"deep\"]}]"));
    }

    @Override
    public void useProvider(CompanionContext ctx, String base, java.util.function.Consumer<String> why) {
        why.accept("");
    }

    @Override
    public void model(CompanionContext ctx, String name, java.util.function.Consumer<String> why) { why.accept(""); }

    @Override
    public void permission(CompanionContext ctx, String mode, java.util.function.Consumer<String> why) { why.accept(""); }

    @Override
    public void tools(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "[\"read\",\"edit\",\"multiedit\",\"bash\",\"glob\",\"grep\",\"todo\",\"hand_off\",\"wait_for\"]"));
    }

    @Override
    public void loop(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "{\"map\":\"1 plan\\n2 read · edit\\n3 build → ok\",\"origin\":\"\",\"diff\":\"\"}"));
    }

    @Override
    public void reportFormat(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        cb.accept(elemental2.core.Global.JSON.parse(
                "{\"from\":\"console\",\"sections\":[{\"key\":\"what\",\"prompt\":\"What changed\"}," +
                "{\"key\":\"why\",\"prompt\":\"Why it was needed\"}]}"));
    }

    @Override
    public void reportFormat(CompanionContext ctx, java.util.List<String> keys, java.util.List<String> prompts,
                             java.util.function.Consumer<String> why) {
        why.accept("");
    }

    @Override
    public void jobs(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        // 구 콘솔의 데모와 같은 둘: 지금 도는 자식 하나와, 나쁘게 끝난 배경 명령 하나 —
        // 성공만 있는 픽스처는 이 카드가 무엇을 위해 있는지 보여 주지 못한다.
        cb.accept(elemental2.core.Global.JSON.parse(
                "{\"children\":[{\"id\":\"s_demo_child\",\"tool\":\"scout\","
                        + "\"task\":\"find every component that draws an empty state\",\"running\":true,\"steps\":4}],"
                        + "\"background\":[{\"id\":\"bg_demo\",\"command\":\"npm run build\",\"running\":false,\"exit\":1,"
                        + "\"tail\":\"compiling\\u2026\\n3 warnings\\nerror: Token --surface-dim is not defined\"}]}"));
    }

    @Override
    public void handoffs(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        // 하나는 끝나 답이 와 있고 하나는 아직 기다린다 — 건넨 일의 두 상태.
        cb.accept(elemental2.core.Global.JSON.parse(
                "[{\"from\":\"design\",\"to\":\"buttons\",\"socket\":\"/demo/buttons.sock\",\"state\":\"idle\","
                        + "\"request\":\"make the toggle read its state from the store\","
                        + "\"answer\":\"the toggle now reads its state from the store rather than a prop\"},"
                        + "{\"from\":\"design\",\"to\":\"api\",\"socket\":\"/demo/api.sock\",\"state\":\"waiting\","
                        + "\"request\":\"confirm the invoice endpoint is idempotent\"}]"));
    }

    @Override
    public void cron(CompanionContext ctx, java.util.function.Consumer<Object> cb) {
        // 셋: 도는 것, 꺼 둔 것, 그리고 <b>영영 안 도는 것</b> — 마지막이 이 목록이 존재하는
        // 이유다(켜져 있고 평범해 보이는데 다시는 아무도 그 얘기를 하지 않는다).
        cb.accept(elemental2.core.Global.JSON.parse(
                "[{\"name\":\"nightly-audit\",\"schedule\":\"0 3 * * *\",\"enabled\":true,"
                        + "\"prompt\":\"walk yesterday's commits and report anything that looks like a regression\","
                        + "\"file\":\"/Users/you/work/design-system/.magi/config.toml\"},"
                        + "{\"name\":\"weekly-report\",\"schedule\":\"0 9 * * 1\",\"enabled\":false,"
                        + "\"prompt\":\"summarise what changed in the design system this week\","
                        + "\"file\":\"/Users/you/.config/magi/config.toml\",\"global\":true},"
                        + "{\"name\":\"leap-day\",\"schedule\":\"0 0 30 2 *\",\"enabled\":true,"
                        + "\"problem\":\"this schedule never comes round\","
                        + "\"prompt\":\"the one nobody noticed had stopped\","
                        + "\"file\":\"/Users/you/work/design-system/.magi/config.toml\"}]"));
    }
}