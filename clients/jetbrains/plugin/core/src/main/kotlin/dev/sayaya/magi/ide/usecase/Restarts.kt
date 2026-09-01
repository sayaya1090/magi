package dev.sayaya.magi.ide.usecase

/**
 * 데몬을 **다시 띄워도 되나**. 시도 횟수와 간격만 보는 순수 규칙이다.
 *
 * 처음엔 「프로젝트당 한 번」이었다. 그 한 번이 실패하거나, 떴다가 나중에 죽으면 되살릴 길이
 * 없었다 — 백오프 재접속은 붙기만 하지 띄우지 않는다. 라이브에서 정확히 그 모양이 났다:
 * 데몬이 2분 반 만에 나갔고, 그 뒤 10분 동안 화면은 「실행되지 않음」이었고 아무도 다시
 * 띄우지 않았다(실측 2026-09-01).
 *
 * 그렇다고 매 폴마다 띄우면 3초에 하나씩 프로세스를 낳는다. 그래서 **둘 다** 건다 —
 * 최대 [attempts] 번, 그리고 [interval] 밀리초 안에는 두 번 안 띄운다. 다 쓰면 멈춘다:
 * 계속 실패하는 것을 계속 되풀이하는 것은 회복이 아니라 소음이다.
 */
class Restarts(
    private val attempts: Int = 3,
    private val interval: Long = 60_000,
) {
    private var used = 0

    /**
     * 마지막으로 띄운 때. **`Long.MIN_VALUE` 를 「없음」으로 쓰지 않는다** — `now - MIN_VALUE` 가
     * 뒤집혀 음수가 되고, 그러면 `< interval` 이 늘 참이라 **첫 시도부터 거부된다.** 진짜 시계로도
     * 그렇다(1.7e12 - MIN 이 Long 범위를 넘는다). 없음은 없음으로 적는다.
     */
    private var last: Long? = null

    /** 지금 띄워도 되면 true 를 주고 **한 번을 쓴다**. 묻는 것과 쓰는 것을 가르지 않는 이유는
     *  둘 사이에 다른 스레드가 끼면 둘이 같이 띄우기 때문이다. */
    @Synchronized
    fun take(now: Long): Boolean {
        if (used >= attempts) return false
        last?.let { if (now - it < interval) return false }
        used++
        last = now
        return true
    }

    /** 붙었으면 셈을 되돌린다 — 오래 도는 IDE 에서 「예전에 세 번 실패함」이 영구 금지가 되면 안 된다. */
    @Synchronized
    fun ok() {
        used = 0
        last = null
    }

    @get:Synchronized
    val spent: Boolean get() = used >= attempts
}
