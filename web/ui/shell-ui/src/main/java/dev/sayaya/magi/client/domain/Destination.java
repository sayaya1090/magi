package dev.sayaya.magi.client.domain;

/**
 * 드로어의 목적지 하나 — 주소(?v=)이자 모듈 이름이자 문의 라벨.
 *
 * 카탈로그(all)는 이식된 화면만 담는다: 눌러서 빈 화면에 닿는 문은 없는 문보다 나쁘다
 * (기존 콘솔이 제로 타일에 적용하는 그 규칙). 화면이 이식될 때마다 여기 한 줄이 는다.
 */
public final class Destination {
    public final String id;        // ?v= 값 — 주소의 이름
    public final String module;    // /ui/<module>/<module>.nocache.js — 그 화면을 그리는 모듈
    // 그 모듈이 제 스타일시트(/ui/<module>.css)를 함께 싣는가. 화면이 아니라 여기서 말하는
    // 이유: 시트를 거는 것은 스크립트를 들이는 쪽(셸)의 일이고, 화면에게 시키면 잊은 화면이
    // 민얼굴로 뜬다. 대부분의 화면은 console.css만으로 산다 — 거짓이 기본이다.
    public final boolean styles;
    public final String labelKey;  // 문에 쓰는 말(팩 키) — aria-label과 넓은 라벨
    public final String shortKey;  // 접힌 레일·폰 바가 읽는 한두 단어
    public final String subKey;    // 열린 드로어만 그리는 한 줄 설명
    public final String iconPath;  // 24x24 스트로크 패스(기존 콘솔의 그 드로잉, currentColor)
    public final String iconRef;   // 스프라이트의 그림 이름(있는 빌드에서 도형을 갈아입는다)
    public final String may;       // 문이 요구하는 능력(운영 data-may) — null이면 모두의 문
    public final boolean atFoot;   // 발치(#railFoot)의 문인가 — 운영의 그 자리(접근 제어)

    private Destination(String id, String labelKey, String shortKey, String subKey, String iconPath) {
        this(id, labelKey, shortKey, subKey, iconPath, null, "");
    }

    private Destination(String id, String labelKey, String shortKey, String subKey, String iconPath,
                        String may) {
        this(id, labelKey, shortKey, subKey, iconPath, may, "");
    }

    private Destination(String id, String labelKey, String shortKey, String subKey, String iconPath,
                        String may, String iconRef) {
        this(id, labelKey, shortKey, subKey, iconPath, may, iconRef, false);
    }

    private Destination(String id, String labelKey, String shortKey, String subKey, String iconPath,
                        String may, String iconRef, boolean atFoot) {
        this(id, id, labelKey, shortKey, subKey, iconPath, may, iconRef, atFoot);
    }

    private Destination(String id, String module, String labelKey, String shortKey, String subKey,
                        String iconPath, String may, String iconRef, boolean atFoot) {
        this(id, module, labelKey, shortKey, subKey, iconPath, may, iconRef, atFoot, false);
    }

    private Destination(String id, String module, String labelKey, String shortKey, String subKey,
                        String iconPath, String may, String iconRef, boolean atFoot, boolean styles) {
        this.id = id;
        this.module = module;
        this.labelKey = labelKey;
        this.shortKey = shortKey;
        this.subKey = subKey;
        this.iconPath = iconPath;
        this.may = may;
        this.iconRef = iconRef;
        this.atFoot = atFoot;
        this.styles = styles;
    }

    // 주소는 'fleet'이고 모듈은 'companion'이다 — 목록과 상세가 한 모듈의 두 얼굴이라서.
    public static final Destination FLEET = new Destination("fleet", "companion",
            "nav.companions", "nav.companions", "nav.companions_sub",
            "M4 19v-1.6a3.4 3.4 0 0 1 3.4-3.4h2.2a3.4 3.4 0 0 1 3.4 3.4V19M8.5 6.2a2.6 2.6 0 1 1 0 5.2 "
                    + "2.6 2.6 0 0 1 0-5.2M15.5 19v-1.6a3.4 3.4 0 0 0-1.2-2.6M15 6.4a2.6 2.6 0 0 1 0 5",
            null, "#i-sl-users", false, true);   // companion.css를 함께 싣는 유일한 화면

