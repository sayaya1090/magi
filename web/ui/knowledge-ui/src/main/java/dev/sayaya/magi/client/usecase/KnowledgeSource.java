package dev.sayaya.magi.client.usecase;

import java.util.function.Consumer;

/**
 * 지식 화면이 세상에 대는 포트 — interfaces/api(FetchKnowledgeSource)가 구현한다.
 * 목록 셋은 파싱된 배열(JsArray) 또는 null("못 읽었다" — 화면이 말해야 한다).
 */
public interface KnowledgeSource {
    void skills(Consumer<Object> listOrNull);

    void wiki(Consumer<Object> listOrNull);

    void mcp(Consumer<Object> listOrNull);

    /**
     * 규칙/기억을 잊는다 — 운영 규칙: project는 그 소켓에서, 나머지는 피어 라우팅.
     * 답은 <b>사유 한 줄</b>이다(빈 것=됐다) — {@link #saveServer}가 이미 쓰던 그 말.
     */
    void forget(String name, String tier, String team, String socket, String peer, Consumer<String> why);

    /** 한 줄 적어 두기 — tier는 global 또는 team(팀명 동반). */
    void remember(String text, String tier, String team, Consumer<String> why);

    /** MCP 서버 제거 — 소켓 없는 것은 이 콘솔의 global. */
    void removeServer(String name, String socket, Consumer<String> why);

    /**
     * MCP 서버 저장(추가=편집: 같은 이름은 그 파일에서 갈아끼운다 — 운영 규칙).
     * why는 거부 사유(필드 이름이 실려 온다), 성공이면 빈 문자열.
     */
    void saveServer(String socket, jsinterop.base.JsPropertyMap<String> fields,
                    java.util.function.Consumer<String> why);

    /** 이 머신의 임베딩 모델(/console) — 빈 값도 진짜 답이다("없음"). */
    void console(Consumer<String> embedModel);
}
