package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.bridge.AgentStates;
import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.StateMark;
import dev.sayaya.magi.client.domain.Atlas;
import dev.sayaya.magi.client.usecase.MapStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import elemental2.dom.ResizeObserver;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import static dev.sayaya.magi.bridge.Labels.stateWord;
import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 맵 — 운영 loadMap의 이식: 두 경계(머신 상자, 그 안의 계정 상자)와 팀 머리, 노드,
 * 그리고 오간 것의 와이어(측정 기반 — 상자는 흐름 속에 있고 좌표는 브라우저만 안다).
 * 같은 상자 안은 곡선, 상자 사이는 모두의 밑 레인으로 돌아간다 — 상자를 뚫는 선은
 * 어느 쌍의 것인지 말하지 못한다(운영 규칙). 팀은 선이 아니다: 허브는 주소 관례지
 * 라우터가 아니라서, 머리로만 말한다.
 */
@Singleton
public class MapElement {
    private final MapStore store;
    private final HTMLElement root = el("div");
    private boolean wired = false;
    private ResizeObserver watch = null;
    // 선을 마지막으로 그린 상자 크기. 관찰자가 제 그림 때문에 다시 불리는 것을 여기서 끊는다.
    private String drawnAt = "";

    @Inject
    public MapElement(MapStore store) {
        this.store = store;
        root.id = "map";
    }

    public void mount(HTMLElement frame) {
        frame.replaceChildren(root);
        if (wired) return;
        wired = true;
        // 말이 바뀌면 이 판도 다시 칠한다 — 언어를 간 사람이 화면을 옮겨 다니며 옛말을
        // 만나지 않게(운영 labels$의 그 구독).
        dev.sayaya.magi.bridge.Labels.onPack(this::render);
        // 명단 전체가 아니라 <b>이 지도가 그리는 것</b>만 듣는다 — 걸음 수가 늘었다고 지도가
        // 다시 설 이유는 없다(실측: 10초에 70번).
        store.drawn().subscribe(sig -> render());
        // 상태는 판을 다시 세우지 않고 <b>서 있는 노드만</b> 고쳐 입힌다. 지도에서 가장 자주
        // 달라지는 것이 상태인데, 그때마다 통째로 다시 세우면 그 노드에 포커스를 두고 있던
        // 사람이 body로 떨어지고(키보드로 지도를 걷던 중이다) 선을 재는 일까지 다시 한다.
        store.lit().subscribe(sig -> paintStates());
        // 쉰 시간은 판을 다시 세우지 않고 <b>서 있는 줄의 낱말만</b> 고쳐 쓴다. 갓 쉰 노드의 그
        // 낱말은 초 단위라 매 초 달라지는데, 그때마다 판을 다시 세우면 그 사이의 클릭이 사라지고
        // 스크롤이 튄다(실측: 10초에 70번 다시 섰다).
        store.ticked().subscribe(sig -> paintAges());
        store.start();
    }