    // 지식 — 운영 콘솔의 그 문 그대로: 주소도 v=skills, 그림도 겹친 디스크(공유 저장소).
    // 주소는 'skills'(운영과 같은 ?v= 값)이고 모듈 이름은 'knowledge'다 — 모듈 이름이 화면 판의
    // id와 같으면 GWT가 그 이름으로 만든 iframe과 판이 부딪힌다(실측: #skills가 둘).
    public static final Destination KNOWLEDGE = new Destination("skills", "knowledge",
            "nav.shared", "nav.shared", "nav.shared_sub",
            "M12 3c4.2 0 7 1.1 7 2.3S16.2 7.6 12 7.6 5 6.5 5 5.3 7.8 3 12 3M5 5.3v13.4C5 19.9 7.8 21 "
                    + "12 21s7-1.1 7-2.3V5.3M5 12c0 1.2 2.8 2.3 7 2.3s7-1.1 7-2.3",
            null, "#i-sl-database", false);

    // 보드 — 문이 아니라 주소다: 플릿에 관한 질문이라 플릿에서 들어가고(운영 규칙),
    // 레일은 컴패니언 문을 켠 채 둔다(section). 아이콘이 없는 것은 문이 없어서다.
    public static final Destination BOARD = new Destination("board", "boardview",
            "nav.board", "nav.board", "nav.board", "", null, "", false);

    // 맵 — 보드처럼 문 없는 주소: 플릿이 어떻게 놓여 있고 무엇이 오가는지, 같은 목록의
    // 다른 시선이라 컴패니언 문이 켜진 채다.
    public static final Destination MAP = new Destination("map", "mapview",
            "nav.map", "nav.map", "nav.map", "", null, "", false);

    // 접근 — 누가 이 콘솔을 쓸 수 있나. 문은 admin의 것(운영 data-may): 서버가 어차피
    // 거부하지만, 눌러서 거절에 닿는 문은 없는 문보다 나쁘다.
    // 보이는 낱말은 짧은 것("사람"), 읽어 주는 이름은 긴 것("사용자와 권한") — 운영이 문 넷 중
    // 이 하나에만 그렇게 한다. 긴 이름을 보이는 자리에 넣었더니 폰의 하단 바에서 두 줄이 되어
    // 바가 12px 자랐다(실측: 64px 항목이 76px).
    public static final Destination ACCESS = new Destination("access", "accessview",
            "nav.access", "nav.access_short", "nav.access_sub",
            "M12 13.4a2.9 2.9 0 1 0 0-5.8 2.9 2.9 0 0 0 0 5.8M7.5 20v-1.1a3 3 0 0 1 3-3h3a3 3 0 0 "
                    + "1 3 3V20M5.2 10.8a2 2 0 1 0 0-4 2 2 0 0 0 0 4M2.5 17.2v-.8a2.4 2.4 0 0 1 "
                    + "2.4-2.4M18.8 10.8a2 2 0 1 0 0-4 2 2 0 0 0 0 4M21.5 17.2v-.8a2.4 2.4 0 0 "
                    + "0-2.4-2.4",
            "admin", "#i-sl-people-group", true);

    public static Destination[] doors() { return new Destination[]{FLEET, KNOWLEDGE, ACCESS}; }

    public static Destination[] all() {
        return new Destination[]{FLEET, KNOWLEDGE, BOARD, MAP, ACCESS};
    }

    /** 레일이 켤 문 — 보드는 플릿의 다른 시선이라 컴패니언 문이 켜진 채다(운영 규칙). */
    public Destination section() { return this == BOARD || this == MAP ? FLEET : this; }

    /** 주소가 대는 이름의 목적지, 모르면 첫 문 — 잘못 친 주소가 빈 화면이 되지 않게. */
    public static Destination byId(String id) {
        for (Destination d : all()) if (d.id.equals(id)) return d;
        return FLEET;
    }
}
