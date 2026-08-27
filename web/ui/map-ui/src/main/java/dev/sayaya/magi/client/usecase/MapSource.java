package dev.sayaya.magi.client.usecase;

import java.util.function.Consumer;

/** 맵이 세상에 대는 포트 — 두 읽기, 새 엔드포인트 없음(운영 규칙). */
public interface MapSource {
    void fleet(Consumer<Object> listOrNull);

    void handoffs(Consumer<Object> listOrNull);
}
