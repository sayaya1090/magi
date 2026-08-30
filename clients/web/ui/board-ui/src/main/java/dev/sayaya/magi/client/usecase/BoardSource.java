package dev.sayaya.magi.client.usecase;

import java.util.function.Consumer;

/** 보드가 세상에 대는 포트 — 명단과, 컴패니언 하나의 지난 일들(/history). */
public interface BoardSource {
    void fleet(Consumer<Object> listOrNull);

    void history(String socket, String peer, Consumer<Object> listOrNull);
}
