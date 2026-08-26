package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.RailModes;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

/**
 * 레일의 자세 — 드로어 개폐·도구 수·손끝·닫힘(←)을 모아 두 기둥의 상태(RailModes)를
 * 계산하고 알린다. 규칙은 전부 domain/RailModes(순수)에 있고, 여기는 사실의 수집이다.
 *
 * dismissed(←)는 접힌 기둥의 것: 드로어를 열거나 도구 목록이 바뀌면 걷힌다.
 */
@Singleton
public class RailMode {
    private final List<Runnable> observers = new ArrayList<>();
    private final ToolList tools;
    private boolean open = false;
    private boolean hovering = false;
    private boolean dismissed = false;
    private int toolCount = 0;

    @Inject
    public RailMode(ToolList tools) {
        this.tools = tools;
        tools.subscribe(list -> {
            int n = list == null ? 0 : list.size();
            if (n != toolCount) dismissed = false;   // 새 문맥은 새 판단
            toolCount = n;
            emit();
        });
    }

    /** 드로어 개폐 — RailElement(버거·스크림)가 알린다. 여는 순간 닫힘(←)은 걷힌다. */
    public void drawer(boolean isOpen) {
        open = isOpen;
        if (isOpen) dismissed = false;
        emit();
    }

    /** 손끝이 레일 위에 있는가 — 접힌 툴 레일의 피크(라벨 노출)가 읽는다. */
    public void hover(boolean over) {
        if (hovering == over) return;
        hovering = over;
        emit();
    }

    /** ←: 접힌 툴 기둥을 접고 메뉴 기둥으로 — 선택은 그대로다(handbook 데스크톱 규칙). */
    public void dismiss() {
        dismissed = true;
        emit();
    }

    public boolean open() { return open; }

    public RailModes.State menu() { return RailModes.menu(open, toolCount, dismissed); }

    public RailModes.State tool() { return RailModes.tool(open, toolCount, hovering, dismissed); }

    public void subscribe(Runnable o) {
        observers.add(o);
        o.run();
    }

    private void emit() {
        for (Runnable o : observers) o.run();
    }
}
