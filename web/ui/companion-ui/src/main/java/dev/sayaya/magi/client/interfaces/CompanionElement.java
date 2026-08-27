package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.CardSharing;
import dev.sayaya.magi.bridge.ModuleInject;
import dev.sayaya.magi.bridge.Motion;
import dev.sayaya.magi.bridge.PaneSharing;
import dev.sayaya.magi.bridge.Render;
import elemental2.dom.MediaQueryList;
import dev.sayaya.magi.client.usecase.CompanionStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 컴패니언 상세의 레이아웃 — 범용이다: 어떤 타입이든 위와 오른쪽은 같은 것을 답한다.
 *
 * 위는 사실판(무엇이고 무엇을 하는 중인가), 오른쪽은 판(계획·건넨 일·예약). 가운데와
 * 왼쪽은 타입의 몫이라 자리(슬롯)만 내주고 자식이 채운다 — 부모는 무엇이 오는지 모른다.
 * 자식의 이름은 셸의 카탈로그가 풀어 컨텍스트(ui)에 실어 보낸 것이고, 컴패니언이 대는
 * 경로가 아니다: 어느 코드가 도는지는 이 콘솔이 정한다.
 *
 * 왼쪽은 여럿일 수 있다 — 자식이 left를 여러 번 밀면 순서대로 쌓인다.
 *
 * 폰에서는 세 자리를 나란히 둘 폭이 없다: 탭이 한 번에 하나를 보인다(운영의 그 규칙과 같은
 * 이름 — body[panel=talk|facts|files]). 레이아웃이 부모의 것이므로 그 전환도 부모가 진다;
 * 자식은 제 자리를 채울 뿐 자기가 지금 보이는지 모른다.
 */
@Singleton
public class CompanionElement {
    private final CompanionStore store;
    private final DetailElement detail;
    private final SideElement side;
    private final PromptElement prompt;
    private final Arrangement arrange;
    private final HTMLElement stage = el("div");      // #agentview — 세 기둥의 격자
    private final HTMLElement filecol = el("div");    // 왼쪽 기둥(자식의 것)
    private final HTMLElement stream = el("div");     // 가운데 기둥 — 사실판 + 자식의 대화
    private final HTMLElement sidecol = el("div");    // 오른쪽 기둥(부모의 판)
    private final HTMLElement leftFill = el("div");   // 자식이 채우는 껍데기(display:contents)
    private final HTMLElement centreFill = el("div");
    private final HTMLElement tabs = el("md-tabs");
    private final HTMLElement cardTabs = el("md-tabs");   // 가운데의 카드 줄(사실판 + 자식의 카드들)
    // 카드 자리는 <b>카드</b>다 — md-outlined-card. 운영의 그 요소이고, 안쪽 여백(16px)도 판의
    // 테두리도 그 태그에 걸린 규칙에서 온다: div로 세우면 본문이 테두리에 붙는다(실측 16px 차).
    private final HTMLElement cardArea = el("md-outlined-card");
    private String cardShows = "facts";
    private boolean wired = false;
    private String childLoaded = null;
    private String panel = "talk";

    @Inject
    public CompanionElement(CompanionStore store, DetailElement detail, SideElement side,
                            Arrangement arrange, PromptElement prompt) {
        this.store = store;
        this.detail = detail;
        this.side = side;
        this.arrange = arrange;
        this.prompt = prompt;
        // 뼈대의 이름은 운영 콘솔의 것이다 — #agentview/#filecol/#stream/#sidecol. 이름을 새로
        // 지었더니 console.css의 배치 기계(창 높이 앵커·기둥 접기·도크 여백)가 통째로 비켜갔다:
        // 실측으로 대화가 1024px 창에서 224px까지 눌리고 전사는 4천 픽셀로 자라 잘렸다.
        stage.id = "agentview";
        filecol.id = "filecol";
        stream.id = "stream";
        sidecol.id = "sidecol";
        // display:contents 껍데기 — 자식이 제 마크업을 그대로 넣어도 격자에서는 기둥의 직계로
        // 배치된다. 자식이 부모의 구조를 알 필요도, 부모가 자식의 마크업을 알 필요도 없다.
        leftFill.className = "cfill";
        centreFill.className = "cfill";
        filecol.append(leftFill);
        // 운영의 그 순서: 사실판이 전사 위에 선다(같은 기둥 안에서).
        // 운영의 그 순서: 탭 줄 · 사실판 · 연 카드 · 그리고 자식의 전사.
        cardTabs.id = "cardtabs";
        cardTabs.setAttribute("hidden", "");
        cardArea.id = "fileview";
        cardArea.setAttribute("hidden", "");
        stream.append(cardTabs, detail.element(), cardArea, centreFill);
        sidecol.append(side.element());
        stage.append(filecol, stream, sidecol);
        tabs.id = "ptabs";
        tabs.setAttribute("hidden", "");
    }

