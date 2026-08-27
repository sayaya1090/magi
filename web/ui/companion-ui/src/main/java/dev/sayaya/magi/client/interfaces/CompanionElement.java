package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.ModuleInject;
import dev.sayaya.magi.bridge.Stylesheet;
import dev.sayaya.magi.bridge.PaneSharing;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.client.usecase.CompanionStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 컴패니언 상세의 레이아웃 — 범용이다: 어떤 타입이든 위와 오른쪽은 같은 것을 답한다.
 *
 * 위는 사실판(무엇이고 무엇을 하는 중인가), 오른쪽은 판(계획·건넨 일·예약). 가운데와
 * 왼쪽은 타입의 몫이라 자리(슬롯)만 내주고 자식이 채운다 — 부모는 무엇이 오는지 모른다.
 * 자식의 이름은 셸의 카탈로그가 풀어 컨텍스트(ui)에 실어 보낸 것이고, 컴패니언이 대는
 * 경로가 아니다: 어느 코드가 도는지는 이 콘솔이 정한다.
 *
 * 왼쪽은 여럿일 수 있다 — 자식이 left를 여러 번 밀면 순서대로 쌓인다.
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
    private boolean wired = false;
    private String childLoaded = null;

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
        root.append(detail.element(), stage);
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
        store.start();
    }

    /** 타입이 정해지면 그 자식을 들인다 — 한 창에서 한 번만(ModuleInject가 센다). */
    private void adopt(CompanionContext ctx) {
        if (ctx == null || ctx.ui == null || ctx.ui.isEmpty()) return;
        if (ctx.ui.equals(childLoaded)) return;
        childLoaded = ctx.ui;
        ModuleInject.ensure(ctx.ui);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
