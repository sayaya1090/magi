package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.RailModes;
import dev.sayaya.magi.client.usecase.MenuHover;
import dev.sayaya.magi.client.usecase.Navigation;
import dev.sayaya.magi.client.usecase.RailMode;
import dev.sayaya.magi.client.usecase.RailView;
import dev.sayaya.magi.client.usecase.RosterStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import elemental2.dom.KeyboardEvent;
import elemental2.dom.MouseEvent;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.LinkedHashMap;
import java.util.Map;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 드로어 — 메뉴 레일(목적지 전부, 컴패니언 포함)이 기본 기둥이고, 열리면 라벨과 문장
 * (console.css의 열림 규칙)도 여기가 말한다. 옛 nav-wide(모양 전환) 기계는 은퇴 —
 * 남은 상태는 body[nav="open"](폭) 하나다.
 *
 * 툴 레일(ToolRailElement)은 도구가 2개 이상인 문에서만 선다 — 접히면 기둥을 대신하고
 * (메뉴는 HIDE, ← 로 복귀), 열리면 둘째 기둥이다. 두 기둥의 상태는 RailMode가 계산해
 * #rail의 menu/tool 속성으로 적힌다 — CSS가 그 속성을 읽는다. 아직 도구를 등록한 문이
 * 없어(용례 대기) 화면은 메뉴 기둥뿐이다.
 *
 * 대기 배지는 컴패니언 문의 아이콘 위에 산다.
 */
@Singleton
public class RailElement implements RailView {
    private final Navigation nav;
    private final MenuHover hover;
    private final RailMode mode;
    private final HTMLElement rail = el("nav");
    private final HTMLElement scrim = el("div");
    private final HTMLElement menu = el("md-icon-button");
    private final HTMLElement badge = el("md-badge");
    private final Map<String, HTMLElement> items = new LinkedHashMap<>();
    private int waiting = 0;

    @Inject
    public RailElement(Navigation nav, RosterStore roster, MenuHover hover, RailMode mode, ToolRailElement panel) {
        this.nav = nav;
        this.hover = hover;
        this.mode = mode;
        build(panel);
        roster.subscribe(this::countWaiting);
        mode.subscribe(this::applyModes);
    }

    /** 두 기둥의 상태를 속성으로 — CSS가 읽는 계약: #rail[menu=…][tool=…]. */
    private void applyModes() {
        rail.setAttribute("menu", word(mode.menu()));
        rail.setAttribute("tool", word(mode.tool()));
    }

    private static String word(RailModes.State s) {
        switch (s) {
            case EXPAND: return "expand";
            case COLLAPSE: return "collapse";
            default: return "hide";
        }
    }

    public HTMLElement element() { return rail; }
    public HTMLElement scrim() { return scrim; }

    /** 문의 말들 — 라벨·짧은 라벨·aria-label 전부 팩에서. 언어가 바뀌면 다시 부른다. */
    public void paint() {
        menu.setAttribute("aria-label", tr("nav.menu"));
        for (Destination d : Destination.all()) {
            HTMLElement item = items.get(d.id);
            text(item, ".lbl", tr(d.labelKey));
            text(item, ".lblshort", tr(d.shortKey));
            text(item, ".sub", tr(d.subKey));
        }
        relabelFleet();
    }

    @Override
    public void select(Destination d) {
        for (Map.Entry<String, HTMLElement> e : items.entrySet()) {
            boolean on = e.getKey().equals(d.id);
            // 두 이름 다: CSS는 [selected]를 읽고, 보조기술은 aria-current="page"를 듣는다.
            if (on) {
                e.getValue().setAttribute("selected", "");
                e.getValue().setAttribute("aria-current", "page");
            } else {
                e.getValue().removeAttribute("selected");
                e.getValue().removeAttribute("aria-current");
            }
        }
    }

    // ── 배지 ─────────────────────────────────────────────────────────────────

    /** 문에 붙는 수는 기다리는 사람 수다 — 0이면 배지 자체가 없다: 늘 있는 배지는 안 읽힌다. */
    private void countWaiting(FleetAgent[] list) {
        if (list == null) return;
        int n = 0;
        for (FleetAgent a : list) if ("waiting".equals(a.state)) n++;
        waiting = n;
        Js.asPropertyMap(badge).set("value", n > 999 ? "999+" : String.valueOf(n));
        if (n > 0) badge.removeAttribute("hidden");
        else badge.setAttribute("hidden", "");
        relabelFleet();
    }

    /** 배지의 뜻은 위치에 있고 위치는 낭독되지 않는다 — 수는 문의 이름에 실린다. */
    private void relabelFleet() {
        HTMLElement fleet = items.get(Destination.FLEET.id);
        if (fleet == null) return;
        String label = tr(Destination.FLEET.labelKey)
                + (waiting > 0 ? ", " + tr("state.waiting_on_you", "n", String.valueOf(waiting)) : "");
        fleet.setAttribute("aria-label", label);
    }

    // ── 마크업 ───────────────────────────────────────────────────────────────