    public void mount(HTMLElement frame) {
        // 시트는 셸이 스크립트와 함께 걸어 두었다(카탈로그가 그렇게 선언한다) — 여기서 걸지 않는다.
        // #ptabs와 #agentview는 main의 직계다: 운영의 높이 규칙이 main > #agentview로 걸린다.
        frame.replaceChildren(tabs, stage);
        // 이 화면에서 들어오는 것은 무대 전체가 아니라 대화 기둥이다 — 운영도 그 하나만 들인다.
        // 나머지(사실판·기둥)는 자리를 지키는 것들이라, 움직이면 화면이 통째로 흔들린다.
        Motion.enter(stream);
        arrange.engage();
        // 무엇에 걸려 있는지는 컴포저 바로 위에 선다 — 답하려고 목록으로 되돌아가지 않게.
        arrange.putPrompt(prompt.element());
        if (wired) return;
        wired = true;
        // 자식이 미는 렌더를 받을 자리 — 가운데는 하나, 왼쪽은 쌓인다.
        // 자식이 미는 렌더를 받을 자리 — 셋 다 이미 옷을 입은 채로 건넨다.
        // 자식은 상자가 어느 기둥인지도, 창 바닥에 고정된 도크인지도 모른다.
        PaneSharing.host((slot, render) -> {
            HTMLElement box;
            if ("left".equals(slot)) {
                if (leftFill.childElementCount == 0) box = leftFill;   // 첫 판은 기둥 그 자체
                else { box = el("div"); box.className = "cpane"; filecol.append(box); }
            } else if ("dock".equals(slot)) {
                box = arrange.dockSlot();
            } else {
                box = centreFill;
                box.replaceChildren();
            }
            Js.<Render>cast(render).onInvoke(box);
        });
        store.onContext(this::adopt);
        prompt.wire();
        // 자식이 카드를 열고 닫으면 줄이 따라간다 — 무엇이 열려 있는지는 자식만 안다.
        CardSharing.onChange(cards -> drawCards());
        drawCards();
        buildTabs();
        // 폭이 바뀌면 다시 정한다 — 폰에서 넓어진 창은 탭을 걷고 전부를 보여야 한다.
        // 창의 resize를 듣는다: 미디어 질의의 change만 듣던 판은 좁힐 때만 발화하고 넓힐 때
        // 조용했다(실측: 탭이 걷히지 않음). resize는 두 방향 모두에서 온다.
        DomGlobal.window.addEventListener("resize", evt -> layout());
        layout();
        store.start();
    }

    /**
     * 가운데의 카드 줄 — 사실판과, 자식이 연 것들(파일·디프·커밋…).
     *
     * 한 자리에 둘을 그리지 않는다: 무엇이 보이는지 고르는 것이 이 줄이고, 고를 것이 하나뿐이면
     * 줄은 서지 않는다(운영 규칙: 연 파일이 없으면 hidden).
     */
    private final java.util.Set<String> cardsSeen = new java.util.HashSet<>();

