package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.StateMark;
import dev.sayaya.magi.client.domain.Roster;
import dev.sayaya.magi.client.usecase.FleetStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import elemental2.dom.NodeList;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.List;
import java.util.Map;

import static dev.sayaya.magi.bridge.Labels.stateWord;
import static dev.sayaya.magi.bridge.Labels.tr;
import static dev.sayaya.magi.client.interfaces.Dom.cell;
import static dev.sayaya.magi.client.interfaces.Dom.el;
import static dev.sayaya.magi.client.interfaces.Dom.srOnly;

/**
 * 플릿 화면의 루트 — 요약 타일 줄(#summary)과 표(#fleet). 마크업·클래스는 기존 콘솔
 * page.js와 같고 CSS(page.css→console.css)가 그 클래스를 읽는다: 이름이 계약이다.
 *
 * 상태는 스토어가 소유하고 이 요소는 그린다. 필터만 이 화면의 것이다(보는 방식이지
 * 명단의 사실이 아니라서).
 */
@Singleton
public class FleetElement {
    private final FleetStore store;
    private final CardListElement cards;
    private final HTMLElement summary = el("md-chip-set");
    private final HTMLElement fleetEl = el("div");
    private String filter = null;      // 요약 칩 키 하나, 또는 전부(null)
    private FleetAgent[] last = null;  // 필터 클릭이 재조회 없이 다시 그릴 수 있게
    private boolean wired = false;     // 재방문 마운트가 구독을 겹으로 쌓지 않게

    @Inject
    public FleetElement(FleetStore store, CardListElement cards) {
        this.store = store;
        this.cards = cards;
    }

    public void mount(HTMLElement frame) {
        summary.id = "summary";
        fleetEl.id = "fleet";
        frame.replaceChildren();
        frame.append(summary, fleetEl);
        reading();
        // 셸이 목적지 재방문마다 렌더를 다시 부른다 — 구독은 첫 마운트의 것 하나면 된다.
        if (!wired) {
            wired = true;
            store.subscribe(this::take);
        }
        store.start();
    }

    private void take(FleetAgent[] listOrNull) {
        if (listOrNull == null) { paneFailed(); return; }
        last = listOrNull;
        cards.prune(listOrNull);
        render();
    }

    private void render() {
        if (last == null) return;
        int waiting = Roster.waiting(last);
        retitle(waiting);
        summarise(last);

        if (last.length == 0) {
            fleetEl.replaceChildren();
            fleetEl.append(emptyState("empty.no_agents", "empty.no_agents_how"));
            return;
        }
        List<FleetAgent> rows = Roster.rows(last, filter);
        // 이제 아무것도 안 걸리는 필터는 그렇다고 말하고 나갈 길을 내놓는다 — 켜진 칩 아래
        // 맨 표 머리만 남기면 독자가 갇힌다.
        if (filter != null && rows.isEmpty()) {
            HTMLElement note = cell("capnote", null);
            note.append(cell("", tr("filter.only", "state", stateWord(filter))));
            HTMLElement all = el("md-text-button");
            all.textContent = tr("action.show_all");
            all.addEventListener("click", evt -> { filter = null; render(); });
            note.append(all);
            fleetEl.replaceChildren();
            fleetEl.append(tableHead(), note);
            return;
        }
        String newest = Roster.newest(last);
        fleetEl.replaceChildren();
        fleetEl.append(tableHead());
        if (!Roster.teamed(rows)) {
            for (FleetAgent a : rows) fleetEl.append(row(a, newest));
        } else {
            for (Roster.Team t : Roster.teams(rows)) {
                fleetEl.append(teamHead(t));
                for (FleetAgent a : t.members) fleetEl.append(row(a, newest));
            }
        }
    }

    private HTMLElement row(FleetAgent a, String newest) {
        return cards.row(a, newest, store::refresh, this::jumpToNextWaiting);
    }

    /** 탭 제목이 대기 수를 실어 나른다 — 배경 탭에 남겨진 대시보드에 닿는 유일한 채널. */
    private void retitle(int waiting) {
        String name = "magi · " + tr("nav.companions");
        DomGlobal.document.title = waiting > 0 ? "(" + waiting + ") " + name : name;
    }

    /** 네 개의 숫자와 필터 — "뭔가 나를 필요로 하나"에 행을 세지 않고 답하는 줄. */
    /** 이 목록에서 나가는 길 하나 — 같은 모양, 같은 자리(운영 .toview). */
    private HTMLElement toView(String view, String labelKey, boolean lead, String ref, String path) {
        HTMLElement b = el("md-icon-button");
        b.className = "toview" + (lead ? " lead" : "");
        b.setAttribute("aria-label", tr(labelKey));
        b.innerHTML = "<svg data-i=\"" + ref + "\" viewBox=\"0 0 24 24\" width=\"20\" height=\"20\" aria-hidden=\"true\">"
                + "<path d=\"" + path + "\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.6\" "
                + "stroke-linecap=\"round\" stroke-linejoin=\"round\"/></svg>";
        Icons.dress(b);
        b.addEventListener("click", evt -> GoSharing.view(view));
        return b;
    }