    private void build(ToolRailElement panel) {
        scrim.id = "scrim";
        scrim.addEventListener("click", evt -> close());

        rail.id = "rail";
        // 버거는 다섯 획: 위·아래 막대가 반씩이라, 열리면 네 반쪽이 X의 팔이 되고
        // 가운데 획만 제 중심으로 걷혀 사라진다 — 아이콘 교체가 아니라 같은 획의 이동.
        menu.id = "railMenu";
        menu.setAttribute("aria-expanded", "false");
        menu.innerHTML = "<svg class=\"burger\" viewBox=\"0 0 24 24\" width=\"22\" height=\"22\" aria-hidden=\"true\">"
                + "<line class=\"bt bl\" x1=\"3\" y1=\"6\" x2=\"12\" y2=\"6\"/>"
                + "<line class=\"bt br\" x1=\"12\" y1=\"6\" x2=\"21\" y2=\"6\"/>"
                + "<line class=\"bm\" x1=\"3\" y1=\"12\" x2=\"21\" y2=\"12\"/>"
                + "<line class=\"bb bl\" x1=\"3\" y1=\"18\" x2=\"12\" y2=\"18\"/>"
                + "<line class=\"bb br\" x1=\"12\" y1=\"18\" x2=\"21\" y2=\"18\"/></svg>";
        menu.addEventListener("click", evt -> toggle());
        rail.append(menu);

        HTMLElement navBox = el("div");
        navBox.id = "railNav";
        for (Destination d : Destination.all()) {
            HTMLElement item = item(d);
            items.put(d.id, item);
            navBox.append(item);
        }
        rail.append(navBox);

        // 발치는 지금 비어 있다 — 접근 제어 문은 그 화면이 이식될 때 온다. 요소는 두는데,
        // CSS가 빈 발의 헤어라인을 스스로 걷는다(:has 규칙).
        HTMLElement foot = el("div");
        foot.id = "railFoot";
        rail.append(foot);

        // 툴 레일 — 언제 어떤 모습인지는 RailMode(#rail의 menu/tool 속성)가 말한다.
        rail.append(panel.element());
        // 호버 터널: 레일 전체를 벗어날 때만 피크를 접는다. 접힌 툴 기둥의 라벨 피크는
        // 레일 위에 손끝이 있다는 사실 자체가 켠다(RailMode.hover).
        rail.addEventListener("mouseenter", evt -> mode.hover(true));
        rail.addEventListener("mouseleave", evt -> { mode.hover(false); hover.next(null); });

        // 배지: 컴패니언 문의 아이콘 위 — 1단이 늘 기둥이라 여기가 늘 제자리다.
        badge.id = "railBadge";
        badge.setAttribute("hidden", "");
        badge.setAttribute("aria-hidden", "true");
        HTMLElement fleet = items.get(Destination.FLEET.id);
        Element icwrap = fleet == null ? null : fleet.querySelector(".icwrap");
        if (icwrap != null) icwrap.append(badge);

        // 레일은 목적지의 집합이다 — 화살표로 다니고, 끝에서 감기지 않는다(가이드).
        rail.addEventListener("keydown", evt -> {
            KeyboardEvent ke = Js.uncheckedCast(evt);
            if (!"ArrowDown".equals(ke.key) && !"ArrowUp".equals(ke.key)) return;
            HTMLElement[] list = items.values().toArray(new HTMLElement[0]);
            int at = -1;
            for (int i = 0; i < list.length; i++) if (list[i] == DomGlobal.document.activeElement) at = i;
            if (at < 0) return;
            evt.preventDefault();
            int to = "ArrowDown".equals(ke.key) ? Math.min(at + 1, list.length - 1) : Math.max(at - 1, 0);
            if (to != at) list[to].focus();
        });
    }

    /** 목적지 하나 — 진짜 앵커: 가운데 클릭과 복사한 주소가 살아 있어야 항해다. */
    private HTMLElement item(Destination d) {
        HTMLElement a = el("a");
        a.className = "raili";
        a.setAttribute("href", d == Destination.FLEET ? "/next" : "/next?v=" + d.id);
        HTMLElement icwrap = el("span");
        icwrap.className = "icwrap";
        icwrap.innerHTML = "<svg class=\"ic\" viewBox=\"0 0 24 24\" width=\"24\" height=\"24\" aria-hidden=\"true\">"
                + "<path d=\"" + d.iconPath + "\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.6\" "
                + "stroke-linecap=\"round\" stroke-linejoin=\"round\"/></svg>";
        HTMLElement words = el("span");
        words.className = "words";
        words.innerHTML = "<span class=\"lbl\"></span><span class=\"lblshort\"></span><span class=\"sub\"></span>";
        a.append(icwrap, words);
        a.addEventListener("click", evt -> {
            MouseEvent me = Js.uncheckedCast(evt);
            // 수식 키는 브라우저의 것 — 새 탭·새 창은 앵커가 원래 하던 일이다.
            if (me.metaKey || me.ctrlKey || me.shiftKey || me.button != 0) return;
            evt.preventDefault();
            nav.go(d);
        });
        // 피크는 열린 드로어의 것 — 접힌 레일에서 2단은 없고, 호버는 아무 말도 아니다.
        a.addEventListener("mouseenter", evt -> {
            if ("open".equals(DomGlobal.document.body.getAttribute("nav"))) hover.next(d);
        });
        return a;
    }

    // ── 열고 닫기 ────────────────────────────────────────────────────────────
    // 남은 상태는 폭 하나다: 1단이 열려도 기둥이라 모양 전환(nav-wide)이 없고,
    // 따라서 닫힘의 250ms 지연도 없다 — 2단은 CSS가 폭과 함께 걷는다.

    private void toggle() {
        if ("open".equals(DomGlobal.document.body.getAttribute("nav"))) { close(); return; }
        DomGlobal.document.body.setAttribute("nav", "open");
        menu.setAttribute("aria-expanded", "true");
        mode.drawer(true);
    }

    private void close() {
        DomGlobal.document.body.removeAttribute("nav");
        menu.setAttribute("aria-expanded", "false");
        hover.next(null);
        mode.drawer(false);
    }

    // ── 잔손 ─────────────────────────────────────────────────────────────────

    private static void text(HTMLElement host, String selector, String value) {
        Element found = host.querySelector(selector);
        if (found != null) found.textContent = value;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
