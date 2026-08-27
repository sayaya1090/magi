package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;

/**
 * 화면이 세상에 대는 포트 — interfaces/api(BridgeCompanionSource)가 구현한다.
 * 셸이 있으면 전부 창 브리지 구독(요청 0 추가), 없으면(단독/테스트) 제 회선 폴백.
 */
public interface CompanionSource {
    interface Listener {
        /** 지금 보는 컴패니언 — null은 "컴패니언 화면이 아니다"(그리던 것을 세워 둔다). */
        void context(CompanionContext ctxOrNull);

        /** 전사 전체(파싱된 배열), 아직/못 읽었으면 null. */
        void transcript(Object rowsOrNull);

        /** 턴이 열려 있는가 — 진행 바의 사실. */
        void turn(boolean open, double forSec);
    }

    void start(Listener l);

    /** 명단 — 셸이 호스팅하면 그 구독으로, 단독이면 /fleet 한 번. 사실판이 읽는다. */
    void roster(java.util.function.Consumer<Object> listOrNull);

    /** 컨텍스트 창(/context) — 사실판의 그 줄. */
    void context(CompanionContext ctx, java.util.function.Consumer<Object> infoOrNull);

    /** 지금 접기(/compact) — 답이 오면 컨텍스트를 다시 읽는 것은 호출자의 몫. */
    void compact(CompanionContext ctx, Runnable done);

    /** 대상 컴패니언으로 한 마디 — why는 거부 사유, 성공이면 빈 문자열. */
    void submit(CompanionContext ctx, String text, java.util.function.Consumer<String> why);
}
