package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;

/**
 * 화면 모듈 스크립트를 문서에 들이는 포트 — interfaces/api가 구현한다.
 *
 * 나중 일(설계 반영): 컴패니언 타입별 고유 UI는 이 포트의 구현이 (목적지, 컴패니언 타입)로
 * 다른 모듈을 해석하는 것으로 들어온다 — 단, 운영자가 설치한 모듈만. 컴패니언이나
 * 워크스페이스가 실어 온 스크립트는 절대 로드하지 않는다(.magi/plugins와 같은 신뢰 경계).
 */
public interface ModuleLoader {
    /** 목적지의 모듈을 한 번만 주입한다 — 이미 들어온 모듈은 조용히 넘어간다. */
    void ensure(Destination d);
}
