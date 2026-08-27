package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.FleetAgent;
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
    // 운영의 그 다섯 장 그대로, 그 순서로: 계획이 먼저다(이 컴패니언이 하려는 일이고 가만히
    // 있다), 그 아래가 지금 몇 초 사이에 일어나는 일이다 — 뒤집으면 주어 없는 세부가 된다.
    private final HTMLElement strip = el("md-outlined-card");
    private final HTMLElement handoffs = el("md-outlined-card");
    private final HTMLElement queued = el("md-outlined-card");
    private final HTMLElement cron = el("md-outlined-card");
    private final HTMLElement empty = el("div");
    private String lastJobs = "", lastHands = "", lastCron = "";

    @Inject
    public SideElement(CompanionStore store) {
        this.store = store;
        side.id = "side";
        plan.id = "plan";
        plan.setAttribute("hidden", "");
        for (HTMLElement card : new HTMLElement[]{strip, handoffs, queued, cron}) {
            card.setAttribute("hidden", "");
        }
        strip.id = "strip";
        handoffs.id = "handoffs";
        queued.id = "queued";
        cron.id = "cron";
        // 이 판이 통째로 비었을 때 하는 말 — 빈 기둥은 "아직 안 왔다"로 읽힌다.
        empty.className = "empty";
        side.append(plan, strip, handoffs, queued, cron, empty);
        store.onPlan(this::paint);
        // 명단이 흐를 때마다 다시 묻는다 — 이 셋은 로그에 없어서 흐름에 실려 오지 않는다.
        store.onRoster(list -> {
            rosterList = list;
            // 이름이 갈 수 있는 곳인지는 명단이 답한다 — 명단이 늦게 오면 그 답도 늦게 온다.
            lastHands = "";
            refresh();
        });
        store.onContext(c -> refresh());
    }

    public HTMLElement element() { return side; }

    /** 로그 밖의 사실들을 다시 읽는다 — 답이 같으면 다시 그리지 않는다(열린 메뉴·스크롤 보호). */
    private Object rosterList = null;

    private void refresh() {
        if (store.context() == null) {
            for (HTMLElement card : new HTMLElement[]{strip, handoffs, queued, cron}) {
                card.setAttribute("hidden", "");
                card.replaceChildren();
            }
            lastJobs = lastHands = lastCron = "";
            sayEmpty();
            return;
        }
        store.jobs(got -> {
            String sig = got == null ? "" : elemental2.core.Global.JSON.stringify(got);
            if (sig.equals(lastJobs)) return;
            lastJobs = sig;
            JsPropertyMap<Object> j = got == null ? null : Js.uncheckedCast(got);
            drawRunning(j == null ? null : Js.uncheckedCast(j.get("children")),
                    j == null ? null : Js.uncheckedCast(j.get("background")));
            drawQueued(j == null ? null : Js.uncheckedCast(j.get("queued")));
            sayEmpty();
        });
        store.handoffs(got -> {
            String sig = got == null ? "" : elemental2.core.Global.JSON.stringify(got);
            if (sig.equals(lastHands)) return;
            lastHands = sig;
            drawHandoffs(Js.uncheckedCast(got));
            sayEmpty();
        });
        store.cron(got -> {
            String sig = got == null ? "" : elemental2.core.Global.JSON.stringify(got);
            if (sig.equals(lastCron)) return;
            lastCron = sig;
            drawCron(Js.uncheckedCast(got));
            sayEmpty();
        });
    }

    /**
     * 지금 도는 것 — 스폰된 자식과 뒤로 돌린 명령. 끝난 것은 여기 남지 않는다: 그러면 이것은
     * 무엇이 일어나는지가 아니라 <b>흉터</b>가 된다(운영의 그 규칙). 실패는 몇 분 남는다.
     */
    private void drawRunning(JsArrayLike<Object> children, JsArrayLike<Object> background) {
        HTMLElement box = cell("stripjobs", null);
        int n = 0;
        for (int i = 0; children != null && i < children.getLength(); i++) {
            JsPropertyMap<Object> c = Js.uncheckedCast(children.getAt(i));
            boolean running = Js.isTruthy(c.get("running"));
            boolean bad = Js.isTruthy(c.get("err"));
            if (!running && !bad) continue;
            // 자식으로 들어가는 문은 아직 없다(그 화면은 잔여) — 문이 생기기 전까지는 사실로 둔다.
            box.append(chip(tr("detail.subagent"), str(c, "tool").isEmpty() ? tr("detail.subagent") : str(c, "tool"),
                    oneLine(str(c, "task"), 48), running, bad, null));
            n++;
        }
        for (int i = 0; background != null && i < background.getLength(); i++) {
            JsPropertyMap<Object> b = Js.uncheckedCast(background.getAt(i));
            boolean running = Js.isTruthy(b.get("running"));
            double exit = b.get("exit") == null ? 0 : Js.coerceToDouble(b.get("exit"));
            box.append(chip(tr("job.command"), oneLine(str(b, "command"), 40), lastLine(str(b, "tail")),
                    running, !running && exit != 0, null));
            n++;
        }
        if (n == 0) { strip.setAttribute("hidden", ""); strip.replaceChildren(); return; }
        strip.replaceChildren(head(tr("field.running")), box);
        strip.removeAttribute("hidden");
    }

    /** 줄 서 있는 말 — 몇 번째인지까지: "기다림"과 "셋 뒤에서 기다림"은 다른 사실이다. */
    private void drawQueued(JsArrayLike<Object> items) {
        if (items == null || items.getLength() == 0) {
            queued.setAttribute("hidden", "");
            queued.replaceChildren();
            return;
        }
        queued.replaceChildren(head(tr("field.queued")));
        for (int i = 0; i < items.getLength(); i++) {
            JsPropertyMap<Object> q = Js.uncheckedCast(items.getAt(i));
            boolean mine = "person".equals(str(q, "kind"));
            HTMLElement row = cell("qrow" + (mine ? " mine" : ""), null);
            row.append(cell("qn", String.valueOf(i + 1)));
            HTMLElement what = cell("qwhat", null);
            what.append(cell("qwho", mine ? tr("queued.you")
                    : str(q, "from").isEmpty() ? tr("queued.handed") : str(q, "from")));
            what.append(cell("qsaid", oneLine(str(q, "text"), 120)));
            row.append(what);
            queued.append(row);
        }
        queued.removeAttribute("hidden");
    }

    /** 남에게 건넨 일 — 그 일이 어떻게 되고 있는지. 청한 말은 눌러서 펼친다(목록 규칙). */
    private void drawHandoffs(JsArrayLike<Object> list) {
        if (list == null || list.getLength() == 0) {
            handoffs.setAttribute("hidden", "");
            handoffs.replaceChildren();
            return;
        }
        handoffs.replaceChildren(head(tr("field.handed_out")));
        for (int i = 0; i < list.getLength(); i++) {
            JsPropertyMap<Object> h = Js.uncheckedCast(list.getAt(i));
            HTMLElement row = cell("ho " + str(h, "state"), null);
            // 이름은 그 컴패니언으로 가는 길이다 — 건넨 일은 이 화면이 <b>화면에 없는 누군가</b>를
            // 이야기하는 유일한 자리이고, "그래서 그게 어떻게 되고 있나"의 답은 그쪽 화면에 있다.
            // 이 콘솔이 그 이름을 아는 경우에만: 아무 데도 안 가는 링크는 맨 글자보다 나쁘다.
            row.append(wayTo(str(h, "to")));
            HTMLElement req = cell("req", str(h, "request"));
            req.setAttribute("role", "button");
            req.setAttribute("tabindex", "0");
            req.setAttribute("aria-expanded", "false");
            req.addEventListener("click", evt -> {
                boolean open = req.classList.toggle("all");
                req.setAttribute("aria-expanded", String.valueOf(open));
            });
            row.append(req);
            handoffs.append(row);
        }
        handoffs.removeAttribute("hidden");
    }

    /** 건네받은 쪽으로 가는 길 — 명단에 그 이름이 있을 때만 링크가 된다. */
    private HTMLElement wayTo(String name) {
        FleetAgent peer = null;
        JsArrayLike<Object> all = Js.uncheckedCast(rosterList);
        for (int i = 0; all != null && i < all.getLength(); i++) {
            FleetAgent one = Js.uncheckedCast(all.getAt(i));
            if (name.equals(one.name) && one.socket != null && !one.socket.isEmpty()) { peer = one; break; }
        }
        if (peer == null) return cell("to", name);
        final FleetAgent go = peer;
        HTMLElement a = el("a");
        a.className = "to";
        a.textContent = name;
        a.setAttribute("href", dev.sayaya.magi.bridge.Windows.here() + "?d="
                + elemental2.core.Global.encodeURIComponent(go.socket)
                + (go.peer == null || go.peer.isEmpty() ? ""
                   : "&p=" + elemental2.core.Global.encodeURIComponent(go.peer)));
        a.addEventListener("click", evt -> {
            evt.preventDefault();
            dev.sayaya.magi.bridge.GoSharing.go(go.socket, go.peer == null || go.peer.isEmpty() ? null : go.peer);
        });
        return a;
    }

    /** 예약된 일 — 언제 다시 도는가, 또는 <b>왜 영영 안 도는가</b>(그 표시가 이 목록의 값이다). */
    private void drawCron(JsArrayLike<Object> list) {
        if (list == null || list.getLength() == 0) {
            cron.setAttribute("hidden", "");
            cron.replaceChildren();
            return;
        }
        cron.replaceChildren(head(tr("field.scheduled")));
        for (int i = 0; i < list.getLength(); i++) {
            JsPropertyMap<Object> j = Js.uncheckedCast(list.getAt(i));
            boolean on = Js.isTruthy(j.get("enabled"));
            String problem = str(j, "problem");
            HTMLElement row = cell("job" + (on ? "" : " off") + (problem.isEmpty() ? "" : " broken"), null);
            row.append(cell("jname", str(j, "name")), cell("jwhen", str(j, "schedule")));
            String state;
            if (!problem.isEmpty()) state = tr("cron.never") + " — " + problem;
            else if (!on) state = tr("cron.off");
            else if (!str(j, "next").isEmpty()) state = tr("cron.next") + " " + str(j, "next");
            else state = tr("cron.never");
            row.append(cell("jnext", state), cell("jask", str(j, "prompt")));
            row.append(cell("jfile", str(j, "file")
                    + (Js.isTruthy(j.get("global")) ? " \u00B7 " + tr("cron.machine") : "")));
            cron.append(row);
        }
        cron.removeAttribute("hidden");
    }

    /** 다섯 장이 모두 비면 그렇다고 말한다 — 빈 기둥은 아직 안 온 화면처럼 읽힌다. */
    /** 판의 속이 바뀌었다고 알리는 문 — 손잡이는 부모의 것이고, 그 말은 이 판의 사실에서 온다. */
    public interface Changed { void call(); }

    private Changed changed = () -> { };

    public void onChanged(Changed c) { this.changed = c; }

    private void sayEmpty() {
        boolean any = false;
        for (HTMLElement card : new HTMLElement[]{plan, strip, handoffs, queued, cron}) {
            if (!card.hasAttribute("hidden")) any = true;
        }
        if (any) {
            empty.setAttribute("hidden", "");
            empty.replaceChildren();
        } else {
            empty.innerHTML = tr("going_on.none") + "<br>" + tr("going_on.none_how");
            empty.removeAttribute("hidden");
        }
        changed.call();
    }

    private HTMLElement head(String word) {
        HTMLElement h = el("h3");
        h.className = "sidehead";
        h.textContent = word;
        return h;
    }

    /** 작은 알갱이 하나 — 자식은 들어가는 문이고 명령은 사실이다(그래서 하나만 누를 수 있다). */
    private HTMLElement chip(String kind, String name, String say, boolean running, boolean bad, Runnable go) {
        // 운영의 그 마크업(.job/.jdot/.jname/.jsay): 도는 것은 점 하나를 앞에 달고, 이름은 28자,
        // 하는 말은 44자에서 끊는다 — 이 판은 좁고, 여기 오는 것은 요약이지 로그가 아니다.
        // 자식은 들어가는 문이고 배경 명령은 사실이다 — 그래서 하나만 누를 수 있다: 눌리는
        // 것처럼 보이는데 아무 일도 안 하는 것은 맨 글자보다 나쁘다.
        HTMLElement c = el(go == null ? "div" : "button");
        c.className = "job" + (go == null ? "" : " press") + (running ? " live" : " done") + (bad ? " bad" : "");
        if (go == null) {
            if (running) c.append(cell("jdot", ""));
            c.append(cell("jname", oneLine(name, 28)));
            if (!say.isEmpty()) c.append(cell("jsay", oneLine(say, 44)));
        } else {
            // 버튼 안에서는 한 줄로 — 세 조각을 넣으면 컴포넌트가 그것을 쌓아 긴 것이 알갱이
            // 밖으로 흘러나간다(운영이 실측한 그 결함).
            c.setAttribute("type", "button");
            c.textContent = oneLine(name, 28) + (say.isEmpty() ? "" : " \u00B7 " + oneLine(say, 44));
            c.addEventListener("click", evt -> go.run());
        }
        c.setAttribute("aria-label", kind + ": " + name + (say.isEmpty() ? "" : " \u2014 " + say));
        return c;
    }

    private static String oneLine(String s, int n) {
        String t = s == null ? "" : s.replaceAll("\\s+", " ").trim();
        return t.length() > n ? t.substring(0, n) + "\u2026" : t;
    }

    private static String lastLine(String tail) {
        if (tail == null || tail.isEmpty()) return "";
        String[] all = tail.split("\n");
        for (int i = all.length - 1; i >= 0; i--) if (!all[i].trim().isEmpty()) return oneLine(all[i], 48);
        return "";
    }

    private void paint(Object todosOrNull) {
        JsArrayLike<Object> todos = Js.uncheckedCast(todosOrNull);
        if (todos == null || todos.getLength() == 0) {
            plan.setAttribute("hidden", "");
            plan.replaceChildren();
            sayEmpty();
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