    private void drawCards() {
        JsArrayLike<Object> cards = Js.uncheckedCast(CardSharing.current());
        int n = cards == null ? 0 : cards.getLength();
        if (n == 0) {
            cardTabs.setAttribute("hidden", "");
            cardTabs.replaceChildren();
            cardArea.setAttribute("hidden", "");
            cardArea.replaceChildren();
            show(detail.element(), store.context() != null);
            cardShows = "facts";
            return;
        }
        cardTabs.replaceChildren();
        cardTabs.removeAttribute("hidden");
        cardTabs.append(cardTab(tr("field.facts"), "facts", null));
        boolean known = "facts".equals(cardShows);
        // 방금 열린 것으로 간다 — 파일을 눌렀는데 화면이 그대로면 아무 일도 안 일어난 것처럼
        // 읽힌다(운영: openFiles에 밀어 넣고 그 탭을 고른다). 이미 열려 있던 것을 다시 눌러
        // 다시 그려지는 경우는 새것이 아니므로 보던 자리를 뺏지 않는다.
        String opened = null;
        java.util.Set<String> now = new java.util.HashSet<>();
        for (int i = 0; i < n; i++) {
            JsPropertyMap<Object> c = Js.uncheckedCast(cards.getAt(i));
            String key = String.valueOf(c.get("key"));
            now.add(key);
            if (!cardsSeen.contains(key)) opened = key;
            if (key.equals(cardShows)) known = true;
            cardTabs.append(cardTab(String.valueOf(c.get("title")), key, c));
        }
        cardsSeen.retainAll(now);
        cardsSeen.addAll(now);
        if (opened != null) { cardShows = opened; known = true; }
        if (!known) cardShows = String.valueOf(Js.<JsPropertyMap<Object>>uncheckedCast(cards.getAt(n - 1)).get("key"));
        // 고른 것만 그린다 — 사실판과 카드가 같은 자리를 나눠 쓴다(운영 showCard).
        boolean facts = "facts".equals(cardShows);
        show(detail.element(), facts && store.context() != null);
        show(cardArea, !facts);
        if (!facts) {
            cardArea.replaceChildren();
            for (int i = 0; i < n; i++) {
                JsPropertyMap<Object> c = Js.uncheckedCast(cards.getAt(i));
                if (!cardShows.equals(String.valueOf(c.get("key")))) continue;
                Object render = c.get("render");
                if (render != null) Js.<Render>cast(render).onInvoke(cardArea);
            }
        }
        for (int i = 0; i < cardTabs.childElementCount; i++) {
            elemental2.dom.Element t = cardTabs.querySelectorAll("md-secondary-tab").getAt(i);
            if (t == null) continue;
            Js.asPropertyMap(t).set("active", cardShows.equals(t.getAttribute("data-card")));
        }
    }

    /** 탭 하나 — 이름과, 파일이면 닫는 ×(사실판은 닫지 않는다: 늘 있는 것이다). */
    private HTMLElement cardTab(String title, String key, JsPropertyMap<Object> card) {
        HTMLElement t = el("md-secondary-tab");
        t.setAttribute("data-card", key);
        HTMLElement label = el("div");
        label.className = "tablbl";
        label.textContent = title;
        t.append(label);
        t.addEventListener("click", evt -> { cardShows = key; drawCards(); });
        if (card != null) {
            HTMLElement x = el("button");
            x.setAttribute("type", "button");
            x.className = "tabclose hit48";
            x.setAttribute("aria-label", tr("action.close_named", "name", title));
            x.append(dev.sayaya.magi.bridge.Icons.orGlyph("#i-sl-xmark", "×", "mk"));
            Object close = card.get("close");
            x.addEventListener("click", evt -> {
                evt.stopPropagation();
                if ("facts".equals(cardShows) || key.equals(cardShows)) cardShows = "facts";
                if (close != null) Js.<CardSharing.Runner>cast(close).call();
            });
            t.append(x);
        }
        return t;
    }

