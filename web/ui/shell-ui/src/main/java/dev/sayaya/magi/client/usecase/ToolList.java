package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.Tool;
import dev.sayaya.rx.subject.BehaviorSubject;
import lombok.experimental.Delegate;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

import static dev.sayaya.rx.subject.BehaviorSubject.behavior;

/**
 * 지금 레일이 보일 도구 목록 — handbook ToolList의 번역. 손끝이 가리키는 문(피크)이
 * 우선이고, 없으면 서 있는 문의 것이다.
 *
 * 두 사실(서 있는 문·손끝의 문)에서 <b>파생된</b> 흐름이다: 위의 둘이 흘러야 이것이 흐르고,
 * 내놓는 목록이 지난번과 같으면 흐르지 않는다(같은 문 위에서 손끝이 몇 번을 움직이든
 * 레일이 다시 설 일은 아니다).
 *
 * 도구는 문별로 provide()로 들어온다 — 아직 부르는 곳이 없다(용례 대기): 오늘 모든 문의
 * 목록은 비어 있고, 그래서 툴 레일도 없다(2개 이상일 때만 선다 — RailModes).
 */
@Singleton
public class ToolList {
    @Delegate private final BehaviorSubject<List<Tool>> _this = behavior(Collections.emptyList());
    private final Map<String, List<Tool>> byDoor = new HashMap<>();
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

    private void emit() {
        List<Tool> now = current();
        if (now.equals(getValue())) return;
        _this.next(now);
    }
}
