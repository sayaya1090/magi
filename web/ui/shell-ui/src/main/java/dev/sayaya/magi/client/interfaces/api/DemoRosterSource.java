package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.RosterSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 데몬 없이 도는 셸 — 정적 데모의 명단과 전사.
 *
 * 명단은 <b>이 모듈의 것</b>이다: /fleet과 /events의 주인이 셸 하나이므로, 그 목도 여기
 * 하나뿐이다. 다른 화면들은 이 명단을 브리지로 받는다 — 진짜 콘솔에서와 똑같이.
 */
@Singleton
public class DemoRosterSource implements RosterSource {
    private Listener listener;
    private String aimed = null;
    /**
     * 지금의 명단 — 상수가 아니라 <b>움직이는 것</b>이다.
     *
     * 데모가 가만히 있으면 이 콘솔이 무엇을 위한 것인지 절반은 보이지 않는다: 걸음이 늘고,
     * 기다리던 것이 답을 받아 일하러 가고, 하나가 나타났다 떠나고, 계획이 채워지는 것 —
     * 그것들은 설명될 수는 있어도 <b>보여질</b> 수는 없다(구 콘솔 데모가 이 시계를 가진 이유).
     */
    private JsArrayLike<Object> now = null;
    private int beat = 0;
    private boolean ticking = false;

    @Inject
    public DemoRosterSource() {}

    @Override
    public void start(Listener l) {
        listener = l;
        l.link(true);
        push();
        // 1초에 한 박자 — 구 콘솔 데모와 같은 박자다. 한 번만 건다: start는 화면마다 불린다.
        if (!ticking) {
            ticking = true;
            DomGlobal.setInterval(a -> tick(), 1000);
        }
    }

    /** 한 박자 — 도는 것은 걸음이 늘고, 살아 있는 것은 쉰 시간이 늘고, 각본이 제 차례를 낸다. */
    private void tick() {
        JsArrayLike<Object> all = fleet();
        beat++;
        for (int i = 0; i < all.getLength(); i++) {
            jsinterop.base.JsPropertyMap<Object> a = Js.uncheckedCast(all.getAt(i));
            if ("working".equals(String.valueOf(a.get("state")))) {
                a.set("steps", num(a, "steps") + 1);
                a.set("idle", 0d);
                double total = num(a, "planTotal"), done = num(a, "planDone");
                if (total > 0 && beat % 3 == 0 && done < total) a.set("planDone", done + 1);
            } else if (Js.isTruthy(a.get("live"))) {
                a.set("idle", num(a, "idle") + 1);
            }
        }
        script();
        push();
    }

    /**
     * 각본 — 무엇이 언제 바뀌는가. 구 콘솔 데모의 그 스무 박자다: 쉬던 것이 일하러 가고,
     * 일하던 것이 사람에게 묻고, 답을 받고, 하나가 나타났다 스무 박자 뒤에 떠난다.
     */
    private void script() {
        switch (beat) {
            case 2: set("buttons", "{\"state\":\"working\",\"live\":true,\"steps\":0,\"idle\":0,"
                    + "\"task\":\"give the switch a disabled state and the tokens for it\","
                    + "\"planDone\":0,\"planTotal\":3}"); break;
            case 4: ask("design", "the empty state needs a word for \\\"nothing yet\\\" — pick one",
                    "question", "call_77"); break;
            case 6: answered("api", "{\"state\":\"working\",\"planDone\":2}"); break;
            case 8: set("ops", "{\"state\":\"idle\",\"live\":true,\"idle\":0,"
                    + "\"task\":\"rotated the staging certificates\"}"); break;
            case 9: arrive(); break;
            case 10: answered("design", "{\"state\":\"working\",\"planDone\":4,"
                    + "\"doing\":\"check 11, 7m30s elapsed, still running\"}"); break;
            case 12: answered("palette", "{\"state\":\"working\",\"planTotal\":3,\"planDone\":1,"
                    + "\"doing\":\"check 3, 1m40s elapsed, not met yet (exit 1)\"}"); break;
            case 14: set("api", "{\"planDone\":3}"); break;
            case 16: set("buttons", "{\"state\":\"idle\",\"planDone\":3,\"idle\":1}"); break;
            case 18: answered("design", "{\"state\":\"idle\",\"planDone\":5,\"idle\":1}"); break;
            case 20: set("ops", "{\"state\":\"stopped\",\"live\":false,\"idle\":90000}"); break;
            case 22: leave("docs2"); break;
            default: break;
        }
    }

