package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.client.domain.Lanes;
import dev.sayaya.magi.component.Rank;
import dev.sayaya.magi.client.usecase.BoardStore;
import elemental2.core.JsDate;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import elemental2.dom.WheelEvent;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 보드 — 운영 loadBoard의 이식: 하루의 일을 팀 레인으로. 머리는 날짜 필드+화살표+오늘+찾기,
 * 레인은 골칫거리 먼저, 카드는 시각·(여럿이면) 누가·제목(그 대화로 가는 길)·소요·모델·
 * 라벨 칩(누르면 그 말로 좁힌다). 마크업 클래스(.boardhead/.lanes/.lane/.lanehead/.wcard…)는
 * 운영 그대로 — console.css가 입힌다.
 *
 * 카드의 &past= 주소는 컴패니언의 이력 층위가 받는다 — 그 세션의 전사로 곧장.
 */
@Singleton
public class BoardElement {
    private final BoardStore store;
    private final HTMLElement root = el("div");
    private final HTMLElement head = el("div");
    private final HTMLElement day = el("md-outlined-text-field");
    private final HTMLElement prev = el("md-icon-button");
    private final HTMLElement fwd = el("md-icon-button");
    private final HTMLElement today = el("md-text-button");
    private final HTMLElement find = el("md-outlined-text-field");
    private final HTMLElement body = el("div");
    private boolean wired = false;

    @Inject
    public BoardElement(BoardStore store) {
        this.store = store;
        root.id = "board";
        head.className = "boardhead";
        day.setAttribute("type", "date");
        day.setAttribute("label", tr("board.day"));
        day.addEventListener("change", evt -> store.day(value(day)));
        arrow(prev, true, "board.prev");
        prev.addEventListener("click", evt -> step(-1));
        arrow(fwd, false, "board.next");
        fwd.addEventListener("click", evt -> step(1));
        today.textContent = tr("board.today");
        today.addEventListener("click", evt -> store.day(todayISO()));
        find.setAttribute("label", tr("label.find"));
        Icons.glass(find);
        find.addEventListener("input", evt -> store.query(value(find)));
        head.append(prev, day, fwd, today, find);
        // 그림판은 셸이 늦게 가져온다 — 화살은 한 번만 그려지므로 그때 다시 입힌다.
        Icons.onReady(() -> { Icons.dress(prev); Icons.dress(fwd); });
        root.append(head, body);
    }

    public void mount(HTMLElement frame) {
        frame.replaceChildren(root);
        if (wired) return;
        wired = true;
        store.subscribe(this::render);
        store.start(todayISO());
    }

    private void step(int delta) {
        // UTC 통날 산술 — 서머타임 경계에서 하루를 건너뛰지 않는다(운영 규칙).
        JsDate d = new JsDate(store.day() + "T00:00:00Z");
        d.setUTCDate(d.getUTCDate() + delta);
        store.day(d.toISOString().substring(0, 10));
    }

