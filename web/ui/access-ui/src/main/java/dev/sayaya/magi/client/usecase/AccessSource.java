package dev.sayaya.magi.client.usecase;

import java.util.function.Consumer;

/** 접근 화면이 세상에 대는 포트 — 명부 읽기와, 한 사람의 줄을 다시 쓰기. */
public interface AccessSource {
    void roster(Consumer<Object> gotOrNull);

    /** 역할·범위(콤마 목록, 빈 값=모든 컴패니언)를 다시 쓴다 — 답이 오면 재조회는 호출자 몫. */
    void setPerson(String who, String role, String companions, Runnable done);

    void removePerson(String who, Runnable done);
}