    /** 그 이름의 행에 이 사실들을 덮어쓴다. */
    private void set(String name, String patch) {
        jsinterop.base.JsPropertyMap<Object> row = named(name);
        if (row == null) return;
        jsinterop.base.JsPropertyMap<Object> more = Js.uncheckedCast(Global.JSON.parse(patch));
        more.forEach(k -> row.set(k, more.get(k)));
    }

    /** 사람에게 묻기 시작했다 — 물음은 상태이자 문장이다. */
    private void ask(String name, String asking, String kind, String id) {
        jsinterop.base.JsPropertyMap<Object> row = named(name);
        if (row == null) return;
        row.set("state", "waiting");
        row.set("asking", asking);
        row.set("askKind", kind);
        row.set("askId", id);
        row.delete("doing");
    }

    /** 답을 받았다 — 묻던 것이 사라지고 다시 일한다(물음이 남아 있으면 그것이 화면의 사실이다). */
    private void answered(String name, String patch) {
        jsinterop.base.JsPropertyMap<Object> row = named(name);
        if (row == null) return;
        row.delete("asking");
        row.delete("askId");
        row.delete("askKind");
        row.delete("askOptions");
        row.delete("report");
        set(name, patch);
    }

    /** 하나가 온다 — 명단은 사람이 보고 있는 동안에도 늘어난다. */
    private void arrive() {
        elemental2.core.JsArray<Object> all = Js.uncheckedCast(fleet());
        all.push(Global.JSON.parse("{\"socket\":\"/demo/docs2.sock\",\"name\":\"docs2\","
                + "\"role\":\"the handbook and its examples\",\"team\":\"frontend\","
                + "\"workdir\":\"/Users/you/work/docs\",\"session\":\"x1\",\"state\":\"working\","
                + "\"live\":true,\"here\":true,\"task\":\"write the empty-state page from the spec\","
                + "\"steps\":0,\"planDone\":0,\"planTotal\":2,\"idle\":0,\"host\":\"mini\","
                + "\"instance\":\"you@mini\",\"addr\":\"10.0.0.9\",\"pid\":4140}"));
    }

    /** 그리고 떠난다 — 사라지는 것도 이 화면이 보여야 할 사실이다. */
    private void leave(String name) {
        elemental2.core.JsArray<Object> all = Js.uncheckedCast(fleet());
        for (int i = 0; i < all.length; i++) {
            jsinterop.base.JsPropertyMap<Object> row = Js.uncheckedCast(all.getAt(i));
            if (name.equals(String.valueOf(row.get("name")))) { all.splice(i, 1); return; }
        }
    }

    private jsinterop.base.JsPropertyMap<Object> named(String name) {
        JsArrayLike<Object> all = fleet();
        for (int i = 0; i < all.getLength(); i++) {
            jsinterop.base.JsPropertyMap<Object> row = Js.uncheckedCast(all.getAt(i));
            if (name.equals(String.valueOf(row.get("name")))) return row;
        }
        return null;
    }

    private static double num(jsinterop.base.JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? 0 : Js.coerceToDouble(v);
    }

    /** 지금의 명단 — 처음 물을 때 픽스처에서 한 번 만들어 두고, 그 뒤로는 그것이 사실이다. */
    private JsArrayLike<Object> fleet() {
        if (now == null) now = Js.uncheckedCast(Global.JSON.parse(FLEET));
        return now;
    }

    @Override
    public void aim(String socket, String peer) {
        aimed = socket;
        push();
    }