    private void render() {
        if (!store.fleetAnswered()) return;
        set(day, store.day());
        boolean atToday = store.day().compareTo(todayISO()) >= 0;
        if (atToday) { fwd.setAttribute("disabled", ""); today.setAttribute("disabled", ""); }
        else { fwd.removeAttribute("disabled"); today.removeAttribute("disabled"); }

        JsArrayLike<Object> list = Js.uncheckedCast(store.fleet());
        if (list == null) { body.replaceChildren(empty("error.pane", "error.pane_how")); return; }

        // 플릿과 같은 순서로 걷고, 각 컴패니언의 지난 일을 청한다 — 답이 오는 대로 다시 그린다.
        List<JsPropertyMap<Object>> cols = new ArrayList<>();
        for (int i = 0; i < list.getLength(); i++) cols.add(Js.uncheckedCast(list.getAt(i)));
        cols.sort((x, y) -> {
            int d = Lanes.rank(str(x, "state")) - Lanes.rank(str(y, "state"));
            return d != 0 ? d : (int) (num(x, "idle") - num(y, "idle"));
        });
        for (JsPropertyMap<Object> a : cols) store.wantHistory(str(a, "socket"), nul(str(a, "peer")));

        // 레인은 팀 — 무팀은 제 이름으로, 팀을 지어내지 않는다.
        Map<String, List<JsPropertyMap<Object>>> byLane = new LinkedHashMap<>();
        Map<String, List<JsPropertyMap<Object>>> workOf = new LinkedHashMap<>();
        for (JsPropertyMap<Object> a : cols) {
            String lane = Lanes.laneOf(str(a, "team"), str(a, "name"));
            byLane.computeIfAbsent(lane, k -> new ArrayList<>()).add(a);
        }
        HTMLElement lanes = el("div");
        lanes.className = "lanes";
        // 세로 휠로도 가로 스트립이 움직인다 — 평범한 마우스가 제일 흔한 포인터다(운영 규칙).
        lanes.addEventListener("wheel", evt -> {
            WheelEvent we = Js.uncheckedCast(evt);
            if (we.deltaX != 0 || we.deltaY == 0) return;
            if (lanes.scrollWidth <= lanes.clientWidth) return;
            lanes.scrollLeft += we.deltaY;
            evt.preventDefault();
        });
        boolean anything = false;
        for (Map.Entry<String, List<JsPropertyMap<Object>>> e : byLane.entrySet()) {
            List<JsPropertyMap<Object>> work = new ArrayList<>();
            for (JsPropertyMap<Object> who : e.getValue()) {
                JsArrayLike<Object> runs = Js.uncheckedCast(store.historyOf(str(who, "socket"), nul(str(who, "peer"))));
                if (runs == null) continue;
                for (int i = 0; i < runs.getLength(); i++) {
                    JsPropertyMap<Object> h = Js.uncheckedCast(runs.getAt(i));
                    if (Lanes.onDay(dayOf(str(h, "started")), dayOf(str(h, "ended")), store.day())) {
                        JsPropertyMap<Object> card = Js.uncheckedCast(JsPropertyMap.of());
                        card.set("h", h);
                        card.set("who", who);
                        work.add(card);
                    }
                }
            }
            work.sort((x, y) -> str(Js.uncheckedCast(y.get("h")), "started")
                    .compareTo(str(Js.uncheckedCast(x.get("h")), "started")));
            if (!store.query().trim().isEmpty()) {
                List<String> docs = new ArrayList<>();
                for (JsPropertyMap<Object> w : work) {
                    JsPropertyMap<Object> h = Js.uncheckedCast(w.get("h"));
                    StringBuilder b = new StringBuilder(str(h, "title")).append(' ').append(str(h, "model"));
                    JsArrayLike<Object> ls = Js.uncheckedCast(h.get("labels"));
                    if (ls != null) for (int i = 0; i < ls.getLength(); i++) b.append(' ').append(ls.getAt(i));
                    docs.add(b.toString());
                }
                int[] order = Rank.order(store.query(), docs);
                List<JsPropertyMap<Object>> ranked = new ArrayList<>();
                for (int i : order) ranked.add(work.get(i));
                work = ranked;
            }
            if (work.isEmpty()) continue;
            anything = true;
            lanes.append(lane(e.getKey(), e.getValue().size() > 1, work));
        }
        body.replaceChildren(anything ? lanes : empty("board.nothing", "board.nothing_how"));
    }

