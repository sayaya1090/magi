package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.usecase.MenuHover;
import dev.sayaya.magi.client.usecase.Navigation;
import dev.sayaya.magi.client.usecase.RosterStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import java.util.ArrayList;
import java.util.List;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.stateWord;
import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 드로어의 2단 — handbook 툴 레일의 번역. 1단이 "어디로"라면 2단은 "그 안의 무엇":
 * 호버가 가리키는 문(피크), 없으면 서 있는 문의 속을 보인다.
 *
 * 컴패니언 문의 속은 실제 명단이다 — 셸이 이미 소유한 스트림(RosterStore)에서 읽으므로
 * 요청 하나 늘지 않는다. 항목은 그 컴패니언 화면으로 가는 문이다 — 타입이 정한 모듈이
 * 뜬다(코딩 에이전트면 companion-ui).
 */
@Singleton
public class ToolRailElement {
    private final Navigation nav;
    private final HTMLElement panel = el("div");
    private Destination selected = null;   // 손끝이 떠났을 때의 복귀처 — 서 있는 문
    private Destination showing = null;
    private FleetAgent[] roster = null;

    @Inject
    public ToolRailElement(Navigation nav, MenuHover hover, RosterStore store) {
        this.nav = nav;
        panel.id = "railPanel";
        // 피크가 우선, 손끝이 비면 선택된 문 — 렌더는 늘 "무엇을 보일까"의 결과다.
        hover.subscribe(d -> show(d != null ? d : selected));
        nav.subscribe(place -> {
            selected = place.section;
            if (hover.current() == null) show(place.section);
        });
        store.subscribe(list -> {
            if (list == null) return;   // 못 읽음은 마스트헤드의 점이 말한다; 여긴 마지막 앎
            roster = list;
            if (showing != null) render();
        });
    }

    public HTMLElement element() { return panel; }

    private void show(Destination d) {
        if (d == null) return;
        showing = d;
        render();
    }

    private void render() {
        panel.replaceChildren();
        HTMLElement head = el("div");
        head.className = "railpanel-head";
        head.textContent = tr(showing.labelKey);
        HTMLElement sub = el("div");
        sub.className = "railpanel-sub";
        sub.textContent = tr(showing.subKey);
        panel.append(head, sub);
        // 문마다 제 속이 있다. 지금은 컴패니언 문 하나 — 셋째 문이 생기면 이 분기가
        // 목적지별 제공자(포트)로 승격된다.
        if (Destination.FLEET.id.equals(showing.id)) fleetEntries();
    }

    private void fleetEntries() {
        if (roster == null || roster.length == 0) {
            HTMLElement none = el("div");
            none.className = "railpanel-empty";
            none.textContent = tr("empty.no_agents");
            panel.append(none);
            return;
        }
        // 골칫거리 먼저 — 표와 같은 순서. 정본은 fleet-ui의 domain에 있다; 셋째 사용처가
        // 생기면 bridge로 승격해 한 벌로 줄인다.
        List<FleetAgent> rows = new ArrayList<>();
        for (FleetAgent a : roster) rows.add(a);
        rows.sort((x, y) -> {
            int d = Boolean.compare(x.elsewhere, y.elsewhere);
            return d != 0 ? d : rank(x.state) - rank(y.state);
        });
        for (FleetAgent a : rows) {
            HTMLElement item = el("a");
            item.className = "subitem " + group(a.state);
            HTMLElement dot = el("span");
            dot.className = "sdot";
            HTMLElement name = el("span");
            name.textContent = a.name;
            HTMLElement word = el("span");
            word.className = "sword";
            word.textContent = stateWord(a.state);
            item.append(dot, name, word);
            // 항목은 그 컴패니언 화면으로 가는 문 — 이동은 셸의 Navigation이 진다.
            final String socket = a.socket;
            final String peer = a.peer;
            item.addEventListener("click", evt -> {
                evt.preventDefault();
                nav.goCompanion(socket, peer);
            });
            panel.append(item);
        }
    }

    private static int rank(String s) {
        if (s == null) return 2;
        switch (s) {
            case "waiting": return 0;
            case "working": return 1;
            case "idle": return 2;
            case "abandoned": return 3;
            case "stopped": return 4;
            default: return 5;
        }
    }

    private static String group(String s) {
        if (s == null) return "idle";
        switch (s) {
            case "waiting": case "working": case "idle": return s;
            case "abandoned": case "stopped": return "gone";
            case "remote": return "remote";
            default: return "idle";
        }
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
