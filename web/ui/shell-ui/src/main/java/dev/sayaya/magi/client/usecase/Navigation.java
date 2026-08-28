package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.Place;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import elemental2.dom.URLSearchParams;

import dev.sayaya.rx.subject.BehaviorSubject;
import lombok.experimental.Delegate;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.rx.subject.BehaviorSubject.behavior;

/**
 * 어디에 있는가 — 주소가 원본이고 이 클래스는 그 독본이다.
 * 카탈로그 화면은 ?v=, 컴패니언은 ?d=(&p=) — 기존 콘솔의 그 주소라, 옛 링크가 새
 * 콘솔에서도 같은 곳에 닿는다. 문 클릭도 행 클릭도 pushState, 뒤로가기는 popstate;
 * 셋 다 같은 settle로 모인다. 이미 서 있는 곳을 다시 누르면 맨 위로 스크롤한다.
 */
@Singleton
public class Navigation {
    // 흐름 그 자체다 — 늦게 온 구독자도 지금 서 있는 자리를 즉시 받는다(BehaviorSubject의
    // 그 성질이, 손으로 쓰던 "현재값 재생"이었다). 아직 첫 자리를 정하기 전(start 이전)에는
    // null이 흐르므로, 읽는 쪽은 늘 그랬듯 null을 "아직 모른다"로 읽는다.
    @Delegate private final BehaviorSubject<Place> _this = behavior(null);

    @Inject
    public Navigation() {}

    /**
     * 자리가 정해진 뒤에만 부른다 — 흐름의 첫 값은 "아직 모른다"(null)이고, 그것을 자리로
     * 읽으면 판들이 아무 데도 아닌 곳을 그린다. 거르는 자리는 여기다: 읽는 쪽 넷이 저마다
     * 같은 null 가드를 다는 대신.
     */
    public dev.sayaya.rx.Subscription subscribe(java.util.function.Consumer<Place> o) {
        return _this.filter(java.util.Objects::nonNull).subscribe(o);
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
        if (getValue() == null || !getValue().isCompanion()) return;
        move(Place.companion(getValue().socket, getValue().peer, pastOrNull));
    }

    /** 자식 층위 — 그 컴패니언이 낳은 아이 하나로. null이면 지금 대화로 돌아온다. */
    public void goSub(String idOrNull) {
        if (getValue() == null || !getValue().isCompanion()) return;
        move(Place.companion(getValue().socket, getValue().peer, null, idOrNull));
    }

    private void move(Place p) {
        if (p.same(getValue())) { DomGlobal.window.scrollTo(0, 0); return; }
        String path = DomGlobal.window.location.pathname;
        String url;
        if (p.isCompanion()) {
            url = path + "?d=" + Global.encodeURIComponent(p.socket)
                    + (p.peer != null ? "&p=" + Global.encodeURIComponent(p.peer) : "")
                    // ?past= 는 빈 값도 값이다: 빈 past는 목록이고, 없음만이 지금 대화다(운영 규칙).
                    + (p.past != null ? "&past=" + Global.encodeURIComponent(p.past) : "")
                    // 자식 층위 — 지난 일과 함께 서지 않는다(둘 다 지금 대화를 대신한다).
                    + (p.sub != null && !p.sub.isEmpty() ? "&sub=" + Global.encodeURIComponent(p.sub) : "");
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
            return Place.companion(d, q.get("p"), q.has("past") ? nz(q.get("past")) : null, q.get("sub"));
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
        _this.next(p);
    }
}