    @Override
    public void facts(java.util.function.Consumer<Object> consoleInfo, java.util.function.Consumer<Object> caps) {
        consoleInfo.accept(Global.JSON.parse("{\"user\":\"you\",\"host\":\"devbox\",\"version\":\"dev\"," +
                "\"embed\":\"nomic-embed-text\"}"));
        // 데모의 사람은 전부 할 수 있다 — 무엇이 가려지는지가 아니라 무엇이 있는지를 보이는 자리다.
        caps.accept(Global.JSON.parse("[\"read\",\"answer\",\"prompt\",\"curate\",\"configure\",\"shell\",\"admin\"]"));
    }

    @Override
    public void refresh() { push(); }

    private double tick = -1;
    private int shown = 0;
    private String streaming = null;

    /** 이 컴패니언의 전사를 한 턴씩 흘린다 — 다른 컴패니언으로 옮기면 처음부터. */
    private void stream() {
        JsArrayLike<Object> all = Js.uncheckedCast(Global.JSON.parse(TRANSCRIPT));
        if (!aimed.equals(streaming)) {
            streaming = aimed;
            shown = 0;
            if (tick >= 0) DomGlobal.clearTimeout(tick);
            tick = DomGlobal.setTimeout(a -> step(all), 200);
            listener.transcript(Global.JSON.parse("[]"));
            return;
        }
        listener.transcript(slice(all, shown));
    }

    private void step(JsArrayLike<Object> all) {
        shown++;
        listener.transcript(slice(all, shown));
        if (shown < all.getLength()) tick = DomGlobal.setTimeout(a -> step(all), 1400);
    }

    private static Object slice(JsArrayLike<Object> all, int n) {
        Object[] out = new Object[Math.min(n, all.getLength())];
        for (int i = 0; i < out.length; i++) out[i] = all.getAt(i);
        return out;
    }

    private void push() {
        listener.roster(Js.uncheckedCast(fleet()));
        if (aimed == null || aimed.isEmpty()) {
            listener.transcript(null);
            listener.turn(false, 0);
            return;
        }
        // 한 턴씩 온다 — 진짜 스트림이 그렇고, 완성된 대화를 통째로 건네는 데모는 그 사실을
        // 한 번도 보여 주지 않는다(구 콘솔 데모와 같은 박자: 200ms 뒤 첫 턴, 이후 1.4초마다).
        stream();
        // 기다리는 컴패니언의 턴은 열려 있지 않다 — 무엇을 물었는지가 화면의 사실이다.
        listener.turn(!aimed.contains("docs"), 42);
    }

