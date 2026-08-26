package dev.sayaya.magi.bridge;

/**
 * 셸이 화면 모듈에 건네는 "지금 무엇을 보고 있나" — handbook의 UriSharing에 대응.
 * 컴패니언 타입별 고유 UI 모듈도 이 계약만 알고 로드된다(타입 → 모듈 해석은 셸의 몫).
 */
public final class CompanionContext {
    public String socket; // ?d= — 어느 컴패니언인가
    public String peer;   // ?peer= — 어느 콘솔을 거쳐서인가 (없으면 로컬)
    public String type;   // 컴패니언 레코드가 선언한 타입 (기본 UI면 null)
}