    private void render() {
        if (!store.answered()) return;
        JsArrayLike<Object> rows = Js.uncheckedCast(store.fleet());
        JsArrayLike<Object> hands = Js.uncheckedCast(store.handoffs());
        HTMLElement head = el("h2");
        head.className = "sectionhead";
        HTMLElement word = el("span");
        word.textContent = tr("nav.map");
        head.append(word);
        head.setAttribute("aria-label", tr("nav.map"));
        // 표로 돌아가는 길 — 두 목적지가 아니라 한 목적지의 두 시선(운영 toTable).
        HTMLElement back = el("md-text-button");
        back.className = "astable";
        Icons.say(back, tr("map.as_table"), "#i-sl-layer-group");
        back.addEventListener("click", evt -> GoSharing.view("fleet"));
        head.append(back);
        if (rows == null || hands == null) {
            root.replaceChildren(head, empty("error.pane", "error.pane_how"));
            return;
        }
        if (rows.getLength() == 0) {
            root.replaceChildren(head, empty("map.empty", "map.empty_how"));
            return;
        }
        // 머신 → 계정 → 행들. 피어는 그 콘솔 이름이 곧 머신이다(운영 규칙).
        Map<String, Map<String, List<JsPropertyMap<Object>>>> machines = new LinkedHashMap<>();
        for (int i = 0; i < rows.getLength(); i++) {
            JsPropertyMap<Object> a = Js.uncheckedCast(rows.getAt(i));
            String host = hostOf(a);
            String who = Atlas.accountOf(str(a, "instance"));
            if (who.isEmpty()) who = tr("map.here");
            machines.computeIfAbsent(host, k -> new LinkedHashMap<>())
                    .computeIfAbsent(who, k -> new ArrayList<>()).add(a);
        }
        List<Map.Entry<String, Map<String, List<JsPropertyMap<Object>>>>> placed =
                new ArrayList<>(machines.entrySet());
        placed.sort((x, y) -> {
            int rx = bestRank(x.getValue()), ry = bestRank(y.getValue());
            return rx != ry ? rx - ry : x.getKey().compareTo(y.getKey());
        });
        HTMLElement boxes = cell("places", null);
        for (Map.Entry<String, Map<String, List<JsPropertyMap<Object>>>> e : placed) {
            boxes.append(machineBox(e.getKey(), e.getValue()));
        }
        Element wires = DomGlobal.document.createElementNS("http://www.w3.org/2000/svg", "svg");
        wires.setAttribute("class", "wires");
        wires.setAttribute("aria-hidden", "true");
        HTMLElement canvas = cell("mapcanvas", null);
        canvas.append(wires, boxes);
        HTMLElement legend = cell("maplegend", null);
        for (String[] k : new String[][]{{"ok", "map.edge_ok"}, {"flight", "map.edge_working"},
                {"down", "map.edge_down"}}) {
            HTMLElement item = cell("mapkey", null);
            item.append(cell("wirekey " + k[0], null), cell("", tr(k[1])));
            legend.append(item);
        }
        root.replaceChildren(head, cell("accsay", tr("map.lead")), canvas, legend);
        drawWires(canvas, wires, rows, hands);
        if (watch != null) watch.disconnect();
        // 선은 상자의 <b>크기</b>에 달렸다. 그런데 선을 다시 그리는 것 자체가 관찰 대상 안을
        // 건드려 관찰자를 다시 부르므로, 조건 없이 다시 그리면 제 꼬리를 문다(실측: 10초에
        // 60번, 크기는 한 번도 안 변했다). 그래서 <b>정말 달라졌을 때만</b> 다시 그린다.
        drawnAt = "";
        watch = new ResizeObserver((entries, obs) -> {
            elemental2.dom.DOMRect box = canvas.getBoundingClientRect();
            String now = ((int) box.width) + "x" + ((int) box.height);
            if (now.equals(drawnAt)) return null;
            drawnAt = now;
            clear(wires);
            drawWires(canvas, wires, rows, hands);
            return null;
        });
        watch.observe(canvas);
    }

    /**
     * 이미 서 있는 노드의 쉰 시간만 고쳐 쓴다 — 자리도 순서도 그대로. 노드는 그린 순서 그대로
     * 서 있으므로, 같은 순서로 걸으며 남의 기계의 것에만 적는다(그리는 규칙과 같은 조건).
     */
    private void paintAges() {
        elemental2.dom.NodeList<elemental2.dom.Element> drawn = root.querySelectorAll(".nodeage");
        JsArrayLike<Object> rows = Js.uncheckedCast(store.fleet());
        int at = 0;
        for (int i = 0; rows != null && i < rows.getLength(); i++) {
            JsPropertyMap<Object> a = Js.uncheckedCast(rows.getAt(i));
            if (!Js.isTruthy(a.get("elsewhere"))) continue;
            if (at >= drawn.getLength()) return;
            double idle = a.get("idle") == null ? -1 : Js.coerceToDouble(a.get("idle"));
            String word = idle >= 0 ? tr("time.ago", "d", dur((int) idle)) : "";
            elemental2.dom.Element line = drawn.getAt(at++);
            // 같은 낱말을 다시 쓰는 것은 무동작이 아니다 — textContent 는 글자 노드를 갈아치우므로
            // 판이 바뀌었다고 보는 눈(관찰자·스크린리더)에는 매번 바뀐 것으로 보인다.
            if (!word.equals(line.textContent)) line.textContent = word;
        }
        paintUnseen(rows);
    }

