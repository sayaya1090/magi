package dev.sayaya.magi.client.usecase;

/**
 * 이 브라우저가 말에 대해 아는 것 — 사람이 고른 값과 브라우저가 선호하는 순서, 그리고
 * 고른 값을 적어 두는 자리.
 *
 * 포트로 두는 이유는 규칙과 브라우저를 가르기 위해서다: 어느 말로 그릴지는 순수한 규칙
 * ({@link dev.sayaya.magi.client.domain.Tongue#pick})이고, 그 규칙에 먹일 두 사실만 브라우저의
 * 것이다.
 */
public interface TongueSource {
    /** localStorage 에 적힌 값 — 적힌 적 없으면 빈 문자열. */
    String stored();

    /** navigator.languages — 브라우저가 선호하는 순서. */
    String[] preferred();

    /** 사람이 골랐다 — 다음 방문과 콘솔이 같은 말을 하도록 적어 둔다. */
    void keep(String code);
}
