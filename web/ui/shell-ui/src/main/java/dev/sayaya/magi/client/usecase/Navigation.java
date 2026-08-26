package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;
import elemental2.dom.DomGlobal;
import elemental2.dom.URLSearchParams;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 어디에 있는가 — 주소(?v=)가 원본이고 이 클래스는 그 독본이다.
 * 문 클릭은 pushState, 뒤로가기는 popstate; 둘 다 같은 settle로 모인다.
 * 이미 서 있는 목적지를 다시 누르면 맨 위로 스크롤한다(가이드의 재선택 규칙).
 */
@Singleton
public class Navigation {
    private final List<Consumer<Destination>> observers = new ArrayList<>();
    private Destination current = null;

    @Inject
    public Navigation() {}

    public void subscribe(Consumer<Destination> o) {
        observers.add(o);
        if (current != null) o.accept(current);
    }

    public void start() {
        DomGlobal.window.addEventListener("popstate", evt -> settle(fromUrl()));
        settle(fromUrl());
    }

    public void go(Destination d) {
        if (current == d) { DomGlobal.window.scrollTo(0, 0); return; }
        String path = DomGlobal.window.location.pathname;
        // 첫 문은 맨주소가 갖는다 — 기존 콘솔의 HREF 규칙(fleet은 '').
        String url = d == Destination.FLEET ? path : path + "?v=" + d.id;
        DomGlobal.window.history.pushState(null, "", url);
        settle(d);
    }

    private Destination fromUrl() {
        String v = new URLSearchParams(DomGlobal.window.location.search).get("v");
        return Destination.byId(v == null ? "fleet" : v);
    }

    private void settle(Destination d) {
        current = d;
        for (Consumer<Destination> o : observers) o.accept(d);
    }
}
