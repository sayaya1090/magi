package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.FleetAgent;

/**
 * 플릿에 하는 두 가지 일의 포트: 턴 세우기, 막힌 질문에 답하기.
 * interfaces 계층(FetchFleetCommander)이 구현한다.
 */
public interface FleetCommander {
    /** 열려 있는 턴을 세운다. then은 성공이든 거부든 답이 온 뒤 — 화면 갱신용. */
    void interrupt(FleetAgent a, Runnable then);

    /**
     * 막힌 질문/퍼미션에 답한다. 답은 텍스트로 간다(옵션 누름도 그 옵션의 원문).
     *
     * <p>then이 {@link Runnable}이 아닌 이유: 답은 <b>거부될 수 있고</b>, 그때 사유가 온다
     * (BFF가 본문에 적어 보낸다 — "그 부름은 이미 답을 받았다" 따위). 답이 서지 못했다는 것은
     * 부탁이 서지 못한 것보다 나쁘다: 컴패니언은 여전히 멈춘 채 그 답을 기다리고 있다.
     * 사유를 받는 쪽만이 사람이 쓴 글을 상자에 되돌려 놓을 수 있다.
     *
     * @param then 답이 온 뒤 — 성공이면 {@code ""}, 거부면 그 사유.
     */
    void answer(FleetAgent a, String text, java.util.function.Consumer<String> then);
}
