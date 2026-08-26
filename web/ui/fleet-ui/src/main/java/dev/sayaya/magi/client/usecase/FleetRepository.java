package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.FleetAgent;

/**
 * 명단을 읽는 포트. 답은 언제나 watch로 돌아온다 — refresh는 요청일 뿐이라,
 * 셸이 스트림을 소유하든(브리지) 이 모듈이 소유하든(단독) 화면 쪽 모양이 같다.
 */
public interface FleetRepository {
    interface RosterHandler {
        /** null은 "못 읽었다"다 — 빈 목록과 못 읽음은 다른 화면이 된다. */
        void roster(FleetAgent[] listOrNull);
    }

    /** 구독. 재접속(단독 모드)과 현재값 재생(브리지 모드)은 구현의 몫. */
    void watch(RosterHandler h);

    /** 재조회 요청 — 첫 로드도, 행동 뒤의 새로고침도 이것이다. */
    void refresh();
}
