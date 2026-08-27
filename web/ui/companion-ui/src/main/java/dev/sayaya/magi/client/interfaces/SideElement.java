package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.client.usecase.CompanionStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 오른쪽 판 — 범용이다: 어떤 타입의 컴패니언이든 "무엇을 하기로 했나"는 같은 물음이다.
 *
 * 지금은 계획 하나다(운영 drawPlan의 이식): 진행은 막대가 아니라 개수로도 말하고 —
 * 투두는 일정표가 아니다 — 항목은 그 자신의 표식(끝남·하는 중·아직)을 단다. 건넨 일과
 * 예약은 잔여이고, 그때도 이 판의 것이다.
 */
@Singleton
public class SideElement {
    private final CompanionStore store;
    private final HTMLElement side = el("aside");
    private final HTMLElement plan = el("md-outlined-card");

    @Inject
    public SideElement(CompanionStore store) {
        this.store = store;
        side.id = "side";
        plan.id = "plan";
        plan.setAttribute("hidden", "");
        side.append(plan);
        store.onPlan(this::paint);
    }

    public HTMLElement element() { return side; }

    private void paint(Object todosOrNull) {
        JsArrayLike<Object> todos = Js.uncheckedCast(todosOrNull);
        if (todos == null || todos.getLength() == 0) {
            plan.setAttribute("hidden", "");
            plan.replaceChildren();
            return;
        }
        int done = 0;
        for (int i = 0; i < todos.getLength(); i++) {
            if ("completed".equals(str(Js.uncheckedCast(todos.getAt(i)), "status"))) done++;
        }
        plan.replaceChildren();
        HTMLElement head = el("h3");
        head.className = "sidehead";
        head.textContent = tr("field.plan");
        // 아는 개수라 결정적 막대다 — 밑에 목록이 보이는 사람에게 "뭔가 되고 있다"는 말은
        // 아무것도 아니다(운영 규칙).
        HTMLElement bar = el("md-linear-progress");
        Js.asPropertyMap(bar).set("value", done / (double) todos.getLength());
        bar.className = "planbar";
        bar.setAttribute("aria-label", tr("plan.progress", "done", String.valueOf(done),
                "total", String.valueOf(todos.getLength())));
        plan.append(head, bar, cell("plancount",
                tr("plan.progress", "done", String.valueOf(done), "total", String.valueOf(todos.getLength()))));
        for (int i = 0; i < todos.getLength(); i++) {
            JsPropertyMap<Object> t = Js.uncheckedCast(todos.getAt(i));
            String status = str(t, "status");
            HTMLElement row = cell("td " + status, null);
            HTMLElement mark = el("span");
            mark.className = "mk";
            mark.setAttribute("aria-hidden", "true");
            mark.textContent = "completed".equals(status) ? "\u2713"
                    : "in_progress".equals(status) ? "\u25B8" : "\u00B7";
            row.append(mark, cell("tdtext", str(t, "content")));
            plan.append(row);
        }
        plan.removeAttribute("hidden");
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement cell(String cls) { return cell(cls, null); }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
