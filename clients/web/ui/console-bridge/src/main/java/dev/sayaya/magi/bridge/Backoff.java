package dev.sayaya.magi.bridge;

/**
 * 끊긴 회선에 다시 붙기까지 기다리는 시간 — 1·2·4·8·16·30초, 그다음은 30초.
 *
 * ⚠ **여기 수를 새로 정하지 않는다.** 정본은 `clients/contract/lifecycle-policy.json` 이고,
 * 두 편집기(`Launches`/`launches.ts`)가 이미 그것을 쓴다. 이 콘솔만 **고정 1.5초로 무한
 * 재시도**하고 있었다 — 지터도 물러섬도 없이(실측 2026-09-11, `FetchRosterSource.open` 의
 * error 처리). 그러면 서버가 한 번 재시작할 때 열려 있던 **모든 탭이 같은 순간에** 몰린다.
 * SSE 는 탭마다 회선 하나라 그 쏠림이 편집기보다 크다.
 *
 * GWT 로 컴파일되므로 실행 중에 계약 파일을 읽을 수 없다 — 수는 여기 박히고, **시험이 그
 * 파일과 대조한다**(`RoomsTest`/`CodingScreenTest` 와 같은 자리에서).
 *
 * ⚠ **지터를 먼저 얹고 상한을 나중에 씌운다.** 반대로 하면 30초 단계에 ±20% 가 붙어 36초까지
 * 가고, 계약이 적은 「최대 30초」가 거짓이 된다.
 */
public final class Backoff {
    private Backoff() { }

    private static final int[] STEPS_MS = { 1_000, 2_000, 4_000, 8_000, 16_000, 30_000 };

    public static final int CAP_MS = 30_000;
    public static final double JITTER = 0.2;

    /**
     * [attempt] 번째 재시도까지 기다릴 밀리초. 첫 재시도가 1 이다.
     *
     * [rand] 는 0..1 을 주는 것이면 무엇이든 된다 — 시험은 양 끝(0.0·1.0)을 직접 넣어 경계를
     * 잰다. 무작위로 굴려 「대충 맞다」고 말하는 시험은 상한이 지터에 먹히는 것을 못 짚는다.
     */
    public static int delayMs(int attempt, double rand) {
        int i = Math.min(Math.max(attempt, 1), STEPS_MS.length) - 1;
        double base = STEPS_MS[i];
        double spread = base * JITTER;
        double r = Math.min(Math.max(rand, 0.0), 1.0);
        return (int) Math.min(base - spread + 2 * spread * r, CAP_MS);
    }
}
