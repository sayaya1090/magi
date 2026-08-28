package dev.sayaya.magi.client.usecase;

import java.util.function.Consumer;

/** 데몬이 읽는 완성 설정 — 브라우저에만 사는 취향(테마·언어)은 여기 오지 않는다. */
public interface SettingsSource {
    /** 지금의 완성 설정과 고를 수 있는 프로파일들. 능력이 없으면 null. */
    void read(String socket, String peer, Consumer<Object> cb);

    /** 한 칸을 바꾼다 — 누를 때마다 저장한다(그래서 이 화면엔 저장 버튼이 없다). */
    void save(String socket, String peer, String field, String value, Runnable then);

    /** 이 콘솔이 아는 모델 프로파일들 — 위의 완성 설정이 고르는 그 백엔드들이다. */
    void profiles(String socket, Consumer<Object> list);

    /**
     * CLI 백엔드가 답한 제공자들(/providers) — 이름·주소·그 주소가 서빙하는 모델들.
     * 이 콘솔의 설정이 아니라 <b>지금 돌고 있는 것</b>이라, 고르면 주소와 모델이 함께 채워진다.
     */
    void providers(Consumer<Object> list);

    /** 프로파일 하나를 저장하거나(delete=false) 지운다. why는 거부 사유, 성공은 빈 문자열. */
    void saveProfile(String socket, String name, String baseUrl, String model, String key,
                     boolean delete, Consumer<String> why);

    /** 이 콘솔의 푸시 공개키 — 없으면 빈 문자열(키 없는 콘솔은 알림을 보낼 수 없다). */
    void pushKey(Consumer<String> key);

    /** 이 브라우저를 구독에 올리거나(delete=false) 내린다. */
    void push(String endpoint, String p256dh, String auth, boolean delete, Runnable then);
}
