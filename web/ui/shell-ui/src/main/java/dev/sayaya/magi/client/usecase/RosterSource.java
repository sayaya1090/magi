package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.FleetAgent;

/**
 * 명단과 회선 건강을 셸에 대는 포트 — interfaces/api(FetchRosterSource)가 구현한다.
 * 셸이 이 회선의 유일한 소유자다(창당 1스트림); 화면들은 브리지로 얻어 듣는다.
 */
public interface RosterSource {
    interface Listener {
        /** null은 "못 읽었다" — 그대로 흘린다: 첫 로드의 실패는 화면이 말해야 한다. */
        void roster(FleetAgent[] listOrNull);

        /** 스트림이 살아 있는가 — 마스트헤드의 점이 읽는 사실. */
        void link(boolean up);
    }

    /** 스트림 구독. 재접속은 구현의 몫. */
    void start(Listener l);

    /** 한 번 읽기 요청 — 답은 Listener.roster로 돌아온다. */
    void refresh();
}