    /**
     * 통째로 침묵한 상자 위의 그 줄도 늙는다 — "3분 전"은 4분째에 거짓말이다.
     *
     * 노드의 쉰 시간과 함께 고쳐 쓴다: 이 줄이 세는 것은 그 상자의 가장 최근 소식이고,
     * 그것은 상자 안 행들의 idle 중 가장 작은 값이다(그리는 자리와 같은 셈).
     */
    private void paintUnseen(JsArrayLike<Object> rows) {
        elemental2.dom.NodeList<Element> lines = root.querySelectorAll(".placeseen[data-host]");
        for (int i = 0; i < lines.getLength(); i++) {
            Element line = lines.getAt(i);
            String host = line.getAttribute("data-host");
            double fresh = Double.MAX_VALUE;
            for (int j = 0; rows != null && j < rows.getLength(); j++) {
                JsPropertyMap<Object> a = Js.uncheckedCast(rows.getAt(j));
                if (!host.equals(hostOf(a))) continue;
                double idle = a.get("idle") == null ? -1 : Js.coerceToDouble(a.get("idle"));
                if (idle >= 0) fresh = Math.min(fresh, idle);
            }
            String said = unseenWord(fresh);
            if (!said.equals(line.textContent)) line.textContent = said;
        }
    }

    /**
     * 이미 서 있는 노드에 지금 상태를 <b>고쳐 입힌다</b> — 자리도 순서도 그대로.
     *
     * 순서에 기대지 않고 소켓으로 찾는다: 상태는 그리는 순서를 정하지 않지만(정렬은 신뢰가
     * 한다) 명단이 실어 오는 순서는 그때그때 다르고, 순서로 짚으면 두 노드가 서로의 상태를
     * 입는다 — 그러면 고친 것이 아니라 거짓말이 된다.
     *
     * 그림은 <b>달라졌을 때만</b> 갈아 끼운다. 상태 다섯이 그림 다섯은 아니어서(stopped와
     * abandoned는 한 그림) 매번 갈면 아무것도 안 달라진 자리에서 자식이 사라졌다 생긴다.
     */
    private void paintStates() {
        JsArrayLike<Object> rows = Js.uncheckedCast(store.fleet());
        for (int i = 0; rows != null && i < rows.getLength(); i++) {
            JsPropertyMap<Object> a = Js.uncheckedCast(rows.getAt(i));
            Element n = bySock(str(a, "socket"));
            if (n == null) continue;
            String state = str(a, "state");
            String cls = "node state " + state + (Js.isTruthy(a.get("elsewhere")) ? " faroff" : "");
            if (!cls.equals(n.className)) n.className = cls;
            String mark = StateMark.of(AgentStates.groupOf(state));
            if (!mark.equals(n.getAttribute("data-mark"))) {
                n.setAttribute("data-mark", mark);
                Element had = n.querySelector(".nodemark");
                if (had != null) n.replaceChild(Icons.orGlyph(mark, "\u2022", "nodemark"), had);
            }
            Element word = n.querySelector(".nodestate");
            String said = stateWord(state);
            if (word != null && !said.equals(word.textContent)) word.textContent = said;
        }
    }

    /** 이 소켓의 노드 — 그리는 자리와 재는 자리가 같은 열쇠를 쓴다({@link #spot}). */
    private Element bySock(String sock) {
        return root.querySelector(".node[data-sock=\"" + sock.replace("\"", "") + "\"]");
    }

