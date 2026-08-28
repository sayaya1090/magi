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
    public final String past;         // 컴패니언에서만: null=지금 대화, ""=지난 일 목록, 값=그 세션
    /** 컴패니언에서만: 그 컴패니언이 낳은 자식 하나(?sub=) — 지난 일과 나란한 또 하나의 층위. */
    public final String sub;
    /**
     * 그 화면의 조각 하나 — 회의실의 ?m=(어느 방인가) 같은 것. 셸은 그 값이 무엇을 뜻하는지
     * 모르고, 주소에 싣고 되읽어 화면에 돌려주기만 한다: 뜻을 아는 것은 그 화면이고, 주소를
     * 쓰는 것은 셸이다(뒤로가기가 셸이 모르는 자리에 서지 않게).
     */
    public final String pieceKey, piece;

    private Place(Destination screen, String socket, String peer, String past) {
        this(screen, socket, peer, past, null, null, null);
    }

    private Place(Destination screen, String socket, String peer, String past,
                  String pieceKey, String piece) {
        this(screen, socket, peer, past, pieceKey, piece, null);
    }

    private Place(Destination screen, String socket, String peer, String past,
                  String pieceKey, String piece, String sub) {
        this.screen = screen;
        this.section = screen.section();
        this.socket = socket;
        this.peer = peer;
        this.past = past;
        this.pieceKey = pieceKey;
        this.piece = piece;
        this.sub = sub;
    }

    public static Place at(Destination d) { return new Place(d, null, null, null); }

    /** 그 화면의 조각을 실은 자리 — 값이 비면 조각 없는 자리와 같다. */
    public static Place at(Destination d, String key, String value) {
        boolean has = key != null && !key.isEmpty() && value != null && !value.isEmpty();
        return new Place(d, null, null, null, has ? key : null, has ? value : null);
    }

    public static Place companion(String socket, String peer, String past) {
        return companion(socket, peer, past, null);
    }

    /** 그 컴패니언의 자식 하나를 보는 자리 — 지난 일 층위와 같은 자리에 선다(둘이 함께 서지 않는다). */
    public static Place companion(String socket, String peer, String past, String sub) {
        return new Place(Destination.FLEET, socket, peer == null || peer.isEmpty() ? null : peer,
                past, null, null, sub);
    }

    public boolean isCompanion() { return socket != null; }

    public boolean same(Place o) {
        return o != null && screen == o.screen && eq(socket, o.socket) && eq(peer, o.peer)
                && eq(past, o.past) && eq(sub, o.sub) && eq(pieceKey, o.pieceKey) && eq(piece, o.piece);
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }
}