    /** 아홉이면 표가 표처럼 읽힌다 — 하나짜리 데모는 이 화면이 무엇을 위한 것인지 못 보여 준다. */
    /**
     * 아홉이면 표가 표처럼 읽힌다 — 하나짜리 데모는 이 화면이 무엇을 위한 것인지 못 보여 준다.
     *
     * 명단은 <b>구 콘솔의 데모와 같은 아홉</b>이다: 두 데모를 나란히 놓고 볼 때 다른 함대를 보고
     * 있으면 화면 차이인지 자료 차이인지 아무도 가릴 수 없다. 네 가지 인스턴스(내 기계의 내 것,
     * 내 다른 기계, 남의 이 기계, 소문으로만 아는 것)가 한 벌씩 들어 있는 것이 이 픽스처의 요점이다.
     */
    private static final String FLEET = "[{\"socket\": \"/demo/design.sock\", \"name\": \"design\", \"role\": \"the design system: component specs and visual review\","
            + " \"team\": \"frontend\", \"hub\": true, \"workdir\": \"/Users/you/work/design-system\", \"session\": \"d1\","
            + " \"state\": \"working\", \"live\": true, \"task\": \"spec the empty state for the fleet table, and name the exact tokens\","
            + " \"doing\": \"check 6, 4m12s elapsed, not met yet (exit 1)\", \"steps\": 7, \"planDone\": 2, \"planTotal\": 5,"
            + " \"idle\": 12, \"host\": \"studio\", \"instance\": \"you@studio\", \"trust\": \"own\", \"addr\": \"10.0.0.4\","
            + " \"pid\": 4127, \"permission\": \"auto\", \"user\": \"you\", \"version\": \"v0.23.0\", \"here\": true}, {\"socket\": \"/demo/api.sock\","
            + " \"name\": \"api\", \"role\": \"the billing API and its contracts\", \"team\": \"backend\", \"hub\": true,"
            + " \"workdir\": \"/Users/you/work/billing\", \"session\": \"a1\", \"state\": \"waiting\", \"live\": true, \"asking\": \"run: psql -c \\\"drop table staging_invoices\\\"\","
            + " \"askId\": \"call_42\", \"askKind\": \"permission\", \"askIndex\": 1, \"askTotal\": 1, \"task\": \"add the idempotency key\","
            + " \"steps\": 3, \"planDone\": 1, \"planTotal\": 4, \"idle\": 4, \"host\": \"studio\", \"instance\": \"you@studio\","
            + " \"trust\": \"own\", \"addr\": \"10.0.0.4\", \"pid\": 4128, \"version\": \"v0.23.0\", \"here\": true}, {\"socket\": \"/demo/design.sock2\","
            + " \"name\": \"palette\", \"role\": \"colour and type\", \"team\": \"frontend\", \"workdir\": \"/Users/you/work/design-system\","
            + " \"session\": \"p1\", \"state\": \"waiting\", \"live\": true, \"asking\": \"which surface should the empty state sit on?\","
            + " \"askId\": \"call_51\", \"askKind\": \"question\", \"askOptions\": [\"surface\", \"surface-container-low\"],"
            + " \"askIndex\": 1, \"askTotal\": 1, \"report\": [{\"key\": \"tried\", \"text\": \"drew it on surface and on surface-container-low,"
            + " both themes; measured 4.7:1 and 6.1:1 against the muted label\"}, {\"key\": \"stakes\", \"text\": \"surface matches the table around it but the empty state stops reading as a panel; the container reads as a panel and is one more layer to keep in step with the cards\"},"
            + " {\"key\": \"lean\", \"text\": \"surface-container-low — the contrast is the one with headroom, and light is already the tighter theme\"}],"
            + " \"task\": \"spec the empty state\", \"steps\": 5, \"idle\": 22, \"host\": \"studio\", \"instance\": \"you@studio\","
            + " \"trust\": \"own\", \"addr\": \"10.0.0.4\", \"pid\": 4131, \"here\": true}, {\"socket\": \"/demo/buttons.sock\","
            + " \"name\": \"buttons\", \"role\": \"components\", \"team\": \"frontend\", \"workdir\": \"/Users/you/work/ui-kit\","
            + " \"session\": \"b1\", \"state\": \"idle\", \"live\": true, \"task\": \"the toggle now reads its state from the store rather than a prop\","
            + " \"idle\": 640, \"host\": \"studio\", \"instance\": \"you@studio\", \"trust\": \"own\", \"addr\": \"10.0.0.4\","
            + " \"pid\": 4129, \"here\": true}, {\"socket\": \"/demo/ops.sock\", \"name\": \"ops\", \"role\": \"deploys and alerting\","
            + " \"workdir\": \"/Users/you/work/infra\", \"session\": \"o1\", \"state\": \"stopped\", \"live\": false, \"task\": \"rotated the staging certificates\","
            + " \"idle\": 90000, \"host\": \"studio\", \"instance\": \"you@studio\", \"trust\": \"own\", \"addr\": \"10.0.0.4\","
            + " \"version\": \"v0.22.0\", \"here\": true}, {\"socket\": \"/demo/tests.sock\", \"name\": \"tests\", \"role\": \"the slow suite,"
            + " off the laptop\", \"team\": \"backend\", \"workdir\": \"/home/you/work/billing\", \"session\": \"t1\", \"state\": \"working\","
            + " \"elsewhere\": true, \"live\": true, \"idle\": 34, \"host\": \"buildbox\", \"instance\": \"you@buildbox\","
            + " \"trust\": \"admitted\", \"addr\": \"10.0.0.21\"}, {\"socket\": \"/demo/risk.sock\", \"name\": \"risk\", \"role\": \"credit models\","
            + " \"team\": \"backend\", \"workdir\": \"/Users/sam/work/risk\", \"session\": \"r1\", \"state\": \"waiting\","
            + " \"elsewhere\": true, \"live\": true, \"idle\": 8, \"host\": \"studio\", \"instance\": \"sam@studio\", \"trust\": \"admitted\","
            + " \"addr\": \"10.0.0.4\"}, {\"socket\": \"/demo/deploy.sock\", \"name\": \"deploy\", \"role\": \"production rollouts\","
            + " \"team\": \"backend\", \"workdir\": \"/home/ops/infra\", \"session\": \"p9\", \"state\": \"idle\", \"elsewhere\": true,"
            + " \"live\": true, \"idle\": 51, \"host\": \"mini\", \"instance\": \"ops@mini\", \"trust\": \"unknown\", \"addr\": \"10.0.0.9\"},"
            + " {\"socket\": \"/demo/docs.sock\", \"name\": \"docs\", \"role\": \"the manual\", \"workdir\": \"/home/you/work/docs\","
            + " \"session\": \"m4\", \"state\": \"remote\", \"elsewhere\": true, \"live\": false, \"idle\": 900, \"host\": \"buildbox\","
            + " \"instance\": \"you@buildbox\", \"trust\": \"admitted\", \"addr\": \"10.0.0.21\"}]";