    private int bestRank(Map<String, List<JsPropertyMap<Object>>> accounts) {
        int best = 9;
        for (List<JsPropertyMap<Object>> list : accounts.values()) {
            best = Math.min(best, Atlas.trustRank(trustOf(list)));
        }
        return best;
    }

    private HTMLElement machineBox(String host, Map<String, List<JsPropertyMap<Object>>> accounts) {
        HTMLElement box = cell("machine", null);
        HTMLElement top = cell("machinetop", null);
        top.append(cell("machinename", host));
        List<JsPropertyMap<Object>> all = new ArrayList<>();
        accounts.values().forEach(all::addAll);
        for (JsPropertyMap<Object> a : all) {
            if (!str(a, "addr").isEmpty()) { top.append(cell("machineaddr", str(a, "addr"))); break; }
        }
        box.append(top);
        // 상자 위엔 나쁜 소식만: 통째 침묵 — 그때 볼 것은 링크다(운영 규칙).
        String t = trustOf(all);
        boolean anyLive = false;
        double fresh = Double.MAX_VALUE;
        for (JsPropertyMap<Object> a : all) {
            if (Js.isTruthy(a.get("live"))) anyLive = true;
            double idle = a.get("idle") == null ? -1 : Js.coerceToDouble(a.get("idle"));
            if (idle >= 0) fresh = Math.min(fresh, idle);
        }
        if (!t.isEmpty() && !"own".equals(t) && !anyLive) {
            HTMLElement seen = cell("placeseen down", unseenWord(fresh));
            // 이 줄의 말도 초마다 늙는다 — 어느 상자의 것인지 적어 두어야 판을 다시 세우지 않고
            // 고쳐 쓸 수 있다. 상태가 판을 다시 세우지 않게 된 지금, 적어 두지 않으면 이 말은
            // 판이 다시 설 때까지 "3분 전"에 얼어붙는다.
            seen.setAttribute("data-host", host);
            box.append(seen);
        }
        HTMLElement inner = cell("accounts", null);
        List<Map.Entry<String, List<JsPropertyMap<Object>>>> ordered = new ArrayList<>(accounts.entrySet());
        ordered.sort((x, y) -> Atlas.trustRank(trustOf(x.getValue())) - Atlas.trustRank(trustOf(y.getValue())));
        for (Map.Entry<String, List<JsPropertyMap<Object>>> e : ordered) {
            inner.append(placeBox(e.getKey(), e.getValue()));
        }
        box.append(inner);
        return box;
    }

    private HTMLElement placeBox(String who, List<JsPropertyMap<Object>> list) {
        String t = trustOf(list);
        HTMLElement box = cell("place " + (t.isEmpty() ? "unsaid" : t), null);
        HTMLElement top = cell("placetop", null);
        top.append(cell("placename", who));
        if (!t.isEmpty()) top.append(cell("placetrust " + t, tr(trustKey(t))));
        box.append(top);
        // 팀은 세 번째 경계 — 상자를 가로지르니 안에선 머리로(운영 규칙).
        Map<String, List<JsPropertyMap<Object>>> teams = new LinkedHashMap<>();
        for (JsPropertyMap<Object> a : list) {
            teams.computeIfAbsent(str(a, "team"), k -> new ArrayList<>()).add(a);
        }
        List<String> named = new ArrayList<>(teams.keySet());
        named.remove("");
        named.sort(String::compareTo);
        for (String team : named) {
            box.append(cell("teamlabel", team));
            for (JsPropertyMap<Object> a : teams.get(team)) box.append(node(a));
        }
        for (JsPropertyMap<Object> a : teams.getOrDefault("", new ArrayList<>())) box.append(node(a));
        return box;
    }

