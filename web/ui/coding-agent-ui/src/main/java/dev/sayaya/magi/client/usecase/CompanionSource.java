package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;

/**
 * 이 화면이 세상에 대는 포트 — <b>이 화면만 하는 일</b>로만 이루어져 있다.
 *
 * 한 API를 두 모듈이 읽으면 그것은 단일 원천이 아니라는 증거다. 그래서 여기 없는 것들:
 * <ul>
 *   <li>명단과 스트림(/fleet, /events) — 셸의 것이고, 창에 하나다. 전사와 턴은 그 스트림에서
 *       브리지로 온다(start의 Listener가 받는 것이 그것이다).</li>
 *   <li>계획·컨텍스트·접기(/plan, /context, /compact) — 컴패니언 패널(부모)의 사실판과
 *       오른쪽 판이 읽는 것이고, 이 자식은 그리지 않는다.</li>
 *   <li>답(/answer) — 기다리는 질문은 부모가 알고 부모가 보낸다. 이 화면의 컴포저는 부모가
 *       알려 준 그 문으로 답을 넘긴다(AskSharing).</li>
 * </ul>
 * 남는 것이 이 화면의 것이다: 한 마디 보내기, 그리고 지난 일 층위의 두 읽기.
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

    /** 이 컴패니언의 지난 일 목록(/history) — 그 층위를 그리는 것이 이 화면이라 이 화면의 것이다. */
    void history(CompanionContext ctx, java.util.function.Consumer<Object> listOrNull);

    /** 지난 한 세션의 전사(/transcript…&session=) — 스트림이 아니라 한 번의 읽기다. */
    void pastTranscript(CompanionContext ctx, String session,
                        java.util.function.Consumer<Object> rowsOrNull);

    /** 대상 컴패니언으로 한 마디 — why는 거부 사유, 성공이면 빈 문자열. */
    void submit(CompanionContext ctx, String text, java.util.function.Consumer<String> why);
}
