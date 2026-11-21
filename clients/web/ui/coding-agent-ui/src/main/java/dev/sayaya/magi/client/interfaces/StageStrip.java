package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Labels;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.Set;
import java.util.List;
import java.util.Map;

/**
 * 진행 단계 칸 — 도구가 알린 단계 목록을 한 줄로 보인다(시작 → 단계 … → 끝).
 *
 * 노드마다 큰 표시(32px) 아래에 이름을 단다. 단계마다 id 로 노드를 지켜 새 알림이 오면 그 노드의 글자와
 * 상태만 바꾸고, 단계가 늘거나 순서가 바뀔 때만 줄을 다시 엮는다 — 칸을 새로 만들지 않으므로 깜빡이지
 * 않는다. 시작과 끝도 단계와 같은 상태를 갖는다: 시작은 실행 중이면 running, 끝나면 done; 끝은 모두
 * 끝나야 done, 실패가 있으면 failed, 그 전에는 waiting. 무엇의 단계인지는 모른다: 이름, 종류, 상태,
 * 센 값만 그린다.
 */
final class StageStrip {
    /** 표시: 24 단위 상자 안의 단순한 도형, 노드의 글자색(currentColor)으로 긋는다. */
    private static final Map<String, String> MARK = new HashMap<>();
    static {
        MARK.put("tool", "<circle cx='12' cy='12' r='3'/><path d='M12 3v3m0 12v3M3 12h3m12 0h3M5.6 5.6l2.1 2.1m8.6 8.6 2.1 2.1M5.6 18.4l2.1-2.1m8.6-8.6 2.1-2.1'/>");
        MARK.put("mcp", "<rect x='4' y='4' width='16' height='6' rx='1'/><rect x='4' y='14' width='16' height='6' rx='1'/><path d='M8 7h.01M8 17h.01'/>");
        MARK.put("web", "<circle cx='12' cy='12' r='9'/><path d='M3 12h18M12 3c3 3.5 3 14.5 0 18M12 3c-3 3.5-3 14.5 0 18'/>");
        MARK.put("harness", "<path d='M12 3l2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5z'/>");
        MARK.put("subagent", "<circle cx='12' cy='8' r='4'/><path d='M4 21c1-4 4.5-6 8-6s7 2 8 6'/>");
        MARK.put("step", "<circle cx='12' cy='12' r='4'/>");
        MARK.put("start", "<path d='M8 5l11 7-11 7z'/>");
        MARK.put("end", "<path d='M5 21V4M5 4h11l-2 4 2 4H5'/>");
    }

    /** 노드 하나: 표시와 그 아래 이름. */
    private static final class Node {
        final HTMLElement el = el("span");
        final HTMLElement icon = el("span");
        final HTMLElement label = el("span");
        String key = "";

        Node(String role) {
            el.className = "stage";
            el.setAttribute("data-role", role);
            icon.className = "stage-icon";
            label.className = "stage-label";
            el.append(icon, label);
        }

        void mark(String k, String state) {
            if (!k.equals(key)) {
                String shape = MARK.containsKey(k) ? MARK.get(k) : MARK.get("step");
                icon.innerHTML = "<svg viewBox='0 0 24 24' width='32' height='32' aria-hidden='true' focusable='false'>" + shape + "</svg>";
                key = k;
            }
            if (!state.equals(el.getAttribute("data-state"))) el.setAttribute("data-state", state);
        }
    }

    private final HTMLElement box = el("div");
    private final Node start = new Node("start");
    private final Node end = new Node("end");
    private final Map<String, Node> nodes = new HashMap<>();
    private String order = "\u0001";

    StageStrip() {
        box.className = "stages";
        box.setAttribute("hidden", "");
        start.mark("start", "waiting");
        end.mark("end", "waiting");
    }

    HTMLElement element() { return box; }

