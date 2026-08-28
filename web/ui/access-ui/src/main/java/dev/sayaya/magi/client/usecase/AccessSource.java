package dev.sayaya.magi.client.usecase;

import java.util.function.Consumer;

/** 접근 화면이 세상에 대는 포트 — 명부 읽기와, 한 사람의 줄을 다시 쓰기. */
public interface AccessSource {
    void roster(Consumer<Object> gotOrNull);

    /**
     * 역할·범위(콤마 목록, 빈 값=모든 컴패니언)를 다시 쓴다 — 재조회는 호출자 몫.
     *
     * <p>답은 <b>사유 한 줄</b>이다(빈 것=됐다). 앞서 이 자리는 `Runnable done`이라 명단을
     * 다시 읽는 일만 했다: 거절당하면 바뀌지 않은 명단이 그대로 다시 서고, 사람은 제가 누른
     * 것이 왜 안 됐는지 듣지 못했다 — 하필 남의 접근을 고치는 자리에서.</p>
     */
    void setPerson(String who, String role, String companions, Consumer<String> why);

    void removePerson(String who, Consumer<String> why);
}
