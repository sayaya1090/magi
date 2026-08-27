package dev.sayaya.magi.client.usecase;

import java.util.function.Consumer;

/**
 * 회의실이 바깥에 묻는 것 전부 — 구현은 interfaces/api의 것이고, 테스트는 가짜를 문다.
 * 답은 파싱된 네이티브 객체로 온다(전사 행과 같은 규칙: 모양은 BFF가 정하고 화면이 읽는다).
 */
public interface MeetingSource {
    /** 지금 열려 있고 끝난 회의들 — 목록 화면의 아래 절반. */
    void rooms(Consumer<Object> cb);

    /** 회의 하나 — 없으면 null(사라진 방). */
    void room(String id, Consumer<Object> cb);

    /** 이 콘솔이 부를 수 있는 컴패니언들 — 남의 기계의 것은 부를 수 없다. */
    void fleet(Consumer<Object> cb);

    /** 연다. 답은 만들어진 회의(그 주소로 간다), 실패면 why에 사유. */
    void convene(String topic, String[] sockets, Consumer<Object> made, Consumer<String> why);

    /** 한 마디, 또는 지명(call), 또는 바닥 잡기(hold) — 운영의 /meet-say 한 문. */
    void say(String id, String text, String call, boolean hold, Consumer<String> why);

    /** 마무리 / 다시 열기 / 결론을 그 컴패니언에게 건네기. */
    void close(String id, Runnable then);
    void reopen(String id, String why, Runnable then);
    void hand(String id, String who, Consumer<String> why);

    /** 한 컴패니언의 그 방 전사 — "무엇을 하는 중인가"를 보이는 데만 쓴다. */
    void roomRows(String socket, String room, Consumer<Object> cb);
}
