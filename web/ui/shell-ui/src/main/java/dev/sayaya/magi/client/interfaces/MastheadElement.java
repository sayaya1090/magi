package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Icons;

import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.bridge.Facts;
import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.Place;
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
    private final RosterStore roster;
    private final HTMLElement header = el("header");
    private final HTMLElement whereami = el("span");
    private final HTMLElement back = el("a");
    // 계단은 링크다 — 한 겹 들어간 화면에서 이것이 대화로 돌아가는 길이고, 서 있는 자리일
    // 때도 같은 요소여야 두 상태가 같은 계단으로 읽힌다(운영도 <a>다).
    private final HTMLElement deep = el("a");
    private final HTMLElement state = el("span");
    private final HTMLElement chrome = el("span");   // 화면이 미는 창 손잡이의 자리
    private final HTMLElement gear = el("md-icon-button");   // 환경설정으로 가는 문
    private final HTMLElement palBtn = el("md-icon-button");  // ⌘K가 없는 손을 위한 같은 문
    private final PaletteElement palette;
    private String said = "";   // 폴마다 같은 문장을 다시 발표하지 않는 라이브영역 가드
    private Place standing = null;

    @Inject
    public MastheadElement(RosterStore roster, Navigation nav, PaletteElement palette) {
        this.nav = nav;
        this.roster = roster;
        this.palette = palette;
        build();
        nav.subscribe(place -> {
            standing = place;
            crumbs();
            // 자리가 바뀌면 그 줄의 몫도 바뀐다 — 목록에서는 수를, 컴패니언 곁에서는 점만.
            count(lastRoster);
        });
        roster.subscribe(list -> { count(list); crumbs(); });
        roster.subscribeLink(up -> {
            state.classList.toggle("live", up);
            state.classList.toggle("lost", !up);
        });
        whoami();
        scrolledMark();
        // 화면이 제 손잡이를 놓을 자리를 연다 — 셸은 그것이 무엇을 여는지 모른다.
        dev.sayaya.magi.bridge.ChromeSharing.host(render ->
                Js.<dev.sayaya.magi.bridge.Render>cast(render).onInvoke(chrome));
        // 본문 위에 무엇이 얹히는지는 화면이 안다(폰의 탭 줄) — 그 자리를 재는 것은 셸이다.
        dev.sayaya.magi.bridge.ChromeSharing.hostRemeasure(this::measureShelltop);
        // 컴패니언 화면은 창 높이에 물린다: page.css가 그 높이를 calc(100dvh - shelltop)으로
        // 잡고, shelltop은 실측값이다(운영도 잰다). 재지 않으면 기둥이 창 밖으로 흘러 마지막
        // 카드가 잘린다 — 기본값 5.5rem은 이 셸의 마스트헤드 높이가 아니다.
        DomGlobal.requestAnimationFrame(ts -> measureShelltop());
        // 폭이 바뀌면 그 위에 얹히는 것도 바뀐다(폰의 탭 줄) — 다시 잰다.
        DomGlobal.window.addEventListener("resize", evt -> measureShelltop());
    }

    public HTMLElement element() { return header; }

    /** 언어가 정해진 뒤의 말들 — 크럼의 이름. 팩이 바뀌면 다시 부른다. */
    public void paint() {
        gear.setAttribute("aria-label", tr("nav.preferences"));
        // 어느 손가락인지도 말한다 — 맥이면 ⌘K, 아니면 Ctrl+K.
        String cmdKey = mac() ? "⌘K" : "Ctrl+K";
        palBtn.setAttribute("aria-label", tr("pal.head"));
        palBtn.setAttribute("title", tr("pal.head") + "  ·  " + cmdKey);
        gear.setAttribute("title", tr("nav.preferences"));
        crumbs();
    }

    /**
     * 크럼은 서 있는 곳이다: 화면이면 그 문의 이름 하나, 컴패니언이면 문(링크) + 이름.
     *
     * 그리고 두 끝에 이름을 붙인다 — 폰은 그 둘만 보이기 때문이다(운영 paintCrumbs의 규칙):
     * 마지막 계단이 서 있는 곳(.leaf, 헤드라인)이고 그 앞이 나가는 길(.up, 뒤로 화살표).
     * CSS가 마크업 모양으로 짐작하지 않고 여기서 정하는 이유는, 계단 수가 화면마다 다르고
     * 지금 무엇이 계단인지는 이 함수만 알기 때문이다. ⚠ 이걸 빠뜨리면 폰에서 컴패니언
     * 화면의 유일한 출구가 사라진다(실측: #back이 display:none).
     */
    private void crumbs() {
        Destination screen = standing == null ? Destination.FLEET : standing.screen;
        boolean inCompanion = standing != null && standing.isCompanion();
        back.textContent = tr(screen.labelKey);
        // 이름과 목적지는 같은 곳이어야 한다 — 이 계단은 <b>서 있는 곳</b>이고, 그것이 곧 그리로
        // 돌아가는 길이다(운영 paintCrumbs의 규칙). 늘 /next를 걸어 두면, 설정에서 "설정"이라
        // 적힌 계단을 눌렀을 때 명단으로 나가 버린다.
        back.setAttribute("href", Destination.FLEET.id.equals(screen.id) ? Windows.here() : Windows.here() + "?v=" + screen.id);
        back.className = inCompanion ? "" : "here";
        if (!inCompanion) {
            deep.remove();
        } else {
            deep.className = "here";
            deep.textContent = nameOf(standing.socket);
            deep.addEventListener("click", evt -> { evt.preventDefault(); nav.goPast(null); });
            // 이 계단도 링크다 — 한 겹 들어간 화면(지난 일·표결)에서 이것이 대화로 돌아가는
            // 길이고, 서 있는 자리일 때도 같은 요소여야 그 둘이 같은 계단으로 읽힌다.
            deep.setAttribute("href", Windows.here() + "?d="
                    + elemental2.core.Global.encodeURIComponent(standing.socket)
                    + (standing.peer == null || standing.peer.isEmpty() ? ""
                       : "&p=" + elemental2.core.Global.encodeURIComponent(standing.peer)));
            back.insertAdjacentElement("afterend", deep);
        }
        markRungs(inCompanion);
    }

    /** 두 끝 — 마지막은 .leaf, 그 앞은 .up. 이름은 낱말로 붙인다(화살표는 그림이다). */
    private void markRungs(boolean inCompanion) {
        back.classList.remove("up", "leaf");
        deep.classList.remove("up", "leaf");
        if (!inCompanion) {
            back.classList.add("leaf");
            back.removeAttribute("aria-label");
            return;
        }
        deep.classList.add("leaf");
        back.classList.add("up");
        // 화살표는 생성 콘텐츠라 이름에 섞인다 — 낱말이 이름을 이긴다(운영에서 실측된 그 규칙).
        back.setAttribute("aria-label", back.textContent.trim());
    }

    private String nameOf(String socket) {
        dev.sayaya.magi.bridge.FleetAgent[] list = currentRoster();
        if (list != null && socket != null) {
            for (dev.sayaya.magi.bridge.FleetAgent a : list) {
                if (socket.equals(a.socket) && a.name != null && !a.name.isEmpty()) return a.name;
            }
        }
        return socket == null ? "" : socket;
    }

    private dev.sayaya.magi.bridge.FleetAgent[] lastRoster = null;

    private dev.sayaya.magi.bridge.FleetAgent[] currentRoster() { return lastRoster; }

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
        // 목록 화면에서 크럼은 서 있는 곳 그 자체다 — 링크처럼 그리지 않는다(CSS .here).
        back.className = "here";
        back.addEventListener("click", evt -> {
            evt.preventDefault();
            nav.go(standing == null ? Destination.FLEET : standing.screen);
        });
        crumbs.append(back);
        state.id = "state";
        state.setAttribute("role", "status");
        state.setAttribute("aria-live", "polite");
        chrome.id = "chrome";
        // 톱니는 늘 그 자리다 — 환경설정은 레일의 문이 아니라 이 창의 chrome이라서(운영도
        // 마스트헤드에 둔다). 컴패니언 위에서 누르면 그 컴패니언의 설정으로 간다: 주소의
        // ?d= 가 그대로 남으므로 화면이 그 사실을 읽는다.
        gear.id = "prefs";
        gear.setAttribute("aria-label", tr("nav.preferences"));
        // data-i를 단다: 구운 스프라이트가 있는 빌드에서는 Icons.dress가 이 도형을 그것으로
        // 갈아입힌다(운영 dressIcons와 같은 거래). 없으면 여기 그린 도형이 그대로 산다 —
        // 없는 이름을 달아 두면 그 자리만 다른 알파벳을 쓰는 아이콘이 된다(실측: 이 둘만 달랐다).
        gear.innerHTML = "<svg data-i=\"#i-sl-sliders\" viewBox=\"0 0 24 24\" width=\"22\" height=\"22\" aria-hidden=\"true\">"
                + "<path d=\"M4 7h10M18 7h2M4 12h2M10 12h10M4 17h12M20 17h0\" fill=\"none\" "
                + "stroke=\"currentColor\" stroke-width=\"1.8\" stroke-linecap=\"round\"/>"
                + "<circle cx=\"16\" cy=\"7\" r=\"2\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.8\"/>"
                + "<circle cx=\"8\" cy=\"12\" r=\"2\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.8\"/>"
                + "<circle cx=\"18\" cy=\"17\" r=\"2\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.8\"/></svg>";
        gear.addEventListener("click", evt -> nav.go(Destination.SETTINGS));
        // 수식키가 없는 손에게는 이 버튼이 팔레트의 유일한 길이다 — 아무 데도 적혀 있지 않은
        // 단축키는 아무도 발견하지 못한다(운영이 이 버튼을 둔 이유).
        palBtn.id = "palOpen";
        palBtn.addEventListener("click", evt -> palette.show());
        palBtn.innerHTML = "<svg data-i=\"#i-sl-magnifying-glass\" viewBox=\"0 0 24 24\" width=\"22\" height=\"22\" aria-hidden=\"true\">"
                + "<circle cx=\"11\" cy=\"11\" r=\"6.2\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.8\"/>"
                + "<path d=\"M15.6 15.6 20 20\" stroke=\"currentColor\" stroke-width=\"1.8\" stroke-linecap=\"round\"/></svg>";
        header.append(mark, whereami, crumbs, state, chrome, palBtn, gear);
        // 그림이 구워져 있으면 지금 갈아입는다(스프라이트는 셸이 들여놓은 뒤에 온다).
        Icons.dress(header);
    }

    /** 몇이 있고 몇이 기다리는가 — 그리고 기다림은 누르면 그리로 간다. */
    private void count(dev.sayaya.magi.bridge.FleetAgent[] list) {
        if (list == null) return;   // 못 읽음은 점(lost)이 말한다; 수는 마지막 앎을 지킨다
        lastRoster = list;
        // 이 수는 <b>목록 화면의 것</b>이다 — 그 표를 세는 말이라, 다른 문 안에서는 그 화면이
        // 하는 이야기 위에 남의 수가 얹힌다(운영은 목록을 그릴 때만 이 줄을 쓴다). 컴패니언
        // 곁에서는 같은 줄이 그 컴패니언의 계단을 이고 있어 더더욱 그렇고, 860px에서는 그 수
        // 때문에 바가 두 줄로 접혀 아래 줄의 아이콘이 본문을 밀어냈다.
        boolean onList = standing != null && !standing.isCompanion()
                && dev.sayaya.magi.client.domain.Destination.FLEET.id.equals(standing.screen.id);
        if (!onList) {
            said = "";
            state.replaceChildren();
            state.classList.toggle("asking", false);
            return;
        }
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
        // 어느 magi인지는 창 전체의 사실이다 — 셸이 한 번 읽어 올린 것을 든다.
        Facts.onConsole(parsed -> {
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

    /**
     * 본문이 <b>시작되는 자리</b> — 컴패니언 기둥의 앵커.
     *
     * 마스트헤드 높이가 아니라 &lt;main&gt;의 top이다(운영 measureMasthead도 그것을 잰다):
     * 그 위에 서는 것이 마스트헤드만이 아니기 때문이다. 폰에서는 탭 줄이 하나 더 얹히고,
     * 데모에는 띠가 하나 더 있다. 마스트헤드만 재면 그 차이만큼 기둥이 창 밖으로 흘러, 전사가
     * 제 안에서 구르는 대신 페이지를 늘린다(실측: 390px에서 페이지 높이 1993px, 마지막 카드가
     * 화면 밖).
     */
    private void measureShelltop() {
        elemental2.dom.Element main = DomGlobal.document.querySelector("main");
        double top = main == null ? 0 : main.getBoundingClientRect().top;
        if (top <= 0) return;
        DomGlobal.document.documentElement.style.setProperty("--magi-comp-shelltop",
                Math.ceil(top) + "px");
    }

    /** 위에 얹히는 것이 바뀌면(폰의 탭 줄, 데모의 띠) 다시 잰다 — 그 자리는 창의 사실이다. */
    public void remeasure() { measureShelltop(); }

    /** 이 손이 어느 기계의 것인가 — 단축키를 그 기계의 말로 적으려고. */
    private static boolean mac() {
        String ua = String.valueOf(DomGlobal.navigator.userAgent).toLowerCase();
        return ua.contains("mac") || ua.contains("iphone") || ua.contains("ipad");
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
