package dev.sayaya.magi.client.domain;

/**
 * 컴패니언 타입 카탈로그 — 타입 키가 어느 UI 모듈과 어느 이름으로 풀리는지의 한 곳.
 *
 * 지금은 하나다: 타입 1 = 코딩 에이전트, 오늘의 magi 컴패니언 전부가 이것이고
 * companion-ui가 그 화면이다. 디자인·인프라 관리·리서처 같은 타입이 생기면 여기 한
 * 줄과 오퍼레이터가 설치한 모듈 하나가 늘어난다 — 모르는 타입과 무선언 행은 기본(1)로
 * 풀린다: 빈 화면으로 가는 해석은 없는 해석보다 나쁘다.
 */
public final class CompanionType {
    public final String id;       // 컴패니언 레코드가 선언하는 값 (FleetAgent.type)
    public final String labelKey; // 사람이 읽는 이름(팩 키) — "코딩 에이전트"
    public final String module;   // /ui/<module>/<module>.nocache.js 의 그 이름

    private CompanionType(String id, String labelKey, String module) {
        this.id = id;
        this.labelKey = labelKey;
        this.module = module;
    }

    public static final CompanionType CODING = new CompanionType("1", "type.coding", "companion");

    public static CompanionType[] all() { return new CompanionType[]{CODING}; }

    /** 선언이 대는 타입, 없거나 모르면 기본(코딩 에이전트). */
    public static CompanionType byId(String id) {
        if (id == null || id.isEmpty()) return CODING;
        for (CompanionType t : all()) if (t.id.equals(id)) return t;
        return CODING;
    }
}
