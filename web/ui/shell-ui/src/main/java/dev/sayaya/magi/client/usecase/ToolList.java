package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.Tool;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Consumer;

/**
 * 지금 레일이 보일 도구 목록 — handbook ToolList의 번역. 손끝이 가리키는 문(피크)이
 * 우선이고, 없으면 서 있는 문의 것이다.
 *
 * 도구는 문별로 provide()로 들어온다 — 아직 부르는 곳이 없다(용례 대기): 오늘 모든 문의
 * 목록은 비어 있고, 그래서 툴 레일도 없다(2개 이상일 때만 선다 — RailModes).
 */
@Singleton
public class ToolList {
    private final Map<String, List<Tool>> byDoor = new HashMap<>();
    private final List<Consumer<List<Tool>>> observers = new ArrayList<>();
    private Destination selected = null;
    private Destination hovered = null;

    @Inject
    public ToolList(Navigation nav, MenuHover hover) {
        nav.subscribe(place -> { selected = place.section; emit(); });
        hover.subscribe(d -> { hovered = d; emit(); });
    }

    /** 문의 도구를 등록한다 — 등록은 곧 레일의 사실이 된다. */
    public void provide(String destId, List<Tool> tools) {
        byDoor.put(destId, tools == null ? Collections.emptyList() : tools);
        emit();
    }

    public List<Tool> current() {
        Destination d = hovered != null ? hovered : selected;
        if (d == null) return Collections.emptyList();
        List<Tool> tools = byDoor.get(d.id);
        return tools == null ? Collections.emptyList() : tools;
    }

    public void subscribe(Consumer<List<Tool>> o) {
        observers.add(o);
        o.accept(current());
    }

    private void emit() {
        List<Tool> now = current();
        for (Consumer<List<Tool>> o : observers) o.accept(now);
    }
}