    private HTMLElement lane(String key, boolean crowded, List<JsPropertyMap<Object>> work) {
        HTMLElement lane = el("div");
        lane.className = "lane";
        HTMLElement title = el("h3");
        title.className = "lanehead";
        title.append(cell("lname", key), cell("lcount", String.valueOf(work.size())));
        lane.append(title);
        for (JsPropertyMap<Object> w : work) {
            JsPropertyMap<Object> h = Js.uncheckedCast(w.get("h"));
            JsPropertyMap<Object> who = Js.uncheckedCast(w.get("who"));
            boolean current = Js.isTruthy(h.get("current"));
            HTMLElement card = cell("wcard" + (current ? " now" : ""), null);
            HTMLElement when = cell("wwhen", current ? tr("board.now") : hhmm(str(h, "started")));
            // 누가 했나 — 레인에 여럿이거나 레인 이름과 다를 때만(운영 규칙).
            if (crowded || !str(who, "name").equals(key)) when.append(cell("wwho", str(who, "name")));
            card.append(when);
            // 제목이 길이다 — 그 대화로: 주소도 클릭도 그 세션(이력 층위)에 닿는다.
            HTMLElement what = el("a");
            what.className = "wwhat";
            String href = Windows.here() + "?d=" + str(who, "socket")
                    + (str(who, "peer").isEmpty() ? "" : "&p=" + str(who, "peer"))
                    + (str(h, "id").isEmpty() ? "" : "&past=" + str(h, "id"));
            what.setAttribute("href", href);
            String title2 = str(h, "title");
            what.textContent = title2.isEmpty() ? tr("history.untitled") : title2;
            final String pastId = str(h, "id");
            what.addEventListener("click", evt -> {
                evt.preventDefault();
                GoSharing.go(str(who, "socket"), nul(str(who, "peer")));
                if (!pastId.isEmpty()) GoSharing.past(pastId);
            });
            card.append(what);
            if (!current && !str(h, "started").isEmpty() && !str(h, "ended").isEmpty()) {
                double mins = Math.round((JsDate.parse(str(h, "ended")) - JsDate.parse(str(h, "started"))) / 60000d);
                if (mins > 0) card.append(cell("wlong", dur((int) (mins * 60))));
            }
            if (!str(h, "model").isEmpty()) card.append(cell("wmodel", str(h, "model")));
            JsArrayLike<Object> labels = Js.uncheckedCast(h.get("labels"));
            if (labels != null) {
                for (int i = 0; i < labels.getLength(); i++) {
                    String l = String.valueOf(labels.getAt(i));
                    HTMLElement chip = el("button");
                    chip.setAttribute("type", "button");
                    chip.className = "wlabel hit48";
                    chip.textContent = l;
                    // 라벨은 눌러서 찾는 것이다 — 같은 말을 단 두 번째 일이 찾던 그것이라서.
                    chip.addEventListener("click", evt -> { set(find, l); store.query(l); });
                    card.append(chip);
                }
            }
            lane.append(card);
        }
        return lane;
    }

    // ── 잔손 ─────────────────────────────────────────────────────────────────

    private static void arrow(HTMLElement b, boolean left, String key) {
        b.setAttribute("aria-label", tr(key));
        // 제 도형을 그려 두고 그림판이 있으면 갈아입힌다(운영의 그 순서: data-i + dressIcons) —
        // 없는 빌드에서는 이 화살이 그대로 남는다.
        b.innerHTML = "<svg data-i=\"" + (left ? "#i-sl-chevron-left" : "#i-sl-chevron-right")
                + "\" viewBox=\"0 0 24 24\" width=\"20\" height=\"20\" aria-hidden=\"true\"><path d=\""
                + (left ? "M14.5 5.5 8 12l6.5 6.5" : "M9.5 5.5 16 12l-6.5 6.5")
                + "\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.8\" stroke-linecap=\"round\" stroke-linejoin=\"round\"/></svg>";
        Icons.dress(b);
    }

    private static String todayISO() {
        JsDate now = new JsDate();
        return new JsDate(JsDate.now() - now.getTimezoneOffset() * 60000d).toISOString().substring(0, 10);
    }

    private static String dayOf(String ts) {
        double t = JsDate.parse(ts == null ? "" : ts);
        if (Double.isNaN(t)) return "";
        return new JsDate(t - new JsDate(t).getTimezoneOffset() * 60000d).toISOString().substring(0, 10);
    }

    private static String hhmm(String ts) {
        double t = JsDate.parse(ts == null ? "" : ts);
        if (Double.isNaN(t)) return "";
        return new JsDate(t - new JsDate(t).getTimezoneOffset() * 60000d).toISOString().substring(11, 16);
    }

    /** s/m/h/d — 운영 dur()와 같은 축약. */
    private static String dur(int s) {
        if (s < 60) return s + "s";
        if (s < 3600) return Math.round(s / 60f) + "m";
        if (s < 86400) return Math.round(s / 3600f) + "h";
        return Math.round(s / 86400f) + "d";
    }

    private static HTMLElement empty(String whatKey, String howKey) {
        HTMLElement e = el("div");
        e.className = "empty";
        e.innerHTML = tr(whatKey) + "<br>" + tr(howKey);
        return e;
    }

    private static String value(HTMLElement f) {
        Object v = Js.asPropertyMap(f).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static void set(HTMLElement f, String v) { Js.asPropertyMap(f).set("value", v); }

    private static String str(JsPropertyMap<Object> r, String k) {
        Object v = r.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static double num(JsPropertyMap<Object> r, String k) {
        Object v = r.get(k);
        return v == null ? 0 : Js.coerceToDouble(v);
    }

    private static String nul(String s) { return s == null || s.isEmpty() ? null : s; }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
