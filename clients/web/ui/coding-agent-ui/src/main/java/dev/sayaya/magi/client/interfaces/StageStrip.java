package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Labels;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * 진행 단계 칸 — 도구가 알린 단계 목록을 한 줄로 보인다(시작 → 단계 … → 끝).
 *
 * 단계마다 id 로 노드를 지킨다. 새 알림이 오면 그 노드의 글자와 상태만 바꾸고, 단계가 늘거나
 * 순서가 바뀔 때만 줄을 다시 엮는다 — 칸을 새로 만들지 않으므로 깜빡이지 않는다. 무엇의 단계인지는
 * 모른다: 이름(label), 종류(kind), 상태(state), 센 값만 그린다.
 */
final class StageStrip {
    private static final Map<String, String> GLYPH = new HashMap<>();
    static {
        GLYPH.put("tool", "\u2699");
        GLYPH.put("mcp", "\u26C1");
        GLYPH.put("web", "\u25CD");
        GLYPH.put("harness", "\u2726");
        GLYPH.put("subagent", "\u2727");
    }

    private final HTMLElement box = el("div");
    private final HTMLElement start = el("span");
    private final HTMLElement end = el("span");
    private final Map<String, HTMLElement> nodes = new HashMap<>();
    private String order = "";

    StageStrip() {
        box.className = "stages";
        box.setAttribute("hidden", "");
        start.className = "stage-end";
        end.className = "stage-end";
        box.append(start, end);
    }

    HTMLElement element() { return box; }

    /** 이 단계들을 보인다. 없거나 비었으면 숨는다. */
    void show(Object stages) {
        JsArrayLike<Object> list = stages == null ? null : Js.asArrayLike(stages);
        if (list == null || list.getLength() == 0) {
            box.setAttribute("hidden", "");
            return;
        }
        start.textContent = Labels.tr("progress.start");
        end.textContent = Labels.tr("progress.end");
        List<String> ids = new ArrayList<>();
        boolean started = false, allDone = true;
        for (int i = 0; i < list.getLength(); i++) {
            JsPropertyMap<Object> s = Js.asPropertyMap(list.getAt(i));
            String id = str(s.get("id"));
            if (id.isEmpty()) continue;
            ids.add(id);
            HTMLElement n = nodes.get(id);
            if (n == null) {
                n = el("span");
                n.className = "stage";
                n.setAttribute("data-stage-id", id);
                nodes.put(id, n);
            }
            String state = str(s.get("state"));
            String glyph = GLYPH.get(str(s.get("kind")));
            StringBuilder text = new StringBuilder();
            if (glyph != null) text.append(glyph).append(' ');
            text.append(str(s.get("label")));
            int done = num(s.get("done")), total = num(s.get("total")), running = num(s.get("running"));
            if (total > 0) text.append(' ').append(done).append('/').append(total);
            if (running > 0) text.append(" \u00B7").append(running);
            if (!text.toString().equals(n.textContent)) n.textContent = text.toString();
            if (!state.equals(n.getAttribute("data-state"))) n.setAttribute("data-state", state);
            if (!"waiting".equals(state)) started = true;
            if (!"done".equals(state)) allDone = false;
        }
        nodes.keySet().retainAll(ids);
        start.setAttribute("data-state", started ? "done" : "waiting");
        end.setAttribute("data-state", allDone && !ids.isEmpty() ? "done" : "waiting");
        String now = String.join("\u0000", ids);
        if (!now.equals(order)) {
            order = now;
            box.textContent = "";
            box.append(start);
            for (String id : ids) box.append(nodes.get(id));
            box.append(end);
        }
        box.removeAttribute("hidden");
    }

    private static String str(Object o) { return o == null ? "" : String.valueOf(o); }

    private static int num(Object o) { return o == null ? 0 : (int) Js.asDouble(o); }

    private static HTMLElement el(String tag) { return Js.cast(DomGlobal.document.createElement(tag)); }
}