    /** 전사는 화면이 그릴 줄 아는 것을 한 번씩 보인다: 말·생각·도구(성공과 실패)·디프. */
    private static final String TRANSCRIPT = "[{\"who\": \"user\", \"text\": \"spec the empty state for the fleet table, and name the exact tokens\"},"
            + " {\"who\": \"thinking\", \"text\": \"Three empty states, and the tokens differ between them. Reading each before writing anything down.\"},"
            + " {\"who\": \"assistant\", \"text\": \"Reading what the empty states do today.\"}, {\"who\": \"tool\", \"tool\": \"grep\","
            + " \"args\": \"pattern: empty, path: cmd/magi-web/page.go\", \"ok\": true, \"text\": \"page.go:612  e.innerHTML = 'Nothing learned yet.<br>'\\npage.go:988  empty state for the board\\npage.go:1136 .empty { max-width:52ch }\"},"
            + " {\"who\": \"tool\", \"tool\": \"bash\", \"args\": \"go test ./cmd/magi-web/\", \"ok\": false, \"out\": \"--- FAIL: TestTheEmptyStateNamesItsTokens\\n    page_test.go:88: no token named for the board\"},"
            + " {\"who\": \"tool\", \"tool\": \"edit\", \"ok\": true, \"args\": \"{\\\"path\\\":\\\"src/inbox.tsx\\\",\\\"old\\\":\\\"  color: #8a8a8a;\\\","
            + "\\\"new\\\":\\\"  color: var(--surface-dim);\\\"}\", \"diff\": \"-  color: #8a8a8a;\\n+  color: var(--surface-dim);\","
            + " \"out\": \"\\\"edited src/inbox.tsx\\\"\"}, {\"who\": \"tool\", \"tool\": \"todo_write\", \"ok\": true, \"args\": \"{\\\"todos\\\": [{\\\"content\\\": \\\"read what the empty states do now\\\","
            + " \\\"status\\\": \\\"completed\\\"}, {\\\"content\\\": \\\"write the spec\\\", \\\"status\\\": \\\"completed\\\"}, {\\\"content\\\": \\\"name the tokens it uses\\\","
            + " \\\"status\\\": \\\"in_progress\\\"}, {\\\"content\\\": \\\"get it reviewed by buttons\\\", \\\"status\\\": \\\"pending\\\"}]}\"},"
            + " {\"who\": \"tool\", \"tool\": \"write\", \"args\": \"path: docs/UI.md\", \"pending\": true}, {\"who\": \"assistant\","
            + " \"text\": \"Three of them, and none says what would be there.\\n\\n| where | today | should be |\\n|---|---|---|\\n| fleet | *Nothing learned yet.* | surface-container-low |\\n| board | (blank) | surface |\\n| shared | (blank) | surface |\\n\\nThe rule,"
            + " as one line:\\n\\n```css\\n.empty { background: var(--magi-ref-surfaceContainerLow); max-width: 52ch; }\\n```\\n\\nNote the current markup writes a literal <br> into innerHTML — that is the third defect,"
            + " not a fourth.\"}]";
}