    /** 이 단계들을 보인다. 없거나 비었으면 숨는다. */
    void show(Object stages) {
        JsArrayLike<Object> list = stages == null ? null : Js.asArrayLike(stages);
        if (list == null || list.getLength() == 0) {
            box.setAttribute("hidden", "");
            return;
        }
        start.label.textContent = Labels.tr("progress.start");
        end.label.textContent = Labels.tr("progress.end");
        List<String> ids = new ArrayList<>();
        Map<String, List<String>> dependencies = new LinkedHashMap<>();
        boolean allWaiting = true, live = false, failed = false;
        for (int i = 0; i < list.getLength(); i++) {
            JsPropertyMap<Object> s = Js.asPropertyMap(list.getAt(i));
            String id = str(s.get("id"));
            if (id.isEmpty()) continue;
            ids.add(id);
            Object after = s.get("after");
            if (after != null) {
                JsArrayLike<Object> predecessors = Js.asArrayLike(after);
                List<String> parents = new ArrayList<>();
                for (int j = 0; j < predecessors.getLength(); j++) parents.add(str(predecessors.getAt(j)));
                dependencies.put(id, parents);
            }
            Node n = nodes.get(id);
            if (n == null) {
                n = new Node("step");
                n.el.setAttribute("data-stage-id", id);
                nodes.put(id, n);
            }
            String state = str(s.get("state"));
            StringBuilder text = new StringBuilder(str(s.get("label")));
            int done = num(s.get("done")), total = num(s.get("total")), running = num(s.get("running"));
            if (total > 0) text.append(' ').append(done).append('/').append(total);
            if (running > 0) text.append(" \u00B7").append(running);
            if (!text.toString().equals(n.label.textContent)) n.label.textContent = text.toString();
            String kind = str(s.get("kind"));
            n.mark(kind.isEmpty() ? "step" : kind, state);
            if (!"waiting".equals(state)) allWaiting = false;
            if ("running".equals(state) || "waiting".equals(state)) live = true;
            if ("failed".equals(state) || "skipped".equals(state)) failed = true;
        }
        nodes.keySet().retainAll(ids);
        boolean none = ids.isEmpty();
        start.mark("start", none || allWaiting ? "waiting" : live ? "running" : "done");
        end.mark("end", none || live ? "waiting" : failed ? "failed" : "done");
        StringBuilder topology = new StringBuilder();
        for (String id : ids) {
            appendKey(topology, id);
            List<String> parents = dependencies.get(id);
            appendKey(topology, parents == null ? "linear" : Integer.toString(parents.size()));
            if (parents != null) for (String parent : parents) appendKey(topology, parent);
        }
        String now = topology.toString();
        if (!now.equals(order)) {
            order = now;
            box.textContent = "";
            if (dependencies.isEmpty()) {
                start.el.removeAttribute("style");
                end.el.removeAttribute("style");
                box.append(start.el);
                for (String id : ids) {
                    HTMLElement node = nodes.get(id).el;
                    node.removeAttribute("style");
                    box.append(arrow(), node);
                }
                box.append(arrow(), end.el);
            } else {
                layoutGraph(ids, dependencies);
            }
        }
        box.removeAttribute("hidden");
    }

    private static void appendKey(StringBuilder key, String value) {
        key.append(value.length()).append(':').append(value);
    }

    private void layoutGraph(List<String> ids, Map<String, List<String>> dependencies) {
        HTMLElement graph = el("div");
        graph.className = "stage-graph";
        Map<String, int[]> positions = new LinkedHashMap<>();
        Map<Integer, Integer> rowCounts = new HashMap<>();
        Set<String> consumed = new HashSet<>();
        StringBuilder paths = new StringBuilder();
        int[] origin = {0, 0};
        place(graph, start.el, origin);
        int lastColumn = 0, rowCount = 1;
        int[] tail = origin;
        String tailId = null;
        for (String id : ids) {
            List<int[]> parents = new ArrayList<>();
            int column = 1;
            List<String> predecessors = dependencies.get(id);
            if (predecessors != null) for (String parent : predecessors) {
                int[] position = positions.get(parent);
                if (position == null) continue;
                parents.add(position);
                consumed.add(parent);
                column = Math.max(column, position[0] + 1);
            }
            if (parents.isEmpty()) {
                column = tail[0] + 1;
                if (tailId != null) consumed.add(tailId);
            }
            int row = rowCounts.getOrDefault(column, 0);
            rowCounts.put(column, row + 1);
            lastColumn = Math.max(lastColumn, column);
            rowCount = Math.max(rowCount, row + 1);
            int[] position = {column, row};
            positions.put(id, position);
            place(graph, nodes.get(id).el, position);
            if (parents.isEmpty()) connect(paths, tail, position);
            else for (int[] parent : parents) connect(paths, parent, position);
            if (predecessors == null) {
                tail = position;
                tailId = id;
            }
        }
        int[] terminal = {lastColumn + 1, 0};
        place(graph, end.el, terminal);
        for (String id : ids) if (!consumed.contains(id)) connect(paths, positions.get(id), terminal);
        HTMLElement wires = el("div");
        wires.className = "stage-wires";
        wires.setAttribute("aria-hidden", "true");
        wires.innerHTML = "<svg width='" + ((lastColumn + 2) * 144) + "' height='" + (rowCount * 76) + "'>" + paths + "</svg>";
        graph.append(wires);
        box.append(graph);
    }

    private static void place(HTMLElement graph, HTMLElement node, int[] position) {
        node.setAttribute("style", "grid-column:" + (position[0] + 1) + ";grid-row:" + (position[1] + 1));
        graph.append(node);
    }

    private static void connect(StringBuilder paths, int[] from, int[] to) {
        int x = from[0] * 144 + 90, y = from[1] * 76 + 16;
        int endX = to[0] * 144 + 53, endY = to[1] * 76 + 16;
        int bend = (x + endX) / 2;
        paths.append("<path d='M").append(x).append(' ').append(y).append(" C")
                .append(bend).append(' ').append(y).append(' ').append(bend).append(' ').append(endY)
                .append(' ').append(endX).append(' ').append(endY).append(" m-5 -4 l5 4 -5 4'/>");
    }

    private static HTMLElement arrow() {
        HTMLElement a = el("span");
        a.className = "stage-arrow";
        a.setAttribute("data-role", "arrow");
        a.setAttribute("aria-hidden", "true");
        a.textContent = "\u2192";
        return a;
    }

    private static String str(Object o) { return o == null ? "" : String.valueOf(o); }

    private static int num(Object o) { return o == null ? 0 : (int) Js.asDouble(o); }

    private static HTMLElement el(String tag) { return Js.cast(DomGlobal.document.createElement(tag)); }
}
