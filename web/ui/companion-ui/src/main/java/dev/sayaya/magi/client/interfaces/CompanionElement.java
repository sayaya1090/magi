package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.ModuleInject;
import dev.sayaya.magi.bridge.Stylesheet;
import dev.sayaya.magi.bridge.PaneSharing;
import dev.sayaya.magi.bridge.Render;
import elemental2.dom.MediaQueryList;
import dev.sayaya.magi.client.usecase.CompanionStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

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
    private final HTMLElement root = el("section");
    private final HTMLElement stage = el("div");
    private final HTMLElement left = el("div");
    private final HTMLElement centre = el("div");
    private final HTMLElement tabs = el("md-tabs");
    private boolean wired = false;
    private String childLoaded = null;
    private String panel = "talk";

    @Inject
    public CompanionElement(CompanionStore store, DetailElement detail, SideElement side) {
        this.store = store;
        this.detail = detail;
        this.side = side;
        root.id = "companion";
        stage.id = "cstage";
        left.id = "cleft";
        centre.id = "cframe";
        stage.append(left, centre, side.element());
        tabs.id = "ptabs";
        tabs.setAttribute("hidden", "");
        root.append(tabs, detail.element(), stage);
    }

    public void mount(HTMLElement frame) {
        Stylesheet.ensure("companion");   // 무대의 세 자리는 이 모듈이 말한다
        frame.replaceChildren(root);
        if (wired) return;
        wired = true;
        // 자식이 미는 렌더를 받을 자리 — 가운데는 하나, 왼쪽은 쌓인다.
        PaneSharing.host((slot, render) -> {
            HTMLElement box;
            if ("left".equals(slot)) {
                box = el("div");
                box.className = "cpane";
                left.append(box);
            } else {
                box = centre;
                box.replaceChildren();
            }
            Js.<Render>cast(render).onInvoke(box);
        });
        store.onContext(this::adopt);
        buildTabs();
        // 폭이 바뀌면 다시 정한다 — 폰에서 넓어진 창은 탭을 걷고 전부를 보여야 한다.
        // 창의 resize를 듣는다: 미디어 질의의 change만 듣던 판은 좁힐 때만 발화하고 넓힐 때
        // 조용했다(실측: 탭이 걷히지 않음). resize는 두 방향 모두에서 온다.
        DomGlobal.window.addEventListener("resize", evt -> layout());
        layout();
        store.start();
    }

    /** 폰의 탭 — 대화 · 정보 · 파일. 이름은 운영의 그 말이다(팩 키도 같다). */
    private void buildTabs() {
        for (String[] t : new String[][]{{"talk", "panel.talk"}, {"facts", "panel.facts"},
                {"files", "panel.files"}}) {
            HTMLElement tab = el("md-primary-tab");
            tab.id = "ptab-" + t[0];
            tab.textContent = tr(t[1]);
            final String name = t[0];
            tab.addEventListener("click", evt -> { panel = name; layout(); });
            tabs.append(tab);
        }
    }

    /**
     * 지금 무엇이 보이는가. 넓으면 전부(탭은 걷는다), 좁으면 탭이 고른 하나.
     * body[panel=…]으로 말한다 — console.css가 읽는 계약이고, 자식도 그 말을 읽을 수 있다.
     */
    private void layout() {
        boolean narrow = DomGlobal.window.matchMedia("(max-width:63.9375em)").matches;
        boolean companion = store.context() != null;
        if (!narrow || !companion) {
            tabs.setAttribute("hidden", "");
            DomGlobal.document.body.removeAttribute("panel");
            show(detail.element(), companion);
            show(left, true);
            show(centre, true);
            show(side.element(), true);
            return;
        }
        tabs.removeAttribute("hidden");
        DomGlobal.document.body.setAttribute("panel", panel);
        show(detail.element(), "facts".equals(panel));
        show(left, "files".equals(panel));
        show(centre, "talk".equals(panel));
        // 오른쪽(계획)은 정보와 함께 — 폰에서 넷째 탭을 만들기보다, 무엇을 하기로 했나는
        // 무엇인가와 같은 화면에서 읽힌다.
        show(side.element(), "facts".equals(panel));
        elemental2.dom.NodeList<elemental2.dom.Element> all = tabs.querySelectorAll("md-primary-tab");
        for (int i = 0; i < all.getLength(); i++) {
            elemental2.dom.Element tab = all.getAt(i);
            Js.asPropertyMap(tab).set("active", tab.id.equals("ptab-" + panel));
        }
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
        ModuleInject.ensure(ctx.ui);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
