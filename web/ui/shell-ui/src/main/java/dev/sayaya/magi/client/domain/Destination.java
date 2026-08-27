package dev.sayaya.magi.client.domain;

/**
 * 드로어의 목적지 하나 — 주소(?v=)이자 모듈 이름이자 문의 라벨.
 *
 * 카탈로그(all)는 이식된 화면만 담는다: 눌러서 빈 화면에 닿는 문은 없는 문보다 나쁘다
 * (기존 콘솔이 제로 타일에 적용하는 그 규칙). 화면이 이식될 때마다 여기 한 줄이 는다.
 */
public final class Destination {
    public final String id;        // ?v= 값이자 /ui/<id>/<id>.nocache.js 의 모듈 이름
    public final String labelKey;  // 문에 쓰는 말(팩 키) — aria-label과 넓은 라벨
    public final String shortKey;  // 접힌 레일·폰 바가 읽는 한두 단어
    public final String subKey;    // 열린 드로어만 그리는 한 줄 설명
    public final String iconPath;  // 24x24 스트로크 패스(기존 콘솔의 그 드로잉, currentColor)

    private Destination(String id, String labelKey, String shortKey, String subKey, String iconPath) {
        this.id = id;
        this.labelKey = labelKey;
        this.shortKey = shortKey;
        this.subKey = subKey;
        this.iconPath = iconPath;
    }

    public static final Destination FLEET = new Destination("fleet",
            "nav.companions", "nav.companions", "nav.companions_sub",
            "M4 19v-1.6a3.4 3.4 0 0 1 3.4-3.4h2.2a3.4 3.4 0 0 1 3.4 3.4V19M8.5 6.2a2.6 2.6 0 1 1 0 5.2 "
                    + "2.6 2.6 0 0 1 0-5.2M15.5 19v-1.6a3.4 3.4 0 0 0-1.2-2.6M15 6.4a2.6 2.6 0 0 1 0 5");

    // 지식 — 운영 콘솔의 그 문 그대로: 주소도 v=skills, 그림도 겹친 디스크(공유 저장소).
    public static final Destination KNOWLEDGE = new Destination("skills",
            "nav.shared", "nav.shared", "nav.shared_sub",
            "M12 3c4.2 0 7 1.1 7 2.3S16.2 7.6 12 7.6 5 6.5 5 5.3 7.8 3 12 3M5 5.3v13.4C5 19.9 7.8 21 "
                    + "12 21s7-1.1 7-2.3V5.3M5 12c0 1.2 2.8 2.3 7 2.3s7-1.1 7-2.3");

    public static Destination[] all() { return new Destination[]{FLEET, KNOWLEDGE}; }

    /** 주소가 대는 이름의 목적지, 모르면 첫 문 — 잘못 친 주소가 빈 화면이 되지 않게. */
    public static Destination byId(String id) {
        for (Destination d : all()) if (d.id.equals(id)) return d;
        return FLEET;
    }
}
