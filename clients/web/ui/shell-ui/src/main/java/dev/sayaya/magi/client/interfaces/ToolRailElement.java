package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.RailModes;
import dev.sayaya.magi.client.domain.Tool;
import dev.sayaya.magi.client.usecase.MenuHover;
import dev.sayaya.magi.client.usecase.Navigation;
import dev.sayaya.magi.client.usecase.RailMode;
import dev.sayaya.magi.client.usecase.ToolList;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 툴 레일 — handbook 툴 레일의 번역. 도구가 2개 이상인 문에서만 선다(RailModes):
 * 접힌 드로어에서는 메뉴 기둥을 대신해 **기둥 자체가 툴 레일이 되고**(아이콘, 피크 시
 * 라벨), 첫 항목은 ←(메뉴 레일로) — 선택은 유지된다. 열린 드로어에서는 1단 메뉴 레일
 * 오른쪽의 둘째 기둥으로 선다(머리 = 문의 이름과 문장).
 *
 * 아직 어느 문도 도구를 등록하지 않아(ToolList.provide 용례 대기) 화면에는 없다 —
 * handbook 규칙: 속이 비면 펼쳐지지 않는다.
 */
@Singleton
public class ToolRailElement {
    private final HTMLElement box = el("div");
    // 도구의 기둥은 sayaya-ui md-navigation-rail이다 — 메뉴 기둥과 같은 컴포넌트, 같은 모양.
    // 다만 도구는 목적지가 아니라 동작이라, 누른 뒤 컴포넌트가 옮긴 선택을 바로 비운다.
    private final HTMLElement column = el("md-navigation-rail");
    private final RailMode mode;
    private final MenuHover hover;
    private final Navigation nav;
    private List<Tool> tools = List.of();
    private Destination showing = null;   // 열린 드로어의 머리가 대는 문

    @Inject
    public ToolRailElement(RailMode mode, ToolList list, MenuHover hover, Navigation nav) {
        this.mode = mode;
        this.hover = hover;
        this.nav = nav;
        box.id = "railTool";
        column.id = "railToolNav";
        // 접힌 기둥은 아이콘만 — 라벨은 피크(펼친 모양)에서 선다(옛 .words 규칙의 그 몫).
        Js.asPropertyMap(column).set("hideCollapsedLabels", true);
        Js.asPropertyMap(column).set("activeIndex", -1);
        column.addEventListener("navigation-rail-activated", evt -> Js.asPropertyMap(column).set("activeIndex", -1));
        nav.subscribe(place -> { if (hover.current() == null) showing = place.section; });
        hover.subscribe(d -> { if (d != null) showing = d; render(); });
        list.subscribe(now -> { tools = now == null ? List.of() : now; render(); });
        mode.subscribe(this::render);
    }

    public HTMLElement element() { return box; }

    private void render() {
        box.replaceChildren();
        if (mode.tool() == RailModes.State.HIDE) return;
        // 열린 드로어의 둘째 기둥엔 머리가 선다 — 어느 문의 속인지.
        if (mode.open() && showing != null) {
            HTMLElement head = el("div");
            head.className = "railpanel-head";
            head.textContent = tr(showing.labelKey);
            HTMLElement sub = el("div");
            sub.className = "railpanel-sub";
            sub.textContent = tr(showing.subKey);
            box.append(head, sub);
        } else {
            // 접힌 기둥의 첫 항목: ← 메뉴 레일로. 선택을 잃지 않는다.
            HTMLElement back = el("md-icon-button");
            back.id = "railToolClose";
            back.setAttribute("aria-label", tr("nav.menu"));
            back.innerHTML = "<svg viewBox=\"0 0 24 24\" width=\"22\" height=\"22\" aria-hidden=\"true\">"
                    + "<path d=\"M14 6l-6 6 6 6\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.8\" "
                    + "stroke-linecap=\"round\" stroke-linejoin=\"round\"/></svg>";
            back.addEventListener("click", evt -> { evt.preventDefault(); mode.dismiss(); });
            box.append(back);
        }
        List<Tool> sorted = new ArrayList<>(tools);
        sorted.sort(Comparator.comparingInt(t -> t.order));
        column.replaceChildren();
        // 피크(접힌 드로어 위 손끝)와 둘째 기둥은 펼친 모양, 접힌 기둥은 아이콘 기둥.
        Js.asPropertyMap(column).set("expanded", mode.tool() == RailModes.State.EXPAND);
        Js.asPropertyMap(column).set("activeIndex", -1);
        for (Tool t : sorted) column.append(item(t));
        box.append(column);
    }

    /**
     * 도구 하나 — md-navigation-rail-item. 누르면 그 도구의 일이 돈다(주소는 그대로).
     * 컴포넌트는 누른 항목을 "현재 위치"로 표시하므로, 그 표시를 곧바로 걷는다: 도구는 가는 곳이 아니다.
     */
    private HTMLElement item(Tool t) {
        HTMLElement b = el("md-navigation-rail-item");
        b.className = "tooli";
        Js.asPropertyMap(b).set("label", tr(t.labelKey));
        HTMLElement icon = el("span");
        icon.setAttribute("slot", "inactive-icon");
        icon.innerHTML = "<svg class=\"ic\" viewBox=\"0 0 24 24\" width=\"24\" height=\"24\" aria-hidden=\"true\">"
                + "<path d=\"" + t.iconPath + "\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.6\" "
                + "stroke-linecap=\"round\" stroke-linejoin=\"round\"/></svg>";
        b.append(icon);
        b.addEventListener("click", evt -> {
            Js.asPropertyMap(column).set("activeIndex", -1);
            if (t.run != null) t.run.run();
        });
        return b;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
