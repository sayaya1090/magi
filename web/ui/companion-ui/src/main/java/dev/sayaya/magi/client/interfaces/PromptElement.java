package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.AskSharing;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.Motion;
import dev.sayaya.magi.client.usecase.CompanionStore;
import dev.sayaya.magi.client.usecase.FleetCommander;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 이 컴패니언이 무엇에 걸려 있는가 — 제 화면의 도크에, 컴포저 바로 위에.
 *
 * 컴패니언의 페이지가 이것을 볼 수 없는 유일한 자리였다: 질문은 전사에 없다(무엇이
 * 일어났는가의 기록이 아니라 무엇을 해야 하는가에 대한 물음이라서). 그래서 화면에는 그냥
 * 멈춘 실행이 보이고, 답하려면 목록으로 되돌아가야 했다.
 *
 * 어느 층의 것인가: <b>범용</b>이다. 무엇을 물었든 답하는 방식은 타입과 무관하고(퍼미션 넷,
 * 보기 목록, 한 줄 답), 도크는 이미 부모의 것이다. 자식은 이 상자가 있는 줄도 모른다 —
 * 다만 <b>맨 질문</b>일 때는 컴포저가 곧 답이라, 그 사실만 창 브리지로 알린다(AskSharing).
 * 답할 입력이 없는 타입은 그 알림을 무시하면 되고, 그래도 이 상자는 제 일을 한다.
 */
@Singleton
public class PromptElement {
    private final CompanionStore store;
    private final FleetCommander commander;
    private final AnswerBox answers;
    private final HTMLElement box = el("div");
    private boolean wasUp = false;   // 폴마다 다시 그려도 등장은 한 번이다
    private String socket = null;

    @Inject
    public PromptElement(CompanionStore store, FleetCommander commander, AnswerBox answers) {
        this.store = store;
        this.commander = commander;
        this.answers = answers;
        box.id = "prompt";
        box.setAttribute("hidden", "");
    }

    public HTMLElement element() { return box; }

    /** 명단이 흐르는 동안 계속 불린다 — 기다리는 그 컴패니언이면 상자를 세운다. */
    public void wire() {
        store.onContext(ctx -> { socket = ctx == null ? null : ctx.socket; render(null); });
        store.onRoster(list -> render(list));
    }

    private void render(Object list) {
        FleetAgent a = rowOf(list);
        if (a == null || !"waiting".equals(a.state)) {
            box.setAttribute("hidden", "");
            box.replaceChildren();
            wasUp = false;
            AskSharing.publish(null);
            return;
        }
        HTMLElement inner = el("div");
        inner.className = "inner";
        HTMLElement k = el("div");
        k.className = "asking";
        // 몇 개 중 몇 번째인지는 둘 이상일 때만 — "1 of 1"은 아무도 묻지 않은 것에 답하면서,
        // 진짜가 나타날 자리를 눈이 건너뛰게 만든다(운영의 그 판단).
        k.textContent = "⏸ " + str(a.asking)
                + (a.askTotal > 1 ? "  " + tr("ask.of", "i", String.valueOf(a.askIndex),
                                              "n", String.valueOf(a.askTotal)) : "");
        inner.append(k);
        HTMLElement why = grounds(a);
        if (why != null) inner.append(why);
        // 맨 질문은 컴포저가 답한다 — 글 상자 둘을 위아래로 세우지 않는다(운영의 그 규칙:
        // 위는 질문에 답하고 아래는 듣지 않는 에이전트에게 새 부탁을 보내는데 무엇이 무엇인지
        // 말해 주는 표가 없었다). 보기 목록이 딸렸으면 그 목록은 그린다: 화면 밖으로 스크롤된
        // 산문에서 보기를 외워 그대로 타이핑하게 두는 것이 그 규칙의 대가였다.
        boolean plainQuestion = "question".equals(a.askKind)
                && (a.askOptions == null || a.askOptions.length == 0);
        if (!plainQuestion) inner.append(answers.of(a, text -> answer(a, text), true));
        box.replaceChildren(inner);
        box.removeAttribute("hidden");
        if (!wasUp) Motion.play(box, Motion.RISE);
        wasUp = true;
        // 컴포저가 지금 무엇을 하는 자리인지 — 답할 곳이 있는 자식만 이 사실을 쓴다.
        AskSharing.publish(plainQuestion ? AskSharing.ask(a.askId, a.askKind, a.socket, a.peer) : null);
    }

    private void answer(FleetAgent a, String text) {
        commander.answer(a, text, () -> { });
    }

    /** 근거 — 있으면 접힌 폭에 맞춰 나란히, 없으면 없다(운영 grounds). */
    private HTMLElement grounds(FleetAgent a) {
        if (a.report == null || a.report.length == 0) return null;
        HTMLElement box2 = el("div");
        box2.className = "grounds span";
        for (FleetAgent.ReportSection sec : a.report) {
            if (sec == null || sec.text == null || sec.text.isEmpty()) continue;
            HTMLElement s = el("div");
            s.className = "gsec";
            HTMLElement key = el("div");
            key.className = "gk";
            key.textContent = str(sec.key);
            HTMLElement val = el("div");
            val.className = "gv";
            val.textContent = sec.text;
            s.append(key, val);
            box2.append(s);
        }
        return box2.childElementCount > 0 ? box2 : null;
    }

    private FleetAgent rowOf(Object list) {
        if (list == null || socket == null) return null;
        JsArrayLike<Object> rows = Js.uncheckedCast(list);
        for (int i = 0; i < rows.getLength(); i++) {
            FleetAgent r = Js.uncheckedCast(rows.getAt(i));
            if (socket.equals(r.socket)) return r;
        }
        return null;
    }

    private static String str(String s) { return s == null ? "" : s; }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
