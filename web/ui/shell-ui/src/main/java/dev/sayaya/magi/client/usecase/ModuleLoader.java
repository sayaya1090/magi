package dev.sayaya.magi.client.usecase;

/**
 * 화면 모듈 스크립트를 문서에 들이는 포트 — interfaces/api가 구현한다.
 *
 * 모듈 이름은 목적지 id(카탈로그 화면)이거나 타입 카탈로그가 해석한 것(컴패니언)이다.
 * 오퍼레이터가 설치한 모듈만 로드한다 — 컴패니언이나 워크스페이스가 실어 온 스크립트는
 * 절대 로드하지 않는다(.magi/plugins와 같은 신뢰 경계).
 */
public interface ModuleLoader {
    /**
     * 이름의 모듈을 한 번만 주입한다 — 이미 들어온 모듈은 조용히 넘어간다.
     *
     * styles는 그 모듈이 제 스타일시트를 함께 싣는지에 대한 카탈로그의 선언이다. 시트를
     * 거는 일이 들이는 쪽에 있는 이유: 화면에게 "네 시트는 네가 걸어라"라고 시키면 잊은
     * 화면이 민얼굴로 뜨고, 그 실패는 화면 코드 어디에도 적혀 있지 않다.
     */
    void ensure(String module, boolean styles);
}