    /** 노드 — 같은 다섯 상태의 같은 말. 딴 머신 것은 링크가 아니다(남의 파일시스템 경로). */
    private HTMLElement node(JsPropertyMap<Object> a) {
        boolean remote = Js.isTruthy(a.get("elsewhere"));
        HTMLElement n = el(remote ? "div" : "a");
        n.className = "node state " + str(a, "state") + (remote ? " faroff" : "");
        n.setAttribute("data-sock", str(a, "socket"));
        if (!remote) {
            n.setAttribute("href", Windows.here() + "?d=" + str(a, "socket")
                    + (str(a, "peer").isEmpty() ? "" : "&p=" + str(a, "peer")));
            n.addEventListener("click", evt -> {
                evt.preventDefault();
                GoSharing.go(str(a, "socket"), str(a, "peer").isEmpty() ? null : str(a, "peer"));
            });
        }
        // 상태가 입는 그림 — 있으면 스프라이트, 없으면 늘 그리던 점(운영 iconOr와 같은 계약).
        // 점을 글자로 박아 두면 링크의 읽히는 이름이 "•ws1 Idle"이 된다(실측): 그림은 그림으로.
        String mark = StateMark.of(AgentStates.groupOf(str(a, "state")));
        // 표를 적어 둔다 — 고쳐 입힐 때 <b>같은 그림인지</b>를 물어볼 자리가 여기뿐이다
        // (상태 다섯이 그림 다섯은 아니다: stopped와 abandoned는 한 그림이다).
        n.setAttribute("data-mark", mark);
        n.append(Icons.orGlyph(mark, "\u2022", "nodemark"), cell("nodename", str(a, "name")));
        if (Js.isTruthy(a.get("hub"))) n.append(cell("nodehub", tr("team.speaks")));
        if (remote) {
            double idle = a.get("idle") == null ? -1 : Js.coerceToDouble(a.get("idle"));
            n.append(cell("nodeage" + (Js.isTruthy(a.get("live")) ? "" : " down"),
                    idle >= 0 ? tr("time.ago", "d", dur((int) idle)) : ""));
        }
        n.append(cell("nodestate", stateWord(str(a, "state"))));
        return n;
    }

    // ── 와이어: 측정 기반 — 레이아웃 없는 곳에선 0×0이라 아무것도 그리지 않는다 ──

    private int lane = 0;

    private void drawWires(HTMLElement canvas, Element svg, JsArrayLike<Object> rows,
                           JsArrayLike<Object> hands) {
        elemental2.dom.DOMRect frame = canvas.getBoundingClientRect();
        if (frame.width == 0 || frame.height == 0) return;
        svg.setAttribute("viewBox", "0 0 " + frame.width + " " + frame.height);
        Map<String, JsPropertyMap<Object>> bySock = new LinkedHashMap<>();
        Map<String, JsPropertyMap<Object>> byName = new LinkedHashMap<>();
        for (int i = 0; i < rows.getLength(); i++) {
            JsPropertyMap<Object> a = Js.uncheckedCast(rows.getAt(i));
            bySock.put(str(a, "socket"), a);
            byName.put(str(a, "name").toLowerCase(), a);
        }
        lane = 0;
        for (int i = 0; i < hands.getLength(); i++) {
            JsPropertyMap<Object> h = Js.uncheckedCast(hands.getAt(i));
            JsPropertyMap<Object> from = byName.get(str(h, "from").toLowerCase());
            JsPropertyMap<Object> to = bySock.containsKey(str(h, "socket"))
                    ? bySock.get(str(h, "socket")) : byName.get(str(h, "to").toLowerCase());
            if (from == null || to == null || from == to) continue;
            Spot a = spot(canvas, frame, str(from, "socket"));
            Spot b = spot(canvas, frame, str(to, "socket"));
            if (a == null || b == null) continue;
            String cls = Atlas.edgeClass(Js.isTruthy(to.get("live")), str(h, "state"));
            boolean together = a.ml == b.ml && a.mt == b.mt;
            if (together) curve(svg, a, b, cls, frame.width);
            else around(svg, a, b, frame.height - 8 - (lane++ % 6) * 7, cls, frame.width);
        }
    }

