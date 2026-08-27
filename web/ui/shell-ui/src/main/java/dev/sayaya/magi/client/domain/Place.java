package dev.sayaya.magi.client.domain;

/**
 * 셸이 서 있는 곳 — 카탈로그 화면(목적지)이거나 컴패니언 하나.
 *
 * 컴패니언은 Destination이 아니다: 문(레일)은 카탈로그 화면에만 달리고, 컴패니언은
 * 플릿의 행과 레일 2단의 항목으로 들어간다. 그래도 레일의 선택 표시는 section이
 * 답한다 — 컴패니언 화면에 서 있어도 "컴패니언" 문이 켜져 있는 것이 맞다.
 */
public final class Place {
    public final Destination screen;  // 서 있는 화면(주소의 것) — 보드도 화면이다
    public final Destination section; // 레일이 켜는 문 — 보드는 컴패니언 문
    public final String socket;       // null이면 카탈로그 화면
    public final String peer;         // 없으면 null

    private Place(Destination screen, String socket, String peer) {
        this.screen = screen;
        this.section = screen.section();
        this.socket = socket;
        this.peer = peer;
    }

    public static Place at(Destination d) { return new Place(d, null, null); }

    public static Place companion(String socket, String peer) {
        return new Place(Destination.FLEET, socket, peer == null || peer.isEmpty() ? null : peer);
    }

    public boolean isCompanion() { return socket != null; }

    public boolean same(Place o) {
        return o != null && screen == o.screen && eq(socket, o.socket) && eq(peer, o.peer);
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }
}
