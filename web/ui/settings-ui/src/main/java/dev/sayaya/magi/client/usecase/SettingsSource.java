package dev.sayaya.magi.client.usecase;

import java.util.function.Consumer;

/** 데몬이 읽는 완성 설정 — 브라우저에만 사는 취향(테마·언어)은 여기 오지 않는다. */
public interface SettingsSource {
    /** 지금의 완성 설정과 고를 수 있는 프로파일들. 능력이 없으면 null. */
    void read(String socket, String peer, Consumer<Object> cb);

    /** 한 칸을 바꾼다 — 누를 때마다 저장한다(그래서 이 화면엔 저장 버튼이 없다). */
    void save(String socket, String peer, String field, String value, Runnable then);
}
