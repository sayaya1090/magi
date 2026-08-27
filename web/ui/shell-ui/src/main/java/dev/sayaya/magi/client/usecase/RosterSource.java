package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.FleetAgent;

/**
 * 스트림을 셸에 대는 포트 — interfaces/api(FetchRosterSource)가 구현한다.
 * 셸이 이 회선의 유일한 소유자다(창당 1스트림); 화면들은 브리지로 얻어 듣는다.
 * 명단은 늘 흐르고, aim()으로 컴패니언에 조준되면 전사와 턴이 같은 회선에 실린다.
 */
public interface RosterSource {
    interface Listener {
        /** null은 "못 읽었다" — 그대로 흘린다: 첫 로드의 실패는 화면이 말해야 한다. */
        void roster(FleetAgent[] listOrNull);

        /** 스트림이 살아 있는가 — 마스트헤드의 점이 읽는 사실. */
        void link(boolean up);

        /** 조준된 컴패니언의 전사 전체(파싱된 배열), 못 읽었으면 null. */
        void transcript(Object rowsOrNull);

        /** 조준된 세션에 턴이 열려 있는가 — 진행 바의 사실. forSec은 열린 지 몇 초. */
        void turn(boolean open, double forSec);
    }

    /** 스트림 구독. 재접속은 구현의 몫. */
    void start(Listener l);

    /**
     * 창 전체가 같은 답을 들어야 하는 두 사실 — 어느 magi인가(/console), 나는 무엇을 할 수
     * 있나(/me). 셸이 읽어 창에 올리고 화면들은 그것을 든다: 한 API의 주인은 하나다.
     */
    void facts(java.util.function.Consumer<Object> consoleInfo, java.util.function.Consumer<Object> caps);

    /** 조준을 바꾼다 — null이면 명단 전용(카탈로그 화면). 회선 재개설은 구현의 몫. */
    void aim(String socket, String peer);

    /** 한 번 읽기 요청 — 답은 Listener.roster로 돌아온다. */
    void refresh();
}
