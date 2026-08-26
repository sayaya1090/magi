package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.usecase.Navigation;
import dev.sayaya.magi.client.usecase.RosterStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 마스트헤드 — 기존 콘솔 #masthead의 이식: 브랜드, 어느 magi인지(user@host), 크럼, 그리고
 * 말하는 한 곳(#state: 연결 점 + 컴패니언 수 + "N명이 기다림" 점프). id·클래스는
 * page.css(→console.css)가 읽는 계약이다.
 *
 * 점은 셋을 색이 아니라 모양으로 가른다(CSS): live는 작은 점+할로, asking은 굵은 점,
 * lost는 빈 고리 — 색맹인 독자도 같은 사실을 읽는다.
 */
@Singleton
public class MastheadElement {
    private final Navigation nav;
    private final HTMLElement header = el("header");
    private final HTMLElement whereami = el("span");
    private final HTMLElement back = el("a");
    private final HTMLElement state = el("span");
    private String said = "";   // 폴마다 같은 문장을 다시 발표하지 않는 라이브영역 가드

    @Inject
    public MastheadElement(RosterStore roster, Navigation nav) {
        this.nav = nav;
        build();
        roster.subscribe(this::count);
        roster.subscribeLink(up -> {
            state.classList.toggle("live", up);
            state.classList.toggle("lost", !up);
        });
        whoami();
        scrolledMark();
    }

    public HTMLElement element() { return header; }

    /** 언어가 정해진 뒤의 말들 — 크럼의 이름. 팩이 바뀌면 다시 부른다. */
    public void paint() {
        back.textContent = tr("nav.companions");
    }

    private void build() {
        header.id = "masthead";
        HTMLElement mark = el("h1");
        mark.className = "mark";
        mark.textContent = "MAGI";
        whereami.id = "whereami";
        whereami.className = "whereami";
        HTMLElement crumbs = el("nav");
        crumbs.id = "crumbs";
        back.id = "back";
        back.setAttribute("href", "/next");
        // 목록 화면에서 크럼은 서 있는 곳 그 자체다 — 링크처럼 그리지 않는다(CSS .here).
        back.className = "here";
        back.addEventListener("click", evt -> { evt.preventDefault(); nav.go(Destination.FLEET); });
        crumbs.append(back);
        state.id = "state";
        state.setAttribute("role", "status");
        state.setAttribute("aria-live", "polite");
        header.append(mark, whereami, crumbs, state);
    }

    /** 몇이 있고 몇이 기다리는가 — 그리고 기다림은 누르면 그리로 간다. */
    private void count(dev.sayaya.magi.bridge.FleetAgent[] list) {
        if (list == null) return;   // 못 읽음은 점(lost)이 말한다; 수는 마지막 앎을 지킨다
        int waiting = 0;
        for (dev.sayaya.magi.bridge.FleetAgent a : list) if ("waiting".equals(a.state)) waiting++;
        String n = String.valueOf(list.length);
        String sentence = tr(list.length == 1 ? "count.agent" : "count.agents", "n", n)
                + (waiting > 0 ? " · " : "");
        String whole = sentence + (waiting > 0 ? tr("state.waiting_on_you", "n", String.valueOf(waiting)) : "");
        // 폴리트 라이브영역: 같은 문장의 재구축은 재발표다 — 바뀐 때만 다시 쓴다.
        boolean same = whole.equals(said);
        said = whole;
        state.classList.toggle("asking", waiting > 0);
        if (same) return;
        state.replaceChildren();
        HTMLElement count = el("div");
        count.className = "scount";
        count.textContent = sentence;
        state.append(count);
        if (waiting > 0) {
            HTMLElement go = el("md-text-button");
            go.className = "jump";
            HTMLElement full = el("div");
            full.className = "jfull";
            full.textContent = tr("state.waiting_on_you", "n", String.valueOf(waiting));
            HTMLElement brief = el("div");
            brief.className = "jshort";
            brief.textContent = tr("state.waiting_short", "n", String.valueOf(waiting));
            go.append(full, brief);
            go.setAttribute("aria-label", tr("state.waiting_on_you", "n", String.valueOf(waiting)));
            go.addEventListener("click", evt -> {
                nav.go(Destination.FLEET);
                DomGlobal.requestAnimationFrame(ts -> {
                    Element row = DomGlobal.document.querySelector("#fleet .card.waiting");
                    if (row != null) row.scrollIntoView();
                });
            });
            state.append(go);
        }
    }

    /** 어느 magi인가 — user@host, 콘솔이 말할 수 있는 반쪽이라도. 못 말하면 빈 채로 둔다:
     *  "unknown"이라 적힌 마스트헤드는 주장하지 않는 것보다 나쁘다. */
    private void whoami() {
        Console.fetchList("/console", parsed -> {
            if (parsed == null) return;
            JsPropertyMap<Object> c = Js.asPropertyMap(parsed);
            String user = str(c, "user"), host = str(c, "host");
            whereami.textContent = !user.isEmpty() && !host.isEmpty() ? user + "@" + host
                    : !host.isEmpty() ? host : user;
        });
    }

    /** 페이지가 움직였다는 표시 — 바의 채움과 헤어라인이 이 속성을 읽는다(body[scrolled]). */
    private void scrolledMark() {
        final boolean[] was = {false};
        DomGlobal.window.addEventListener("scroll", evt -> {
            boolean now = DomGlobal.window.scrollY > 0;
            if (now == was[0]) return;
            was[0] = now;
            if (now) DomGlobal.document.body.setAttribute("scrolled", "");
            else DomGlobal.document.body.removeAttribute("scrolled");
        });
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
