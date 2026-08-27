package dev.sayaya.magi.client.domain;

/**
 * 컴패니언 타입 카탈로그 — 타입 키가 어느 UI 모듈과 어느 이름으로 풀리는지의 한 곳.
 *
 * 컴패니언 화면은 두 겹이다: **범용 패널**(companion — 위의 사실판과 오른쪽 판, 어떤
 * 타입이든 같은 것을 답한다)과 그 안의 **자식 UI**(타입의 것 — 가운데와 왼쪽 슬롯).
 * 그래서 카탈로그가 푸는 것은 자식 모듈의 이름이다.
 *
 * 지금은 하나다: 타입 1 = 코딩 에이전트, 오늘의 magi 컴패니언 전부가 이것이고 자식은
 * coding-agent-ui다. 디자인·인프라 관리·리서처 같은 타입이 생기면 여기 한 줄과
 * 오퍼레이터가 설치한 모듈 하나가 는다 — 모르는 타입과 무선언 행은 기본(1)로 풀린다:
 * 빈 화면으로 가는 해석은 없는 해석보다 나쁘다.
 *
 * ⚠ 컴패니언은 이름(타입)을 선언할 뿐 경로를 대지 않는다. 어느 코드가 도는지는 이 콘솔이
 * 정한다 — 워크스페이스가 실어 보낸 스크립트를 감독자 콘솔이 들이는 일은 없다.
 */
public final class CompanionType {
    /** 범용 패널 — 타입이 무엇이든 이 모듈이 레이아웃을 소유한다(목록도 그 모듈의 것이다). */
    public static final String PANEL = "companion";

    public final String id;       // 컴패니언 레코드가 선언하는 값 (FleetAgent.type)
    public final String labelKey; // 사람이 읽는 이름(팩 키) — "코딩 에이전트"
    public final String module;   // 범용 패널 안에 들어갈 자식 UI 모듈의 이름
    public final String panel;    // 그 자식을 품는 패널 — 지금은 하나뿐이다

    private CompanionType(String id, String labelKey, String module) {
        this.id = id;
        this.labelKey = labelKey;
        this.module = module;
        this.panel = PANEL;
    }

    public static final CompanionType CODING = new CompanionType("1", "type.coding", "coding");

    public static CompanionType[] all() { return new CompanionType[]{CODING}; }

    /** 선언이 대는 타입, 없거나 모르면 기본(코딩 에이전트). */
    public static CompanionType byId(String id) {
        if (id == null || id.isEmpty()) return CODING;
        for (CompanionType t : all()) if (t.id.equals(id)) return t;
        return CODING;
    }
}
