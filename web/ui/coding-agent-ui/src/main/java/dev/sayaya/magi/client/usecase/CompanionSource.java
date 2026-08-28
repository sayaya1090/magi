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

    /**
     * 이 컴패니언이 낳은 자식들 — 하나를 열어 볼 때 그 아이가 <b>무엇이었는지</b>를 여기서 읽는다
     * (무슨 일을 맡았고, 아직 도는지, 어느 모델로). 전사는 그 아이디로 /transcript에서 온다.
     */
    void subagents(CompanionContext ctx, java.util.function.Consumer<Object> listOrNull);

    /** 지난 한 세션의 전사(/transcript…&session=) — 스트림이 아니라 한 번의 읽기다. */
    void pastTranscript(CompanionContext ctx, String session,
                        java.util.function.Consumer<Object> rowsOrNull);

    /** 대상 컴패니언으로 한 마디 — why는 거부 사유, 성공이면 빈 문자열. */
    void submit(CompanionContext ctx, String text, java.util.function.Consumer<String> why);

    /**
     * 한 카운슬 라운드가 <b>무엇을 보고</b> 판단했는가(/council&round=) — 과제·플랜·보고서·
     * 행동·바뀐 것. 표결을 검증 가능하게 만드는 나머지 반이고, 전사 행에는 그것을 담을 자리가 없다.
     */
    void councilEvidence(CompanionContext ctx, int round, java.util.function.Consumer<Object> seenOrNull);

    /**
     * 컴포저가 쓰다 만 말의 다음(/suggest) — 답은 <b>이어붙일</b> 글이다(쓴 것을 대신하지 않는다).
     * 라우팅된 빠른 프로필이 없으면 서버가 스스로 아무 말도 하지 않는다.
     */
    void suggest(CompanionContext ctx, String prefix, java.util.function.Consumer<String> text);

    /**
     * 돌고 있는 턴을 멈춘다(/interrupt) — 답하는 것과 같은 권한이다: "그건 하지 마"라고 말할 수
     * 있는 사람이 "그만"이라고도 말할 수 있다.
     */
    void interrupt(CompanionContext ctx, java.util.function.Consumer<String> why);
}