    private static final class Spot {
        double l, r, y, ml, mt, mr, mlft;
    }

    private Spot spot(HTMLElement canvas, elemental2.dom.DOMRect frame, String sock) {
        Element el = canvas.querySelector("[data-sock=\"" + sock.replace("\"", "") + "\"]");
        if (el == null) return null;
        elemental2.dom.DOMRect r = el.getBoundingClientRect();
        Spot s = new Spot();
        s.l = r.left - frame.left;
        s.r = r.right - frame.left;
        s.y = r.top - frame.top + r.height / 2;
        Element outer = el.parentElement;
        while (outer != null && !(" " + outer.className + " ").contains(" machine ")) outer = outer.parentElement;
        elemental2.dom.DOMRect m = outer == null ? r : outer.getBoundingClientRect();
        s.ml = m.left - frame.left;
        s.mt = m.top - frame.top;
        s.mr = m.right - frame.left;
        s.mlft = m.left - frame.left;
        return s;
    }

    private void curve(Element svg, Spot a, Spot b, String cls, double width) {
        double want = Math.max(16, Math.abs(b.y - a.y) / 2);
        double right = width - Math.max(a.r, b.r) - 4;
        if (right >= 12) {
            double out = Math.min(want, right);
            path(svg, "M" + a.r + " " + a.y + " C" + (a.r + out) + " " + a.y + " "
                    + (b.r + out) + " " + b.y + " " + b.r + " " + b.y, cls);
            return;
        }
        double out = Math.min(want, Math.max(4, Math.min(a.l, b.l) - 4));
        path(svg, "M" + a.l + " " + a.y + " C" + (a.l - out) + " " + a.y + " "
                + (b.l - out) + " " + b.y + " " + b.l + " " + b.y, cls);
    }

    private void around(Element svg, Spot a, Spot b, double y, String cls, double width) {
        double leave = Math.min(a.mr + 10, width - 4);
        double enter = Math.max(b.mlft - 10, 4);
        path(svg, "M" + a.r + " " + a.y + " H" + leave + " V" + y + " H" + enter
                + " V" + b.y + " H" + b.l, cls);
    }

    private void path(Element svg, String d, String cls) {
        Element p = DomGlobal.document.createElementNS("http://www.w3.org/2000/svg", "path");
        p.setAttribute("d", d);
        p.setAttribute("class", "wire " + cls);
        svg.append(p);
    }

    private static void clear(Element svg) {
        while (svg.firstChild != null) svg.removeChild(svg.firstChild);
    }

    // ── 잔손 ─────────────────────────────────────────────────────────────────

    /** 이 행이 서 있는 머신 — 피어는 그 콘솔 이름이 곧 머신이다(운영 규칙). */
    private static String hostOf(JsPropertyMap<Object> a) {
        return !str(a, "peer").isEmpty() ? str(a, "peer")
                : !str(a, "host").isEmpty() ? str(a, "host") : tr("map.here");
    }

    /** 통째로 침묵한 상자가 이고 있는 말 — 들은 적이 없으면 줄표. */
    private static String unseenWord(double fresh) {
        return tr("map.unseen", "ago",
                fresh == Double.MAX_VALUE ? "\u2014" : tr("time.ago", "d", dur((int) fresh)));
    }

    private static String trustOf(List<JsPropertyMap<Object>> list) {
        for (JsPropertyMap<Object> a : list) if (!str(a, "trust").isEmpty()) return str(a, "trust");
        return "";
    }

    private static String trustKey(String t) {
        switch (t) {
            case "own": return "map.trust_own";
            case "admitted": return "map.trust_admitted";
            case "unknown": return "map.trust_unknown";
            default: return "map.trust_unsaid";
        }
    }

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

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
