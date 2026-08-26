package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.FleetAgent;

/**
 * 플릿에 하는 두 가지 일의 포트: 턴 세우기, 막힌 질문에 답하기.
 * interfaces 계층(FetchFleetCommander)이 구현한다.
 */
public interface FleetCommander {
    /** 열려 있는 턴을 세운다. then은 성공이든 거부든 답이 온 뒤 — 화면 갱신용. */
    void interrupt(FleetAgent a, Runnable then);

    /** 막힌 질문/퍼미션에 답한다. 답은 텍스트로 간다(옵션 누름도 그 옵션의 원문). */
    void answer(FleetAgent a, String text, Runnable then);
}
