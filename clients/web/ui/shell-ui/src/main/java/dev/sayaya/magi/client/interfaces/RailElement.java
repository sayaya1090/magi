package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.RailModes;
import dev.sayaya.magi.client.usecase.MenuHover;
import dev.sayaya.magi.client.usecase.Navigation;
import dev.sayaya.magi.client.usecase.MayStore;
import dev.sayaya.magi.client.usecase.RailMode;
import dev.sayaya.magi.client.usecase.RailView;
import dev.sayaya.magi.client.usecase.RosterStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import elemental2.dom.MouseEvent;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.LinkedHashMap;
import java.util.Map;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 드로어 — 메뉴 레일(목적지 전부, 컴패니언 포함)이 기본 기둥이고, 열리면 라벨과 문장도 여기가
 * 말한다. 기둥은 sayaya-ui의 <code>md-navigation-rail</code>이 그리고(넓은 폭), 폰 폭에서는
 * 같은 문들이 <code>md-navigation-bar</code>로 선다 — M3가 폭에 따라 나눠 쓰라는 두 컴포넌트.
 * 셸이 쥔 것은 문의 목록과 상태다: 무엇이 선택됐나, 누가 기다리나, 어느 문이 숨나.
 *
 * 남은 상태는 body[nav="open"](폭)과 nav-wide(모양) 둘 — 레일의 expanded는 모양을 따른다.
 * 툴 레일(ToolRailElement)과 두 기둥의 상태(#rail[menu][tool], RailMode)는 그대로다.
 *
 * 목적지는 컴포넌트가 버튼(role=tab)으로 그린다. 그래서 링크가 하던 일 중 새 탭 열기는 여기서
 * 이벤트로 대신한다(수식 키·가운데 클릭) — 우클릭의 링크 메뉴와 주소 미리보기는 없다.
 */
@Singleton
public class RailElement implements RailView {
    private final Navigation nav;
    private final MenuHover hover;
    private final RailMode mode;
    private final HTMLElement rail = el("nav");
    private final HTMLElement scrim = el("div");
    private final HTMLElement menu = el("md-icon-button");
    private final HTMLElement column = el("md-navigation-rail");
    private final HTMLElement foot = el("div");
    private final HTMLElement bar = el("md-navigation-bar");
    // 기다리는 수의 읽는 몫. 컴포넌트의 버튼은 라벨을 제 이름으로 고정해서, 옛 레일이 문 이름에
    // 싣던 "N명이 기다린다"를 실을 자리가 없다 — 그래서 보이지 않는 한 줄이 그것을 말한다.
    private final HTMLElement waitingNote = el("span");
    private final Map<String, HTMLElement> items = new LinkedHashMap<>();
    private final Map<String, HTMLElement> tabs = new LinkedHashMap<>();
    private int waiting = 0;
    private Destination current = null;

    @Inject
    public RailElement(Navigation nav, RosterStore roster, MenuHover hover, RailMode mode,
                       MayStore may, ToolRailElement panel) {
        this.nav = nav;
        this.hover = hover;
        this.mode = mode;
        build(panel);
        roster.subscribe(this::countWaiting);
        // 말이 바뀌면 문 이름도 바뀐다 — 사람이 언어를 갈면 이 기둥부터 눈에 들어온다.
        dev.sayaya.magi.bridge.Labels.onPack(this::paint);
        mode.subscribe(this::applyModes);
        // 능력이 늦게 도착해도 문은 따라 접힌다 — 취하는 쪽만: 숨긴 것을 되살리진 않는다(운영 applyMay).
        may.subscribe(n -> {
            for (Destination d : Destination.doors()) {
                if (may.may(d.may)) continue;
                HTMLElement item = items.get(d.id), tab = tabs.get(d.id);
                if (item != null) item.setAttribute("hidden", "");
                if (tab != null) tab.setAttribute("hidden", "");
            }
        });
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

    /** 문의 말들 — 짧은 라벨·한 줄 설명 전부 팩에서. 언어가 바뀌면 다시 부른다. */
    public void paint() {
        menu.setAttribute("aria-label", tr("nav.menu"));
        for (Destination d : Destination.doors()) {
            // 보이는 낱말은 짧은 이름이다 — 접힌 기둥에서도 열린 서랍에서도 폰 바에서도.
            // 긴 이름은 크럼과 제목이 지킨다(컴포넌트의 버튼은 라벨을 제 이름으로 읽힌다).
            HTMLElement item = items.get(d.id), tab = tabs.get(d.id);
            if (item != null) {
                Js.asPropertyMap(item).set("label", tr(d.shortKey));
                Element sub = item.querySelector("[slot=supporting-text]");
                if (sub != null) sub.textContent = tr(d.subKey);
            }
            if (tab != null) Js.asPropertyMap(tab).set("label", tr(d.shortKey));
        }
        sayWaiting();
    }

    @Override
    public void select(Destination d) {
        current = d;
        int railIndex = -1, barIndex = -1, i = 0, j = 0;
        for (Destination one : Destination.doors()) {
            boolean on = one.id.equals(d.id);
            HTMLElement item = items.get(one.id), tab = tabs.get(one.id);
            mark(item, on);
            mark(tab, on);
            if (one.atFoot) {
                // 발치의 문은 레일의 목적지 목록(activeIndex) 밖이라 제 손으로 켠다.
                Js.asPropertyMap(item).set("active", on);
            } else {
                if (on) railIndex = i;
                i++;
            }
            if (on) barIndex = j;
            j++;
        }
        Js.asPropertyMap(column).set("activeIndex", railIndex);
        Js.asPropertyMap(bar).set("activeIndex", barIndex);
    }

    /** 두 이름 다: CSS·시험은 [selected]를 읽고, aria-current="page"는 그 문이 여기라고 말한다. */
    private static void mark(HTMLElement e, boolean on) {
        if (e == null) return;
        // 레일 항목은 링크라 컴포넌트가 제 <a>에 aria-current를 단다(2.5.0.6) — 호스트에 또 달면 두 번 읽힌다.
        boolean link = e.hasAttribute("href");
        if (on) {
            e.setAttribute("selected", "");
            if (!link) e.setAttribute("aria-current", "page");
        } else {
            e.removeAttribute("selected");
            e.removeAttribute("aria-current");
        }
    }

    // ── 배지 ─────────────────────────────────────────────────────────────────

    /** 문에 붙는 수는 기다리는 사람 수다 — 0이면 배지 자체가 없다: 늘 있는 배지는 안 읽힌다. */
    private void countWaiting(FleetAgent[] list) {
        if (list == null) return;
        int n = 0;
        for (FleetAgent a : list) if ("waiting".equals(a.state)) n++;
        waiting = n;
        String value = n > 999 ? "999+" : String.valueOf(n);
        for (HTMLElement e : new HTMLElement[]{items.get(Destination.FLEET.id), tabs.get(Destination.FLEET.id)}) {
            if (e == null) continue;
            Js.asPropertyMap(e).set("badgeValue", value);
            Js.asPropertyMap(e).set("showBadge", n > 0);
        }
        sayWaiting();
    }

    /** 0이면 빈 줄 — 늘 있는 말은 안 읽힌다(배지와 같은 규칙). */
    private void sayWaiting() {
        waitingNote.textContent = waiting > 0 ? tr("state.waiting_on_you", "n", String.valueOf(waiting)) : "";
    }

    // ── 마크업 ───────────────────────────────────────────────────────────────

    private void build(ToolRailElement panel) {
        scrim.id = "scrim";
        scrim.addEventListener("click", evt -> close());
        rail.id = "rail";
        // 버거는 다섯 획: 위·아래 막대가 반씩이라, 열리면 네 반쪽이 X의 팔이 되고
        // 가운데 획만 제 중심으로 걷혀 사라진다 — 아이콘 교체가 아니라 같은 획의 이동.
        // 레일 내장 메뉴 버튼은 아이콘 폰트(Material Symbols)를 부르므로 쓰지 않는다. 그리고 기둥
        // 안(header 슬롯)이 아니라 #rail의 직계다: 도구 있는 문에서 메뉴 기둥이 숨어도(menu=hide)
        // 서랍을 여는 손잡이는 남아야 한다.
        menu.id = "railMenu";
        menu.setAttribute("aria-expanded", "false");
        menu.innerHTML = "<svg class=\"burger\" viewBox=\"0 0 24 24\" width=\"22\" height=\"22\" aria-hidden=\"true\">"
                + "<line class=\"bt bl\" x1=\"3\" y1=\"6\" x2=\"12\" y2=\"6\"/>"
                + "<line class=\"bt br\" x1=\"12\" y1=\"6\" x2=\"21\" y2=\"6\"/>"
                + "<line class=\"bm\" x1=\"3\" y1=\"12\" x2=\"21\" y2=\"12\"/>"
                + "<line class=\"bb bl\" x1=\"3\" y1=\"18\" x2=\"12\" y2=\"18\"/>"
                + "<line class=\"bb br\" x1=\"12\" y1=\"18\" x2=\"21\" y2=\"18\"/></svg>";
        menu.addEventListener("click", evt -> toggle());
        column.id = "railNav";
        // 펼친 레일의 선택 표시는 항목 폭을 채운다 — 옛 레일의 행 전체 하이라이트. 레일은 직계 항목에만
        // 전파하므로 발치 항목은 아래에서 따로 켠다.
        Js.asPropertyMap(column).set("fullWidthIndicator", true);
        // 발치는 운영이 접근 제어 문에게 준 자리다 — 매일 다니는 문들과 한 줄에 서지 않는다.
        foot.id = "railFoot";
        foot.setAttribute("slot", "footer");
        bar.id = "railBar";
        for (Destination d : Destination.doors()) {
            HTMLElement item = item(d, "md-navigation-rail-item");
            item.append(supporting());
            items.put(d.id, item);
            if (d.atFoot) Js.asPropertyMap(item).set("fullWidthIndicator", true);
            (d.atFoot ? foot : column).append(item);
            HTMLElement tab = item(d, "md-navigation-tab");
            tabs.put(d.id, tab);
            bar.append(tab);
        }
        column.append(foot);
        waitingNote.id = "railWaiting";
        waitingNote.className = "sr-only";
        waitingNote.setAttribute("aria-live", "polite");
        rail.append(menu, column, bar, waitingNote);
        // 툴 레일 — 언제 어떤 모습인지는 RailMode(#rail의 menu/tool 속성)가 말한다.
        rail.append(panel.element());
        // 호버 터널: 레일 전체를 벗어날 때만 피크를 접는다. 접힌 툴 기둥의 라벨 피크는
        // 레일 위에 손끝이 있다는 사실 자체가 켠다(RailMode.hover).
        rail.addEventListener("mouseenter", evt -> mode.hover(true));
        rail.addEventListener("mouseleave", evt -> { mode.hover(false); hover.next(null); });
    }

    private static HTMLElement supporting() {
        HTMLElement sub = el("span");
        sub.setAttribute("slot", "supporting-text");
        // .sub: console.css가 이 줄을 "폭이 다 벌어진 뒤(250ms) 페이드인"으로 들인다(railSubIn) —
        // 옛 레일의 그 규칙을 이 항목에도 걸리게 하는 이름이다.
        sub.className = "sub";
        return sub;
    }

    /**
     * 목적지 하나 — 레일 항목이든 폰 바의 탭이든 같은 문이다. 일반 클릭은 셸의 항해로, 수식 키와
     * 가운데 클릭은 새 탭으로(앵커가 하던 일 중 이벤트로 옮길 수 있는 몫).
     */
    private HTMLElement item(Destination d, String tag) {
        HTMLElement e = el(tag);
        // .dest, not .raili: .raili is the tool rail's hand-drawn item, and console.css still draws it.
        e.className = "dest";
        String href = d == Destination.FLEET ? Windows.here() : Windows.here() + "?v=" + d.id;
        e.setAttribute("data-href", href);
        // data-i: 스프라이트가 있으면 Icons.dress가 이 도형을 그 그림으로 갈아입힌다(운영 규칙).
        HTMLElement icon = el("span");
        icon.setAttribute("slot", "inactive-icon");
        icon.className = "icwrap";
        icon.innerHTML = "<svg data-i=\"" + d.iconRef + "\" class=\"ic\" viewBox=\"0 0 24 24\" width=\"24\" height=\"24\" aria-hidden=\"true\">"
                + "<path d=\"" + d.iconPath + "\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.6\" "
                + "stroke-linecap=\"round\" stroke-linejoin=\"round\"/></svg>";
        e.append(icon);
        if ("md-navigation-rail-item".equals(tag)) {
            // 진짜 링크다(2.5.0.6: href → <a href>) — 가운데 클릭·수식 키·우클릭의 "링크 주소 복사"·
            // 주소 미리보기는 브라우저가 한다. 셸은 평범한 클릭만 가로채 제 항해로 돌린다. 발치의 문은
            // 레일의 목적지 목록 밖이라 activated가 오지 않으므로 클릭에서 직접 막는다.
            e.setAttribute("href", href);
            e.addEventListener("click", evt -> {
                MouseEvent me = Js.uncheckedCast(evt);
                if (me.metaKey || me.ctrlKey || me.shiftKey || me.altKey || me.button != 0) {
                    // 새 탭으로 간 문은 여기가 아니다 — 컴포넌트가 옮겼을지 모를 선택을 되돌린다.
                    if (current != null) select(current);
                    return;
                }
                evt.preventDefault();
                nav.go(d);
            });
            e.addEventListener("mouseenter", evt -> {
                if ("open".equals(DomGlobal.document.body.getAttribute("nav"))) hover.next(d);
            });
            return e;
        }
        // 폰 바의 탭은 아직 버튼이다 — 링크가 하던 새 탭 열기를 이벤트로 대신한다.
        e.addEventListener("click", evt -> {
            MouseEvent me = Js.uncheckedCast(evt);
            if (me.metaKey || me.ctrlKey || me.shiftKey) {
                evt.stopPropagation();
                DomGlobal.window.open(href, "_blank");
                // 컴포넌트는 안쪽 클릭으로 제 선택을 이미 옮겼다 — 새 탭으로 간 문은 여기가 아니다.
                if (current != null) select(current);
                return;
            }
            nav.go(d);
        });
        // 가운데 클릭은 click이 아니라 auxclick으로 온다 — 버튼에는 브라우저가 해 주던 일이 없다.
        e.addEventListener("auxclick", evt -> {
            MouseEvent me = Js.uncheckedCast(evt);
            if (me.button != 1) return;
            evt.preventDefault();
            DomGlobal.window.open(href, "_blank");
        });
        // 피크는 열린 드로어의 것 — 접힌 레일에서 2단은 없고, 호버는 아무 말도 아니다.
        e.addEventListener("mouseenter", evt -> {
            if ("open".equals(DomGlobal.document.body.getAttribute("nav"))) hover.next(d);
        });
        return e;
    }

    // ── 열고 닫기 ────────────────────────────────────────────────────────────
    // 운영 콘솔의 2속성 기계 그대로: nav=open은 폭이고 nav-wide는 모양이다. 레일의 expanded는
    // 모양이라 nav-wide와 함께 움직인다 — 열 때도 닫을 때도 폭 트랜지션(250ms)이 끝난 뒤에 바뀐다.
    // 닫을 때는 폭 트랜지션 중에 낱말이 먼저 무너지지 않게(원본의 그 지연). 열 때는 sayaya-ui 레일의
    // 펼침 모션이 expanded가 바뀌는 순간 도착 자리를 재기 때문이다: 틀이 아직 96px일 때 재면 그 폭을
    // inline으로 붙든 채 서랍이 넓어지고, 모션이 끝나는 순간 303px로 튄다(측정: 79px로 450ms, 한 프레임에 +224px).

    private double widePending = -1;

    private void toggle() {
        if ("open".equals(DomGlobal.document.body.getAttribute("nav"))) { close(); return; }
        DomGlobal.document.body.setAttribute("nav", "open");
        menu.setAttribute("aria-expanded", "true");
        shapeAfterWidth(true);
        mode.drawer(true);
    }

    private void close() {
        DomGlobal.document.body.removeAttribute("nav");
        menu.setAttribute("aria-expanded", "false");
        shapeAfterWidth(false);
        hover.next(null);
        mode.drawer(false);
    }

    private void shapeAfterWidth(boolean wide) {
        if (widePending >= 0) DomGlobal.clearTimeout(widePending);
        widePending = DomGlobal.setTimeout(a -> {
            if (wide) DomGlobal.document.body.setAttribute("nav-wide", "");
            else DomGlobal.document.body.removeAttribute("nav-wide");
            expand(wide);
            widePending = -1;
        }, 250);
    }

    /** 레일과 발치의 항목 — 발치는 레일 직계가 아니라 레일이 전파하지 않는다. */
    private void expand(boolean on) {
        Js.asPropertyMap(column).set("expanded", on);
        for (Destination d : Destination.doors()) {
            if (d.atFoot) Js.asPropertyMap(items.get(d.id)).set("expanded", on);
        }
    }

    // ── 잔손 ─────────────────────────────────────────────────────────────────

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