    /** 폰의 탭 — 대화 · 정보 · 파일. 이름은 운영의 그 말이다(팩 키도 같다). */
    private void buildTabs() {
        for (String[] t : new String[][]{{"talk", "panel.talk"}, {"facts", "panel.facts"},
                {"files", "panel.files"}, {"plan", "panel.plan"}}) {
            HTMLElement tab = el("md-primary-tab");
            tab.id = "ptab-" + t[0];
            tab.textContent = tr(t[1]);
            final String name = t[0];
            tab.addEventListener("click", evt -> {
                // 옆 자리로 옮기는 것이므로 옆에서 들어온다 — 읽는 이가 움직인 방향으로.
                // 위아래로 들어오면 이 넷이 서로의 아래에 있는 것처럼 읽힌다(운영의 그 판단).
                int was = order(panel), now = order(name);
                panel = name;
                layout();
                if (was != now) Motion.play(panelBox(name), now > was ? Motion.FROM_RIGHT : Motion.FROM_LEFT);
            });
            tabs.append(tab);
        }
    }

    /**
     * 지금 무엇이 보이는가. 넓으면 전부(탭은 걷는다), 좁으면 탭이 고른 하나.
     * body[panel=…]으로 말한다 — console.css가 읽는 계약이고, 자식도 그 말을 읽을 수 있다.
     */
    private void layout() {
        // 기준은 운영 콘솔의 그 폭이다(52.5em=840px): console.css가 그 위에서 #ptabs를
        // display:none !important로 눌러 둔다. 여기서 더 넓은 기준을 쓰면 840~1023px 구간이
        // 탭도 안 보이는데 판은 탭 규칙대로 감춰진 상태가 된다 — 실측: 860px에서 전사 폭 0.
        boolean narrow = DomGlobal.window.matchMedia("(max-width:52.4375em)").matches;
        boolean companion = store.context() != null;
        if (!narrow || !companion) {
            tabs.setAttribute("hidden", "");
            DomGlobal.document.body.removeAttribute("panel");
            show(detail.element(), companion);
            show(filecol, true);
            show(stream, true);
            show(sidecol, true);
            return;
        }
        tabs.removeAttribute("hidden");
        DomGlobal.document.body.setAttribute("panel", panel);
        // 폰에서는 한 번에 하나 — 운영의 네 탭 그대로(대화·정보·파일·계획).
        show(stream, "talk".equals(panel) || "facts".equals(panel));
        show(detail.element(), "facts".equals(panel));
        show(centreFill, "talk".equals(panel));
        show(filecol, "files".equals(panel));
        show(sidecol, "plan".equals(panel));
        elemental2.dom.NodeList<elemental2.dom.Element> all = tabs.querySelectorAll("md-primary-tab");
        for (int i = 0; i < all.getLength(); i++) {
            elemental2.dom.Element tab = all.getAt(i);
            Js.asPropertyMap(tab).set("active", tab.id.equals("ptab-" + panel));
        }
    }

    /** 탭의 차례 — 방향을 정하는 데만 쓴다(운영의 그 순서: 대화·정보·작업공간·진행). */
    private static int order(String name) {
        String[] all = {"talk", "facts", "files", "plan"};
        for (int i = 0; i < all.length; i++) if (all[i].equals(name)) return i;
        return 0;
    }

    /** 그 탭이 보이는 판 — 움직이는 것은 판이지 무대가 아니다. */
    private HTMLElement panelBox(String name) {
        if ("talk".equals(name)) return centreFill.firstElementChild == null ? stream : stream;
        if ("facts".equals(name)) return detail.element();
        if ("files".equals(name)) return filecol;
        return sidecol;
    }

    private static void show(HTMLElement e, boolean on) {
        if (on) e.removeAttribute("hidden"); else e.setAttribute("hidden", "");
    }

    /** 타입이 정해지면 그 자식을 들인다 — 한 창에서 한 번만(ModuleInject가 센다). */
    private void adopt(CompanionContext ctx) {
        layout();
        if (ctx == null || ctx.ui == null || ctx.ui.isEmpty()) return;
        if (ctx.ui.equals(childLoaded)) return;
        childLoaded = ctx.ui;
        // 자식의 시트도 들이는 쪽이 건다 — 그 선언은 셸의 카탈로그가 컨텍스트에 실어 보냈다.
        ModuleInject.ensure(ctx.ui, ctx.uiStyles);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