    private void summarise(FleetAgent[] list) {
        summary.replaceChildren();
        for (Map.Entry<String, Integer> e : Roster.counts(list).entrySet()) {
            String k = e.getKey();
            int n = e.getValue();
            HTMLElement b = el("md-filter-chip");
            b.className = "tile " + k;
            JsPropertyMap<Object> pm = Js.asPropertyMap(b);
            // 0인 칩은 soft-disabled(발표는 되고 누름만 죽음), 켜진 필터는 절대 죽이지 않는다 —
            // 명단이 흘러가 0이 된 필터를 끌 수 없으면 그 아래 빈 표에 갇힌다.
            pm.set("softDisabled", n == 0 && !k.equals(filter));
            pm.set("alwaysFocusable", true);
            pm.set("selected", k.equals(filter));
            // 상태의 마크가 칩의 아이콘 슬롯에 — 수와 낱말은 그대로다(마크는 셋째 방식이지
            // 대체가 아니다). 스프라이트 없는 빌드에선 마크가 없고, 그것도 정상이다.
            elemental2.dom.Element mark = Icons.of(StateMark.of(k), null);
            if (mark != null) {
                mark.setAttribute("slot", "icon");
                b.append(mark);
            }
            b.append(cell("n", String.valueOf(n)), cell("k", stateWord(k)));
            b.addEventListener("click", evt -> {
                filter = k.equals(filter) ? null : k;
                render();
                if (filter != null) jumpToFirstRow();
            });
            summary.append(b);
        }
        // 이 목록에서 나가는 길들 — 같은 모양, 한 자리(운영 .toview). 볼 것이 있을 때만.
        if (last != null && last.length > 0) {
            summary.append(toView("board", "nav.board", true, "#i-sl-chart-kanban",
                    "M4 5.5h5v13H4zM9.5 5.5h5v8h-5zM15 5.5h5v10.5h-5z"));
        }
        // 맵은 하나뿐일 때 상자 속 상자다 — 볼 것이 둘부터(운영 규칙).
        if (last != null && last.length > 1) {
            summary.append(toView("map", "nav.map", false, "#i-sl-share-from-square",
                    "M12 4.2a2 2 0 1 1 0 4 2 2 0 0 1 0-4M6 15.8a2 2 0 1 1 0 4 2 2 0 0 1 0-4"
                            + "M18 15.8a2 2 0 1 1 0 4 2 2 0 0 1 0-4M12 8.2v3.6M12 11.8H6v4M12 11.8h6v4"));
        }
    }

    private HTMLElement tableHead() {
        HTMLElement h = cell("thead", null);
        String[][] cols = {{"", "col.status"}, {"", "col.agent"}, {"", "col.doing"},
                           {"r", "col.steps"}, {"r", "col.age"}, {"", "col.host"}, {"r", ""}};
        for (String[] c : cols) h.append(cell(c[0], c[1].isEmpty() ? "" : tr(c[1])));
        return h;
    }

    /** 팀 이름과 그 팀이 말해야 할 두 사실: 누가 대변하나, 몇이 기다리나. */
    private HTMLElement teamHead(Roster.Team t) {
        HTMLElement h = el("h3");
        h.className = "teamhead";
        h.append(cell("tname", t.name.isEmpty() ? tr("team.none") : t.name));
        List<String> hubs = t.hubs();
        if (!hubs.isEmpty()) h.append(cell("thub", tr("team.spoken_for", "name", String.join(", ", hubs))));
        int waiting = t.waiting();
        if (waiting > 0) {
            HTMLElement b = el("md-badge");
            Js.asPropertyMap(b).set("value", String.valueOf(waiting));
            b.setAttribute("aria-hidden", "true");
            h.append(b, srOnly(tr("state.waiting_on_you", "n", String.valueOf(waiting))));
        }
        HTMLElement tn = cell("tn", String.valueOf(t.members.size()));
        tn.setAttribute("aria-hidden", "true");
        h.append(tn, srOnly(tr(t.members.size() == 1 ? "count.agent" : "count.agents",
                "n", String.valueOf(t.members.size()))));
        return h;
    }

    // 답하기는 브라우즈가 아니라 큐다: 답하면 다음 기다리는 행으로 화면이 옮겨간다.
    private void jumpToNextWaiting(String justAnswered) {
        DomGlobal.requestAnimationFrame(ts -> {
            NodeList<Element> rows = fleetEl.querySelectorAll(".card.waiting");
            for (int i = 0; i < rows.length; i++) {
                Element row = rows.getAt(i);
                // 방금 답한 행은 한 틱 더 waiting으로 그려질 수 있다 — 돌아가 앉으면 답이
                // 안 먹힌 것처럼 보인다. 답 상자 없는 행(남의 콘솔 몫)도 건너뛴다.
                if (justAnswered != null && justAnswered.equals(row.getAttribute("data-socket"))) continue;
                if (row.querySelector(".answer") == null) continue;
                row.scrollIntoView();
                return;
            }
        });
    }

    private void jumpToFirstRow() {
        DomGlobal.requestAnimationFrame(ts -> {
            Element first = fleetEl.querySelector(".card");
            if (first != null) first.scrollIntoView();
        });
    }

    private void reading() {
        if (fleetEl.childElementCount > 0) return;
        fleetEl.replaceChildren();
        fleetEl.append(cell("paneloading", tr("loading.roster")));
    }

    /** 첫 로드의 거부만 빈칸을 대신한다 — 이후의 거부는 화면을 그대로 둔다(낡고-말한 것 > 빈 것). */
    private void paneFailed() {
        boolean loadingOnly = fleetEl.childElementCount == 1
                && fleetEl.firstElementChild.classList.contains("paneloading");
        if (fleetEl.childElementCount > 0 && !loadingOnly) return;
        fleetEl.replaceChildren();
        fleetEl.append(emptyState("error.pane", "error.pane_how"));
    }

    private static HTMLElement emptyState(String whatKey, String howKey) {
        HTMLElement e = el("div");
        e.className = "empty";
        // 팩은 이 서버가 서빙·임베드한다; 컴패니언도 네트워크도 여기 못 닿는다 — HTML이어도 된다.
        e.innerHTML = tr(whatKey) + "<br>" + tr(howKey);
        return e;
    }
}
