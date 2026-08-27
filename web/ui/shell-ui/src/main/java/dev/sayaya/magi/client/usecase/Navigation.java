package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.Place;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import elemental2.dom.URLSearchParams;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 어디에 있는가 — 주소가 원본이고 이 클래스는 그 독본이다.
 * 카탈로그 화면은 ?v=, 컴패니언은 ?d=(&p=) — 기존 콘솔의 그 주소라, 옛 링크가 새
 * 콘솔에서도 같은 곳에 닿는다. 문 클릭도 행 클릭도 pushState, 뒤로가기는 popstate;
 * 셋 다 같은 settle로 모인다. 이미 서 있는 곳을 다시 누르면 맨 위로 스크롤한다.
 */
@Singleton
public class Navigation {
    private final List<Consumer<Place>> observers = new ArrayList<>();
    private Place current = null;

    @Inject
    public Navigation() {}

    public void subscribe(Consumer<Place> o) {
        observers.add(o);
        if (current != null) o.accept(current);
    }

    public void start() {
        DomGlobal.window.addEventListener("popstate", evt -> settle(fromUrl()));
        settle(fromUrl());
    }

    public void go(Destination d) {
        move(Place.at(d));
    }

    public void goCompanion(String socket, String peer) {
        move(Place.companion(socket, peer, null));
    }

    /** 화면과 그 화면의 조각 하나 — 셸은 그 뜻을 모른 채 주소에 싣고 되읽어 준다. */
    public void goViewWith(String view, String key, String value) {
        move(Place.at(Destination.byId(view), key, value));
    }

    /** 지난 일 층위 — 서 있는 컴패니언 위에서만 뜻이 있다: null=지금 대화, ""=목록, 값=그 세션. */
    public void goPast(String pastOrNull) {
        if (current == null || !current.isCompanion()) return;
        move(Place.companion(current.socket, current.peer, pastOrNull));
    }

    private void move(Place p) {
        if (p.same(current)) { DomGlobal.window.scrollTo(0, 0); return; }
        String path = DomGlobal.window.location.pathname;
        String url;
        if (p.isCompanion()) {
            url = path + "?d=" + Global.encodeURIComponent(p.socket)
                    + (p.peer != null ? "&p=" + Global.encodeURIComponent(p.peer) : "")
                    // ?past= 는 빈 값도 값이다: 빈 past는 목록이고, 없음만이 지금 대화다(운영 규칙).
                    + (p.past != null ? "&past=" + Global.encodeURIComponent(p.past) : "");
        } else {
            // 첫 문은 맨주소가 갖는다 — 기존 콘솔의 HREF 규칙(fleet은 '').
            url = p.screen == Destination.FLEET ? path : path + "?v=" + p.screen.id;
            if (p.piece != null) {
                url += (url.contains("?") ? "&" : "?") + p.pieceKey + "="
                        + Global.encodeURIComponent(p.piece);
            }
        }
        DomGlobal.window.history.pushState(null, "", url);
        settle(p);
    }

    private Place fromUrl() {
        URLSearchParams q = new URLSearchParams(DomGlobal.window.location.search);
        String d = q.get("d");
        if (d != null && !d.isEmpty()) {
            return Place.companion(d, q.get("p"), q.has("past") ? nz(q.get("past")) : null);
        }
        String v = q.get("v");
        Destination screen = Destination.byId(v == null ? "fleet" : v);
        // 화면이 제 조각을 주소에서 읽는다(Windows.query) — 셸은 그것을 자리의 일부로만 센다:
        // 조각이 바뀌면 다른 자리이고, 그래야 뒤로가기가 그 방에서 목록으로 돌아온다.
        for (String key : Destination.PIECES) {
            String piece = q.get(key);
            if (piece != null && !piece.isEmpty()) return Place.at(screen, key, piece);
        }
        return Place.at(screen);
    }

    private static String nz(String s) { return s == null ? "" : s; }

    private void settle(Place p) {
        current = p;
        for (Consumer<Place> o : observers) o.accept(p);
    }
}
